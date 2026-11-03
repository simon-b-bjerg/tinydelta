// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// cLargeDriver runs the callback decoder with its data in files, as a
// device has it on external flash, so it handles more than the program's
// own memory holds: on 16-bit MSP430, offsets and lengths past 64 KB.
// Arguments: buffer:page:read:tables dictionary patch output capacity.
// Exit 0: decoded. 1: rejected (code printed). 3: bad callback use.
const cLargeDriver = `#include <stdio.h>
#include <stdlib.h>
#include "tinydelta_io.h"
` + cDriverHelpers + `
typedef struct {
	FILE *fp;
	uint32_t pos, size; /* the file's position; its size, for reads */
} region_t;

static int place(region_t *r, uint32_t off) {
	if (off != r->pos && fseek(r->fp, (long)off, SEEK_SET) != 0) return 0;
	r->pos = off;
	return 1;
}
static int region_rd(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	region_t *r = c;
	if (off > r->size || n > r->size - off) { printf("read out of range\n"); exit(3); }
	if (!place(r, off) || fread(b, 1, n, r->fp) != n) return 1;
	r->pos += n;
	return 0;
}
static int region_wr(void *c, uint32_t off, const uint8_t *b, uint32_t n) {
	region_t *r = c;
	if (off != r->size) { printf("write not sequential\n"); exit(3); }
	if (!place(r, off) || fwrite(b, 1, n, r->fp) != n) return 1;
	r->pos += n;
	r->size += n;
	return 0;
}
static uint32_t file_size(FILE *fp) {
	long size;
	fseek(fp, 0, SEEK_END);
	size = ftell(fp);
	fseek(fp, 0, SEEK_SET);
	return (uint32_t)size;
}

int main(int argc, char **argv) {
	unsigned long buffer_size, page_size, tables_size;
	region_t dict = {0}, patch = {0}, target = {0};
	uint32_t n = 0;
	int use_read, rc;
	char *rest;
	(void)slurp; /* the shared helpers' other half: not needed here */
	args_from_file(&argc, &argv);
	if (argc != 6) return 2;
	buffer_size = strtoul(argv[1], &rest, 10);
	if (*rest++ != ':') return 2;
	page_size = strtoul(rest, &rest, 10);
	if (*rest++ != ':') return 2;
	use_read = (int)strtoul(rest, &rest, 10);
	if (*rest++ != ':') return 2;
	tables_size = strtoul(rest, &rest, 10);
	dict.fp = fopen(argv[2], "rb");
	patch.fp = fopen(argv[3], "rb");
	target.fp = fopen(argv[4], "w+b");
	if (!dict.fp || !patch.fp || !target.fp) return 2;
	dict.size = file_size(dict.fp);
	patch.size = file_size(patch.fp);
	tinydelta_io_t io = {
		.patch_read = region_rd, .p_patch_ctx = &patch, .patch_len = patch.size,
		.dict_read = region_rd, .p_dict_ctx = &dict, .dict_len = dict.size,
		.target_write = region_wr, .target_read = use_read ? region_rd : NULL,
		.p_target_ctx = &target, .target_cap = (uint32_t)strtoul(argv[5], NULL, 10),
		.p_buffer = malloc(buffer_size), .buffer_size = (uint32_t)buffer_size, .page_size = (uint32_t)page_size,
		.p_tables = tables_size ? malloc(tables_size) : NULL, .tables_size = (uint32_t)tables_size,
	};
	rc = tinydelta_decode_io(&io, &n);
	if (rc != TINYDELTA_OK) {
		printf("%d\n", rc);
		return 1;
	}
	if (n != target.size) { printf("length disagrees with bytes written\n"); return 3; }
	fclose(target.fp);
	return 0;
}
`

// TestCLargeTargets decodes past what the other C tests reach, with the
// callback decoder on data in files: dictionary, patch and target over
// 64 KB, in each of the forms a device gets. It matters most on 16-bit
// targets, where every offset past 64 KB needs the 32-bit arithmetic.
func TestCLargeTargets(t *testing.T) {
	const size = 100_000
	dict, target := firmwareLike(80, size)
	if len(dict) <= 1<<16 || len(target) <= 1<<16 {
		t.Fatalf("dictionary %d B, target %d B: not past 64 KB", len(dict), len(target))
	}
	inner := withVersion(FormatVersion2, encodeCovers(dict, target, MaxSize))
	outer := withHeader(encodeGreedyLimited(nil, inner, MaxSize, 256))
	plain := withHeader(encodeGreedy(dict, target, MaxSize))
	limited := withHeader(encodeGreedyLimited(dict, target, MaxSize, 256))
	tables := CodedTablesSize(codedLayoutB)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "driver.c"), []byte(cLargeDriver), 0o644)
	bin := filepath.Join(dir, "decode_large")
	cCompile(t, bin, append(append([]string{}, cCoderSources...), "c/tinydelta_io.c", filepath.Join(dir, "driver.c"))...)

	decode := func(t *testing.T, cfg ioConfig, read int, dict, patch []byte, capacity int) []byte {
		t.Helper()
		d, p, o := filepath.Join(dir, "d"), filepath.Join(dir, "p"), filepath.Join(dir, "o")
		os.WriteFile(d, dict, 0o644)
		os.WriteFile(p, patch, 0o644)
		spec := fmt.Sprintf("%d:%d:%d:%d", cfg.buffer, cfg.page, read, cfg.tables)
		if out, err := cCommand(bin, spec, d, p, o, fmt.Sprint(capacity)).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, bytes.TrimSpace(out))
		}
		got, err := os.ReadFile(o)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	for _, c := range []struct {
		name  string
		patch []byte
		cfg   ioConfig
		read  int
	}{
		{"plain, with target_read", plain, ioConfig{256, 256, 0}, 1},
		{"limited to the buffer, no target_read", limited, ioConfig{256, 256, 0}, 0},
		{"coded version 1", encodeCoded(plain, codedLayoutB), ioConfig{256, 256, tables}, 1},
		{"coded version 2", encodeCoded(inner, codedLayoutB), ioConfig{256, 256, tables}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := decode(t, c.cfg, c.read, dict, c.patch, len(target)); !bytes.Equal(got, target) {
				t.Error("wrong target")
			}
		})
	}

	// Two passes with a coded outer patch: the first pass needs the
	// tables too.
	t.Run("two passes, coded outer patch", func(t *testing.T) {
		cfg := ioConfig{256, 256, tables}
		got := decode(t, cfg, 0, nil, encodeCoded(outer, codedLayoutB), len(inner))
		if !bytes.Equal(got, inner) {
			t.Fatal("wrong intermediate patch")
		}
		if got = decode(t, cfg, 0, dict, inner, len(target)); !bytes.Equal(got, target) {
			t.Error("wrong target")
		}
	})
}
