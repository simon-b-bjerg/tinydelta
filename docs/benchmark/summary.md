# tinydelta patch sizes

Tools: tinydelta (tinydelta v0.0.0-20261105221618-c81a2ef59301); openvcdiff (open-vcdiff 868f459a8d815125c2457f8c74b12493853100f9); xdelta3 (Xdelta version 3.0.11); bsdiff (bsdiff 4.3-23); detools (detools 0.53.0); hpatchlite (HPatchLite v1.0.2); zstd (Zstandard CLI (64-bit) v1.5.7)

Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. Geometric mean is the right average for ratios; "smaller" is the share of pairs where tinydelta is smaller. tinydelta is its coded patch with 777 B of coder tables: per pair, the smaller of the plain patch and the coded ones that fit, as the encoder sends it. Every variant is listed on its own in the patch-size tables.

## firmware, 112–364 KB, 1 commit apart

112 pairs from mbedtls_msp430/msp430x-large, micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11, micropython_samd/samd-ADAFRUIT_TRINKET_M0, micropython_samd/samd-SEEED_XIAO_SAMD21; median target 185,736 bytes.

Two-pass is the smaller in 91% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.12 | 0.10 | 0.08-0.27 | 100% |
| open-vcdiff, zlib-compressed | 0.17 | 0.15 | 0.12-0.27 | 100% |
| xdelta3 | 0.20 | 0.19 | 0.14-0.31 | 100% |
| bsdiff | 0.72 | 0.94 | 0.23-1.06 | 71% |
| detools (heatshrink) | 0.27 | 0.53 | 0.02-0.64 | 100% |
| HPatchLite, 4 KB dict. | 0.66 | 0.70 | 0.55-0.83 | 100% |
| HPatchLite, 32 KB dict. | 0.69 | 0.73 | 0.57-0.85 | 100% |
| zstd --patch-from | 0.48 | 0.47 | 0.39-0.62 | 100% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 1.91% | 0.03% | 3.24% |
| tinydelta, plain or two-pass | 3.06% | 0.03% | 4.73% |
| tinydelta, plain | 8.21% | 0.03% | 10.73% |
| tinydelta, plain (greedy) | 8.21% | 0.03% | 10.73% |
| tinydelta, two-pass | 3.06% | 0.03% | 4.73% |
| tinydelta, two-pass, 1 KB window | 3.26% | 0.03% | 5.18% |
| tinydelta, coded, 522 B tables | 2.07% | 0.03% | 3.35% |
| tinydelta, coded, 1287 B tables | 1.91% | 0.03% | 3.22% |
| tinydelta, coded and relocated (firmware), 1545 B tables | 0.26% | 0.02% | 1.71% |
| tinydelta, coded and relocated, rule table in the patch | 0.35% | 0.03% | 1.73% |
| tinydelta, plain, copies at most 256 B back | 8.37% | 0.03% | 11.00% |
| tinydelta, plain, copies at most 1 KB back | 8.29% | 0.03% | 10.88% |
| tinydelta, plain, copies at most 4 KB back | 8.24% | 0.03% | 10.76% |
| open-vcdiff | 20.62% | 0.10% | 28.59% |
| open-vcdiff, zlib-compressed | 12.81% | 0.10% | 19.24% |
| xdelta3 | 11.34% | 0.08% | 14.68% |
| bsdiff | 1.96% | 0.12% | 3.11% |
| detools (heatshrink) | 3.54% | 1.59% | 5.10% |
| HPatchLite, 4 KB dict. | 2.69% | 0.04% | 3.94% |
| HPatchLite, 32 KB dict. | 2.56% | 0.04% | 3.77% |
| zstd --patch-from | 4.21% | 0.04% | 6.23% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 4,020 | 50 | 7,058 |
| tinydelta, plain or two-pass | 6,720 | 53 | 10,450 |
| tinydelta, plain | 16,990 | 57 | 29,667 |
| tinydelta, plain (greedy) | 16,990 | 57 | 29,667 |
| tinydelta, two-pass | 6,720 | 53 | 10,450 |
| tinydelta, two-pass, 1 KB window | 7,250 | 53 | 11,205 |
| tinydelta, coded, 522 B tables | 4,284 | 50 | 7,503 |
| tinydelta, coded, 1287 B tables | 4,015 | 50 | 7,046 |
| tinydelta, coded and relocated (firmware), 1545 B tables | 484 | 41 | 6,224 |
| tinydelta, coded and relocated, rule table in the patch | 649 | 50 | 6,224 |
| tinydelta, plain, copies at most 256 B back | 17,356 | 57 | 30,227 |
| tinydelta, plain, copies at most 1 KB back | 17,196 | 57 | 29,990 |
| tinydelta, plain, copies at most 4 KB back | 17,056 | 57 | 29,774 |
| open-vcdiff | 42,209 | 191 | 68,262 |
| open-vcdiff, zlib-compressed | 27,540 | 198 | 42,218 |
| xdelta3 | 23,786 | 140 | 40,522 |
| bsdiff | 4,280 | 222 | 6,812 |
| detools (heatshrink) | 7,754 | 3,057 | 12,331 |
| HPatchLite, 4 KB dict. | 5,746 | 78 | 9,935 |
| HPatchLite, 32 KB dict. | 5,510 | 78 | 9,532 |
| zstd --patch-from | 9,390 | 82 | 15,633 |

