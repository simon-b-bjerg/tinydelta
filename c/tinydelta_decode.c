/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

#include <string.h>

#include "tinydelta.h"
#include "tinydelta_coder.h"
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
#if TINYDELTA_CODER
    bool b_coded;            /* instruction-stream bytes come from the coder */
    uint32_t stream_left;    /* coded: instruction-stream bytes still to come */
    tinydelta_coder_t coder; /* coded: the range decoder, reading patch bytes */
#endif
} memory_decoder_t;

/* ---- Patch bytes ------------------------------------------------------------
 * Every instruction-stream byte comes through memory_byte: the next patch
 * byte, or for a coded patch the next byte the coder decodes from them. */

/* The next patch byte as stored (also the coder's source). */
static tinydelta_result_t memory_raw_byte(void *p_ctx, uint8_t *p_byte) {
    memory_decoder_t *p_decoder = (memory_decoder_t *)p_ctx; /* Cast: the context is always the decoder. */

    if (p_decoder->patch_pos >= p_decoder->patch_len) {
        return TINYDELTA_ERR_MALFORMED;
    }
    *p_byte = p_decoder->p_patch[p_decoder->patch_pos];
    p_decoder->patch_pos++;
    return TINYDELTA_OK;
}

/* The next instruction-stream byte, a field of the given kind. */
static tinydelta_result_t memory_byte(memory_decoder_t *p_decoder, tinydelta_field_t field, uint8_t *p_byte) {
#if TINYDELTA_CODER
    if (p_decoder->b_coded) {
        if (p_decoder->stream_left == 0u) {
            return TINYDELTA_ERR_MALFORMED;
        }
        p_decoder->stream_left--;
        return tinydelta_coder_byte(&p_decoder->coder, field, p_byte);
    }
#else
    (void)field;
#endif
    return memory_raw_byte(p_decoder, p_byte);
}

/* Reads a varint; first is its first byte's kind. */
static tinydelta_result_t memory_read_varint(memory_decoder_t *p_decoder, tinydelta_field_t first, uint32_t *p_value) {
    tinydelta_field_t field = first;
    uint32_t value          = 0u;

    for (uint32_t shift = 0u; shift < (VARINT_MAX_BYTES * VARINT_BITS_PER_BYTE); shift += VARINT_BITS_PER_BYTE) {
        uint8_t byte;
        tinydelta_result_t result = memory_byte(p_decoder, field, &byte);

        if (result != TINYDELTA_OK) {
            return result;
        }
        if ((shift == ((VARINT_MAX_BYTES - 1u) * VARINT_BITS_PER_BYTE)) && (byte > VARINT_LAST_BYTE_MAX)) {
            return TINYDELTA_ERR_MALFORMED; /* more than 32 bits */
        }
        /* Cast: widens the byte, so the shift (at most 28) stays in 32 bits. */
        value |= ((uint32_t)byte & VARINT_VALUE_MASK) << shift;
        if ((byte & VARINT_CONTINUES) == 0u) {
            *p_value = value;
            return TINYDELTA_OK;
        }
        field = TINYDELTA_FIELD_CONTINUED;
    }
    return TINYDELTA_ERR_MALFORMED; /* unreachable: a 5th byte with more to come exceeds 0x0F */
}

/* Each instruction below checks its length against its source before
 * copying, so the length also fits in size_t (16 bits on some targets,
 * e.g. MSP430) when it reaches memcpy. */

