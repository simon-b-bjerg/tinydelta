#!/usr/bin/env bash
# Static analysis of the C library with three analyzers; any finding fails:
#   - GCC's static analyzer (-fanalyzer)
#   - the Clang Static Analyzer (clang --analyze)
#   - clang-tidy, with the checks in c/.clang-tidy
# Usage: scripts/c-analyze.sh
# Needs gcc 10 or later, clang and clang-tidy. Set CLANG and CLANG_TIDY to
# pick versions, e.g. CLANG=clang-18 CLANG_TIDY=clang-tidy-18.
set -euo pipefail
cd "$(dirname "$0")/../c"
CLANG="${CLANG:-clang}"
CLANG_TIDY="${CLANG_TIDY:-clang-tidy}"
FILES=(tinydelta_decode.c tinydelta_io.c tinydelta_cache.c)
log="$(mktemp)"
trap 'rm -f "$log"' EXIT
FLAGS=(-std=c99 -I.)

  for f in "${FILES[@]}"; do
    gcc "${FLAGS[@]}" -O2 -fanalyzer -Wall -Wextra -Werror -c "$f" -o /dev/null
  done
  echo "gcc -fanalyzer: no findings"

  for f in "${FILES[@]}"; do
    "$CLANG" "${FLAGS[@]}" --analyze -Xanalyzer -analyzer-werror \
      -Xanalyzer -analyzer-output=text -o /dev/null "$f"
  done
  echo "clang --analyze: no findings"

  # Findings go to stdout; stderr only counts suppressed system-header
  # warnings, so it is shown only if clang-tidy fails.
  if ! "$CLANG_TIDY" --quiet "${FILES[@]}" -- "${FLAGS[@]}" 2>"$log"; then
    cat "$log" >&2
    exit 1
  fi
  echo "clang-tidy: no findings"
