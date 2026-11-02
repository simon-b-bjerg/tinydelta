#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Model-checks the C decoders with CBMC (c/cbmc/): every input up to small
# bounds, exhaustively, not a sample. Within the bounds it proves:
#   - no out-of-bounds access and no undefined behaviour in either decoder;
#   - the callback decoder keeps its callback contract, gives the memory
#     decoder's result and target bytes, reports a failed callback as
#     TINYDELTA_ERR_IO and rejects a bad buffer setup;
#   - the loop bounds were enough (--unwinding-assertions).
# Unsigned wraparound is not checked: the decoders rely on it on purpose.
#
# Usage: scripts/c-cbmc.sh   (needs cbmc, e.g. apt install cbmc)
# JOBS sets how many runs go in parallel (default: the number of CPUs).
set -euo pipefail
cd "$(dirname "$0")/../c"
JOBS="${JOBS:-$(nproc)}"

CHECKS="--bounds-check --pointer-check --pointer-overflow-check --signed-overflow-check
        --undefined-shift-check --div-by-zero-check --unwinding-assertions --slice-formula --object-bits 12"

# One run per line: a description, then CBMC's arguments. Bounds are the
# largest dictionary, patch and output in bytes. A patch of 8 bytes holds
# a version byte, an instruction and a 5-byte delta. The callback decoder
# is checked one buffer setup at a time (sizes in bytes; read 0: no
# target_read); with outputs up to 4 bytes, a 1- or 2-byte buffer wraps
# around and far copies go through target_read.
MEMORY="-DDICT_MAX=4 -DPATCH_MAX=8 -DOUT_MAX=8 --unwind 10 cbmc/memory.c tinydelta_decode.c"
IO="-DDICT_MAX=2 -DPATCH_MAX=4 -DOUT_MAX=4 --unwind 6 cbmc/io.c tinydelta_io.c tinydelta_decode.c"
runs="$(cat <<EOF
memory decoder (dictionary <= 4, patch <= 8, output <= 8)|$MEMORY
callback = memory decoder, buffer 1, page 1|$IO -DBUFFER_SIZE=1 -DPAGE_SIZE=1 -DWITH_READ=1
callback = memory decoder, buffer 2, page 1|$IO -DBUFFER_SIZE=2 -DPAGE_SIZE=1 -DWITH_READ=1
callback = memory decoder, buffer 4, page 2|$IO -DBUFFER_SIZE=4 -DPAGE_SIZE=2 -DWITH_READ=1
callback = memory decoder, buffer 1, page 1, no target_read|$IO -DBUFFER_SIZE=1 -DPAGE_SIZE=1 -DWITH_READ=0
callback = memory decoder, buffer 2, page 2, no target_read|$IO -DBUFFER_SIZE=2 -DPAGE_SIZE=2 -DWITH_READ=0
callback = memory decoder, buffer 4, page 4, no target_read|$IO -DBUFFER_SIZE=4 -DPAGE_SIZE=4 -DWITH_READ=0
callback, any one call failing, buffer 1, page 1|$IO -DBUFFER_SIZE=1 -DPAGE_SIZE=1 -DWITH_READ=1 -DINJECT=1 -DEQUIV=0
callback, any one call failing, buffer 2, page 1|$IO -DBUFFER_SIZE=2 -DPAGE_SIZE=1 -DWITH_READ=1 -DINJECT=1 -DEQUIV=0
callback, bad setup: buffer 3|$IO -DBUFFER_SIZE=3 -DPAGE_SIZE=1 -DEQUIV=0
callback, bad setup: page 3|$IO -DBUFFER_SIZE=4 -DPAGE_SIZE=3 -DEQUIV=0
callback, bad setup: page larger than the buffer|$IO -DBUFFER_SIZE=2 -DPAGE_SIZE=4 -DEQUIV=0
callback, bad setup: buffer 0|$IO -DBUFFER_SIZE=0 -DPAGE_SIZE=1 -DEQUIV=0
EOF
)"

logs="$(mktemp -d)"
check() { # "description|arguments"
  local name="${1%%|*}" args="${1#*|}" log start=$SECONDS
  log="$logs/$(echo "$name" | tr -c 'a-zA-Z0-9' '_').log"
  # shellcheck disable=SC2086 # the arguments are word lists
  if cbmc -I. $CHECKS $args >"$log" 2>&1; then
    echo "verified: $name ($(( SECONDS - start )) s)"
  else
    echo "FAILED:   $name"
    grep -E "FAILURE|VERIFICATION|too many|error" "$log" | head -20 | sed 's/^/    /'
    return 1
  fi
}
export -f check
export CHECKS logs

if echo "$runs" | xargs -d '\n' -P "$JOBS" -I{} bash -c 'check "$@"' _ {}; then
  rm -rf "$logs"
  echo "All CBMC checks verified."
else
  echo "CBMC found a failure; full logs in $logs" >&2
  exit 1
fi
