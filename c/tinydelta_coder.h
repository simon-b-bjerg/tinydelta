/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* Range decoder for coded patches (see README.md, "Coded patches").
 * Internal: used by both decoders when built with TINYDELTA_CODER. */
#ifndef TINYDELTA_CODER_H
#define TINYDELTA_CODER_H

#include <stdint.h>

#include "tinydelta.h"

/* Gets the next byte of the coded stream into *p_byte, or fails. */
typedef tinydelta_result_t (*tinydelta_fetch_fn_t)(void *p_ctx, uint8_t *p_byte);

/* What an instruction-stream byte is: it picks the probability states
 * that code it. */
typedef enum {
    TINYDELTA_FIELD_LITERAL     = 0, /* ADD literal */
    TINYDELTA_FIELD_DIFF        = 1, /* ADD_DIFF byte */
    TINYDELTA_FIELD_DELTA       = 2, /* first byte of a COPY/COPY_TARGET delta */
    TINYDELTA_FIELD_CONTINUED   = 3, /* later byte of any varint */
    TINYDELTA_FIELD_INSTRUCTION = 4  /* first byte of an instruction */
} tinydelta_field_t;

/* The coder's state while a coded patch decodes. */
typedef struct {
    tinydelta_fetch_fn_t fetch;
    void *p_fetch_ctx;
    uint8_t *p_states; /* tinydelta_coder_tables_size(layout) bytes */
    uint32_t range;
    uint32_t code;
    uint8_t layout;
    uint8_t prev_opcode; /* the previous instruction's opcode, 4 before the first */
} tinydelta_coder_t;

/* Bytes of probability states a table layout needs (TINYDELTA_CODED_TABLES_A/B/C),
 * 0 for a layout that doesn't exist. */
uint32_t tinydelta_coder_tables_size(uint8_t layout);

/* Starts decoding: resets the states and reads the first 4 coded bytes. */
tinydelta_result_t tinydelta_coder_init(tinydelta_coder_t *p_coder, uint8_t layout, uint8_t *p_states, tinydelta_fetch_fn_t fetch, void *p_fetch_ctx);

/* Decodes the next instruction-stream byte, a field of the given kind. */
tinydelta_result_t tinydelta_coder_byte(tinydelta_coder_t *p_coder, tinydelta_field_t field, uint8_t *p_byte);

#endif
