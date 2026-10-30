package tinydelta

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
)

// fuzzMaxSize bounds the fuzzed dictionary and target. The optimizing
// encoder costs roughly 100 ms per few KB, so larger inputs would slow the
// fuzzer to a crawl without reaching any code that small inputs don't.
const fuzzMaxSize = 1024

// applyEdits derives a target from dict by running an edit script, so that
// the fuzzer works on related pairs, as in real use. Fuzzing dictionary and
// target independently would give unrelated data almost every time, which
// encodes as one long ADD and never exercises COPY, SKIP, COPY_TARGET or
// the cursor handling.
//
// The script is read 4 bytes at a time: kind, position, length, value.
//
//	kind%5 == 0  overwrite length bytes with value
//	kind%5 == 1  insert length copies of value     (shifts the alignment)
//	kind%5 == 2  delete length bytes               (shifts the alignment)
//	kind%5 == 3  insert a copy of length bytes from elsewhere in the target
//	kind%5 == 4  append length raw bytes taken from the rest of the script
func applyEdits(dict, script []byte) []byte {
	target := append([]byte{}, dict...)
	for len(script) >= 4 && len(target) <= fuzzMaxSize {
		kind, length, value := script[0], int(script[2]), script[3]
		pos := 0
		if len(target) > 0 {
			pos = int(script[1]) * len(target) / 256
		}
		script = script[4:]

		switch kind % 5 {
		case 0:
			for i := pos; i < len(target) && i < pos+length; i++ {
				target[i] = value
			}
		case 1:
			target = splice(target, pos, 0, bytes.Repeat([]byte{value}, length))
		case 2:
			target = splice(target, pos, min(length, len(target)-pos), nil)
		case 3:
			if len(target) > 0 {
				from := int(value) * len(target) / 256
				n := min(length, len(target)-from)
				target = splice(target, pos, 0, append([]byte{}, target[from:from+n]...))
			}
		case 4:
			n := min(length, len(script))
			target = append(target, script[:n]...)
			script = script[n:]
		}
	}
	if len(target) > fuzzMaxSize {
		target = target[:fuzzMaxSize]
	}
	return target
}

