# tinydelta patch sizes

Tools: tinydelta (tinydelta v0.0.0-20261031124901-8c2fe13563f7); openvcdiff (open-vcdiff 868f459a8d815125c2457f8c74b12493853100f9); xdelta3 (Xdelta version 3.0.11); bsdiff (bsdiff 4.3-23); detools (detools 0.53.0); hpatchlite (HPatchLite v1.0.2); zstd (Zstandard CLI (64-bit) v1.5.7)

Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. Geometric mean is the right average for ratios; "smaller" is the share of pairs where tinydelta is smaller. tinydelta is the library default, as it is sent; its compression mode, decoded twice, is compared like any other tool.

## firmware, 1 commit apart

48 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11; median target 296,652 bytes.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| tinydelta, compression mode, decoded twice | 1.17 | 0.91 | 0.91-2.75 | 73% |
| tinydelta, compression mode, decoded twice, 1 KB window | 1.15 | 0.91 | 0.91-2.57 | 73% |
| open-vcdiff | 0.44 | 0.47 | 0.39-0.48 | 100% |
| open-vcdiff + zlib | 0.45 | 0.42 | 0.36-0.67 | 100% |
| xdelta3 | 0.31 | 0.23 | 0.22-0.73 | 100% |
| bsdiff | 0.27 | 0.12 | 0.11-4.34 | 77% |
| detools (heatshrink) | 0.02 | 0.01 | 0.00-2.30 | 83% |
| HPatchLite, 4 KB dict. | 0.55 | 0.35 | 0.29-2.89 | 77% |
| HPatchLite, 32 KB dict. | 0.56 | 0.35 | 0.29-3.02 | 77% |
| zstd --patch-from | 0.52 | 0.36 | 0.30-1.92 | 75% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 0.01% | 0.01% | 8.56% |
| tinydelta (greedy) | 0.01% | 0.01% | 8.56% |
| tinydelta, compression mode, decoded twice | 0.01% | 0.01% | 2.78% |
| tinydelta, compression mode, decoded twice, 1 KB window | 0.01% | 0.01% | 3.09% |
| open-vcdiff | 0.02% | 0.01% | 19.43% |
| open-vcdiff + zlib | 0.02% | 0.01% | 12.11% |
| xdelta3 | 0.04% | 0.02% | 11.40% |
| bsdiff | 0.08% | 0.05% | 1.73% |
| detools (heatshrink) | 1.57% | 1.57% | 3.50% |
| HPatchLite, 4 KB dict. | 0.02% | 0.02% | 2.81% |
| HPatchLite, 32 KB dict. | 0.02% | 0.02% | 2.69% |
| zstd --patch-from | 0.02% | 0.02% | 4.22% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 21 | 20 | 27,546 |
| tinydelta (greedy) | 21 | 20 | 27,546 |
| tinydelta, compression mode, decoded twice | 23 | 22 | 9,433 |
| tinydelta, compression mode, decoded twice, 1 KB window | 23 | 22 | 10,508 |
| open-vcdiff | 44 | 42 | 65,838 |
| open-vcdiff + zlib | 51 | 48 | 41,931 |
| xdelta3 | 90 | 88 | 37,353 |
| bsdiff | 177 | 172 | 5,981 |
| detools (heatshrink) | 5,715 | 3,610 | 12,116 |
| HPatchLite, 4 KB dict. | 69 | 57 | 8,900 |
| HPatchLite, 32 KB dict. | 69 | 57 | 8,474 |
| zstd --patch-from | 66 | 55 | 15,050 |

## firmware, 5 commits apart

