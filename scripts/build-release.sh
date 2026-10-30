#!/usr/bin/env bash
# Builds release archives of the tinydelta CLI into dist/:
#   tinydelta_<version>_linux_<arch>.tar.gz   (static binary, README, LICENSE)
#   tinydelta_<version>_windows_<arch>.zip    (tinydelta.exe, README, LICENSE)
#   tinydelta-c_<version>.tar.gz              (C decoder sources, conformance vectors)
#   checksums.txt                             (SHA-256)
# Usage: scripts/build-release.sh <version>, e.g. v1.0.0
set -euo pipefail
cd "$(dirname "$0")/.."
version="${1:?usage: $0 <version>}"
rm -rf dist build
mkdir -p dist

for arch in amd64 arm64; do
  stage="build/tinydelta_${version}_linux_${arch}"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=${version}" -o "$stage/tinydelta" ./cmd/tinydelta
  cp README.md LICENSE "$stage/"
  tar -czf "dist/tinydelta_${version}_linux_${arch}.tar.gz" -C build "$(basename "$stage")"
done

for arch in amd64 arm64; do
  stage="build/tinydelta_${version}_windows_${arch}"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=${version}" -o "$stage/tinydelta.exe" ./cmd/tinydelta
  cp README.md LICENSE "$stage/"
  (cd build && zip -qr "../dist/$(basename "$stage").zip" "$(basename "$stage")")
done

csrc="build/tinydelta-c_${version}"
mkdir -p "$csrc"
cp c/*.c c/*.h LICENSE testdata/vectors.txt "$csrc/"
tar -czf "dist/tinydelta-c_${version}.tar.gz" -C build "$(basename "$csrc")"

(cd dist && sha256sum -- * > checksums.txt)
rm -rf build
ls -l dist
