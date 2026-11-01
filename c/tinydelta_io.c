/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

#include "tinydelta_io.h"
#include "tinydelta_coder.h"
#include "tinydelta_internal.h"

/* The callback decoder's state while it runs. The target position counts
 * every byte produced; the ring (the client's buffer, after a coded
 * patch's tables) holds the last ring_mask + 1 of them, at position &
 * ring_mask. */
typedef struct {
    const tinydelta_io_t *p_io;
    uint8_t *p_ring;
    uint32_t ring_mask;   /* ring size - 1 */
    uint32_t page_mask;   /* page_size - 1 */
    uint32_t patch_pos;   /* next patch byte to read */
    uint32_t target_pos;  /* target bytes produced so far */
    uint32_t written_len; /* target bytes passed to target_write */
    uint32_t cursor;      /* dictionary read position */
#if TINYDELTA_CODER
    bool b_coded;            /* instruction-stream bytes come from the coder */
    uint32_t stream_left;    /* coded: instruction-stream bytes still to come */
    tinydelta_coder_t coder; /* coded: the range decoder, reading patch bytes */
#endif
} io_decoder_t;

static uint32_t min_u32(uint32_t first, uint32_t second) {
    return (first < second) ? first : second;
}

/* ---- Patch bytes ------------------------------------------------------------
 * Every instruction-stream byte comes through io_byte: the next patch byte,
 * or for a coded patch the next byte the coder decodes from them. */

/* The next patch byte as stored (also the coder's source): one patch_read
 * of one byte. Wrap patch_read in a small buffer if single-byte reads are
 * slow (README.md, "Callback decoder"). */
