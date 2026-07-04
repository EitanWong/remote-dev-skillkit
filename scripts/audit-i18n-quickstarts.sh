#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

i18n_files=(
  docs/i18n/README.zh-CN.md
  docs/i18n/README.es.md
  docs/i18n/README.fr.md
  docs/i18n/README.de.md
  docs/i18n/README.ja.md
  docs/i18n/README.ko.md
  docs/i18n/README.pt-BR.md
  docs/i18n/README.hi.md
  docs/i18n/README.ar.md
  docs/i18n/README.ru.md
)

required_patterns=(
  "# Remote Dev Skillkit"
  "Codex"
  "Claude Code"
  "Hermes"
  "OpenClaw/OpenCode"
  "MCP"
  "Apache-2.0"
  "Install Remote Dev Skillkit for this agent runtime."
  "Repository: https://github.com/EitanWong/remote-dev-skillkit"
  "Full install prompt: https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md"
  "Clone or update the repository, read the full install prompt"
  "source of truth"
  "Ask one short question"
  "Agent Bootstrap Prompt"
  "../operations/AGENT_BOOTSTRAP_PROMPT.md"
  "go install ./cmd/rdev"
  "rdev doctor"
  "rdev bootstrap agent-plan --repo-root ."
  "rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session"
  "rdev skillkit export --source-root . --out dist/remote-dev-skillkit"
  "rdev skillkit verify --bundle dist/remote-dev-skillkit"
  "--frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent"
  "rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json"
  "rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills"
  "rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute"
  "rdev demo local"
  "rdev mcp tools"
  "../../README.md"
)

failures=()

for file in "${i18n_files[@]}"; do
  if [[ ! -f "$file" ]]; then
    failures+=("$file: missing")
    continue
  fi
  for pattern in "${required_patterns[@]}"; do
    if ! grep -Fq -- "$pattern" "$file"; then
      failures+=("$file: missing required quick-start text: $pattern")
    fi
  done
done

index="docs/i18n/README.md"
if [[ ! -f "$index" ]]; then
  failures+=("$index: missing")
else
  for file in "${i18n_files[@]}"; do
    name="$(basename "$file")"
    if ! grep -Fq -- "$name" "$index"; then
      failures+=("$index: missing language link: $name")
    fi
  done
  if ! grep -Fq -- "scripts/audit-i18n-quickstarts.sh" "$index"; then
    failures+=("$index: missing audit script reference")
  fi
  if ! grep -Fq -- "Agent Bootstrap Prompt" "$index"; then
    failures+=("$index: missing Agent Bootstrap Prompt reference")
  fi
fi

if ((${#failures[@]} > 0)); then
  printf 'i18n_quickstarts_ok=false\n' >&2
  for failure in "${failures[@]}"; do
    printf '%s\n' "$failure" >&2
  done
  exit 1
fi

printf 'i18n_quickstarts_ok=true files=%d\n' "${#i18n_files[@]}"
