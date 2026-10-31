# Benchmark snapshot

The patch size benchmark results shown in the main [README](../../README.md#benchmark),
kept here so the README's numbers and graphs stay fixed until they are
deliberately updated. The method, corpus and tools are described in
[`bench/README.md`](../../bench/README.md).

- **Run:** [Benchmark workflow run 41197054361](https://github.com/simon-b-bjerg/tinydelta/actions/runs/41197054361), 31 October 2026, on the pull request that added compression mode, with every build made fresh; graphs and `summary.md` regenerated from its `results.csv` with the final `bench/plot.py`. The firmware builds of this run were not byte-reproducible (they embed the build date, and MicroPython's build scripts order some tables by Python's randomized hashes), so a later run gives patch sizes a few bytes different; later runs build with the commit date and a fixed hash seed.
- **Corpus:** 166 pairs. Firmware: 88 pairs from 50 builds of MicroPython (stm32 port, NUCLEO_F091RC and PYBV11), consecutive commits between `v1.25.0` and `v1.26.0`. Device trees: 78 pairs from 48 `.dtb` builds of three boards, consecutive commits between `v7.0-dts` and `v7.3-dts`. Pairs are 1 and 5 builds apart.
- **Compression mode:** `tinydelta_cz_twice*` columns: `encode -for-compression`, then a second tinydelta pass against an empty dictionary (also with `-max-distance 1024`). The run also measured tinydelta's patches with zlib and tinyuz; those columns are left out here.
- **tinydelta:** the library's default encoding (optimal, with greedy above roughly 100 KB, so greedy for the firmware images); optimal encoding at any size was not run.

| file | what |
|---|---|
| `forest.svg`, `tradeoff.svg`, `pairs.svg`, `ratio_ecdf.svg`, `patch_share.svg` | the graphs |
| `summary.md` | every number behind them, including patch sizes in bytes |
| `results.csv` | one row per pair: every tool's patch size |
| `decoders.json`, `decoder-size.md` | the decoder footprints and where each comes from; this run's Cortex-M0 measurement |
| `corpus.json`, `inputs/` | the corpus, and the commit and SHA-256 of every build |
| `results-tools.json` | the tool versions |

To update: run the Benchmark workflow by hand (Actions > Benchmark > Run
workflow), copy its results from the `bench-results` branch here, and
update the numbers in the README's Benchmark section in the same change.
