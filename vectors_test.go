package tinydelta

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// vector is one line of testdata/vectors.txt, the conformance vectors:
// what any decoder must produce for a dictionary, patch and capacity.
type vector struct {
	name        string
	capacity    int
	dict, patch []byte
	result      string // OK, MALFORMED or TOO_LARGE
	target      []byte // with OK only
}

// vectorCodes are the C decoders' result codes, as the C test drivers
// print them.
var vectorCodes = map[string]string{"MALFORMED": "-1", "TOO_LARGE": "-2"}

func readVectors(t *testing.T) []vector {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "vectors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var vectors []vector
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 1<<20)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, " ")
		bad := func(why string) { t.Fatalf("testdata/vectors.txt:%d: %s", line, why) }
		if len(fields) < 5 {
			bad("too few fields")
		}
		v := vector{name: fields[0], result: fields[4]}
		if seen[v.name] {
			bad("duplicate name " + v.name)
		}
		seen[v.name] = true
		if v.capacity, err = strconv.Atoi(fields[1]); err != nil || v.capacity < 1 {
			bad("capacity must be a decimal number >= 1")
		}
		unhex := func(s string) []byte {
			if s == "-" {
				return nil
			}
			b, err := hex.DecodeString(s)
			if err != nil || len(b) == 0 {
				bad("bad hex field")
			}
			return b
		}
		v.dict, v.patch = unhex(fields[2]), unhex(fields[3])
		switch {
		case v.result == "OK" && len(fields) == 6:
			v.target = unhex(fields[5])
			if len(v.target) > v.capacity {
				bad("target exceeds capacity")
			}
		case vectorCodes[v.result] != "" && len(fields) == 5:
		default:
			bad("result must be OK followed by the target, or MALFORMED or TOO_LARGE alone")
		}
		vectors = append(vectors, v)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("no vectors in testdata/vectors.txt")
	}
	return vectors
}

// goResult runs the Go decoder on a vector and names its result.
func goResult(v vector) (string, []byte) {
	target, err := DecodeReaderLimit(v.dict, bytes.NewReader(v.patch), v.capacity)
	switch {
	case err == nil:
		return "OK", target
	case errors.Is(err, ErrTooLarge):
		return "TOO_LARGE", nil
	default:
		return "MALFORMED", nil
	}
}

// TestVectors checks the Go decoder against every conformance vector.
func TestVectors(t *testing.T) {
	for _, v := range readVectors(t) {
		result, target := goResult(v)
		if result != v.result || !bytes.Equal(target, v.target) {
			t.Errorf("%s: got %s (%d B), want %s (%d B)", v.name, result, len(target), v.result, len(v.target))
		}
	}
}

// TestCVectors checks the C memory decoder, and the C callback decoder at
// every scratch size, directly and through the page cache, against every
// conformance vector.
func TestCVectors(t *testing.T) {
	vectors := readVectors(t)
	memory := buildCDriver(t)
	ioDrivers := buildCIODrivers(t)
	dir := t.TempDir()

	check := func(v vector, decoder string, out []byte, code string, ok bool) {
		t.Helper()
		got := "OK"
		if !ok {
			got = code
		}
		want := v.result
		if code := vectorCodes[v.result]; code != "" {
			want = code
		}
		if got != want || (ok && !bytes.Equal(out, v.target)) {
			t.Errorf("%s (%s): got %s (%d B), want %s (%d B)", v.name, decoder, got, len(out), want, len(v.target))
		}
	}
	for _, v := range vectors {
		d, p, o := filepath.Join(dir, "d"), filepath.Join(dir, "p"), filepath.Join(dir, "o")
		os.WriteFile(d, v.dict, 0o644)
		os.WriteFile(p, v.patch, 0o644)
		os.Remove(o)
		stdout, err := cCommand(memory, d, p, o, fmt.Sprint(v.capacity)).Output()
		var exit *exec.ExitError
		switch {
		case err == nil:
			out, _ := os.ReadFile(o)
			check(v, "memory decoder", out, "", true)
		case errors.As(err, &exit) && exit.ExitCode() == 1:
			check(v, "memory decoder", nil, string(bytes.TrimSpace(stdout)), false)
		default:
			t.Fatalf("%s: C memory decoder: %v: %s", v.name, err, stdout)
		}

		for _, b := range ioDrivers {
			for _, page := range []int{0, 1, 7, 256} {
				out, code, ok := runCIO(t, b.path, page, v.dict, v.patch, v.capacity)
				check(v, fmt.Sprintf("callback decoder, scratch %d, page %d", b.scratch, page), out, code, ok)
			}
		}
	}
}
