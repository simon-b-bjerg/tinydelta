#!/usr/bin/env bash
# Measures the patch decoders that run on a microcontroller the same way
# scripts/c-size.sh measures tinydelta's: compiled for Cortex-M0 with
# arm-none-eabi-gcc -Os -mthumb, code size as .text, stack per function
# from -fstack-usage. Prints a Markdown report, the numbers behind
# bench/decoders.json.
# Usage: bench/decoder-size.sh <HPatchLite source dir>
set -euo pipefail
cd "$(dirname "$0")/.."
hpl="${1:?usage: $0 <HPatchLite source dir>}"
CC=arm-none-eabi-gcc
FLAGS=(-std=c99 -Os -mcpu=cortex-m0 -mthumb -ffunction-sections -fstack-usage -w)
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

text() { arm-none-eabi-size "$@" | awk 'NR > 1 { t += $1 } END { print t }'; }

measure() { # name files...
  local name="$1"
  shift
  local objs=() dirs=()
  for f in "$@"; do dirs+=(-I "$(dirname "$f")"); done
  for f in "$@"; do
    local o="$out/$name-$(basename "$f" .c).o"
    (cd "$out" && "$CC" "${FLAGS[@]}" "${dirs[@]}" -c "$OLDPWD/$f" -o "$o" 2>"$o.err") ||
      (cd "$out" && "$CC" "${FLAGS[@]}" "${dirs[@]}" -c "$f" -o "$o" 2>"$o.err") ||
      { echo "| $name | build failed: $(head -c 300 "$o.err" | tr '\n' ' ') |"; return; }
    objs+=("$o")
  done
  echo "| $name | $(text "${objs[@]}") B | $(cat "$out"/"$name"-*.su | sort -t$'\t' -k2 -n -r | head -3 |
    awk -F'\t' '{ n = split($1, f, ":"); printf "%s %d B; ", f[n], $2 }') |"
}

find_one() { find "$1" -name "$2" -not -path "*/test*" | head -1; }
hpatch_lite="$(find_one "$hpl" hpatch_lite.c)"
tuz_dec="$(find_one "$hpl" tuz_dec.c)"

echo "| decoder | code (.text, Cortex-M0, -Os) | largest stack frames |"
echo "|---|---:|---|"
measure tinydelta-callback c/tinydelta_io.c
measure tinydelta-cache c/tinydelta_cache.c
if [ -n "$hpatch_lite" ] && [ -n "$tuz_dec" ]; then
  measure hpatchlite "$hpatch_lite" "$tuz_dec"
  measure hpatchlite-patch-only "$hpatch_lite"
else
  echo "| hpatchlite | sources not found under $hpl |"
fi
echo
echo "Compiler: $($CC --version | head -1). HPatchLite: ${hpatch_lite#"$hpl"/}, ${tuz_dec#"$hpl"/}."
