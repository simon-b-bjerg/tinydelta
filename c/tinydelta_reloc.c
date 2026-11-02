/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* Relocation (see tinydelta_reloc.h and README.md, "Relocation"). Which
 * bytes are addresses depends only on the raw old image, a few bytes around
 * each position, so any span of the image relocates the same whichever
 * order the decoder reads it in. The rules match the Go encoder's
 * (reloc.go). */
#include "tinydelta_reloc.h"
#include "tinydelta_internal.h"

#if TINYDELTA_CODER && (TINYDELTA_RELOC != 0)

/* A map entry as stored, byte by byte (the tables buffer has no
 * alignment): start, length, shift (two's complement), little-endian. */
#define ENTRY_START  0u
#define ENTRY_LENGTH 1u
#define ENTRY_SHIFT  2u

tinydelta_result_t
tinydelta_reloc_read_map(tinydelta_reloc_t *p_reloc, tinydelta_varint_fn_t varint, void *p_varint_ctx, uint8_t *p_storage, uint32_t storage_size) {
    uint32_t end              = 0u;
    tinydelta_result_t result = varint(p_varint_ctx, &p_reloc->base);

    if (result == TINYDELTA_OK) {
        result = varint(p_varint_ctx, &p_reloc->count);
    }
    if (result != TINYDELTA_OK) {
        return result;
    }
    if (p_reloc->count > (storage_size / TINYDELTA_RELOC_ENTRY_SIZE)) {
        return TINYDELTA_ERR_VERSION; /* the map doesn't fit the tables buffer */
    }
    p_reloc->p_entries = p_storage;
    for (uint32_t index = 0u; index < p_reloc->count; index++) {
        uint32_t entry[3]; /* gap, length, zigzag shift as read */

        for (uint32_t field = 0u; field < 3u; field++) {
            result = varint(p_varint_ctx, &entry[field]);
            if (result != TINYDELTA_OK) {
                return result;
            }
        }
        if (!range_within(entry[ENTRY_START], entry[ENTRY_LENGTH], 0xFFFFFFFFu - end)) {
            return TINYDELTA_ERR_MALFORMED; /* past 32 bits */
        }
        entry[ENTRY_START] += end;
        entry[ENTRY_SHIFT] = zigzag_decode(entry[ENTRY_SHIFT]);
        end                = entry[ENTRY_START] + entry[ENTRY_LENGTH];
        for (uint32_t byte_index = 0u; byte_index < TINYDELTA_RELOC_ENTRY_SIZE; byte_index++) {
            /* Cast: keeps one byte of the field. */
            p_storage[byte_index] = (uint8_t)(entry[byte_index / 4u] >> ((byte_index % 4u) * 8u));
        }
        p_storage += TINYDELTA_RELOC_ENTRY_SIZE;
    }
    return TINYDELTA_OK;
}

/* The shift of old-image offset, 0 outside every entry (two's complement). */
static uint32_t shift_at(const tinydelta_reloc_t *p_reloc, uint32_t offset) {
    const uint8_t *p_entry = p_reloc->p_entries;

    for (uint32_t index = 0u; index < p_reloc->count; index++) {
        uint32_t entry[3] = {0u, 0u, 0u};

        for (uint32_t byte_index = 0u; byte_index < TINYDELTA_RELOC_ENTRY_SIZE; byte_index++) {
            entry[byte_index / 4u] |= (uint32_t)p_entry[byte_index] << ((byte_index % 4u) * 8u); /* Cast: widens. */
        }
        p_entry += TINYDELTA_RELOC_ENTRY_SIZE;
        if (entry[ENTRY_START] > offset) {
            break; /* entries are in order */
        }
        if ((offset - entry[ENTRY_START]) < entry[ENTRY_LENGTH]) {
            return entry[ENTRY_SHIFT];
        }
    }
    return 0u;
}

/* The shift of an address, if it points into the old image; else 0. */
static uint32_t address_shift(const tinydelta_reloc_t *p_reloc, uint32_t address) {
    uint32_t shift = 0u;

    if ((address >= p_reloc->base) && ((address - p_reloc->base) < p_reloc->dict_len)) {
        shift = shift_at(p_reloc, address - p_reloc->base);
    }
    return shift;
}

/* Stores the count relocated bytes of an instruction or pointer at index
 * where they fall in the span [offset, offset + length). */
static void put(uint32_t offset, uint8_t *p_out, uint32_t length, uint32_t index, const uint8_t *p_bytes, uint32_t count) {
    for (uint32_t byte_index = 0u; byte_index < count; byte_index++) {
        uint32_t position = index + byte_index;

        if ((position >= offset) && ((position - offset) < length)) {
            p_out[position - offset] = p_bytes[byte_index];
        }
    }
}

/* Stores value's count low bytes, little-endian, into p_bytes. */
static void le_bytes(uint8_t *p_bytes, uint32_t value, uint32_t count) {
    for (uint32_t byte_index = 0u; byte_index < count; byte_index++) {
        p_bytes[byte_index] = (uint8_t)(value >> (byte_index * 8u)); /* Cast: keeps one byte. */
    }
}

