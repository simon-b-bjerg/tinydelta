#!/usr/bin/env bash
# Builds the C library with strict warnings as errors, including
# -Wswitch-enum (every opcode handled) and -Wconversion, at several
# TINYDELTA_SCRATCH_SIZE values. Extra arguments are passed to the
# compiler. Usage: scripts/c-check.sh [compiler [flags...]], e.g.
# scripts/c-check.sh msp430-elf-gcc -mlarge
set -euo pipefail
cd "$(dirname "$0")/../c"
CC="${1:-${CC:-cc}}"
shift $(( $# > 0 ? 1 : 0 ))
FLAGS=(-std=c99 -O2 -Wall -Wextra -Wpedantic -Wconversion -Wsign-conversion
       -Wswitch-enum -Wshadow -Werror "$@")
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

"$CC" "${FLAGS[@]}" -c tinydelta_decode.c -o "$out/decode.o"
"$CC" "${FLAGS[@]}" -c tinydelta_cache.c -o "$out/cache.o"
for size in 1 7 64 256; do
  "$CC" "${FLAGS[@]}" -DTINYDELTA_SCRATCH_SIZE="$size" -c tinydelta_io.c -o "$out/io_$size.o"
done
echo "C library builds cleanly with $CC $*"
