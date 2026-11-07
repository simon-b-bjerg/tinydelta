#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

# Code size and worst-case stack of each C decoder configuration for
# MSP430 (msp430-elf-gcc), as a Markdown table (scripts/c-footprint.py).
set -euo pipefail
exec python3 "$(dirname "$0")/c-footprint.py" msp430
