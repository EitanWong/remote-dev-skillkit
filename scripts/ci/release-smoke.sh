#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/rdev-release-smoke.XXXXXX")"

cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

cd "$repo_root"

commit="$(git rev-parse HEAD)"
build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ldflags="-s -w -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.Commit=${commit} -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.BuildTime=${build_time} -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.SourceRoot=release"
artifact_dir="$work_dir/artifacts"
mkdir -p "$artifact_dir"

CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$artifact_dir/rdev" ./cmd/rdev
CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$artifact_dir/rdev-gateway" ./cmd/rdev-gateway
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$ldflags" -o "$artifact_dir/rdev-host.exe" ./cmd/rdev-host

file "$artifact_dir/rdev-gateway" | grep -F 'statically linked' >/dev/null
file "$artifact_dir/rdev-host.exe" | grep -E 'PE32\+.*x86-64' >/dev/null

(
  cd "$artifact_dir"
  sha256sum rdev rdev-gateway rdev-host.exe > SHA256SUMS
  sha256sum --check SHA256SUMS
)

"$artifact_dir/rdev" version --json | RDEV_SMOKE_COMMIT="$commit" python3 -c '
import json
import os
import sys

version = json.load(sys.stdin)
if version.get("commit") != os.environ["RDEV_SMOKE_COMMIT"]:
    raise SystemExit("rdev version commit does not match release source")
if version.get("source_root") != "release":
    raise SystemExit("rdev version source root is not release")
'

"$artifact_dir/rdev" mcp tools | python3 -c '
import json
import sys

tools = json.load(sys.stdin).get("tools", [])
handoff = next((tool for tool in tools if tool.get("name") == "rdev.sessions.handoff"), None)
if handoff is None:
    raise SystemExit("missing rdev.sessions.handoff MCP tool")
if "Windows" not in handoff.get("description", ""):
    raise SystemExit("handoff MCP tool does not describe Windows delivery")
'

go test ./internal/gateway ./internal/httpapi ./internal/cli \
  -run '^(TestWebHandoff.*|TestGatewayServeConfiguresWindowsWebHandoff)$' \
  -count=1
scripts/audit-public-surface.sh

printf 'release_smoke_ok=true\n'
