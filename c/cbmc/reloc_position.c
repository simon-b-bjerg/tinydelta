/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* CBMC harness: one step of the relocation engine with a built-in table
 * (TINYDELTA_RELOC 1 or 2), at any position of any image up to IMAGE_MAX
 * bytes, in any span, with any map (up to one entry, data run and RAM
 * range) and the instruction-length cache in any state the engine can
 * leave it in. tinydelta_reloc_span is this step for each position of a
 * span, so no read or write outside the buffers, overflow or undefined
 * shift here means none in any span. DATA_RUNS 0: code everywhere, 1:
 * data everywhere, 2: either (slower). LENGTH_AT 1 checks length_at alone,
 * at any position, so the step may be checked with it replaced by any
 * value (smaller: with sync, starts() decodes up to sync lengths).
 * POSITION fixes p, for runs that are too large with p symbolic (data
 * rules whose unless looks before p): one run per position covers all. */
#include <assert.h>
#include <stddef.h>

#include "tinydelta_reloc.c" /* the engine's static functions and table */

#ifndef IMAGE_MAX
#define IMAGE_MAX 16
#endif
#ifndef DATA_RUNS
#define DATA_RUNS 0
#endif
#ifndef LENGTH_AT
#define LENGTH_AT 0
#endif

uint32_t nondet_u32(void);
uint8_t nondet_u8(void);

int main(void) {
    static uint8_t image_area[IMAGE_MAX], out_area[IMAGE_MAX];
    static uint8_t entries[TINYDELTA_RELOC_ENTRY_SIZE], runs[8], outside[TINYDELTA_RELOC_ENTRY_SIZE];
    uint32_t image_len = nondet_u32(), offset = nondet_u32(), length = nondet_u32(), p = nondet_u32();
    tinydelta_reloc_t reloc;
    sync_t sync;
    span_t span;
    small_t list;
    small_t index;
    const uint8_t *p_rule;

    __CPROVER_assume(image_len >= 1u && image_len <= IMAGE_MAX);
    __CPROVER_assume(offset < image_len && length >= 1u && length <= image_len - offset);
    /* The span's positions: from reach before it to its end. */
    __CPROVER_assume(p < offset + length && p + TINYDELTA_RELOC_RULES_REACH >= offset);
#ifdef POSITION
    __CPROVER_assume(p == POSITION); /* one run per position: everything it decides is then concrete */
#endif
    for (uint32_t i = 0; i < IMAGE_MAX; i++) {
        image_area[i] = nondet_u8();
    }
    for (uint32_t i = 0; i < TINYDELTA_RELOC_ENTRY_SIZE; i++) {
        entries[i] = nondet_u8();
        outside[i] = nondet_u8();
    }
    for (uint32_t i = 0; i < 8u; i++) {
        runs[i] = nondet_u8();
    }
    /* Any cache state: lengths as length_at gives them (at most MAX_SKIP + MAX_LENGTH bytes). */
    sync.next = nondet_u32();
    for (uint32_t i = 0; i < SYNC_SLOTS; i++) {
        sync.units[i] = nondet_u8();
        __CPROVER_assume(sync.units[i] <= MAX_SKIP + MAX_LENGTH);
    }
    /* The data-run cache as the engine starts it: empty, so in_data looks the
     * run up. Any later state is a run or gap of the map, and a hit on it
     * reads nothing; any other state would make in_data's answer arbitrary. */
    sync.data_low  = 0u;
    sync.data_high = 0u;
    sync.b_data    = false;

    reloc.p_rules       = tinydelta_reloc_rules;
    reloc.rules_len     = TINYDELTA_RELOC_RULES_SIZE;
    reloc.b_shipped     = false;
    reloc.p_dict        = image_area + (IMAGE_MAX - image_len); /* ends where the image ends */
    reloc.dict_len      = image_len;
    reloc.raw           = NULL;
    reloc.p_raw_ctx     = NULL;
    reloc.base          = nondet_u32();
    reloc.p_entries     = entries;
    reloc.count         = nondet_u32() & 1u;
    reloc.p_runs        = runs;
    reloc.p_outside     = outside;
    reloc.outside_count = nondet_u32() & 1u;
#if DATA_RUNS == 0
    reloc.run_count = 0u;
#elif DATA_RUNS == 1
    for (uint32_t i = 0; i < 8u; i++) {
        runs[i] = (i == 5u) ? 1u : 0u; /* one run: start 0, one block (256 bytes, as stored) */
    }
    reloc.run_count = 1u;
#else
    reloc.run_count = nondet_u32() & 1u;
#endif

    span.offset = offset;
    span.length = length;
    span.p_out  = out_area + (IMAGE_MAX - length); /* ends where the span ends */

#if LENGTH_AT
    /* An instruction length at any position (the step's proof may replace
     * length_at by any value: --remove-function-body length_at). */
    (void)length_at(&reloc, &sync, p);
    (void)sync;
    (void)span;
    (void)list;
    (void)index;
    (void)p_rule;
#else
    /* The body of tinydelta_reloc_span's loop. */
    p_rule = applies(&reloc, &sync, p, &list, &index);
    if ((p_rule != NULL) && move_at(&reloc, p_rule, p, NULL) && !excluded(&reloc, &sync, p_rule, list, p)) {
        (void)move_at(&reloc, p_rule, p, &span);
    }
#endif
    return 0;
}
