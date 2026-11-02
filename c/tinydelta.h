/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

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

/* The patch format versions these decoders read: the first byte of every
 * patch. Version 2 is made for compression (ADD_DIFF in place of
 * COPY_TARGET). A patch with any other first byte is rejected with
 * TINYDELTA_ERR_VERSION. */
#define TINYDELTA_FORMAT_VERSION   1u
#define TINYDELTA_FORMAT_VERSION_2 2u

typedef enum {
    TINYDELTA_OK            = 0,
    TINYDELTA_ERR_MALFORMED = -1, /* corrupt or truncated patch, or one that
                                     doesn't fit the dictionary */
    TINYDELTA_ERR_TOO_LARGE = -2, /* target would exceed its capacity, or the
                                     dictionary exceeds TINYDELTA_MAX_SIZE */
    TINYDELTA_ERR_IO        = -3, /* a read/write callback reported failure
                                     (tinydelta_decode_io only) */
    TINYDELTA_ERR_VERSION   = -4  /* the patch's version byte is neither 1
                                     nor 2: a newer format version, or not
                                     a patch; or a coded patch this build
                                     (TINYDELTA_CODER) or buffer can't
                                     decode */
} tinydelta_result_t;

/* Applies the patch to the dictionary and writes the target into
 * p_target, which holds target_cap bytes (values above TINYDELTA_MAX_SIZE
 * are treated as TINYDELTA_MAX_SIZE). On success, sets *p_target_len to
 * the target's length. Uses 32-bit arithmetic only, no allocation, and
 * reads the patch once, front to back. */
tinydelta_result_t tinydelta_decode(
    const uint8_t *p_dict, uint32_t dict_len, const uint8_t *p_patch, uint32_t patch_len, uint8_t *p_target, uint32_t target_cap, uint32_t *p_target_len);

/* ---- Coded patches -------------------------------------------------------
 * A coded patch (first byte 0x81 or 0x82) is a version 1 or 2 patch whose
 * instruction stream is range-coded, decoded in one pass. Its decoder needs
 * RAM for probability tables, the size of the patch's table layout, for as
 * long as it decodes: a buffer that may live on the stack. Built with
 * TINYDELTA_CODER = 1 only; otherwise coded patches are rejected with
 * TINYDELTA_ERR_VERSION. */

#ifndef TINYDELTA_CODER
#define TINYDELTA_CODER 0
#endif

/* Table sizes of the three layouts, in bytes (encoder: -coder-tables). */
#define TINYDELTA_CODED_TABLES_A 1287u
#define TINYDELTA_CODED_TABLES_B 777u
#define TINYDELTA_CODED_TABLES_C 522u

#if TINYDELTA_CODER
/* tinydelta_decode for plain and coded patches: p_tables holds tables_size
 * bytes for a coded patch's tables (unused for a plain patch). A coded
 * patch whose layout needs more is rejected with TINYDELTA_ERR_VERSION. */
tinydelta_result_t tinydelta_decode_coded(const uint8_t *p_dict,
                                          uint32_t dict_len,
                                          const uint8_t *p_patch,
                                          uint32_t patch_len,
                                          uint8_t *p_target,
                                          uint32_t target_cap,
                                          uint8_t *p_tables,
                                          uint32_t tables_size,
                                          uint32_t *p_target_len);
#endif

#endif
