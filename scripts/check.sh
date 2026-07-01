#!/usr/bin/env bash
set -euo pipefail

find cmd internal pkg scripts -name '*.go' -print0 | xargs -0 gofmt -w
go test ./...
go vet ./...
find scripts -name '*.sh' -print0 | xargs -0 -n1 bash -n
scripts/audit-public-surface.sh
scripts/audit-i18n-quickstarts.sh
