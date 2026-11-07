/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* Relocation for coded patches (README.md, "Relocation";
 * docs/relocation-rules.md): rewrites the addresses in old-image bytes the
 * decoders read, through the map the patch carries, finding them with a
 * relocation rule table. Internal: used by both decoders when built with
 * TINYDELTA_RELOC. */
#ifndef TINYDELTA_RELOC_H
#define TINYDELTA_RELOC_H

#include <stdbool.h>
#include <stdint.h>

#include "tinydelta.h"
#include "tinydelta_coder.h"

/* The most raw old-image bytes the engine asks for at once. */
#define TINYDELTA_RELOC_RAW_MAX 20u

/* Points to length raw old-image bytes from index (all within the image;
 * length 1 to TINYDELTA_RELOC_RAW_MAX), valid until the next call. On a read
 * failure it points to any bytes and records the failure for its decoder to
 * report after tinydelta_reloc_span. */
typedef const uint8_t *(*tinydelta_raw_fn_t)(void *p_ctx, uint32_t index, uint32_t length);

/* Reads the next varint of the patch header. */
typedef tinydelta_result_t (*tinydelta_varint_fn_t)(void *p_ctx, uint32_t *p_value);

/* Bytes of RAM per map entry and per RAM range: start, length, shift, each
 * 32 bits (8 per data run: start, length). */
#define TINYDELTA_RELOC_ENTRY_SIZE 12u

/* The rule lists of a table, in order. */
#define TINYDELTA_RULE_LISTS 4u

/* The map, the rule table and how to read the raw old image: from p_dict
 * when it is in memory, else through raw. */
typedef struct {
    const uint8_t *p_entries; /* count entries of TINYDELTA_RELOC_ENTRY_SIZE bytes, by image offset */
    uint32_t count;
    const uint8_t *p_runs; /* run_count data runs, 8 bytes each: start and length in bytes (256-byte blocks in the patch) */
    uint32_t run_count;
    const uint8_t *p_outside; /* outside_count RAM ranges, as entries, by address */
    uint32_t outside_count;
    uint32_t base;         /* the old image's address on the device */
    uint32_t dict_len;     /* the old image's length */
    const uint8_t *p_dict; /* the old image, or NULL to read it through raw */
    tinydelta_raw_fn_t raw;
    void *p_raw_ctx;
    const uint8_t *p_rules;                    /* the rule table */
    uint32_t rules_len;                        /* its length; while reading the header, a shipped table's */
    uint32_t list_start[TINYDELTA_RULE_LISTS]; /* each list's first rule, as an offset in the table */
    uint32_t list_count[TINYDELTA_RULE_LISTS];
    uint32_t unit;       /* bytes between positions: 1, 2 or 4 */
    uint32_t word_bytes; /* 1-4 */
    uint32_t sync;       /* units decoded back to find instruction starts */
    uint32_t reach;      /* bytes a rule reaches past its position, less one */
    bool b_big;          /* words are big-endian */
    bool b_shipped;      /* the table comes in the coded stream */
} tinydelta_reloc_t;

/* Reads a coded patch's relocation header after its features byte: the
 * rule table's CRC (or a shipped table's length), then the map into
 * p_storage, which holds storage_size bytes (a shipped table first).
 * TINYDELTA_ERR_VERSION for a table this decoder doesn't have, or a map
 * that doesn't fit; TINYDELTA_ERR_MALFORMED for an entry past 32 bits.
 * fetch reads raw header bytes, varint header varints. */
tinydelta_result_t tinydelta_reloc_read_header(tinydelta_reloc_t *p_reloc,
                                               uint8_t features,
                                               tinydelta_fetch_fn_t fetch,
                                               tinydelta_varint_fn_t varint,
                                               void *p_ctx,
                                               uint8_t *p_storage,
                                               uint32_t storage_size);

/* After the coder has started: decodes a shipped table from the coded
 * stream into its place, and checks the table the patch uses
 * (TINYDELTA_ERR_MALFORMED if it isn't a valid table). */
tinydelta_result_t tinydelta_reloc_start(tinydelta_reloc_t *p_reloc, tinydelta_coder_t *p_coder);

/* p_out holds the old image's bytes [offset, offset + length), raw; makes
 * them the relocated image's. */
void tinydelta_reloc_span(const tinydelta_reloc_t *p_reloc, uint32_t offset, uint8_t *p_out, uint32_t length);

#endif
