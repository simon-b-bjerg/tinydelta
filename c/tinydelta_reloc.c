/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* Relocation (see tinydelta_reloc.h and README.md, "Relocation"). Which
 * bytes are addresses depends only on the raw old image, a few bytes around
 * each position, so any span of the image relocates the same whichever
 * order the decoder reads it in. The rules match the Go encoder's
 * (reloc.go). Instructions are read as 16-bit words, which keeps the code
 * small on 16-bit cores. */
#include "tinydelta_reloc.h"
#include "tinydelta_internal.h"

#if TINYDELTA_CODER && (TINYDELTA_RELOC != 0)

/* The span being relocated. */
typedef struct {
    uint32_t offset;
    uint32_t length;
    uint8_t *p_out;
} span_t;

/* Map entries are stored without alignment (the tables buffer has none):
 * start, length, shift (two's complement), each 4 bytes little-endian. */
static void store_le32(uint8_t *p_bytes, uint32_t value) {
    for (uint32_t byte_index = 0u; byte_index < 4u; byte_index++) {
        p_bytes[byte_index] = (uint8_t)value; /* Cast: keeps the low byte. */
        value >>= 8;
    }
}

static uint32_t load_le32(const uint8_t *p_bytes) {
    uint32_t value = 0u;

    for (uint32_t byte_index = 4u; byte_index > 0u; byte_index--) {
        value = (value << 8) | p_bytes[byte_index - 1u];
    }
    return value;
}

/* Reads a list from the header: a count, then per record its fields as
 * varints (gap since the previous record's end, length, and for 3 fields a
 * zigzag shift), stored at *pp_storage as 4-byte start, length[, shift].
 * TINYDELTA_ERR_VERSION if it doesn't fit the *p_size bytes left. */
static tinydelta_result_t
read_list(tinydelta_varint_fn_t varint, void *p_varint_ctx, uint8_t **pp_storage, uint32_t *p_size, uint32_t fields, uint32_t *p_count) {
    uint32_t end              = 0u;
    tinydelta_result_t result = varint(p_varint_ctx, p_count);

    for (uint32_t index = 0u; (result == TINYDELTA_OK) && (index < *p_count); index++) {
        uint32_t record[3] = {0u, 0u, 0u}; /* gap, length, zigzag shift as read */

        if (*p_size < (fields << 2)) {
            return TINYDELTA_ERR_VERSION; /* doesn't fit the tables buffer */
        }
        *p_size -= fields << 2;
        for (uint32_t field = 0u; (result == TINYDELTA_OK) && (field < fields); field++) {
            result = varint(p_varint_ctx, &record[field]);
        }
        if ((result == TINYDELTA_OK) && !range_within(record[0], record[1], 0xFFFFFFFFu - end)) {
            result = TINYDELTA_ERR_MALFORMED; /* past 32 bits */
        }
        record[0] += end;
        record[2] = zigzag_decode(record[2]);
        end       = record[0] + record[1];
        for (uint32_t field = 0u; field < fields; field++) {
            store_le32(*pp_storage, record[field]);
            *pp_storage += 4u;
        }
    }
    return result;
}

tinydelta_result_t
tinydelta_reloc_read_map(tinydelta_reloc_t *p_reloc, tinydelta_varint_fn_t varint, void *p_varint_ctx, uint8_t *p_storage, uint32_t storage_size) {
    tinydelta_result_t result = varint(p_varint_ctx, &p_reloc->base);

    p_reloc->p_entries     = p_storage;
    p_reloc->run_count     = 0u;
    p_reloc->outside_count = 0u;
    if (result == TINYDELTA_OK) {
        result = read_list(varint, p_varint_ctx, &p_storage, &storage_size, 3u, &p_reloc->count);
    }
#if TINYDELTA_RELOC == TINYDELTA_RELOC_MSP430X
    /* Data runs, in blocks of 256 bytes. */
    p_reloc->p_runs = p_storage;
    if (result == TINYDELTA_OK) {
        result = read_list(varint, p_varint_ctx, &p_storage, &storage_size, 2u, &p_reloc->run_count);
    }
    /* Addresses outside the image that moved. */
    p_reloc->p_outside = p_storage;
    if (result == TINYDELTA_OK) {
        result = read_list(varint, p_varint_ctx, &p_storage, &storage_size, 3u, &p_reloc->outside_count);
    }
#endif
    return result;
}

