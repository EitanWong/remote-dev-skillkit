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
