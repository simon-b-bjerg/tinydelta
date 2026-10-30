/* libFuzzer harness for the C decoders. Run it with scripts/c-fuzz.sh.
 *
 * Every input is decoded by:
 *   - a reference decoder in this file, written from the README's wire
 *     format with 64-bit arithmetic and no shortcuts;
 *   - tinydelta_decode (memory decoder);
 *   - tinydelta_decode_io (callback decoder) at three scratch-buffer sizes
 *     (1, 7 and 64 bytes), each with plain RAM callbacks and with
 *     tinydelta_cache on top of a simulated flash.
 *
 * All of them must agree: the same result (OK, MALFORMED or TOO_LARGE) and,
 * on success, the same target bytes. The callbacks also police the
 * documented contract: sequential target writes, no reads outside the
 * patch or dictionary, no reads of target bytes not yet written, whole
 * page-aligned flash programs, exactly one flush. Any violation aborts.
 * The input can also make one callback call fail, which the callback
 * decoder must then report as TINYDELTA_ERR_IO.
 * Build with -fsanitize=fuzzer,address,undefined, so out-of-bounds access
 * and undefined behaviour abort too.
 *
 * Input layout:
 *   byte 0-1  output capacity, little-endian, taken modulo FUZZ_MAX_TARGET + 1
 *   byte 2    dictionary length (clipped to the bytes that follow)
 *   byte 3    flash page size for the cache, 1 + (byte % FUZZ_MAX_PAGE)
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
#include "tinydelta_cache.h"
#include "tinydelta_io.h"

/* tinydelta_io.c compiled three times with different TINYDELTA_SCRATCH_SIZE,
 * its entry point renamed for each (see scripts/c-fuzz.sh). */
tinydelta_result fuzz_decode_io_scratch1(const tinydelta_io *io, uint32_t *target_len);
tinydelta_result fuzz_decode_io_scratch7(const tinydelta_io *io, uint32_t *target_len);
tinydelta_result fuzz_decode_io_scratch64(const tinydelta_io *io, uint32_t *target_len);

#define FUZZ_HEADER_SIZE 5u
#define FUZZ_MAX_TARGET  4096u
#define FUZZ_MAX_PAGE    16u

#define FAIL(...) do { fprintf(stderr, "fuzz_decode: " __VA_ARGS__); fputc('\n', stderr); abort(); } while (0)

/* ---- Reference decoder --------------------------------------------------- */

