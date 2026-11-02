# tinydelta patch sizes

Tools: tinydelta (tinydelta v0.0.0-20261101231348-4345e5bc6698); openvcdiff (open-vcdiff 868f459a8d815125c2457f8c74b12493853100f9); xdelta3 (Xdelta version 3.0.11); bsdiff (bsdiff 4.3-23); detools (detools 0.53.0); hpatchlite (HPatchLite v1.0.2); zstd (Zstandard CLI (64-bit) v1.5.7)

Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. Geometric mean is the right average for ratios; "smaller" is the share of pairs where tinydelta is smaller. tinydelta is its coded patch with 777 B of coder tables: per pair, the smaller of the plain patch and the coded ones that fit, as the encoder sends it. Every variant is listed on its own in the patch-size tables.

## firmware, 185–364 KB, 1 commit apart

96 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11, micropython_samd/samd-ADAFRUIT_TRINKET_M0, micropython_samd/samd-SEEED_XIAO_SAMD21; median target 207,492 bytes.

Two-pass is the smaller in 93% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.12 | 0.10 | 0.08-0.27 | 100% |
| open-vcdiff, zlib-compressed | 0.17 | 0.15 | 0.12-0.28 | 100% |
| xdelta3 | 0.20 | 0.18 | 0.14-0.31 | 100% |
| bsdiff | 0.76 | 0.94 | 0.25-1.06 | 71% |
| detools (heatshrink) | 0.29 | 0.53 | 0.02-0.64 | 100% |
| HPatchLite, 4 KB dict. | 0.67 | 0.70 | 0.56-0.79 | 100% |
| HPatchLite, 32 KB dict. | 0.69 | 0.73 | 0.59-0.82 | 100% |
| zstd --patch-from | 0.47 | 0.46 | 0.39-0.57 | 100% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 1.97% | 0.03% | 3.17% |
| tinydelta, plain or two-pass | 3.14% | 0.03% | 4.84% |
| tinydelta, plain | 8.52% | 0.04% | 10.86% |
| tinydelta, plain (greedy) | 8.52% | 0.04% | 10.86% |
| tinydelta, two-pass | 3.14% | 0.03% | 4.84% |
| tinydelta, two-pass, 1 KB window | 3.34% | 0.03% | 5.28% |
| tinydelta, coded, 522 B tables | 2.10% | 0.03% | 3.32% |
| tinydelta, coded, 1287 B tables | 1.97% | 0.03% | 3.16% |
| tinydelta, plain, copies at most 256 B back | 8.68% | 0.04% | 11.12% |
| tinydelta, plain, copies at most 1 KB back | 8.62% | 0.04% | 11.01% |
| tinydelta, plain, copies at most 4 KB back | 8.55% | 0.04% | 10.92% |
| open-vcdiff | 21.16% | 0.10% | 28.74% |
| open-vcdiff, zlib-compressed | 13.58% | 0.11% | 19.71% |
| xdelta3 | 11.93% | 0.08% | 14.93% |
| bsdiff | 2.05% | 0.12% | 3.10% |
| detools (heatshrink) | 3.73% | 1.59% | 5.24% |
| HPatchLite, 4 KB dict. | 2.84% | 0.05% | 4.09% |
| HPatchLite, 32 KB dict. | 2.71% | 0.05% | 3.93% |
| zstd --patch-from | 4.56% | 0.05% | 6.32% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 4,436 | 60 | 7,224 |
| tinydelta, plain or two-pass | 7,539 | 62 | 10,713 |
| tinydelta, plain | 18,770 | 74 | 30,521 |
| tinydelta, plain (greedy) | 18,770 | 74 | 30,521 |
| tinydelta, two-pass | 7,539 | 62 | 10,713 |
| tinydelta, two-pass, 1 KB window | 8,236 | 63 | 11,542 |
| tinydelta, coded, 522 B tables | 4,756 | 60 | 7,678 |
| tinydelta, coded, 1287 B tables | 4,430 | 60 | 7,214 |
| tinydelta, plain, copies at most 256 B back | 19,112 | 74 | 31,037 |
| tinydelta, plain, copies at most 1 KB back | 18,952 | 74 | 30,758 |
| tinydelta, plain, copies at most 4 KB back | 18,818 | 74 | 30,576 |
| open-vcdiff | 49,011 | 214 | 68,906 |
| open-vcdiff, zlib-compressed | 31,368 | 217 | 42,918 |
| xdelta3 | 26,447 | 158 | 41,049 |
| bsdiff | 4,653 | 242 | 6,989 |
| detools (heatshrink) | 8,207 | 3,634 | 12,842 |
| HPatchLite, 4 KB dict. | 6,345 | 117 | 10,513 |
| HPatchLite, 32 KB dict. | 6,065 | 117 | 10,049 |
| zstd --patch-from | 9,684 | 104 | 16,388 |

