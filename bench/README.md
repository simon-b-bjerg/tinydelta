# Patch size benchmark

Compares tinydelta's patch sizes with six other delta tools (open-vcdiff,
xdelta3, bsdiff, detools, HPatchLite and zstd) on real version pairs, so
the comparison reflects actual updates rather than synthetic edits.

## Corpus

Every pair is two builds of the same thing, a few commits apart, from a
fixed commit range of a public repository ([`corpus.json`](corpus.json)):

| kind | source | items | range |
|---|---|---|---|
| firmware | [MicroPython](https://github.com/micropython/micropython), stm32 port | NUCLEO_F091RC (Cortex-M0), PYBV11 (Cortex-M4), 230–365 KB | commits touching firmware code between `v1.25.0` and `v1.26.0`; built with a fixed version string and build date (those of `v1.25.0`) and a fixed Python hash seed, so every build is byte-reproducible and a commit that doesn't change the compiled code gives an identical image, which is skipped |
| firmware | [MicroPython](https://github.com/micropython/micropython), samd port | ADAFRUIT_TRINKET_M0, SEEED_XIAO_SAMD21 (Cortex-M0+, about 185 KB) | as for the stm32 port above, for commits touching the samd port |
| configuration | Linux kernel configurations ([`arch/arm/configs`](https://github.com/torvalds/linux/tree/master/arch/arm/configs)), the files as they are, 3–6 KB of text | shmobile, at91_dt, sunxi, lpc32xx, u8500, bcm2835 `_defconfig` | every non-merge commit changing the file between `v5.0` and `v7.3` (the history is cloned from 2019 on only) |
| configuration | Linux device trees as below, of microcontroller boards, 10–17 KB | stm32f746-disco, stm32f769-disco, stm32h743i-disco, stm32h750i-art-pi, lpc4357-ea4357-devkit | as for the device trees below, between `v6.5-dts` (where the 32-bit Arm trees moved into vendor directories) and `v7.3-dts` |
| large configuration | Linux device trees ([devicetree-rebasing](https://git.kernel.org/pub/scm/linux/kernel/git/devicetree/devicetree-rebasing.git)), compiled to `.dtb` | rk3588-rock-5b, imx8mp-evk, k3-am625-sk, 47–107 KB | commits touching the board's vendor directory or `include/` between `v7.0-dts` and `v7.3-dts`, every non-merge commit in topological order (the kernel's main line brings device tree changes in a few large merges) |

Up to 25 distinct builds per item: a commit whose output equals any
earlier build is skipped, as is one that does not build. (A walk through
several branches meets the state of an older base again; counting it
would pair a change with its own reversal, or a file with itself.) Pairs are 1 and 5
builds apart. The commit and SHA-256 of every build are kept with the
results, so the inputs can be rebuilt and checked.

## Tools

| tool | settings |
|---|---|
| tinydelta | the coded patch for a decoder with 777 B of coder tables (`encode -coder-tables 777`: the smaller of the plain patch and the coded ones that fit, decoded in one pass), the one an update server would send to a decoder built with the coder. This is the tinydelta in every comparison |
| tinydelta, plain or two-pass | for a decoder without the coder: per pair, the smaller of the plain patch (`encode`, the library's default: optimal, falling back to greedy above roughly 100 KB, so greedy for these firmware images) and the two-pass patch (`encode -two-pass`). In the trade-off graph next to the coded one, each with its own decoder footprint |
| tinydelta variants | in the summary and the patch-share graphs: `encode -greedy`; the two-pass patch on its own and with `-window 1024`; coded patches with `-coder-tables 522` and `1287`; the plain patch with `-window 256`, `1024` and `4096` (summary only), which the callback decoder decodes without `target_read` with a buffer of that size. Optimal at any size (`TestEncodeUnlimited`, the encoder's memory limit lifted) is available with `run.py --tools ...,tinydelta_optimal`, but is not run by default: it takes a few hundred MB and a minute or more per firmware pair |
| open-vcdiff | `encode -target_matches`, its smallest setting |
| open-vcdiff, zlib-compressed | open-vcdiff's patch compressed afterwards with zlib (DEFLATE) at level 9; the decoder would need zlib too, which the trade-off graph doesn't show |
| tiny-vcdiff | open-vcdiff with `-interleaved`, the VCDIFF variant that [tiny-vcdiff](https://github.com/jue89/tiny-vcdiff), an embedded decoder, reads; in the trade-off graph only, with tiny-vcdiff's code and RAM |
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
| `pairs.svg` | the headline: every pair, one panel per kind of data: tinydelta against open-vcdiff, HPatchLite (4 KB), detools, bsdiff and zstd, colour by tool, marker by commit step |
| `forest.svg` | per kind of data, tinydelta's patch size divided by that of open-vcdiff, xdelta3, HPatchLite (4 KB), detools, bsdiff and zstd (geometric mean, 10th-90th percentile), 1 and 5 commits apart |
| `ratio_ecdf_<kind>.svg`, `patch_share_<kind>.svg` | the full distributions, one graph per kind of data (`firmware`, `configuration`, `large_configuration`): the size ratio, and patch size as a share of the target |
| `tradeoff.svg` | each tool's median patch size against its decoder's footprint on the device: code plus RAM buffers, stack excluded |
| `summary.md` | the numbers behind the graphs, plus patch sizes in bytes |
| `decoders.json`, `decoder-size.md` | the decoder costs behind the trade-off graph, with where each comes from, and this run's measurement of the decoders that build for Cortex-M0 |
| `results.csv` | one row per pair, every tool's patch size |
| `inputs/`, `corpus.json`, `results-tools.json` | the commit and SHA-256 of every build, the corpus and the tool versions |

`pairs.svg`, `forest.svg` and `tradeoff.svg` show firmware and configuration; large configuration, larger than what tinydelta is made for, is in the summary and its own distribution graphs.

Every graph is also a PNG, for slides. Every comparison has one baseline,
"tinydelta" as defined above, as sent. Its footprint is the callback
decoder with the coder, a 256-byte buffer and 777 B of coder tables; the
trade-off graph also shows the plain or two-pass patch, at the footprint
of the callback decoder without the coder and a 256-byte buffer. The
summary lists every variant on its own, and how often two-pass wins.

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
