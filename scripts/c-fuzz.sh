#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Fuzzes the C library with libFuzzer, AddressSanitizer and
# UndefinedBehaviorSanitizer, starting from inputs made by the Go encoders:
# the decoders (c/fuzz/fuzz_decode.c), then the relocation engine on its own
# (c/fuzz/fuzz_reloc.c), each for the given time.
#
# Usage: scripts/c-fuzz.sh [seconds [corpus dir]]   (default 60 s each)
# Needs clang with libFuzzer (on Debian/Ubuntu: clang and libclang-rt-<N>-dev)
# and Go. Set CLANG to pick a compiler, e.g. CLANG=clang-18.
#
# The corpora (inputs that reached new code) are kept in the corpus dir and
# in <corpus dir>-reloc if one is given, e.g. for scripts/c-coverage.sh.
# Inputs that crash are written to fuzz-artifacts/ (the relocation
# engine's as reloc-...), hex dumped, and the script fails; replay one with:
# <build dir>/fuzz_decode fuzz-artifacts/crash-... (or fuzz_reloc).
set -euo pipefail
cd "$(dirname "$0")/.."
seconds="${1:-60}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
corpus="${2:-$work/corpus}"
for harness in fuzz_decode fuzz_reloc; do
  scripts/c-fuzz-build.sh "$harness" "$work/$harness" -O1 -fsanitize=address,undefined -fno-sanitize-recover=all
done

TINYDELTA_C_FUZZ_SEEDS="$work/seeds" TINYDELTA_C_FUZZ_RELOC_SEEDS="$work/reloc-seeds" \
  go test -count=1 -run '^TestWriteCFuzz' . >/dev/null

mkdir -p "$corpus" "$corpus-reloc" fuzz-artifacts
status=0
# harness, corpus, seeds, longest input, artifact prefix
for run in "fuzz_decode $corpus $work/seeds 1200 fuzz-artifacts/" "fuzz_reloc $corpus-reloc $work/reloc-seeds 2048 fuzz-artifacts/reloc-"; do
  read -r harness dir seeds max_len prefix <<<"$run"
  echo "Fuzzing $harness for ${seconds}s"
  # Progress lines (NEW/REDUCE/pulse) are dropped to keep the log short; the
  # fuzzer's own exit status decides the result.
  {
    s=0
    "$work/$harness" "$dir" "$seeds" \
      -max_total_time="$seconds" -max_len="$max_len" -print_final_stats=1 \
      -artifact_prefix="$prefix" 2>&1 || s=$?
    echo "$s" >"$work/status"
  } | grep -vE '^#[0-9]+[[:space:]]+(NEW|REDUCE|pulse)' || true
  [ "$(cat "$work/status")" = 0 ] || status=1
done
if [ "$status" = 0 ]; then
  rmdir fuzz-artifacts
  echo "No failures"
  exit 0
fi

# Hex dump each failing input, split into its fields (see the input layout
# in c/fuzz/fuzz_decode.c), and repeat it as a GitHub Actions annotation.
for f in fuzz-artifacts/*; do
  [ -f "$f" ] || continue
  hex="$(od -An -v -tx1 "$f" | tr -d ' \n')"
  size=$(( ${#hex} / 2 ))
  echo
  echo "Failing input $f ($size bytes):"
  if [ "$size" -ge 5 ] && [[ "$(basename "$f")" != reloc-* ]]; then
    cap=$(( 16#${hex:2:2} << 8 | 16#${hex:0:2} ))
    dict=$(( 16#${hex:4:2} ))
    [ "$dict" -gt $(( size - 5 )) ] && dict=$(( size - 5 ))
    echo "  header (raw): capacity $cap, dictionary $dict bytes, page byte $(( 16#${hex:6:2} )), fail at call $(( 16#${hex:8:2} ))"
    echo "  dictionary: ${hex:10:$(( dict * 2 ))}"
    echo "  patch:      ${hex:$(( 10 + dict * 2 ))}"
  fi
  od -A x -t x1z -v "$f"
  echo "::error title=C fuzzer failing input::$f ($size bytes): $hex"
done
echo "Fuzzing failed; crashing inputs are in fuzz-artifacts/" >&2
exit 1