## firmware, 185–364 KB, 5 commits apart

80 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11, micropython_samd/samd-ADAFRUIT_TRINKET_M0, micropython_samd/samd-SEEED_XIAO_SAMD21; median target 207,492 bytes.

Two-pass is the smaller in 100% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.12 | 0.12 | 0.09-0.16 | 100% |
| open-vcdiff, zlib-compressed | 0.18 | 0.17 | 0.14-0.23 | 100% |
| xdelta3 | 0.24 | 0.24 | 0.18-0.28 | 100% |
| bsdiff | 0.97 | 0.97 | 0.88-1.11 | 62% |
| detools (heatshrink) | 0.63 | 0.64 | 0.56-0.70 | 100% |
| HPatchLite, 4 KB dict. | 0.78 | 0.79 | 0.69-0.86 | 100% |
| HPatchLite, 32 KB dict. | 0.80 | 0.81 | 0.71-0.90 | 100% |
| zstd --patch-from | 0.55 | 0.55 | 0.49-0.61 | 100% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 3.89% | 2.53% | 5.09% |
| tinydelta, plain or two-pass | 6.01% | 4.13% | 7.76% |
| tinydelta, plain | 12.01% | 9.81% | 14.09% |
| tinydelta, plain (greedy) | 12.01% | 9.81% | 14.09% |
| tinydelta, two-pass | 6.01% | 4.13% | 7.76% |
| tinydelta, two-pass, 1 KB window | 6.38% | 4.51% | 8.20% |
| tinydelta, coded, 522 B tables | 4.04% | 2.68% | 5.28% |
| tinydelta, coded, 1287 B tables | 3.87% | 2.52% | 5.06% |
| tinydelta, plain, copies at most 256 B back | 12.19% | 9.98% | 14.39% |
| tinydelta, plain, copies at most 1 KB back | 12.13% | 9.92% | 14.28% |
| tinydelta, plain, copies at most 4 KB back | 12.06% | 9.86% | 14.15% |
| open-vcdiff | 32.68% | 24.18% | 39.69% |
| open-vcdiff, zlib-compressed | 22.54% | 15.72% | 26.99% |
| xdelta3 | 16.62% | 13.18% | 19.58% |
| bsdiff | 3.99% | 2.72% | 5.23% |
| detools (heatshrink) | 6.08% | 4.53% | 7.43% |
| HPatchLite, 4 KB dict. | 4.87% | 3.51% | 6.35% |
| HPatchLite, 32 KB dict. | 4.74% | 3.38% | 6.18% |
| zstd --patch-from | 7.16% | 5.28% | 8.91% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 8,794 | 5,040 | 14,564 |
| tinydelta, plain or two-pass | 13,988 | 8,624 | 18,823 |
| tinydelta, plain | 25,406 | 19,598 | 39,346 |
| tinydelta, plain (greedy) | 25,406 | 19,598 | 39,346 |
| tinydelta, two-pass | 13,988 | 8,624 | 18,823 |
| tinydelta, two-pass, 1 KB window | 14,888 | 9,337 | 20,439 |
| tinydelta, coded, 522 B tables | 9,150 | 5,335 | 14,971 |
| tinydelta, coded, 1287 B tables | 8,746 | 5,023 | 14,492 |
| tinydelta, plain, copies at most 256 B back | 25,947 | 19,895 | 40,581 |
| tinydelta, plain, copies at most 1 KB back | 25,732 | 19,756 | 40,058 |
| tinydelta, plain, copies at most 4 KB back | 25,515 | 19,627 | 39,650 |
| open-vcdiff | 67,466 | 53,949 | 95,254 |
| open-vcdiff, zlib-compressed | 46,892 | 37,174 | 64,805 |
| xdelta3 | 34,122 | 27,607 | 51,347 |
| bsdiff | 9,212 | 5,636 | 13,073 |
| detools (heatshrink) | 13,546 | 9,011 | 20,816 |
| HPatchLite, 4 KB dict. | 10,958 | 6,913 | 17,200 |
| HPatchLite, 32 KB dict. | 10,686 | 6,653 | 16,648 |
| zstd --patch-from | 15,526 | 10,108 | 25,396 |

