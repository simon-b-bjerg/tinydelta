/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* Decodes one patch on a target (or its simulator) for scripts/c-time.py:
 * the old image, patch and new image are linked in (TestTimingInputs). The
 * decode runs between timing_start() and timing_stop(); with SKIP_DECODE
 * it doesn't, for a baseline to subtract where a simulator can only count
 * a whole run. CALLBACK selects the callback decoder (a 256-byte buffer of
 * 64-byte pages, reading and writing the arrays), else the memory decoder;
 * TINYDELTA_CODER the coded entry point. timing_result is 0 if the target
 * came out right. */
#include <stddef.h>
#include <stdint.h>

#include "tinydelta.h"
#include "tinydelta_io.h"

#define OUT_SIZE    8192u
#define TABLES_SIZE 2048u /* layout C (522 B) and the map */

extern const unsigned char old_image[], patch[], new_image[];
extern const unsigned long old_image_len, patch_len, new_image_len;

static uint8_t out[OUT_SIZE];
#if TINYDELTA_CODER
static uint8_t tables[TABLES_SIZE];
#endif
volatile int timing_result = -1;
volatile uint32_t timing_length;

__attribute__((noinline)) void timing_start(void) {
    __asm__ volatile("");
}
__attribute__((noinline)) void timing_stop(void) {
    __asm__ volatile("");
}
void timing_done(void); /* per target: stops the simulator */

#if CALLBACK
static uint8_t buffer[256];

static int read_array(void *p_ctx, uint32_t offset, uint8_t *p_buffer, uint32_t length) {
    const unsigned char *p_data = p_ctx;

    for (uint32_t i = 0u; i < length; i++) {
        p_buffer[i] = p_data[offset + i];
    }
    return TINYDELTA_CALLBACK_OK;
}

static int write_out(void *p_ctx, uint32_t offset, const uint8_t *p_buffer, uint32_t length) {
    (void)p_ctx;
    if (offset + length > OUT_SIZE) {
        return TINYDELTA_CALLBACK_FAILED;
    }
    for (uint32_t i = 0u; i < length; i++) {
        out[offset + i] = p_buffer[i];
    }
    return TINYDELTA_CALLBACK_OK;
}

static int read_out(void *p_ctx, uint32_t offset, uint8_t *p_buffer, uint32_t length) {
    (void)p_ctx;
    for (uint32_t i = 0u; i < length; i++) {
        p_buffer[i] = out[offset + i];
    }
    return TINYDELTA_CALLBACK_OK;
}
#endif

int main(void) {
    uint32_t length = 0u;
    int result      = 0;

    timing_start();
#if SKIP_DECODE
    length = (uint32_t)new_image_len; /* the baseline: compares the new image with itself */
#elif CALLBACK
    {
        tinydelta_io_t io = {read_array, (void *)patch, (uint32_t)patch_len, read_array, (void *)old_image, (uint32_t)old_image_len, write_out, read_out, NULL,
                             OUT_SIZE,   buffer,        sizeof buffer,       64u,
#if TINYDELTA_CODER
                             tables,     TABLES_SIZE
#else
                             NULL, 0u
#endif
        };
        result = (int)tinydelta_decode_io(&io, &length);
    }
#elif TINYDELTA_CODER
    result = (int)tinydelta_decode_coded(old_image, (uint32_t)old_image_len, patch, (uint32_t)patch_len, out, OUT_SIZE, tables, TABLES_SIZE, &length);
#else
    result = (int)tinydelta_decode(old_image, (uint32_t)old_image_len, patch, (uint32_t)patch_len, out, OUT_SIZE, &length);
#endif
    timing_stop();
    {
#if SKIP_DECODE
        const unsigned char *p_got = new_image;
#else
        const unsigned char *p_got = out;
#endif
        if ((result == 0) && (length != new_image_len)) {
            result = 100;
        }
        for (uint32_t i = 0u; (result == 0) && (i < length); i++) {
            if (p_got[i] != new_image[i]) {
                result = 101;
            }
        }
    }
    timing_length = length;
    timing_result = result;
    timing_done();
    return 0;
}
