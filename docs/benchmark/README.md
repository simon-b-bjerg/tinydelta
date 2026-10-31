# Benchmark snapshot

The patch size benchmark results shown in the main [README](../../README.md#benchmark),
kept here so the README's numbers and graphs stay fixed until they are
deliberately updated. The method, corpus and tools are described in
[`bench/README.md`](../../bench/README.md).

- **Run:** [Benchmark workflow run 41202776240](https://github.com/simon-b-bjerg/tinydelta/actions/runs/41202776240), 31 October 2026, every build made fresh and byte-reproducible (MicroPython built with the commit's date and a fixed Python hash seed); graphs and `summary.md` regenerated from its `results.csv` with the final `bench/plot.py`. Earlier snapshots used firmware builds that were not reproducible: MicroPython's build scripts ordered some tables by randomized hashes, which reshuffled them in every build and made every firmware patch larger, for every tool (tinydelta's about twice as large, HPatchLite's about 1.5 times).
- **Corpus:** 166 pairs. Firmware: 88 pairs from 50 builds of MicroPython (stm32 port, NUCLEO_F091RC and PYBV11), consecutive commits between `v1.25.0` and `v1.26.0`. Device trees: 78 pairs from 48 `.dtb` builds of three boards, consecutive commits between `v7.0-dts` and `v7.3-dts`. Pairs are 1 and 5 builds apart.
- **Compression mode:** `tinydelta_cz_twice*` columns: `encode -for-compression`, then a second tinydelta pass against an empty dictionary (also with `-max-distance 1024`).
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
