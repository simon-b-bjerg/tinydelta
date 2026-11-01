#include "tinydelta_io.h"
#include "tinydelta_internal.h"

/* ADD_DIFF reads its difference bytes through this stack buffer. */
#define DIFF_CHUNK 16u

/* The callback decoder's state while it runs. The target position counts
 * every byte produced; the buffer holds the last buffer_size of them, at
 * position & buffer_mask. */
typedef struct {
    const tinydelta_io_t *p_io;
    uint32_t buffer_mask; /* buffer_size - 1 */
    uint32_t page_mask;   /* page_size - 1 */
    uint32_t patch_pos;   /* next patch byte to read */
    uint32_t target_pos;  /* target bytes produced so far */
    uint32_t written_len; /* target bytes passed to target_write */
    uint32_t cursor;      /* dictionary read position */
} io_decoder_t;

static uint32_t min_u32(uint32_t first, uint32_t second) {
    return (first < second) ? first : second;
}

/* ---- Patch reading ------------------------------------------------------ */

/* Reads the varint at the current patch position, with one patch_read of
 * at most VARINT_MAX_BYTES bytes. */
static tinydelta_result_t io_read_varint(io_decoder_t *p_decoder, uint32_t *p_value) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint8_t bytes[VARINT_MAX_BYTES];
    uint32_t available = min_u32(p_io->patch_len - p_decoder->patch_pos, VARINT_MAX_BYTES);
    uint32_t used;
    tinydelta_result_t result;

    if (available == 0u) {
        return TINYDELTA_ERR_MALFORMED;
    }
    if (p_io->patch_read(p_io->p_patch_ctx, p_decoder->patch_pos, bytes, available) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    result = varint_parse(bytes, available, p_value, &used);
    if (result == TINYDELTA_OK) {
        p_decoder->patch_pos += used;
    }
    return result;
}

/* ---- Target buffer ------------------------------------------------------
 * Target bytes are produced straight into the buffer, at most up to the end
 * of the current page at a time. Pages never wrap around the end of the
 * buffer, because page_size divides buffer_size.
 */

/* Where the next target byte goes in the buffer. */
static uint8_t *write_position(const io_decoder_t *p_decoder) {
    return p_decoder->p_io->p_buffer + (p_decoder->target_pos & p_decoder->buffer_mask);
}

/* How many of up to length bytes fit before the current page ends. */
static uint32_t page_room(const io_decoder_t *p_decoder, uint32_t length) {
    return min_u32(length, p_decoder->p_io->page_size - (p_decoder->target_pos & p_decoder->page_mask));
}

/* Writes the bytes produced since the last write: always within one page,
 * so contiguous in the buffer. */
static tinydelta_result_t write_page(io_decoder_t *p_decoder) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint32_t length            = p_decoder->target_pos - p_decoder->written_len;
    const uint8_t *p_page      = p_io->p_buffer + (p_decoder->written_len & p_decoder->buffer_mask);

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

/* Reads length bytes from offset in a read callback's data (dictionary,
 * patch or, for a far COPY_TARGET, the target already written) straight
 * into the buffer, a page at a time. */
