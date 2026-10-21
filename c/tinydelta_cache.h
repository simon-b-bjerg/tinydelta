/* Optional write cache for tinydelta_decode_io's target, for storage that
 * should be written a page at a time (e.g. NOR flash page programs).
 *
 * It sits between the decoder and the client's raw flash functions:
 * target writes are collected in a client-supplied page buffer, and
 * flash_write is called with one whole page at a time, at increasing
 * page-aligned offsets, plus a final partial page when decoding
 * finishes. Target reads are served from flash for data already written
 * and from the page buffer for data not yet written, so reads that span
 * the boundary between the two work too.
 *
 * Offsets passed to flash_read/flash_write are relative to the start of
 * the target region; page_size alignment is relative to that start. The
 * flash must already be erased (or flash_write must erase as needed), and
 * flash_read must see data from earlier flash_write calls. */
#ifndef TINYDELTA_CACHE_H
#define TINYDELTA_CACHE_H

#include <stdint.h>

#include "tinydelta_io.h"

typedef struct {
    tinydelta_read_fn flash_read;
    tinydelta_write_fn flash_write;
    void *flash_ctx;
    uint8_t *page;      /* client-supplied buffer of page_size bytes */
    uint32_t page_size;
    uint32_t base;      /* target offset of page[0] */
    uint32_t fill;      /* bytes currently buffered in page */
} tinydelta_cache;

void tinydelta_cache_init(tinydelta_cache *cache, tinydelta_read_fn flash_read,
                          tinydelta_write_fn flash_write, void *flash_ctx,
                          uint8_t *page, uint32_t page_size);

/* Points io's target callbacks (read, write, flush) at the cache. */
void tinydelta_cache_attach(tinydelta_cache *cache, tinydelta_io *io);

/* The target callbacks themselves, for wiring up by hand; ctx is the
 * tinydelta_cache. Writes must be sequential (each starts where the last
 * ended); flush writes out the final partial page. */
int tinydelta_cache_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len);
int tinydelta_cache_write(void *ctx, uint32_t offset, const uint8_t *buf, uint32_t len);
int tinydelta_cache_flush(void *ctx);

#endif
