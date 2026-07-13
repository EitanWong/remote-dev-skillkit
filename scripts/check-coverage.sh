#!/usr/bin/env bash
set -euo pipefail

packages=(
  ./internal/gateway
  ./internal/hostrunner
  ./internal/mcpstdio
  ./internal/cli
)

for package in "${packages[@]}"; do
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
  threshold=80
  if [[ "$package" == "./internal/cli" ]]; then
    threshold=82
  fi
  if ! awk -v coverage="$coverage" -v threshold="$threshold" 'BEGIN { exit !(coverage + 0 >= threshold) }'; then
    printf 'coverage_check_failed package=%s coverage=%s threshold=%s.0\n' "$package" "$coverage" "$threshold" >&2
    exit 1
  fi
  printf 'coverage_check_ok package=%s coverage=%s threshold=%s.0\n' "$package" "$coverage" "$threshold"
done
