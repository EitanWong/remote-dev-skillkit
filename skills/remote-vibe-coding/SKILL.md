---
name: remote-vibe-coding
description: Use when an agent needs to delegate coding work to an enrolled host running Codex, Claude Code, OpenCode, acpx, tmux, or shell adapters.
---

# Remote Vibe Coding

Use this skill to run coding tasks on an enrolled host while keeping work policy-bound and auditable.

## Rules

- Prefer ACP/acpx adapters over raw PTY scraping when available.
- Lock a workspace before starting a coding job.
- Use a branch or worktree for code changes.
- Prefer hosts started with `--workspace-lock-store` for coding jobs.
- Treat Codex, Claude Code, ACP, shell, and PowerShell as adapters behind the signed-job/evidence/approval contract.
- For Codex MVP jobs, require `codex.run` and `git.diff`, use a locked workspace/worktree, and expect `rdev.codex-result.v1` artifacts with Git status, diff/stat, and verification command evidence.
- Prefer `go test -json` when verifying Go projects so Codex artifacts include `rdev.test-report.v1` summaries.
- For shell or Codex jobs that may install packages, request elevation, control GUI, manage services, push, merge, deploy, publish, or change credentials, expect `rdev.approval-required.v1` before execution unless a matching approval token is present.
- When canceling a running Codex job, expect the host to cooperatively cancel the local Codex process, keep the gateway job in `canceled` state, and append cancellation evidence when available.
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
