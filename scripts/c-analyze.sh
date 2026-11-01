#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Static analysis of the C library with four analyzers; any finding fails:
#   - GCC's static analyzer (-fanalyzer)
#   - the Clang Static Analyzer (clang --analyze)
#   - clang-tidy, with the checks in c/.clang-tidy
#   - cppcheck's MISRA C:2012 addon, with the deviations listed below
# Usage: scripts/c-analyze.sh
# Needs gcc 10 or later, clang, clang-tidy and cppcheck 2.13 or later. Set
# CLANG and CLANG_TIDY to pick versions, e.g. CLANG=clang-18
# CLANG_TIDY=clang-tidy-18.
set -euo pipefail
cd "$(dirname "$0")/../c"
CLANG="${CLANG:-clang}"
CLANG_TIDY="${CLANG_TIDY:-clang-tidy}"
FILES=(tinydelta_decode.c tinydelta_io.c tinydelta_coder.c)
log="$(mktemp)"
work="$(mktemp -d)" # cppcheck's working files, so none land in c/
trap 'rm -rf "$log" "$work"' EXIT
FLAGS=(-std=c99 -I.)
CODER=(-DTINYDELTA_CODER=0 -DTINYDELTA_CODER=1) # every analyzer runs without and with coded patches

for coder in "${CODER[@]}"; do
  for f in "${FILES[@]}"; do
    gcc "${FLAGS[@]}" "$coder" -O2 -fanalyzer -Wall -Wextra -Werror -c "$f" -o /dev/null
  done
done
echo "gcc -fanalyzer: no findings"

for coder in "${CODER[@]}"; do
  for f in "${FILES[@]}"; do
    "$CLANG" "${FLAGS[@]}" "$coder" --analyze -Xanalyzer -analyzer-werror \
      -Xanalyzer -analyzer-output=text -o /dev/null "$f"
  done
done
echo "clang --analyze: no findings"

# Findings go to stdout; stderr only counts suppressed system-header
# warnings, so it is shown only if clang-tidy fails.
for coder in "${CODER[@]}"; do
  if ! "$CLANG_TIDY" --quiet "${FILES[@]}" -- "${FLAGS[@]}" "$coder" 2>"$log"; then
    cat "$log" >&2
    exit 1
  fi
done
echo "clang-tidy: no findings"

# MISRA C:2012 deviations (README.md, "C coding rules"):
MISRA_DEVIATIONS=(
  # 15.5 (advisory) single point of exit: an early return per failed check
  --suppress=misra-c2012-15.5
  # 17.8 (advisory) parameters reused as offset and length counters
  --suppress=misra-c2012-17.8
  # 18.4 (advisory) buffer + offset pointer arithmetic
  --suppress=misra-c2012-18.4
  # 10.8 the masked two-bit opcode is cast to tinydelta_opcode, so
  # -Wswitch-enum proves every opcode has a case
  --suppress='misra-c2012-10.8:*tinydelta_internal.h'
  # 11.8 cppcheck false positive: a callback's void *ctx is copied out of
  # the const tinydelta_io; no qualifier is removed from the data
  --suppress='misra-c2012-11.8:*tinydelta_io.c'
  # 11.5 (advisory) the coder's byte source is given its decoder as void *,
  # like the callbacks' contexts
  --suppress='misra-c2012-11.5:*tinydelta_decode.c'
  --suppress='misra-c2012-11.5:*tinydelta_io.c'
)
for coder in "${CODER[@]}"; do
  unused=() # 2.5 (advisory) unused macros: without the coder, its macros go unused
  [ "$coder" = -DTINYDELTA_CODER=0 ] && unused=(--suppress=misra-c2012-2.5)
  cppcheck --addon=misra --enable=style --std=c99 -I. "$coder" --quiet --error-exitcode=1 --cppcheck-build-dir="$work" \
    "${MISRA_DEVIATIONS[@]}" "${unused[@]}" "${FILES[@]}"
done
echo "cppcheck MISRA C:2012: no findings beyond the listed deviations"
