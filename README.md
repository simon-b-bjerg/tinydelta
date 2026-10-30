<h1 align="center">tinydelta</h1>

<p align="center">
  <b>Tiny binary patches for small devices.</b><br>
  A minimal delta format with an optimizing encoder and a C decoder that fits in under 1 KB of flash.
</p>

<p align="center">
  <a href="https://github.com/simon-b-bjerg/tinydelta/actions/workflows/ci.yml"><img src="https://github.com/simon-b-bjerg/tinydelta/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/simon-b-bjerg/tinydelta/releases"><img src="https://img.shields.io/github/v/release/simon-b-bjerg/tinydelta?sort=semver" alt="Release"></a>
  <a href="https://pkg.go.dev/github.com/simon-b-bjerg/tinydelta"><img src="https://pkg.go.dev/badge/github.com/simon-b-bjerg/tinydelta.svg" alt="Go Reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
</p>

---

tinydelta produces a patch that turns an old file (the *dictionary*) into
a new one (the *target*). It is built for updating configuration and
firmware on resource-constrained devices, where every byte sent over the
air counts and the decoder has to fit on a microcontroller.

The work is split deliberately: **the encoder does the heavy lifting** on
a server, searching hard for the smallest patch, while **the decoder
stays trivial**: four instructions, 32-bit arithmetic, no allocation and
no compression library.

## Highlights

- **Small patches.** No header, framing or checksum: a patch is just a
  stream of short instructions. An unchanged region typically costs one
  or two bytes.
- **Optimizing encoder.** Dynamic programming over exact instruction
  costs, never worse than the built-in greedy encoder, which is
  available when speed matters more.
- **Tiny C decoder.** 380 bytes of code on Cortex-M0 when everything is
  in RAM; 712 bytes when the data lives on external flash behind your
  own read/write functions, plus an optional 304-byte page write cache.
- **No dynamic allocation, no static RAM.** The C decoders never call
  `malloc` and have no global or static variables (0 bytes of `.data`
  and `.bss`). All working state is on the stack, and the only other
  memory is buffers you pass in, so the decoders are reentrant.
