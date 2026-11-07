/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* libFuzzer harness for the relocation engine (tinydelta_reloc.c), built as
 * a decoder that accepts shipped rule tables (TINYDELTA_RELOC_SHIPPED), so
 * the table is untrusted input. Run it with scripts/c-fuzz.sh.
 *
 * Every input is a rule table, an old image and a map. The table checker
 * must never read past the table; for a table it accepts:
 *   - relocating the whole image in one span, from memory;
 *   - relocating it in chunks of 1 to 16 bytes, in an order the input picks
 *     (forwards, backwards, or every other chunk first), the image read
 *     through the raw-byte callback;
 * must give the same bytes: the engine's promise that any span relocates the
 * same whatever order and size the decoder reads it in. The callback
 * polices reads outside the image. Every buffer is allocated to its exact
 * size, so with -fsanitize=address any read or write outside one aborts,
 * and -fsanitize=undefined catches undefined behaviour.
 *
 * Input layout:
 *   byte 0-1  rule table length, little-endian (clipped to the bytes left)
 *   byte 2    image length (clipped to the bytes left after the table)
 *   byte 3-6  the image's base address, little-endian
 *   byte 7    map: bits 0-2 entries (0-7), 3-4 data runs (0-3), 5-7 RAM ranges (0-7)
 *   byte 8    chunks: bits 0-3 chunk size - 1, bits 4-5 order
 *   then      the map's records as the patch header gives them (entries and
 *             RAM ranges: gap, length, zigzag shift; data runs: gap, length
 *             in 256-byte blocks; each value 4 bytes little-endian here, a
 *             varint in a patch), zero-filled if the input is short; then the
 *             table, then the image
 *
 * The map goes through tinydelta_reloc_read_header, as in a decoder, which
 * keeps its records in order and within 32 bits (an input it refuses is
 * skipped); the engine relies on that order. */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "tinydelta_internal.h" /* the features byte's fields */
#include "tinydelta_reloc.h"
#if TINYDELTA_RELOC_SHIPPED
#define TINYDELTA_RELOC_RULES_INTERPRET 1 /* as the engine has it */
#endif
#if TINYDELTA_RELOC == TINYDELTA_RELOC_THUMB
#include "tinydelta_rules_thumb.h"
#else
#include "tinydelta_rules_msp430x.h"
#endif

#define FUZZ_HEADER_SIZE 9u

#define FAIL(...)                                                                                                                                              \
    do {                                                                                                                                                       \
        fprintf(stderr, "fuzz_reloc: " __VA_ARGS__);                                                                                                           \
        fputc('\n', stderr);                                                                                                                                   \
        abort();                                                                                                                                               \
    } while (0)

typedef struct {
    const uint8_t *image;
    uint32_t len;
} raw_source;

static const uint8_t *raw_read(void *ctx, uint32_t index, uint32_t length) {
    const raw_source *s = ctx;
    if (length < 1 || length > TINYDELTA_RELOC_RAW_MAX || index >= s->len || length > s->len - index) {
        FAIL("raw read of %u bytes at %u of a %u-byte image", length, index, s->len);
    }
    return &s->image[index];
}

/* The header as tinydelta_reloc_read_header reads it: the built-in table's
 * CRC, then base, each list's count and its records' values. */
typedef struct {
    uint32_t values[4u + 7u * 3u + 3u * 2u + 7u * 3u]; /* base, three counts, their records' values */
    uint32_t count, next;
    uint16_t crc;
    uint32_t crc_bytes;
} header_source;

static tinydelta_result_t header_fetch(void *ctx, uint8_t *p_byte) {
    header_source *h = ctx;
    *p_byte          = (uint8_t)(h->crc >> (8u * h->crc_bytes++));
    return TINYDELTA_OK;
}

static tinydelta_result_t header_varint(void *ctx, uint32_t *p_value) {
    header_source *h = ctx;
    if (h->next >= h->count) {
        FAIL("the header read %u values, more than its %u", h->next + 1u, h->count);
    }
    *p_value = h->values[h->next++];
    return TINYDELTA_OK;
}

static uint32_t le32_at(const uint8_t *b) {
    return (uint32_t)b[0] | ((uint32_t)b[1] << 8) | ((uint32_t)b[2] << 16) | ((uint32_t)b[3] << 24);
}

/* A copy of n bytes from *p (fewer if the input runs out: zeros), in a
 * buffer of exactly n bytes (at least 1, so malloc doesn't return NULL). */
