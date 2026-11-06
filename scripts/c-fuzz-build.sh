#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Builds a C fuzz harness with libFuzzer, for scripts/c-fuzz.sh (with
# sanitizers) and scripts/c-coverage.sh (with coverage instrumentation).
# Harnesses: c/fuzz/fuzz_decode.c (the decoders) and c/fuzz/fuzz_reloc.c
# (the relocation engine), each in three builds with the coder, and the
# decoders in a fourth without it:
#   fuzz_decode, fuzz_reloc                 MSP430X rules built in, shipped tables accepted
#   fuzz_decode_device, fuzz_reloc_device   MSP430X built in only (a device's build)
#   fuzz_decode_thumb, fuzz_reloc_thumb     Thumb built in only
#   fuzz_decode_plain                       no coder, no relocation (the smallest build)
# Extra arguments are passed to the compiler.
# Usage: scripts/c-fuzz-build.sh <build> <output binary> [flags...]
# Set CLANG to pick a compiler, e.g. CLANG=clang-18.
set -euo pipefail
cd "$(dirname "$0")/.."
build="$1" out="$2"
shift 2
CLANG="${CLANG:-clang}"
case "$build" in
  *_device) harness="${build%_device}" reloc=(-DTINYDELTA_CODER=1 -DTINYDELTA_RELOC=2) ;;
  *_thumb) harness="${build%_thumb}" reloc=(-DTINYDELTA_CODER=1 -DTINYDELTA_RELOC=1) ;;
  *_plain) harness="${build%_plain}" reloc=(-DTINYDELTA_CODER=0) ;;
  *) harness="$build" reloc=(-DTINYDELTA_CODER=1 -DTINYDELTA_RELOC=2 -DTINYDELTA_RELOC_SHIPPED=1) ;;
esac
flags=(-std=c99 -g -fsanitize=fuzzer -Ic "${reloc[@]}" "$@")

"$CLANG" "${flags[@]}" "c/fuzz/$harness.c" c/tinydelta_decode.c c/tinydelta_io.c c/tinydelta_coder.c c/tinydelta_reloc.c -o "$out"