/* Reads count raw bytes at index (within the image), little-endian. */
static tinydelta_result_t raw_le(const tinydelta_reloc_t *p_reloc, uint32_t index, uint32_t count, uint32_t *p_value) {
    uint32_t value = 0u;

    for (uint32_t byte_index = count; byte_index > 0u; byte_index--) {
        uint8_t byte;

        if (p_reloc->p_dict != NULL) {
            byte = p_reloc->p_dict[index + byte_index - 1u];
        } else {
            tinydelta_result_t result = p_reloc->raw(p_reloc->p_raw_ctx, index + byte_index - 1u, &byte);

            if (result != TINYDELTA_OK) {
                return result;
            }
        }
        value = (value << 8) | byte;
    }
    *p_value = value;
    return TINYDELTA_OK;
}

#if TINYDELTA_RELOC == TINYDELTA_RELOC_THUMB

/* Whether a BL starts at h: a halfword 0xF000-0xF7FF, then one
 * 0xF800-0xFFFF (BLs can't overlap: the ranges are disjoint). */
static tinydelta_result_t thumb_bl(const tinydelta_reloc_t *p_reloc, uint32_t h, bool *p_is_bl, uint32_t *p_value) {
    *p_is_bl = false;
    if (!range_within(h, 4u, p_reloc->dict_len) || (((p_reloc->base + h) & 1u) != 0u)) {
        return TINYDELTA_OK;
    }
    tinydelta_result_t result = raw_le(p_reloc, h, 4u, p_value);

    if (result == TINYDELTA_OK) {
        *p_is_bl = ((*p_value & 0xF800F800u) == 0xF800F000u);
    }
    return result;
}

tinydelta_result_t tinydelta_reloc_span(const tinydelta_reloc_t *p_reloc, uint32_t offset, uint8_t *p_out, uint32_t length) {
    uint32_t end = offset + length;
    uint32_t value;
    bool b_bl;
    tinydelta_result_t result;

    /* BL: its offset gains the callee's shift less the call site's. */
    for (uint32_t h = (offset >= 3u) ? (offset - 3u) : 0u; h < end; h++) {
        result = thumb_bl(p_reloc, h, &b_bl, &value);
        if (result != TINYDELTA_OK) {
            return result;
        }
        if (b_bl) {
            /* The halfwords' 11-bit fields; the offset is twice that, sign-extended from 23 bits. */
            uint32_t field  = ((value & 0x7FFu) << 11) | ((value >> 16) & 0x7FFu);
            uint32_t branch = (field << 1) | (((field & 0x200000u) != 0u) ? 0xFF800000u : 0u);
            uint32_t callee = h + 4u + branch;
            uint32_t next   = branch - shift_at(p_reloc, h);

            if (callee < p_reloc->dict_len) {
                next += shift_at(p_reloc, callee);
            }
            if ((next != branch) && ((next & 1u) == 0u) && ((next + 0x400000u) < 0x800000u)) {
                uint8_t bytes[4];

                field = (next >> 1) & 0x3FFFFFu;
                le_bytes(bytes, 0xF800F000u | ((field >> 11) & 0x7FFu) | ((field & 0x7FFu) << 16), 4u);
                put(offset, p_out, length, h, bytes, 4u);
            }
        }
    }
    /* Aligned 32-bit pointers into the image, outside BLs. */
    for (uint32_t a = (offset >= 3u) ? (offset - 3u) : 0u; a < end; a++) {
        bool b_skip = (((p_reloc->base + a) & 3u) != 0u) || !range_within(a, 4u, p_reloc->dict_len);

        for (uint32_t back = 0u; !b_skip && (back < 3u); back++) {
            uint32_t h = a + (back * 2u); /* a - 2, a, a + 2 */

            if (h >= 2u) {
                result = thumb_bl(p_reloc, h - 2u, &b_bl, &value);
                if (result != TINYDELTA_OK) {
                    return result;
                }
                b_skip = b_bl;
            }
        }
        if (!b_skip) {
            uint32_t shift;

            result = raw_le(p_reloc, a, 4u, &value);
            if (result != TINYDELTA_OK) {
                return result;
            }
            shift = address_shift(p_reloc, value);
            if (shift != 0u) {
                uint8_t bytes[4];

                le_bytes(bytes, value + shift, 4u);
                put(offset, p_out, length, a, bytes, 4u);
            }
        }
    }
    return TINYDELTA_OK;
}

#elif TINYDELTA_RELOC == TINYDELTA_RELOC_MSP430X

/* What looks like an instruction with 20-bit addresses at h. */
typedef struct {
    uint32_t length;   /* bytes, 0 if none */
    uint32_t count;    /* address fields */
    uint32_t lo[2];    /* where each field's low 16 bits are */
    uint32_t shift[2]; /* where its high 4 bits are in the first word */
} msp430x_insn_t;

