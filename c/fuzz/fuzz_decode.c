/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* libFuzzer harness for the C decoders. Run it with scripts/c-fuzz.sh.
 *
 * Every input is decoded by:
 *   - a reference decoder in this file, written from the README's wire
 *     format with 64-bit arithmetic and no shortcuts, coded patches
 *     included (its own range decoder);
 *   - tinydelta_decode (memory decoder);
 *   - tinydelta_decode_io (callback decoder) on a simulated flash, with a
 *     page of 1-32 bytes and a buffer of 1-4 pages (from byte 3), with
 *     target_read and, a second time, without.
 *
 * All of them must agree: the same result (OK, MALFORMED, TOO_LARGE or VERSION) and,
 * on success, the same target bytes. Without target_read, the callback
 * decoder may instead reject a copy reaching back past its buffer as
 * MALFORMED. The callbacks also police the documented contract: no reads
 * outside the patch or dictionary, no reads of target bytes not yet
 * written, whole page-aligned flash programs in order. Any violation
 * aborts. The input can also make one callback call fail, which the
 * callback decoder must then report as TINYDELTA_ERR_IO.
 * Build with -fsanitize=fuzzer,address,undefined, so out-of-bounds access
 * and undefined behaviour abort too.
 *
 * Input layout:
 *   byte 0-1  output capacity, little-endian, taken modulo FUZZ_MAX_TARGET + 1
 *   byte 2    dictionary length (clipped to the bytes that follow)
 *   byte 3    for the callback decoder, page 1 << (byte % 6) and buffer
 *             page << ((byte >> 3) % 3)
 *   byte 4    callback call to fail, counting from 1 across all callbacks
 *             of one decode; 0 for none
 *   then      the dictionary, then the patch
 */
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "tinydelta.h"
#include "tinydelta_io.h"

#define FUZZ_HEADER_SIZE 5u
#define FUZZ_MAX_TARGET  4096u

#define FAIL(...)                                                                                                                                              \
    do {                                                                                                                                                       \
        fprintf(stderr, "fuzz_decode: " __VA_ARGS__);                                                                                                          \
        fputc('\n', stderr);                                                                                                                                   \
        abort();                                                                                                                                               \
    } while (0)

/* ---- Reference decoder --------------------------------------------------- */

/* ---- Reference byte source: plain, or a coded patch's range decoder ---- */

typedef struct {
    const uint8_t *patch;
    uint64_t patch_len, pos;
    int coded;
    uint64_t stream_left; /* coded: instruction-stream bytes still to come */
    int layout, prev_op;
    uint64_t range, code; /* kept below 2^32 */
    uint8_t states[1287];
} ref_source;

static const uint16_t ref_lps[63]     = {2048, 1944, 1845, 1752, 1663, 1578, 1498, 1422, 1350, 1281, 1216, 1154, 1096, 1040, 987, 937, 890, 844, 802, 761, 722,
                                         685,  651,  618,  586,  557,  528,  501,  476,  452,  429,  407,  386,  367,  348,  330, 314, 298, 283, 268, 255, 242,
                                         229,  218,  207,  196,  186,  177,  168,  159,  151,  144,  136,  129,  123,  117,  111, 105, 100, 95,  90,  85,  81};
static const uint8_t ref_next_lps[63] = {0,  0,  1,  2,  3,  4,  4,  5,  6,  7,  8,  9,  10, 10, 11, 12, 13, 14, 14, 15, 16,
                                         17, 17, 18, 19, 20, 20, 21, 22, 22, 23, 24, 24, 25, 26, 26, 27, 27, 28, 29, 29, 30,
                                         30, 31, 31, 32, 32, 33, 33, 33, 34, 34, 35, 35, 35, 36, 36, 36, 37, 37, 37, 38, 38};

static int ref_raw(ref_source *s, uint8_t *byte) {
    if (s->pos >= s->patch_len) {
        return -1;
    }
    *byte = s->patch[s->pos++];
    return 0;
}

/* One bit at states[index]: LZMA's range decoder, 12-bit probabilities
 * from 1-byte states (state * 2 + more likely bit). */
