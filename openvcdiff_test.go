// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"compress/zlib"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSizeVsOpenVCDiff compares patch sizes against the real open-vcdiff
// encoder (built from source; set OPENVCDIFF_BIN to its `vcdiff` binary).
// open-vcdiff runs with -target_matches, its best setting for size, and
// no checksum. open-vcdiff's output is also shown zlib-wrapped at best
// compression, since that's the pipeline tinydelta replaces; tinydelta
// itself ships unwrapped.
func TestSizeVsOpenVCDiff(t *testing.T) {
	bin := os.Getenv("OPENVCDIFF_BIN")
	if bin == "" {
		t.Skip("OPENVCDIFF_BIN not set")
	}

	r := rand.New(rand.NewSource(3))
	base := textFile(r, fileSize)
	half := fileSize / 2
	scattered := append([]byte{}, base...)
	for i := 0; i < 10; i++ {
		scattered[r.Intn(len(scattered))] ^= 0x20
	}
	config := append([]byte{}, base...)
	for i := 0; i < 5; i++ {
		at := r.Intn(len(config) - 4)
		copy(config[at:at+4], "####")
	}
	cases := []struct {
		name      string
		dict, tgt []byte
	}{
		{"identical", base, base},
		{"one byte changed", base, splice(base, 1500, 1, []byte{'#'})},
		{"5 sparse 4B edits", base, config},
		{"10 scattered edits", base, scattered},
		{"insert 100 bytes", base, splice(base, 1000, 0, randomFile(r, 100))},
		{"delete 200 bytes", base, splice(base, 800, 200, nil)},
		{"append 256 bytes", base, append(append([]byte{}, base...), textFile(r, 256)...)},
		{"truncate", base, base[:2000]},
		{"swap halves", base, append(append([]byte{}, base[half:]...), base[:half]...)},
		{"unrelated text", base, textFile(r, fileSize)},
		{"unrelated random", randomFile(r, fileSize), randomFile(r, fileSize)},
	}

	t.Logf("%-20s %8s %8s %8s", "case", "tiny", "ovc", "ovc+z")
	var sums [3]int
	for _, c := range cases {
		tiny := encode(c.dict, c.tgt, MaxSize)
		ovc := openVCDiffEncode(t, bin, c.dict, c.tgt)
		row := [3]int{len(tiny), len(ovc), len(zlibBest(t, ovc))}
		for i := range sums {
			sums[i] += row[i]
		}
		t.Logf("%-20s %8d %8d %8d", c.name, row[0], row[1], row[2])
	}
	t.Logf("%-20s %8d %8d %8d", "TOTAL", sums[0], sums[1], sums[2])
}

func openVCDiffEncode(t *testing.T, bin string, dict, tgt []byte) []byte {
	t.Helper()
	dir := t.TempDir()
	d, g, out := filepath.Join(dir, "d"), filepath.Join(dir, "t"), filepath.Join(dir, "o")
	os.WriteFile(d, dict, 0o600)
	os.WriteFile(g, tgt, 0o600)
	cmd := exec.Command(bin, "encode", "-dictionary", d, "-target", g, "-delta", out, "-target_matches")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("open-vcdiff encode: %v: %s", err, msg)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func zlibBest(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(b)
	w.Close()
	return buf.Bytes()
}
