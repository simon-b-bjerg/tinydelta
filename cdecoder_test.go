package tinydelta

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cDriverHelpers holds what the C test drivers share, written to also run
// under a bare-metal instruction simulator (e.g. msp430-elf-run):
//
//   - args_from_file: the simulator's startup code calls main() with no
//     arguments, so scripts/sim-run.sh leaves them in the file "args", one
//     per line, in the working directory.
//   - slurp reads a whole file to its end; seeking to the end to find the
//     length doesn't work under the MSP430 simulator.
//
// Needs <stdio.h> and <stdlib.h>.
const cDriverHelpers = `
static uint8_t *slurp(const char *f, uint32_t *n) {
	enum { CHUNK = 1024 };
	FILE *fp = fopen(f, "rb");
	uint8_t *b = NULL;
	size_t len = 0, got;
	if (!fp) exit(2);
	do {
		b = realloc(b, len + CHUNK);
		if (!b) exit(2);
		got = fread(b + len, 1, CHUNK, fp);
		len += got;
	} while (got == CHUNK);
	fclose(fp);
	*n = (uint32_t)len;
	return b;
}
static void args_from_file(int *argc, char ***argv) {
	static char text[1024], *args[8];
	int count = 1;
	size_t n;
	FILE *fp;
	if (*argc > 1 || !(fp = fopen("args", "rb"))) return;
	n = fread(text, 1, sizeof text - 1, fp);
	fclose(fp);
	text[n] = 0;
	args[0] = "driver";
	for (char *s = text; *s && count < 7; count++) {
		args[count] = s;
		while (*s && *s != '\n') s++;
		if (*s) *s++ = 0;
	}
	*argc = count;
	*argv = args;
}
`

// cCapacity is the target capacity the C tests use where any target
// should fit: larger than every test target, and small enough for the RAM
// of a 16-bit microcontroller (MSP430 under the simulator).
const cCapacity = 8 << 10

// cDriver feeds files to the C reference decoder (c/tinydelta_decode.c):
// driver <dict> <patch> <out> <out_cap>. It exits 0 on success, 1 when the
// decoder rejects the input, after printing the result code.
const cDriver = `#include <stdio.h>
#include <stdlib.h>
#include "tinydelta.h"
` + cDriverHelpers + `int main(int argc, char **argv) {
	args_from_file(&argc, &argv);
	if (argc != 5) return 2;
	uint32_t dn, pn, on;
	uint8_t *d = slurp(argv[1], &dn), *p = slurp(argv[2], &pn);
	uint32_t cap = (uint32_t)atol(argv[4]);
	uint8_t *o = malloc(cap ? cap : 1);
	tinydelta_result rc = tinydelta_decode(d, dn, p, pn, o, cap, &on);
	if (rc != TINYDELTA_OK) {
		printf("%d\n", rc);
		return 1;
	}
	FILE *fp = fopen(argv[3], "wb");
	fwrite(o, 1, on, fp);
	fclose(fp);
	return 0;
}
`

// cCompile compiles C test binaries with $TINYDELTA_CC (default cc). It may
// carry flags and name a cross compiler, e.g. "arm-linux-gnueabihf-gcc
// -static", in which case $TINYDELTA_C_RUNNER (e.g. "qemu-arm") runs the
// results; see cCommand. Skips the test if the compiler isn't installed.
func cCompile(t *testing.T, out string, args ...string) {
	t.Helper()
	cc := strings.Fields(os.Getenv("TINYDELTA_CC"))
	if len(cc) == 0 {
		cc = []string{"cc"}
	}
	if _, err := exec.LookPath(cc[0]); err != nil {
		t.Skipf("no C compiler (%s) on PATH", cc[0])
	}
	all := append(append(cc[1:len(cc):len(cc)], "-std=c99", "-O2", "-Wall", "-Wextra", "-Werror", "-Ic"), args...)
	if msg, err := exec.Command(cc[0], append(all, "-o", out)...).CombinedOutput(); err != nil {
		t.Fatalf("compiling %s: %v\n%s", filepath.Base(out), err, msg)
	}
}

// cCommand runs a binary from cCompile, through $TINYDELTA_C_RUNNER if set.
func cCommand(bin string, args ...string) *exec.Cmd {
	if runner := strings.Fields(os.Getenv("TINYDELTA_C_RUNNER")); len(runner) > 0 {
		return exec.Command(runner[0], append(append(runner[1:], bin), args...)...)
	}
	return exec.Command(bin, args...)
}

