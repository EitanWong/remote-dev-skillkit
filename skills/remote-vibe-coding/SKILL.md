---
name: remote-vibe-coding
description: Use when an agent needs to delegate coding work to an enrolled host running Codex, Claude Code, OpenCode, acpx, tmux, shell, or PowerShell adapters.
---

# Remote Vibe Coding

Use this skill to run coding tasks on an enrolled host while keeping work policy-bound and auditable.

## Rules

- Follow the canonical final safety loop in `docs/architecture/PERFECT_ENDING_SOLUTION.md`: typed intent, signed host-bound envelope, host-side validation, locked workspace, adapter execution, redacted evidence, audit, and revocation.
- Treat `Converged Perfect Ending - 2026-06-30` in `docs/architecture/PERFECT_ENDING_SOLUTION.md` as the top decision lens; patch that layer for future architecture changes instead of appending more dated final sections. `Final Closure Specification - 2026-06-30` and `Definitive Perfect-Ending Blueprint - 2026-06-30` remain the implementation detail: every host, adapter, transport, release, and Skillkit change must preserve the kernel/plugin boundaries, gateway data model, host kernel layers, API/MCP surface, deployment profiles, acceptance matrix, product surfaces, component boundaries, stable protocol spine, mode separation, permission lattice, state machines, host sovereignty kernel, adapter lifecycle, reliability contract, security contract, golden paths, v1.0 proof gates, and close order.
- Preserve the final control-plane split: agents request typed work, the gateway governs, the host verifies locally, adapters execute only inside bounds, and proof comes from verifiers and evidence.
- Prefer ACP/acpx adapters over raw PTY scraping when available.
- Lock a workspace before starting a coding job.
- Use a branch or worktree for code changes.
- Prefer hosts started with `--workspace-lock-store` for coding jobs.
- For local/pre-production HTTPS or mTLS gateway drills, start the dev gateway with `--tls-cert --tls-key [--client-ca]` and start hosts with `--gateway-ca` plus `--gateway-client-cert --gateway-client-key` when client certificates are required; treat this only as transport authentication, not job authorization or production WSS readiness.
- Treat Codex, Claude Code, ACP, shell, and PowerShell as adapters behind the signed-job/evidence/approval contract.
- Before trusting a managed or temporary host registration that includes an enrollment certificate, verify it with MCP tool `rdev.enrollment.verify_certificate` or CLI `rdev enrollment verify-certificate`; initialize signed empty revocation baselines with `rdev enrollment init-revocations`, append later revocations with `rdev enrollment revoke-certificate --current`, and when a gateway exposes signed revocations, fetch them with `rdev enrollment fetch-revocations --root-public-key ...` and include `revocations_json` / `revocations_artifact_id` or `--revocations`. Certificate and revocation verification are read-only and never grant host access by themselves.
- For Codex MVP jobs, require `codex.run` and `git.diff`, use a locked workspace/worktree, and expect `rdev.codex-result.v1` artifacts with Git status, diff/stat, and verification command evidence.
- For Claude Code MVP jobs, require `claude-code.run` and `git.diff`, use a locked workspace/worktree, and expect `rdev.claude-code-result.v1` artifacts with Claude Code output, Git status, diff/stat, and verification command evidence. The default command is `claude -p <prompt>`; signed payloads may override `claude_code_command` and `claude_code_args` for deterministic or custom hosts.
- For ACP/acpx MVP jobs, require `acpx.run` and `git.diff`, use a locked workspace/worktree, and expect `rdev.acpx-result.v1` artifacts with acpx output, Git status, diff/stat, and verification command evidence. The default command is `acpx --cwd <workspace> codex exec <prompt>`; signed payloads may override `acpx_command`, `acpx_agent`, and `acpx_args` because upstream acpx is still alpha.
- Prefer `go test -json` when verifying Go projects so Codex artifacts include `rdev.test-report.v1` summaries.
- For shell, PowerShell, Codex, Claude Code, or acpx jobs that may install packages, request elevation, control GUI, manage services, push, merge, deploy, publish, change credentials, or change execution policy, expect `rdev.approval-required.v1` before execution unless a matching approval token is present.
- For PowerShell MVP jobs, require `powershell.user`, provide an explicit `command`, allowlist `pwsh`, `powershell`, `powershell.exe`, or the exact signed payload `powershell_command`, and expect `rdev.powershell-result.v1` evidence. Do not ask the adapter to bypass execution policy.
- When canceling a running Codex, Claude Code, acpx, shell, or PowerShell job, expect the host to cooperatively cancel the local process, keep the gateway job in `canceled` state, and append cancellation evidence when available.
- For adapter SDK or release-evidence runs, prefer starting managed or temporary hosts with `--capture-runtime-fixture` so shell, PowerShell, Codex, Claude Code, and acpx jobs append `rdev.adapter-runtime-fixture.v1` evidence in addition to their primary adapter result artifacts.
- For new adapters, start with `rdev adapter scaffold`, implement the runtime lifecycle through `adapterkit.RunLifecycle`, then verify lifecycle, runtime, result, and cancellation evidence through MCP tools `rdev.adapter.verify_lifecycle`, `rdev.adapter.verify_runtime`, `rdev.adapter.verify_result`, and `rdev.adapter.verify_cancellation`, CLI commands `rdev adapter verify-lifecycle`, `rdev adapter verify-runtime`, `rdev adapter verify-result`, and `rdev adapter verify-cancellation`, or `pkg/adapterkit` conformance before exposing the adapter to agents; shell, PowerShell, Codex, Claude Code, and acpx are the reference fixtures.
- Use `rdev acceptance managed-mac --out <empty-dir> --repo <repo>` before claiming the managed Mac coding golden path; review both `evidence/` and `approval-evidence/`.
- Verify acceptance output with `rdev acceptance verify --report <out>/report.json` before treating it as release evidence.
- Before publishing release artifacts or bootstrap download instructions, create and verify a signed release bundle with `rdev release create-bundle ...`, `rdev release verify-bundle --bundle <bundle> --root-public-key <root>`, and standalone `rdev-verify --bundle <bundle> --root-public-key <root>` when target-host bootstrap verification matters.
- Build release artifacts with `scripts/release/build-artifacts.sh` and review `rdev.build-artifacts.v1` before preparing candidates; release smoke must use real binaries for bootstrap-critical artifacts such as `rdev-host.exe` and `rdev-verify.exe`.
- For multi-platform releases, run `scripts/release/prepare-platform-candidates.sh --build-manifest <build-artifacts.json> ...` and review `rdev.platform-release-candidates.v1`; each platform candidate must verify independently before a public release plan is trusted.
- Use `scripts/github/plan-platform-release.sh --platform-candidates <platform-candidates.json> --repo <owner/repo>` for multi-platform public release planning; review platform archives, `rdev.platform-release-index.v1`, `rdev.github-platform-release-verification.v1`, `INSTALL_PLATFORMS.md`, and generated `gh release` commands before any external mutation.
- Before creating or mutating GitHub repositories, labels, milestones, issues, projects, or releases, run `scripts/github/audit-project-readiness.sh --repo <owner/repo> --out <path>` and review `rdev.github-project-readiness.v1`; the report must keep `external_mutation=false`.
- Before publishing a GitHub Release or instructing users to install artifacts, run `rdev release prepare-candidate ...` and then `rdev release verify-candidate --candidate <dir|release-candidate.json>`; treat `ok=false` as release-blocking.
- For agent-framework distribution, run `rdev skillkit export`, `rdev skillkit verify`, `rdev skillkit plan-install`, `rdev skillkit verify-install-plan`, and `rdev skillkit install` dry-run before telling users to install into Codex, Claude Code, Hermes, OpenClaw, OpenCode, or a generic MCP agent; review generated scripts and require `--execute` before local copying while keeping `external_mutation=false`.
- Use `scripts/github/plan-release.sh --candidate <dir|release-candidate.json> --repo <owner/repo>` to create a local GitHub Release plan; do not run the generated `gh release` commands without explicit operator approval.
- For release-surface changes, expect `./scripts/check.sh` and `./scripts/ci/release-smoke.sh` to pass locally and in GitHub Actions before claiming release readiness.
- For service-backed managed Mac acceptance, first generate and review `rdev acceptance managed-mac-service --out <empty-dir> --gateway <url> --ticket-code <code> --repo <repo>`; it must not auto-run `launchctl`. Use `rdev host service-control --execute` only after reviewing the generated plan.
- For Linux managed service work, use `rdev host install-service --platform linux` only as a reviewed systemd user-unit plan with release-bundle gate arguments; for release-evidence planning, run `rdev acceptance linux-managed-service` and `rdev acceptance verify-linux-managed-service`, then package real run evidence with `rdev acceptance package-linux-managed-service`. Do not claim real Linux managed-service support until a Linux host proves start/status/reboot-or-logout reconnect/job evidence/stop/uninstall acceptance.
- For Windows managed service work, use `rdev host install-service --platform windows` only as a reviewed `sc.exe` command plan with `start= demand` and release-bundle gate arguments; for release-evidence planning, run `rdev acceptance windows-managed-service` and `rdev acceptance verify-windows-managed-service`. Do not claim real Windows Service support until a clean Windows host proves create/status/start/reconnect/stop/uninstall acceptance.
- Do not push, merge, deploy, or modify credentials without approval.
- Return evidence: diff summary, tests run, exit codes, and artifacts.

## Workflow

1. List hosts with `rdev.hosts.list`.
2. Inspect capabilities with `rdev.hosts.capabilities`.
3. Select an adapter: `acpx`, `codex`, `claude-code`, `shell`, or `powershell`.
4. Prepare isolation with `rdev workspace prepare-worktree` when using local CLI workflows.
5. Create a job with workspace policy.
6. Stream status until completion.
7. Inspect artifacts and audit events.
8. Request approval before push/merge/deploy.