## configuration, 3.1–20 KB, 1 commit apart

180 pairs from defconfig/at91_dt_defconfig, defconfig/bcm2835_defconfig, defconfig/lpc32xx_defconfig, defconfig/shmobile_defconfig, defconfig/sunxi_defconfig, defconfig/u8500_defconfig, dtb_mcu/lpc4357-ea4357-devkit, dtb_mcu/stm32f746-disco, dtb_mcu/stm32f769-disco, dtb_mcu/stm32h743i-disco, dtb_mcu/stm32h750i-art-pi; median target 4,885 bytes.

Two-pass is the smaller in 11% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.33 | 0.34 | 0.23-0.48 | 100% |
| open-vcdiff, zlib-compressed | 0.34 | 0.33 | 0.21-0.53 | 100% |
| xdelta3 | 0.24 | 0.24 | 0.10-0.51 | 100% |
| bsdiff | 0.13 | 0.12 | 0.05-0.39 | 100% |
| detools (heatshrink) | 0.17 | 0.15 | 0.08-0.44 | 100% |
| HPatchLite, 4 KB dict. | 0.57 | 0.59 | 0.40-0.78 | 97% |
| HPatchLite, 32 KB dict. | 0.57 | 0.58 | 0.40-0.81 | 97% |
| zstd --patch-from | 0.50 | 0.53 | 0.25-0.86 | 96% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 0.35% | 0.14% | 2.00% |
| tinydelta, plain or two-pass | 0.35% | 0.14% | 2.04% |
| tinydelta, plain | 0.35% | 0.14% | 2.06% |
| tinydelta, plain (greedy) | 0.38% | 0.15% | 2.27% |
| tinydelta, two-pass | 0.35% | 0.14% | 2.04% |
| tinydelta, two-pass, 1 KB window | 0.35% | 0.14% | 2.05% |
| tinydelta, coded, 522 B tables | 0.35% | 0.14% | 2.00% |
| tinydelta, coded, 1287 B tables | 0.35% | 0.14% | 1.99% |
| tinydelta, plain, copies at most 256 B back | 0.35% | 0.14% | 2.11% |
| tinydelta, plain, copies at most 1 KB back | 0.35% | 0.14% | 2.07% |
| tinydelta, plain, copies at most 4 KB back | 0.35% | 0.14% | 2.06% |
| open-vcdiff | 0.92% | 0.52% | 5.67% |
| open-vcdiff, zlib-compressed | 1.06% | 0.61% | 3.84% |
| xdelta3 | 1.72% | 0.68% | 3.94% |
| bsdiff | 3.50% | 1.19% | 6.08% |
| detools (heatshrink) | 2.23% | 1.84% | 3.87% |
| HPatchLite, 4 KB dict. | 0.65% | 0.31% | 2.56% |
| HPatchLite, 32 KB dict. | 0.65% | 0.31% | 2.54% |
| zstd --patch-from | 0.77% | 0.32% | 2.30% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 24 | 7 | 143 |
| tinydelta, plain or two-pass | 24 | 7 | 149 |
| tinydelta, plain | 24 | 7 | 149 |
| tinydelta, plain (greedy) | 26 | 7 | 154 |
| tinydelta, two-pass | 24 | 7 | 149 |
| tinydelta, two-pass, 1 KB window | 24 | 7 | 157 |
| tinydelta, coded, 522 B tables | 24 | 7 | 143 |
| tinydelta, coded, 1287 B tables | 24 | 7 | 136 |
| tinydelta, plain, copies at most 256 B back | 24 | 7 | 157 |
| tinydelta, plain, copies at most 1 KB back | 24 | 7 | 157 |
| tinydelta, plain, copies at most 4 KB back | 24 | 7 | 150 |
| open-vcdiff | 60 | 25 | 373 |
| open-vcdiff, zlib-compressed | 66 | 34 | 250 |
| xdelta3 | 96 | 71 | 255 |
| bsdiff | 190 | 148 | 353 |
| detools (heatshrink) | 124 | 89 | 442 |
| HPatchLite, 4 KB dict. | 42 | 16 | 218 |
| HPatchLite, 32 KB dict. | 42 | 16 | 218 |
| zstd --patch-from | 44 | 28 | 155 |

