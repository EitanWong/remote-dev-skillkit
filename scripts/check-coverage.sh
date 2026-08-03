#!/usr/bin/env bash
set -euo pipefail

# package:threshold pairs. Security-critical packages are coverage-gated so
# quality cannot silently regress. Thresholds sit a few points below measured
# values so unrelated changes do not cause flaky failures; raise them as gaps
# close. See docs/development/QUALITY_MATRIX.md for the module catalog.
checks=(
  "./internal/gateway:80"
  "./internal/hostrunner:80"
  "./internal/mcpstdio:80"
  "./internal/cli:82"
  "./internal/update:90"
  "./internal/operatorauth:70"
  "./internal/hosttrust:70"
  "./internal/httpapi:60"
  "./internal/hostidentity:60"
  "./internal/policy:65"
  "./internal/audit:70"
  "./internal/model:65"
  "./internal/contracts:70"
  "./internal/protectedstore:30"
)

for entry in "${checks[@]}"; do
  package="${entry%%:*}"
  threshold="${entry##*:}"
  output=$(go test -cover "$package" 2>&1) || {
    printf '%s\n' "$output"
    exit 1
  }
  printf '%s\n' "$output"
  coverage=$(printf '%s\n' "$output" | awk '
    /coverage:/ {
      for (i = 1; i <= NF; i++) {
        if ($i == "coverage:") {
          value = $(i + 1)
          gsub("%", "", value)
          print value
        }
      }
    }
  ' | tail -1)
  if [[ -z "$coverage" ]]; then
    printf 'coverage_check_failed package=%s reason=coverage_not_reported\n' "$package" >&2
    exit 1
  fi
  if ! awk -v coverage="$coverage" -v threshold="$threshold" 'BEGIN { exit !(coverage + 0 >= threshold) }'; then
    printf 'coverage_check_failed package=%s coverage=%s threshold=%s.0\n' "$package" "$coverage" "$threshold" >&2
    exit 1
  fi
  printf 'coverage_check_ok package=%s coverage=%s threshold=%s.0\n' "$package" "$coverage" "$threshold"
done
