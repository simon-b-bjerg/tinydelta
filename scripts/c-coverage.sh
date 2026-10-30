#!/usr/bin/env bash
# Measures how much of the C library the fuzz harness reaches: builds
# c/fuzz/fuzz_decode.c with coverage instrumentation, replays the seed
# patches from the Go encoders plus any corpus dirs given (e.g. the one
# kept by scripts/c-fuzz.sh), and prints a Markdown report of line,
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
FILES=(c/tinydelta_decode.c c/tinydelta_io.c c/tinydelta_cache.c)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
scripts/c-fuzz-build.sh "$work/fuzz_cov" -O0 -fprofile-instr-generate -fcoverage-mapping
TINYDELTA_C_FUZZ_SEEDS="$work/seeds" go test -count=1 -run '^TestWriteCFuzzSeeds$' . >/dev/null

# -runs=0: run every input once, no fuzzing.
LLVM_PROFILE_FILE="$work/run.profraw" "$work/fuzz_cov" -runs=0 "$work/seeds" "$@" \
  > "$work/replay.log" 2>&1 || { cat "$work/replay.log"; exit 1; }
"$LLVM_PROFDATA" merge -sparse "$work/run.profraw" -o "$work/run.profdata"
cov=("$work/fuzz_cov" -instr-profile="$work/run.profdata")

inputs=$(( $(ls "$work/seeds" | wc -l) ))
for dir in "$@"; do inputs=$(( inputs + $(ls "$dir" | wc -l) )); done
echo "C library coverage from the fuzz harness ($inputs inputs)."
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
