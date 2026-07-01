---
name: remote-vibe-coding
description: Use when an agent needs to delegate coding work to an enrolled host running Codex, Claude Code, OpenCode, acpx, tmux, shell, or PowerShell adapters.
---

# Remote Vibe Coding

Use this skill to run coding tasks on an enrolled host while keeping work policy-bound and auditable.

## Rules

- Follow the canonical final safety loop in `docs/architecture/PERFECT_ENDING_SOLUTION.md`: typed intent, signed host-bound envelope, host-side validation, locked workspace, adapter execution, redacted evidence, audit, and revocation.
- Treat Remote Dev Skillkit as AI-native. The human should be able to say which
  machine needs help; the agent should probe local configuration, create an
  invite with MCP tool `rdev.invites.create` or CLI `rdev invite create`, hand
  the generated `host_command` to the human for the target machine, wait for the
  host to appear, request approval only when needed, then dispatch scoped jobs
  and collect evidence.
- Preserve the current architecture decision layer in `docs/architecture/PERFECT_ENDING_SOLUTION.md`; update that document when changing host, adapter, transport, release, or Skillkit boundaries.
- Preserve the final control-plane split: agents request typed work, the gateway governs, the host verifies locally, adapters execute only inside bounds, and proof comes from verifiers and evidence.
- Before creating a coding job, discover the available hosts, target OS,
  workspace path, Git state, installed adapters, gateway/MCP configuration,
  release trust inputs, and operator-approved capabilities. Probe with read-only checks
  such as `rdev doctor`, `rdev.hosts.list`, `rdev.hosts.capabilities`,
  `rdev mcp tools`, `git status`, `git rev-parse`, `command -v`, and `where`.
- If gateway URL, ticket code, host identity, workspace root, adapter choice,
  release root, framework install path, or approval policy is unclear, ask the
  user or operator before proceeding. Do not infer real values from examples.
- Never invent unclear gateway, workspace, adapter, release, framework, or
  approval configuration from placeholder examples.
- Adapt to the detected system: use LaunchAgent planning on macOS, systemd user
  units on Linux, Windows Service plans on Windows, PowerShell only when present,
  and shell/Codex/Claude/acpx only when the host advertises those capabilities.