## configuration, 3.1–20 KB, 5 commits apart

136 pairs from defconfig/at91_dt_defconfig, defconfig/bcm2835_defconfig, defconfig/lpc32xx_defconfig, defconfig/shmobile_defconfig, defconfig/sunxi_defconfig, defconfig/u8500_defconfig, dtb_mcu/lpc4357-ea4357-devkit, dtb_mcu/stm32f746-disco, dtb_mcu/stm32f769-disco, dtb_mcu/stm32h743i-disco; median target 4,868 bytes.

Two-pass is the smaller in 22% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.39 | 0.43 | 0.25-0.55 | 100% |
| open-vcdiff, zlib-compressed | 0.53 | 0.56 | 0.41-0.65 | 100% |
| xdelta3 | 0.50 | 0.53 | 0.37-0.67 | 100% |
| bsdiff | 0.34 | 0.34 | 0.19-0.74 | 100% |
| detools (heatshrink) | 0.42 | 0.42 | 0.29-0.68 | 100% |
| HPatchLite, 4 KB dict. | 0.70 | 0.67 | 0.58-0.93 | 96% |
| HPatchLite, 32 KB dict. | 0.70 | 0.67 | 0.58-0.95 | 96% |
| zstd --patch-from | 0.84 | 0.86 | 0.65-1.07 | 81% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 2.10% | 0.91% | 5.24% |
| tinydelta, plain or two-pass | 2.14% | 0.91% | 5.72% |
| tinydelta, plain | 2.14% | 0.91% | 6.48% |
| tinydelta, plain (greedy) | 2.32% | 0.94% | 7.16% |
| tinydelta, two-pass | 2.14% | 0.91% | 5.72% |
| tinydelta, two-pass, 1 KB window | 2.15% | 0.91% | 5.92% |
| tinydelta, coded, 522 B tables | 2.10% | 0.91% | 5.24% |
| tinydelta, coded, 1287 B tables | 2.03% | 0.91% | 4.95% |
| tinydelta, plain, copies at most 256 B back | 2.15% | 0.92% | 6.88% |
| tinydelta, plain, copies at most 1 KB back | 2.15% | 0.91% | 6.74% |
| tinydelta, plain, copies at most 4 KB back | 2.14% | 0.91% | 6.57% |
| open-vcdiff | 4.97% | 1.99% | 19.15% |
| open-vcdiff, zlib-compressed | 3.77% | 2.09% | 8.80% |
| xdelta3 | 3.86% | 2.53% | 8.66% |
| bsdiff | 6.07% | 4.51% | 11.32% |
| detools (heatshrink) | 4.76% | 3.12% | 9.62% |
| HPatchLite, 4 KB dict. | 3.19% | 1.50% | 7.09% |
| HPatchLite, 32 KB dict. | 3.19% | 1.50% | 7.09% |
| zstd --patch-from | 2.60% | 1.42% | 4.99% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 120 | 46 | 766 |
| tinydelta, plain or two-pass | 122 | 46 | 918 |
| tinydelta, plain | 124 | 46 | 1,050 |
| tinydelta, plain (greedy) | 134 | 48 | 1,139 |
| tinydelta, two-pass | 122 | 46 | 918 |
| tinydelta, two-pass, 1 KB window | 124 | 46 | 927 |
| tinydelta, coded, 522 B tables | 120 | 46 | 790 |
| tinydelta, coded, 1287 B tables | 118 | 46 | 747 |
| tinydelta, plain, copies at most 256 B back | 126 | 46 | 1,092 |
| tinydelta, plain, copies at most 1 KB back | 125 | 46 | 1,070 |
| tinydelta, plain, copies at most 4 KB back | 124 | 46 | 1,065 |
| open-vcdiff | 284 | 96 | 3,266 |
| open-vcdiff, zlib-compressed | 212 | 101 | 1,394 |
| xdelta3 | 210 | 121 | 1,384 |
| bsdiff | 348 | 242 | 1,033 |
| detools (heatshrink) | 286 | 148 | 1,074 |
| HPatchLite, 4 KB dict. | 174 | 74 | 794 |
| HPatchLite, 32 KB dict. | 174 | 74 | 788 |
| zstd --patch-from | 138 | 68 | 765 |