/* The shift of the entry holding key among count entries; 0 if none. */
static uint32_t entry_shift(const uint8_t *p_entry, uint32_t count, uint32_t key) {
    for (uint32_t index = 0u; index < count; index++) {
        uint32_t start = load_le32(p_entry);

        if (start > key) {
            break; /* entries are in order */
        }
        if ((key - start) < load_le32(p_entry + 4u)) {
            return load_le32(p_entry + 8u);
        }
        p_entry += TINYDELTA_RELOC_ENTRY_SIZE;
    }
    return 0u;
}

/* The shift of an address: by its offset if it points into the old
 * image, else (MSP430X) by the address itself. */
static uint32_t address_shift(const tinydelta_reloc_t *p_reloc, uint32_t address) {
    uint32_t offset = address - p_reloc->base;

    if ((address < p_reloc->base) || (offset >= p_reloc->dict_len)) {
#if TINYDELTA_RELOC == TINYDELTA_RELOC_MSP430X
        return entry_shift(p_reloc->p_outside, p_reloc->outside_count, address);
#else
        return 0u;
#endif
    }
    return entry_shift(p_reloc->p_entries, p_reloc->count, offset);
}

/* The raw 16-bit little-endian word at index and index + 1 (within the
 * image). */
static uint32_t raw16(const tinydelta_reloc_t *p_reloc, uint32_t index) {
    uint32_t word = 0u;

    for (uint32_t byte_index = 2u; byte_index > 0u; byte_index--) {
        uint32_t position = index + byte_index - 1u;
        uint8_t byte      = (p_reloc->p_dict != NULL) ? p_reloc->p_dict[position] : p_reloc->raw(p_reloc->p_raw_ctx, position);

        word = (word << 8) | byte;
    }
    return word;
}

/* Stores a relocated 16-bit word at index: the bytes of it in the span. */
static void put16(const span_t *p_span, uint32_t index, uint32_t word, uint32_t mask) {
    for (uint32_t byte_index = 0u; byte_index < 2u; byte_index++) {
        uint32_t position = (index + byte_index) - p_span->offset; /* wraps around below the span */

        if (position < p_span->length) {
            /* Cast: keeps the low byte. Only the mask's bits change, so moved bits of an overlapping instruction stay. */
            p_span->p_out[position] = (uint8_t)((p_span->p_out[position] & ~mask) | (word & mask));
        }
        word >>= 8;
        mask >>= 8;
    }
}

/* Whether size bytes at h are in the image, at an even address. */
static bool even_fits(const tinydelta_reloc_t *p_reloc, uint32_t h, uint32_t size) {
    return range_within(h, size, p_reloc->dict_len) && (((p_reloc->base + h) & 1u) == 0u);
}

#if TINYDELTA_RELOC == TINYDELTA_RELOC_THUMB

/* Whether a BL starts at h: a halfword 0xF000-0xF7FF, then one
 * 0xF800-0xFFFF (BLs can't overlap: the ranges are disjoint). */
static bool thumb_bl(const tinydelta_reloc_t *p_reloc, uint32_t h) {
    return even_fits(p_reloc, h, 4u) && ((raw16(p_reloc, h) & 0xF800u) == 0xF000u) && ((raw16(p_reloc, h + 2u) & 0xF800u) == 0xF800u);
}

void tinydelta_reloc_span(const tinydelta_reloc_t *p_reloc, uint32_t offset, uint8_t *p_out, uint32_t length) {
    span_t span  = {offset, length, p_out};
    uint32_t end = offset + length;

    for (uint32_t h = (offset >= 3u) ? (offset - 3u) : 0u; h < end; h++) {
        if (thumb_bl(p_reloc, h)) {
            /* The 22-bit field, 11 bits per halfword; the offset is twice it,
             * sign-extended from 23 bits, and gains the callee's shift less the
             * call site's. */
            uint32_t field  = ((raw16(p_reloc, h) & 0x7FFu) << 11) | (raw16(p_reloc, h + 2u) & 0x7FFu);
            uint32_t branch = (field << 1) | (((field & 0x200000u) != 0u) ? 0xFF800000u : 0u);
            uint32_t callee = h + 4u + branch;
            uint32_t next   = branch - address_shift(p_reloc, p_reloc->base + h);

            if (callee < p_reloc->dict_len) {
                next += address_shift(p_reloc, p_reloc->base + callee);
            }
            if ((next != branch) && ((next & 1u) == 0u) && ((next + 0x400000u) < 0x800000u)) {
                field = next >> 1;
                put16(&span, h, 0xF000u | ((field >> 11) & 0x7FFu), 0xFFFFu);
                put16(&span, h + 2u, 0xF800u | (field & 0x7FFu), 0xFFFFu);
            }
        } else if ((((p_reloc->base + h) & 3u) == 0u) && even_fits(p_reloc, h, 4u) && !((h >= 2u) && thumb_bl(p_reloc, h - 2u)) && !thumb_bl(p_reloc, h + 2u)) {
            /* An aligned 32-bit pointer into the image that no BL overlaps. */
            uint32_t pointer = raw16(p_reloc, h) | (raw16(p_reloc, h + 2u) << 16);
            uint32_t shift   = address_shift(p_reloc, pointer);

            if (shift != 0u) {
                pointer += shift;
                put16(&span, h, pointer, 0xFFFFu);
                put16(&span, h + 2u, pointer >> 16, 0xFFFFu);
            }
        } else {
            /* nothing to relocate at h */
        }
    }
}

