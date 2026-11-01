/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

#include "tinydelta_coder.h"

#include "tinydelta_internal.h"

#if TINYDELTA_CODER

/* A binary range decoder (LZMA's) with 12-bit probabilities, whose
 * probabilities are 1-byte states as in H.264's CABAC: state number * 2,
 * plus the more likely bit. The encoder (coded.go) mirrors it bit for bit. */

#define CODER_PROB_BITS  12u
#define CODER_PROB_ONE   4096u       /* 1 << CODER_PROB_BITS */
#define CODER_TOP        0x01000000u /* normalize below this range */
#define CODER_STATES     63u
#define CODER_OP_STATES  15u        /* 5 previous opcodes x 3 tree nodes */
#define CODER_LEN_STATES (4u * 63u) /* 4 opcodes x 63 tree nodes */
#define CODER_TREE       255u
#define CODER_PREV_START 4u /* "previous opcode" before the first instruction */

#define CODER_LAYOUT_C 1u /* one shared byte table */
#define CODER_LAYOUT_B 2u /* ADD_DIFF bytes, and one for the rest */
#define CODER_LAYOUT_A 3u /* one table per field */

uint32_t tinydelta_coder_tables_size(uint8_t layout) {
    uint32_t size = 0u;

    if (layout == CODER_LAYOUT_A) {
        size = TINYDELTA_CODED_TABLES_A;
    } else if (layout == CODER_LAYOUT_B) {
        size = TINYDELTA_CODED_TABLES_B;
    } else if (layout == CODER_LAYOUT_C) {
        size = TINYDELTA_CODED_TABLES_C;
    } else {
        size = 0u; /* no such layout */
    }
    return size;
}

tinydelta_result_t tinydelta_coder_init(tinydelta_coder_t *p_coder, uint8_t layout, uint8_t *p_states, tinydelta_fetch_fn_t fetch, void *p_fetch_ctx) {
    uint32_t size = tinydelta_coder_tables_size(layout);

    p_coder->fetch       = fetch;
    p_coder->p_fetch_ctx = p_fetch_ctx;
    p_coder->p_states    = p_states;
    p_coder->layout      = layout;
    p_coder->prev_opcode = CODER_PREV_START;
    p_coder->range       = 0xFFFFFFFFu;
    p_coder->code        = 0u;
    for (uint32_t index = 0u; index < size; index++) {
        p_states[index] = 0u; /* state 0, 0 more likely: even odds */
    }
    for (uint32_t index = 0u; index < 4u; index++) {
        uint8_t byte;
        tinydelta_result_t result = fetch(p_fetch_ctx, &byte);

        if (result != TINYDELTA_OK) {
            return result;
        }
        p_coder->code = (p_coder->code << 8) | byte;
    }
    return TINYDELTA_OK;
}