## large configuration, 47–107 KB, 1 commit apart

24 pairs from dtb/imx8mp-evk, dtb/k3-am625-sk, dtb/rk3588-rock-5b; median target 62,786 bytes.

Two-pass is the smaller in 67% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.18 | 0.20 | 0.10-0.32 | 100% |
| open-vcdiff, zlib-compressed | 0.30 | 0.29 | 0.23-0.42 | 100% |
| xdelta3 | 0.27 | 0.26 | 0.21-0.40 | 100% |
| bsdiff | 0.38 | 0.59 | 0.13-1.00 | 88% |
| detools (heatshrink) | 0.12 | 0.25 | 0.02-0.56 | 100% |
| HPatchLite, 4 KB dict. | 0.65 | 0.72 | 0.46-0.90 | 100% |
| HPatchLite, 32 KB dict. | 0.66 | 0.75 | 0.46-0.94 | 96% |
| zstd --patch-from | 0.60 | 0.59 | 0.51-0.78 | 100% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 0.52% | 0.03% | 1.95% |
| tinydelta, plain or two-pass | 0.72% | 0.03% | 2.45% |
| tinydelta, plain | 1.47% | 0.03% | 4.82% |
| tinydelta, plain (greedy) | 1.58% | 0.03% | 4.82% |
| tinydelta, two-pass | 0.72% | 0.03% | 2.45% |
| tinydelta, two-pass, 1 KB window | 0.77% | 0.03% | 2.60% |
| tinydelta, coded, 522 B tables | 0.56% | 0.03% | 2.13% |
| tinydelta, coded, 1287 B tables | 0.52% | 0.03% | 1.94% |
| tinydelta, plain, copies at most 256 B back | 1.63% | 0.03% | 5.32% |
| tinydelta, plain, copies at most 1 KB back | 1.47% | 0.03% | 5.01% |
| tinydelta, plain, copies at most 4 KB back | 1.47% | 0.03% | 4.90% |
| open-vcdiff | 4.27% | 0.10% | 15.15% |
| open-vcdiff, zlib-compressed | 2.08% | 0.09% | 5.72% |
| xdelta3 | 2.19% | 0.11% | 6.04% |
| bsdiff | 0.79% | 0.22% | 2.11% |
| detools (heatshrink) | 2.01% | 1.60% | 3.52% |
| HPatchLite, 4 KB dict. | 0.72% | 0.06% | 2.18% |
| HPatchLite, 32 KB dict. | 0.70% | 0.06% | 2.09% |
| zstd --patch-from | 0.90% | 0.05% | 2.83% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 305 | 21 | 1,563 |
| tinydelta, plain or two-pass | 458 | 20 | 1,899 |
| tinydelta, plain | 718 | 21 | 4,830 |
| tinydelta, plain (greedy) | 774 | 23 | 4,830 |
| tinydelta, two-pass | 458 | 20 | 1,899 |
| tinydelta, two-pass, 1 KB window | 458 | 20 | 2,080 |
| tinydelta, coded, 522 B tables | 324 | 21 | 1,665 |
| tinydelta, coded, 1287 B tables | 304 | 21 | 1,543 |
| tinydelta, plain, copies at most 256 B back | 795 | 22 | 5,297 |
| tinydelta, plain, copies at most 1 KB back | 719 | 22 | 5,017 |
| tinydelta, plain, copies at most 4 KB back | 719 | 22 | 4,903 |
| open-vcdiff | 2,302 | 75 | 15,192 |
| open-vcdiff, zlib-compressed | 1,227 | 64 | 5,720 |
| xdelta3 | 1,150 | 96 | 6,022 |
| bsdiff | 495 | 174 | 1,585 |
| detools (heatshrink) | 1,702 | 994 | 2,955 |
| HPatchLite, 4 KB dict. | 401 | 41 | 1,770 |
| HPatchLite, 32 KB dict. | 396 | 41 | 1,698 |
| zstd --patch-from | 500 | 40 | 2,485 |

