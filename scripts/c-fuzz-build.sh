#!/usr/bin/env bash
# Builds the C fuzz harness (c/fuzz/fuzz_decode.c) with libFuzzer, for
# scripts/c-fuzz.sh (with sanitizers) and scripts/c-coverage.sh (with
# coverage instrumentation). Extra arguments are passed to the compiler.
# Usage: scripts/c-fuzz-build.sh <output binary> [flags...]
# Set CLANG to pick a compiler, e.g. CLANG=clang-18.
set -euo pipefail
cd "$(dirname "$0")/.."
out="$1"
shift
CLANG="${CLANG:-clang}"
flags=(-std=c99 -g -fsanitize=fuzzer -Ic "$@")

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
# The callback decoder at three scratch-buffer sizes, renamed apart.
for size in 1 7 64; do
  "$CLANG" "${flags[@]}" -c c/tinydelta_io.c -DTINYDELTA_SCRATCH_SIZE="$size" \
    -Dtinydelta_decode_io="fuzz_decode_io_scratch$size" -o "$work/io_$size.o"
done
"$CLANG" "${flags[@]}" c/fuzz/fuzz_decode.c c/tinydelta_decode.c c/tinydelta_cache.c \
  "$work"/io_*.o -o "$out"