/* Decodes one bit with the state at p_states[state_index] and updates it. */
static tinydelta_result_t coder_bit(tinydelta_coder_t *p_coder, uint32_t state_index, uint32_t *p_bit) {
    /* Each state's probability of the less likely bit, out of CODER_PROB_ONE. */
    static const uint16_t lps_table[CODER_STATES] = {
        2048u, 1944u, 1845u, 1752u, 1663u, 1578u, 1498u, 1422u, 1350u, 1281u, 1216u, 1154u, 1096u, 1040u, 987u, 937u, 890u, 844u, 802u, 761u, 722u,
        685u,  651u,  618u,  586u,  557u,  528u,  501u,  476u,  452u,  429u,  407u,  386u,  367u,  348u,  330u, 314u, 298u, 283u, 268u, 255u, 242u,
        229u,  218u,  207u,  196u,  186u,  177u,  168u,  159u,  151u,  144u,  136u,  129u,  123u,  117u,  111u, 105u, 100u, 95u,  90u,  85u,  81u,
    };

    /* The state after the less likely bit; after the more likely one, state s
     * becomes s + 1, at most CODER_STATES - 1. */
    static const uint8_t next_lps_table[CODER_STATES] = {
        0u,  0u,  1u,  2u,  3u,  4u,  4u,  5u,  6u,  7u,  8u,  9u,  10u, 10u, 11u, 12u, 13u, 14u, 14u, 15u, 16u,
        17u, 17u, 18u, 19u, 20u, 20u, 21u, 22u, 22u, 23u, 24u, 24u, 25u, 26u, 26u, 27u, 27u, 28u, 29u, 29u, 30u,
        30u, 31u, 31u, 32u, 32u, 33u, 33u, 33u, 34u, 34u, 35u, 35u, 35u, 36u, 36u, 36u, 37u, 37u, 37u, 38u, 38u,
    };
    uint8_t state   = p_coder->p_states[state_index];
    uint32_t number = (uint32_t)state >> 1; /* Cast: widens the state byte. */
    uint32_t mps    = (uint32_t)state & 1u; /* Cast: widens the state byte. */
    uint32_t lps    = lps_table[number];
    uint32_t p_zero = (mps == 0u) ? (CODER_PROB_ONE - lps) : lps;
    uint32_t bound  = (p_coder->range >> CODER_PROB_BITS) * p_zero;
    uint32_t bit    = 0u;

    if (p_coder->code < bound) {
        p_coder->range = bound;
    } else {
        p_coder->code -= bound;
        p_coder->range -= bound;
        bit = 1u;
    }
    if (bit == mps) {
        number = (number < (CODER_STATES - 1u)) ? (number + 1u) : number;
    } else {
        if (number == 0u) {
            mps ^= 1u;
        }
        number = next_lps_table[number];
    }
    /* Cast: number is below 64, so the state fits a byte. */
    p_coder->p_states[state_index] = (uint8_t)((number * 2u) + mps);
    while (p_coder->range < CODER_TOP) {
        uint8_t byte;
        tinydelta_result_t result = p_coder->fetch(p_coder->p_fetch_ctx, &byte);

        if (result != TINYDELTA_OK) {
            return result;
        }
        p_coder->range <<= 8;
        p_coder->code = (p_coder->code << 8) | byte;
    }
    *p_bit = bit;
    return TINYDELTA_OK;
}

/* Decodes the bits from_bit down to to_bit of a byte through a bit tree
 * whose node 1 is p_states[base], adding them to *p_value. */
static tinydelta_result_t coder_tree(tinydelta_coder_t *p_coder, uint32_t base, uint32_t from_bit, uint32_t to_bit, uint32_t *p_value) {
    uint32_t node = 1u;

    for (uint32_t bit_index = from_bit + 1u; bit_index > to_bit; bit_index--) {
        uint32_t bit;
        tinydelta_result_t result = coder_bit(p_coder, base + node - 1u, &bit);

        if (result != TINYDELTA_OK) {
            return result;
        }
        *p_value |= bit << (bit_index - 1u);
        node = (node * 2u) + bit;
    }
    return TINYDELTA_OK;
}

/* The byte table a field uses in the coder's layout. */
static uint32_t coder_table(uint8_t layout, tinydelta_field_t field) {
    uint32_t table = 0u;

    if (layout == CODER_LAYOUT_A) {
        table = (uint32_t)field; /* Cast: the enumerators 0-3 are the table numbers. */
    } else if ((layout == CODER_LAYOUT_B) && (field == TINYDELTA_FIELD_DIFF)) {
        table = 1u;
    } else {
        table = 0u;
    }
    return table;
}

tinydelta_result_t tinydelta_coder_byte(tinydelta_coder_t *p_coder, tinydelta_field_t field, uint8_t *p_byte) {
    uint32_t value = 0u;
    tinydelta_result_t result;

    if (field == TINYDELTA_FIELD_INSTRUCTION) {
        /* The opcode bits given the previous opcode, then the six other
         * bits given this opcode. */
        uint32_t opcode;

        result = coder_tree(p_coder, (uint32_t)p_coder->prev_opcode * 3u, 1u, 0u, &value); /* Cast: widens. */
        if (result != TINYDELTA_OK) {
            return result;
        }
        opcode               = value;
        result               = coder_tree(p_coder, CODER_OP_STATES + (opcode * 63u), 7u, 2u, &value);
        /* Cast: opcode is 0-3. */
        p_coder->prev_opcode = (uint8_t)opcode;
    } else {
        result = coder_tree(p_coder, CODER_OP_STATES + CODER_LEN_STATES + (coder_table(p_coder->layout, field) * CODER_TREE), 7u, 0u, &value);
    }
    /* Cast: the tree decodes at most 8 bits. */
    *p_byte = (uint8_t)value;
    return result;
}

#endif
