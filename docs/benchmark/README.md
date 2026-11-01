# Benchmark snapshot

The patch size benchmark results shown in the main [README](../../README.md#benchmark),
kept here so the README's numbers and graphs stay fixed until they are
deliberately updated. The method, corpus and tools are described in
[`bench/README.md`](../../bench/README.md).

- **Run:** [Benchmark workflow run 41312166591](https://github.com/simon-b-bjerg/tinydelta/actions/runs/41312166591), 1 November 2026, every build byte-reproducible. MicroPython is built with a fixed version string and build date (those of `v1.25.0`) and a fixed Python hash seed, so a commit that doesn't change the compiled code gives an identical image and is skipped. A build equal to any earlier one is skipped too: the device tree walks cross several branches and meet an older base's state again, which earlier snapshots counted as new versions (pairing changes with their own reversal, and some files with themselves).
- **Corpus:** 530 pairs, 1 and 5 builds apart. Firmware, 176 pairs: MicroPython's stm32 port (NUCLEO_F091RC, PYBV11) and samd port (ADAFRUIT_TRINKET_M0, SEEED_XIAO_SAMD21), 25 builds per board between `v1.25.0` and `v1.26.0`. Configuration, 316 pairs: six Linux `arch/arm/configs` files (228 pairs, `v5.0` to `v7.3`) and five microcontroller boards' device trees (88 pairs, `v6.5-dts` to `v7.3-dts`). Large configuration, 38 pairs: three arm64 boards' device trees, `v7.0-dts` to `v7.3-dts`.
- **Compression mode:** `tinydelta_cz_twice*` columns: `encode -for-compression`, then a second tinydelta pass against an empty dictionary (also with `-max-distance 1024`).
- **Copy distance limits:** `tinydelta_window_256`, `_1k`, `_4k` columns: the plain patch with `-max-distance 256`, `1024` and `4096`.
- **open-vcdiff, zlib-compressed:** `openvcdiff_zlib`, open-vcdiff's patch compressed afterwards with zlib at level 9.
- **tiny-vcdiff:** `openvcdiff_interleaved`, open-vcdiff's interleaved format, which tiny-vcdiff reads.
- **tinydelta:** per pair, the smaller of the plain patch and compression mode decoded twice (computed by `bench/plot.py`; both are in `results.csv`). Plain is the library's default encoding (optimal, with greedy above roughly 100 KB, so greedy for the firmware images and the larger device trees); optimal encoding at any size was not run.

| file | what |
|---|---|
| `forest.svg`, `tradeoff.svg`, `pairs.svg`, `ratio_ecdf_<kind>.svg`, `patch_share_<kind>.svg` (kinds: `firmware`, `configuration`, `large_configuration`) | the graphs |
| `summary.md` | every number behind them, including patch sizes in bytes |
| `results.csv` | one row per pair: every tool's patch size |
| `decoders.json`, `decoder-size.md` | the decoder footprints and where each comes from; this run's Cortex-M0 measurement |
| `corpus.json`, `inputs/` | the corpus, and the commit and SHA-256 of every build |
| `results-tools.json` | the tool versions |

To update: run the Benchmark workflow by hand (Actions > Benchmark > Run
workflow), copy its results from the `bench-results` branch here, and
update the numbers in the README's Benchmark section in the same change.
