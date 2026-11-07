/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* The benchmark's firmware images (bench/collect.py, collector "mbedtls"):
 * code, then constants, then the initial values of variables, in one flash
 * region from 0x10000 (the image's base), and the variables in RAM at
 * 0x1C00, as on an MSP430FR. The section names cover MSP430 GCC's
 * -mlarge (.lower, .upper, .either) and other targets'. */
ENTRY(main)

MEMORY {
    FLASH (rx) : ORIGIN = 0x10000, LENGTH = 0x70000
    RAM (rw)   : ORIGIN = 0x1C00, LENGTH = 0x2000
}

SECTIONS {
    .text : {
        *(.text .text.* .lower.text .lower.text.* .upper.text .upper.text.* .either.text .either.text.*)
    } > FLASH
    .rodata : {
        KEEP(*(.rodata.bench_commands .lower.rodata.bench_commands .upper.rodata.bench_commands .either.rodata.bench_commands))
        *(.rodata .rodata.* .lower.rodata .lower.rodata.* .upper.rodata .upper.rodata.* .either.rodata .either.rodata.* .const .const.*)
    } > FLASH
    .data : {
        *(.data .data.* .lower.data .lower.data.* .upper.data .upper.data.* .either.data .either.data.*)
    } > RAM AT > FLASH
    .bss (NOLOAD) : {
        *(.bss .bss.* .lower.bss .lower.bss.* .upper.bss .upper.bss.* .either.bss .either.bss.* COMMON)
    } > RAM
    /DISCARD/ : {
        *(.comment .note .note.* .ARM.attributes .ARM.exidx* .ARM.extab* .MSP430.attributes .MP430.attributes)
    }
}
