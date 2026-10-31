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

// cIODriver runs tinydelta_decode_io (c/tinydelta_io.c) over RAM-backed
// callbacks that also police the callback contract:
//
//	driver <page> <dict> <patch> <out> <cap> [failat]
//
// page 0 writes the target straight into a buffer; page > 0 routes it
// through tinydelta_cache (c/tinydelta_cache.c) onto a simulated flash.
// Exit 0: decoded, target written to <out>. 1: decoder rejected the input
// (code printed). 3: a contract violation, e.g. a read past the data, a
// non-sequential or misaligned write, or reading flash not yet written.
// failat N makes the Nth callback call fail, to exercise error paths.
const cIODriver = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "tinydelta_cache.h"
` + cDriverHelpers + `
static uint8_t *dict, *patch, *target, *flash;
static uint32_t dict_len, patch_len, cap, produced, flushed, page;
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
/* Direct target: a plain buffer. */
static int tgt_rd(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	(void)c;
	if (!in(off, n, produced)) violation("target read of bytes not yet produced");
	if (fail_now()) return 1;
	memcpy(b, target + off, n);
	return 0;
}
static int tgt_wr(void *c, uint32_t off, const uint8_t *b, uint32_t n) {
	(void)c;
	if (off != produced) violation("target write not sequential");
	if (!in(off, n, cap)) violation("target write past capacity");
	if (fail_now()) return 1;
	memcpy(target + off, b, n);
	produced += n;
	return 0;
}
/* Simulated flash under the cache: whole pages only, in order. */
static int fl_rd(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	(void)c;
	if (!in(off, n, flushed)) violation("flash read of a page not yet written");
	if (fail_now()) return 1;
	memcpy(b, flash + off, n);
	return 0;
}
static int fl_wr(void *c, uint32_t off, const uint8_t *b, uint32_t n) {
	(void)c;
	if (final_seen) violation("flash write after the final partial page");
	if (off != flushed || off % page) violation("flash write not sequential/page-aligned");
	if (n != page) final_seen = 1;
	if (n == 0 || n > page) violation("flash write of a bad length");
	if (!in(off, n, cap)) violation("flash write past capacity");
	if (fail_now()) return 1;
	memcpy(flash + off, b, n);
	flushed += n;
	return 0;
}


