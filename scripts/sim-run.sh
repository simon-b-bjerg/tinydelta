#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Runs a test program under a bare-metal instruction simulator, for use as
# $TINYDELTA_C_RUNNER, e.g. "scripts/sim-run.sh msp430-elf-run".
#
# Bare-metal startup code calls main() without arguments, so the
# arguments are written to the file "args" (one per line) in a fresh
# working directory, where the test drivers read them back. File I/O
# and the exit status pass through the simulator to the host.
# Usage: scripts/sim-run.sh <simulator> <program> [args...]
set -euo pipefail

sim="$1" program="$2"
shift 2
dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT
printf '%s\n' "$@" > "$dir/args"
cd "$dir"
"$sim" "$program"
