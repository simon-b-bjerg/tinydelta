#!/usr/bin/env bash
# Fuzzes the C decoders with libFuzzer, AddressSanitizer and
# UndefinedBehaviorSanitizer (see c/fuzz/fuzz_decode.c), starting from
# patches made by the Go encoders.
#
# Usage: scripts/c-fuzz.sh [seconds [corpus dir]]   (default 60 s)
# Needs clang with libFuzzer (on Debian/Ubuntu: clang and libclang-rt-<N>-dev)
# and Go. Set CLANG to pick a compiler, e.g. CLANG=clang-18.
#
# The corpus (inputs that reached new code) is kept in the corpus dir if
# one is given, e.g. for scripts/c-coverage.sh. Inputs that crash are
# written to fuzz-artifacts/, hex dumped, and the script fails; replay one
# with: <build dir>/fuzz_decode fuzz-artifacts/crash-...
set -euo pipefail
cd "$(dirname "$0")/.."
seconds="${1:-60}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
corpus="${2:-$work/corpus}"
scripts/c-fuzz-build.sh "$work/fuzz_decode" -O1 -fsanitize=address,undefined -fno-sanitize-recover=all

TINYDELTA_C_FUZZ_SEEDS="$work/seeds" go test -count=1 -run '^TestWriteCFuzzSeeds$' . >/dev/null

mkdir -p "$corpus" fuzz-artifacts
echo "Fuzzing the C decoders for ${seconds}s"
# Progress lines (NEW/REDUCE/pulse) are dropped to keep the log short; the
# fuzzer's own exit status decides the result.
{
  status=0
  "$work/fuzz_decode" "$corpus" "$work/seeds" \
    -max_total_time="$seconds" -max_len=1200 -print_final_stats=1 \
    -artifact_prefix=fuzz-artifacts/ 2>&1 || status=$?
  echo "$status" >"$work/status"
} | grep -vE '^#[0-9]+[[:space:]]+(NEW|REDUCE|pulse)' || true
if [ "$(cat "$work/status")" = 0 ]; then
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
  if [ "$size" -ge 5 ]; then
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
