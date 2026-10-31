/* Wire-format details shared by the decoders (see README.md).
 * Internal: not needed to use the library. */
#ifndef TINYDELTA_INTERNAL_H
#define TINYDELTA_INTERNAL_H

#include <stdint.h>

#include "tinydelta.h"

/* ---- Header ------------------------------------------------------------
 * One byte, the format version, before the first instruction.
 */

#define TINYDELTA_HEADER_LEN 1u

/* ---- Instructions -------------------------------------------------------
 * Every instruction starts with one varint: the opcode in its low bits, the
 * number of target bytes the instruction produces (its length) above them.
 */

typedef enum {
    TINYDELTA_OP_COPY        = 0, /* dictionary at cursor + delta; the cursor
                                     moves to just past the copied bytes */
    TINYDELTA_OP_ADD         = 1, /* literal bytes, taken from the patch */
    TINYDELTA_OP_SKIP        = 2, /* dictionary at the cursor */
    TINYDELTA_OP_COPY_TARGET = 3  /* version 1: earlier target bytes, at
                                     position + delta; version 2: ADD_DIFF,
                                     patch bytes added to the dictionary
                                     bytes at the cursor */
} tinydelta_opcode;

#define TINYDELTA_OPCODE_BITS 2u
#define TINYDELTA_OPCODE_MASK ((1u << TINYDELTA_OPCODE_BITS) - 1u)

static inline tinydelta_opcode instruction_opcode(uint32_t instruction) {
    return (tinydelta_opcode)(instruction & TINYDELTA_OPCODE_MASK);
}

static inline uint32_t instruction_length(uint32_t instruction) {
    return instruction >> TINYDELTA_OPCODE_BITS;
}

/* ---- Varints ------------------------------------------------------------
 * LEB128: 7 value bits per byte, least significant group first, top bit set
 * on every byte except the last. At most 32 bits, so at most 5 bytes, and
 * the 5th byte may only carry bits 28-31.
 */

#define VARINT_MAX_BYTES      5u
#define VARINT_BITS_PER_BYTE  7u
#define VARINT_VALUE_MASK     0x7Fu
#define VARINT_CONTINUES      0x80u
#define VARINT_LAST_BYTE_MAX  0x0Fu

/* Parses one varint from the start of bytes[0 .. available). On success
 * sets *value and *used (the bytes it took). Fails if the varint is
 * truncated or holds more than 32 bits. */
static inline tinydelta_result varint_parse(const uint8_t *bytes, uint32_t available,
                                            uint32_t *value, uint32_t *used) {
    uint32_t result = 0;
    uint32_t limit = available < VARINT_MAX_BYTES ? available : VARINT_MAX_BYTES;

    for (uint32_t i = 0; i < limit; i++) {
        uint8_t byte = bytes[i];
        if (i == VARINT_MAX_BYTES - 1 && byte > VARINT_LAST_BYTE_MAX) {
            return TINYDELTA_ERR_MALFORMED; /* more than 32 bits */
        }
        result |= (uint32_t)(byte & VARINT_VALUE_MASK) << (VARINT_BITS_PER_BYTE * i);
        if (!(byte & VARINT_CONTINUES)) {
            *value = result;
            *used = i + 1;
            return TINYDELTA_OK;
        }
    }
    return TINYDELTA_ERR_MALFORMED; /* truncated */
}

/* ---- Deltas -------------------------------------------------------------
 * COPY and COPY_TARGET deltas are zigzag-encoded signed values
 * (0, -1, 1, -2, ... -> 0, 1, 2, 3, ...). The result is kept as uint32_t
 * and added with wrap-around: a negative delta that reaches below 0 wraps
 * to 2^31 or more, which no valid position (below 2^30) can equal, so one
 * unsigned bounds check rejects it.
 */
static inline uint32_t zigzag_decode(uint32_t encoded) {
    return (encoded >> 1) ^ (0u - (encoded & 1u));
}

/* True if [start, start + length) lies within [0, size). */
static inline int range_within(uint32_t start, uint32_t length, uint32_t size) {
    return start <= size && length <= size - start;
}

#endif
