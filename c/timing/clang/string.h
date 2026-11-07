/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* For the clang MSP430 timing build, which has no C library headers
 * (c/timing/libc.c has the routines). */
#ifndef TIMING_STRING_H
#define TIMING_STRING_H
#include <stddef.h>
void *memcpy(void *p_to, const void *p_from, size_t n);
void *memmove(void *p_to, const void *p_from, size_t n);
void *memset(void *p_to, int value, size_t n);
#endif
