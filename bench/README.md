# Patch size benchmark

Compares tinydelta's patch sizes with open-vcdiff, xdelta3 and bsdiff on
real version pairs, so the comparison reflects actual updates rather than
synthetic edits.

## Corpus

Every pair is two builds of the same thing, a few commits apart, from a
fixed commit range of a public repository ([`corpus.json`](corpus.json)):

| kind | source | items | range |
|---|---|---|---|
| firmware | [MicroPython](https://github.com/micropython/micropython), stm32 port | NUCLEO_F091RC (Cortex-M0), PYBV11 (Cortex-M4) | commits touching firmware code between `v1.25.0` and `v1.26.0`; built with the commit's date as build date and a fixed Python hash seed, so every build is byte-reproducible |
| device tree | Linux device trees ([devicetree-rebasing](https://git.kernel.org/pub/scm/linux/kernel/git/devicetree/devicetree-rebasing.git)), compiled to `.dtb` | rk3588-rock-5b, imx8mp-evk, k3-am625-sk | commits touching the board's vendor directory or `include/` between `v7.0-dts` and `v7.3-dts`, every non-merge commit in topological order (the kernel's main line brings device tree changes in a few large merges) |

Up to 25 distinct builds per item: a commit that leaves the output
unchanged is skipped, as is one that does not build. Pairs are 1 and 5
builds apart. The commit and SHA-256 of every build are kept with the
results, so the inputs can be rebuilt and checked.

## Tools

| tool | settings |
|---|---|
| tinydelta | `encode`, as the library does it, which the comparisons use: optimal, falling back to greedy above roughly 100 KB, so greedy for these firmware images; also `encode -greedy`. Optimal at any size (`TestEncodeUnlimited`, the encoder's memory limit lifted) is available with `run.py --tools ...,tinydelta_optimal`, but is not run by default: it takes a few hundred MB and a minute or more per firmware pair |
| tinydelta + zlib | the tinydelta patch at zlib level 9; zlib's framing makes very small patches larger |
| tinydelta, compression mode | `encode -for-compression` (format version 2 where it compresses better), then: zlib level 9; tinyuz `-c-4k`, the compressor and window HPatchLite 4 KB uses; or a second tinydelta pass against an empty dictionary, also with `-max-distance 1024`. Shown in the trade-off graph and the summary, not in the forest graph |
| open-vcdiff | `encode -target_matches`, its smallest setting; also its output zlib-compressed at level 9, the pipeline tinydelta replaces |
| xdelta3 | `-9 -S none` (VCDIFF, no secondary compression) |
| bsdiff | bsdiff 4 (bzip2 inside) |
| detools | `create_patch -c heatshrink`: sequential patch, the embedded bsdiff variant |
| HPatchLite | `hdiffi -c-tuz-4k` and `-c-tuz-32k`: HDiffPatch's MCU variant with tinyuz compression, at a 4 KB and its default 32 KB dictionary (the decoder's RAM) |
| zstd | `-19 --patch-from`: a general-purpose reference; its decoder needs the whole old file as its window, so it does not fit a microcontroller |

Patch size is only half the comparison for a device: the decoders also
differ in code size and RAM, which `tradeoff.svg` shows next to it.

Every patch is decoded again and compared with the target before its size
counts.

## Results

Each manual run of the Benchmark workflow (Actions > Benchmark > Run
workflow) commits its results to the [`bench-results`](../../tree/bench-results)
branch; a pull request that changes the benchmark runs it too and
commits to `bench-results-preview`, replaced each time. The results:

| file | what |
|---|---|
| `forest.svg` | the headline: per kind of data and tool, tinydelta's patch size divided by the tool's (geometric mean, 10th-90th percentile), 1 and 5 commits apart |
| `pairs.svg` | every pair: tinydelta against open-vcdiff, HPatchLite (4 KB) and bsdiff |
| `ratio_ecdf.svg`, `patch_share.svg` | the full distributions: the size ratio, and patch size as a share of the target |
| `tradeoff.svg` | each tool's median patch size against its decoder's footprint on the device: code plus RAM buffers, stack excluded |
| `summary.md` | the numbers behind the graphs, plus patch sizes in bytes |
| `decoders.json`, `decoder-size.md` | the decoder costs behind the trade-off graph, with where each comes from, and this run's measurement of the decoders that build for Cortex-M0 |
| `results.csv` | one row per pair, every tool's patch size |
| `inputs/`, `corpus.json`, `results-tools.json` | the commit and SHA-256 of every build, the corpus and the tool versions |

Every graph is also a PNG, for slides. Every comparison has one baseline:
tinydelta's patch as it is sent, uncompressed. tinydelta + zlib is a row
like any other tool, showing what compression would add; what it costs
the decoder shows in the trade-off graph.

Decoder footprints ([`decoders.json`](decoders.json)) are code plus the
RAM buffers a decoder needs (dictionaries, windows, caches); stack is
recorded but not counted. Code is measured where the decoder builds for
a microcontroller (`decoder-size.sh`, Cortex-M0, `-Os`); buffers come
from the measurement or the tool's documentation. A decoder that needs
the whole old file in RAM gets the median old file of that kind of data
added. Where code size is unknown or the old file is added, the point is
a lower bound.

## Running it locally

Needs git, Go, Python 3 with matplotlib and detools, `arm-none-eabi-gcc`,
`cpp`, `dtc`, `xdelta3`, `bsdiff`, `zstd`, and open-vcdiff's `vcdiff` and
HPatchLite's `hdiffi`/`hpatchi` on PATH (`run.py --tools` leaves out what
you don't have).

```sh
go build -o /tmp/tinydelta ./cmd/tinydelta
python3 bench/collect.py bench/.cache          # clones and builds; slow the first time
TINYDELTA=/tmp/tinydelta python3 bench/run.py bench/.cache results/results.csv
python3 bench/plot.py results/results.csv results
```

Pairs are encoded in parallel, one per CPU (`BENCH_JOBS` to change it).
To include optimal encoding at any size, build the test binary with
`go test -c -o /tmp/tinydelta-optimal.test .`, set
`TINYDELTA_OPTIMAL=/tmp/tinydelta-optimal.test` and add
`tinydelta_optimal` to `--tools`; it needs about 700 bytes of memory per
target byte. `run.py --tools tinydelta,xdelta3`
runs a subset of the tools, and
`BENCH_CORPUS=<file>` points the scripts at another corpus file.
