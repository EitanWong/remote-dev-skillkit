#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

failures=()

add_failure() {
  failures+=("$1")
}

agent_files=(
  README.md
  TASKS.md
  docs/operations/AGENT_BOOTSTRAP_PROMPT.md
  docs/operations/BOOTSTRAP.md
  docs/operations/MCP_STDIO.md
  docs/operations/SKILLKIT_INSTALL.md
  skills/host-triage/SKILL.md
  skills/remote-job-review/SKILL.md
  skills/remote-vibe-coding/SKILL.md
  skills/safe-remote-support/SKILL.md
  internal/contracts/tools.go
  mcp/tools.json
)

if rg -n --hidden --glob '!.git/**' \
  'recommended (target-usable )?gateway URL|recommended gateway candidate|recommended gateway_url_candidates|use the returned gateway_url_candidates|use gateway_url_candidates|turn gateway_url_candidates' \
  "${agent_files[@]}" >/tmp/rdev-agent-surface-gateway.$$ 2>/dev/null; then
  add_failure "gateway candidate wording can make Agents hand-pick target URLs:\n$(cat /tmp/rdev-agent-surface-gateway.$$)"
fi
rm -f /tmp/rdev-agent-surface-gateway.$$

if rg -n --hidden --glob '!.git/**' \
  'Agents should (create target-side connection entries with|call this before asking a human to connect a new target host)' \
  README.md docs/operations skills internal/contracts/tools.go mcp/tools.json >/tmp/rdev-agent-surface-invite.$$ 2>/dev/null; then
  add_failure "low-level invite wording can become the ordinary first-contact path:\n$(cat /tmp/rdev-agent-surface-invite.$$)"
fi
rm -f /tmp/rdev-agent-surface-invite.$$

if rg -n --hidden --glob '!.git/**' \
  'Install Remote Dev Skillkit for this agent runtime|Full install prompt:|Clone or update the repository, read the full install prompt' \
  README.md docs/i18n >/tmp/rdev-agent-surface-short-prompt.$$ 2>/dev/null; then
  add_failure "README/i18n should route Agents to the canonical bootstrap prompt link, not embed a second short install prompt:\n$(cat /tmp/rdev-agent-surface-short-prompt.$$)"
fi
rm -f /tmp/rdev-agent-surface-short-prompt.$$

if ((${#failures[@]} > 0)); then
  printf 'agent_surfaces_ok=false\n' >&2
  for failure in "${failures[@]}"; do
    printf '%b\n' "$failure" >&2
  done
  exit 1
fi

printf 'agent_surfaces_ok=true\n'
