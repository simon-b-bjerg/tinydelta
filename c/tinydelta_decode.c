#include <string.h>

#include "tinydelta.h"
#include "tinydelta_internal.h"

tinydelta_result tinydelta_decode(const uint8_t *dict, uint32_t dict_len,
                                  const uint8_t *patch, uint32_t patch_len,
                                  uint8_t *out, uint32_t out_cap,
                                  uint32_t *out_len) {
    uint32_t patch_pos = 0; /* next patch byte to read */
    uint32_t out_pos = 0;   /* target bytes produced so far */
    uint32_t cursor = 0;    /* dictionary read position */

    if (dict_len > TINYDELTA_MAX_SIZE) return TINYDELTA_ERR_TOO_LARGE;
    if (out_cap > TINYDELTA_MAX_SIZE) out_cap = TINYDELTA_MAX_SIZE;

    while (patch_pos < patch_len) {
        uint32_t instruction, length, delta, address, used;

        if (varint_parse(patch + patch_pos, patch_len - patch_pos, &instruction, &used) != TINYDELTA_OK) {
            return TINYDELTA_ERR_MALFORMED;
        }
        patch_pos += used;

        length = instruction_length(instruction);
        if (length > out_cap - out_pos) return TINYDELTA_ERR_TOO_LARGE;

        switch (instruction_opcode(instruction)) {
        case TINYDELTA_OP_COPY:
            if (varint_parse(patch + patch_pos, patch_len - patch_pos, &delta, &used) != TINYDELTA_OK) {
                return TINYDELTA_ERR_MALFORMED;
            }
            patch_pos += used;
            address = cursor + zigzag_decode(delta);
            if (!range_within(address, length, dict_len)) return TINYDELTA_ERR_MALFORMED;

            memcpy(out + out_pos, dict + address, length);
            cursor = address + length;
            break;

        case TINYDELTA_OP_ADD:
            if (!range_within(patch_pos, length, patch_len)) return TINYDELTA_ERR_MALFORMED;

            memcpy(out + out_pos, patch + patch_pos, length);
            patch_pos += length;
            cursor += length;
            break;

        case TINYDELTA_OP_SKIP:
            if (!range_within(cursor, length, dict_len)) return TINYDELTA_ERR_MALFORMED;

            memcpy(out + out_pos, dict + cursor, length);
            cursor += length;
            break;

        case TINYDELTA_OP_COPY_TARGET:
            if (varint_parse(patch + patch_pos, patch_len - patch_pos, &delta, &used) != TINYDELTA_OK) {
                return TINYDELTA_ERR_MALFORMED;
            }
            patch_pos += used;
            address = out_pos + zigzag_decode(delta);
            if (address >= out_pos) return TINYDELTA_ERR_MALFORMED; /* must be earlier output */

            /* Byte by byte: the source may overlap the bytes being written
             * (e.g. a delta of -1 repeats the previous byte). */
            for (uint32_t i = 0; i < length; i++) {
                out[out_pos + i] = out[address + i];
            }
            cursor += length;
            break;
        }
        out_pos += length;
    }

    *out_len = out_pos;
    return TINYDELTA_OK;
}
