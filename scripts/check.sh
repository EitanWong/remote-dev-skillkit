#!/usr/bin/env bash
set -euo pipefail

unformatted="$(find cmd internal pkg -type f -name '*.go' -print0 | xargs -0 -r gofmt -l)"
if [[ -n "$unformatted" ]]; then
  printf 'gofmt_check_failed\n%s\n' "$unformatted" >&2
  exit 1
fi

go test ./...
go vet ./...
scripts/check-coverage.sh
find scripts -name '*.sh' -print0 | xargs -0 -n1 bash -n
scripts/audit-public-surface.sh
scripts/audit-skills.sh
scripts/ux-smoke.sh
