/* CBMC harness: the callback decoder, for every dictionary, patch and
 * capacity up to the bounds below, with one buffer setup per run
 * (BUFFER_SIZE, PAGE_SIZE, WITH_READ; see scripts/c-cbmc.sh).
 *
 * Proves, within the bounds: no out-of-bounds access or undefined
 * behaviour, and every callback keeps its contract: reads only inside
 * the patch and dictionary, target reads only of bytes already written,
 * writes of whole pages, in order, at page-aligned offsets, then at most
 * one final partial page, within the capacity.
 *
 * And, by run:
 *   EQUIV          the memory decoder's result and target bytes, except
 *                  that without target_read a copy reaching past the
 *                  buffer may be rejected as malformed instead;
 *   INJECT         any one callback call failing gives TINYDELTA_ERR_IO;
 *   a bad setup    (BUFFER_SIZE or PAGE_SIZE not a power of two, or the
 *                  page larger than the buffer) TINYDELTA_ERR_MALFORMED. */
#include <assert.h>
#include <string.h>

#include "tinydelta.h"
#include "tinydelta_io.h"

#ifndef DICT_MAX
#define DICT_MAX 3
#endif
#ifndef PATCH_MAX
#define PATCH_MAX 6
#endif
#ifndef OUT_MAX
#define OUT_MAX 6
#endif
#ifndef BUFFER_SIZE
#define BUFFER_SIZE 2
#endif
#ifndef PAGE_SIZE
#define PAGE_SIZE 1
#endif
#ifndef WITH_READ
#define WITH_READ 1
#endif
#ifndef INJECT
#define INJECT 0
#endif
#ifndef EQUIV
#define EQUIV 1 /* 0: only safety and the callback contract */
#endif

uint32_t nondet_u32(void);

typedef struct {
    const uint8_t *bytes;
    uint32_t len;
} source;

static uint8_t flash[OUT_MAX];
static uint32_t written, cap, calls, fail_at;
static int final_seen, failed;

/* With INJECT, the fail_at-th callback call fails. */
static int fail_now(void) {
    if (!INJECT || ++calls != fail_at) return 0;
    failed = 1;
    return 1;
}

static int source_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len) {
    const source *s = ctx;
    assert(len > 0 && offset <= s->len && len <= s->len - offset);
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    memcpy(buf, s->bytes + offset, len);
    return TINYDELTA_CALLBACK_OK;
}

static int flash_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len) {
    (void)ctx;
    assert(len > 0 && offset <= written && len <= written - offset);
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    memcpy(buf, flash + offset, len);
    return TINYDELTA_CALLBACK_OK;
}

static int flash_write(void *ctx, uint32_t offset, const uint8_t *buf, uint32_t len) {
    (void)ctx;
    assert(!final_seen); /* nothing after the final partial page */
    assert(offset == written && offset % PAGE_SIZE == 0);
    assert(len > 0 && len <= PAGE_SIZE && len <= cap - offset);
    if (len < PAGE_SIZE) final_seen = 1;
    if (fail_now()) return TINYDELTA_CALLBACK_FAILED;
    memcpy(flash + offset, buf, len);
    written += len;
    return TINYDELTA_CALLBACK_OK;
}

#define POWER_OF_TWO(x) ((x) != 0 && ((x) & ((x) - 1u)) == 0)
#define VALID_SETUP (POWER_OF_TWO(BUFFER_SIZE) && POWER_OF_TWO(PAGE_SIZE) && PAGE_SIZE <= BUFFER_SIZE)

int main(void) {
    uint32_t dict_len = nondet_u32(), patch_len = nondet_u32();
    cap = nondet_u32();
    fail_at = nondet_u32();
    __CPROVER_assume(dict_len <= DICT_MAX && patch_len <= PATCH_MAX && cap <= OUT_MAX);

    /* Each input ends where its array ends, so reading or writing one
     * byte past it leaves the array, which CBMC reports. */
    static uint8_t dict_area[DICT_MAX], patch_area[PATCH_MAX], buffer[BUFFER_SIZE + 1];
    for (uint32_t i = 0; i < DICT_MAX; i++) dict_area[i] = (uint8_t)nondet_u32();
    for (uint32_t i = 0; i < PATCH_MAX; i++) patch_area[i] = (uint8_t)nondet_u32();
    const uint8_t *dict = dict_area + (DICT_MAX - dict_len), *patch = patch_area + (PATCH_MAX - patch_len);

    source patch_src = {patch, patch_len}, dict_src = {dict, dict_len};
    tinydelta_io_t io = {
        .patch_read = source_read, .p_patch_ctx = &patch_src, .patch_len = patch_len,
        .dict_read = source_read, .p_dict_ctx = &dict_src, .dict_len = dict_len,
        .target_write = flash_write, .target_read = WITH_READ ? flash_read : 0,
        .p_target_ctx = 0, .target_cap = cap,
        .p_buffer = buffer + 1, .buffer_size = BUFFER_SIZE, .page_size = PAGE_SIZE,
    };
    uint32_t io_len = 0xFFFFFFFFu;
    tinydelta_result_t result = tinydelta_decode_io(&io, &io_len);

    if (!VALID_SETUP) {
        assert(result == TINYDELTA_ERR_MALFORMED);
    } else if (INJECT) {
        if (failed) assert(result == TINYDELTA_ERR_IO);
    } else if (EQUIV) {
        static uint8_t out_area[OUT_MAX];
        uint8_t *out = out_area + (OUT_MAX - cap);
        uint32_t mem_len = 0xFFFFFFFFu;
        tinydelta_result_t expected = tinydelta_decode(dict, dict_len, patch, patch_len, out, cap, &mem_len);
        assert(result == expected || (!WITH_READ && result == TINYDELTA_ERR_MALFORMED));
        if (result == TINYDELTA_OK) {
            assert(io_len == mem_len && written == mem_len);
            if (io_len == mem_len && written == mem_len) assert(memcmp(flash, out, mem_len) == 0);
        }
    }
    return 0;
}