static tinydelta_result_t io_raw_byte(void *p_ctx, uint8_t *p_byte) {
    io_decoder_t *p_decoder    = (io_decoder_t *)p_ctx; /* Cast: the context is always the decoder. */
    const tinydelta_io_t *p_io = p_decoder->p_io;

    if (p_decoder->patch_pos >= p_io->patch_len) {
        return TINYDELTA_ERR_MALFORMED;
    }
    if (p_io->patch_read(p_io->p_patch_ctx, p_decoder->patch_pos, p_byte, 1u) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    p_decoder->patch_pos++;
    return TINYDELTA_OK;
}

/* The next instruction-stream byte, a field of the given kind. */
static tinydelta_result_t io_byte(io_decoder_t *p_decoder, tinydelta_field_t field, uint8_t *p_byte) {
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
    return io_raw_byte(p_decoder, p_byte);
}

/* Reads a varint; first is its first byte's kind. */
static tinydelta_result_t io_read_varint(io_decoder_t *p_decoder, tinydelta_field_t first, uint32_t *p_value) {
    tinydelta_field_t field = first;
    uint32_t value          = 0u;

    for (uint32_t shift = 0u; shift < (VARINT_MAX_BYTES * VARINT_BITS_PER_BYTE); shift += VARINT_BITS_PER_BYTE) {
        uint8_t byte;
        tinydelta_result_t result = io_byte(p_decoder, field, &byte);

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

/* ---- Target ring ------------------------------------------------------------
 * Target bytes are produced straight into the ring, at most up to the end
 * of the current page at a time. Pages never wrap around the end of the
 * ring, because page_size divides the ring's size.
 */

/* Where the next target byte goes in the ring. */
static uint8_t *write_position(const io_decoder_t *p_decoder) {
    return p_decoder->p_ring + (p_decoder->target_pos & p_decoder->ring_mask);
}

/* How many of up to length bytes fit before the current page ends. */
static uint32_t page_room(const io_decoder_t *p_decoder, uint32_t length) {
    return min_u32(length, p_decoder->p_io->page_size - (p_decoder->target_pos & p_decoder->page_mask));
}

/* Writes the bytes produced since the last write: always within one page,
 * so contiguous in the ring. */
static tinydelta_result_t write_page(io_decoder_t *p_decoder) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint32_t length            = p_decoder->target_pos - p_decoder->written_len;
    const uint8_t *p_page      = p_decoder->p_ring + (p_decoder->written_len & p_decoder->ring_mask);

    if (length == 0u) {
        return TINYDELTA_OK;
    }
    if (p_io->target_write(p_io->p_target_ctx, p_decoder->written_len, p_page, length) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    p_decoder->written_len = p_decoder->target_pos;
    return TINYDELTA_OK;
}

/* Accounts for length bytes just placed at the write position (never more
 * than page_room allows) and writes the page out once it is full. */
static tinydelta_result_t produced(io_decoder_t *p_decoder, uint32_t length) {
    p_decoder->target_pos += length;
    if ((p_decoder->target_pos & p_decoder->page_mask) != 0u) {
        return TINYDELTA_OK;
    }
    return write_page(p_decoder);
}

/* Produces length bytes into the ring, a page at a time: read from offset
 * in a read callback's data, or with read NULL the next instruction-stream
 * bytes (ADD's literals). With b_add_diff (ADD_DIFF), each byte read also
 * gets the next instruction-stream byte added. */
static tinydelta_result_t read_to_target(io_decoder_t *p_decoder, tinydelta_read_fn_t read, void *p_ctx, uint32_t offset, uint32_t length, bool b_add_diff) {
    while (length > 0u) {
        uint32_t chunk    = page_room(p_decoder, length);
        uint8_t *p_output = write_position(p_decoder);
        tinydelta_result_t result;

        if ((read != NULL) && (read(p_ctx, offset, p_output, chunk) != TINYDELTA_CALLBACK_OK)) {
            return TINYDELTA_ERR_IO;
        }
        for (uint32_t index = 0u; ((read == NULL) || b_add_diff) && (index < chunk); index++) {
            uint8_t byte;

            result = io_byte(p_decoder, (read == NULL) ? TINYDELTA_FIELD_LITERAL : TINYDELTA_FIELD_DIFF, &byte);
            if (result != TINYDELTA_OK) {
                return result;
            }
            /* Cast: the sum wraps modulo 256, as the format defines. */
            p_output[index] = (read == NULL) ? byte : (uint8_t)(p_output[index] + byte);
        }
        result = produced(p_decoder, chunk);
        if (result != TINYDELTA_OK) {
            return result;
        }
        offset += chunk;
        length -= chunk;
    }
    return TINYDELTA_OK;
}

/* ---- Instructions ------------------------------------------------------- */

/* COPY: dictionary bytes at cursor + delta; the cursor moves there. */
static tinydelta_result_t io_decode_copy(io_decoder_t *p_decoder, uint32_t length) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint32_t delta;
    uint32_t address;
    tinydelta_result_t result = io_read_varint(p_decoder, TINYDELTA_FIELD_DELTA, &delta);

    if (result != TINYDELTA_OK) {
        return result;
    }
    address = p_decoder->cursor + zigzag_decode(delta);
    if (!range_within(address, length, p_io->dict_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    p_decoder->cursor = address;
    return read_to_target(p_decoder, p_io->dict_read, p_io->p_dict_ctx, address, length, false);
}

/* ADD: literal bytes, taken from the instruction stream. */
static tinydelta_result_t io_decode_add(io_decoder_t *p_decoder, uint32_t length) {
    return read_to_target(p_decoder, NULL, NULL, 0u, length, false);
}

/* SKIP: dictionary bytes at the cursor. */
static tinydelta_result_t io_decode_skip(io_decoder_t *p_decoder, uint32_t length) {
    const tinydelta_io_t *p_io = p_decoder->p_io;

    if (!range_within(p_decoder->cursor, length, p_io->dict_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    return read_to_target(p_decoder, p_io->dict_read, p_io->p_dict_ctx, p_decoder->cursor, length, false);
}

/* COPY_TARGET within the ring: byte by byte, so a source overlapping the
 * bytes being written (a short repeating run) works. */
static tinydelta_result_t copy_in_ring(io_decoder_t *p_decoder, uint32_t distance, uint32_t length) {
    uint8_t *p_ring = p_decoder->p_ring;

    while (length > 0u) {
        uint32_t chunk = page_room(p_decoder, length);
        tinydelta_result_t result;

        for (uint32_t offset = 0u; offset < chunk; offset++) {
            uint32_t position                       = p_decoder->target_pos + offset;
            p_ring[position & p_decoder->ring_mask] = p_ring[(position - distance) & p_decoder->ring_mask];
        }
        result = produced(p_decoder, chunk);
        if (result != TINYDELTA_OK) {
            return result;
        }
        length -= chunk;
    }
    return TINYDELTA_OK;
}

/* COPY_TARGET (version 1): earlier target bytes at target position + delta.
 * From the ring when they are at most its size back, else through
 * target_read. */
static tinydelta_result_t io_decode_copy_target(io_decoder_t *p_decoder, uint32_t length) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint32_t delta;
    uint32_t address;
    uint32_t distance;
    tinydelta_result_t result = io_read_varint(p_decoder, TINYDELTA_FIELD_DELTA, &delta);

    if (result != TINYDELTA_OK) {
        return result;
    }
    address = p_decoder->target_pos + zigzag_decode(delta);
    if (address >= p_decoder->target_pos) {
        return TINYDELTA_ERR_MALFORMED; /* must be earlier output */
    }
    distance = p_decoder->target_pos - address;
    if (distance <= (p_decoder->ring_mask + 1u)) {
        return copy_in_ring(p_decoder, distance, length);
    }
    if (p_io->target_read == NULL) {
        return TINYDELTA_ERR_MALFORMED; /* reaches back further than the ring */
    }
    /* The source is more than a ring back, so it lies before the current
     * page: already written, the whole way. */
    return read_to_target(p_decoder, p_io->target_read, p_io->p_target_ctx, address, length, false);
}

/* ADD_DIFF (version 2): dictionary bytes at the cursor, each plus the next
 * instruction-stream byte. */
static tinydelta_result_t io_decode_add_diff(io_decoder_t *p_decoder, uint32_t length) {
    const tinydelta_io_t *p_io = p_decoder->p_io;

    if (!range_within(p_decoder->cursor, length, p_io->dict_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    return read_to_target(p_decoder, p_io->dict_read, p_io->p_dict_ctx, p_decoder->cursor, length, true);
}

/* ---- Entry point -------------------------------------------------------- */

static bool is_power_of_two(uint32_t value) {
    return (value != 0u) && ((value & (value - 1u)) == 0u);
}

#if TINYDELTA_CODER
/* Reads a coded patch's header (the first byte's flag already seen) and
 * starts the coder with its tables at the front of the buffer:
 * *p_tables_size becomes their size, *p_version the instruction set's. */
static tinydelta_result_t io_start_coded(io_decoder_t *p_decoder, uint32_t *p_tables_size, uint8_t *p_version) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint8_t features;
    uint8_t layout;
    tinydelta_result_t result = io_raw_byte(p_decoder, &features);

    if (result != TINYDELTA_OK) {
        return result;
    }
    layout         = features & TINYDELTA_CODED_LAYOUT_MASK;
    *p_tables_size = tinydelta_coder_tables_size(layout);
    if (((features & ~TINYDELTA_CODED_LAYOUT_MASK) != 0u) || (*p_tables_size == 0u) || (p_io->buffer_size <= *p_tables_size)) {
        return TINYDELTA_ERR_VERSION; /* a feature this build or buffer lacks */
    }
    result = io_read_varint(p_decoder, TINYDELTA_FIELD_CONTINUED, &p_decoder->stream_left); /* stored as is: not coded yet */
    if (result != TINYDELTA_OK) {
        return result;
    }
    p_decoder->b_coded = true;
    *p_version &= (uint8_t)~TINYDELTA_CODED_FLAG; /* Cast: the flag fits a byte. */
    return tinydelta_coder_init(&p_decoder->coder, layout, p_io->p_buffer, io_raw_byte, p_decoder);
}
#endif

/* Whether instructions remain. */
static bool io_more(const io_decoder_t *p_decoder) {
#if TINYDELTA_CODER
    if (p_decoder->b_coded) {
        return p_decoder->stream_left > 0u;
    }
#endif
    return p_decoder->patch_pos < p_decoder->p_io->patch_len;
}

tinydelta_result_t tinydelta_decode_io(const tinydelta_io_t *p_io, uint32_t *p_target_len) {
    io_decoder_t decoder;
    uint32_t target_cap  = min_u32(p_io->target_cap, TINYDELTA_MAX_SIZE);
    uint32_t tables_size = 0u; /* a coded patch's tables, at the front of the buffer */
    uint8_t version;
    bool b_format_v2;
    tinydelta_result_t result;

    if ((p_io->p_buffer == NULL) || !is_power_of_two(p_io->page_size)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    if (p_io->dict_len > TINYDELTA_MAX_SIZE) {
        return TINYDELTA_ERR_TOO_LARGE;
    }

    decoder.p_io        = p_io;
    decoder.page_mask   = p_io->page_size - 1u;
    decoder.patch_pos   = 0u;
    decoder.target_pos  = 0u;
    decoder.written_len = 0u;
    decoder.cursor      = 0u;
    result              = io_raw_byte(&decoder, &version);
    if (result != TINYDELTA_OK) {
        return result;
    }
#if TINYDELTA_CODER
    decoder.b_coded = false;
    if ((version == (TINYDELTA_CODED_FLAG | TINYDELTA_FORMAT_VERSION)) || (version == (TINYDELTA_CODED_FLAG | TINYDELTA_FORMAT_VERSION_2))) {
        result = io_start_coded(&decoder, &tables_size, &version);
        if (result != TINYDELTA_OK) {
            return result;
        }
    }
#endif
    /* The ring is the buffer after the tables: a power of two, page_size at
     * most its size (so it divides it). */
    if (!is_power_of_two(p_io->buffer_size - tables_size) || (p_io->page_size > (p_io->buffer_size - tables_size))) {
        return TINYDELTA_ERR_MALFORMED;
    }
    decoder.p_ring    = &p_io->p_buffer[tables_size];
    decoder.ring_mask = p_io->buffer_size - tables_size - 1u;
    b_format_v2       = (version == TINYDELTA_FORMAT_VERSION_2);
    if ((version != TINYDELTA_FORMAT_VERSION) && !b_format_v2) {
        return TINYDELTA_ERR_VERSION;
    }

    while (io_more(&decoder)) {
        uint32_t instruction;
        uint32_t length;

        result = io_read_varint(&decoder, TINYDELTA_FIELD_INSTRUCTION, &instruction);
        if (result != TINYDELTA_OK) {
            return result;
        }
        length = instruction_length(instruction);
        if (length > (target_cap - decoder.target_pos)) {
            return TINYDELTA_ERR_TOO_LARGE;
        }

        switch (instruction_opcode(instruction, b_format_v2)) {
        case TINYDELTA_OP_COPY:
            result = io_decode_copy(&decoder, length);
            break;
        case TINYDELTA_OP_ADD:
            result = io_decode_add(&decoder, length);
            break;
        case TINYDELTA_OP_SKIP:
            result = io_decode_skip(&decoder, length);
            break;
        case TINYDELTA_OP_COPY_TARGET:
            result = io_decode_copy_target(&decoder, length);
            break;
        case TINYDELTA_OP_ADD_DIFF:
            result = io_decode_add_diff(&decoder, length);
            break;
        default: /* unreachable: every opcode has a case */
            result = TINYDELTA_ERR_MALFORMED;
            break;
        }
        if (result != TINYDELTA_OK) {
            return result;
        }
        decoder.cursor += length; /* the target position moved as bytes were produced */
    }

    result = write_page(&decoder); /* the last, partial page */
    if (result != TINYDELTA_OK) {
        return result;
    }
    *p_target_len = decoder.target_pos;
    return TINYDELTA_OK;
}
