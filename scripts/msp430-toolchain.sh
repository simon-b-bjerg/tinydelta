#!/usr/bin/env bash
# Builds a GNU MSP430 toolchain from source into <prefix> (default
# ~/.cache/msp430): binutils, GCC (C only) with newlib, and GNU's MSP430
# instruction simulator (msp430-elf-run, from GDB's sim/). Takes a while;
# CI caches the result. Afterwards, put <prefix>/bin on PATH.
# Needs a host compiler, make, and the GMP, MPFR and MPC development
# packages (Debian/Ubuntu: libgmp-dev libmpfr-dev libmpc-dev).
# Failures are also printed as GitHub Actions error annotations.
# Usage: scripts/msp430-toolchain.sh [prefix]
set -euo pipefail

PREFIX="${1:-$HOME/.cache/msp430}"
TARGET=msp430-elf
BINUTILS_URL=https://ftp.gnu.org/gnu/binutils/binutils-2.43.1.tar.xz
GCC_URL=https://ftp.gnu.org/gnu/gcc/gcc-14.2.0/gcc-14.2.0.tar.xz
NEWLIB_URL=https://sourceware.org/pub/newlib/newlib-4.4.0.20231231.tar.gz
GDB_URL=https://ftp.gnu.org/gnu/gdb/gdb-15.2.tar.xz
JOBS="$(nproc 2>/dev/null || echo 2)"
COMMON=(--target="$TARGET" --prefix="$PREFIX" --disable-nls --disable-werror)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export PATH="$PREFIX/bin:$PATH"

fail() { # message [log file]
  echo "::error::$1"
  if [ -n "${2:-}" ]; then
    echo "::error::$(tail -n 25 "$2" | sed 's/%/%25/g' | awk '{ printf "%s%%0A", $0 }')"
  fi
  exit 1
}

fetch() { # url; sets $src to the unpacked source directory
  local name file code
  name="$(basename "$1")"
  file="$work/$name"
  code="$(curl -sSL -o "$file" -w '%{http_code}' "$1" || true)"
  [ "$code" = 200 ] || fail "download of $1 failed (HTTP $code)"
  tar -xf "$file" -C "$work" || fail "unpacking $name failed"
  src="$work/${name%.tar.*}"
  [ -d "$src" ] || fail "$name did not unpack to ${name%.tar.*}"
}

build() { # name source-dir make-targets install-targets configure-args...
  local name="$1" src="$2" targets="$3" installs="$4"
  shift 4
  echo "Building $name"
  mkdir -p "$work/build-$name"
  cd "$work/build-$name"
  "$src/configure" "${COMMON[@]}" "$@" > log 2>&1 || fail "configuring $name failed" log
  make -j"$JOBS" $targets > log 2>&1 || fail "building $name failed" log
  make $installs > log 2>&1 || fail "installing $name failed" log
}

mkdir -p "$PREFIX"
fetch "$BINUTILS_URL"
build binutils "$src" all install \
  --disable-gdb --disable-gdbserver --disable-sim
fetch "$GCC_URL"
build gcc "$src" "all-gcc all-target-libgcc" "install-gcc install-target-libgcc" \
  --enable-languages=c --with-newlib --without-headers --disable-shared \
  --disable-threads --disable-libssp --disable-lto
fetch "$NEWLIB_URL"
build newlib "$src" all install
fetch "$GDB_URL"
build sim "$src" all-sim install-sim \
  --enable-sim --disable-gdb --disable-gdbserver --disable-binutils \
  --disable-ld --disable-gas --disable-gprof

"$TARGET-gcc" --version | head -1
command -v "$TARGET-run"
