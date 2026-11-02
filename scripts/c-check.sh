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

for coder in 0 1; do # without and with coded patches (TINYDELTA_CODER)
  for f in tinydelta_decode tinydelta_io tinydelta_coder; do
    "$CC" "${FLAGS[@]}" -DTINYDELTA_CODER=$coder -c $f.c -o "$out/$f-$coder.o"
  done
done
echo "C library builds cleanly with $CC $*, with and without TINYDELTA_CODER"
