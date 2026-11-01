/* libFuzzer harness for the C decoders. Run it with scripts/c-fuzz.sh.
 *
 * Every input is decoded by:
 *   - a reference decoder in this file, written from the README's wire
 *     format with 64-bit arithmetic and no shortcuts;
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

static int ref_varint(const uint8_t *patch, uint64_t patch_len, uint64_t *pos, uint64_t *value) {
    uint64_t result = 0;
    for (int i = 0; i < 5; i++) {
        if (*pos >= patch_len) {
            return -1;
        }
        uint8_t byte = patch[(*pos)++];
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

static tinydelta_result_t
ref_decode(const uint8_t *dict, uint64_t dict_len, const uint8_t *patch, uint64_t patch_len, uint8_t *out, uint64_t out_cap, uint64_t *out_len) {
    uint64_t patch_pos = 1, out_pos = 0; /* past the version byte */
    int64_t cursor = 0;

    if (patch_len < 1) {
        return TINYDELTA_ERR_MALFORMED;
    }
    if (patch[0] != 1 && patch[0] != 2) {
        return TINYDELTA_ERR_VERSION;
    }
    const int version2 = patch[0] == 2; /* opcode 3 is ADD_DIFF */
    while (patch_pos < patch_len) {
        uint64_t instruction, z;
        if (ref_varint(patch, patch_len, &patch_pos, &instruction)) {
            return TINYDELTA_ERR_MALFORMED;
        }
        uint64_t length = instruction >> 2;
        if (out_pos + length > out_cap) {
            return TINYDELTA_ERR_TOO_LARGE;
        }

        switch (instruction & 3) {
        case 0: { /* COPY */
            if (ref_varint(patch, patch_len, &patch_pos, &z)) {
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
            if (patch_pos + length > patch_len) {
                return TINYDELTA_ERR_MALFORMED;
            }
            memcpy(out + out_pos, patch + patch_pos, length);
            patch_pos += length;
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
                if ((uint64_t)cursor + length > dict_len || patch_pos + length > patch_len) {
                    return TINYDELTA_ERR_MALFORMED;
                }
                for (uint64_t i = 0; i < length; i++) {
                    out[out_pos + i] = (uint8_t)(dict[(uint64_t)cursor + i] + patch[patch_pos + i]);
                }
                patch_pos += length;
                cursor += (int64_t)length;
                break;
            }
            if (ref_varint(patch, patch_len, &patch_pos, &z)) {
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
    tinydelta_result_t mem = tinydelta_decode(dict, dict_len, patch, patch_len, mem_out, out_cap, &mem_len);
    if (mem != ref) {
        FAIL("memory decoder returned %d, reference %d", mem, ref);
    }
    if (ref == TINYDELTA_OK && (mem_len != ref_len || memcmp(mem_out, ref_out, mem_len) != 0)) {
        FAIL("memory decoder output differs from reference");
    }

    for (int with_read = 1; with_read >= 0; with_read--) {
        source patch_src = {patch, patch_len}, dict_src = {dict, dict_len};
        uint8_t buffer[128];
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
            .buffer_size  = buffer_size,
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