// buildCDriver compiles cDriver with the memory decoder and returns the
// binary's path.
func buildCDriver(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "decode")
	os.WriteFile(filepath.Join(dir, "driver.c"), []byte(cDriver), 0o644)
	cCompile(t, bin, "c/tinydelta_decode.c", filepath.Join(dir, "driver.c"))
	return bin
}

// TestCDecoderMatchesGo compiles the C reference decoder (skipped when no C
// compiler is installed) and checks it against Decode: on valid patches
// from both encoders it must reproduce the target exactly, and on
// truncated or corrupted patches, or with a too-small output buffer, it
// must accept or reject exactly the inputs Go does, with identical output.
func TestCDecoderMatchesGo(t *testing.T) {
	dir := t.TempDir()
	bin := buildCDriver(t)

	var failure string // how the last failed run ended, for test messages
	cDecode := func(dict, patch []byte, capacity int) ([]byte, bool) {
		d, p, o := filepath.Join(dir, "d"), filepath.Join(dir, "p"), filepath.Join(dir, "o")
		os.WriteFile(d, dict, 0o644)
		os.WriteFile(p, patch, 0o644)
		if msg, err := cCommand(bin, d, p, o, fmt.Sprint(capacity)).CombinedOutput(); err != nil {
			failure = fmt.Sprintf("%v: %s", err, bytes.TrimSpace(msg))
			return nil, false
		}
		out, _ := os.ReadFile(o)
		return out, true
	}
	compare := func(label string, dict, patch []byte, capacity int) {
		t.Helper()
		want, gErr := decodeAsC(dict, patch, capacity)
		got, cOK := cDecode(dict, patch, capacity)
		if (gErr == nil) != cOK || (cOK && !bytes.Equal(got, want)) {
			t.Errorf("%s: Go err=%v, C accepted=%v (outputs equal: %v)", label, gErr, cOK, bytes.Equal(got, want))
		}
	}

	r := rand.New(rand.NewSource(12))
	base := textFile(r, fileSize)
	pairs := []struct {
		name      string
		dict, tgt []byte
	}{
		{"identical", base, base},
		{"edits", base, splice(splice(base, 900, 7, []byte("EDITED")), 2000, 0, randomFile(r, 30))},
		{"run", base, append(append([]byte{}, base[:500]...), bytes.Repeat([]byte{'z'}, 400)...)},
		{"unrelated", base, textFile(r, fileSize)},
		{"empty dict", nil, base},
		{"empty target", base, nil},
	}
	const big = cCapacity
	for _, p := range pairs {
		if len(p.tgt) >= big {
			t.Fatalf("%s: target does not fit cCapacity", p.name)
		}
		for _, enc := range []struct {
			name string
			fn   func(dict, target []byte, maxLen int) []byte
		}{{"optimal", encode}, {"greedy", encodeGreedy}} {
			patch := withHeader(enc.fn(p.dict, p.tgt, MaxSize))
			label := p.name + "/" + enc.name
			got, ok := cDecode(p.dict, patch, big)
			if !ok || !bytes.Equal(got, p.tgt) {
				t.Fatalf("%s: C decoder did not reproduce the target (%s)", label, failure)
			}
			compare(label+" cap=len", p.dict, patch, max(len(p.tgt), 1))
			if len(p.tgt) > 1 {
				compare(label+" cap=len-1", p.dict, patch, len(p.tgt)-1)
			}
			for k := 0; k < 12 && len(patch) > 0; k++ {
				compare(fmt.Sprintf("%s cut=%d", label, k), p.dict, patch[:r.Intn(len(patch))], big)
				m := append([]byte{}, patch...)
				m[r.Intn(len(m))] = byte(r.Intn(256))
				compare(fmt.Sprintf("%s flip=%d", label, k), p.dict, m, big)
			}
		}
	}
}

// decodeAsC is DecodeReaderLimit restricted to the format versions the C
// decoders implement (version 1 only, so far), for comparing the two.
func decodeAsC(dict, patch []byte, capacity int) ([]byte, error) {
	if len(patch) > 0 && patch[0] != FormatVersion {
		return nil, ErrUnsupportedVersion
	}
	return DecodeReaderLimit(dict, bytes.NewReader(patch), capacity)
}
