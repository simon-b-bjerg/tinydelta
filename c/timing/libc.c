/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* The C library routines the decoders use, plain, for the timing builds
 * (which link no C library): a device's own are usually faster. */
#include <stddef.h>

void *memcpy(void *p_to, const void *p_from, size_t n) {
    unsigned char *p_d       = p_to;
    const unsigned char *p_s = p_from;
    while (n-- > 0u) {
        *p_d++ = *p_s++;
    }
    return p_to;
}

void *memmove(void *p_to, const void *p_from, size_t n) {
    unsigned char *p_d       = p_to;
    const unsigned char *p_s = p_from;
    if (p_d < p_s) {
        while (n-- > 0u) {
            *p_d++ = *p_s++;
        }
    } else {
        while (n-- > 0u) {
            p_d[n] = p_s[n];
        }
    }
    return p_to;
}

void *memset(void *p_to, int value, size_t n) {
    unsigned char *p_d = p_to;
    while (n-- > 0u) {
        *p_d++ = (unsigned char)value;
    }
    return p_to;
}
