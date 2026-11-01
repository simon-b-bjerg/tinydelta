// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// cIODriver runs tinydelta_decode_io (c/tinydelta_io.c) over
// RAM-backed callbacks that also police the callback contract:
//
//	driver <buffer>:<page>:<read>:<tables> <dict> <patch> <out> <cap> [failat]
//
// buffer and page are the buffer and page sizes; read 0 leaves
// target_read NULL; tables is the size of the coder's tables buffer (0:
// p_tables NULL). Exit 0: decoded, target written to <out>. 1: the
// decoder rejected the input (code printed). 3: a contract violation: a
// read past the data, a target write that is not the next whole page (or
// the final partial one), or a target read of bytes not yet written.
// failat N makes the Nth callback call fail, to exercise error paths.
const cIODriver = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "tinydelta_io.h"
` + cDriverHelpers + `
static uint8_t *dict, *patch, *flash;
static uint32_t dict_len, patch_len, cap, written, page;
static int final_seen;
static long calls, failat = -1;

static void violation(const char *what) { printf("violation: %s\n", what); exit(3); }
static int fail_now(void) { return ++calls == failat; }
static int in(uint32_t off, uint32_t len, uint32_t size) { return off <= size && len <= size - off; }

static int patch_rd(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	(void)c;
	if (!in(off, n, patch_len)) violation("patch read out of range");
	if (fail_now()) return 1;
	memcpy(b, patch + off, n);
	return 0;
}
static int dict_rd(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	(void)c;
	if (!in(off, n, dict_len)) violation("dict read out of range");
	if (fail_now()) return 1;
	memcpy(b, dict + off, n);
	return 0;
}
static int fl_rd(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	(void)c;
	if (!in(off, n, written)) violation("target read of bytes not yet written");
	if (fail_now()) return 1;
	memcpy(b, flash + off, n);
	return 0;
}
static int fl_wr(void *c, uint32_t off, const uint8_t *b, uint32_t n) {
	(void)c;
	if (final_seen) violation("target write after the final partial page");
	if (off != written || off % page) violation("target write not sequential/page-aligned");
	if (n != page) final_seen = 1;
	if (n == 0 || n > page) violation("target write of a bad length");
	if (!in(off, n, cap)) violation("target write past capacity");
	if (fail_now()) return 1;
	memcpy(flash + off, b, n);
	written += n;
	return 0;
}

