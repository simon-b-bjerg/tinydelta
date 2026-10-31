#include "tinydelta_io.h"
#include "tinydelta_internal.h"

/* ADD_DIFF reads its difference bytes through this stack buffer. */
#define DIFF_CHUNK 16u

typedef struct {
    const tinydelta_io *io;
    uint32_t mask;      /* buffer_size - 1 */
    uint32_t patch_pos; /* next patch byte to read */
    uint32_t out_pos;   /* target bytes produced so far */
    uint32_t written;   /* target bytes passed to target_write */
} decoder;

static uint32_t min_u32(uint32_t a, uint32_t b) {
    return a < b ? a : b;
}

/* Reads the varint at the current patch position, with one patch_read of
 * at most VARINT_MAX_BYTES bytes. */
static tinydelta_result read_varint(decoder *d, uint32_t *value) {
    uint8_t bytes[VARINT_MAX_BYTES];
    uint32_t available = min_u32(d->io->patch_len - d->patch_pos, VARINT_MAX_BYTES);
    uint32_t used;
    tinydelta_result result;

    if (available == 0) return TINYDELTA_ERR_MALFORMED;
    if (d->io->patch_read(d->io->patch_ctx, d->patch_pos, bytes, available) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    result = varint_parse(bytes, available, value, &used);
    if (result == TINYDELTA_OK) d->patch_pos += used;
    return result;
}

/* Where the next target byte goes in the buffer, and how many of up to
 * length bytes fit there before the current page ends. Pages never wrap
 * around the end of the buffer: page_size divides buffer_size. */
static uint8_t *write_position(const decoder *d) {
    return d->io->buffer + (d->out_pos & d->mask);
}

static uint32_t page_room(const decoder *d, uint32_t length) {
    return min_u32(length, d->io->page_size - (d->out_pos & (d->io->page_size - 1u)));
}

/* Writes the bytes produced since the last write: always within one page,
 * so contiguous in the buffer. */
static tinydelta_result write_page(decoder *d) {
    uint32_t length = d->out_pos - d->written;

    if (length == 0) return TINYDELTA_OK;
    if (d->io->target_write(d->io->target_ctx, d->written, d->io->buffer + (d->written & d->mask), length) !=
        TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    d->written = d->out_pos;
    return TINYDELTA_OK;
}

/* Accounts for length bytes just placed at the write position (never
 * more than page_room allows) and writes the page out once it is full. */
static tinydelta_result produced(decoder *d, uint32_t length) {
    d->out_pos += length;
    return (d->out_pos & (d->io->page_size - 1u)) == 0 ? write_page(d) : TINYDELTA_OK;
}

/* Reads length bytes from offset in a read callback's data (dictionary,
 * patch or, for a far COPY_TARGET, the target already written) straight
 * into the buffer. With add_diff (format version 2's ADD_DIFF), each byte
 * also gets the next patch byte added, DIFF_CHUNK bytes at a time. */
static tinydelta_result read_to_target(decoder *d, tinydelta_read_fn read, void *ctx,
                                       uint32_t offset, uint32_t length, int add_diff) {
    uint8_t diff[DIFF_CHUNK];
    while (length > 0) {
        uint32_t chunk = page_room(d, length);
        uint8_t *out = write_position(d);
        tinydelta_result result;

        if (add_diff) chunk = min_u32(chunk, DIFF_CHUNK);
        if (read(ctx, offset, out, chunk) != TINYDELTA_CALLBACK_OK) return TINYDELTA_ERR_IO;
        if (add_diff) {
            if (d->io->patch_read(d->io->patch_ctx, d->patch_pos, diff, chunk) != TINYDELTA_CALLBACK_OK) {
                return TINYDELTA_ERR_IO;
            }
            for (uint32_t i = 0; i < chunk; i++) out[i] = (uint8_t)(out[i] + diff[i]);
            d->patch_pos += chunk;
        }
        result = produced(d, chunk);
        if (result != TINYDELTA_OK) return result;
        offset += chunk;
        length -= chunk;
    }
    return TINYDELTA_OK;
}


/* COPY_TARGET within the buffer: byte by byte, so a source overlapping
 * the bytes being written (a short repeating run) works. */
static tinydelta_result copy_in_buffer(decoder *d, uint32_t distance, uint32_t length) {
    uint8_t *buffer = d->io->buffer;

    while (length > 0) {
        uint32_t chunk = page_room(d, length);
        tinydelta_result result;

        for (uint32_t i = 0; i < chunk; i++) {
            uint32_t at = d->out_pos + i;
            buffer[at & d->mask] = buffer[(at - distance) & d->mask];
        }
        result = produced(d, chunk);
        if (result != TINYDELTA_OK) return result;
        length -= chunk;
    }
    return TINYDELTA_OK;
}

/* Powers of two, page_size at most buffer_size (so it divides it). */
static int buffer_valid(const tinydelta_io *io) {
    return io->buffer != 0 && io->page_size != 0 && (io->page_size & (io->page_size - 1u)) == 0 &&
           io->buffer_size >= io->page_size && (io->buffer_size & (io->buffer_size - 1u)) == 0;
}

tinydelta_result tinydelta_decode_io(const tinydelta_io *io, uint32_t *target_len) {
    decoder d;
    uint32_t cursor = 0; /* dictionary read position */
    uint32_t target_cap = min_u32(io->target_cap, TINYDELTA_MAX_SIZE);
    uint8_t version;
    tinydelta_result result;

    d.io = io;
    d.mask = io->buffer_size - 1u;
    d.patch_pos = TINYDELTA_HEADER_LEN;
    d.out_pos = 0;
    d.written = 0;
    if (!buffer_valid(io)) return TINYDELTA_ERR_MALFORMED;
    if (io->dict_len > TINYDELTA_MAX_SIZE) return TINYDELTA_ERR_TOO_LARGE;
    if (io->patch_len < TINYDELTA_HEADER_LEN) return TINYDELTA_ERR_MALFORMED;
    if (io->patch_read(io->patch_ctx, 0, &version, TINYDELTA_HEADER_LEN) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    const int is_v2 = version == TINYDELTA_FORMAT_VERSION_2;
    if (version != TINYDELTA_FORMAT_VERSION && !is_v2) return TINYDELTA_ERR_VERSION;

    while (d.patch_pos < io->patch_len) {
        uint32_t instruction, length, delta, address;

        result = read_varint(&d, &instruction);
        if (result != TINYDELTA_OK) return result;

        length = instruction_length(instruction);
        if (length > target_cap - d.out_pos) return TINYDELTA_ERR_TOO_LARGE;

        switch (instruction_opcode(instruction)) {
        case TINYDELTA_OP_COPY:
            result = read_varint(&d, &delta);
            if (result != TINYDELTA_OK) return result;
            address = cursor + zigzag_decode(delta);
            if (!range_within(address, length, io->dict_len)) return TINYDELTA_ERR_MALFORMED;

            result = read_to_target(&d, io->dict_read, io->dict_ctx, address, length, 0);
            cursor = address + length;
            break;

        case TINYDELTA_OP_ADD:
            if (!range_within(d.patch_pos, length, io->patch_len)) return TINYDELTA_ERR_MALFORMED;

            result = read_to_target(&d, io->patch_read, io->patch_ctx, d.patch_pos, length, 0);
            d.patch_pos += length;
            cursor += length;
            break;

        case TINYDELTA_OP_SKIP:
            if (!range_within(cursor, length, io->dict_len)) return TINYDELTA_ERR_MALFORMED;

            result = read_to_target(&d, io->dict_read, io->dict_ctx, cursor, length, 0);
            cursor += length;
            break;

        case TINYDELTA_OP_COPY_TARGET:
            if (is_v2) { /* ADD_DIFF: dictionary byte at the cursor + patch byte */
                if (!range_within(cursor, length, io->dict_len) ||
                    !range_within(d.patch_pos, length, io->patch_len)) return TINYDELTA_ERR_MALFORMED;
                result = read_to_target(&d, io->dict_read, io->dict_ctx, cursor, length, 1);
                cursor += length;
                break;
            }
            result = read_varint(&d, &delta);
            if (result != TINYDELTA_OK) return result;
            address = d.out_pos + zigzag_decode(delta);
            if (address >= d.out_pos) return TINYDELTA_ERR_MALFORMED; /* must be earlier output */

            if (d.out_pos - address <= io->buffer_size) {
                result = copy_in_buffer(&d, d.out_pos - address, length);
            } else if (io->target_read == 0) {
                return TINYDELTA_ERR_MALFORMED; /* reaches back further than the buffer */
            } else {
                /* The source is more than a buffer back, so it lies before
                 * the current page: already written, the whole way. */
                result = read_to_target(&d, io->target_read, io->target_ctx, address, length, 0);
            }
            cursor += length;
            break;
        }
        if (result != TINYDELTA_OK) return result;
    }

    result = write_page(&d); /* the last, partial page */
    if (result != TINYDELTA_OK) return result;
    *target_len = d.out_pos;
    return TINYDELTA_OK;
}
