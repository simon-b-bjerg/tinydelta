# Changelog

## Unreleased

### Format

- **Version byte.** Every patch starts with one byte, the format
  version. Patches from 1.0.0 and 1.1.0-rc.1 have none: prepend `0x01`
  to decode them with this release.
- **Format version 2**, for [compression mode](README.md#compression-mode):
  opcode 3 is `ADD_DIFF` (patch bytes added to the dictionary bytes at
  the cursor) instead of `COPY_TARGET`. Version 1 is unchanged.

### Encoder (Go, command line, Python)

- **Compression mode.** `EncodeOptions.ForCompression`, `encode
  -for-compression`, `encode(..., for_compression=True)` make the first
  pass; that patch encoded again against an empty dictionary is the one
  to send when it is smaller than the plain patch. The device decodes
  it twice with the same decoder. On the benchmark, this makes patches a
  third smaller typically, and 2.2 times smaller for large changes.
- **Copy distance limit.** `EncodeOptions.MaxTargetDistance`, `encode
  -max-distance N`, `encode(..., max_distance=N)` keep `COPY_TARGET`
  within the last N bytes of output, so the C callback decoder needs no
  `target_read` with an N-byte buffer.

### C decoders

- Both decoders read format versions 1 and 2: 472 B (memory decoder)
  and 876 B (callback decoder) of code on Cortex-M0.
- New result `TINYDELTA_ERR_VERSION` for a version byte other than 1 or
  2.
- **Breaking: the callback decoder takes one buffer of yours.**
  `tinydelta_io` gains `buffer`, `buffer_size` and `page_size` (powers
  of two, the page size dividing the buffer size). The buffer is the
  decoder's working buffer, the flash page cache and the recent output
  `COPY_TARGET` reads from. `target_write` always gets whole pages in
  order, plus a final partial page; `target_read` is only used for copies
  reaching back past the buffer, and may be `NULL`.
- **Removed:** the write cache (`tinydelta_cache.h`, `tinydelta_cache.c`),
  `target_flush` and `TINYDELTA_SCRATCH_SIZE`. The new callback decoder
  is smaller than the old one with the write cache (876 B against
  852 + 300 B on Cortex-M0).

#### Moving C code to the new callback decoder

With the write cache:

```c
static uint8_t page[256];
tinydelta_cache cache;
tinydelta_io io = { /* patch and dictionary callbacks */ .target_cap = cap };
tinydelta_cache_init(&cache, flash_read, flash_program, base, page, sizeof page);
tinydelta_cache_attach(&cache, &io);
tinydelta_decode_io(&io, &len);
```

now:

```c
uint8_t page[256];  /* may be a local array */
tinydelta_io io = { /* patch and dictionary callbacks */ .target_cap = cap,
    .target_write = flash_program, .target_read = flash_read, .target_ctx = base,
    .buffer = page, .buffer_size = sizeof page, .page_size = sizeof page };
tinydelta_decode_io(&io, &len);
```

Without the write cache, set `page_size` to the size you want each
`target_write` to have, and `buffer_size` to it or a larger power of
two. Drop `target_flush`: the final partial page is the last write.

### Benchmark

- Patch sizes on 166 real firmware and device-tree updates against six
  other delta tools, with decoder footprints, in the README's Benchmark
  section; scripts and method in [`bench/`](bench/README.md).

## 1.1.0-rc.1 (2026-10-30)

- Python package (`pip install tinydelta`): wheels for Linux and Windows
  that bundle the command-line tool.
- Conformance vectors (`testdata/vectors.txt`) for decoders in any
  language, and static analysis of the C library.
- The C decoders are tested on MSP430 in GNU's simulator and fuzzed with
  libFuzzer, AddressSanitizer and UndefinedBehaviorSanitizer.
- Windows binaries (amd64, arm64).
- Go 1.21 is the minimum version.

## 1.0.0 (2026-10-21)

First release: the format, the optimizing and greedy encoders, the Go
decoder, the C memory and callback decoders with the write cache, and
the command-line tool.
