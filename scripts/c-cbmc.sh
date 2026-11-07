#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Model-checks the C decoders with CBMC (c/cbmc/): every input up to small
# bounds, exhaustively, not a sample. Within the bounds it proves:
#   - no out-of-bounds access and no undefined behaviour in either decoder;
#   - the callback decoder keeps its callback contract, gives the memory
#     decoder's result and target bytes, reports a failed callback as
#     TINYDELTA_ERR_IO and rejects a bad buffer setup;
#   - one step of the coder, from any state a corrupt patch can reach, stays
#     in its tables and keeps the range normalized, which carries over from
#     step to step (a whole coded patch is too much arithmetic for CBMC);
#   - one step of the relocation engine with each built-in table (the body
#     of tinydelta_reloc_span's loop), at any position of any image, with
#     any map and its instruction-length cache in any state: no access
#     outside the image, the span, the map or the table, and no undefined
#     behaviour, so none in any span. With MSP430X's sync, the step is
#     checked with length_at giving any value, and length_at on its own;
#   - the loop bounds were enough (--unwinding-assertions).
# Unsigned wraparound is not checked: the decoders rely on it on purpose.
#
# Usage: scripts/c-cbmc.sh   (needs cbmc, goto-cc and goto-instrument, e.g.
# apt install cbmc)
# JOBS sets how many runs go in parallel (default: the number of CPUs);
# ONLY=text runs only the checks whose description contains text.
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
# The relocation step (cbmc/reloc_position.c), each loop bounded by the
# tables: bit loops by the widest slice (16 bits); the generated matcher
# is asked again only after a require rule turns its match down (at most
# one such rule per list here: two passes).
RELOC="-DTINYDELTA_CODER=1 --unwind 2 --unwindset main.0:17,main.1:13,main.2:9,main.3:17,main.4:9,records_before.0:2,load_le32.0:5,first_rule.0:3,rule_addr.0:5,rule_addr.1:3,join.0:7,join.1:5,split.0:17,split.1:7,split.2:3,starts.0:17,starts.1:17,starts.2:17,excluded.0:5,excluded.1:3,move_at.0:3 cbmc/reloc_position.c"
runs="$(cat <<EOF
memory decoder (dictionary <= 4, patch <= 8, output <= 8)|$MEMORY
coder, one byte from any state|-DTINYDELTA_CODER=1 --unwind 10 --unwindset main.0:1290,coder_bit.0:4 cbmc/coder.c tinydelta_coder.c
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
relocation, Thumb built in: one step, image <= 16|$RELOC -DTINYDELTA_RELOC=1 -DIMAGE_MAX=16 -DDATA_RUNS=0
relocation, MSP430X built in: length_at anywhere, any data run|$RELOC -DTINYDELTA_RELOC=2 -DIMAGE_MAX=16 -DDATA_RUNS=2 -DLENGTH_AT=1
relocation, MSP430X built in: one code step, image <= 16, any length_at|$RELOC -DTINYDELTA_RELOC=2 -DIMAGE_MAX=16 -DDATA_RUNS=0 REMOVE=length_at
EOF
)"
# The MSP430X data rules look back for their unless: with p symbolic, CBMC
# unrolls both rule lists there; one run per position keeps it concrete.
for p in $(seq 0 15); do
  runs+=$'\n'"relocation, MSP430X built in: one data step at $p, image <= 16, any length_at|$RELOC -DTINYDELTA_RELOC=2 -DIMAGE_MAX=16 -DDATA_RUNS=1 -DPOSITION=$p REMOVE=length_at"
done

# ONLY=text runs just the checks whose description contains it.
if [ -n "${ONLY:-}" ]; then
  runs="$(echo "$runs" | grep -F -- "$ONLY")"
fi

logs="$(mktemp -d)"
check() { # "description|arguments"
  local name="${1%%|*}" args="${1#*|}" log start=$SECONDS
  log="$logs/$(echo "$name" | tr -c 'a-zA-Z0-9' '_').log"
  local ok=0
  if [[ "$args" == *REMOVE=* ]]; then
    # A function replaced by any value: compile, drop its body, check.
    local fn="${args##*REMOVE=}" defs=() rest=() a
    args="${args% REMOVE=*}"
    for a in $args; do # defines to goto-cc, the rest to cbmc
      case "$a" in -D*) defs+=("$a") ;; *.c) defs+=("$a") ;; *) rest+=("$a") ;; esac
    done
    # shellcheck disable=SC2086 # CHECKS is a word list
    goto-cc -I. "${defs[@]}" -o "$log.gb" >"$log" 2>&1 &&
      goto-instrument --remove-function-body "$fn" "$log.gb" "$log.2.gb" >>"$log" 2>&1 &&
      cbmc $CHECKS "${rest[@]}" "$log.2.gb" >>"$log" 2>&1 && ok=1
  else
    # shellcheck disable=SC2086 # the arguments are word lists
    cbmc -I. $CHECKS $args >"$log" 2>&1 && ok=1
  fi
  if [ "$ok" = 1 ]; then
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
