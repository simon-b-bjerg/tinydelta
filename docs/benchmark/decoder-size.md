| decoder | code (.text, Cortex-M0, -Os) | largest stack frames |
|---|---:|---|
| tinydelta-callback | 718 B | tinydelta_decode_io 152 B; copy_to_target 32 B; read_varint 32 B;  |
| tinydelta-cache | 300 B | tinydelta_cache_read 32 B; tinydelta_cache_write 24 B; tinydelta_cache_flush 16 B;  |
| hpatchlite | 3250 B | hpatch_lite_patch 80 B; tuz_decompress_mem 64 B; hpatchi_inplaceB 56 B;  |
| hpatchlite-patch-only | 1683 B | hpatch_lite_patch 80 B; hpatchi_inplaceB 56 B; hpatchi_inplace_open 56 B;  |

Compiler: arm-none-eabi-gcc (15:13.2.rel1-2) 13.2.1 20231009. HPatchLite: HDiffPatch/libHDiffPatch/HPatchLite/hpatch_lite.c, tinyuz/decompress/tuz_dec.c.
