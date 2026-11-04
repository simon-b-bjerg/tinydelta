#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Measures how much of the C library the fuzz harnesses reach: builds
# c/fuzz/fuzz_decode.c and c/fuzz/fuzz_reloc.c, in the three builds of
# scripts/c-fuzz-build.sh, with coverage instrumentation, replays the seeds
# from the Go encoders plus any corpus dir given and the ones kept next to
# it by scripts/c-fuzz.sh, and prints a Markdown report of line,
# branch and function coverage per file, with the lines never reached.
# The last line is "Total line coverage: <percent>%".
#
# Usage: scripts/c-coverage.sh [corpus dir...]
# Needs clang with libFuzzer, llvm-profdata, llvm-cov and Go. Set CLANG,
# LLVM_PROFDATA and LLVM_COV to pick versions, e.g. clang-18.
set -euo pipefail
cd "$(dirname "$0")/.."
LLVM_PROFDATA="${LLVM_PROFDATA:-llvm-profdata}"
LLVM_COV="${LLVM_COV:-llvm-cov}"
FILES=(c/tinydelta_decode.c c/tinydelta_io.c c/tinydelta_coder.c c/tinydelta_reloc.c)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
builds=(fuzz_decode fuzz_decode_device fuzz_decode_thumb fuzz_reloc fuzz_reloc_device fuzz_reloc_thumb)
for build in "${builds[@]}"; do
  scripts/c-fuzz-build.sh "$build" "$work/$build" -O0 -fprofile-instr-generate -fcoverage-mapping
done
TINYDELTA_C_FUZZ_SEEDS="$work/seeds" TINYDELTA_C_FUZZ_RELOC_SEEDS="$work/reloc-seeds" \
  go test -count=1 -run '^TestWriteCFuzz' . >/dev/null

# -runs=0: run every input once, no fuzzing. Each build replays the seeds
# and every corpus kept for that harness (all three builds' corpora).
decode_dirs=() reloc_dirs=()
for dir in "$@"; do
  for s in "" -device -thumb; do
    [ -d "$dir$s" ] && decode_dirs+=("$dir$s")
    [ -d "$dir-reloc$s" ] && reloc_dirs+=("$dir-reloc$s")
  done
done
profiles=() cov=()
for build in "${builds[@]}"; do
  case "$build" in
    fuzz_decode*) replay=("$work/seeds" "${decode_dirs[@]}") ;;
    *) replay=("$work/reloc-seeds" "${reloc_dirs[@]}") ;;
  esac
  LLVM_PROFILE_FILE="$work/$build.profraw" "$work/$build" -runs=0 "${replay[@]}" \
    > "$work/replay.log" 2>&1 || { cat "$work/replay.log"; exit 1; }
  profiles+=("$work/$build.profraw")
  if [ ${#cov[@]} -eq 0 ]; then cov=("$work/$build"); else cov+=(-object "$work/$build"); fi
done
"$LLVM_PROFDATA" merge -sparse "${profiles[@]}" -o "$work/run.profdata"
cov+=(-instr-profile="$work/run.profdata")

inputs=$(( $(ls "$work/seeds" | wc -l) + $(ls "$work/reloc-seeds" | wc -l) ))
for dir in "${decode_dirs[@]}" "${reloc_dirs[@]}"; do inputs=$(( inputs + $(ls "$dir" | wc -l) )); done
echo "C library coverage from the fuzz harnesses ($inputs inputs)."
echo
echo "| file | lines | branches | functions |"
echo "|---|---:|---:|---:|"
"$LLVM_COV" report "${cov[@]}" "${FILES[@]}" |
  awk '$1 ~ /\.c$|^TOTAL$/ {
    # columns: regions(3) functions(3) lines(3) branches(3)
    name = $1; sub(/.*\//, "", name)
    if (name == "TOTAL") name = "**total**"
    printf "| %s | %s | %s | %s |\n", name, $10, $13, $7
  }'
echo
uncovered="$("$LLVM_COV" show "${cov[@]}" "${FILES[@]}" -show-line-counts \
  -show-instantiations=false -use-color=false |
  awk '/^\/.*\.c:$/ { file = $0; sub(/.*\//, "", file); sub(/:$/, "", file) }
       /^ *[0-9]+\| *0\|/ { split($0, f, "|"); line = f[1]; gsub(/ /, "", line)
                            src = f[3]; sub(/^ */, "", src)
                            printf "%s:%s  %s\n", file, line, src }')"
if [ -n "$uncovered" ]; then
  echo "Lines never reached:"
  echo
  echo '```'
  echo "$uncovered"
  echo '```'
else
  echo "Every line is reached."
fi
echo
total="$("$LLVM_COV" report "${cov[@]}" "${FILES[@]}" | awk '$1 == "TOTAL" { print $10 }')"
echo "Total line coverage: $total"