static uint8_t *take(const uint8_t **p, size_t *left, size_t n) {
    uint8_t *b  = calloc(n > 0 ? n : 1, 1);
    size_t have = n < *left ? n : *left;
    if (b == NULL) {
        abort();
    }
    memcpy(b, *p, have);
    *p += have;
    *left -= have;
    return b;
}

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    if (size < FUZZ_HEADER_SIZE) {
        return 0;
    }
    size_t table_len = (size_t)data[0] | ((size_t)data[1] << 8);
    size_t image_len = data[2];
    uint32_t base    = (uint32_t)data[3] | ((uint32_t)data[4] << 8) | ((uint32_t)data[5] << 16) | ((uint32_t)data[6] << 24);
    uint32_t entries = data[7] & 7u, runs = (data[7] >> 3) & 3u, ranges = (data[7] >> 5) & 7u;
    uint32_t chunk = (data[8] & 15u) + 1u, order = (data[8] >> 4) & 3u;
    const uint8_t *p = data + FUZZ_HEADER_SIZE;
    size_t left      = size - FUZZ_HEADER_SIZE;

    uint8_t *records = take(&p, &left, (entries * 3u + runs * 2u + ranges * 3u) * 4u);
    if (table_len > left) {
        table_len = left;
    }
    uint8_t *table = take(&p, &left, table_len);
    if (image_len > left) {
        image_len = left;
    }
    uint8_t *image = take(&p, &left, image_len);

    tinydelta_reloc_t reloc;
    memset(&reloc, 0, sizeof reloc);
    /* The map through the decoder's header reader, into storage of exactly its size. */
    header_source h            = {{0}, 0, 0, 0, 0};
    const uint32_t lists[3][2] = {{entries, 3u}, {runs, 2u}, {ranges, 3u}};
    uint32_t at                = 0;
    h.values[h.count++]        = base;
    for (uint32_t l = 0; l < 3u; l++) {
        h.values[h.count++] = lists[l][0];
        for (uint32_t i = 0; i < lists[l][0] * lists[l][1]; i++, at += 4u) {
            h.values[h.count++] = le32_at(records + at);
        }
    }
    h.crc                 = TINYDELTA_RELOC_RULES_CRC; /* the built-in table's (a shipped build's table is set below) */
    uint32_t storage_size = (entries + ranges) * TINYDELTA_RELOC_ENTRY_SIZE + runs * 8u;
    uint8_t *storage      = malloc(storage_size > 0u ? storage_size : 1u);
    if (storage == NULL) {
        abort();
    }
    tinydelta_result_t header = tinydelta_reloc_read_header(&reloc, TINYDELTA_CODED_RELOC_BUILTIN << TINYDELTA_CODED_RELOC_SHIFT, header_fetch, header_varint,
                                                            &h, storage, storage_size);
    (void)tinydelta_reloc_rules; /* the engine's own copy is the one in use */
#if TINYDELTA_RELOC_SHIPPED
    reloc.p_rules   = table; /* untrusted: the checker's */
    reloc.rules_len = (uint32_t)table_len;
#endif
    reloc.b_shipped = false; /* the table is here already, not in a coded stream */
    reloc.dict_len  = (uint32_t)image_len;

    if ((header != TINYDELTA_OK) && (header != TINYDELTA_ERR_MALFORMED)) {
        FAIL("the header reader gave %d for a map that fits", header);
    }
    if (header == TINYDELTA_OK && h.next != h.count) {
        FAIL("the header reader read %u of the map's %u values", h.next, h.count);
    }
    if (header == TINYDELTA_OK && image_len > 0 && tinydelta_reloc_start(&reloc, NULL) == TINYDELTA_OK) {
        uint8_t *whole = take(&(const uint8_t *){image}, &(size_t){image_len}, image_len);
        uint8_t *parts = calloc(image_len, 1);
        uint32_t n     = (uint32_t)image_len;
        uint32_t count = (n + chunk - 1u) / chunk;
        raw_source src = {image, n};

        if (parts == NULL) {
            abort();
        }
        /* In one span, from memory. */
        reloc.p_dict = image;
        tinydelta_reloc_span(&reloc, 0, whole, n);

        /* In chunks, through the callback, in the order picked: each chunk into
         * its own buffer of its exact size, copied into place. */
        reloc.p_dict    = NULL;
        reloc.raw       = raw_read;
        reloc.p_raw_ctx = &src;
        for (uint32_t k = 0; k < count; k++) {
            uint32_t i = k;
            if (order == 1u) {
                i = count - 1u - k; /* backwards */
            } else if (order >= 2u) {
                i = (k < (count + 1u) / 2u) ? 2u * k : 2u * (k - (count + 1u) / 2u) + 1u; /* even chunks, then odd */
            }
            uint32_t offset = i * chunk;
            uint32_t len    = (n - offset < chunk) ? n - offset : chunk;
            uint8_t *out    = malloc(len);
            if (out == NULL) {
                abort();
            }
            memcpy(out, image + offset, len);
            tinydelta_reloc_span(&reloc, offset, out, len);
            memcpy(parts + offset, out, len);
            free(out);
        }
        if (memcmp(whole, parts, n) != 0) {
            for (uint32_t i = 0; i < n; i++) {
                if (whole[i] != parts[i]) {
                    FAIL("chunks of %u bytes (order %u) give byte %u = 0x%02x, one span 0x%02x", chunk, order, i, parts[i], whole[i]);
                }
            }
        }
        free(whole);
        free(parts);
    }
    free(records);
    free(storage);
    free(table);
    free(image);
    return 0;
}
