#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

failures=()

add_failure() {
  failures+=("$1")
}

if find . -name .DS_Store -print -quit | grep -q .; then
  add_failure ".DS_Store files are present"
fi

public_files=(
  README.md
  SECURITY.md
  CONTRIBUTING.md
  CODE_OF_CONDUCT.md
  TASKS.md
  AGENTS.md
  docs/architecture
  docs/project
  docs/operations
  docs/security
  docs/i18n
  cmd
  internal
  pkg
  examples
  mcp
  skills
  scripts
  .github
)

private_pattern='/(Users|home)/[^[:space:]/]+/[^[:space:]`"<>)]*|C:\\Users\\[^\\[:space:]]+\\[^[:space:]`"<>)]*'
if [[ -n "${RDEV_PUBLIC_SURFACE_PRIVATE_PATTERNS:-}" ]]; then
  private_pattern="${private_pattern}|${RDEV_PUBLIC_SURFACE_PRIVATE_PATTERNS}"
fi
if rg -n --hidden --glob '!.git/**' --glob '!dist/**' --glob '!bin/**' --glob '!scripts/audit-public-surface.sh' "$private_pattern" "${public_files[@]}" >/tmp/rdev-public-private-matches.$$ 2>/dev/null; then
  if grep -Ev '/Users/example(/|$)|/home/example(/|$)|C:\\Users\\Alice\\|C:\\Users\\example\\' /tmp/rdev-public-private-matches.$$ >/tmp/rdev-public-private-filtered.$$; then
    add_failure "private identifiers found:\n$(cat /tmp/rdev-public-private-filtered.$$)"
  fi
fi
rm -f /tmp/rdev-public-private-matches.$$ /tmp/rdev-public-private-filtered.$$

if rg -n --hidden --glob '!.git/**' --glob '!dist/**' --glob '!bin/**' --glob '!scripts/audit-public-surface.sh' '^(Date: 20[0-9]{2}-[0-9]{2}-[0-9]{2}|## .*20[0-9]{2}-[0-9]{2}-[0-9]{2})' README.md docs/project docs/architecture skills >/tmp/rdev-public-dated-final-matches.$$ 2>/dev/null; then
  add_failure "dated public architecture labels found:\n$(cat /tmp/rdev-public-dated-final-matches.$$)"
fi
rm -f /tmp/rdev-public-dated-final-matches.$$

if rg -n --hidden --glob '!.git/**' --glob '!dist/**' --glob '!bin/**' --glob '!scripts/audit-public-surface.sh' 'github\.com/an operator|an operator[A-Z][A-Za-z]+' . >/tmp/rdev-public-bad-repo-matches.$$ 2>/dev/null; then
  add_failure "invalid anonymized repository placeholders found:\n$(cat /tmp/rdev-public-bad-repo-matches.$$)"
fi
rm -f /tmp/rdev-public-bad-repo-matches.$$

if ((${#failures[@]} > 0)); then
  printf 'public_surface_ok=false\n' >&2
  for failure in "${failures[@]}"; do
    printf '%b\n' "$failure" >&2
  done
  exit 1
fi

printf 'public_surface_ok=true\n'
