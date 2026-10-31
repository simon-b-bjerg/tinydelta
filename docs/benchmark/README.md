# Benchmark snapshot

The patch size benchmark results shown in the main [README](../../README.md#benchmark),
kept here so the README's numbers and graphs stay fixed until they are
deliberately updated. The method, corpus and tools are described in
[`bench/README.md`](../../bench/README.md).

- **Run:** [Benchmark workflow run 41245460450](https://github.com/simon-b-bjerg/tinydelta/actions/runs/41245460450), 31 October 2026, every build byte-reproducible. MicroPython is built with a fixed version string and build date (those of `v1.25.0`) and a fixed Python hash seed, so a commit that doesn't change the compiled code gives an identical image and is skipped. Earlier snapshots built each commit with its own version and date, so more than half of their firmware pairs differed only in the startup banner, which flattered tinydelta's firmware figures.
- **Corpus:** 166 pairs. Firmware: 88 pairs from 50 builds of MicroPython (stm32 port, NUCLEO_F091RC and PYBV11), the first 25 commits per board between `v1.25.0` and `v1.26.0` that change its compiled code. Device trees: 78 pairs from 48 `.dtb` builds of three boards, consecutive commits between `v7.0-dts` and `v7.3-dts`. Pairs are 1 and 5 builds apart.
- **Compression mode:** `tinydelta_cz_twice*` columns: `encode -for-compression`, then a second tinydelta pass against an empty dictionary (also with `-max-distance 1024`).
- **Copy distance limits:** `tinydelta_window_256`, `_1k`, `_4k` columns: the plain patch with `-max-distance 256`, `1024` and `4096`.
- **open-vcdiff, zlib-compressed:** `openvcdiff_zlib`, open-vcdiff's patch compressed afterwards with zlib at level 9.
- **tiny-vcdiff:** `openvcdiff_interleaved`, open-vcdiff's interleaved format, which tiny-vcdiff reads.
- **tinydelta:** per pair, the smaller of the plain patch and compression mode decoded twice (computed by `bench/plot.py`; both are in `results.csv`). Plain is the library's default encoding (optimal, with greedy above roughly 100 KB, so greedy for the firmware images); optimal encoding at any size was not run.

| file | what |
|---|---|
| `forest.svg`, `tradeoff.svg`, `pairs.svg`, `ratio_ecdf_firmware.svg`, `ratio_ecdf_device_tree.svg`, `patch_share_firmware.svg`, `patch_share_device_tree.svg` | the graphs |
| `summary.md` | every number behind them, including patch sizes in bytes |
| `results.csv` | one row per pair: every tool's patch size |
| `decoders.json`, `decoder-size.md` | the decoder footprints and where each comes from; this run's Cortex-M0 measurement |
| `corpus.json`, `inputs/` | the corpus, and the commit and SHA-256 of every build |
| `results-tools.json` | the tool versions |

To update: run the Benchmark workflow by hand (Actions > Benchmark > Run
workflow), copy its results from the `bench-results` branch here, and
update the numbers in the README's Benchmark section in the same change.
