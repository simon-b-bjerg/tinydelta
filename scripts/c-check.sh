#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Builds the C library with strict warnings as errors, including
# -Wswitch-enum (every opcode handled) and -Wconversion. Extra arguments
# are passed to the compiler. Usage: scripts/c-check.sh [compiler [flags...]], e.g.
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
"$CC" "${FLAGS[@]}" -c tinydelta_io.c -o "$out/io.o"
echo "C library builds cleanly with $CC $*"