## large configuration, 47–107 KB, 5 commits apart

14 pairs from dtb/imx8mp-evk, dtb/rk3588-rock-5b; median target 62,802 bytes.

Two-pass is the smaller in 100% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.15 | 0.16 | 0.11-0.23 | 100% |
| open-vcdiff, zlib-compressed | 0.36 | 0.35 | 0.28-0.50 | 100% |
| xdelta3 | 0.34 | 0.34 | 0.27-0.45 | 100% |
| bsdiff | 0.85 | 0.85 | 0.75-0.96 | 100% |
| detools (heatshrink) | 0.53 | 0.53 | 0.50-0.61 | 100% |
| HPatchLite, 4 KB dict. | 0.84 | 0.86 | 0.82-0.92 | 100% |
| HPatchLite, 32 KB dict. | 0.87 | 0.89 | 0.84-0.95 | 100% |
| zstd --patch-from | 0.74 | 0.75 | 0.64-0.88 | 100% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 1.82% | 1.61% | 2.97% |
| tinydelta, plain or two-pass | 2.49% | 1.98% | 3.97% |
| tinydelta, plain | 5.04% | 3.73% | 5.35% |
| tinydelta, plain (greedy) | 5.04% | 3.73% | 5.35% |
| tinydelta, two-pass | 2.49% | 1.98% | 3.97% |
| tinydelta, two-pass, 1 KB window | 2.53% | 2.19% | 4.05% |
| tinydelta, coded, 522 B tables | 1.89% | 1.71% | 3.04% |
| tinydelta, coded, 1287 B tables | 1.78% | 1.59% | 2.89% |
| tinydelta, plain, copies at most 256 B back | 5.51% | 3.75% | 5.91% |
| tinydelta, plain, copies at most 1 KB back | 5.20% | 3.76% | 5.49% |
| tinydelta, plain, copies at most 4 KB back | 5.13% | 3.75% | 5.43% |
| open-vcdiff | 13.01% | 8.77% | 16.09% |
| open-vcdiff, zlib-compressed | 5.71% | 4.29% | 6.00% |
| xdelta3 | 6.05% | 4.85% | 6.56% |
| bsdiff | 2.19% | 1.69% | 3.50% |
| detools (heatshrink) | 3.49% | 3.15% | 4.86% |
| HPatchLite, 4 KB dict. | 2.13% | 1.94% | 3.39% |
| HPatchLite, 32 KB dict. | 2.06% | 1.86% | 3.30% |
| zstd --patch-from | 2.58% | 2.14% | 3.37% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 1,753 | 1,034 | 2,053 |
| tinydelta, plain or two-pass | 2,170 | 1,476 | 2,537 |
| tinydelta, plain | 3,307 | 2,364 | 5,687 |
| tinydelta, plain (greedy) | 3,307 | 2,364 | 5,687 |
| tinydelta, two-pass | 2,170 | 1,476 | 2,537 |
| tinydelta, two-pass, 1 KB window | 2,374 | 1,484 | 2,632 |
| tinydelta, coded, 522 B tables | 1,840 | 1,064 | 2,164 |
| tinydelta, coded, 1287 B tables | 1,718 | 1,018 | 2,020 |
| tinydelta, plain, copies at most 256 B back | 3,476 | 2,417 | 6,326 |
| tinydelta, plain, copies at most 1 KB back | 3,364 | 2,392 | 5,876 |
| tinydelta, plain, copies at most 4 KB back | 3,317 | 2,378 | 5,804 |
| open-vcdiff | 8,172 | 5,900 | 17,213 |
| open-vcdiff, zlib-compressed | 3,752 | 2,827 | 6,395 |
| xdelta3 | 4,110 | 3,066 | 6,838 |
| bsdiff | 1,848 | 1,281 | 2,225 |
| detools (heatshrink) | 3,035 | 2,084 | 3,757 |
| HPatchLite, 4 KB dict. | 2,068 | 1,240 | 2,298 |
| HPatchLite, 32 KB dict. | 2,002 | 1,191 | 2,221 |
| zstd --patch-from | 2,113 | 1,369 | 2,858 |