- **Verified.** The C decoders are tested against the Go decoder on
  valid, truncated and corrupted patches, on x86-64, 32-bit ARM and
  16-bit MSP430 (in GNU's instruction simulator), and
  fuzzed with address and undefined-behaviour sanitizers, in CI
  ([Tests](#tests)).
- **Simple, documented format.** The full wire format fits on one page
  ([below](#wire-format)), so a decoder in another language is an
  afternoon's work.

## Benchmark

Patch size for a pair of real ~3.5 KB binary device-configuration
files, in both directions:

| encoder | A → B | B → A |
|---|---:|---:|
| **tinydelta** | **277 B** | **232 B** |
| tinydelta, greedy | 297 B | 237 B |
| xdelta3 3.0, `-9 -S none` | 407 B | 336 B |
| bsdiff 4.3 | 436 B | 455 B |
| open-vcdiff, interleaved, `-target_matches` | 677 B | 520 B |
| zlib `-9` of the new file (no delta) | 916 B | 862 B |

bsdiff is built for executables, where code shifts cause many small
changes to addresses. Each patch carries a 32-byte header plus three
bzip2 streams, which is a lot of fixed overhead for small files.

Compressing a tinydelta patch afterwards barely helps at this size: the
instructions already remove most of the redundancy zlib would find, and
zlib's own overhead costs about as much as it saves.

## Why another delta format?

Established formats were not designed for a decoder that has to fit in a
few hundred bytes of flash and use no RAM:

- **VCDIFF (RFC 3284)** is a general format with windows, a 256-entry
  instruction code table, an address cache with near and same modes, and
  optional secondary compression. Its reference decoder, open-vcdiff, is
  a C++ library. A small C decoder,
  [tiny-vcdiff](https://github.com/jue89/tiny-vcdiff), only reads
  open-vcdiff's interleaved output and still needs about 4 KB of code and
  3.2 KB of static RAM.
- **bsdiff** produces a header plus three bzip2 streams. Embedded
  decoders such as [detools](https://github.com/eerimoq/detools) replace
  bzip2 with a lighter compressor (heatshrink), which brings code and
  stack back up.

tinydelta makes different trade-offs: no header, framing or compression,
four instructions, one integer encoding and 32-bit arithmetic, with all
the effort in the encoder. The decoder is simple because the format has
almost nothing to decode.

Measured on a 230 KB MicroPython firmware for a Cortex-M0 (STM32
NUCLEO-F091RC), after changing one string and rebuilding. The decoders
were built with `arm-none-eabi-gcc -Os -mthumb` and linked with
`--gc-sections`, so only code that is used counts:

| decoder | patch | code (Cortex-M0) | static RAM | stack |
|---|---:|---:|---:|---:|
| **tinydelta**, memory decoder, greedy patch | **4,530 B** | **380 B** | 0 | 64 B |
| **tinydelta**, memory decoder, optimal patch¹ | **4,431 B** | **380 B** | 0 | 64 B |
| **tinydelta**, callback decoder, greedy patch | **4,530 B** | 712 B | 0 | ~190 B |
| detools + heatshrink (default settings²) | 4,772 B | 1,948 B | 0 | ~1.6 KB |
| tiny-vcdiff (open-vcdiff, interleaved) | 10,848 B | 3,881 B | 3,221 B | ~150 B |

¹ `Encode` falls back to the greedy parse above roughly 100 KB (see
[Go library](#go-library)), so it produces the greedy patch for this
230 KB image. The optimal figure comes from lifting that limit,
which took about 30 s on one core; the patch is about 2% smaller and
decodes with the same decoder.

² detools also offers firmware-specific data formats and other
heatshrink window sizes, which were not tried and may give smaller
patches (a larger window needs more RAM in the decoder).

In short: the smallest of the decoders compared, with no static RAM and
patches as small as or smaller than the alternatives. Code size is each
decoder's own `.text` after linking, with the small test program that
calls it subtracted. tinydelta's figures are the ones from
[`scripts/c-size.sh`](scripts/c-size.sh); measured the same way as the
others they come out within 4 B of these (378 B and 716 B). Stack
figures are sums of `-fstack-usage` frames.

What you give up:

- **Speed was not compared.** The goal is a small decoder, not a fast
  one. Decode speed has not been measured on a device, where flash
  access will probably dominate, and no claim is made that it beats the
  other decoders.
- **Small patch reads.** The callback decoder reads the patch a few bytes
  at a time. On slow flash, wrap `patch_read` in a small buffer.
- **No checksum, no VCDIFF compatibility.** Send a CRC or hash with the
  patch (see [On the device](#on-the-device)). Existing VCDIFF tools
  cannot read or write tinydelta patches.
- **One firmware project.** These numbers come from one project and one
  kind of change. Data and use cases differ a lot, so compare the
  formats on your own files before choosing; `tinydelta encode -v`
  prints the patch size.

The size of a firmware patch depends heavily on the build, not only on
the encoder. In the test above, changing one string made the patch about
4.5 KB (2% of the image): the rebuilt image differed in several hundred
places spread across it, which looks like moved addresses and offsets,
and the data at the end was shifted by a few bytes. A reproducible build
with a stable layout should help any delta format, this one included.

## Contents

- [Why another delta format?](#why-another-delta-format)
- [Installation](#installation)
- [Command-line usage](#command-line-usage)
- [Go library](#go-library)
- [C library](#c-library)
- [Wire format](#wire-format)
- [How the encoder works](#how-the-encoder-works)
- [Development](#development)
- [License](#license)

## Installation

### Command-line tool

Download a prebuilt static binary for Linux or Windows (amd64 or arm64)
from the [releases page](https://github.com/simon-b-bjerg/tinydelta/releases):

```sh
VERSION=v1.0.0   # pick the latest release
curl -fsSLO "https://github.com/simon-b-bjerg/tinydelta/releases/download/${VERSION}/tinydelta_${VERSION}_linux_amd64.tar.gz"
tar -xzf "tinydelta_${VERSION}_linux_amd64.tar.gz"
sudo install "tinydelta_${VERSION}_linux_amd64/tinydelta" /usr/local/bin/
```

For Windows (amd64 or arm64), download
`tinydelta_<version>_windows_amd64.zip` (or `_arm64`) from the same page, unzip it and
put `tinydelta.exe` somewhere on your `PATH`.

Each release includes `checksums.txt` with SHA-256 sums of its files.

Or build it with Go:

```sh
go install github.com/simon-b-bjerg/tinydelta/cmd/tinydelta@latest
```

### Go library

```sh
go get github.com/simon-b-bjerg/tinydelta
```

### C library

Copy the files you need from [`c/`](c/) into your project (see
[C library](#c-library)), or download `tinydelta-c_<version>.tar.gz`
from a release.

## Command-line usage

```text
tinydelta encode [-greedy] [-max-len N] [-v] <dictionary> <target> [<patch>]
tinydelta decode [-max-size N] [-v] <dictionary> <patch> [<target>]
tinydelta version
```

```console
$ tinydelta encode -v old.cfg new.cfg update.td
dictionary 3566 B, target 3501 B -> patch 277 B (7.9% of target)

$ tinydelta decode old.cfg update.td restored.cfg
```

An omitted output file, or `-`, means standard output, and a patch of
`-` is read from standard input, so the commands compose with pipes.
`encode -greedy` trades a few percent of patch size for much faster
encoding. `encode -max-len N` makes sure no single instruction produces
more than N target bytes (see [below](#limiting-instruction-length)).
`decode -max-size` rejects patches whose output would exceed
the given size. The exit status is 0 on success, 1 on failure and 2 on
a usage error.

## Go library

```go
import "github.com/simon-b-bjerg/tinydelta"

patch, err := tinydelta.Encode(oldFile, newFile)       // optimizing
patch, err := tinydelta.EncodeGreedy(oldFile, newFile) // fast

patch, err := tinydelta.EncodeWith(oldFile, newFile, tinydelta.EncodeOptions{
	MaxInstructionLen: 1024, // no instruction produces more than 1024 bytes
})

newFile, err := tinydelta.Decode(oldFile, patch)
newFile, err := tinydelta.DecodeReaderLimit(oldFile, r, maxSize) // from an io.Reader, capped
```

| function | notes |
|---|---|
| `Encode` | Optimizing parse; roughly 100–300 ms for a few KB. Never larger than `EncodeGreedy`'s patch; falls back to greedy above roughly 100 KB to bound memory. |
| `EncodeGreedy` | One pass with one byte of lookahead; well under a millisecond for a few KB, patches typically a few percent larger. |
| `EncodeWith` | Either encoder, chosen with `EncodeOptions.Greedy`, with options such as `MaxInstructionLen`. |
| `Decode`, `DecodeReader` | Apply a patch from a byte slice or an `io.Reader`. |
| `DecodeReaderLimit` | As `DecodeReader`, but fails with `ErrTooLarge` before the target would exceed a size you choose. |

### Limiting instruction length

`EncodeOptions.MaxInstructionLen` (CLI: `encode -max-len N`) caps how
many target bytes any one `ADD`, `SKIP`, `COPY` or `COPY_TARGET` may
produce; longer runs are split into several instructions. This helps a
decoder that handles one instruction at a time, e.g. one that needs a
bound on how long a single step takes or how much it writes. The format
and decoders are unchanged, so limited patches decode anywhere. The cost
is small: on the benchmark pair (A → B), the patch grows from 277 B to
281 B with a limit of 1024, and to 348 B with a limit of 64.

Dictionary and target are limited to `MaxSize` (2³⁰ − 1 bytes); larger
inputs return `ErrTooLarge`. Malformed patches return an error, never a
panic.

## C library

Plain C99 in [`c/`](c/): 32-bit arithmetic only, no dynamic allocation,
no static RAM (0 B `.data`/`.bss`; reentrant), and no dependencies
beyond `memcpy`.

| part | files | use it when |
|---|---|---|
| **memory decoder** | `tinydelta.h`, `tinydelta_internal.h`, `tinydelta_decode.c` | dictionary, patch and output buffer are all in RAM |
| **callback decoder** | `tinydelta.h`, `tinydelta_internal.h`, `tinydelta_io.h`, `tinydelta_io.c` | the data is elsewhere, e.g. external flash, reached through your own read/write functions |
| **write cache** (optional) | `tinydelta_cache.h`, `tinydelta_cache.c` | with the callback decoder, to program flash a whole page at a time |

### Code size and stack

`arm-none-eabi-gcc` 10.3, `-Os -mthumb`, default
`TINYDELTA_SCRATCH_SIZE` (64). Code size is `.text`; `memcpy` comes from
your C library, and nothing else is pulled in (no software division
even on Cortex-M0). Reproduce with [`scripts/c-size.sh`](scripts/c-size.sh).

| part | Cortex-M0 | Cortex-M3 | Cortex-M4 | stack (Cortex-M0) |
|---|---:|---:|---:|---:|
| memory decoder | 380 B | 362 B | 366 B | 64 B |
| callback decoder | 712 B | 660 B | 660 B | ~200 B\* |
| write cache | 304 B | 288 B | 288 B | +32 B |

\* Deepest path, including the 64-byte internal buffer, excluding your
callbacks' own stack use.

On 16-bit MSP430 the code is roughly two and a half times larger, as
every 32-bit operation takes several 16-bit instructions.
`msp430-elf-gcc` 14.2, `-Os`; reproduce with
[`scripts/c-size-msp430.sh`](scripts/c-size-msp430.sh).

| part | MSP430 | MSP430X | MSP430X, `-mlarge` | stack (`-mlarge`) |
|---|---:|---:|---:|---:|
| memory decoder | 964 B | 932 B | 1,068 B | 112 B |
| callback decoder | 1,740 B | 1,682 B | 1,736 B | ~245 B\* |
| write cache | 628 B | 602 B | 678 B | +40 B |

### Results

Every decoding function returns a `tinydelta_result`:

| value | meaning |
|---|---|
| `TINYDELTA_OK` | success |
| `TINYDELTA_ERR_MALFORMED` | corrupt or truncated patch, or one that doesn't fit the dictionary |
| `TINYDELTA_ERR_TOO_LARGE` | the target would exceed its capacity, or the dictionary exceeds `TINYDELTA_MAX_SIZE` |
| `TINYDELTA_ERR_IO` | one of your callbacks failed (callback decoder only) |

On any error, whatever was already written to the target is incomplete
and must not be used.

### Memory decoder

```c
tinydelta_result tinydelta_decode(const uint8_t *dict, uint32_t dict_len,
                                  const uint8_t *patch, uint32_t patch_len,
                                  uint8_t *out, uint32_t out_cap,
                                  uint32_t *out_len);
```

Writes the target into `out` and sets `*out_len`. It fails with
`TINYDELTA_ERR_TOO_LARGE` rather than write past `out_cap`.

### Callback decoder

```c
tinydelta_result tinydelta_decode_io(const tinydelta_io *io, uint32_t *target_len);
```

[`tinydelta_io`](c/tinydelta_io.h) holds your callbacks, a context
pointer for each data source, and the sizes:

| data | callbacks | access pattern |
|---|---|---|
| patch | `patch_read` | front to back, a few bytes at a time |
| dictionary | `dict_read` | random offsets |
| target | `target_write`, `target_read`, `target_flush` | writes are strictly sequential; reads are at random offsets and must return anything already written, even if still buffered; `target_flush` (optional, may be `NULL`) is called once at the end |

Offsets are relative to the start of the patch, dictionary or target. A
callback returns `TINYDELTA_CALLBACK_OK` on success; anything else stops
decoding with `TINYDELTA_ERR_IO`.

Data moves through an internal stack buffer of `TINYDELTA_SCRATCH_SIZE`
bytes (default 64; define it when compiling `tinydelta_io.c` to change
it). A larger buffer means fewer, larger callback calls. Runs of a
repeated byte, and other short repeating patterns, are expanded inside
the buffer and written in buffer-sized pieces rather than byte by byte.

### Write cache

[`tinydelta_cache`](c/tinydelta_cache.h) provides the three target
callbacks on top of your raw flash read and program functions. Writes
collect in a page buffer you provide, and flash is programmed one whole
page at a time at increasing, page-aligned offsets, plus a final partial
page. Reads come from flash for data already programmed and from the
page buffer for the rest, including reads that span both.

Example: patch, dictionary and target in three regions of one SPI flash.

```c
#include "tinydelta_cache.h"

/* Your flash driver; addresses here are made up. */
extern int spi_flash_read(uint32_t addr, uint8_t *buf, uint32_t len);
extern int spi_flash_program(uint32_t addr, const uint8_t *buf, uint32_t len);
#define DICT_BASE   0x000000u   /* current config */
#define PATCH_BASE  0x010000u   /* received patch */
#define TARGET_BASE 0x020000u   /* new config, erased beforehand */

static int rd(void *base, uint32_t off, uint8_t *buf, uint32_t len) {
    return spi_flash_read((uint32_t)(uintptr_t)base + off, buf, len);
}
static int wr(void *base, uint32_t off, const uint8_t *buf, uint32_t len) {
    return spi_flash_program((uint32_t)(uintptr_t)base + off, buf, len);
}

tinydelta_result apply_patch(uint32_t dict_len, uint32_t patch_len,
                             uint32_t target_cap, uint32_t *target_len) {
    static uint8_t page[256];            /* one flash page */
    tinydelta_cache cache;
    tinydelta_io io = {
        .patch_read = rd, .patch_ctx = (void *)(uintptr_t)PATCH_BASE, .patch_len = patch_len,
        .dict_read  = rd, .dict_ctx  = (void *)(uintptr_t)DICT_BASE,  .dict_len  = dict_len,
        .target_cap = target_cap,
    };
    tinydelta_cache_init(&cache, rd, wr, (void *)(uintptr_t)TARGET_BASE, page, sizeof page);
    tinydelta_cache_attach(&cache, &io);
    return tinydelta_decode_io(&io, target_len);   /* TINYDELTA_OK on success */
}
```

### On the device

A patch is deliberately bare: it has no header, no checksum, no length
and no end marker. It doesn't say which dictionary it was made for, and
a patch cut off between two instructions still decodes, into a shorter
target. Those checks belong in your update protocol, which already has
to carry the patch and usually has its own metadata.

#### Suggested integration

Send these alongside the patch, for example in the message or manifest
that announces the update:

| field | why |
|---|---|
| format version | the patch has no version marker of its own |
| dictionary CRC or hash | applying a patch to the wrong base image decodes "successfully" into garbage |
| patch length | the decoder runs until the patch ends, so a missing tail can't be detected from the patch alone |
| target length | caps the output and detects a short result |
| target CRC or hash | the final check that the result is exactly what was encoded |

Use a CRC-32 against accidental damage, or a cryptographic hash such as
SHA-256 when the update must also be authenticated. In that case, sign
the metadata (or the target hash), not the patch: the same target can
be described by different patches.

Then, on the device:

1. **Check the dictionary** against its CRC or hash before decoding.
2. **Check the patch** has the announced length (and, if your transport
   doesn't already guarantee it, its own CRC).
3. **Decode into a separate region**, never over the dictionary:
   patching in place isn't supported. Erase it first, or erase as you
   write.
4. **Cap the output at the target length**: pass it as `out_cap` /
   `target_cap`. A short, valid patch can describe a very large target,
   and the decoders stop at the cap.
5. **Check the result**: `out_len` / `target_len` must equal the target
   length, and the target's CRC or hash must match. Only then switch
   over.

On any failure, discard the target region and keep running from the
dictionary.

## Wire format

A patch is a sequence of instructions, decoded until the patch ends.
There is nothing before, between or after them. Decoding needs the
**dictionary** (the old file) and produces the **target** (the new file).

### Integers

Every integer is an unsigned **LEB128 varint**: 7 bits per byte, least
significant group first, high bit (`0x80`) set on every byte except the
last. A varint holds at most 32 bits: at most **5 bytes**, and the 5th
byte must be `0x00`–`0x0F`. Anything longer is invalid.

```text
300 = 0b10_0101100  ->  0xAC 0x02     (0x2C | 0x80, then 0x02)
```

Signed deltas are **zigzag**-encoded first, so small values of either
sign stay short: 0, −1, 1, −2, 2, … → 0, 1, 2, 3, 4, …
Decode with `delta = (z >> 1) ^ -(z & 1)`.

### Instructions

Each instruction starts with one varint `v`:

```text
opcode = v & 3
len    = v >> 2        // number of target bytes this instruction produces
```

| opcode | name | followed by | produces |
|---|---|---|---|
| 0 | `COPY` | zigzag `delta` | `len` bytes from `dict[cursor + delta]`, then `cursor = cursor + delta + len` |
| 1 | `ADD` | `len` literal bytes | those bytes, as-is |
| 2 | `SKIP` | — | `len` bytes from `dict[cursor]` |
| 3 | `COPY_TARGET` | zigzag `delta` | `len` bytes from the target already produced, starting at `pos + delta` (must be `< pos`), copied byte by byte |

### State

The decoder keeps two counters, both starting at 0:

- **`pos`**: bytes of target produced so far; advances by `len` after
  every instruction.
- **`cursor`**: a read position in the dictionary; advances by `len`
  after every instruction, except that `COPY` sets it to just past the
  bytes it copied.

Without any `COPY`, `cursor` tracks `pos`, so an unchanged region of a
same-layout file is a single `SKIP`: one byte for runs of up to 31
bytes, two bytes up to 4095.

### Rules

- `COPY` with `len = 0` copies nothing but still moves `cursor`: it is
  how an encoder realigns the cursor after an insertion or deletion.
- `COPY_TARGET` copies byte by byte, so its source may overlap the bytes
  it is writing: `delta = -1` repeats the previous byte for any length.
- `COPY` and `SKIP` must stay within the dictionary; `COPY_TARGET` must
  start strictly before `pos`. Anything else is a malformed patch.
- Dictionary and target are each at most 2³⁰ − 1 bytes, which keeps
  every varint, position and delta within 32 bits.

### Example

```text
dictionary: "the quick brown fox jumps over the lazy dog"
target:     "the quick BROWN fox jumps over the lazy dog!"
patch:      2a 15 42 52 4f 57 4e 72 05 21          (10 bytes)

2a          SKIP 10        (10<<2 | 2)   "the quick "
15 ...      ADD 5          (5<<2 | 1)    "BROWN"
72          SKIP 28        (28<<2 | 2)   " fox jumps over the lazy dog"
05 21       ADD 1          (1<<2 | 1)    "!"
```

### Stability

The wire format is fixed for the 1.x releases: a patch made by any 1.x
encoder decodes with any 1.x decoder, Go or C. The format has no version
marker, so an incompatible change would be a new major version. Encoders
may still produce different, usually smaller, patches for the same input
in later releases; they stay valid for every 1.x decoder. The Go API
follows [semantic versioning](https://semver.org).

## How the encoder works

Both encoders find match candidates in the dictionary and in the target
produced so far with hash chains over 4-byte sequences, plus a direct
comparison at the cursor so that even 1–3 byte unchanged runs become a
`SKIP`.

- **Greedy** walks the target once, taking at each position the longest
  match worth more than its encoding cost, with one byte of lookahead.
- **Optimizing** (the default) runs dynamic programming over (target
  position, cursor offset), pricing every instruction at its exact
  encoded size. The cursor is what makes this worthwhile: a slightly
  shorter match that keeps the cursor aligned can save more later than
  a longer one that moves it, which is where greedy loses a few percent.

## Development

```sh
go test ./...          # Go tests, plus the C decoders if a C compiler is installed
scripts/c-check.sh     # strict C build (-Wall -Wextra -Wconversion -Wswitch-enum ... -Werror)
scripts/c-fuzz.sh 300  # fuzz the C decoders for 5 min (needs clang with libFuzzer)
scripts/c-size.sh      # Cortex-M code size and stack (needs arm-none-eabi-gcc)
scripts/c-size-msp430.sh  # MSP430 code size and stack (needs msp430-elf-gcc)
go test -run '^$' -fuzz '^FuzzEncodeRoundTrip$' -fuzztime 5m .   # fuzz the encoders
```

### Tests

What is tested, and how:

| test | what it checks |
|---|---|
| Go unit tests (`decode_test.go`, `format_test.go`) | each instruction, varint and zigzag edge cases, the 32-bit varint limit, and that malformed patches (truncated, out of range, forward `COPY_TARGET`, huge `ADD`) fail cleanly instead of panicking |
| Round trips (`interop_test.go`, `limit_test.go`) | both encoders on fixed 3 KB fixtures and random mutations decode back exactly; `MaxInstructionLen` is respected; the optimizing encoder is never worse than greedy |
| `FuzzEncodeRoundTrip` (Go fuzzing) | derives a target from a fuzzed dictionary with a fuzzed edit script (overwrite, insert, delete, repeat, append); both encoders' patches must decode back to it, decode with an output cap of exactly the target length but fail one byte short, respect `MaxInstructionLen`, and the optimizing patch must not be larger than the greedy one |
| `TestCDecoderMatchesGo`, `TestCIODecoderMatchesGo` | both C decoders against the Go decoder on valid, truncated and corrupted patches and too-small output buffers, at several scratch-buffer and flash-page sizes; the test driver polices the callback contract (sequential whole-page programming, no reads of unwritten data) |
| `TestCIODecoderCallbackFailures` | a failing read, write or flush callback stops decoding with `TINYDELTA_ERR_IO` |
| C fuzzing ([`c/fuzz/fuzz_decode.c`](c/fuzz/fuzz_decode.c)) | libFuzzer with AddressSanitizer and UndefinedBehaviorSanitizer, seeded with patches from the Go encoders. Every input goes through an independent reference decoder written from the wire format, the memory decoder, and the callback decoder at scratch sizes 1, 7 and 64, each with and without the page cache. All must agree on the result and the target bytes, and the callbacks abort on any breach of the contract, as the sanitizers do on any memory error or undefined behaviour |
| `scripts/c-check.sh` | the C library builds with strict warnings as errors, at scratch sizes 1, 7, 64 and 256 |

#### How the fuzzing works

A fuzzer runs a test over and over with generated inputs. It starts
from a few seed inputs, mutates them (flips bits, inserts, deletes and
splices bytes) and watches which code each input reaches. Inputs that
reach new code are kept and mutated further, so over time it works its
way into rarely taken paths, such as a varint cut off at its 5th byte
or a copy that ends exactly at the end of the dictionary, that
hand-written tests tend to miss. Both fuzzers check properties that
must hold for every input, rather than comparing with expected output.

There are two, one for each side of the format:

- **`FuzzEncodeRoundTrip` tests the encoders** with Go's built-in
  fuzzing. Each input is a dictionary, an edit script and an
  instruction length limit. The target is built by applying the script
  to the dictionary, so dictionary and target are related as in a real
  update and the encoders produce every instruction type; two unrelated
  random files would only give one long `ADD`. Both encoders must then
  produce a patch that decodes back to the target and meets the checks
  in the table above.
- **`c/fuzz/fuzz_decode.c` tests the C decoders** with libFuzzer. Each
  input is a raw patch with a dictionary and an output capacity, so the
  fuzzer can produce anything: valid patches, truncated ones, out of
  range offsets, oversized lengths. It starts from 300 real patches
  from the Go encoders, so most inputs are nearly valid and reach deep
  into the decoders instead of being rejected at the first byte. The C
  decoders must give the same answer as a separate reference decoder,
  for valid and invalid patches alike, and must never touch memory
  outside their buffers. AddressSanitizer and
  UndefinedBehaviorSanitizer turn any out-of-bounds access, overflow
  or undefined behaviour into an immediate failure, even when the
  output happens to be correct.

What runs in CI, on every push to `main` and every pull request:

| job | runs |
|---|---|
| Go tests (Go 1.21 and current) | `gofmt`, `go vet`, `go test ./...` (including the C decoder tests, built with gcc on x86-64); on current Go also 60 s of `FuzzEncodeRoundTrip` |
| C decoders fuzzed | `scripts/c-fuzz.sh 60`; crashing inputs are uploaded as a build artifact |
| Go tests on Windows | `go test ./...` for the library and the CLI |
| C library on 32-bit ARM | `scripts/c-check.sh` for x86-64 and ARM, the C decoder tests on 32-bit ARM under QEMU, and the Cortex-M code size and stack report (informational) |
| C library on MSP430 | `scripts/c-check.sh` for 16-bit MSP430 and MSP430X (small and large memory models), the C decoder tests on MSP430X under GNU's MSP430 instruction simulator, and the MSP430 code size and stack report (informational). The toolchain is built from GNU sources by [`scripts/msp430-toolchain.sh`](scripts/msp430-toolchain.sh) and cached |

Not covered: nothing runs on a real microcontroller; the Cortex-M and
MSP430 figures are measured from the compiled code, not on hardware.

To run the C decoder tests on another architecture, set a cross
compiler and an emulator, as CI does for 32-bit ARM:

```sh
TINYDELTA_CC="arm-linux-gnueabihf-gcc -static" TINYDELTA_C_RUNNER=qemu-arm go test -run TestC .
```

or for MSP430, as a bare-metal program in the simulator.
[`scripts/sim-run.sh`](scripts/sim-run.sh) hands the program its
arguments in a file, as bare-metal startup code passes none:

```sh
scripts/msp430-toolchain.sh && export PATH="$HOME/.cache/msp430/bin:$PATH"   # once, ~15 min
TINYDELTA_CC="msp430-elf-gcc -mcpu=msp430x -mlarge -msim" \
TINYDELTA_C_RUNNER="scripts/sim-run.sh msp430-elf-run" go test -run TestC .
```

A failing input from `FuzzEncodeRoundTrip` lands in `testdata/fuzz/`
and becomes a regression test once committed. One from
`scripts/c-fuzz.sh` lands in `fuzz-artifacts/`; replay it by passing
the file to the built `fuzz_decode` binary.

Optional tests use local data when present: `TestRealConfigs`
round-trips every pair of `*.cfg` files in each subdirectory of
`testdata/private/` (git-ignored, or set `TINYDELTA_CONFIG_DIR`), and
`TestSizeVsOpenVCDiff` compares patch sizes with open-vcdiff when
`OPENVCDIFF_BIN` points at its `vcdiff` binary.

### Releases

The [release workflow](.github/workflows/release.yml) tests the tagged
code, builds static Linux and Windows binaries for amd64 and arm64 plus
a C source archive with
[`scripts/build-release.sh`](scripts/build-release.sh), and attaches
them to the GitHub release with SHA-256 checksums. It runs when:

- a tag of the form `vX.Y.Z` is pushed (it creates the release);
- a release is published in the GitHub UI (it adds the files to it);
- you start it by hand under Actions > Release > Run workflow and give
  an existing tag.

```sh
git tag v1.0.0 && git push origin v1.0.0
```

To try the release build without publishing anything, start it by hand
and leave the tag empty: it builds the selected branch, versioned
`dev-<commit>`, and attaches the archives to the workflow run as
`release-archives`, where you can download them. A pull request that
changes the release workflow or `scripts/build-release.sh` does the
same automatically.

### Contributing

Issues and pull requests are welcome. Please keep `go test ./...` and
`scripts/c-check.sh` passing and code `gofmt`-formatted. A change to the
wire format must update this README, both Go encoders, the Go decoder
and both C decoders together, with tests.

## License

[MIT](LICENSE) © 2026 Simon Bjerg
