/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* MSP430 startup for the timing builds (msp430.ld): stack, .data, .bss,
 * main. timing_done turns the CPU off, which stops mspdebug's simulator
 * counting. */
extern int main(void);
extern char __data_load[], __data_start[], __data_end[], __bss_start[], __bss_end[];

void timing_init(void) {
    char *p_from = __data_load;
    for (char *p = __data_start; p < __data_end; p++) {
        *p = *p_from++;
    }
    for (char *p = __bss_start; p < __bss_end; p++) {
        *p = 0;
    }
}

__attribute__((naked, section(".text.start"))) void _start(void) {
    __asm__ volatile("mov #0x3E00, r1\n call #timing_init\n call #main\n 1: jmp 1b");
}

void timing_done(void) {
    __asm__ volatile("bis #0x10, r2"); /* CPUOFF */
    for (;;) {
    }
}