static int ref_bit(ref_source *s, int index, int *bit) {
    int state = s->states[index] >> 1, mps = s->states[index] & 1;
    uint64_t p0    = mps == 0 ? 4096 - ref_lps[state] : ref_lps[state];
    uint64_t bound = (s->range >> 12) * p0;
    if (s->code < bound) {
        s->range = bound;
        *bit     = 0;
    } else {
        s->code -= bound;
        s->range -= bound;
        *bit = 1;
    }
    if (*bit == mps) {
        state = state < 62 ? state + 1 : 62;
    } else {
        if (state == 0) {
            mps ^= 1;
        }
        state = ref_next_lps[state];
    }
    s->states[index] = (uint8_t)(state * 2 + mps);
    while (s->range < (1u << 24)) {
        uint8_t byte;
        if (ref_raw(s, &byte)) {
            return -1;
        }
        s->range = (s->range << 8) & 0xFFFFFFFFu;
        s->code  = ((s->code << 8) | byte) & 0xFFFFFFFFu;
    }
    return 0;
}

/* Bits hi..lo of a byte through a bit tree at states[base]. */
static int ref_tree(ref_source *s, int base, int hi, int lo, int *value) {
    int node = 1;
    for (int b = hi; b >= lo; b--) {
        int bit;
        if (ref_bit(s, base + node - 1, &bit)) {
            return -1;
        }
        *value |= bit << b;
        node = node * 2 + bit;
    }
    return 0;
}

enum {
    F_LITERAL,
    F_DIFF,
    F_DELTA,
    F_CONTINUED,
    F_INSTRUCTION
};

/* The next instruction-stream byte, a field of kind field. */
static int ref_byte(ref_source *s, int field, uint8_t *byte) {
    if (!s->coded) {
        return ref_raw(s, byte);
    }
    if (s->stream_left == 0) {
        return -1;
    }
    s->stream_left--;
    int value = 0;
    if (field == F_INSTRUCTION) {
        if (ref_tree(s, s->prev_op * 3, 1, 0, &value) || ref_tree(s, 15 + (value & 3) * 63, 7, 2, &value)) {
            return -1;
        }
        s->prev_op = value & 3;
    } else {
        int table = s->layout == 3 ? field : (s->layout == 2 && field == F_DIFF) ? 1 : 0;
        if (ref_tree(s, 15 + 252 + table * 255, 7, 0, &value)) {
            return -1;
        }
    }
    *byte = (uint8_t)value;
    return 0;
}

static int ref_varint(ref_source *src, int first, uint64_t *value) {
    uint64_t result = 0;
    for (int i = 0; i < 5; i++) {
        uint8_t byte;
        if (ref_byte(src, i == 0 ? first : F_CONTINUED, &byte)) {
            return -1;
        }
        if (i == 4 && byte > 0x0F) {
            return -1;
        }
        result |= (uint64_t)(byte & 0x7F) << (7 * i);
        if (!(byte & 0x80)) {
            *value = result;
            return 0;
        }
    }
    return -1;
}

static int64_t ref_zigzag(uint64_t z) {
    return (int64_t)(z >> 1) ^ -(int64_t)(z & 1);
}

/* The layout of a coded patch's tables (0: not coded), for sizing the
 * callback decoder's buffer. */
static int ref_layout(const uint8_t *patch, uint64_t patch_len) {
    if (patch_len >= 2 && (patch[0] == 0x81 || patch[0] == 0x82) && patch[1] >= 1 && patch[1] <= 3) {
        return patch[1];
    }
    return 0;
}

