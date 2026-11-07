/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* A third build of the C library in this package, as an MSP430X device
 * would have it: the MSP430X rules built in, no shipped tables, so its
 * rules are matched by the code generated with them. Its public names get
 * an msp430x_ prefix so the builds link side by side. */
#undef TINYDELTA_RELOC
#undef TINYDELTA_RELOC_SHIPPED
#define TINYDELTA_RELOC 2

#define tinydelta_coder_byte        msp430x_tinydelta_coder_byte
#define tinydelta_coder_init        msp430x_tinydelta_coder_init
#define tinydelta_coder_tables_size msp430x_tinydelta_coder_tables_size
#define tinydelta_decode            msp430x_tinydelta_decode
#define tinydelta_decode_coded      msp430x_tinydelta_decode_coded
#define tinydelta_decode_io         msp430x_tinydelta_decode_io
#define tinydelta_reloc_read_header msp430x_tinydelta_reloc_read_header
#define tinydelta_reloc_span        msp430x_tinydelta_reloc_span
#define tinydelta_reloc_start       msp430x_tinydelta_reloc_start
