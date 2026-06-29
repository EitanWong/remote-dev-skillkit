#!/usr/bin/env bash
set -euo pipefail

gofmt -w cmd internal
go test ./...
go vet ./...
find scripts -name '*.sh' -print0 | xargs -0 -n1 bash -n
