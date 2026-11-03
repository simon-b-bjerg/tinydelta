/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* CBMC harness: one tinydelta_coder_byte from any coder state, as corrupt
 * patches can reach: any range at or above the normalizing limit, any code
 * and probability states, any field and table layout, any input bytes.
 * Proves, for that step: no out-of-bounds access, no undefined behaviour,
 * termination, and the range still normalized, so the proof carries over
 * to the next step, and so to a whole patch. */
#include <assert.h>
#include <stddef.h>

#include "tinydelta_coder.h"

#define CODER_TOP 0x01000000u

uint8_t nondet_u8(void);
uint32_t nondet_u32(void);

static tinydelta_result_t fetch_any(void *p_ctx, uint8_t *p_byte) {
    (void)p_ctx;
    *p_byte = nondet_u8();
    return TINYDELTA_OK;
}

int main(void) {
    static uint8_t states[TINYDELTA_CODED_TABLES_A];
    tinydelta_coder_t coder;
    uint8_t byte;
    uint32_t field = nondet_u32();

    coder.layout = nondet_u8();
    __CPROVER_assume(tinydelta_coder_tables_size(coder.layout) != 0u);
    __CPROVER_assume(field <= (uint32_t)TINYDELTA_FIELD_INSTRUCTION);
    coder.prev_opcode = nondet_u8();
    __CPROVER_assume(coder.prev_opcode <= 4u);
    coder.range = nondet_u32();
    coder.code  = nondet_u32();
    __CPROVER_assume(coder.range >= CODER_TOP);
    coder.fetch       = fetch_any;
    coder.p_fetch_ctx = NULL;
    coder.p_states    = states;
    for (uint32_t index = 0u; index < TINYDELTA_CODED_TABLES_A; index++) {
        states[index] = nondet_u8();
        __CPROVER_assume((states[index] >> 1) < 63u); /* the coder writes no others */
    }

    tinydelta_result_t result = tinydelta_coder_byte(&coder, (tinydelta_field_t)field, &byte);

    assert(result == TINYDELTA_OK);
    assert(coder.range >= CODER_TOP);
    assert(coder.prev_opcode <= 4u);
    return 0;
}
