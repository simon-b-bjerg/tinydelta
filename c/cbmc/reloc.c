/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* CBMC harness: the relocation rule engine, for every rule table, old
 * image, map and span up to the bounds below (see scripts/c-cbmc.sh), as a
 * shipped table could be anything. Proves, within the bounds: checking the
 * table never reads past it, and relocating a span of an image with an
 * accepted table reads only the table, the image and the map, writes only
 * the span, and has no undefined behaviour. */
#include <assert.h>
#include <stddef.h>

#include "tinydelta_reloc.h"

#ifndef TABLE_MAX
#define TABLE_MAX 16
#endif
#ifndef IMAGE_MAX
#define IMAGE_MAX 6
#endif

uint32_t nondet_u32(void);
uint8_t nondet_u8(void);

int main(void) {
    static uint8_t table_area[TABLE_MAX], image_area[IMAGE_MAX], out_area[IMAGE_MAX];
    static uint8_t entries[TINYDELTA_RELOC_ENTRY_SIZE], runs[8], outside[TINYDELTA_RELOC_ENTRY_SIZE];
    uint32_t table_len = nondet_u32(), image_len = nondet_u32(), offset = nondet_u32(), length = nondet_u32();
    tinydelta_reloc_t reloc;

    __CPROVER_assume(table_len <= TABLE_MAX && image_len >= 1u && image_len <= IMAGE_MAX);
    __CPROVER_assume(offset < image_len && length >= 1u && length <= image_len - offset);
    for (uint32_t i = 0; i < TABLE_MAX; i++) table_area[i] = nondet_u8();
    for (uint32_t i = 0; i < IMAGE_MAX; i++) image_area[i] = nondet_u8();
    for (uint32_t i = 0; i < TINYDELTA_RELOC_ENTRY_SIZE; i++) {
        entries[i] = nondet_u8();
        outside[i] = nondet_u8();
    }
    for (uint32_t i = 0; i < 8u; i++) runs[i] = nondet_u8();

    /* Each array ends where its contents end, so a byte past them leaves it. */
    reloc.p_rules       = table_area + (TABLE_MAX - table_len);
    reloc.rules_len     = table_len;
    reloc.b_shipped     = false;
    reloc.p_dict        = image_area + (IMAGE_MAX - image_len);
    reloc.dict_len      = image_len;
    reloc.raw           = NULL;
    reloc.p_raw_ctx     = NULL;
    reloc.base          = nondet_u32();
    reloc.p_entries     = entries;
    reloc.count         = nondet_u32() & 1u;
    reloc.p_runs        = runs;
    reloc.run_count     = nondet_u32() & 1u;
    reloc.p_outside     = outside;
    reloc.outside_count = nondet_u32() & 1u;

    if (tinydelta_reloc_start(&reloc, NULL) == TINYDELTA_OK) {
        uint8_t *out = out_area + (IMAGE_MAX - length);

        for (uint32_t i = 0; i < length; i++) out[i] = reloc.p_dict[offset + i];
        tinydelta_reloc_span(&reloc, offset, out, length);
    }
    return 0;
}
