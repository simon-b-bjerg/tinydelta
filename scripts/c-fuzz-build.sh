#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Builds a C fuzz harness with libFuzzer, for scripts/c-fuzz.sh (with
# sanitizers) and scripts/c-coverage.sh (with coverage instrumentation):
# c/fuzz/<harness>.c (fuzz_decode: the decoders; fuzz_reloc: the
# relocation engine), with the coder and relocation (the MSP430X table
# built in, and shipped tables accepted). Extra arguments are passed to
# the compiler.
# Usage: scripts/c-fuzz-build.sh <harness> <output binary> [flags...]
# Set CLANG to pick a compiler, e.g. CLANG=clang-18.
set -euo pipefail
cd "$(dirname "$0")/.."
harness="$1" out="$2"
shift 2
CLANG="${CLANG:-clang}"
flags=(-std=c99 -g -fsanitize=fuzzer -Ic -DTINYDELTA_CODER=1 -DTINYDELTA_RELOC=2 -DTINYDELTA_RELOC_SHIPPED=1 "$@")

"$CLANG" "${flags[@]}" "c/fuzz/$harness.c" c/tinydelta_decode.c c/tinydelta_io.c c/tinydelta_coder.c c/tinydelta_reloc.c -o "$out"
