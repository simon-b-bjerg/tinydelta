; SPDX-FileCopyrightText: 2026 Simon Bjerg
; SPDX-License-Identifier: MIT
;
; The MSP430 EABI helpers clang calls (no libgcc for clang): 16- and
; 32-bit shifts, a bit at a time (MSP430 has no barrel shifter), and
; multiplies on the hardware multiplier.
        .text
        .global __mspabi_slll
__mspabi_slll:  ; r13:r12 <<= r14
        tst r14
        jz 2f
1:      rla r12
        rlc r13
        dec r14
        jnz 1b
2:      ret
        .global __mspabi_srll
__mspabi_srll:
        tst r14
        jz 2f
1:      clrc
        rrc r13
        rrc r12
        dec r14
        jnz 1b
2:      ret
        .global __mspabi_sral
__mspabi_sral:
        tst r14
        jz 2f
1:      rra r13
        rrc r12
        dec r14
        jnz 1b
2:      ret
        .global __mspabi_slli
__mspabi_slli:  ; r12 <<= r13
        tst r13
        jz 2f
1:      rla r12
        dec r13
        jnz 1b
2:      ret
        .global __mspabi_srli
__mspabi_srli:
        tst r13
        jz 2f
1:      clrc
        rrc r12
        dec r13
        jnz 1b
2:      ret
        .global __mspabi_srai
__mspabi_srai:
        tst r13
        jz 2f
1:      rra r12
        dec r13
        jnz 1b
2:      ret
; Multiplies on the hardware multiplier (MPY 0x130, OP2 0x138, RESLO 0x13A,
; RESHI 0x13C), as the MSP430s with one have it and mspdebug's simulator
; emulates (simio add hwmult).
        .global __mspabi_mpyi   ; r12 *= r13
__mspabi_mpyi:
        mov r12, &0x0130
        mov r13, &0x0138
        mov &0x013A, r12
        ret
        .global __mspabi_mpyl   ; r13:r12 *= r15:r14, the low 32 bits
__mspabi_mpyl:
        mov r12, &0x0130        ; aL * bH
        mov r15, &0x0138
        mov &0x013A, r11
        mov r13, &0x0130        ; + aH * bL
        mov r14, &0x0138
        add &0x013A, r11
        mov r12, &0x0130        ; aL * bL, the cross terms into its high half
        mov r14, &0x0138
        mov &0x013A, r12
        mov &0x013C, r13
        add r11, r13
        ret
