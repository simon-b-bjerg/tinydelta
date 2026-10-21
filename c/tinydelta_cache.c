#include <string.h>

#include "tinydelta_cache.h"

void tinydelta_cache_init(tinydelta_cache *cache, tinydelta_read_fn flash_read,
                          tinydelta_write_fn flash_write, void *flash_ctx,
                          uint8_t *page, uint32_t page_size) {
    cache->flash_read = flash_read;
    cache->flash_write = flash_write;
    cache->flash_ctx = flash_ctx;
    cache->page = page;
    cache->page_size = page_size;
    cache->base = 0;
    cache->fill = 0;
}

void tinydelta_cache_attach(tinydelta_cache *cache, tinydelta_io *io) {
    io->target_read = tinydelta_cache_read;
    io->target_write = tinydelta_cache_write;
    io->target_flush = tinydelta_cache_flush;
    io->target_ctx = cache;
}

int tinydelta_cache_write(void *ctx, uint32_t offset, const uint8_t *buf, uint32_t len) {
    tinydelta_cache *cache = ctx;
    uint32_t buffered_end = cache->base + cache->fill;

    if (cache->page_size == 0 || offset != buffered_end) {
        return TINYDELTA_CALLBACK_FAILED; /* writes must be sequential */
    }
    while (len > 0) {
        uint32_t space = cache->page_size - cache->fill;
        uint32_t chunk = len < space ? len : space;

        memcpy(cache->page + cache->fill, buf, chunk);
        cache->fill += chunk;
        buf += chunk;
        len -= chunk;

        if (cache->fill == cache->page_size) { /* page full: program it */
            if (cache->flash_write(cache->flash_ctx, cache->base, cache->page, cache->page_size) != TINYDELTA_CALLBACK_OK) {
                return TINYDELTA_CALLBACK_FAILED;
            }
            cache->base += cache->page_size;
            cache->fill = 0;
        }
    }
    return TINYDELTA_CALLBACK_OK;
}

int tinydelta_cache_read(void *ctx, uint32_t offset, uint8_t *buf, uint32_t len) {
    tinydelta_cache *cache = ctx;
    uint32_t buffered_end = cache->base + cache->fill;
    uint32_t from_flash = 0;

    if (offset > buffered_end || len > buffered_end - offset) {
        return TINYDELTA_CALLBACK_FAILED; /* not written yet */
    }

    /* The part before the page buffer has already been programmed. */
    if (offset < cache->base) {
        from_flash = cache->base - offset;
        if (from_flash > len) from_flash = len;
        if (cache->flash_read(cache->flash_ctx, offset, buf, from_flash) != TINYDELTA_CALLBACK_OK) {
            return TINYDELTA_CALLBACK_FAILED;
        }
    }

    /* The rest, if any, is still in the page buffer. */
    if (len > from_flash) {
        uint32_t page_offset = offset + from_flash - cache->base;
        memcpy(buf + from_flash, cache->page + page_offset, len - from_flash);
    }
    return TINYDELTA_CALLBACK_OK;
}

int tinydelta_cache_flush(void *ctx) {
    tinydelta_cache *cache = ctx;

    if (cache->fill == 0) return TINYDELTA_CALLBACK_OK;
    if (cache->flash_write(cache->flash_ctx, cache->base, cache->page, cache->fill) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_CALLBACK_FAILED;
    }
    cache->base += cache->fill;
    cache->fill = 0;
    return TINYDELTA_CALLBACK_OK;
}
