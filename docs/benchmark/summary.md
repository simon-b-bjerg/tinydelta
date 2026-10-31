# tinydelta patch sizes

Tools: tinydelta (tinydelta v0.0.0-20261031215036-460954a633e0); openvcdiff (open-vcdiff 868f459a8d815125c2457f8c74b12493853100f9); xdelta3 (Xdelta version 3.0.11); bsdiff (bsdiff 4.3-23); detools (detools 0.53.0); hpatchlite (HPatchLite v1.0.2); zstd (Zstandard CLI (64-bit) v1.5.7)

Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. Geometric mean is the right average for ratios; "smaller" is the share of pairs where tinydelta is smaller. tinydelta is, per pair, the smaller of its plain patch (the library default) and its compression-mode patch, the one an update server would send. Both are listed on their own in the patch-size tables.

## firmware, 1 commit apart

48 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11; median target 296,668 bytes.

Compression mode is the smaller in 92% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.18 | 0.15 | 0.13-0.35 | 100% |
| open-vcdiff, zlib-compressed | 0.26 | 0.24 | 0.22-0.35 | 100% |
| xdelta3 | 0.29 | 0.26 | 0.23-0.42 | 100% |
| bsdiff | 1.12 | 1.56 | 0.34-1.62 | 17% |
| detools (heatshrink) | 0.38 | 0.80 | 0.02-0.92 | 96% |
| HPatchLite, 4 KB dict. | 0.95 | 1.04 | 0.64-1.20 | 33% |
| HPatchLite, 32 KB dict. | 0.98 | 1.07 | 0.66-1.24 | 19% |
| zstd --patch-from | 0.68 | 0.68 | 0.55-0.95 | 94% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 2.76% | 0.04% | 4.25% |
| tinydelta, plain | 8.40% | 0.04% | 10.43% |
| tinydelta, plain (greedy) | 8.40% | 0.04% | 10.43% |
| tinydelta, compression mode, decoded twice | 2.76% | 0.04% | 4.25% |
| tinydelta, compression mode, decoded twice, 1 KB window | 3.05% | 0.04% | 4.70% |
| tinydelta, plain, copies at most 256 B back | 8.48% | 0.04% | 10.69% |
| tinydelta, plain, copies at most 1 KB back | 8.44% | 0.04% | 10.60% |
| tinydelta, plain, copies at most 4 KB back | 8.42% | 0.04% | 10.50% |
| open-vcdiff | 18.93% | 0.14% | 26.54% |
| open-vcdiff, zlib-compressed | 11.70% | 0.11% | 17.40% |
| xdelta3 | 11.34% | 0.09% | 13.93% |
| bsdiff | 1.76% | 0.12% | 2.78% |
| detools (heatshrink) | 3.40% | 1.60% | 4.84% |
| HPatchLite, 4 KB dict. | 2.69% | 0.06% | 3.81% |
| HPatchLite, 32 KB dict. | 2.56% | 0.06% | 3.65% |
| zstd --patch-from | 4.27% | 0.05% | 6.23% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 8,234 | 92 | 13,810 |
| tinydelta, plain | 22,030 | 98 | 32,296 |
| tinydelta, plain (greedy) | 22,030 | 98 | 32,296 |
| tinydelta, compression mode, decoded twice | 8,234 | 92 | 13,810 |
| tinydelta, compression mode, decoded twice, 1 KB window | 8,994 | 92 | 15,030 |
| tinydelta, plain, copies at most 256 B back | 22,378 | 98 | 33,031 |
| tinydelta, plain, copies at most 1 KB back | 22,235 | 98 | 32,696 |
| tinydelta, plain, copies at most 4 KB back | 22,096 | 98 | 32,425 |
| open-vcdiff | 56,961 | 318 | 81,512 |
| open-vcdiff, zlib-compressed | 35,378 | 252 | 53,024 |
| xdelta3 | 31,078 | 210 | 42,864 |
| bsdiff | 5,150 | 269 | 8,773 |
| detools (heatshrink) | 9,208 | 4,841 | 15,094 |
| HPatchLite, 4 KB dict. | 7,451 | 146 | 11,913 |
| HPatchLite, 32 KB dict. | 7,172 | 143 | 11,479 |
| zstd --patch-from | 11,764 | 119 | 19,725 |

## firmware, 5 commits apart

40 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11; median target 296,668 bytes.

