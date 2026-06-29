#!/usr/bin/env bash
set -euo pipefail

dry_run=false
repo=""

usage() {
  cat >&2 <<'USAGE'
usage: scripts/github/bootstrap-project.sh [--dry-run] OWNER/REPO

Creates or updates GitHub labels, milestones, and the current seed backlog.
Use --dry-run to preview without mutating GitHub.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      dry_run=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
    *)
      if [[ -n "$repo" ]]; then
        echo "unexpected extra argument: $1" >&2
        usage
        exit 2
      fi
      repo="$1"
      shift
      ;;
  esac
done

if [[ -z "$repo" ]]; then
  usage
  exit 2
fi

if [[ "$dry_run" == false ]] && ! command -v gh >/dev/null 2>&1; then
  echo "gh is required unless --dry-run is used" >&2
  exit 127
fi

say() {
  printf '%s\n' "$*"
}

ensure_label() {
  local name="$1"
  local color="$2"
  local description="$3"
  if [[ "$dry_run" == true ]]; then
    say "label: $name [$color] $description"
    return
  fi
  if gh label list --repo "$repo" --json name --jq '.[].name' | grep -Fxq "$name"; then
    gh label edit "$name" --repo "$repo" --color "$color" --description "$description" >/dev/null
  else
    gh label create "$name" --repo "$repo" --color "$color" --description "$description" >/dev/null
  fi
}

ensure_milestone() {
  local title="$1"
  local description="$2"
  if [[ "$dry_run" == true ]]; then
    say "milestone: $title :: $description"
    return
  fi
  if gh api "repos/$repo/milestones" --jq '.[].title' | grep -Fxq "$title"; then
    return
  fi
  gh api "repos/$repo/milestones" -f title="$title" -f description="$description" >/dev/null
}

