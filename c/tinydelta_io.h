/* tinydelta decoder with the dictionary, patch and target behind
 * callbacks, for data that isn't in RAM (e.g. external flash).
 * See README.md for the wire format. */
#ifndef TINYDELTA_IO_H
#define TINYDELTA_IO_H

#include <stdint.h>

#include "tinydelta.h"

/* Size of the decoder's internal buffer, which moves bytes from a read
 * callback to the write callback. Larger means fewer, bigger callback
 * calls. It lives on tinydelta_decode_io's stack. Must be at least 1. */
#ifndef TINYDELTA_SCRATCH_SIZE
#define TINYDELTA_SCRATCH_SIZE 64
#endif

/* What callbacks return. Anything other than TINYDELTA_CALLBACK_OK aborts
 * decoding with TINYDELTA_ERR_IO. */
enum {
    TINYDELTA_CALLBACK_OK     = 0,
    TINYDELTA_CALLBACK_FAILED = -1
};

/* Offsets are relative to the start of the patch, dictionary or target. */
typedef int (*tinydelta_read_fn)(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len);
typedef int (*tinydelta_write_fn)(void *ctx, uint32_t offset, const uint8_t *buf, uint32_t len);
typedef int (*tinydelta_flush_fn)(void *ctx);

typedef struct {
    /* Patch: read front to back, a few bytes at a time. */
    tinydelta_read_fn patch_read;
    void *patch_ctx;
    uint32_t patch_len;

    /* Dictionary: read at random offsets. */
    tinydelta_read_fn dict_read;
    void *dict_ctx;
    uint32_t dict_len;

    /* Target:
     *   target_write  called with strictly sequential offsets: each write
     *                 starts where the previous one ended.
     *   target_read   random access; must return any bytes already passed
     *                 to target_write, even if still buffered, not yet in
     *                 flash.
     *   target_flush  optional (may be NULL); called once after a
     *                 successful decode.
     * tinydelta_cache.h provides all three on top of page-wise flash. */
    tinydelta_read_fn target_read;
    tinydelta_write_fn target_write;
    tinydelta_flush_fn target_flush;
    void *target_ctx;
    uint32_t target_cap; /* max target size (capped at TINYDELTA_MAX_SIZE) */
} tinydelta_io;

/* Applies the patch to the dictionary, writing the target through the
 * target callbacks. On success, sets *target_len to the target's length.
 * On error, whatever was already written to the target is incomplete and
 * must not be used. No allocation; uses TINYDELTA_SCRATCH_SIZE bytes of
 * stack plus a few words. */
tinydelta_result tinydelta_decode_io(const tinydelta_io *io, uint32_t *target_len);

#endif
