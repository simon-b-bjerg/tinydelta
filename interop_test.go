// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"math/rand"
	"testing"
)

// There is no third-party encoder or decoder for this format to check
// against -- it's custom -- so these tests check self-consistency:
// encode -> Decode round trips on fixed fixtures and random mutations,
// for both encoders, plus size bounds where a case has an obvious one.

const fileSize = 3 * 1024

func textFile(r *rand.Rand, n int) []byte {
	words := []string{"the", "quick", "brown", "fox", "jumps", "over", "lazy", "dog",
		"delta", "window", "copy", "add", "run", "address", "cache", "source",
		"target", "encoder", "decoder", "instruction", "diff", "format"}
	var b []byte
	for len(b) < n {
		b = append(b, words[r.Intn(len(words))]...)
		if r.Intn(12) == 0 {
			b = append(b, ".\n"...)
		} else {
			b = append(b, ' ')
		}
	}
	return b[:n]
}

func randomFile(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	r.Read(b)
	return b
}

func splice(b []byte, at, del int, ins []byte) []byte {
	out := append([]byte{}, b[:at]...)
	out = append(out, ins...)
	return append(out, b[at+del:]...)
}

func TestRoundTrip3KBFixtures(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	base := textFile(r, fileSize)
	half := fileSize / 2
	scattered := append([]byte{}, base...)
	for i := 0; i < 10; i++ {
		scattered[r.Intn(len(scattered))] ^= 0x20
	}
	cases := []struct {
		name      string
		dict, tgt []byte
	}{
		{"identical", base, base},
		{"one byte changed", base, splice(base, 1500, 1, []byte{'#'})},
		{"insert 100 bytes", base, splice(base, 1000, 0, randomFile(r, 100))},
		{"delete 200 bytes", base, splice(base, 800, 200, nil)},
		{"append 256 bytes", base, append(append([]byte{}, base...), textFile(r, 256)...)},
		{"truncate", base, base[:2000]},
		{"swap halves", base, append(append([]byte{}, base[half:]...), base[:half]...)},
		{"10 scattered edits", base, scattered},
		{"unrelated text", base, textFile(r, fileSize)},
		{"unrelated random", randomFile(r, fileSize), randomFile(r, fileSize)},
		{"empty dict", nil, base},
		{"empty target", base, []byte{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			patch := encode(c.dict, c.tgt, MaxSize)
			got, err := Decode(c.dict, withHeader(patch))
			if err != nil || !bytes.Equal(got, c.tgt) {
				t.Fatalf("round trip failed (err %v)", err)
			}
			t.Logf("dict %4d B  tgt %4d B  patch %4d B", len(c.dict), len(c.tgt), len(patch))
		})
	}
}

func TestRoundTripRandomMutations(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 500; i++ {
		var dict []byte
		if i%2 == 0 {
			dict = textFile(r, fileSize)
		} else {
			dict = randomFile(r, fileSize)
		}
		tgt := append([]byte{}, dict...)
		for e := r.Intn(20); e > 0; e-- {
			at := r.Intn(len(tgt) + 1)
			switch r.Intn(4) {
			case 0:
				tgt = splice(tgt, at, 0, randomFile(r, r.Intn(64)))
			case 1:
				tgt = splice(tgt, at, min(r.Intn(64), len(tgt)-at), nil)
			case 2:
				from := r.Intn(len(tgt) + 1)
				n := min(r.Intn(300), len(tgt)-from)
				tgt = splice(tgt, at, 0, append([]byte{}, tgt[from:from+n]...))
			case 3:
				tgt = splice(tgt, at, 0, bytes.Repeat([]byte{byte(r.Intn(256))}, r.Intn(100)))
			}
		}
		greedy := encodeGreedy(dict, tgt, MaxSize)
		if got, err := Decode(dict, withHeader(greedy)); err != nil || !bytes.Equal(got, tgt) {
			t.Fatalf("#%d: greedy round trip failed (err %v)", i, err)
		}
		if i%10 != 0 {
			continue // the optimal encoder is ~100x slower; check a tenth of the pairs
		}
		opt := encode(dict, tgt, MaxSize)
		if got, err := Decode(dict, withHeader(opt)); err != nil || !bytes.Equal(got, tgt) {
			t.Fatalf("#%d: optimal round trip failed (err %v)", i, err)
		}
		if len(opt) > len(greedy) {
			t.Fatalf("#%d: optimal patch (%d B) larger than greedy (%d B)", i, len(opt), len(greedy))
		}
	}
}