static int ref_varint(const uint8_t *patch, uint64_t patch_len, uint64_t *pos, uint64_t *value) {
    uint64_t result = 0;
    for (int i = 0; i < 5; i++) {
        if (*pos >= patch_len) return -1;
        uint8_t byte = patch[(*pos)++];
        if (i == 4 && byte > 0x0F) return -1;
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

static tinydelta_result ref_decode(const uint8_t *dict, uint64_t dict_len,
                                   const uint8_t *patch, uint64_t patch_len,
                                   uint8_t *out, uint64_t out_cap, uint64_t *out_len) {
    uint64_t patch_pos = 0, out_pos = 0;
    int64_t cursor = 0;

    while (patch_pos < patch_len) {
        uint64_t instruction, z;
        if (ref_varint(patch, patch_len, &patch_pos, &instruction)) return TINYDELTA_ERR_MALFORMED;
        uint64_t length = instruction >> 2;
        if (out_pos + length > out_cap) return TINYDELTA_ERR_TOO_LARGE;

        switch (instruction & 3) {
        case 0: { /* COPY */
            if (ref_varint(patch, patch_len, &patch_pos, &z)) return TINYDELTA_ERR_MALFORMED;
            int64_t address = cursor + ref_zigzag(z);
            if (address < 0 || (uint64_t)address + length > dict_len) return TINYDELTA_ERR_MALFORMED;
            memcpy(out + out_pos, dict + address, length);
            cursor = address + (int64_t)length;
            break;
        }
        case 1: /* ADD */
            if (patch_pos + length > patch_len) return TINYDELTA_ERR_MALFORMED;
            memcpy(out + out_pos, patch + patch_pos, length);
            patch_pos += length;
            cursor += (int64_t)length;
            break;
        case 2: /* SKIP */
            if ((uint64_t)cursor + length > dict_len) return TINYDELTA_ERR_MALFORMED;
            memcpy(out + out_pos, dict + cursor, length);
            cursor += (int64_t)length;
            break;
        default: { /* COPY_TARGET */
            if (ref_varint(patch, patch_len, &patch_pos, &z)) return TINYDELTA_ERR_MALFORMED;
            int64_t address = (int64_t)out_pos + ref_zigzag(z);
            if (address < 0 || (uint64_t)address >= out_pos) return TINYDELTA_ERR_MALFORMED;
            for (uint64_t i = 0; i < length; i++) out[out_pos + i] = out[(uint64_t)address + i];
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
    if (++calls != fail_at) return 0;
    failure_injected = 1;
    return 1;
}

typedef struct {
    const uint8_t *bytes;
    uint32_t len;
} source;

typedef struct {
    uint8_t bytes[FUZZ_MAX_TARGET];
    uint32_t written;
    uint32_t flushes;
} sink;

typedef struct {
    uint8_t bytes[FUZZ_MAX_TARGET];
    uint32_t programmed;
    uint32_t page_size;
} flash;

static int source_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len) {
    const source *s = ctx;
    if (len == 0 || offset > s->len || len > s->len - offset) FAIL("read [%u,+%u) outside %u-byte source", offset, len, s->len);
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    memcpy(buf, s->bytes + offset, len);
    return TINYDELTA_CALLBACK_OK;
}

static int sink_write(void *ctx, uint32_t offset, const uint8_t *buf, uint32_t len) {
    sink *t = ctx;
    if (offset != t->written) FAIL("target write at %u, expected %u", offset, t->written);
    if (len == 0 || len > FUZZ_MAX_TARGET - offset) FAIL("target write of %u bytes at %u past capacity", len, offset);
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    memcpy(t->bytes + offset, buf, len);
    t->written += len;
    return TINYDELTA_CALLBACK_OK;
}

static int sink_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len) {
    const sink *t = ctx;
    if (len == 0 || offset > t->written || len > t->written - offset) FAIL("target read [%u,+%u) of unwritten data", offset, len);
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    memcpy(buf, t->bytes + offset, len);
    return TINYDELTA_CALLBACK_OK;
}

static int sink_flush(void *ctx) {
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    ((sink *)ctx)->flushes++;
    return TINYDELTA_CALLBACK_OK;
}

static int flash_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len) {
    const flash *f = ctx;
    if (offset > f->programmed || len > f->programmed - offset) FAIL("flash read [%u,+%u) of unprogrammed data", offset, len);
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    memcpy(buf, f->bytes + offset, len);
    return TINYDELTA_CALLBACK_OK;
}

static int flash_program(void *ctx, uint32_t offset, const uint8_t *buf, uint32_t len) {
    flash *f = ctx;
    if (offset != f->programmed || offset % f->page_size != 0) FAIL("flash program at %u, expected page-aligned %u", offset, f->programmed);
    if (len == 0 || len > f->page_size || len > FUZZ_MAX_TARGET - offset) FAIL("flash program of %u bytes", len);
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    memcpy(f->bytes + offset, buf, len);
    f->programmed += len;
    return TINYDELTA_CALLBACK_OK;
}

/* ---- Fuzz entry point ---------------------------------------------------- */

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    static uint8_t ref_out[FUZZ_MAX_TARGET], mem_out[FUZZ_MAX_TARGET];
    static sink target;
    static flash target_flash;
    static tinydelta_result (*const decode_io[])(const tinydelta_io *, uint32_t *) = {
        fuzz_decode_io_scratch1, fuzz_decode_io_scratch7, fuzz_decode_io_scratch64,
    };

    if (size < FUZZ_HEADER_SIZE) return 0;
    uint32_t out_cap = (uint32_t)(data[0] | (data[1] << 8)) % (FUZZ_MAX_TARGET + 1u);
    uint32_t dict_len = data[2];
    uint32_t page_size = 1u + data[3] % FUZZ_MAX_PAGE;
    fail_at = data[4];
    data += FUZZ_HEADER_SIZE;
    size -= FUZZ_HEADER_SIZE;
    if (dict_len > size) dict_len = (uint32_t)size;
    const uint8_t *dict = data;
    const uint8_t *patch = data + dict_len;
    uint32_t patch_len = (uint32_t)size - dict_len;

    uint64_t ref_len = 0;
    uint32_t mem_len = 0;
    tinydelta_result ref = ref_decode(dict, dict_len, patch, patch_len, ref_out, out_cap, &ref_len);
    tinydelta_result mem = tinydelta_decode(dict, dict_len, patch, patch_len, mem_out, out_cap, &mem_len);
    if (mem != ref) FAIL("memory decoder returned %d, reference %d", mem, ref);
    if (ref == TINYDELTA_OK && (mem_len != ref_len || memcmp(mem_out, ref_out, mem_len) != 0)) {
        FAIL("memory decoder output differs from reference");
    }

    for (size_t d = 0; d < sizeof decode_io / sizeof decode_io[0]; d++) {
        for (int use_cache = 0; use_cache <= 1; use_cache++) {
            source patch_src = {patch, patch_len}, dict_src = {dict, dict_len};
            uint8_t page[FUZZ_MAX_PAGE];
            tinydelta_cache cache;
            tinydelta_io io = {
                .patch_read = source_read, .patch_ctx = &patch_src, .patch_len = patch_len,
                .dict_read = source_read, .dict_ctx = &dict_src, .dict_len = dict_len,
                .target_read = sink_read, .target_write = sink_write, .target_flush = sink_flush,
                .target_ctx = &target, .target_cap = out_cap,
            };
            memset(&target, 0, sizeof target);
            memset(&target_flash, 0, sizeof target_flash);
            calls = 0;
            failure_injected = 0;
            if (use_cache) {
                target_flash.page_size = page_size;
                tinydelta_cache_init(&cache, flash_read, flash_program, &target_flash, page, page_size);
                tinydelta_cache_attach(&cache, &io);
            }

            uint32_t io_len = 0;
            tinydelta_result result = decode_io[d](&io, &io_len);
            if (failure_injected) {
                if (result != TINYDELTA_ERR_IO) FAIL("callback decoder %zu (cache %d) returned %d after a failed callback", d, use_cache, result);
                continue;
            }
            if (result != mem) FAIL("callback decoder %zu (cache %d) returned %d, memory decoder %d", d, use_cache, result, mem);
            if (result != TINYDELTA_OK) continue;

            const uint8_t *produced = use_cache ? target_flash.bytes : target.bytes;
            uint32_t produced_len = use_cache ? target_flash.programmed : target.written;
            if (io_len != mem_len || produced_len != mem_len || memcmp(produced, mem_out, mem_len) != 0) {
                FAIL("callback decoder %zu (cache %d) output differs", d, use_cache);
            }
            if (!use_cache && target.flushes != 1) FAIL("target_flush called %u times", target.flushes);
        }
    }
    return 0;
}