static tinydelta_result_t
ref_decode(const uint8_t *dict, uint64_t dict_len, const uint8_t *patch, uint64_t patch_len, uint8_t *out, uint64_t out_cap, uint64_t *out_len) {
    static ref_source src;
    uint64_t out_pos = 0;
    int64_t cursor   = 0;
    uint64_t instruction, z;

    memset(&src, 0, sizeof src);
    src.patch     = patch;
    src.patch_len = patch_len;
    src.pos       = 1; /* past the version byte */
    if (patch_len < 1) {
        return TINYDELTA_ERR_MALFORMED;
    }
    int version = patch[0];
    if (version == 0x81 || version == 0x82) { /* coded: features, stream length, coder */
        uint8_t features;
        if (ref_raw(&src, &features)) {
            return TINYDELTA_ERR_MALFORMED;
        }
        if (features < 1 || features > 3) {
            return TINYDELTA_ERR_VERSION;
        }
        if (ref_varint(&src, F_CONTINUED, &src.stream_left)) {
            return TINYDELTA_ERR_MALFORMED;
        }
        src.coded   = 1;
        src.layout  = features;
        src.prev_op = 4;
        src.range   = 0xFFFFFFFFu;
        for (int i = 0; i < 4; i++) {
            uint8_t byte;
            if (ref_raw(&src, &byte)) {
                return TINYDELTA_ERR_MALFORMED;
            }
            src.code = (src.code << 8) | byte;
        }
        version &= 0x7F;
    }
    if (version != 1 && version != 2) {
        return TINYDELTA_ERR_VERSION;
    }
    const int version2 = version == 2; /* opcode 3 is ADD_DIFF */
    while (src.coded ? src.stream_left > 0 : src.pos < patch_len) {
        if (ref_varint(&src, F_INSTRUCTION, &instruction)) {
            return TINYDELTA_ERR_MALFORMED;
        }
        uint64_t length = instruction >> 2;
        if (out_pos + length > out_cap) {
            return TINYDELTA_ERR_TOO_LARGE;
        }

        switch (instruction & 3) {
        case 0: { /* COPY */
            if (ref_varint(&src, F_DELTA, &z)) {
                return TINYDELTA_ERR_MALFORMED;
            }
            int64_t address = cursor + ref_zigzag(z);
            if (address < 0 || (uint64_t)address + length > dict_len) {
                return TINYDELTA_ERR_MALFORMED;
            }
            memcpy(out + out_pos, dict + address, length);
            cursor = address + (int64_t)length;
            break;
        }
        case 1: /* ADD */
            for (uint64_t i = 0; i < length; i++) {
                if (ref_byte(&src, F_LITERAL, &out[out_pos + i])) {
                    return TINYDELTA_ERR_MALFORMED;
                }
            }
            cursor += (int64_t)length;
            break;
        case 2: /* SKIP */
            if ((uint64_t)cursor + length > dict_len) {
                return TINYDELTA_ERR_MALFORMED;
            }
            memcpy(out + out_pos, dict + cursor, length);
            cursor += (int64_t)length;
            break;
        default: { /* COPY_TARGET, or in version 2 ADD_DIFF */
            if (version2) {
                if ((uint64_t)cursor + length > dict_len) {
                    return TINYDELTA_ERR_MALFORMED;
                }
                for (uint64_t i = 0; i < length; i++) {
                    uint8_t diff;
                    if (ref_byte(&src, F_DIFF, &diff)) {
                        return TINYDELTA_ERR_MALFORMED;
                    }
                    out[out_pos + i] = (uint8_t)(dict[(uint64_t)cursor + i] + diff);
                }
                cursor += (int64_t)length;
                break;
            }
            if (ref_varint(&src, F_DELTA, &z)) {
                return TINYDELTA_ERR_MALFORMED;
            }
            int64_t address = (int64_t)out_pos + ref_zigzag(z);
            if (address < 0 || (uint64_t)address >= out_pos) {
                return TINYDELTA_ERR_MALFORMED;
            }
            for (uint64_t i = 0; i < length; i++) {
                out[out_pos + i] = out[(uint64_t)address + i];
            }
            cursor += (int64_t)length;
            break;
        }
        }
        out_pos += length;
    }
    *out_len = out_pos;
    return TINYDELTA_OK;
}

/* ---- Callbacks that police the contract ---------------------------------- */

/* Failure injection: the fail_at-th callback call of a decode fails. */
static uint32_t calls, fail_at;
static int failure_injected;

static int fail_now(void) {
    if (++calls != fail_at) {
        return 0;
    }
    failure_injected = 1;
    return 1;
}

typedef struct {
    const uint8_t *bytes;
    uint32_t len;
} source;

typedef struct {
    uint8_t bytes[FUZZ_MAX_TARGET];
    uint32_t programmed;
    uint32_t page_size;
} flash;

static int source_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len) {
    const source *s = ctx;
    if (len == 0 || offset > s->len || len > s->len - offset) {
        FAIL("read [%u,+%u) outside %u-byte source", offset, len, s->len);
    }
    if (fail_now()) {
        return TINYDELTA_CALLBACK_FAILED;
    }
    memcpy(buf, s->bytes + offset, len);
    return TINYDELTA_CALLBACK_OK;
}

static int flash_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len) {
    const flash *f = ctx;
    if (offset > f->programmed || len > f->programmed - offset) {
        FAIL("flash read [%u,+%u) of unprogrammed data", offset, len);
    }
    if (fail_now()) {
        return TINYDELTA_CALLBACK_FAILED;
    }
    memcpy(buf, f->bytes + offset, len);
    return TINYDELTA_CALLBACK_OK;
}

