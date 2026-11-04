// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

//go:build cgo

package cdiff

/*
#cgo CFLAGS: -std=c99 -I${SRCDIR}/../../c -DTINYDELTA_CODER=1 -DTINYDELTA_RELOC=2 -DTINYDELTA_RELOC_SHIPPED=1
#include <stdlib.h>
#include <string.h>
#include "tinydelta.h"
#include "tinydelta_io.h"

// The callback decoder's side: the patch and dictionary in memory, the
// target as flash written in whole pages, in order. A broken promise of
// the decoder's (a read out of range, a write out of order) is recorded
// in violation and fails the call.
typedef struct {
	const uint8_t *patch, *dict;
	uint32_t patch_len, dict_len;
	uint8_t *flash;
	uint32_t cap, written, page;
	int final_seen;
	long calls, fail_at;
	const char *violation;
} cdiff_io;

static int cdiff_in(uint32_t off, uint32_t len, uint32_t size) { return off <= size && len <= size - off; }
static int cdiff_fail(cdiff_io *s) { return ++s->calls == s->fail_at; }

static int cdiff_patch_read(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	cdiff_io *s = c;
	if (!cdiff_in(off, n, s->patch_len)) { s->violation = "patch read out of range"; return -1; }
	if (cdiff_fail(s)) return -1;
	memcpy(b, s->patch + off, n);
	return 0;
}
static int cdiff_dict_read(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	cdiff_io *s = c;
	if (!cdiff_in(off, n, s->dict_len)) { s->violation = "dictionary read out of range"; return -1; }
	if (cdiff_fail(s)) return -1;
	memcpy(b, s->dict + off, n);
	return 0;
}
static int cdiff_target_read(void *c, uint32_t off, uint8_t *b, uint32_t n) {
	cdiff_io *s = c;
	if (!cdiff_in(off, n, s->written)) { s->violation = "target read of bytes not yet written"; return -1; }
	if (cdiff_fail(s)) return -1;
	memcpy(b, s->flash + off, n);
	return 0;
}
static int cdiff_target_write(void *c, uint32_t off, const uint8_t *b, uint32_t n) {
	cdiff_io *s = c;
	if (s->final_seen) { s->violation = "target write after the final partial page"; return -1; }
	if (off != s->written || (off % s->page) != 0) { s->violation = "target write not in order or not page-aligned"; return -1; }
	if (n == 0 || n > s->page) { s->violation = "target write of a bad length"; return -1; }
	if (!cdiff_in(off, n, s->cap)) { s->violation = "target write past the capacity"; return -1; }
	if (n != s->page) s->final_seen = 1;
	if (cdiff_fail(s)) return -1;
	memcpy(s->flash + off, b, n);
	s->written += n;
	return 0;
}

static int cdiff_decode_io(cdiff_io *s, uint8_t *buffer, uint32_t buffer_size, uint8_t *tables, uint32_t tables_size, uint32_t *out_len) {
	tinydelta_io_t io;
	memset(&io, 0, sizeof io);
	io.patch_read = cdiff_patch_read; io.p_patch_ctx = s; io.patch_len = s->patch_len;
	io.dict_read = cdiff_dict_read; io.p_dict_ctx = s; io.dict_len = s->dict_len;
	io.target_write = cdiff_target_write; io.target_read = cdiff_target_read; io.p_target_ctx = s;
	io.target_cap = s->cap;
	io.p_buffer = buffer; io.buffer_size = buffer_size; io.page_size = s->page;
	io.p_tables = tables; io.tables_size = tables_size;
	return (int)tinydelta_decode_io(&io, out_len);
}
*/
import "C"

import "unsafe"

// Available reports whether the C decoders are linked in (they need cgo).
const Available = true

// cbytes is b in C memory (at least 1 byte, so never NULL); free it with C.free.
func cbytes(b []byte, size int) *C.uint8_t {
	p := (*C.uint8_t)(C.calloc(C.size_t(max(size, 1)), 1))
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), max(size, 1)), b)
	return p
}

// Decode runs tinydelta_decode_coded: the target (capacity bytes at most)
// and the result code, OK on success. The tables buffer is tablesSize bytes.
func Decode(dict, patch []byte, capacity, tablesSize int) ([]byte, int) {
	d, p := cbytes(dict, len(dict)), cbytes(patch, len(patch))
	out, tables := cbytes(nil, capacity), cbytes(nil, tablesSize)
	defer C.free(unsafe.Pointer(d))
	defer C.free(unsafe.Pointer(p))
	defer C.free(unsafe.Pointer(out))
	defer C.free(unsafe.Pointer(tables))
	var n C.uint32_t
	rc := int(C.tinydelta_decode_coded(d, C.uint32_t(len(dict)), p, C.uint32_t(len(patch)), out, C.uint32_t(capacity), tables, C.uint32_t(tablesSize), &n))
	if rc != OK {
		return nil, rc
	}
	return C.GoBytes(unsafe.Pointer(out), C.int(n)), rc
}

// DecodeIO runs tinydelta_decode_io with cfg: the target, the result code,
// and the first broken promise of the decoder's towards its callbacks
// ("" if none).
func DecodeIO(dict, patch []byte, capacity, tablesSize int, cfg IOConfig) ([]byte, int, string) {
	s := (*C.cdiff_io)(C.calloc(1, C.size_t(unsafe.Sizeof(C.cdiff_io{}))))
	defer C.free(unsafe.Pointer(s))
	s.dict, s.patch = cbytes(dict, len(dict)), cbytes(patch, len(patch))
	s.dict_len, s.patch_len = C.uint32_t(len(dict)), C.uint32_t(len(patch))
	s.flash, s.cap, s.page, s.fail_at = cbytes(nil, capacity), C.uint32_t(capacity), C.uint32_t(cfg.PageSize), C.long(cfg.FailAt)
	buffer, tables := cbytes(nil, cfg.BufferSize), cbytes(nil, tablesSize)
	defer C.free(unsafe.Pointer(s.dict))
	defer C.free(unsafe.Pointer(s.patch))
	defer C.free(unsafe.Pointer(s.flash))
	defer C.free(unsafe.Pointer(buffer))
	defer C.free(unsafe.Pointer(tables))
	var n C.uint32_t
	rc := int(C.cdiff_decode_io(s, buffer, C.uint32_t(cfg.BufferSize), tables, C.uint32_t(tablesSize), &n))
	violation := ""
	if s.violation != nil {
		violation = C.GoString(s.violation)
	}
	if rc != OK {
		return nil, rc, violation
	}
	return C.GoBytes(unsafe.Pointer(s.flash), C.int(n)), rc, violation
}