- Prefer ACP/acpx adapters over raw PTY scraping when available.
- Lock a workspace before starting a coding job.
- Use a branch or worktree for code changes.
- Prefer hosts started with `--workspace-lock-store` for coding jobs.
- For HTTPS, mTLS, or WSS gateway drills, start the gateway with `--tls-cert --tls-key [--client-ca]` and start hosts with `--gateway-ca` plus `--gateway-client-cert --gateway-client-key` when client certificates are required; use `--transport wss` for WebSocket job delivery. Treat transport authentication as connectivity identity only, not job authorization.
- Treat Codex, Claude Code, ACP, shell, and PowerShell as adapters behind the signed-job/evidence/approval contract.
- Before trusting a managed or temporary host registration that includes an enrollment certificate, verify it with MCP tool `rdev.enrollment.verify_certificate` or CLI `rdev enrollment verify-certificate`; when requesting or renewing a certificate from a configured gateway, use `rdev enrollment issue-certificate --root-public-key ...` or `rdev enrollment renew-certificate --gateway ... --root-public-key ...` so returned certificates are pinned-root verified before local write, and include `--operator-token-file` when the gateway was started with `--operator-auth`. Initialize signed empty revocation baselines with `rdev enrollment init-revocations`, renew expiring local certificates with `rdev enrollment renew-certificate --revocations ...`, append later revocations with `rdev enrollment revoke-certificate --current`, and when a gateway exposes signed revocations, fetch them with `rdev enrollment fetch-revocations --root-public-key ... [--operator-token-file ...]` and include `revocations_json` / `revocations_artifact_id` or `--revocations`. Hosts that register with an enrollment certificate should use `rdev host serve --renew-enrollment-certificate --enrollment-root-public-key ...` for near-expiry hosted renewal and `rdev host serve --fetch-enrollment-revocations --enrollment-root-public-key ... [--operator-token-file ...]` when the gateway publishes signed revocations, so the host verifies the local certificate, refreshes it before expiry, and refuses revoked certificates before registration. Certificate and revocation verification are read-only and never grant host access by themselves.
- For enrollment authority operations, produce machine-readable evidence with `rdev enrollment lifecycle key-custody`, `rdev enrollment lifecycle fleet-renewal-plan`, and `rdev enrollment lifecycle emergency-drill`; do not store private keys, private hostnames, or local machine paths in public evidence.
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
- Build release artifacts with `scripts/release/build-artifacts.sh` and review `rdev.build-artifacts.v1`, `sbom.spdx.json`, `provenance.json`, and `checksums.txt` before preparing candidates; release smoke must use real binaries for bootstrap-critical artifacts such as `rdev-host.exe` and `rdev-verify.exe`.
- For multi-platform releases, run `scripts/release/prepare-platform-candidates.sh --build-manifest <build-artifacts.json> ...` and review `rdev.platform-release-candidates.v1`; each platform candidate must verify independently before a public release plan is trusted.
- Use `scripts/github/plan-platform-release.sh --platform-candidates <platform-candidates.json> --repo <owner/repo>` for multi-platform public release planning; review platform archives, `rdev.platform-release-index.v1`, `rdev.github-platform-release-verification.v1`, `INSTALL_PLATFORMS.md`, and generated `gh release` commands before any external mutation.
- Before creating or mutating GitHub repositories, labels, milestones, issues, projects, or releases, run `scripts/github/audit-project-readiness.sh --repo <owner/repo> --out <path>` and review `rdev.github-project-readiness.v1`; the report must keep `external_mutation=false`.
- Before publishing a GitHub Release or instructing users to install artifacts, run `rdev release prepare-candidate ...` and then `rdev release verify-candidate --candidate <dir|release-candidate.json>`; review `sbom.spdx.json`, `provenance.json`, package-relative paths, and verification output, and treat `ok=false` as release-blocking.
- For agent-framework distribution, run `rdev skillkit export`, `rdev skillkit verify`, `rdev skillkit plan-install`, `rdev skillkit verify-install-plan`, and `rdev skillkit install` dry-run before telling users to install into Codex, Claude Code, Hermes, OpenClaw, OpenCode, or a generic MCP agent; review generated scripts and require `--execute` before local copying while keeping `external_mutation=false`.
- Use `scripts/github/plan-release.sh --candidate <dir|release-candidate.json> --repo <owner/repo>` to create a local GitHub Release plan; do not run the generated `gh release` commands without explicit operator approval.
- For release-surface changes, expect `./scripts/check.sh` and `./scripts/ci/release-smoke.sh` to pass locally and in GitHub Actions before claiming release readiness.
- For service-backed managed Mac acceptance, first generate and review `rdev acceptance managed-mac-service --out <empty-dir> --gateway <url> --ticket-code <code> --repo <repo>`; it must not auto-run `launchctl`. Use `rdev host service-control --execute` only after reviewing the generated plan.
- For Linux managed service work, use `rdev host install-service --platform linux` only as a reviewed systemd user-unit plan with release-bundle gate arguments; for release-evidence planning, run `rdev acceptance linux-managed-service` and `rdev acceptance verify-linux-managed-service`, then package real run evidence with `rdev acceptance package-linux-managed-service`. Do not claim real Linux managed-service support until a Linux host proves start/status/reboot-or-logout reconnect/job evidence/stop/uninstall acceptance.
- For Windows managed service work, use `rdev host install-service --platform windows` only as a reviewed `sc.exe` command plan with `start= demand` and release-bundle gate arguments; for release-evidence planning, run `rdev acceptance windows-managed-service` and `rdev acceptance verify-windows-managed-service`. Do not claim real Windows Service support until a clean Windows host proves create/status/start/reconnect/stop/uninstall acceptance.
- Do not push, merge, deploy, or modify credentials without approval.
- Return evidence: diff summary, tests run, exit codes, and artifacts.

## Workflow

1. Discover the local runtime, MCP tools, gateway configuration, and candidate
   host list.
2. If no suitable host is active, create an invite with `rdev.invites.create`
   or `rdev invite create` and ask the human to run only the generated
   `host_command` on the target machine.
3. Wait for the host with `rdev.hosts.list`; approve it with
   `rdev.hosts.approve` only after the operator confirms the host is expected.
4. Inspect host OS, workspace, capabilities, adapters, and approval policy.
5. Ask for any missing gateway, host, workspace, release, adapter, or approval
   configuration that cannot be safely discovered.
6. Select an adapter: `acpx`, `codex`, `claude-code`, `shell`, or `powershell`.
7. Prepare isolation with `rdev workspace prepare-worktree` when using local CLI workflows.
8. Create a job with workspace policy.
9. Stream status until completion.
10. Inspect artifacts and audit events.
11. Request approval before push/merge/deploy.