int main(int argc, char **argv) {
	unsigned long buffer_size, page_size, tables_size;
	int use_read;
	char *rest;
	args_from_file(&argc, &argv);
	if (argc != 6 && argc != 7) return 2;
	/* buffer:page:read, parsed without sscanf (too large for MSP430). */
	buffer_size = strtoul(argv[1], &rest, 10);
	if (*rest++ != ':') return 2;
	page_size = strtoul(rest, &rest, 10);
	if (*rest++ != ':') return 2;
	use_read = (int)strtoul(rest, &rest, 10);
	if (*rest++ != ':') return 2;
	tables_size = strtoul(rest, &rest, 10);
	if (*rest != 0) return 2;
	page = (uint32_t)page_size;
	dict = slurp(argv[2], &dict_len);
	patch = slurp(argv[3], &patch_len);
	cap = (uint32_t)atol(argv[5]);
	if (argc == 7) failat = atol(argv[6]);
	flash = malloc(cap ? cap : 1);

	tinydelta_io_t io = {
		.patch_read = patch_rd, .patch_len = patch_len,
		.dict_read = dict_rd, .dict_len = dict_len,
		.target_write = fl_wr, .target_read = use_read ? fl_rd : NULL,
		.target_cap = cap,
		.p_buffer = malloc(buffer_size ? buffer_size : 1),
		.buffer_size = (uint32_t)buffer_size, .page_size = page,
		.p_tables = tables_size ? malloc(tables_size) : NULL, .tables_size = (uint32_t)tables_size,
	};
	uint32_t n = 0;
	int rc = tinydelta_decode_io(&io, &n);
	if (rc != TINYDELTA_OK) {
		printf("%d\n", rc);
		return 1;
	}
	if (written != n) violation("target_len disagrees with bytes written");
	FILE *fp = fopen(argv[4], "wb");
	fwrite(flash, 1, n, fp);
	fclose(fp);
	return 0;
}
`

// ioConfig is one buffer setup for the callback decoder driver: buffer
// and page size, and the coder's tables buffer (0: none).
type ioConfig struct{ buffer, page, tables int }

// Buffer setups that hit every path: a 1-byte buffer (every copy one
// byte, every far copy through target_read), a buffer of one page, a
// buffer of several pages, and pages larger than a whole patch.
var ioConfigs = []ioConfig{{1, 1, 0}, {8, 8, 0}, {64, 16, 0}, {256, 64, 0}, {1024, 1024, 0}}

func buildCIODriver(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "driver.c"), []byte(cIODriver), 0o644)
	bin := filepath.Join(dir, "decode_io")
	cCompile(t, bin, append(append([]string{}, cCoderSources...), "c/tinydelta_io.c", filepath.Join(dir, "driver.c"))...)
	return bin
}

// runCIO runs the callback decoder driver; ok=false means the decoder rejected the
// input.
func runCIO(t *testing.T, bin string, c ioConfig, read bool, dict, patch []byte, capacity int, extra ...string) (out []byte, code string, ok bool) {
	t.Helper()
	dir := t.TempDir()
	d, p, o := filepath.Join(dir, "d"), filepath.Join(dir, "p"), filepath.Join(dir, "o")
	os.WriteFile(d, dict, 0o644)
	os.WriteFile(p, patch, 0o644)
	readFlag := 0
	if read {
		readFlag = 1
	}
	args := append([]string{fmt.Sprintf("%d:%d:%d:%d", c.buffer, c.page, readFlag, c.tables), d, p, o, fmt.Sprint(capacity)}, extra...)
	stdout, err := cCommand(bin, args...).Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		out, _ := os.ReadFile(o)
		return out, "", true
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		code := string(bytes.TrimSpace(stdout))
		if code == "" {
			code = fmt.Sprintf("none; exit 1, stderr %q", bytes.TrimSpace(exit.Stderr))
		}
		return nil, code, false
	default:
		t.Fatalf("C callback decoder (%+v): %v: %s", c, err, stdout)
		return nil, "", false
	}
}

// ioTestPairs are the dictionary/target pairs the callback decoder tests decode.
func ioTestPairs(r *rand.Rand) []struct {
	name      string
	dict, tgt []byte
} {
	base := textFile(r, fileSize)
	block := randomFile(r, 300)
	return []struct {
		name      string
		dict, tgt []byte
	}{
		{"identical", base, base},
		{"edits", base, splice(splice(base, 900, 7, []byte("EDITED")), 2000, 0, randomFile(r, 30))},
		{"runs", base, append(append(append([]byte{}, base[:500]...), bytes.Repeat([]byte{'z'}, 400)...), bytes.Repeat([]byte("abc"), 50)...)},
		// A block repeated 1,000 bytes later: a far COPY_TARGET.
		{"far repeat", nil, append(append(append([]byte{}, block...), textFile(r, 700)...), block...)},
		{"unrelated", base, textFile(r, fileSize)},
		{"empty dict", nil, base},
		{"empty target", base, nil},
	}
}

// TestCIODecoderMatchesGo checks tinydelta_decode_io at several
// buffer and page sizes against the Go decoder: valid patches from both
// version 1 encoders and the version 2 encoder must reproduce the target,
// and truncated or corrupted patches, or a too-small capacity, must be
// accepted or rejected exactly as Go does. target_read is available, so
// far copies work at every buffer size.
func TestCIODecoderMatchesGo(t *testing.T) {
	bin := buildCIODriver(t)
	r := rand.New(rand.NewSource(31))
	const capacity = cCapacity
	for _, p := range ioTestPairs(r) {
		if len(p.tgt) >= capacity {
			t.Fatalf("%s: target does not fit cCapacity", p.name)
		}
		patches := map[string][]byte{
			"optimal":            withHeader(encode(p.dict, p.tgt, MaxSize)),
			"greedy":             withHeader(encodeGreedy(p.dict, p.tgt, MaxSize)),
			"version 2":          withVersion(FormatVersion2, encodeCovers(p.dict, p.tgt, MaxSize)),
			"coded A":            encodeCoded(withHeader(encode(p.dict, p.tgt, MaxSize)), codedLayoutA),
			"coded B, version 2": encodeCoded(withVersion(FormatVersion2, encodeCovers(p.dict, p.tgt, MaxSize)), codedLayoutB),
			"coded C":            encodeCoded(withHeader(encodeGreedy(p.dict, p.tgt, MaxSize)), codedLayoutC),
		}
		for name, patch := range patches {
			for _, c := range ioConfigs {
				c := withTables(c, patch)
				got, code, ok := runCIO(t, bin, c, true, p.dict, patch, capacity)
				if !ok || !bytes.Equal(got, p.tgt) {
					t.Fatalf("%s/%s %+v: did not reproduce the target (code %s)", p.name, name, c, code)
				}
				if len(p.tgt) > 1 {
					if _, code, ok := runCIO(t, bin, c, true, p.dict, patch, len(p.tgt)-1); ok || code != "-2" {
						t.Errorf("%s/%s %+v: capacity one byte short: ok=%v code %s, want TINYDELTA_ERR_TOO_LARGE", p.name, name, c, ok, code)
					}
				}
			}
			for k := 0; k < 4 && len(patch) > 0; k++ {
				variants := [][]byte{patch[:r.Intn(len(patch))]}
				m := append([]byte{}, patch...)
				m[r.Intn(len(m))] = byte(r.Intn(256))
				variants = append(variants, m)
				for _, v := range variants {
					want, gErr := DecodeReaderLimit(p.dict, bytes.NewReader(v), capacity)
					for _, c := range ioConfigs {
						c := withTables(c, patch)
						got, _, ok := runCIO(t, bin, c, true, p.dict, v, capacity)
						if (gErr == nil) != ok || (ok && !bytes.Equal(got, want)) {
							t.Errorf("%s/%s corrupted %+v: Go err=%v, C accepted=%v", p.name, name, c, gErr, ok)
						}
					}
				}
			}
		}
	}
}

// withTables gives a buffer setup the coder's tables a coded patch needs,
// exactly that size.
func withTables(c ioConfig, patch []byte) ioConfig {
	if len(patch) > 1 && (patch[0] == codedFlag|FormatVersion || patch[0] == codedFlag|FormatVersion2) {
		c.tables = CodedTablesSize(int(patch[1] & 3))
	}
	return c
}

// TestCIODecoderCodedTables checks the coder's tables buffer: one buffer
// of a given size decodes every layout that fits it, whatever the ring;
// a layout that doesn't fit, or no tables at all, is
// TINYDELTA_ERR_VERSION before anything is written.
func TestCIODecoderCodedTables(t *testing.T) {
	bin := buildCIODriver(t)
	r := rand.New(rand.NewSource(34))
	dict := textFile(r, 2000)
	tgt := splice(dict, 700, 3, []byte("changed"))
	plain := withHeader(encode(dict, tgt, MaxSize))
	for _, tables := range []int{0, 521, 522, 777, 1287, 2000} {
		for _, layout := range []int{codedLayoutC, codedLayoutB, codedLayoutA} {
			patch := encodeCoded(plain, layout)
			for _, c := range []ioConfig{{1, 1, tables}, {256, 64, tables}} {
				got, code, ok := runCIO(t, bin, c, true, dict, patch, cCapacity)
				if fits := tables >= CodedTablesSize(layout); fits && (!ok || !bytes.Equal(got, tgt)) {
					t.Errorf("layout %d, %+v: did not decode (code %s)", layout, c, code)
				} else if !fits && (ok || code != "-4") {
					t.Errorf("layout %d, %+v: ok=%v code %s, want TINYDELTA_ERR_VERSION", layout, c, ok, code)
				}
			}
		}
	}
}

// TestCIODecoderWithoutTargetRead checks the point of the buffer: a
// patch made with Window no larger than the buffer decodes
// without target_read, and one reaching further back is rejected as
// malformed instead.
func TestCIODecoderWithoutTargetRead(t *testing.T) {
	bin := buildCIODriver(t)
	r := rand.New(rand.NewSource(32))
	for _, p := range ioTestPairs(r) {
		for _, c := range ioConfigs {
			limited := mustEncode(t, p.dict, p.tgt, EncodeOptions{Window: c.buffer})
			if got, code, ok := runCIO(t, bin, c, false, p.dict, limited, cCapacity); !ok || !bytes.Equal(got, p.tgt) {
				t.Fatalf("%s %+v, limited to the buffer: did not reproduce the target (code %s)", p.name, c, code)
			}
		}
	}
	block := randomFile(r, 300)
	tgt := append(append(append([]byte{}, block...), textFile(r, 700)...), block...)
	far := mustEncode(t, nil, tgt, EncodeOptions{})
	if _, code, ok := runCIO(t, bin, ioConfig{64, 16, 0}, false, nil, far, cCapacity); ok || code != "-1" {
		t.Errorf("far copy without target_read: ok=%v code %s, want TINYDELTA_ERR_MALFORMED", ok, code)
	}
}

// TestCIODecoderCallbackFailures makes each callback call fail in turn
// and checks the decoder reports TINYDELTA_ERR_IO.
func TestCIODecoderCallbackFailures(t *testing.T) {
	bin := buildCIODriver(t)
	r := rand.New(rand.NewSource(33))
	dict := textFile(r, 2048)
	tgt := splice(splice(dict, 500, 10, []byte("changed!")), 1500, 0, bytes.Repeat([]byte{'q'}, 100))
	tgt = append(tgt, tgt[:200]...) // a far copy, through target_read
	plain := withHeader(encode(dict, tgt, MaxSize))
	for _, patch := range [][]byte{plain, withVersion(FormatVersion2, encodeCovers(dict, tgt, MaxSize)), encodeCoded(plain, codedLayoutC)} {
		for _, c := range []ioConfig{{1, 1, 0}, {64, 16, 0}} {
			c := withTables(c, patch)
			for n := 1; n <= 400; n++ {
				_, code, ok := runCIO(t, bin, c, true, dict, patch, cCapacity, fmt.Sprint(n))
				if ok {
					break // fewer than n callback calls: nothing left to fail
				}
				if code != "-3" {
					t.Errorf("%+v failing call %d: code %s, want TINYDELTA_ERR_IO (-3)", c, n, code)
				}
			}
		}
	}
}

// TestCIODecoderBufferChecks checks that a buffer or page size that is
// not a power of two, or a page larger than the buffer, is rejected.
func TestCIODecoderBufferChecks(t *testing.T) {
	bin := buildCIODriver(t)
	patch := withHeader(append(appendVarint(nil, 3<<2|opAdd), "abc"...))
	for _, c := range []ioConfig{{0, 0, 0}, {3, 1, 0}, {8, 3, 0}, {8, 16, 0}, {8, 0, 0}} {
		if _, code, ok := runCIO(t, bin, c, true, nil, patch, cCapacity); ok || code != "-1" {
			t.Errorf("%+v: ok=%v code %s, want TINYDELTA_ERR_MALFORMED", c, ok, code)
		}
	}
}
