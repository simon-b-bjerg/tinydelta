# tinydelta patch sizes

Tools: tinydelta (tinydelta v0.0.0-20261031124901-8c2fe13563f7); openvcdiff (open-vcdiff 868f459a8d815125c2457f8c74b12493853100f9); xdelta3 (Xdelta version 3.0.11); bsdiff (bsdiff 4.3-23); detools (detools 0.53.0); hpatchlite (HPatchLite v1.0.2); zstd (Zstandard CLI (64-bit) v1.5.7)

Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. Geometric mean is the right average for ratios; "smaller" is the share of pairs where tinydelta is smaller. tinydelta is, per pair, the smaller of its plain patch (the library default) and its compression mode decoded twice, as sent: what an integrator following the README ships, with a decoder that reads format versions 1 and 2. Both are listed on their own in the patch-size tables.

## firmware, 1 commit apart

48 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11; median target 296,652 bytes.

Compression mode is the smaller in 27% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.36 | 0.46 | 0.14-0.48 | 100% |
| open-vcdiff, zlib-compressed | 0.36 | 0.40 | 0.22-0.42 | 100% |
| xdelta3 | 0.25 | 0.23 | 0.22-0.28 | 100% |
| bsdiff | 0.22 | 0.12 | 0.11-1.58 | 77% |
| detools (heatshrink) | 0.01 | 0.01 | 0.00-0.78 | 100% |
| HPatchLite, 4 KB dict. | 0.44 | 0.35 | 0.29-1.01 | 88% |
| HPatchLite, 32 KB dict. | 0.45 | 0.35 | 0.29-1.06 | 77% |
| zstd --patch-from | 0.42 | 0.36 | 0.30-0.73 | 98% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 0.01% | 0.01% | 2.78% |
| tinydelta, plain | 0.01% | 0.01% | 8.56% |
| tinydelta, plain (greedy) | 0.01% | 0.01% | 8.56% |
| tinydelta, compression mode, decoded twice | 0.01% | 0.01% | 2.78% |
| tinydelta, compression mode, decoded twice, 1 KB window | 0.01% | 0.01% | 3.09% |
| open-vcdiff | 0.02% | 0.01% | 19.43% |
| open-vcdiff, zlib-compressed | 0.02% | 0.01% | 12.11% |
| xdelta3 | 0.04% | 0.02% | 11.40% |
| bsdiff | 0.08% | 0.05% | 1.73% |
| detools (heatshrink) | 1.57% | 1.57% | 3.50% |
| HPatchLite, 4 KB dict. | 0.02% | 0.02% | 2.81% |
| HPatchLite, 32 KB dict. | 0.02% | 0.02% | 2.69% |
| zstd --patch-from | 0.02% | 0.02% | 4.22% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 21 | 20 | 9,433 |
| tinydelta, plain | 21 | 20 | 27,546 |
| tinydelta, plain (greedy) | 21 | 20 | 27,546 |
| tinydelta, compression mode, decoded twice | 23 | 22 | 9,433 |
| tinydelta, compression mode, decoded twice, 1 KB window | 23 | 22 | 10,508 |
| open-vcdiff | 44 | 42 | 65,838 |
| open-vcdiff, zlib-compressed | 51 | 48 | 41,931 |
| xdelta3 | 90 | 88 | 37,353 |
| bsdiff | 177 | 172 | 5,981 |
| detools (heatshrink) | 5,715 | 3,610 | 12,116 |
| HPatchLite, 4 KB dict. | 69 | 57 | 8,900 |
| HPatchLite, 32 KB dict. | 69 | 57 | 8,474 |
| zstd --patch-from | 66 | 55 | 15,050 |

## firmware, 5 commits apart

40 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11; median target 296,652 bytes.

Compression mode is the smaller in 60% of these pairs.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| open-vcdiff | 0.27 | 0.33 | 0.14-0.47 | 100% |
| open-vcdiff, zlib-compressed | 0.33 | 0.36 | 0.22-0.43 | 100% |
| xdelta3 | 0.29 | 0.26 | 0.24-0.42 | 100% |
| bsdiff | 0.53 | 1.15 | 0.13-1.58 | 45% |
| detools (heatshrink) | 0.09 | 0.26 | 0.01-0.83 | 100% |
| HPatchLite, 4 KB dict. | 0.69 | 0.96 | 0.36-1.10 | 70% |
| HPatchLite, 32 KB dict. | 0.70 | 1.01 | 0.36-1.13 | 45% |
| zstd --patch-from | 0.59 | 0.64 | 0.37-0.97 | 90% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 0.47% | 0.01% | 3.98% |
| tinydelta, plain | 0.70% | 0.01% | 8.69% |
| tinydelta, plain (greedy) | 0.70% | 0.01% | 8.69% |
| tinydelta, compression mode, decoded twice | 0.47% | 0.01% | 3.98% |
| tinydelta, compression mode, decoded twice, 1 KB window | 0.47% | 0.01% | 4.23% |
| open-vcdiff | 1.43% | 0.02% | 20.73% |
| open-vcdiff, zlib-compressed | 1.09% | 0.02% | 13.06% |
| xdelta3 | 1.12% | 0.04% | 11.81% |
| bsdiff | 0.41% | 0.07% | 2.89% |
| detools (heatshrink) | 1.83% | 1.57% | 4.83% |
| HPatchLite, 4 KB dict. | 0.49% | 0.03% | 3.64% |
| HPatchLite, 32 KB dict. | 0.45% | 0.03% | 3.52% |
| zstd --patch-from | 0.43% | 0.02% | 5.37% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta | 1,076 | 24 | 14,482 |
| tinydelta, plain | 1,606 | 24 | 31,566 |
| tinydelta, plain (greedy) | 1,606 | 24 | 31,566 |
| tinydelta, compression mode, decoded twice | 1,076 | 26 | 14,482 |
| tinydelta, compression mode, decoded twice, 1 KB window | 1,082 | 26 | 15,412 |
| open-vcdiff | 3,276 | 58 | 74,752 |
| open-vcdiff, zlib-compressed | 2,490 | 62 | 46,939 |
| xdelta3 | 2,563 | 93 | 42,815 |
| bsdiff | 938 | 178 | 10,507 |
| detools (heatshrink) | 5,720 | 3,613 | 17,589 |
| HPatchLite, 4 KB dict. | 1,119 | 59 | 13,240 |
| HPatchLite, 32 KB dict. | 1,033 | 59 | 12,804 |
| zstd --patch-from | 996 | 59 | 19,543 |

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
| open-vcdiff | 7,613 | 49 | 17,040 |
| open-vcdiff, zlib-compressed | 3,518 | 54 | 6,305 |
| xdelta3 | 3,664 | 86 | 6,759 |
| bsdiff | 1,259 | 169 | 2,376 |
| detools (heatshrink) | 2,235 | 1,157 | 3,863 |
| HPatchLite, 4 KB dict. | 1,343 | 40 | 2,407 |
| HPatchLite, 32 KB dict. | 1,282 | 40 | 2,321 |
| zstd --patch-from | 1,517 | 38 | 3,010 |