static tinydelta_result_t read_to_target(io_decoder_t *p_decoder, tinydelta_read_fn_t read, void *p_ctx, uint32_t offset, uint32_t length) {
    while (length > 0u) {
        uint32_t chunk = page_room(p_decoder, length);
        tinydelta_result_t result;

        if (read(p_ctx, offset, write_position(p_decoder), chunk) != TINYDELTA_CALLBACK_OK) {
            return TINYDELTA_ERR_IO;
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
    tinydelta_result_t result = io_read_varint(p_decoder, &delta);

    if (result != TINYDELTA_OK) {
        return result;
    }
    address = p_decoder->cursor + zigzag_decode(delta);
    if (!range_within(address, length, p_io->dict_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    p_decoder->cursor = address;
    return read_to_target(p_decoder, p_io->dict_read, p_io->p_dict_ctx, address, length);
}

/* ADD: literal bytes, taken from the patch. */
static tinydelta_result_t io_decode_add(io_decoder_t *p_decoder, uint32_t length) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint32_t start             = p_decoder->patch_pos;

    if (!range_within(start, length, p_io->patch_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    p_decoder->patch_pos += length;
    return read_to_target(p_decoder, p_io->patch_read, p_io->p_patch_ctx, start, length);
}

/* SKIP: dictionary bytes at the cursor. */
static tinydelta_result_t io_decode_skip(io_decoder_t *p_decoder, uint32_t length) {
    const tinydelta_io_t *p_io = p_decoder->p_io;

    if (!range_within(p_decoder->cursor, length, p_io->dict_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    return read_to_target(p_decoder, p_io->dict_read, p_io->p_dict_ctx, p_decoder->cursor, length);
}

/* COPY_TARGET within the buffer: byte by byte, so a source overlapping the
 * bytes being written (a short repeating run) works. */
static tinydelta_result_t copy_in_buffer(io_decoder_t *p_decoder, uint32_t distance, uint32_t length) {
    uint8_t *p_buffer = p_decoder->p_io->p_buffer;

    while (length > 0u) {
        uint32_t chunk = page_room(p_decoder, length);
        tinydelta_result_t result;

        for (uint32_t offset = 0u; offset < chunk; offset++) {
            uint32_t position                           = p_decoder->target_pos + offset;
            p_buffer[position & p_decoder->buffer_mask] = p_buffer[(position - distance) & p_decoder->buffer_mask];
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
 * From the buffer when they are at most buffer_size bytes back, else
 * through target_read. */
static tinydelta_result_t io_decode_copy_target(io_decoder_t *p_decoder, uint32_t length) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint32_t delta;
    uint32_t address;
    uint32_t distance;
    tinydelta_result_t result = io_read_varint(p_decoder, &delta);

    if (result != TINYDELTA_OK) {
        return result;
    }
    address = p_decoder->target_pos + zigzag_decode(delta);
    if (address >= p_decoder->target_pos) {
        return TINYDELTA_ERR_MALFORMED; /* must be earlier output */
    }
    distance = p_decoder->target_pos - address;
    if (distance <= p_io->buffer_size) {
        return copy_in_buffer(p_decoder, distance, length);
    }
    if (p_io->target_read == NULL) {
        return TINYDELTA_ERR_MALFORMED; /* reaches back further than the buffer */
    }
    /* The source is more than a buffer back, so it lies before the current
     * page: already written, the whole way. */
    return read_to_target(p_decoder, p_io->target_read, p_io->p_target_ctx, address, length);
}

/* ADD_DIFF (version 2): dictionary bytes at the cursor, each plus the next
 * patch byte, DIFF_CHUNK bytes at a time. */
static tinydelta_result_t io_decode_add_diff(io_decoder_t *p_decoder, uint32_t length) {
    const tinydelta_io_t *p_io = p_decoder->p_io;
    uint8_t diff[DIFF_CHUNK];
    uint32_t offset = p_decoder->cursor;

    if (!range_within(offset, length, p_io->dict_len) || !range_within(p_decoder->patch_pos, length, p_io->patch_len)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    while (length > 0u) {
        uint32_t chunk    = min_u32(page_room(p_decoder, length), DIFF_CHUNK);
        uint8_t *p_output = write_position(p_decoder);
        tinydelta_result_t result;

        if (p_io->dict_read(p_io->p_dict_ctx, offset, p_output, chunk) != TINYDELTA_CALLBACK_OK) {
            return TINYDELTA_ERR_IO;
        }
        if (p_io->patch_read(p_io->p_patch_ctx, p_decoder->patch_pos, diff, chunk) != TINYDELTA_CALLBACK_OK) {
            return TINYDELTA_ERR_IO;
        }
        for (uint32_t index = 0u; index < chunk; index++) {
            /* Cast: the sum wraps modulo 256, as the format defines. */
            p_output[index] = (uint8_t)(p_output[index] + diff[index]);
        }
        p_decoder->patch_pos += chunk;
        result = produced(p_decoder, chunk);
        if (result != TINYDELTA_OK) {
            return result;
        }
        offset += chunk;
        length -= chunk;
    }
    return TINYDELTA_OK;
}

/* ---- Entry point -------------------------------------------------------- */

static bool is_power_of_two(uint32_t value) {
    return (value != 0u) && ((value & (value - 1u)) == 0u);
}

/* Powers of two, page_size at most buffer_size (so it divides it). */
static bool buffer_valid(const tinydelta_io_t *p_io) {
    return (p_io->p_buffer != NULL) && is_power_of_two(p_io->page_size) && is_power_of_two(p_io->buffer_size) && (p_io->page_size <= p_io->buffer_size);
}

tinydelta_result_t tinydelta_decode_io(const tinydelta_io_t *p_io, uint32_t *p_target_len) {
    io_decoder_t decoder;
    uint32_t target_cap = min_u32(p_io->target_cap, TINYDELTA_MAX_SIZE);
    uint8_t version;
    bool b_format_v2;
    tinydelta_result_t result;

    if (!buffer_valid(p_io)) {
        return TINYDELTA_ERR_MALFORMED;
    }
    if (p_io->dict_len > TINYDELTA_MAX_SIZE) {
        return TINYDELTA_ERR_TOO_LARGE;
    }
    if (p_io->patch_len < TINYDELTA_HEADER_LEN) {
        return TINYDELTA_ERR_MALFORMED;
    }
    if (p_io->patch_read(p_io->p_patch_ctx, 0u, &version, TINYDELTA_HEADER_LEN) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    b_format_v2 = (version == TINYDELTA_FORMAT_VERSION_2);
    if ((version != TINYDELTA_FORMAT_VERSION) && !b_format_v2) {
        return TINYDELTA_ERR_VERSION;
    }

    decoder.p_io        = p_io;
    decoder.buffer_mask = p_io->buffer_size - 1u;
    decoder.page_mask   = p_io->page_size - 1u;
    decoder.patch_pos   = TINYDELTA_HEADER_LEN;
    decoder.target_pos  = 0u;
    decoder.written_len = 0u;
    decoder.cursor      = 0u;

    while (decoder.patch_pos < p_io->patch_len) {
        uint32_t instruction;
        uint32_t length;

        result = io_read_varint(&decoder, &instruction);
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
