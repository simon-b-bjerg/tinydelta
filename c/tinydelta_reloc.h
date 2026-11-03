/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* Relocation for coded patches (see README.md, "Relocation"): rewrites the
 * addresses in old-image bytes the decoders read, through the map the patch
 * carries. Internal: used by both decoders when built with TINYDELTA_RELOC. */
#ifndef TINYDELTA_RELOC_H
#define TINYDELTA_RELOC_H

#include <stdint.h>

#include "tinydelta.h"

/* Gets the raw old-image byte at index (always within the image). On a
 * read failure it returns anything and records the failure for its
 * decoder to report after tinydelta_reloc_span. */
typedef uint8_t (*tinydelta_raw_fn_t)(void *p_ctx, uint32_t index);

/* Reads the next varint of the patch header. */
typedef tinydelta_result_t (*tinydelta_varint_fn_t)(void *p_ctx, uint32_t *p_value);

/* Bytes of RAM per map entry: start, length, shift, each 32 bits (and 8 per
 * MSP430X data run: start, length). */
#define TINYDELTA_RELOC_ENTRY_SIZE 12u

/* The map and how to read the raw old image: from p_dict when it is in
 * memory, else through raw. */
typedef struct {
    const uint8_t *p_entries; /* count entries of TINYDELTA_RELOC_ENTRY_SIZE bytes */
    uint32_t count;
    const uint8_t *p_runs; /* MSP430X: run_count data runs, 8 bytes each: start and length in 256-byte blocks */
    uint32_t run_count;
    const uint8_t *p_outside; /* MSP430X: outside_count entries as p_entries, keyed by address: RAM that moved */
    uint32_t outside_count;
    uint32_t base;         /* the old image's address on the device */
    uint32_t dict_len;     /* the old image's length */
    const uint8_t *p_dict; /* the old image, or NULL to read it through raw */
    tinydelta_raw_fn_t raw;
    void *p_raw_ctx;
} tinydelta_reloc_t;

/* Reads the map from the patch header (after the features byte) into
 * p_storage, which holds storage_size bytes: TINYDELTA_ERR_VERSION if it
 * doesn't fit, TINYDELTA_ERR_MALFORMED if an entry reaches past 32 bits. */
tinydelta_result_t
tinydelta_reloc_read_map(tinydelta_reloc_t *p_reloc, tinydelta_varint_fn_t varint, void *p_varint_ctx, uint8_t *p_storage, uint32_t storage_size);

/* p_out holds the old image's bytes [offset, offset + length), raw; makes
 * them the relocated image's. */
void tinydelta_reloc_span(const tinydelta_reloc_t *p_reloc, uint32_t offset, uint8_t *p_out, uint32_t length);

#endif
