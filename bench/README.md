# Patch size benchmark

Compares tinydelta's patch sizes with open-vcdiff, xdelta3 and bsdiff on
real version pairs, so the comparison reflects actual updates rather than
synthetic edits.

## Corpus

Every pair is two builds of the same thing, a few commits apart, from a
fixed commit range of a public repository ([`corpus.json`](corpus.json)):

| kind | source | items | range |
|---|---|---|---|
| firmware | [MicroPython](https://github.com/micropython/micropython), stm32 port | NUCLEO_F091RC (Cortex-M0), PYBV11 (Cortex-M4) | commits touching firmware code between `v1.25.0` and `v1.26.0` |
| device tree | Linux device trees ([devicetree-rebasing](https://git.kernel.org/pub/scm/linux/kernel/git/devicetree/devicetree-rebasing.git)), compiled to `.dtb` | rk3588-rock-5b, imx8mp-evk, k3-am625-sk | commits touching the board's vendor directory or `include/` between `v7.0-dts` and `v7.3-dts`, every non-merge commit in topological order (the kernel's main line brings device tree changes in a few large merges) |

Up to 25 distinct builds per item: a commit that leaves the output
unchanged is skipped, as is one that does not build. Pairs are 1 and 5
builds apart. The commit and SHA-256 of every build are kept with the
results, so the inputs can be rebuilt and checked.

## Tools

| tool | settings |
|---|---|
| tinydelta | optimal at any size (the library's encoder with its memory limit lifted, through `TestEncodeUnlimited`; the comparisons use this); also the library's default, which falls back to greedy above roughly 100 KB as it does for most firmware, and `encode -greedy` |
| tinydelta + zlib | the optimal patch at zlib level 9, compared with the same tools; zlib's framing makes very small patches larger |
| open-vcdiff | `encode -target_matches`, its smallest setting; also its output zlib-compressed at level 9, the pipeline tinydelta replaces |
| xdelta3 | `-9 -S none` (VCDIFF, no secondary compression) |
| bsdiff | bsdiff 4 (bzip2 inside) |
| detools | `create_patch -c heatshrink`: sequential patch, the embedded bsdiff variant |
| HPatchLite | `hdiffi -c-tuz-4k` and `-c-tuz-32k`: HDiffPatch's MCU variant with tinyuz compression, at a 4 KB and its default 32 KB dictionary (the decoder's RAM) |
| zstd | `-19 --patch-from`: a general-purpose reference; its decoder needs the whole old file as its window, so it does not fit a microcontroller |

The graphs compare like with like: tinydelta with the uncompressed formats
(open-vcdiff, xdelta3), and tinydelta + zlib with the compressed ones.
Patch size is only half the comparison for a device: the decoders also
differ in code size and RAM.

Every patch is decoded again and compared with the target before its size
counts.

## Results

Each manual run of the Benchmark workflow (Actions > Benchmark > Run
workflow) commits its results to the [`bench-results`](../../tree/bench-results)
branch: `results.csv` (one row per pair), `summary.md` (per kind and step:
geometric mean, median and 10th/90th percentile of tinydelta's size ratio
to each tool, and how often tinydelta is smaller), `ratio_ecdf.svg`,
`patch_share.svg`, the tool versions, and the input commits and hashes.
A pull request that changes the benchmark runs it too, with the results
only attached to the run.

## Running it locally

Needs git, Go, Python 3 with matplotlib and detools, `arm-none-eabi-gcc`,
`cpp`, `dtc`, `xdelta3`, `bsdiff`, `zstd`, and open-vcdiff's `vcdiff` and
HPatchLite's `hdiffi`/`hpatchi` on PATH (`run.py --tools` leaves out what
you don't have).

```sh
go build -o /tmp/tinydelta ./cmd/tinydelta
python3 bench/collect.py bench/.cache          # clones and builds; slow the first time
go test -c -o /tmp/tinydelta-optimal.test .
TINYDELTA=/tmp/tinydelta TINYDELTA_OPTIMAL=/tmp/tinydelta-optimal.test \
  python3 bench/run.py bench/.cache results/results.csv
python3 bench/plot.py results/results.csv results
```

Pairs are encoded in parallel, one per CPU (`BENCH_JOBS` to change it);
optimal encoding of firmware needs about 700 bytes of memory per target
byte, so a few hundred MB per job. `run.py --tools tinydelta,xdelta3`
runs a subset of the tools, and
`BENCH_CORPUS=<file>` points the scripts at another corpus file.
