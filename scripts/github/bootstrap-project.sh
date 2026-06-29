#!/usr/bin/env bash
set -euo pipefail

repo="${1:-}"
if [[ -z "$repo" ]]; then
  echo "usage: $0 OWNER/REPO" >&2
  exit 2
fi

ensure_label() {
  local name="$1"
  local color="$2"
  local description="$3"
  if gh label list --repo "$repo" --json name --jq '.[].name' | grep -Fxq "$name"; then
    gh label edit "$name" --repo "$repo" --color "$color" --description "$description" >/dev/null
  else
    gh label create "$name" --repo "$repo" --color "$color" --description "$description" >/dev/null
  fi
}

ensure_milestone() {
  local title="$1"
  if gh api "repos/$repo/milestones" --jq '.[].title' | grep -Fxq "$title"; then
    return
  fi
  gh api "repos/$repo/milestones" -f title="$title" >/dev/null
}

create_issue_if_missing() {
  local title="$1"
  local milestone="$2"
  local labels="$3"
  local body="$4"
  if gh issue list --repo "$repo" --state all --search "$title in:title" --json title --jq '.[].title' | grep -Fxq "$title"; then
    return
  fi
  gh issue create --repo "$repo" --title "$title" --body "$body" --milestone "$milestone" --label "$labels" >/dev/null
}

ensure_label "area:gateway" "1f77b4" "gateway state, APIs, queues, leases, storage"
ensure_label "area:host" "2ca02c" "host runtime, identity, trust, execution loop"
ensure_label "area:policy" "d62728" "policy decisions, approval gates, denial explanations"
ensure_label "area:evidence" "9467bd" "artifacts, evidence bundles, audit export, redaction"
ensure_label "area:transport" "17becf" "HTTPS polling, WSS, mTLS, reconnect"
ensure_label "area:adapter" "ff7f0e" "shell, PowerShell, Git, Codex, Claude Code, ACP, GUI, mesh"
ensure_label "area:bootstrap" "8c564b" "join manifest, release verification, platform bootstrap"
ensure_label "area:docs" "7f7f7f" "architecture, operations, release docs, runbooks"
ensure_label "kind:feature" "0e8a16" "new product capability"
ensure_label "kind:security" "b60205" "security boundary, exploit prevention, trust model"
ensure_label "kind:test" "5319e7" "automated or real-environment acceptance coverage"
ensure_label "kind:ops" "0052cc" "deployment, service management, release operations"
ensure_label "priority:p0" "b60205" "blocks safe use or release"
ensure_label "priority:p1" "fbca04" "required for current milestone"
ensure_label "priority:p2" "c5def5" "useful but not blocking current milestone"

ensure_milestone "v0.1 Local Safety Kernel"
ensure_milestone "v0.2 Temporary Windows MVP"
ensure_milestone "v0.3 Managed Mac Coding"
ensure_milestone "v0.4 Multi-Host Runtime"
ensure_milestone "v1.0 Public Skillkit"

create_issue_if_missing "Add host-side denial policy explanations" "v0.1 Local Safety Kernel" "area:policy,area:host,kind:security,priority:p1" \
"Acceptance:
- every hostrunner denial returns a structured explanation code and human summary
- missing workspace, missing capability, unsupported adapter, non-allowlisted command, workspace escape, identity mismatch, expired/tampered/replayed envelopes are covered
- CLI/HTTP failure reporting includes the explanation in artifacts or failure payload"

create_issue_if_missing "Export evidence bundles directly from gateway job ids" "v0.1 Local Safety Kernel" "area:evidence,area:gateway,kind:feature,priority:p1" \
"Acceptance:
- dev gateway can export a job evidence bundle without manually assembling JSON input files
- bundle includes job, envelope, artifacts, audit slice, chain, checksums, manifest
- tests cover succeeded and failed jobs"

create_issue_if_missing "Add local demo evidence bundle command" "v0.1 Local Safety Kernel" "area:evidence,area:docs,kind:test,priority:p2" \
"Acceptance:
- README quick start can produce a demo bundle end to end
- bundle can be inspected and audit chain verifies"

create_issue_if_missing "Complete v0.1 release checklist" "v0.1 Local Safety Kernel" "area:docs,kind:ops,priority:p1" \
"Acceptance:
- ./scripts/check.sh passes
- all v0.1 Definition of Done bullets are backed by command evidence
- CHANGELOG entry and tag plan are ready"

create_issue_if_missing "Implement outbound host transport prototype" "v0.2 Temporary Windows MVP" "area:transport,area:host,area:gateway,kind:feature,priority:p1" \
"Acceptance:
- host can claim/complete jobs over HTTPS polling without local-only restrictions
- reconnect/backoff behavior is tested
- cancellation/revocation checked between claims and during long-running jobs"

create_issue_if_missing "Build Windows foreground temporary host UX" "v0.2 Temporary Windows MVP" "area:host,area:bootstrap,kind:feature,priority:p1" \
"Acceptance:
- Windows host runs visibly in foreground
- shows operator, reason, TTL, gateway, stop instructions
- no service/scheduled-task/Run-key persistence in temporary mode"

create_issue_if_missing "Harden Windows bootstrap verification" "v0.2 Temporary Windows MVP" "area:bootstrap,kind:security,priority:p1" \
"Acceptance:
- bootstrap verifies signed manifest and host binary before execution
- Authenticode policy path is documented and testable
- bootstrap does not weaken execution policy, Defender, UAC, firewall, or Group Policy"

create_issue_if_missing "Add workspace lock manager" "v0.3 Managed Mac Coding" "area:host,area:policy,kind:feature,priority:p1" \
"Acceptance:
- one writer per repo root unless isolated worktree is created
- stale locks expire or release through cancel
- lock acquire/release are audited"

create_issue_if_missing "Implement Git worktree evidence flow" "v0.3 Managed Mac Coding" "area:adapter,area:evidence,kind:feature,priority:p1" \
"Acceptance:
- managed coding job creates branch/worktree
- captures status, diff stat, full diff artifact, verification commands
- push/merge/tag requires approval"

create_issue_if_missing "Implement Codex adapter MVP" "v0.3 Managed Mac Coding" "area:adapter,area:host,kind:feature,priority:p1" \
"Acceptance:
- runs Codex CLI inside locked workspace
- streams bounded output
- returns adapter result, diff/test evidence, residual risk"

echo "GitHub project bootstrap completed for $repo"
