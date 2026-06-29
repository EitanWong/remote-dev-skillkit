---
name: remote-vibe-coding
description: Use when an agent needs to delegate coding work to an enrolled host running Codex, Claude Code, OpenCode, acpx, tmux, or shell adapters.
---

# Remote Vibe Coding

Use this skill to run coding tasks on an enrolled host while keeping work policy-bound and auditable.

## Rules

- Follow the canonical final safety loop in `docs/architecture/PERFECT_ENDING_SOLUTION.md`: typed intent, signed host-bound envelope, host-side validation, locked workspace, adapter execution, redacted evidence, audit, and revocation.
- Preserve the final control-plane split: agents request typed work, the gateway governs, the host verifies locally, adapters execute only inside bounds, and proof comes from verifiers and evidence.
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
- Build release artifacts with `scripts/release/build-artifacts.sh` and review `rdev.build-artifacts.v1` before preparing candidates; release smoke must use real binaries for bootstrap-critical artifacts such as `rdev-host.exe` and `rdev-verify.exe`.
- For multi-platform releases, run `scripts/release/prepare-platform-candidates.sh --build-manifest <build-artifacts.json> ...` and review `rdev.platform-release-candidates.v1`; each platform candidate must verify independently before a public release plan is trusted.
- Use `scripts/github/plan-platform-release.sh --platform-candidates <platform-candidates.json> --repo <owner/repo>` for multi-platform public release planning; review platform archives, `rdev.platform-release-index.v1`, `rdev.github-platform-release-verification.v1`, `INSTALL_PLATFORMS.md`, and generated `gh release` commands before any external mutation.
- Before publishing a GitHub Release or instructing users to install artifacts, run `rdev release prepare-candidate ...` and then `rdev release verify-candidate --candidate <dir|release-candidate.json>`; treat `ok=false` as release-blocking.
- Use `scripts/github/plan-release.sh --candidate <dir|release-candidate.json> --repo <owner/repo>` to create a local GitHub Release plan; do not run the generated `gh release` commands without explicit operator approval.
- For release-surface changes, expect `./scripts/check.sh` and `./scripts/ci/release-smoke.sh` to pass locally and in GitHub Actions before claiming release readiness.
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
