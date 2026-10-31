#!/usr/bin/env bash
# Prints the C library's code size (.text) and worst-case stack per part
# for Cortex-M, as a Markdown table. Needs arm-none-eabi-gcc.
set -euo pipefail
cd "$(dirname "$0")/../c"
CC=arm-none-eabi-gcc
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

text() { arm-none-eabi-size "$1" | awk 'NR == 2 { print $1 }'; }

echo "| part | Cortex-M0 | Cortex-M3 | Cortex-M4 |"
echo "|---|---|---|---|"
# name:file:extra flags; the v2 rows have format version 2 compiled in.
for part in decode:tinydelta_decode: io:tinydelta_io: cache:tinydelta_cache: \
    decodev2:tinydelta_decode:-DTINYDELTA_FORMAT_V2=1 iov2:tinydelta_io:-DTINYDELTA_FORMAT_V2=1; do
  IFS=: read -r name file extra <<< "$part"
  row="| $file.c${extra:+, with format version 2} |"
  for cpu in cortex-m0 cortex-m3 cortex-m4; do
    (cd "$out" && "$CC" -std=c99 -Os -mcpu="$cpu" -mthumb -fstack-usage \
      -Wall -Wextra -Werror ${extra:+"$extra"} -I"$OLDPWD" -c "$OLDPWD/$file.c" -o "${name}_$cpu.o")
    row="$row $(text "$out/${name}_$cpu.o") B |"
  done
  echo "$row"
done
echo
echo "Compiler: $($CC --version | head -1); flags: -Os -mthumb; .text only (memcpy from libc not included)."
echo
echo "Stack, Cortex-M0, per function (-fstack-usage), version 1 builds, then the callback decoder with version 2:"
echo
echo '```'
cat "$out"/{decode,io,cache}_cortex-m0.su "$out"/iov2_cortex-m0.su | awk -F'\t' '{ split($1, f, ":"); printf "%-28s %4d B\n", f[4], $2 }'
echo '```'
