/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* Cortex-M startup for the timing builds (arm.ld): the vector table's
 * stack pointer and reset, copying .data and clearing .bss. The emulator
 * stops at timing_done. */
#include <stdint.h>

extern int main(void);
extern uint32_t __stack_top[], __data_load[], __data_start[], __data_end[], __bss_start[], __bss_end[];

void reset(void) {
    uint32_t *p_from = __data_load;
    for (uint32_t *p = __data_start; p < __data_end; p++) {
        *p = *p_from++;
    }
    for (uint32_t *p = __bss_start; p < __bss_end; p++) {
        *p = 0u;
    }
    (void)main();
    for (;;) {
    }
}

__attribute__((noinline)) void timing_done(void) {
    for (;;) {
    }
}

__attribute__((section(".vectors"), used)) const void *const vectors[2] = {__stack_top, (const void *)reset};
