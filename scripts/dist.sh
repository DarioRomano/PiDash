#!/bin/bash
# Builds the release packages into dist/:
#   pidash_linux_arm64.tar.gz   Raspberry Pi 3/4/5 with a 64-bit kernel (the usual case)
#   pidash_linux_armv6.tar.gz   32-bit Raspberry Pi OS
#   pidash_linux_amd64.tar.gz   ordinary PCs, for trying it out
#   SHA256SUMS
#
# Each package holds the program, the installer and the example settings, so a
# Pi needs nothing but the package. Asset names carry no version, which gives
# every release the same download address under .../releases/latest/download/.
#
# Usage: scripts/dist.sh [version]      (run from the repository root)
#
# Environment, for the tests that install from packages built here:
#   PIDASH_DIST_DIR     where to put them (default: dist)
#   PIDASH_DIST_ARCHES  which to build, of "arm64 armv6 amd64" (default: all)
set -euo pipefail

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ ! -f web/dist/index.html ]; then
  echo "web/dist is not built; run 'make build-web' first" >&2
  exit 1
fi

DIST="${PIDASH_DIST_DIR:-dist}"
ARCHES="${PIDASH_DIST_ARCHES:-arm64 armv6 amd64}"
rm -rf "${DIST:?}"
mkdir -p "$DIST/stage"

build() { # name GOARCH [GOARM]
  local name="$1" dir="$DIST/stage/pidash_linux_$1"
  mkdir -p "$dir"
  CGO_ENABLED=0 GOOS=linux GOARCH="$2" GOARM="${3:-}" \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "$dir/pidash" ./cmd/pidash
  cp -r deploy config.example.toml install.sh README.md LICENSE "$dir/"
  echo "$VERSION" > "$dir/VERSION"
  # Fixed ownership and order keep the archive independent of who built it.
  tar -C "$DIST/stage" --owner=0 --group=0 --numeric-owner --sort=name -czf "$DIST/pidash_linux_$name.tar.gz" "pidash_linux_$name"
  echo "  $DIST/pidash_linux_$name.tar.gz"
}

echo "Packaging PiDash $VERSION"
for arch in $ARCHES; do
  case "$arch" in
    arm64) build arm64 arm64 ;;
    armv6) build armv6 arm 6 ;;
    amd64) build amd64 amd64 ;;
    *) echo "no package is built for '$arch'" >&2; exit 1 ;;
  esac
done

rm -rf "$DIST/stage"
(cd "$DIST" && sha256sum pidash_linux_*.tar.gz > SHA256SUMS)
echo "  $DIST/SHA256SUMS"