## firmware, 112–364 KB, 5 commits apart

92 pairs from mbedtls_msp430/msp430x-large, micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11, micropython_samd/samd-ADAFRUIT_TRINKET_M0, micropython_samd/samd-SEEED_XIAO_SAMD21; median target 185,736 bytes.

Two-pass is the smaller in 100% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.12 | 0.12 | 0.09-0.15 | 100% |
| open-vcdiff, zlib-compressed | 0.18 | 0.19 | 0.14-0.24 | 100% |
| xdelta3 | 0.25 | 0.25 | 0.19-0.32 | 100% |
| bsdiff | 0.97 | 0.97 | 0.88-1.09 | 64% |
| detools (heatshrink) | 0.63 | 0.65 | 0.56-0.70 | 100% |
| HPatchLite, 4 KB dict. | 0.79 | 0.80 | 0.69-0.90 | 100% |
| HPatchLite, 32 KB dict. | 0.82 | 0.82 | 0.71-0.93 | 100% |
| zstd --patch-from | 0.57 | 0.57 | 0.49-0.65 | 100% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 3.76% | 2.52% | 5.08% |
| tinydelta, plain or two-pass | 5.70% | 4.00% | 7.74% |
| tinydelta, plain | 11.64% | 7.31% | 14.06% |
| tinydelta, plain (greedy) | 11.64% | 7.31% | 14.06% |
| tinydelta, two-pass | 5.70% | 4.00% | 7.74% |
| tinydelta, two-pass, 1 KB window | 6.13% | 4.21% | 8.13% |
| tinydelta, coded, 522 B tables | 3.93% | 2.68% | 5.26% |
| tinydelta, coded, 1287 B tables | 3.74% | 2.52% | 5.04% |
| tinydelta, coded and relocated (firmware), 1545 B tables | 0.95% | 0.41% | 3.72% |
| tinydelta, coded and relocated, rule table in the patch | 1.05% | 0.44% | 3.72% |
| tinydelta, plain, copies at most 256 B back | 11.96% | 7.47% | 14.35% |
| tinydelta, plain, copies at most 1 KB back | 11.82% | 7.40% | 14.23% |
| tinydelta, plain, copies at most 4 KB back | 11.71% | 7.36% | 14.12% |
| open-vcdiff | 30.98% | 23.02% | 39.56% |
| open-vcdiff, zlib-compressed | 21.19% | 14.29% | 26.92% |
| xdelta3 | 15.79% | 9.82% | 19.39% |
| bsdiff | 3.84% | 2.67% | 5.22% |
| detools (heatshrink) | 5.84% | 4.30% | 7.39% |
| HPatchLite, 4 KB dict. | 4.78% | 3.40% | 6.22% |
| HPatchLite, 32 KB dict. | 4.62% | 3.25% | 6.06% |
| zstd --patch-from | 6.97% | 4.44% | 8.59% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta, coded, 777 B tables | 8,347 | 3,942 | 14,102 |
| tinydelta, plain or two-pass | 12,818 | 6,082 | 18,615 |
| tinydelta, plain | 24,462 | 9,007 | 38,917 |
| tinydelta, plain (greedy) | 24,462 | 9,007 | 38,917 |
| tinydelta, two-pass | 12,818 | 6,082 | 18,615 |
| tinydelta, two-pass, 1 KB window | 13,618 | 6,414 | 20,091 |
| tinydelta, coded, 522 B tables | 8,670 | 4,235 | 14,505 |
| tinydelta, coded, 1287 B tables | 8,296 | 3,916 | 14,000 |
| tinydelta, coded and relocated (firmware), 1545 B tables | 1,700 | 837 | 13,529 |
| tinydelta, coded and relocated, rule table in the patch | 1,820 | 965 | 13,529 |
| tinydelta, plain, copies at most 256 B back | 24,955 | 9,210 | 39,731 |
| tinydelta, plain, copies at most 1 KB back | 24,749 | 9,117 | 39,401 |
| tinydelta, plain, copies at most 4 KB back | 24,586 | 9,073 | 39,118 |
| open-vcdiff | 65,818 | 29,861 | 95,130 |
| open-vcdiff, zlib-compressed | 46,018 | 16,142 | 63,669 |
| xdelta3 | 33,530 | 11,578 | 51,090 |
| bsdiff | 8,282 | 4,418 | 13,049 |
| detools (heatshrink) | 12,564 | 6,159 | 20,743 |
| HPatchLite, 4 KB dict. | 10,375 | 4,630 | 17,018 |
| HPatchLite, 32 KB dict. | 10,071 | 4,508 | 16,500 |
| zstd --patch-from | 14,870 | 5,748 | 23,770 |

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
| tinydelta, coded and relocated (firmware), 1545 B tables | 0.35% | 0.14% | 1.99% |
| tinydelta, coded and relocated, rule table in the patch | 0.35% | 0.14% | 1.99% |
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
| tinydelta, coded and relocated (firmware), 1545 B tables | 24 | 7 | 136 |
| tinydelta, coded and relocated, rule table in the patch | 24 | 7 | 136 |
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
| tinydelta, coded and relocated (firmware), 1545 B tables | 2.03% | 0.91% | 4.95% |
| tinydelta, coded and relocated, rule table in the patch | 2.03% | 0.91% | 4.95% |
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
| tinydelta, coded and relocated (firmware), 1545 B tables | 118 | 46 | 747 |
| tinydelta, coded and relocated, rule table in the patch | 118 | 46 | 747 |
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
| tinydelta, coded and relocated (firmware), 1545 B tables | 0.52% | 0.03% | 1.94% |
| tinydelta, coded and relocated, rule table in the patch | 0.52% | 0.03% | 1.94% |
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
| tinydelta, coded and relocated (firmware), 1545 B tables | 304 | 21 | 1,543 |
| tinydelta, coded and relocated, rule table in the patch | 304 | 21 | 1,543 |
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
| tinydelta, coded and relocated (firmware), 1545 B tables | 1.78% | 1.59% | 2.89% |
| tinydelta, coded and relocated, rule table in the patch | 1.78% | 1.59% | 2.89% |
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
| tinydelta, coded and relocated (firmware), 1545 B tables | 1,718 | 1,018 | 2,020 |
| tinydelta, coded and relocated, rule table in the patch | 1,718 | 1,018 | 2,020 |
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

