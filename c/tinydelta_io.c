#include <stddef.h>

#include "tinydelta_io.h"
#include "tinydelta_internal.h"

#if TINYDELTA_SCRATCH_SIZE < 1
#error "TINYDELTA_SCRATCH_SIZE must be at least 1"
#endif

#define SCRATCH_SIZE ((uint32_t)TINYDELTA_SCRATCH_SIZE)

typedef struct {
    const tinydelta_io *io;
    uint32_t patch_pos; /* next patch byte to read */
    uint32_t out_pos;   /* target bytes produced so far */
    uint8_t scratch[TINYDELTA_SCRATCH_SIZE];
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

/* Writes the first length scratch bytes to the target. */
static tinydelta_result write_scratch(decoder *d, uint32_t length) {
    if (d->io->target_write(d->io->target_ctx, d->out_pos, d->scratch, length) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    d->out_pos += length;
    return TINYDELTA_OK;
}

/* Copies length bytes from offset in a read callback's data (dictionary or
 * patch) to the target, a scratch buffer at a time. */
static tinydelta_result copy_to_target(decoder *d, tinydelta_read_fn read, void *ctx,
                                       uint32_t offset, uint32_t length) {
    while (length > 0) {
        uint32_t chunk = min_u32(length, SCRATCH_SIZE);
        tinydelta_result result;

        if (read(ctx, offset, d->scratch, chunk) != TINYDELTA_CALLBACK_OK) return TINYDELTA_ERR_IO;
        result = write_scratch(d, chunk);
        if (result != TINYDELTA_OK) return result;

        offset += chunk;
        length -= chunk;
    }
    return TINYDELTA_OK;
}

/* COPY_TARGET: length bytes from address, an earlier target position.
 * When the copy is longer than its distance back, its source overlaps the
 * bytes being written, so it is only ever read where already produced. */
static tinydelta_result copy_from_target(decoder *d, uint32_t address, uint32_t length) {
    const tinydelta_io *io = d->io;
    uint32_t distance = d->out_pos - address;
    tinydelta_result result;

    if (distance < SCRATCH_SIZE && length > distance) {
        /* A short repeating pattern (e.g. a run of one byte): read one
         * period, repeat it to fill as many whole periods as fit, and
         * write that block until done. Every block starts at a period
         * boundary. (Built up by repetition rather than computed with a
         * division, which Cortex-M0 would do in software.) */
        uint32_t block = distance;

        if (io->target_read(io->target_ctx, address, d->scratch, distance) != TINYDELTA_CALLBACK_OK) {
            return TINYDELTA_ERR_IO;
        }
        while (block + distance <= SCRATCH_SIZE) {
            for (uint32_t i = 0; i < distance; i++) {
                d->scratch[block + i] = d->scratch[i];
            }
            block += distance;
        }
        while (length > 0) {
            uint32_t chunk = min_u32(length, block);
            result = write_scratch(d, chunk);
            if (result != TINYDELTA_OK) return result;
            length -= chunk;
        }
        return TINYDELTA_OK;
    }

    /* Otherwise, chunks of at most `distance` bytes always lie entirely
     * before the write position. */
    while (length > 0) {
        uint32_t chunk = min_u32(min_u32(length, distance), SCRATCH_SIZE);

        if (io->target_read(io->target_ctx, address, d->scratch, chunk) != TINYDELTA_CALLBACK_OK) {
            return TINYDELTA_ERR_IO;
        }
        result = write_scratch(d, chunk);
        if (result != TINYDELTA_OK) return result;

        address += chunk;
        length -= chunk;
    }
    return TINYDELTA_OK;
}

tinydelta_result tinydelta_decode_io(const tinydelta_io *io, uint32_t *target_len) {
    decoder d;
    uint32_t cursor = 0; /* dictionary read position */
    uint32_t target_cap = min_u32(io->target_cap, TINYDELTA_MAX_SIZE);
    tinydelta_result result;

    d.io = io;
    d.patch_pos = TINYDELTA_HEADER_LEN;
    d.out_pos = 0;
    if (io->dict_len > TINYDELTA_MAX_SIZE) return TINYDELTA_ERR_TOO_LARGE;
    if (io->patch_len < TINYDELTA_HEADER_LEN) return TINYDELTA_ERR_MALFORMED;
    if (io->patch_read(io->patch_ctx, 0, d.scratch, TINYDELTA_HEADER_LEN) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    if (d.scratch[0] != TINYDELTA_FORMAT_VERSION) return TINYDELTA_ERR_VERSION;

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

            result = copy_to_target(&d, io->dict_read, io->dict_ctx, address, length);
            cursor = address + length;
            break;

        case TINYDELTA_OP_ADD:
            if (!range_within(d.patch_pos, length, io->patch_len)) return TINYDELTA_ERR_MALFORMED;

            result = copy_to_target(&d, io->patch_read, io->patch_ctx, d.patch_pos, length);
            d.patch_pos += length;
            cursor += length;
            break;

        case TINYDELTA_OP_SKIP:
            if (!range_within(cursor, length, io->dict_len)) return TINYDELTA_ERR_MALFORMED;

            result = copy_to_target(&d, io->dict_read, io->dict_ctx, cursor, length);
            cursor += length;
            break;

        case TINYDELTA_OP_COPY_TARGET:
            result = read_varint(&d, &delta);
            if (result != TINYDELTA_OK) return result;
            address = d.out_pos + zigzag_decode(delta);
            if (address >= d.out_pos) return TINYDELTA_ERR_MALFORMED; /* must be earlier output */

            result = copy_from_target(&d, address, length);
            cursor += length;
            break;
        }
        if (result != TINYDELTA_OK) return result;
    }

    if (io->target_flush != NULL && io->target_flush(io->target_ctx) != TINYDELTA_CALLBACK_OK) {
        return TINYDELTA_ERR_IO;
    }
    *target_len = d.out_pos;
    return TINYDELTA_OK;
}
