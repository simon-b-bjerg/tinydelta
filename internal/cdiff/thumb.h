/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* A second build of the C library in this package, as a Cortex-M device
 * would have it: the Thumb rules built in, no shipped tables. Its public
 * names get a thumb_ prefix so the two builds link side by side. */
#undef TINYDELTA_RELOC
#undef TINYDELTA_RELOC_SHIPPED
#define TINYDELTA_RELOC 1

#define tinydelta_coder_byte        thumb_tinydelta_coder_byte
#define tinydelta_coder_init        thumb_tinydelta_coder_init
#define tinydelta_coder_tables_size thumb_tinydelta_coder_tables_size
#define tinydelta_decode            thumb_tinydelta_decode
#define tinydelta_decode_coded      thumb_tinydelta_decode_coded
#define tinydelta_decode_io         thumb_tinydelta_decode_io
#define tinydelta_reloc_read_header thumb_tinydelta_reloc_read_header
#define tinydelta_reloc_span        thumb_tinydelta_reloc_span
#define tinydelta_reloc_start       thumb_tinydelta_reloc_start
