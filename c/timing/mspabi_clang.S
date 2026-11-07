; SPDX-FileCopyrightText: 2026 Simon Bjerg
; SPDX-License-Identifier: MIT
;
; The MSP430 EABI helpers clang calls (no libgcc for clang): 16- and
; 32-bit shifts and multiplies, a bit at a time, as small as they come.
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
        .global __mspabi_mpyi   ; r12 *= r13
__mspabi_mpyi:
        mov r12, r14
        clr r12
1:      tst r13
        jz 2f
        bit #1, r13
        jz 3f
        add r14, r12
3:      rla r14
        clrc
        rrc r13
        jmp 1b
2:      ret
        .global __mspabi_mpyl   ; r13:r12 *= r15:r14
__mspabi_mpyl:
        push r8
        push r10
        mov r12, r10
        mov r13, r11
        clr r12
        clr r13
1:      mov r14, r8
        bis r15, r8
        tst r8
        jz 2f
        bit #1, r14
        jz 3f
        add r10, r12
        addc r11, r13
3:      rla r10
        rlc r11
        clrc
        rrc r15
        rrc r14
        jmp 1b
2:      pop r10
        pop r8
        ret
