/* tinydelta decoder with the dictionary, patch and target behind
 * callbacks, for data that isn't in RAM (e.g. external flash). See
 * README.md, "Callback decoder", and for the wire format.
 *
 * One client-supplied buffer is at once the decoder's working buffer, the
 * flash page cache and the history COPY_TARGET reads from. Target bytes
 * are produced straight into the buffer and handed to target_write a
 * whole page at a time: at page-aligned offsets, every write page_size
 * bytes long except the last, which ends the target. The buffer keeps the
 * last buffer_size bytes of the target, so a COPY_TARGET reaching back at
 * most buffer_size bytes is served from RAM; one reaching further reads
 * flash through target_read. Patches made with
 * EncodeOptions.MaxTargetDistance = buffer_size (CLI: encode
 * -max-distance) never do, and then target_read may be NULL. */
#ifndef TINYDELTA_IO_H
#define TINYDELTA_IO_H

#include <stdint.h>

#include "tinydelta.h"

/* What callbacks return. Anything other than TINYDELTA_CALLBACK_OK aborts
 * decoding with TINYDELTA_ERR_IO. */
enum {
    TINYDELTA_CALLBACK_OK     = 0,
    TINYDELTA_CALLBACK_FAILED = -1
};

/* Offsets are relative to the start of the patch, dictionary or target. */
typedef int (*tinydelta_read_fn)(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len);
typedef int (*tinydelta_write_fn)(void *ctx, uint32_t offset, const uint8_t *buf, uint32_t len);

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
     *   target_write  whole pages, in order (see above).
     *   target_read   only for COPY_TARGETs reaching back more than
     *                 buffer_size bytes, and only ever of bytes already
     *                 passed to target_write. May be NULL; such a patch is
     *                 then rejected as malformed. */
    tinydelta_write_fn target_write;
    tinydelta_read_fn target_read;
    void *target_ctx;
    uint32_t target_cap; /* max target size (capped at TINYDELTA_MAX_SIZE) */

    /* The buffer: buffer_size bytes, a power of two. page_size, the size
     * of each target_write, is a power of two no larger than buffer_size
     * (equal is fine: one page of RAM). */
    uint8_t *buffer;
    uint32_t buffer_size;
    uint32_t page_size;
} tinydelta_io;

/* Applies the patch to the dictionary, writing the target through
 * target_write. On success, sets *target_len to the target's length. On
 * error, whatever was already written is incomplete and must not be used.
 * Returns TINYDELTA_ERR_MALFORMED also when buffer_size or page_size is
 * not as required. No allocation; a few dozen bytes of stack besides the
 * buffer. */
tinydelta_result tinydelta_decode_io(const tinydelta_io *io, uint32_t *target_len);

#endif