int main(int argc, char **argv) {
	args_from_file(&argc, &argv);
	if (argc != 6 && argc != 7) return 2;
	page = (uint32_t)atol(argv[1]);
	dict = slurp(argv[2], &dict_len);
	patch = slurp(argv[3], &patch_len);
	cap = (uint32_t)atol(argv[5]);
	if (argc == 7) failat = atol(argv[6]);
	target = malloc(cap ? cap : 1);
	flash = malloc(cap ? cap : 1);
	memset(flash, 0xFF, cap);

	tinydelta_io io = {
		.patch_read = patch_rd, .patch_len = patch_len,
		.dict_read = dict_rd, .dict_len = dict_len,
		.target_read = tgt_rd, .target_write = tgt_wr,
		.target_cap = cap,
	};
	uint8_t *pagebuf = malloc(page ? page : 1);
	tinydelta_cache cache;
	if (page) {
		tinydelta_cache_init(&cache, fl_rd, fl_wr, NULL, pagebuf, page);
		tinydelta_cache_attach(&cache, &io);
	}
	uint32_t n = 0;
	int rc = tinydelta_decode_io(&io, &n);
	if (rc != TINYDELTA_OK) {
		printf("%d\n", rc);
		return 1;
	}
	if (page && flushed != n) violation("decode finished with target not fully flushed");
	if (!page && produced != n) violation("target_len disagrees with bytes written");
	FILE *fp = fopen(argv[4], "wb");
	fwrite(page ? flash : target, 1, n, fp);
	fclose(fp);
	return 0;
}
`

type cIOBinary struct {
	scratch int
	path    string
}

func buildCIODrivers(t *testing.T) []cIOBinary {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "driver.c"), []byte(cIODriver), 0o644)
	var bins []cIOBinary
	// Scratch sizes chosen to hit every copy path: 1 (every copy is one
	// byte), 7 (short runs both under and over the scratch size), 64 (the
	// default).
	for _, s := range []int{1, 7, 64} {
		bin := filepath.Join(dir, fmt.Sprintf("decode_io_%d", s))
		cCompile(t, bin, fmt.Sprintf("-DTINYDELTA_SCRATCH_SIZE=%d", s),
			"c/tinydelta_io.c", "c/tinydelta_cache.c", filepath.Join(dir, "driver.c"))
		bins = append(bins, cIOBinary{s, bin})
	}
	return bins
}

// runCIO runs one driver; ok=false means the decoder rejected the input.
func runCIO(t *testing.T, bin string, page int, dict, patch []byte, capacity int, extra ...string) (out []byte, code string, ok bool) {
	t.Helper()
	dir := t.TempDir()
	d, p, o := filepath.Join(dir, "d"), filepath.Join(dir, "p"), filepath.Join(dir, "o")
	os.WriteFile(d, dict, 0o644)
	os.WriteFile(p, patch, 0o644)
	args := append([]string{fmt.Sprint(page), d, p, o, fmt.Sprint(capacity)}, extra...)
	stdout, err := cCommand(bin, args...).Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		out, _ := os.ReadFile(o)
		return out, "", true
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		code := string(bytes.TrimSpace(stdout))
		if code == "" { // not the decoder's result code: show what happened
			code = fmt.Sprintf("none; exit 1, stderr %q", bytes.TrimSpace(exit.Stderr))
		}
		return nil, code, false
	default:
		t.Fatalf("C I/O decoder (page %d): %v: %s", page, err, stdout)
		return nil, "", false
	}
}

// TestCIODecoderMatchesGo checks tinydelta_decode_io, directly and through
// the page cache (page sizes 1, 7 and 256), at three scratch sizes, against
// the Go decoder: valid patches from both encoders must reproduce the
// target exactly, and truncated or corrupted patches, or a too-small
// target capacity, must be accepted or rejected exactly as Go does. The
// driver also fails the test on any callback contract violation, such as
// a target or flash read of bytes not yet written.
func TestCIODecoderMatchesGo(t *testing.T) {
	bins := buildCIODrivers(t)
	pages := []int{0, 1, 7, 256}

	r := rand.New(rand.NewSource(21))
	base := textFile(r, fileSize)
	type pair struct {
		name      string
		dict, tgt []byte
	}
	pairs := []pair{
		{"identical", base, base},
		{"edits", base, splice(splice(base, 900, 7, []byte("EDITED")), 2000, 0, randomFile(r, 30))},
		{"runs", base, append(append(append([]byte{}, base[:500]...), bytes.Repeat([]byte{'z'}, 400)...), bytes.Repeat([]byte("abc"), 50)...)},
		{"unrelated", base, textFile(r, fileSize)},
		{"empty dict", nil, base},
		{"empty target", base, nil},
	}
	for i := 0; i < 12; i++ {
		tgt := append([]byte{}, base...)
		for e := r.Intn(12); e > 0; e-- {
			at := r.Intn(len(tgt) + 1)
			switch r.Intn(3) {
			case 0:
				tgt = splice(tgt, at, 0, randomFile(r, r.Intn(40)))
			case 1:
				tgt = splice(tgt, at, min(r.Intn(60), len(tgt)-at), nil)
			case 2:
				from := r.Intn(len(tgt) + 1)
				n := min(r.Intn(300), len(tgt)-from)
				tgt = splice(tgt, at, 0, append([]byte{}, tgt[from:from+n]...))
			}
		}
		pairs = append(pairs, pair{fmt.Sprintf("mutation %d", i), base, tgt})
	}

	const capacity = cCapacity
	for _, p := range pairs {
		if len(p.tgt) >= capacity {
			t.Fatalf("%s: target does not fit cCapacity", p.name)
		}
		for _, enc := range []struct {
			name string
			fn   func(dict, target []byte, maxLen int) []byte
		}{{"optimal", encode}, {"greedy", encodeGreedy}} {
			patch := withHeader(enc.fn(p.dict, p.tgt, MaxSize))
			for _, b := range bins {
				for _, page := range pages {
					label := fmt.Sprintf("%s/%s scratch=%d page=%d", p.name, enc.name, b.scratch, page)
					got, code, ok := runCIO(t, b.path, page, p.dict, patch, capacity)
					if !ok || !bytes.Equal(got, p.tgt) {
						t.Fatalf("%s: did not reproduce the target (code %s)", label, code)
					}
					if len(p.tgt) > 1 {
						if _, code, ok := runCIO(t, b.path, page, p.dict, patch, len(p.tgt)-1); ok || code != "-2" {
							t.Errorf("%s: capacity one byte short: ok=%v code=%s, want TINYDELTA_ERR_TOO_LARGE", label, ok, code)
						}
					}
				}
			}
		}
	}

	// Corrupted and truncated patches: same accept/reject (and output) as Go.
	for _, p := range pairs[:6] {
		patch := withHeader(encode(p.dict, p.tgt, MaxSize))
		for k := 0; k < 6 && len(patch) > 0; k++ {
			variants := [][]byte{patch[:r.Intn(len(patch))]}
			m := append([]byte{}, patch...)
			m[r.Intn(len(m))] = byte(r.Intn(256))
			variants = append(variants, m)
			for _, v := range variants {
				want, gErr := DecodeReaderLimit(p.dict, bytes.NewReader(v), capacity)
				for _, b := range bins {
					for _, page := range pages {
						got, _, ok := runCIO(t, b.path, page, p.dict, v, capacity)
						if (gErr == nil) != ok || (ok && !bytes.Equal(got, want)) {
							t.Errorf("%s corrupted (scratch=%d page=%d): Go err=%v, C accepted=%v", p.name, b.scratch, page, gErr, ok)
						}
					}
				}
			}
		}
	}
}

// TestCIODecoderCallbackFailures makes each callback call fail in turn
// and checks the decoder reports TINYDELTA_ERR_IO, never succeeding or
// violating the callback contract.
func TestCIODecoderCallbackFailures(t *testing.T) {
	bins := buildCIODrivers(t)
	r := rand.New(rand.NewSource(22))
	dict := textFile(r, 2048)
	tgt := splice(splice(dict, 500, 10, []byte("changed!")), 1500, 0, bytes.Repeat([]byte{'q'}, 100))
	patch := withHeader(encode(dict, tgt, MaxSize))
	for _, b := range bins {
		for _, page := range []int{0, 7} {
			for n := 1; n <= 40; n++ {
				_, code, ok := runCIO(t, b.path, page, dict, patch, cCapacity, fmt.Sprint(n))
				if ok {
					break // fewer than n callback calls: nothing left to fail
				}
				if code != "-3" {
					t.Errorf("scratch=%d page=%d failing call %d: code %s, want TINYDELTA_ERR_IO (-3)", b.scratch, page, n, code)
				}
			}
		}
	}
}
