#!/usr/bin/env bash
# Prints the C library's code size (.text) and worst-case stack per part
# for MSP430, as a Markdown table: the original 16-bit MSP430 CPU, and
# MSP430X (as in the MSP430F5xx/FRxx families) in the small and large
# memory models. Needs msp430-elf-gcc (scripts/msp430-toolchain.sh).
set -euo pipefail
cd "$(dirname "$0")/../c"
CC=msp430-elf-gcc
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

text() { msp430-elf-size "$1" | awk 'NR == 2 { print $1 }'; }

# name:compiler flags
TARGETS=("msp430:-mcpu=msp430" "msp430x:-mcpu=msp430x" "msp430x-large:-mcpu=msp430x -mlarge")

echo "| part | MSP430 | MSP430X | MSP430X, -mlarge |"
echo "|---|---|---|---|"
for part in decode:tinydelta_decode io:tinydelta_io cache:tinydelta_cache; do
  name="${part%%:*}" file="${part#*:}"
  row="| $file.c |"
  for target in "${TARGETS[@]}"; do
    cpu="${target%%:*}"
    read -ra flags <<< "${target#*:}"
    (cd "$out" && "$CC" -std=c99 -Os "${flags[@]}" -fstack-usage \
      -Wall -Wextra -Werror -I"$OLDPWD" -c "$OLDPWD/$file.c" -o "${name}_$cpu.o")
    row="$row $(text "$out/${name}_$cpu.o") B |"
  done
  echo "$row"
done
echo
echo "Compiler: $($CC --version | head -1); flags: -Os; .text only (memcpy from libc not included)."
echo
echo "Stack, MSP430X -mlarge, per function (-fstack-usage):"
echo
echo '```'
cat "$out"/*_msp430x-large.su | awk -F'\t' '{ n = split($1, f, ":"); printf "%-28s %4d B\n", f[n], $2 }'
echo '```'
