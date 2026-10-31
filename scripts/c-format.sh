#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Checks that the C sources match c/.clang-format; with --fix, reformats
# them in place instead. Usage: scripts/c-format.sh [--fix]
# Needs clang-format 18 or later (InsertBraces). Set CLANG_FORMAT to pick a
# version, e.g. CLANG_FORMAT=clang-format-18.
set -euo pipefail
cd "$(dirname "$0")/../c"
CLANG_FORMAT="${CLANG_FORMAT:-clang-format}"
FILES=(*.c *.h fuzz/*.c)

if [ "${1:-}" = "--fix" ]; then
  "$CLANG_FORMAT" -i "${FILES[@]}"
  echo "clang-format: reformatted"
else
  "$CLANG_FORMAT" --dry-run --Werror "${FILES[@]}"
  echo "clang-format: no changes needed"
fi