static tinydelta_result_t msp430x_candidate(const tinydelta_reloc_t *p_reloc, uint32_t h, msp430x_insn_t *p_insn) {
    uint32_t w0;
    tinydelta_result_t result;

    p_insn->length = 0u;
    p_insn->count  = 0u;
    if (!range_within(h, 4u, p_reloc->dict_len) || (((p_reloc->base + h) & 1u) != 0u)) {
        return TINYDELTA_OK;
    }
    result = raw_le(p_reloc, h, 2u, &w0);
    if (result != TINYDELTA_OK) {
        return result;
    }
    p_insn->lo[0] = h + 2u;
    if (((w0 & 0xFFF0u) == 0x13B0u) || ((w0 & 0xFFF0u) == 0x1380u) || ((w0 & 0xF0F0u) == 0x0060u)) {
        /* CALLA #imm20 / &abs20, MOVA Rs,&abs20: high bits in bits 3-0 */
        p_insn->length   = 4u;
        p_insn->count    = 1u;
        p_insn->shift[0] = 0u;
    } else if (((w0 & 0xF0C0u) == 0x0080u) || ((w0 & 0xF0F0u) == 0x0020u)) {
        /* MOVA/CMPA/ADDA/SUBA #imm20,Rd, MOVA &abs20,Rd: bits 11-8 */
        p_insn->length   = 4u;
        p_insn->count    = 1u;
        p_insn->shift[0] = 8u;
    } else if ((w0 & 0xF800u) == 0x1800u) {
        /* Extension word, then a two-operand instruction. */
        uint32_t w1;
        uint32_t position = h + 4u;

        result = raw_le(p_reloc, h + 2u, 2u, &w1);
        if ((result != TINYDELTA_OK) || (w1 < 0x4000u)) {
            return result;
        }
        uint32_t as_mode = (w1 >> 4) & 3u;
        uint32_t source  = (w1 >> 8) & 0xFu;
        uint32_t ad_mode = (w1 >> 7) & 1u;
        uint32_t dest    = w1 & 0xFu;

        if (((as_mode == 1u) && (source != 3u)) || ((as_mode == 3u) && (source == 0u))) {
            if (!((as_mode == 1u) && (source == 0u))) { /* not PC-relative */
                p_insn->lo[p_insn->count]    = position;
                p_insn->shift[p_insn->count] = 7u;
                p_insn->count++;
            }
            position += 2u;
        }
        if (ad_mode == 1u) {
            if (dest != 0u) {
                p_insn->lo[p_insn->count]    = position;
                p_insn->shift[p_insn->count] = 0u;
                p_insn->count++;
            }
            position += 2u;
        }
        if (position > p_reloc->dict_len) {
            p_insn->count = 0u;
            return TINYDELTA_OK;
        }
        p_insn->length = position - h;
    } else {
        /* not one of ours */
    }
    return TINYDELTA_OK;
}

tinydelta_result_t tinydelta_reloc_span(const tinydelta_reloc_t *p_reloc, uint32_t offset, uint8_t *p_out, uint32_t length) {
    uint32_t end = offset + length;

    for (uint32_t h = (offset >= 7u) ? (offset - 7u) : 0u; h < end; h++) {
        msp430x_insn_t insn;
        uint8_t bytes[2];
        uint32_t w0;
        tinydelta_result_t result = msp430x_candidate(p_reloc, h, &insn);

        /* Not if an instruction up to 6 bytes before reaches over h. */
        for (uint32_t back = 2u; (result == TINYDELTA_OK) && (insn.length != 0u) && (back <= 6u) && (back <= h); back += 2u) {
            msp430x_insn_t before;

            result = msp430x_candidate(p_reloc, h - back, &before);
            if (before.length > back) {
                insn.length = 0u;
            }
        }
        if (result == TINYDELTA_OK) {
            result = raw_le(p_reloc, h, (insn.length != 0u) ? 2u : 0u, &w0);
        }
        if (result != TINYDELTA_OK) {
            return result;
        }
        for (uint32_t field = 0u; (insn.length != 0u) && (field < insn.count); field++) {
            uint32_t low;
            uint32_t shift;

            result = raw_le(p_reloc, insn.lo[field], 2u, &low);
            if (result != TINYDELTA_OK) {
                return result;
            }
            low |= ((w0 >> insn.shift[field]) & 0xFu) << 16;
            shift = address_shift(p_reloc, low);
            low += shift;
            if ((shift != 0u) && (low < 0x100000u)) {
                w0 = (w0 & ~(0xFu << insn.shift[field])) | ((low >> 16) << insn.shift[field]);
                le_bytes(bytes, low, 2u);
                put(offset, p_out, length, insn.lo[field], bytes, 2u);
            }
        }
        if (insn.length != 0u) {
            le_bytes(bytes, w0, 2u);
            put(offset, p_out, length, h, bytes, 2u);
        }
    }
    return TINYDELTA_OK;
}

#endif

#endif
