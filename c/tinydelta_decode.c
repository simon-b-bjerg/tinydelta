/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

#include <string.h>

#include "tinydelta.h"
#include "tinydelta_internal.h"

/* The memory decoder's state while it runs. */
typedef struct {
    const uint8_t *p_dict;
    uint32_t dict_len;
    const uint8_t *p_patch;
    uint32_t patch_len;
    uint8_t *p_target;
    uint32_t patch_pos;  /* next patch byte to read */
    uint32_t target_pos; /* target bytes produced so far */
    uint32_t cursor;     /* dictionary read position */
} memory_decoder_t;

/* Reads the varint at the current patch position. */
static tinydelta_result_t memory_read_varint(memory_decoder_t *p_decoder, uint32_t *p_value) {
    uint32_t used;

    if (varint_parse(p_decoder->p_patch + p_decoder->patch_pos, p_decoder->patch_len - p_decoder->patch_pos, p_value, &used) != TINYDELTA_OK) {
        return TINYDELTA_ERR_MALFORMED;
    }
    p_decoder->patch_pos += used;
    return TINYDELTA_OK;
}

/* Each instruction below checks its length against its source before
 * copying, so the length also fits in size_t (16 bits on some targets,
 * e.g. MSP430) when it reaches memcpy. */

/* COPY: dictionary bytes at cursor + delta; the cursor moves there. */
static tinydelta_result_t memory_decode_copy(memory_decoder_t *p_decoder, uint32_t length) {
    uint32_t delta;
    uint32_t address;
    tinydelta_result_t result = memory_read_varint(p_decoder, &delta);

    if (result != TINYDELTA_OK) {
        return result;
    }
    address = p_decoder->cursor + zigzag_decode(delta);
    if (!range_within(address, length, p_decoder->dict_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    /* Cast: length is within the dictionary, so it fits in size_t. */
    (void)memcpy(p_decoder->p_target + p_decoder->target_pos, p_decoder->p_dict + address, (size_t)length);
    p_decoder->cursor = address;
    return TINYDELTA_OK;
}

/* ADD: literal bytes, taken from the patch. */
static tinydelta_result_t memory_decode_add(memory_decoder_t *p_decoder, uint32_t length) {
    if (!range_within(p_decoder->patch_pos, length, p_decoder->patch_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    /* Cast: length is within the patch, so it fits in size_t. */
    (void)memcpy(p_decoder->p_target + p_decoder->target_pos, p_decoder->p_patch + p_decoder->patch_pos, (size_t)length);
    p_decoder->patch_pos += length;
    return TINYDELTA_OK;
}

/* SKIP: dictionary bytes at the cursor. */
static tinydelta_result_t memory_decode_skip(memory_decoder_t *p_decoder, uint32_t length) {
    if (!range_within(p_decoder->cursor, length, p_decoder->dict_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    /* Cast: length is within the dictionary, so it fits in size_t. */
    (void)memcpy(p_decoder->p_target + p_decoder->target_pos, p_decoder->p_dict + p_decoder->cursor, (size_t)length);
    return TINYDELTA_OK;
}

/* COPY_TARGET (version 1): earlier target bytes at target position +
 * delta, copied byte by byte, so the source may overlap the bytes being
 * written (a delta of -1 repeats the previous byte). */
static tinydelta_result_t memory_decode_copy_target(memory_decoder_t *p_decoder, uint32_t length) {
    uint32_t delta;
    uint32_t address;
    tinydelta_result_t result = memory_read_varint(p_decoder, &delta);

    if (result != TINYDELTA_OK) {
        return result;
    }
    address = p_decoder->target_pos + zigzag_decode(delta);
    if (address >= p_decoder->target_pos) {
        return TINYDELTA_ERR_MALFORMED; /* must be earlier output */
    }
    for (uint32_t offset = 0u; offset < length; offset++) {
        p_decoder->p_target[p_decoder->target_pos + offset] = p_decoder->p_target[address + offset];
    }
    return TINYDELTA_OK;
}

/* ADD_DIFF (version 2): dictionary bytes at the cursor, each plus the
 * next patch byte. */
static tinydelta_result_t memory_decode_add_diff(memory_decoder_t *p_decoder, uint32_t length) {
    if (!range_within(p_decoder->cursor, length, p_decoder->dict_len) || !range_within(p_decoder->patch_pos, length, p_decoder->patch_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    for (uint32_t offset = 0u; offset < length; offset++) {
        /* Cast: the sum wraps modulo 256, as the format defines. */
        p_decoder->p_target[p_decoder->target_pos + offset] =
            (uint8_t)(p_decoder->p_dict[p_decoder->cursor + offset] + p_decoder->p_patch[p_decoder->patch_pos + offset]);
    }
    p_decoder->patch_pos += length;
    return TINYDELTA_OK;
}

tinydelta_result_t tinydelta_decode(
    const uint8_t *p_dict, uint32_t dict_len, const uint8_t *p_patch, uint32_t patch_len, uint8_t *p_target, uint32_t target_cap, uint32_t *p_target_len) {
    memory_decoder_t decoder;
    bool b_format_v2;

    if (dict_len > TINYDELTA_MAX_SIZE) {
        return TINYDELTA_ERR_TOO_LARGE;
    }
    if (target_cap > TINYDELTA_MAX_SIZE) {
        target_cap = TINYDELTA_MAX_SIZE;
    }
    if (patch_len < TINYDELTA_HEADER_LEN) {
        return TINYDELTA_ERR_MALFORMED;
    }
    b_format_v2 = (p_patch[0] == TINYDELTA_FORMAT_VERSION_2);
    if ((p_patch[0] != TINYDELTA_FORMAT_VERSION) && !b_format_v2) {
        return TINYDELTA_ERR_VERSION;
    }

    decoder.p_dict     = p_dict;
    decoder.dict_len   = dict_len;
    decoder.p_patch    = p_patch;
    decoder.patch_len  = patch_len;
    decoder.p_target   = p_target;
    decoder.patch_pos  = TINYDELTA_HEADER_LEN;
    decoder.target_pos = 0u;
    decoder.cursor     = 0u;

    while (decoder.patch_pos < patch_len) {
        uint32_t instruction;
        uint32_t length;
        tinydelta_result_t result = memory_read_varint(&decoder, &instruction);

        if (result != TINYDELTA_OK) {
            return result;
        }
        length = instruction_length(instruction);
        if (length > (target_cap - decoder.target_pos)) {
            return TINYDELTA_ERR_TOO_LARGE;
        }

        switch (instruction_opcode(instruction, b_format_v2)) {
        case TINYDELTA_OP_COPY:
            result = memory_decode_copy(&decoder, length);
            break;
        case TINYDELTA_OP_ADD:
            result = memory_decode_add(&decoder, length);
            break;
        case TINYDELTA_OP_SKIP:
            result = memory_decode_skip(&decoder, length);
            break;
        case TINYDELTA_OP_COPY_TARGET:
            result = memory_decode_copy_target(&decoder, length);
            break;
        case TINYDELTA_OP_ADD_DIFF:
            result = memory_decode_add_diff(&decoder, length);
            break;
        default: /* unreachable: every opcode has a case */
            result = TINYDELTA_ERR_MALFORMED;
            break;
        }
        if (result != TINYDELTA_OK) {
            return result;
        }
        decoder.target_pos += length;
        decoder.cursor += length;
    }

    *p_target_len = decoder.target_pos;
    return TINYDELTA_OK;
}