Compression mode is the smaller in 100% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.18 | 0.18 | 0.16-0.21 | 100% |
| open-vcdiff, zlib-compressed | 0.28 | 0.27 | 0.24-0.32 | 100% |
| xdelta3 | 0.35 | 0.35 | 0.31-0.45 | 100% |
| bsdiff | 1.51 | 1.52 | 1.43-1.58 | 0% |
| detools (heatshrink) | 0.96 | 0.94 | 0.87-1.05 | 65% |
| HPatchLite, 4 KB dict. | 1.17 | 1.16 | 1.08-1.28 | 0% |
| HPatchLite, 32 KB dict. | 1.21 | 1.20 | 1.11-1.32 | 0% |
| zstd --patch-from | 0.81 | 0.79 | 0.69-0.98 | 95% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 5.21% | 4.12% | 8.19% |
| tinydelta, plain | 11.41% | 9.78% | 15.25% |
| tinydelta, plain (greedy) | 11.41% | 9.78% | 15.25% |
| tinydelta, compression mode, decoded twice | 5.21% | 4.12% | 8.19% |
| tinydelta, compression mode, decoded twice, 1 KB window | 5.65% | 4.45% | 8.77% |
| tinydelta, plain, copies at most 256 B back | 11.68% | 9.98% | 15.57% |
| tinydelta, plain, copies at most 1 KB back | 11.57% | 9.88% | 15.46% |
| tinydelta, plain, copies at most 4 KB back | 11.48% | 9.81% | 15.36% |
| open-vcdiff | 28.32% | 23.42% | 40.73% |
| open-vcdiff, zlib-compressed | 18.91% | 15.25% | 27.85% |
| xdelta3 | 14.74% | 12.96% | 20.39% |
| bsdiff | 3.58% | 2.72% | 5.41% |
| detools (heatshrink) | 5.76% | 4.53% | 7.83% |
| HPatchLite, 4 KB dict. | 4.73% | 3.51% | 6.69% |
| HPatchLite, 32 KB dict. | 4.58% | 3.38% | 6.50% |
| zstd --patch-from | 7.21% | 5.19% | 9.12% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 16,248 | 12,179 | 21,211 |
| tinydelta, plain | 35,804 | 26,298 | 41,167 |
| tinydelta, plain (greedy) | 35,804 | 26,298 | 41,167 |
| tinydelta, compression mode, decoded twice | 16,248 | 12,179 | 21,211 |
| tinydelta, compression mode, decoded twice, 1 KB window | 17,406 | 12,947 | 22,617 |
| tinydelta, plain, copies at most 256 B back | 36,504 | 26,616 | 42,594 |
| tinydelta, plain, copies at most 1 KB back | 36,188 | 26,492 | 42,007 |
| tinydelta, plain, copies at most 4 KB back | 35,924 | 26,363 | 41,561 |
| open-vcdiff | 91,106 | 69,502 | 101,763 |
| open-vcdiff, zlib-compressed | 60,702 | 46,682 | 69,092 |
| xdelta3 | 47,902 | 36,220 | 53,064 |
| bsdiff | 10,584 | 7,779 | 14,592 |
| detools (heatshrink) | 17,341 | 12,150 | 21,402 |
| HPatchLite, 4 KB dict. | 13,852 | 10,027 | 17,622 |
| HPatchLite, 32 KB dict. | 13,438 | 9,690 | 17,068 |
| zstd --patch-from | 20,789 | 15,064 | 27,071 |

## device tree, 1 commit apart

45 pairs from dtb/imx8mp-evk, dtb/k3-am625-sk, dtb/rk3588-rock-5b; median target 62,802 bytes.

Compression mode is the smaller in 71% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.21 | 0.23 | 0.11-0.34 | 100% |
| open-vcdiff, zlib-compressed | 0.37 | 0.35 | 0.28-0.50 | 100% |
| xdelta3 | 0.34 | 0.35 | 0.25-0.48 | 100% |
| bsdiff | 0.54 | 1.11 | 0.14-1.27 | 42% |
| detools (heatshrink) | 0.18 | 0.55 | 0.02-0.82 | 100% |
| HPatchLite, 4 KB dict. | 0.82 | 1.04 | 0.48-1.19 | 47% |
| HPatchLite, 32 KB dict. | 0.84 | 1.07 | 0.48-1.24 | 42% |
| zstd --patch-from | 0.74 | 0.75 | 0.54-1.03 | 87% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 1.28% | 0.03% | 3.12% |
| tinydelta, plain | 3.14% | 0.03% | 5.89% |
| tinydelta, plain (greedy) | 3.14% | 0.04% | 5.89% |
| tinydelta, compression mode, decoded twice | 1.28% | 0.03% | 3.12% |
| tinydelta, compression mode, decoded twice, 1 KB window | 1.33% | 0.03% | 3.20% |
| tinydelta, plain, copies at most 256 B back | 3.18% | 0.04% | 6.58% |
| tinydelta, plain, copies at most 1 KB back | 3.15% | 0.03% | 6.04% |
| tinydelta, plain, copies at most 4 KB back | 3.15% | 0.03% | 5.96% |
| open-vcdiff | 7.44% | 0.12% | 16.48% |
| open-vcdiff, zlib-compressed | 3.71% | 0.10% | 6.69% |
| xdelta3 | 4.29% | 0.12% | 7.24% |
| bsdiff | 1.09% | 0.22% | 2.40% |
| detools (heatshrink) | 2.34% | 1.62% | 3.86% |
| HPatchLite, 4 KB dict. | 1.17% | 0.06% | 2.61% |
| HPatchLite, 32 KB dict. | 1.14% | 0.06% | 2.52% |
| zstd --patch-from | 1.77% | 0.06% | 3.09% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 1,174 | 25 | 1,996 |
| tinydelta, plain | 2,424 | 25 | 4,992 |
| tinydelta, plain (greedy) | 2,424 | 28 | 4,992 |
| tinydelta, compression mode, decoded twice | 1,174 | 27 | 1,996 |
| tinydelta, compression mode, decoded twice, 1 KB window | 1,202 | 27 | 2,119 |
| tinydelta, plain, copies at most 256 B back | 2,571 | 27 | 5,487 |
| tinydelta, plain, copies at most 1 KB back | 2,477 | 26 | 5,198 |
| tinydelta, plain, copies at most 4 KB back | 2,447 | 26 | 5,068 |
| open-vcdiff | 6,572 | 79 | 15,464 |
| open-vcdiff, zlib-compressed | 3,056 | 80 | 5,776 |
| xdelta3 | 3,125 | 100 | 6,189 |
| bsdiff | 986 | 178 | 1,727 |
| detools (heatshrink) | 1,767 | 999 | 2,975 |
| HPatchLite, 4 KB dict. | 1,072 | 49 | 1,841 |
| HPatchLite, 32 KB dict. | 1,031 | 49 | 1,780 |
| zstd --patch-from | 1,327 | 46 | 2,607 |

