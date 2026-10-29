#!/usr/bin/env bash
# Fuzzes the C decoders with libFuzzer, AddressSanitizer and
# UndefinedBehaviorSanitizer (see c/fuzz/fuzz_decode.c), starting from
# patches made by the Go encoders.
#
# Usage: scripts/c-fuzz.sh [seconds]     (default 60)
# Needs clang with libFuzzer (on Debian/Ubuntu: clang and libclang-rt-<N>-dev)
# and Go. Set CLANG to pick a compiler, e.g. CLANG=clang-18.
#
# Inputs that crash are written to fuzz-artifacts/ and the script fails;
# replay one with: <build dir>/fuzz_decode fuzz-artifacts/crash-...
set -euo pipefail
cd "$(dirname "$0")/.."
seconds="${1:-60}"
CLANG="${CLANG:-clang}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
flags=(-std=c99 -g -O1 -fsanitize=fuzzer,address,undefined -fno-sanitize-recover=all -Ic)

# The callback decoder at three scratch-buffer sizes, renamed apart.
for size in 1 7 64; do
  "$CLANG" "${flags[@]}" -c c/tinydelta_io.c -DTINYDELTA_SCRATCH_SIZE="$size" \
    -Dtinydelta_decode_io="fuzz_decode_io_scratch$size" -o "$work/io_$size.o"
done
"$CLANG" "${flags[@]}" c/fuzz/fuzz_decode.c c/tinydelta_decode.c c/tinydelta_cache.c \
  "$work"/io_*.o -o "$work/fuzz_decode"

TINYDELTA_C_FUZZ_SEEDS="$work/seeds" go test -count=1 -run '^TestWriteCFuzzSeeds$' . >/dev/null

mkdir -p "$work/corpus" fuzz-artifacts
echo "Fuzzing the C decoders for ${seconds}s"
# Progress lines (NEW/REDUCE/pulse) are dropped to keep the log short; the
# fuzzer's own exit status decides the result.
{
  status=0
  "$work/fuzz_decode" "$work/corpus" "$work/seeds" \
    -max_total_time="$seconds" -max_len=1200 -print_final_stats=1 \
    -artifact_prefix=fuzz-artifacts/ 2>&1 || status=$?
  echo "$status" >"$work/status"
} | grep -vE '^#[0-9]+[[:space:]]+(NEW|REDUCE|pulse)' || true
if [ "$(cat "$work/status")" != 0 ]; then
  echo "Fuzzing failed; crashing inputs are in fuzz-artifacts/" >&2
  exit 1
fi
rmdir fuzz-artifacts
echo "No failures"