#elif TINYDELTA_RELOC == TINYDELTA_RELOC_MSP430X

/* Whether old-image offset h is in a data run (runs are in 256-byte blocks). */
static bool in_data(const tinydelta_reloc_t *p_reloc, uint32_t h) {
    const uint8_t *p_run = p_reloc->p_runs;
    uint32_t block       = h >> 8;

    for (uint32_t index = 0u; index < p_reloc->run_count; index++) {
        uint32_t start = load_le32(p_run);

        if (start > block) {
            break; /* runs are in order */
        }
        if ((block - start) < load_le32(p_run + 4u)) {
            return true;
        }
        p_run += 8u;
    }
    return false;
}

/* What looks like a 4-byte data pointer at h, in a data run: high half
 * 1-15, pointing into the image (0 if none). */
static uint32_t data_pointer(const tinydelta_reloc_t *p_reloc, uint32_t h) {
    uint32_t pointer = 0u;

    if (even_fits(p_reloc, h, 4u) && in_data(p_reloc, h)) {
        pointer = raw16(p_reloc, h) | (raw16(p_reloc, h + 2u) << 16);
        if (((pointer >> 16) == 0u) || ((pointer >> 16) > 0xFu) || (pointer < p_reloc->base) || ((pointer - p_reloc->base) >= p_reloc->dict_len)) {
            pointer = 0u;
        }
    }
    return pointer;
}

/* The length in bytes of what looks like an instruction with 20-bit
 * addresses at h (0 if none). *p_fields gets its address fields, up to
 * two of 8 bits each, the first in the low bits: per field, the position
 * of the address's high 4 bits in the first word (bits 3-0), and which
 * word after the first holds its low 16 bits (bits 7-4). */
static uint32_t msp430x_candidate(const tinydelta_reloc_t *p_reloc, uint32_t h, uint32_t *p_fields) {
    uint32_t w0;
    uint32_t length = 4u;

    *p_fields = 0u;
    if (!even_fits(p_reloc, h, 4u) || in_data(p_reloc, h)) {
        return 0u;
    }
    w0 = raw16(p_reloc, h);
    if (((w0 & 0xFFF0u) == 0x13B0u) || ((w0 & 0xFFF0u) == 0x1380u) || ((w0 & 0xF0F0u) == 0x0060u)) {
        *p_fields = 0x10u; /* CALLA #imm20 / &abs20, MOVA Rs,&abs20: bits 3-0, word 1 */
    } else if (((w0 & 0xF0C0u) == 0x0080u) || ((w0 & 0xF0F0u) == 0x0020u)) {
        *p_fields = 0x18u; /* MOVA/CMPA/ADDA/SUBA #imm20,Rd, MOVA &abs20,Rd: bits 11-8 */
    } else if ((w0 & 0xF800u) == 0x1800u) {
        /* Extension word, then a two-operand instruction and its operand words. */
        uint32_t w1     = raw16(p_reloc, h + 2u);
        uint32_t as     = (w1 >> 4) & 3u;
        uint32_t source = (w1 >> 8) & 0xFu;
        uint32_t word   = 2u; /* the next operand word's number */

        if (w1 < 0x4000u) {
            return 0u; /* not a two-operand instruction */
        }
        if (((as == 1u) && (source != 3u)) || ((as == 3u) && (source == 0u))) {
            if ((as != 1u) || (source != 0u)) { /* not PC-relative */
                *p_fields = 7u | (word << 4);
            }
            word++;
        }
        if ((w1 & 0x80u) != 0u) { /* Ad = 1 */
            if ((w1 & 0xFu) != 0u) {
                *p_fields |= (word << 4) << (((*p_fields) != 0u) ? 8u : 0u);
            }
            word++;
        }
        length = word * 2u;
        if (!range_within(h, length, p_reloc->dict_len)) {
            *p_fields = 0u;
            length    = 0u;
        }
    } else {
        length = 0u;
    }
    return length;
}