// FuzzEncodeRoundTrip checks the encoders against the decoder. For a
// dictionary, a target derived from it by an edit script, and an
// instruction length limit, both encoders must produce a patch that:
//
//   - decodes back to exactly the target;
//   - decodes with an output limit of exactly len(target), and fails with
//     ErrTooLarge at one byte less, so a device that knows the final length
//     can rely on the patch not producing more;
//   - has no instruction longer than the limit (when one is set);
//
// and the optimizing encoder's patch must be no larger than the greedy
// one's, which Encode and EncodeWith document.
//
// `go test` runs the seed corpus below plus any failing inputs saved under
// testdata/fuzz/FuzzEncodeRoundTrip. To search for new failures:
//
//	go test -run '^$' -fuzz '^FuzzEncodeRoundTrip$' -fuzztime 5m .
//
// A failure is saved under testdata/fuzz/ and becomes a regression test
// once committed.
func FuzzEncodeRoundTrip(f *testing.F) {
	r := rand.New(rand.NewSource(3))
	text := textFile(r, 600)
	random := randomFile(r, 300)

	f.Add([]byte{}, []byte{}, uint16(0))
	f.Add(text, []byte{}, uint16(0))                             // identical: SKIP
	f.Add(text, []byte{0, 100, 6, 'X'}, uint16(0))               // overwrite
	f.Add(text, []byte{1, 40, 3, 'Y', 2, 200, 9, 0}, uint16(0))  // insert, delete
	f.Add(text, []byte{3, 128, 120, 10}, uint16(16))             // internal repeat
	f.Add([]byte{}, []byte{1, 0, 200, 'z'}, uint16(0))           // run: COPY_TARGET
	f.Add(random, []byte{4, 0, 50, 0}, uint16(0))                // unrelated tail: ADD
	f.Add(random, []byte{2, 10, 30, 0, 1, 180, 5, 7}, uint16(1)) // every instruction 1 B
	f.Add(text, []byte{0, 10, 4, 'a', 2, 90, 17, 0, 3, 5, 60, 200}, uint16(7))

	f.Fuzz(func(t *testing.T, dict, script []byte, maxLen uint16) {
		if len(dict) > fuzzMaxSize {
			dict = dict[:fuzzMaxSize]
		}
		target := applyEdits(dict, script)

		var sizes [2]int
		for i, greedy := range []bool{true, false} {
			patch, err := EncodeWith(dict, target, EncodeOptions{Greedy: greedy, MaxInstructionLen: int(maxLen)})
			if err != nil {
				t.Fatalf("greedy=%v: encode: %v", greedy, err)
			}
			sizes[i] = len(patch)

			got, err := Decode(dict, patch)
			if err != nil {
				t.Fatalf("greedy=%v: decode: %v", greedy, err)
			}
			if !bytes.Equal(got, target) {
				t.Fatalf("greedy=%v: round trip produced %d B, want %d B", greedy, len(got), len(target))
			}

			// DecodeReaderLimit treats a limit <= 0 as "no limit", so the
			// one-less check needs a target of at least 2 bytes.
			if len(target) >= 2 {
				if _, err := DecodeReaderLimit(dict, bytes.NewReader(patch), len(target)); err != nil {
					t.Fatalf("greedy=%v: decode capped at the exact length: %v", greedy, err)
				}
				if _, err := DecodeReaderLimit(dict, bytes.NewReader(patch), len(target)-1); !errors.Is(err, ErrTooLarge) {
					t.Fatalf("greedy=%v: decode capped one byte short: got %v, want ErrTooLarge", greedy, err)
				}
			}

			if maxLen > 0 {
				for _, n := range instructionLens(t, patch) {
					if n > int(maxLen) {
						t.Fatalf("greedy=%v: instruction of %d B exceeds MaxInstructionLen %d", greedy, n, maxLen)
					}
				}
			}
		}
		if sizes[1] > sizes[0] {
			t.Fatalf("optimizing patch %d B is larger than greedy patch %d B", sizes[1], sizes[0])
		}
	})
}

// TestDumpFuzzInput hex dumps a FuzzEncodeRoundTrip input, such as a
// failing one the fuzzer wrote to testdata/fuzz/, from the file named by
// $TINYDELTA_FUZZ_DUMP; it is skipped when that is unset. It shows the
// arguments as the fuzz function sees them, plus the target derived from
// them, so a failure can be read and turned into a regression test.
func TestDumpFuzzInput(t *testing.T) {
	path := os.Getenv("TINYDELTA_FUZZ_DUMP")
	if path == "" {
		t.Skip("TINYDELTA_FUZZ_DUMP not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Corpus file format: a "go test fuzz v1" line, then one Go literal per
	// argument: []byte("..."), then uint16(n).
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 || lines[0] != "go test fuzz v1" {
		t.Fatalf("%s: not a FuzzEncodeRoundTrip corpus file", path)
	}
	var args [2][]byte
	for i := range args {
		literal, ok := strings.CutPrefix(lines[1+i], "[]byte(")
		literal, ok2 := strings.CutSuffix(literal, ")")
		value, err := strconv.Unquote(literal)
		if !ok || !ok2 || err != nil {
			t.Fatalf("%s: argument %d: cannot parse %q", path, i+1, lines[1+i])
		}
		args[i] = []byte(value)
	}
	dict, script := args[0], args[1]
	if len(dict) > fuzzMaxSize {
		dict = dict[:fuzzMaxSize]
	}
	fmt.Printf("maxLen: %s\n\ndict (%d B):\n%s\nscript (%d B):\n%s\ntarget (%d B):\n%s",
		lines[3], len(dict), hex.Dump(dict), len(script), hex.Dump(script),
		len(applyEdits(dict, script)), hex.Dump(applyEdits(dict, script)))
}