create_issue_if_missing() {
  local title="$1"
  local milestone="$2"
  local labels="$3"
  local body="$4"
  if [[ "$dry_run" == true ]]; then
    say "issue: $title"
    say "  milestone: $milestone"
    say "  labels: $labels"
    return
  fi
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
ensure_label "area:service" "c27ba0" "launchd, Windows Service, systemd, lifecycle control"
ensure_label "area:skillkit" "7057ff" "agent skills, MCP contracts, install bundles"
ensure_label "area:release" "5319e7" "signed artifacts, changelog, release evidence, distribution"
ensure_label "area:docs" "7f7f7f" "architecture, operations, release docs, runbooks"
ensure_label "kind:feature" "0e8a16" "new product capability"
ensure_label "kind:security" "b60205" "security boundary, exploit prevention, trust model"
ensure_label "kind:test" "5319e7" "automated or real-environment acceptance coverage"
ensure_label "kind:ops" "0052cc" "deployment, service management, release operations"
ensure_label "kind:docs" "6f42c1" "documentation, examples, runbooks, public packaging"
ensure_label "priority:p0" "b60205" "blocks safe use or release"
ensure_label "priority:p1" "fbca04" "required for current milestone"
ensure_label "priority:p2" "c5def5" "useful but not blocking current milestone"

ensure_milestone "v0.1 Local Safety Kernel" "Signed local job execution, host-side verification, approval gates, evidence bundles, audit verification, and portable Skillkit export."
ensure_milestone "v0.2 Temporary Windows Host" "One visible verified Windows command starts an outbound-only foreground host, enforces approvals, revokes cleanly, and leaves no persistence."
ensure_milestone "v0.3 Managed Mac Coding" "Eitan-owned managed Mac reconnects after reboot, runs Codex in a locked worktree, returns diff/test evidence, and gates push/merge/deploy."
ensure_milestone "v0.4 Managed Device Generalization" "Windows Service, systemd, OS-protected storage, WSS/mTLS, adapter SDK, and durable reconnect across platforms."
ensure_milestone "v1.0 Public Skillkit" "Stable self-hostable open-source release with signed artifacts, installer docs, conformance suite, threat model, and public acceptance transcripts."

create_issue_if_missing "Run service-backed managed Mac acceptance transcript" "v0.3 Managed Mac Coding" "area:service,area:host,area:evidence,kind:test,priority:p1" \
"Context:
- docs/operations/ACCEPTANCE.md
- docs/project/ACCEPTANCE_TESTS.md
- docs/architecture/PERFECT_ENDING_SOLUTION.md

Acceptance:
- generate a managed Mac LaunchAgent plan
- review the plist, label, logs, identity/trust/nonce/approval stores, and workspace-lock store
- start and inspect with rdev host service-control --execute
- confirm reconnect after login or reboot
- run the managed Mac Codex acceptance job through the service
- run rdev acceptance verify on service-backed evidence
- stop and uninstall the LaunchAgent without touching unrelated plists

Verification:
- attach redacted command transcript
- attach verification JSON
- attach evidence bundle checksums
- ./scripts/check.sh passes before closing"

create_issue_if_missing "Build Windows foreground temporary host no-persistence acceptance" "v0.2 Temporary Windows Host" "area:bootstrap,area:host,area:evidence,kind:test,priority:p1" \
"Context:
- docs/project/ACCEPTANCE_TESTS.md Gate A
- scripts/bootstrap/windows-temporary.ps1
- docs/security/RELEASE_KEY_LIFECYCLE.md

Acceptance:
- clean Windows 10/11 VM joins from one visible command
- bootstrap verifies pinned verifier and signed host artifact before execution
- host runs foreground, shows reason, TTL, gateway, and stop instructions
- host connects outbound only
- package install/elevation/service/GUI probes return approval-required
- host revoke cancels queued/running work where possible
- no service, scheduled task, Run key, startup shortcut, or firewall rule remains

Verification:
- attach PowerShell transcript
- attach no-persistence inspection output
- attach release verification output
- attach evidence and audit checksums"

create_issue_if_missing "Add production WSS host channel with authenticated fallback" "v0.4 Managed Device Generalization" "area:transport,area:gateway,area:host,kind:feature,kind:security,priority:p1" \
"Context:
- docs/architecture/PERFECT_ENDING_SOLUTION.md Transport Closure
- docs/project/ROADMAP.md v0.4

Acceptance:
- host can connect over authenticated WSS for interactive job status and artifact events
- HTTPS long-poll remains a supported fallback
- reconnect and bounded leases are tested
- cancellation/revocation propagates to running jobs
- transport identity never replaces signed job authorization

Verification:
- unit/integration tests cover WSS connect, reconnect, cancellation, and fallback
- docs describe deployment and threat boundaries"

create_issue_if_missing "Add OS-protected managed host identity and trust storage" "v0.4 Managed Device Generalization" "area:host,area:policy,kind:security,priority:p1" \
"Context:
- docs/security/THREAT_MODEL.md
- docs/architecture/PERFECT_ENDING_SOLUTION.md Data And Storage

Acceptance:
- macOS uses Keychain where available
- Windows uses DPAPI where available
- Linux uses libsecret or clearly documented file-backed fallback
- file-backed dev stores remain available for tests
- rollback and revocation checks still pass

Verification:
- platform-targeted tests or documented manual evidence
- migration path from file-backed dev stores"

create_issue_if_missing "Extract adapter SDK and conformance suite" "v0.4 Managed Device Generalization" "area:adapter,area:policy,area:evidence,kind:feature,kind:test,priority:p1" \
"Context:
- docs/architecture/PERFECT_ENDING_SOLUTION.md Adapter SDK Contract
- existing shell and Codex adapter behavior

Acceptance:
- define detect, plan, prepare, run, collect, cleanup interfaces
- shell and Codex adapters pass shared conformance fixtures
- tests cover capability mapping, workspace escape rejection, approval pause, cancellation, redaction, evidence, and cleanup
- new adapter authors can run the conformance suite locally

Verification:
- go test ./internal/...
- docs explain how to add an adapter"

create_issue_if_missing "Implement Claude Code and ACP adapters behind the safety kernel" "v0.4 Managed Device Generalization" "area:adapter,area:skillkit,kind:feature,priority:p2" \
"Context:
- docs/architecture/PERFECT_ENDING_SOLUTION.md Adapter SDK Contract
- skills/remote-vibe-coding/SKILL.md

Acceptance:
- adapters run only after signed envelope, host validation, workspace lock, and approval preflight
- diff/test evidence matches Codex adapter expectations where possible
- push/merge/deploy/publish/credential/service intents pause before execution
- Skillkit documents when to select Codex, Claude Code, ACP, shell, or PowerShell

Verification:
- adapter conformance tests pass
- at least one local fixture produces evidence bundle"

create_issue_if_missing "Add Windows Service and systemd managed host lifecycle" "v0.4 Managed Device Generalization" "area:service,area:host,kind:feature,kind:ops,priority:p1" \
"Context:
- docs/architecture/PERFECT_ENDING_SOLUTION.md Managed Coding Experience
- docs/project/ROADMAP.md v0.4

Acceptance:
- rdev host install-service supports Windows Service and systemd modes
- install, status, service-control, stop, and uninstall are explicit and inspectable
- managed service arguments include identity, trust, nonce, approval, and workspace-lock stores
- temporary mode cannot install persistence through these commands

Verification:
- unit tests for generated service definitions
- dry-run CLI smoke tests for Windows and Linux service plans"

create_issue_if_missing "Package public Skillkit install paths for Codex Claude Code OpenCode Hermes and generic MCP" "v1.0 Public Skillkit" "area:skillkit,area:docs,kind:docs,priority:p1" \
"Context:
- docs/operations/SKILLKIT_INSTALL.md
- rdev skillkit export

Acceptance:
- one install path each for Codex, Claude Code, OpenCode/OpenClaw, Hermes, and generic MCP
- exported bundle includes manifest checksums and framework notes
- quickstart proves a local self-host user can create a ticket, run a job, and export evidence
- no Hermes-specific assumption is required for generic users

Verification:
- rdev skillkit export smoke test
- docs quickstart commands checked locally"

create_issue_if_missing "Prepare signed release pipeline and public acceptance transcript package" "v1.0 Public Skillkit" "area:release,area:evidence,kind:ops,kind:security,priority:p1" \
"Context:
- docs/project/RELEASE_CHECKLIST.md
- docs/security/RELEASE_KEY_LIFECYCLE.md
- docs/project/ACCEPTANCE_TESTS.md

Acceptance:
- build artifacts for macOS, Linux, Windows
- generate checksums, signed artifact manifests, and signed release manifest index
- include SBOM or documented equivalent
- package redacted acceptance transcripts, report JSON, audit verification, and evidence checksums
- release checklist maps every v1.0 gate to evidence

Verification:
- release verify commands pass against staged artifacts
- acceptance transcript package is reproducible from documented commands"

if [[ "$dry_run" == true ]]; then
  say "GitHub project bootstrap dry-run completed for $repo"
else
  say "GitHub project bootstrap completed for $repo"
fi