static int flash_program(void *ctx, uint32_t offset, const uint8_t *buf, uint32_t len) {
    flash *f = ctx;
    if (offset != f->programmed || offset % f->page_size != 0) {
        FAIL("flash program at %u, expected page-aligned %u", offset, f->programmed);
    }
    if (len == 0 || len > f->page_size || len > FUZZ_MAX_TARGET - offset) {
        FAIL("flash program of %u bytes", len);
    }
    if (fail_now()) {
        return TINYDELTA_CALLBACK_FAILED;
    }
    memcpy(f->bytes + offset, buf, len);
    f->programmed += len;
    return TINYDELTA_CALLBACK_OK;
}

/* ---- Fuzz entry point ---------------------------------------------------- */

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    static uint8_t ref_out[FUZZ_MAX_TARGET], mem_out[FUZZ_MAX_TARGET];
    static flash target_flash;

    if (size < FUZZ_HEADER_SIZE) {
        return 0;
    }
    uint32_t out_cap   = (uint32_t)(data[0] | (data[1] << 8)) % (FUZZ_MAX_TARGET + 1u);
    uint32_t dict_len  = data[2];
    /* The callback decoder: a page of 1-32 bytes, a buffer of 1-4 pages. */
    uint32_t page_size = 1u << (data[3] % 6u), buffer_size = page_size << ((data[3] >> 3) % 3u);
    fail_at = data[4];
    data += FUZZ_HEADER_SIZE;
    size -= FUZZ_HEADER_SIZE;
    if (dict_len > size) {
        dict_len = (uint32_t)size;
    }
    const uint8_t *dict  = data;
    const uint8_t *patch = data + dict_len;
    uint32_t patch_len   = (uint32_t)size - dict_len;

    uint64_t ref_len       = 0;
    uint32_t mem_len       = 0;
    tinydelta_result_t ref = ref_decode(dict, dict_len, patch, patch_len, ref_out, out_cap, &ref_len);
    static uint8_t tables[TINYDELTA_CODED_TABLES_A];
    tinydelta_result_t mem = tinydelta_decode_coded(dict, dict_len, patch, patch_len, mem_out, out_cap, tables, sizeof tables, &mem_len);
    if (mem != ref) {
        FAIL("memory decoder returned %d, reference %d", mem, ref);
    }
    if (ref == TINYDELTA_OK && (mem_len != ref_len || memcmp(mem_out, ref_out, mem_len) != 0)) {
        FAIL("memory decoder output differs from reference");
    }

    for (int with_read = 1; with_read >= 0; with_read--) {
        source patch_src = {patch, patch_len}, dict_src = {dict, dict_len};
        /* A coded patch's tables go first, then the ring. */
        static const uint32_t table_sizes[4] = {0u, TINYDELTA_CODED_TABLES_C, TINYDELTA_CODED_TABLES_B, TINYDELTA_CODED_TABLES_A};
        uint32_t tables_size                 = table_sizes[ref_layout(patch, patch_len)];
        uint8_t buffer[TINYDELTA_CODED_TABLES_A + 128];
        tinydelta_io_t io = {
            .patch_read   = source_read,
            .p_patch_ctx  = &patch_src,
            .patch_len    = patch_len,
            .dict_read    = source_read,
            .p_dict_ctx   = &dict_src,
            .dict_len     = dict_len,
            .target_write = flash_program,
            .target_read  = with_read ? flash_read : NULL,
            .p_target_ctx = &target_flash,
            .target_cap   = out_cap,
            .p_buffer     = buffer,
            .buffer_size  = tables_size + buffer_size,
            .page_size    = page_size,
        };
        memset(&target_flash, 0, sizeof target_flash);
        target_flash.page_size    = page_size;
        calls                     = 0;
        failure_injected          = 0;
        uint32_t io_len           = 0;
        tinydelta_result_t result = tinydelta_decode_io(&io, &io_len);
        if (failure_injected) {
            if (result != TINYDELTA_ERR_IO) {
                FAIL("callback decoder returned %d after a failed callback", result);
            }
            continue;
        }
        if (result != mem && !(!with_read && result == TINYDELTA_ERR_MALFORMED)) {
            FAIL("callback decoder (buffer %u, page %u, target_read %d) returned %d, memory decoder %d", buffer_size, page_size, with_read, result, mem);
        }
        if (result == TINYDELTA_OK && (io_len != mem_len || target_flash.programmed != mem_len || memcmp(target_flash.bytes, mem_out, mem_len) != 0)) {
            FAIL("callback decoder output differs");
        }
    }
    return 0;
}