## device tree, 5 commits apart

33 pairs from dtb/imx8mp-evk, dtb/k3-am625-sk, dtb/rk3588-rock-5b; median target 62,802 bytes.

Compression mode is the smaller in 82% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.19 | 0.19 | 0.12-0.30 | 100% |
| open-vcdiff, zlib-compressed | 0.38 | 0.43 | 0.27-0.59 | 100% |
| xdelta3 | 0.32 | 0.40 | 0.17-0.57 | 100% |
| bsdiff | 0.65 | 1.13 | 0.08-1.24 | 24% |
| detools (heatshrink) | 0.28 | 0.62 | 0.02-0.80 | 100% |
| HPatchLite, 4 KB dict. | 0.91 | 1.07 | 0.37-1.20 | 24% |
| HPatchLite, 32 KB dict. | 0.93 | 1.11 | 0.37-1.24 | 24% |
| zstd --patch-from | 0.73 | 0.88 | 0.39-1.15 | 67% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 1.82% | 0.02% | 3.29% |
| tinydelta, plain | 4.07% | 0.02% | 5.42% |
| tinydelta, plain (greedy) | 4.07% | 0.03% | 5.42% |
| tinydelta, compression mode, decoded twice | 1.82% | 0.03% | 3.29% |
| tinydelta, compression mode, decoded twice, 1 KB window | 2.01% | 0.03% | 3.40% |
| tinydelta, plain, copies at most 256 B back | 4.32% | 0.03% | 6.08% |
| tinydelta, plain, copies at most 1 KB back | 4.16% | 0.02% | 5.64% |
| tinydelta, plain, copies at most 4 KB back | 4.11% | 0.02% | 5.51% |
| open-vcdiff | 11.03% | 0.08% | 16.23% |
| open-vcdiff, zlib-compressed | 5.01% | 0.09% | 6.02% |
| xdelta3 | 5.22% | 0.12% | 6.54% |
| bsdiff | 1.59% | 0.24% | 2.63% |
| detools (heatshrink) | 2.95% | 1.61% | 4.08% |
| HPatchLite, 4 KB dict. | 1.72% | 0.06% | 2.80% |
| HPatchLite, 32 KB dict. | 1.64% | 0.06% | 2.70% |
| zstd --patch-from | 2.29% | 0.06% | 3.25% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 1,438 | 15 | 2,651 |
| tinydelta, plain | 2,987 | 15 | 5,662 |
| tinydelta, plain (greedy) | 2,987 | 16 | 5,662 |
| tinydelta, compression mode, decoded twice | 1,438 | 17 | 2,651 |
| tinydelta, compression mode, decoded twice, 1 KB window | 1,495 | 17 | 2,842 |
| tinydelta, plain, copies at most 256 B back | 3,013 | 16 | 6,365 |
| tinydelta, plain, copies at most 1 KB back | 2,999 | 15 | 5,904 |
| tinydelta, plain, copies at most 4 KB back | 2,991 | 15 | 5,775 |
| open-vcdiff | 7,613 | 49 | 17,040 |
| open-vcdiff, zlib-compressed | 3,518 | 54 | 6,305 |
| xdelta3 | 3,664 | 86 | 6,759 |
| bsdiff | 1,259 | 169 | 2,376 |
| detools (heatshrink) | 2,235 | 1,157 | 3,863 |
| HPatchLite, 4 KB dict. | 1,343 | 40 | 2,407 |
| HPatchLite, 32 KB dict. | 1,282 | 40 | 2,321 |
| zstd --patch-from | 1,517 | 38 | 3,010 |
