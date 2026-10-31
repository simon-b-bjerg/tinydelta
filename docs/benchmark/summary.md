# tinydelta patch sizes

Tools: tinydelta (tinydelta v0.0.0-20261031091102-9c2fea30da6e); openvcdiff (open-vcdiff 868f459a8d815125c2457f8c74b12493853100f9); xdelta3 (Xdelta version 3.0.11); bsdiff (bsdiff 4.3-23); detools (detools 0.53.0); hpatchlite (HPatchLite v1.0.2); zstd (Zstandard CLI (64-bit) v1.5.7)

Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. Geometric mean is the right average for ratios; "smaller" is the share of pairs where tinydelta is smaller. tinydelta is the library default, uncompressed as it is sent; tinydelta + zlib is that patch at zlib level 9, compared like any other tool.

## firmware, 1 commit apart

48 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11; median target 296,652 bytes.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| tinydelta + zlib | 1.13 | 1.25 | 0.77-1.52 | 33% |
| open-vcdiff | 0.37 | 0.39 | 0.27-0.47 | 100% |
| open-vcdiff + zlib | 0.47 | 0.42 | 0.40-0.66 | 100% |
| xdelta3 | 0.46 | 0.55 | 0.26-0.73 | 100% |
| bsdiff | 0.48 | 0.39 | 0.14-4.25 | 77% |
| detools (heatshrink) | 0.04 | 0.03 | 0.00-2.28 | 83% |
| HPatchLite, 4 KB dict. | 0.75 | 0.67 | 0.35-2.88 | 77% |
| HPatchLite, 32 KB dict. | 0.76 | 0.67 | 0.35-3.01 | 77% |
| zstd --patch-from | 0.78 | 0.83 | 0.37-1.91 | 73% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 0.05% | 0.01% | 8.56% |
| tinydelta (greedy) | 0.05% | 0.01% | 8.56% |
| tinydelta + zlib | 0.04% | 0.01% | 6.38% |
| open-vcdiff | 0.17% | 0.02% | 19.43% |
| open-vcdiff + zlib | 0.12% | 0.02% | 12.11% |
| xdelta3 | 0.09% | 0.03% | 11.40% |
| bsdiff | 0.13% | 0.05% | 1.73% |
| detools (heatshrink) | 1.63% | 1.57% | 3.50% |
| HPatchLite, 4 KB dict. | 0.07% | 0.02% | 2.81% |
| HPatchLite, 32 KB dict. | 0.07% | 0.02% | 2.69% |
| zstd --patch-from | 0.06% | 0.02% | 4.22% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 114 | 27 | 27,546 |
| tinydelta (greedy) | 114 | 27 | 27,546 |
| tinydelta + zlib | 84 | 35 | 20,950 |
| open-vcdiff | 386 | 59 | 65,838 |
| open-vcdiff + zlib | 266 | 66 | 41,930 |
| xdelta3 | 204 | 103 | 37,353 |
| bsdiff | 290 | 183 | 6,058 |
| detools (heatshrink) | 5,717 | 3,721 | 12,118 |
| HPatchLite, 4 KB dict. | 170 | 77 | 8,902 |
| HPatchLite, 32 KB dict. | 170 | 77 | 8,475 |
| zstd --patch-from | 137 | 72 | 15,050 |

## firmware, 5 commits apart

40 pairs from micropython/stm32-NUCLEO_F091RC, micropython/stm32-PYBV11; median target 296,652 bytes.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| tinydelta + zlib | 1.27 | 1.30 | 1.09-1.46 | 8% |
| open-vcdiff | 0.37 | 0.42 | 0.27-0.47 | 100% |
| open-vcdiff + zlib | 0.54 | 0.59 | 0.41-0.75 | 100% |
| xdelta3 | 0.62 | 0.63 | 0.55-0.76 | 100% |
| bsdiff | 1.24 | 1.53 | 0.37-4.79 | 45% |
| detools (heatshrink) | 0.26 | 0.39 | 0.03-2.36 | 52% |
| HPatchLite, 4 KB dict. | 1.33 | 1.38 | 0.64-3.06 | 45% |
| HPatchLite, 32 KB dict. | 1.37 | 1.48 | 0.64-3.21 | 45% |
| zstd --patch-from | 1.22 | 1.55 | 0.74-1.96 | 40% |

Patch size as a share of the target (median, p10-p90):

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 0.74% | 0.05% | 8.69% |
| tinydelta (greedy) | 0.74% | 0.05% | 8.69% |
| tinydelta + zlib | 0.61% | 0.04% | 6.77% |
| open-vcdiff | 1.59% | 0.16% | 20.73% |
| open-vcdiff + zlib | 1.17% | 0.11% | 13.06% |
| xdelta3 | 1.16% | 0.09% | 11.81% |
| bsdiff | 0.48% | 0.13% | 2.89% |
| detools (heatshrink) | 1.89% | 1.62% | 4.83% |
| HPatchLite, 4 KB dict. | 0.54% | 0.07% | 3.64% |
| HPatchLite, 32 KB dict. | 0.50% | 0.07% | 3.52% |
| zstd --patch-from | 0.48% | 0.06% | 5.37% |

Patch size in bytes (median, p10-p90), what goes over the air:

| tool | median | p10 | p90 |
|---|---:|---:|---:|
| tinydelta (library default) | 1,690 | 108 | 31,563 |
| tinydelta (greedy) | 1,690 | 108 | 31,563 |
| tinydelta + zlib | 1,390 | 80 | 24,449 |
| open-vcdiff | 3,636 | 360 | 74,737 |
| open-vcdiff + zlib | 2,679 | 255 | 46,921 |
| xdelta3 | 2,664 | 197 | 42,810 |
| bsdiff | 1,108 | 290 | 10,526 |
| detools (heatshrink) | 5,727 | 3,729 | 17,597 |
| HPatchLite, 4 KB dict. | 1,229 | 169 | 13,248 |
| HPatchLite, 32 KB dict. | 1,144 | 169 | 12,809 |
| zstd --patch-from | 1,092 | 133 | 19,549 |

## device tree, 1 commit apart

45 pairs from dtb/imx8mp-evk, dtb/k3-am625-sk, dtb/rk3588-rock-5b; median target 62,802 bytes.

| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |
|---|---:|---:|---:|---:|
| tinydelta + zlib | 1.08 | 1.23 | 0.75-1.42 | 38% |
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
| tinydelta + zlib | 2.43% | 0.04% | 4.46% |
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
| tinydelta + zlib | 1,894 | 34 | 3,635 |
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
| tinydelta + zlib | 1.07 | 1.27 | 0.65-1.36 | 18% |
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
| tinydelta + zlib | 3.23% | 0.04% | 4.37% |
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
| tinydelta + zlib | 2,198 | 23 | 4,267 |
| open-vcdiff | 7,613 | 49 | 17,040 |
| open-vcdiff + zlib | 3,518 | 54 | 6,305 |
| xdelta3 | 3,664 | 86 | 6,759 |
| bsdiff | 1,259 | 169 | 2,376 |
| detools (heatshrink) | 2,235 | 1,157 | 3,863 |
| HPatchLite, 4 KB dict. | 1,343 | 40 | 2,407 |
| HPatchLite, 32 KB dict. | 1,282 | 40 | 2,321 |
| zstd --patch-from | 1,517 | 38 | 3,010 |
