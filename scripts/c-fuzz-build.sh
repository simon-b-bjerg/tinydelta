#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Builds the C fuzz harness (c/fuzz/fuzz_decode.c) with libFuzzer, for
# scripts/c-fuzz.sh (with sanitizers) and scripts/c-coverage.sh (with
# coverage instrumentation). Extra arguments are passed to the compiler.
# Usage: scripts/c-fuzz-build.sh <output binary> [flags...]
# Set CLANG to pick a compiler, e.g. CLANG=clang-18.
set -euo pipefail
cd "$(dirname "$0")/.."
out="$1"
shift
CLANG="${CLANG:-clang}"
flags=(-std=c99 -g -fsanitize=fuzzer -Ic -DTINYDELTA_CODER=1 "$@")

"$CLANG" "${flags[@]}" c/fuzz/fuzz_decode.c c/tinydelta_decode.c c/tinydelta_io.c c/tinydelta_coder.c -o "$out"
