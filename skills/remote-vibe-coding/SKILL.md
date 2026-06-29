---
name: remote-vibe-coding
description: Use when an agent needs to delegate coding work to an enrolled host running Codex, Claude Code, OpenCode, acpx, tmux, or shell adapters.
---

# Remote Vibe Coding

Use this skill to run coding tasks on an enrolled host while keeping work policy-bound and auditable.

## Rules

- Follow the final safety loop in `docs/architecture/ULTIMATE_CLOSURE_DESIGN.md` and the execution-ready closure rules in `docs/architecture/PERFECT_ENDING_SOLUTION.md`: typed intent, signed host-bound envelope, host-side validation, locked workspace, adapter execution, redacted evidence, audit, and revocation.
- Prefer ACP/acpx adapters over raw PTY scraping when available.
- Lock a workspace before starting a coding job.
- Use a branch or worktree for code changes.
- Prefer hosts started with `--workspace-lock-store` for coding jobs.
- Treat Codex, Claude Code, ACP, shell, and PowerShell as adapters behind the signed-job/evidence/approval contract.
- For Codex MVP jobs, require `codex.run` and `git.diff`, use a locked workspace/worktree, and expect `rdev.codex-result.v1` artifacts with Git status, diff/stat, and verification command evidence.
- Prefer `go test -json` when verifying Go projects so Codex artifacts include `rdev.test-report.v1` summaries.
- For shell or Codex jobs that may install packages, request elevation, control GUI, manage services, push, merge, deploy, publish, or change credentials, expect `rdev.approval-required.v1` before execution unless a matching approval token is present.
- When canceling a running Codex job, expect the host to cooperatively cancel the local Codex process, keep the gateway job in `canceled` state, and append cancellation evidence when available.
- Use `rdev acceptance managed-mac --out <empty-dir> --repo <repo>` before claiming the managed Mac coding golden path; review both `evidence/` and `approval-evidence/`.
- Verify acceptance output with `rdev acceptance verify --report <out>/report.json` before treating it as release evidence.
- Before publishing release artifacts or bootstrap download instructions, create and verify a signed release bundle with `rdev release create-bundle ...`, `rdev release verify-bundle --bundle <bundle> --root-public-key <root>`, and standalone `rdev-verify --bundle <bundle> --root-public-key <root>` when target-host bootstrap verification matters.
- For service-backed managed Mac acceptance, first generate and review `rdev acceptance managed-mac-service --out <empty-dir> --gateway <url> --ticket-code <code> --repo <repo>`; it must not auto-run `launchctl`. Use `rdev host service-control --execute` only after reviewing the generated plan.
- Do not push, merge, deploy, or modify credentials without approval.
- Return evidence: diff summary, tests run, exit codes, and artifacts.

## Workflow

1. List hosts with `rdev.hosts.list`.
2. Inspect capabilities with `rdev.hosts.capabilities`.
3. Select an adapter: `acpx`, `codex`, `claude`, `shell`, or `powershell`.
4. Prepare isolation with `rdev workspace prepare-worktree` when using local CLI workflows.
5. Create a job with workspace policy.
6. Stream status until completion.
7. Inspect artifacts and audit events.
8. Request approval before push/merge/deploy.