40 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11; median target 296,652 bytes.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| tinydelta, compression mode, decoded twice | 1.57 | 1.49 | 0.92-3.07 | 40% |
| tinydelta, compression mode, decoded twice, 1 KB window | 1.51 | 1.48 | 0.92-2.75 | 40% |
| open-vcdiff | 0.43 | 0.44 | 0.40-0.47 | 100% |
| open-vcdiff + zlib | 0.53 | 0.59 | 0.38-0.75 | 100% |
| xdelta3 | 0.47 | 0.63 | 0.25-0.76 | 100% |
| bsdiff | 0.86 | 1.72 | 0.13-4.80 | 45% |
| detools (heatshrink) | 0.15 | 0.38 | 0.01-2.36 | 52% |
| HPatchLite, 4 KB dict. | 1.12 | 1.44 | 0.36-3.06 | 45% |
| HPatchLite, 32 KB dict. | 1.14 | 1.56 | 0.36-3.22 | 45% |
| zstd --patch-from | 0.96 | 1.58 | 0.37-1.96 | 40% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 0.70% | 0.01% | 8.69% |
| tinydelta (greedy) | 0.70% | 0.01% | 8.69% |
| tinydelta, compression mode, decoded twice | 0.47% | 0.01% | 3.98% |
| tinydelta, compression mode, decoded twice, 1 KB window | 0.47% | 0.01% | 4.23% |
| open-vcdiff | 1.43% | 0.02% | 20.73% |
| open-vcdiff + zlib | 1.09% | 0.02% | 13.06% |
| xdelta3 | 1.12% | 0.04% | 11.81% |
| bsdiff | 0.41% | 0.07% | 2.89% |
| detools (heatshrink) | 1.83% | 1.57% | 4.83% |
| HPatchLite, 4 KB dict. | 0.49% | 0.03% | 3.64% |
| HPatchLite, 32 KB dict. | 0.45% | 0.03% | 3.52% |
| zstd --patch-from | 0.43% | 0.02% | 5.37% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 1,606 | 24 | 31,566 |
| tinydelta (greedy) | 1,606 | 24 | 31,566 |
| tinydelta, compression mode, decoded twice | 1,076 | 26 | 14,482 |
| tinydelta, compression mode, decoded twice, 1 KB window | 1,082 | 26 | 15,412 |
| open-vcdiff | 3,276 | 58 | 74,752 |
| open-vcdiff + zlib | 2,490 | 62 | 46,939 |
| xdelta3 | 2,563 | 93 | 42,815 |
| bsdiff | 938 | 178 | 10,507 |
| detools (heatshrink) | 5,720 | 3,613 | 17,589 |
| HPatchLite, 4 KB dict. | 1,119 | 59 | 13,240 |
| HPatchLite, 32 KB dict. | 1,033 | 59 | 12,804 |
| zstd --patch-from | 996 | 59 | 19,543 |

## device tree, 1 commit apart

45 pairs from dtb/imx8mp-evk, dtb/k3-am625-sk, dtb/rk3588-rock-5b; median target 62,802 bytes.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| tinydelta, compression mode, decoded twice | 1.56 | 1.63 | 0.92-2.84 | 29% |
| tinydelta, compression mode, decoded twice, 1 KB window | 1.52 | 1.62 | 0.92-2.58 | 29% |
| open-vcdiff | 0.34 | 0.33 | 0.28-0.39 | 100% |
| open-vcdiff + zlib | 0.59 | 0.81 | 0.30-0.87 | 100% |
| xdelta3 | 0.54 | 0.77 | 0.25-0.82 | 100% |
| bsdiff | 0.86 | 1.90 | 0.14-3.49 | 42% |
| detools (heatshrink) | 0.30 | 1.15 | 0.02-1.68 | 42% |
| HPatchLite, 4 KB dict. | 1.31 | 1.91 | 0.49-2.96 | 38% |
| HPatchLite, 32 KB dict. | 1.34 | 2.00 | 0.49-3.10 | 38% |
| zstd --patch-from | 1.19 | 1.70 | 0.56-2.04 | 38% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 3.14% | 0.03% | 5.89% |
| tinydelta (greedy) | 3.14% | 0.04% | 5.89% |
| tinydelta, compression mode, decoded twice | 1.28% | 0.03% | 3.12% |
| tinydelta, compression mode, decoded twice, 1 KB window | 1.33% | 0.03% | 3.20% |
| open-vcdiff | 7.44% | 0.12% | 16.48% |
| open-vcdiff + zlib | 3.71% | 0.10% | 6.69% |
| xdelta3 | 4.29% | 0.12% | 7.24% |
| bsdiff | 1.09% | 0.22% | 2.40% |
| detools (heatshrink) | 2.34% | 1.62% | 3.86% |
| HPatchLite, 4 KB dict. | 1.17% | 0.06% | 2.61% |
| HPatchLite, 32 KB dict. | 1.14% | 0.06% | 2.52% |
| zstd --patch-from | 1.77% | 0.06% | 3.09% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 2,424 | 25 | 4,992 |
| tinydelta (greedy) | 2,424 | 28 | 4,992 |
| tinydelta, compression mode, decoded twice | 1,174 | 27 | 1,996 |
| tinydelta, compression mode, decoded twice, 1 KB window | 1,202 | 27 | 2,119 |
| open-vcdiff | 6,572 | 79 | 15,464 |
| open-vcdiff + zlib | 3,056 | 80 | 5,776 |
| xdelta3 | 3,125 | 100 | 6,189 |
| bsdiff | 986 | 178 | 1,727 |
| detools (heatshrink) | 1,767 | 999 | 2,975 |
| HPatchLite, 4 KB dict. | 1,072 | 49 | 1,841 |
| HPatchLite, 32 KB dict. | 1,031 | 49 | 1,780 |
| zstd --patch-from | 1,327 | 46 | 2,607 |

