#!/usr/bin/env bash
# Build the release bundle used for gateway rollouts and host updates.
# Produces /tmp/rdev-release-<commit>/{rdev-gateway,rdev,rdev-host.exe,SHA256SUMS}
# Usage: scripts/build-release.sh
set -euo pipefail

cd "$(dirname "$0")/.."
FULL_SHA=$(git rev-parse HEAD)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
OUT="/tmp/rdev-release-${FULL_SHA:0:7}"
rm -rf "$OUT"
mkdir -p "$OUT"

build() { # build <target> <package> <output> <goos>
  local goos=$4
  local ldflags="-s -w -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.Version=0.0.1-dev -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.Commit=${FULL_SHA} -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.BuildTime=${BUILD_TIME}"
  GOOS="$goos" GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$OUT/$3" "./$2"
}

build gateway cmd/rdev-gateway rdev-gateway linux
build rdev     cmd/rdev        rdev         linux
build host     cmd/rdev-host   rdev-host.exe windows

# Hard gate: the Windows artifact must be a PE image, not an ELF. A silently
# wrong GOOS produced an ELF named rdev-host.exe once; on Windows that fails
# at service start and the host update rolls back forever.
if ! file "$OUT/rdev-host.exe" | grep -q "PE32+"; then
  echo "FATAL: rdev-host.exe is not a PE32+ image" >&2
  exit 1
fi

(cd "$OUT" && sha256sum rdev-gateway rdev rdev-host.exe > SHA256SUMS && cat SHA256SUMS)
"$OUT/rdev" version
echo "RELEASE_READY $OUT"