## Relocation

Firmware with relocation rules: patch sizes summed over every pair, in bytes, map and header included. Tables are the decoder's tables buffer: the coder's tables, and for relocation its map (12 B per entry), plus the rule table when the patch ships it. The change is relocated against coded with 1287 B of tables.

| item | rules, base | step | pairs | coded, 777 B | coded, 1287 B | relocated, 1545 B | relocated, table shipped | change |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| mbedtls_msp430/msp430x-large | msp430x, 0x10000 | 1 | 16 | 26,970 | 26,839 | 5,294 | 11,556 | -80% |
| mbedtls_msp430/msp430x-large | msp430x, 0x10000 | 5 | 12 | 42,838 | 42,417 | 14,642 | 20,792 | -65% |
| micropython/stm32-NUCLEO_F091RC | thumb, 0x08000000 | 1 | 24 | 98,960 | 98,726 | 6,701 | 7,723 | -93% |
| micropython/stm32-NUCLEO_F091RC | thumb, 0x08000000 | 5 | 20 | 192,365 | 191,239 | 26,423 | 27,472 | -86% |
| micropython_samd/samd-ADAFRUIT_TRINKET_M0 | thumb, 0x2000 | 1 | 24 | 89,536 | 89,338 | 15,198 | 16,318 | -83% |
| micropython_samd/samd-ADAFRUIT_TRINKET_M0 | thumb, 0x2000 | 5 | 20 | 145,968 | 145,220 | 35,532 | 36,587 | -76% |
| micropython_samd/samd-SEEED_XIAO_SAMD21 | thumb, 0x2000 | 1 | 24 | 87,473 | 87,277 | 15,041 | 16,156 | -83% |
| micropython_samd/samd-SEEED_XIAO_SAMD21 | thumb, 0x2000 | 5 | 20 | 145,349 | 144,589 | 35,501 | 36,549 | -75% |
