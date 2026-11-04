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

# Without and with coded patches (TINYDELTA_CODER), and with each
# relocation rule table (TINYDELTA_RELOC), the last one of one's own
# (TINYDELTA_RELOC_RULES) accepting shipped tables too.
for config in 0:0 1:0 1:1 1:2 1:3; do
  coder="${config%%:*}" reloc="${config##*:}"
  extra=()
  [ "$reloc" = 3 ] && extra=(-DTINYDELTA_RELOC_RULES_HEADER='"tinydelta_rules_msp430x.h"' -DTINYDELTA_RELOC_SHIPPED=1)
  for f in tinydelta_decode tinydelta_io tinydelta_coder tinydelta_reloc; do
    "$CC" "${FLAGS[@]}" -DTINYDELTA_CODER="$coder" -DTINYDELTA_RELOC="$reloc" "${extra[@]}" -c $f.c -o "$out/$f-$coder-$reloc.o"
  done
done
echo "C library builds cleanly with $CC $*, with and without TINYDELTA_CODER, TINYDELTA_RELOC and TINYDELTA_RELOC_SHIPPED"