## device tree, 5 commits apart

33 pairs from dtb/imx8mp-evk, dtb/k3-am625-sk, dtb/rk3588-rock-5b; median target 62,802 bytes.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| tinydelta, compression mode, decoded twice | 1.63 | 1.71 | 0.89-2.77 | 18% |
| tinydelta, compression mode, decoded twice, 1 KB window | 1.57 | 1.70 | 0.89-2.50 | 18% |
| open-vcdiff | 0.33 | 0.33 | 0.27-0.41 | 100% |
| open-vcdiff + zlib | 0.65 | 0.84 | 0.27-0.90 | 100% |
| xdelta3 | 0.55 | 0.80 | 0.17-0.84 | 100% |
| bsdiff | 1.11 | 2.08 | 0.08-3.29 | 24% |
| detools (heatshrink) | 0.47 | 1.18 | 0.02-1.64 | 24% |
| HPatchLite, 4 KB dict. | 1.55 | 2.10 | 0.37-2.87 | 18% |
| HPatchLite, 32 KB dict. | 1.59 | 2.17 | 0.37-2.97 | 18% |
| zstd --patch-from | 1.24 | 1.74 | 0.39-1.96 | 18% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 4.07% | 0.02% | 5.42% |
| tinydelta (greedy) | 4.07% | 0.03% | 5.42% |
| tinydelta, compression mode, decoded twice | 1.82% | 0.03% | 3.29% |
| tinydelta, compression mode, decoded twice, 1 KB window | 2.01% | 0.03% | 3.40% |
| open-vcdiff | 11.03% | 0.08% | 16.23% |
| open-vcdiff + zlib | 5.01% | 0.09% | 6.02% |
| xdelta3 | 5.22% | 0.12% | 6.54% |
| bsdiff | 1.59% | 0.24% | 2.63% |
| detools (heatshrink) | 2.95% | 1.61% | 4.08% |
| HPatchLite, 4 KB dict. | 1.72% | 0.06% | 2.80% |
| HPatchLite, 32 KB dict. | 1.64% | 0.06% | 2.70% |
| zstd --patch-from | 2.29% | 0.06% | 3.25% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 2,987 | 15 | 5,662 |
| tinydelta (greedy) | 2,987 | 16 | 5,662 |
| tinydelta, compression mode, decoded twice | 1,438 | 17 | 2,651 |
| tinydelta, compression mode, decoded twice, 1 KB window | 1,495 | 17 | 2,842 |
| open-vcdiff | 7,613 | 49 | 17,040 |
| open-vcdiff + zlib | 3,518 | 54 | 6,305 |
| xdelta3 | 3,664 | 86 | 6,759 |
| bsdiff | 1,259 | 169 | 2,376 |
| detools (heatshrink) | 2,235 | 1,157 | 3,863 |
| HPatchLite, 4 KB dict. | 1,343 | 40 | 2,407 |
| HPatchLite, 32 KB dict. | 1,282 | 40 | 2,321 |
| zstd --patch-from | 1,517 | 38 | 3,010 |