/* How many words before a candidate msp430x_starts decodes. */
#define MSP430X_SYNC_WORDS 8u

/* 2 if a source operand with mode as on register reg takes a word. */
static uint32_t operand_word(uint32_t as, uint32_t reg) {
    return ((((as == 1u) && (reg != 3u)) || ((as == 3u) && (reg == 0u))) ? 2u : 0u);
}

/* The length in bytes of any instruction at h, if it is one (0 at odd
 * positions and in data runs). */
static uint32_t msp430x_length(const tinydelta_reloc_t *p_reloc, uint32_t h) {
    uint32_t length = 2u;
    uint32_t w;

    if (!even_fits(p_reloc, h, 2u) || in_data(p_reloc, h)) {
        return 0u;
    }
    w = raw16(p_reloc, h);
    if (((w & 0xF800u) == 0x1800u) && even_fits(p_reloc, h, 4u)) { /* extension word */
        length = 4u;
        w      = raw16(p_reloc, h + 2u);
    }
    if (w >= 0x4000u) { /* two operands */
        length += operand_word((w >> 4) & 3u, (w >> 8) & 0xFu) + ((w >> 6) & 2u);
    } else if (w < 0x1000u) { /* MSP430X address instructions */
        length += ((0x0FCCu >> ((w >> 4) & 0xFu)) & 1u) * 2u;
    } else if (w < 0x1300u) { /* one operand */
        length += operand_word((w >> 4) & 3u, w & 0xFu);
    } else if (w < 0x1400u) { /* RETI, CALLA */
        length += ((0x0B20u >> ((w >> 4) & 0xFu)) & 1u) * 2u;
    } else {
        /* jumps, PUSHM, POPM: one word */
    }
    return length;
}

/* Whether decoding instructions from MSP430X_SYNC_WORDS words before h
 * reaches h as an instruction, not as an operand word of one before. */
static bool msp430x_starts(const tinydelta_reloc_t *p_reloc, uint32_t h) {
    uint32_t covered = 0u; /* bit i: the word i words on is an operand */
    uint32_t sync    = 2u * MSP430X_SYNC_WORDS;

    for (uint32_t back = sync; back > 0u; back -= 2u) {
        if (((covered & 1u) == 0u) && (back <= h)) {
            uint32_t length = msp430x_length(p_reloc, h - back);

            covered |= ((1u << (length >> 1)) - 1u) & ~1u;
        }
        covered >>= 1;
    }
    return ((covered & 1u) == 0u);
}

void tinydelta_reloc_span(const tinydelta_reloc_t *p_reloc, uint32_t offset, uint8_t *p_out, uint32_t length) {
    span_t span  = {offset, length, p_out};
    uint32_t end = offset + length;

    for (uint32_t h = (offset >= 7u) ? (offset - 7u) : 0u; h < end; h++) {
        uint32_t fields;
        uint32_t size;

        if (in_data(p_reloc, h)) {
            /* A data pointer counts unless one 2 bytes before overlaps it. */
            uint32_t pointer = data_pointer(p_reloc, h);

            if ((pointer != 0u) && ((h < 2u) || (data_pointer(p_reloc, h - 2u) == 0u))) {
                uint32_t shift = address_shift(p_reloc, pointer);

                pointer += shift;
                if ((shift != 0u) && (pointer < 0x100000u)) {
                    put16(&span, h, pointer, 0xFFFFu);
                    put16(&span, h + 2u, pointer >> 16, 0xFFFFu);
                }
            }
            continue;
        }
        size = msp430x_candidate(p_reloc, h, &fields);
        if ((size != 0u) && !msp430x_starts(p_reloc, h)) {
            size = 0u;
        }
        if (size != 0u) {
            uint32_t w0 = raw16(p_reloc, h);

            for (; fields != 0u; fields >>= 8) {
                uint32_t high    = fields & 0xFu;
                uint32_t at      = h + (((fields >> 4) & 0xFu) * 2u);
                uint32_t address = raw16(p_reloc, at) | (((w0 >> high) & 0xFu) << 16);
                uint32_t shift   = address_shift(p_reloc, address);

                address += shift;
                if ((shift != 0u) && (address < 0x100000u)) {
                    put16(&span, h, (address >> 16) << high, 0xFu << high); /* the high bits only */
                    put16(&span, at, address, 0xFFFFu);
                }
            }
        }
    }
}

#endif

#endif
