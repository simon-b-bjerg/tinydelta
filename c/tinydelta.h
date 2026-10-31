/* tinydelta C decoders. See README.md for the wire format.
 *
 *   tinydelta_decode     (this header)       dictionary, patch and target
 *                                            all in memory
 *   tinydelta_decode_io  (tinydelta_io.h)    dictionary, patch and target
 *                                            behind read/write callbacks,
 *                                            e.g. on external flash
 */
#ifndef TINYDELTA_H
#define TINYDELTA_H

#include <stdint.h>

/* Largest dictionary or target the format supports: 2^30 - 1 bytes.
 * This bound keeps every value in a patch within 32 bits. */
#define TINYDELTA_MAX_SIZE 0x3FFFFFFFu

/* The patch format version these decoders read: the first byte of every
 * patch. A patch with any other first byte is rejected with
 * TINYDELTA_ERR_VERSION. */
#define TINYDELTA_FORMAT_VERSION 1u

typedef enum {
    TINYDELTA_OK            =  0,
    TINYDELTA_ERR_MALFORMED = -1, /* corrupt or truncated patch, or one that
                                     doesn't fit the dictionary */
    TINYDELTA_ERR_TOO_LARGE = -2, /* target would exceed its capacity, or the
                                     dictionary exceeds TINYDELTA_MAX_SIZE */
    TINYDELTA_ERR_IO        = -3, /* a read/write callback reported failure
                                     (tinydelta_decode_io only) */
    TINYDELTA_ERR_VERSION   = -4  /* the patch's version byte is not
                                     TINYDELTA_FORMAT_VERSION: a patch for
                                     another decoder, or not a patch */
} tinydelta_result;

/* Applies patch to dict and writes the target into out, which holds
 * out_cap bytes (values above TINYDELTA_MAX_SIZE are treated as
 * TINYDELTA_MAX_SIZE). On success, sets *out_len to the target's length.
 * Uses 32-bit arithmetic only, no allocation, and reads the patch once,
 * front to back. */
tinydelta_result tinydelta_decode(const uint8_t *dict, uint32_t dict_len,
                                  const uint8_t *patch, uint32_t patch_len,
                                  uint8_t *out, uint32_t out_cap,
                                  uint32_t *out_len);

#endif