/* COPY: dictionary bytes at cursor + delta; the cursor moves there. */
static tinydelta_result_t memory_decode_copy(memory_decoder_t *p_decoder, uint32_t length) {
    uint32_t delta;
    uint32_t address;
    tinydelta_result_t result = memory_read_varint(p_decoder, TINYDELTA_FIELD_DELTA, &delta);

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
    for (uint32_t offset = 0u; offset < length; offset++) {
        tinydelta_result_t result = memory_byte(p_decoder, TINYDELTA_FIELD_LITERAL, &p_decoder->p_target[p_decoder->target_pos + offset]);

        if (result != TINYDELTA_OK) {
            return result;
        }
    }
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
    tinydelta_result_t result = memory_read_varint(p_decoder, TINYDELTA_FIELD_DELTA, &delta);

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
    if (!range_within(p_decoder->cursor, length, p_decoder->dict_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    for (uint32_t offset = 0u; offset < length; offset++) {
        uint8_t diff;
        tinydelta_result_t result = memory_byte(p_decoder, TINYDELTA_FIELD_DIFF, &diff);

        if (result != TINYDELTA_OK) {
            return result;
        }
        /* Cast: the sum wraps modulo 256, as the format defines. */
        p_decoder->p_target[p_decoder->target_pos + offset] = (uint8_t)(p_decoder->p_dict[p_decoder->cursor + offset] + diff);
    }
    return TINYDELTA_OK;
}

#if TINYDELTA_CODER
/* Reads a coded patch's header (the first byte's flag already seen) and
 * starts the coder: *p_version becomes the instruction set's version. */
static tinydelta_result_t memory_start_coded(memory_decoder_t *p_decoder, uint8_t *p_tables, uint32_t tables_size, uint8_t *p_version) {
    uint8_t features;
    uint8_t layout;
    uint32_t needed;
    tinydelta_result_t result = memory_raw_byte(p_decoder, &features);

    if (result != TINYDELTA_OK) {
        return result;
    }
    layout = features & TINYDELTA_CODED_LAYOUT_MASK;
    needed = tinydelta_coder_tables_size(layout);
    if (((features & ~TINYDELTA_CODED_LAYOUT_MASK) != 0u) || (needed == 0u) || (p_tables == NULL) || (tables_size < needed)) {
        return TINYDELTA_ERR_VERSION; /* a feature this build or buffer lacks */
    }
    result = memory_read_varint(p_decoder, TINYDELTA_FIELD_CONTINUED, &p_decoder->stream_left); /* stored as is: not coded yet */
    if (result != TINYDELTA_OK) {
        return result;
    }
    p_decoder->b_coded = true;
    *p_version &= (uint8_t)~TINYDELTA_CODED_FLAG; /* Cast: the flag fits a byte. */
    return tinydelta_coder_init(&p_decoder->coder, layout, p_tables, memory_raw_byte, p_decoder);
}
#endif

/* Whether instructions remain. */
static bool memory_more(const memory_decoder_t *p_decoder) {
#if TINYDELTA_CODER
    if (p_decoder->b_coded) {
        return p_decoder->stream_left > 0u;
    }
#endif
    return p_decoder->patch_pos < p_decoder->patch_len;
}

/* Both entry points: p_tables is NULL for tinydelta_decode. */
static tinydelta_result_t memory_decode(const uint8_t *p_dict,
                                        uint32_t dict_len,
                                        const uint8_t *p_patch,
                                        uint32_t patch_len,
                                        uint8_t *p_target,
                                        uint32_t target_cap,
                                        uint8_t *p_tables,
                                        uint32_t tables_size,
                                        uint32_t *p_target_len) {
    memory_decoder_t decoder;
    uint8_t version;
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

    decoder.p_dict     = p_dict;
    decoder.dict_len   = dict_len;
    decoder.p_patch    = p_patch;
    decoder.patch_len  = patch_len;
    decoder.p_target   = p_target;
    decoder.patch_pos  = TINYDELTA_HEADER_LEN;
    decoder.target_pos = 0u;
    decoder.cursor     = 0u;
    version            = p_patch[0];
#if TINYDELTA_CODER
    decoder.b_coded = false;
    if ((version == (TINYDELTA_CODED_FLAG | TINYDELTA_FORMAT_VERSION)) || (version == (TINYDELTA_CODED_FLAG | TINYDELTA_FORMAT_VERSION_2))) {
        tinydelta_result_t result = memory_start_coded(&decoder, p_tables, tables_size, &version);

        if (result != TINYDELTA_OK) {
            return result;
        }
    }
#else
    (void)p_tables;
    (void)tables_size;
#endif
    b_format_v2 = (version == TINYDELTA_FORMAT_VERSION_2);
    if ((version != TINYDELTA_FORMAT_VERSION) && !b_format_v2) {
        return TINYDELTA_ERR_VERSION;
    }

    while (memory_more(&decoder)) {
        uint32_t instruction;
        uint32_t length;
        tinydelta_result_t result = memory_read_varint(&decoder, TINYDELTA_FIELD_INSTRUCTION, &instruction);

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

tinydelta_result_t tinydelta_decode(
    const uint8_t *p_dict, uint32_t dict_len, const uint8_t *p_patch, uint32_t patch_len, uint8_t *p_target, uint32_t target_cap, uint32_t *p_target_len) {
    return memory_decode(p_dict, dict_len, p_patch, patch_len, p_target, target_cap, NULL, 0u, p_target_len);
}

#if TINYDELTA_CODER
tinydelta_result_t tinydelta_decode_coded(const uint8_t *p_dict,
                                          uint32_t dict_len,
                                          const uint8_t *p_patch,
                                          uint32_t patch_len,
                                          uint8_t *p_target,
                                          uint32_t target_cap,
                                          uint8_t *p_tables,
                                          uint32_t tables_size,
                                          uint32_t *p_target_len) {
    return memory_decode(p_dict, dict_len, p_patch, patch_len, p_target, target_cap, p_tables, tables_size, p_target_len);
}
#endif
