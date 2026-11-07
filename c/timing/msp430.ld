/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */
/* Classic MSP430, 64 KB: RAM low, code and constants from 0x4000. */
MEMORY { RAM (rwx) : ORIGIN = 0x0200, LENGTH = 0x3C00  ROM (rx) : ORIGIN = 0x4000, LENGTH = 0xBFE0  VEC : ORIGIN = 0xFFFE, LENGTH = 2 }
SECTIONS {
  .text : { *(.text.start) *(.text*) *(.rodata*) *(.lower.text*) *(.lower.rodata*) } > ROM
  .data : { __data_start = .; *(.data*) *(.lower.data*) __data_end = .; } > RAM AT > ROM
  __data_load = LOADADDR(.data);
  .bss : { __bss_start = .; *(.bss*) *(.lower.bss*) *(COMMON) __bss_end = .; } > RAM
  .vectors : { SHORT(_start) } > VEC
}
