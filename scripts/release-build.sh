#!/usr/bin/env bash
# Cross-compile gotin for every release target and package the archives.
#
# Usage: scripts/release-build.sh <tag> [out-dir]
#   <tag>     release tag, e.g. v1.1.0; must match internal/version.Version
#   out-dir   defaults to dist/
#
# Produces gotin_<version>_<os>_<arch>.tar.gz (.zip for windows) and SHA256SUMS.txt.
set -euo pipefail

TARGETS=(
  linux/amd64
  linux/arm64
  darwin/amd64
  darwin/arm64
  windows/amd64
  windows/arm64
)

tag="${1:?usage: $0 <tag> [out-dir]}"
out="${2:-dist}"
version="${tag#v}"

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

src_version="$(sed -n 's/^const Version = "\(.*\)"$/\1/p' internal/version/version.go)"
if [[ "$version" != "$src_version" ]]; then
  echo "error: tag $tag does not match internal/version.Version ($src_version)" >&2
  exit 1
fi

rm -rf "$out"
mkdir -p "$out"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

for target in "${TARGETS[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  name="gotin_${version}_${goos}_${goarch}"
  bin="gotin"
  [[ "$goos" == windows ]] && bin="gotin.exe"

  stage="$work/$name"
  mkdir -p "$stage"
  echo "building $target"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$stage/$bin" ./cmd/gotin
  cp README.md config.json "$stage/"

  if [[ "$goos" == windows ]]; then
    (cd "$work" && zip -qr "$root/$out/$name.zip" "$name")
  else
    tar -C "$work" -czf "$out/$name.tar.gz" "$name"
  fi
done

(
  cd "$out"
  if command -v sha256sum >/dev/null; then
    sha256sum gotin_* > SHA256SUMS.txt
  else
    shasum -a 256 gotin_* > SHA256SUMS.txt
  fi
)

echo "artifacts in $out:"
ls -1 "$out"