// TestSameLengthConfigSparseEdits is the use case this format's SKIP
// opcode and dictionary cursor were designed for: a same-length
// dictionary/target pair differing only in a handful of scattered small
// fields. Nearly the whole file should collapse into a tiny number of
// cheap SKIP instructions (no address field at all) around the edits.
func TestSameLengthConfigSparseEdits(t *testing.T) {
	r := rand.New(rand.NewSource(50))
	dict := textFile(r, 2048)
	target := append([]byte{}, dict...)
	edits := 5
	for i := 0; i < edits; i++ {
		at := r.Intn(len(target) - 4)
		copy(target[at:at+4], []byte{'#', '#', '#', '#'})
	}
	if len(target) != len(dict) {
		t.Fatalf("test setup: target length %d != dict length %d", len(target), len(dict))
	}

	patch := encode(dict, target, MaxSize)
	got, err := Decode(dict, withHeader(patch))
	if err != nil || !bytes.Equal(got, target) {
		t.Fatalf("round trip failed (err %v)", err)
	}
	// 5 edits of 4 bytes each (~2 bytes ADD overhead) plus up to 6 SKIPs
	// stitching the unedited runs together (~2 bytes each): comfortably
	// under 50 bytes for a 2048-byte file is the point being tested, not
	// a tight bound on the exact number.
	if len(patch) > 50 {
		t.Errorf("patch is %d bytes for %d scattered 4-byte edits in a %d-byte file, expected well under 50", len(patch), edits, len(dict))
	}
	t.Logf("dict/target %d B, %d edits -> patch %d B", len(dict), edits, len(patch))
}

// TestOptimalEncoderDirectly checks encodeOptimal on its own, without
// Encode's "keep whichever is smaller" safety net that could hide a bug in
// it: its patches must round-trip and never exceed greedy's.
func TestOptimalEncoderDirectly(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	base := textFile(r, fileSize)
	half := fileSize / 2
	cases := []struct {
		name      string
		dict, tgt []byte
	}{
		{"identical", base, base},
		{"one byte changed", base, splice(base, 1500, 1, []byte{'#'})},
		{"insert 100 bytes", base, splice(base, 1000, 0, randomFile(r, 100))},
		{"delete 200 bytes", base, splice(base, 800, 200, nil)},
		{"append 256 bytes", base, append(append([]byte{}, base...), textFile(r, 256)...)},
		{"swap halves", base, append(append([]byte{}, base[half:]...), base[:half]...)},
		{"unrelated text", base, textFile(r, fileSize)},
		{"unrelated random", randomFile(r, fileSize), randomFile(r, fileSize)},
		{"empty dict", nil, base},
		{"run of one byte", base, bytes.Repeat([]byte{'z'}, fileSize)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			greedy := encodeGreedy(c.dict, c.tgt, MaxSize)
			opt := encodeOptimal(c.dict, c.tgt, greedy, MaxSize, MaxSize)
			if opt == nil {
				t.Fatal("encodeOptimal declined an input well under its size limit")
			}
			if got, err := Decode(c.dict, withHeader(opt)); err != nil || !bytes.Equal(got, c.tgt) {
				t.Fatalf("optimal round trip failed (err %v)", err)
			}
			if len(opt) > len(greedy) {
				t.Errorf("optimal %d B > greedy %d B", len(opt), len(greedy))
			}
			t.Logf("greedy %5d B  optimal %5d B", len(greedy), len(opt))
		})
	}
}

// TestEncodeFallsBackToGreedyWhenTooLarge checks that Encode still
// produces a valid patch (greedy's) when the input exceeds the optimal
// encoder's table limit.
func TestEncodeFallsBackToGreedyWhenTooLarge(t *testing.T) {
	saved := optMaxStates
	optMaxStates = 10
	defer func() { optMaxStates = saved }()

	r := rand.New(rand.NewSource(6))
	dict := textFile(r, fileSize)
	tgt := splice(dict, 1000, 50, textFile(r, 80))
	greedy := encodeGreedy(dict, tgt, MaxSize)
	if opt := encodeOptimal(dict, tgt, greedy, MaxSize, MaxSize); opt != nil {
		t.Fatal("encodeOptimal ran despite exceeding optMaxStates")
	}
	patch := encode(dict, tgt, MaxSize)
	if !bytes.Equal(patch, greedy) {
		t.Error("Encode over the size limit should return greedy's patch")
	}
	if got, err := Decode(dict, withHeader(patch)); err != nil || !bytes.Equal(got, tgt) {
		t.Fatalf("round trip failed (err %v)", err)
	}
}
