# Roadmap

The roadmap implements the canonical [Final System Design](../architecture/FINAL_SYSTEM_DESIGN.md): consent-first enrollment, outbound-only transport, signed job envelopes, host-side policy enforcement, workspace locks, approval gates, audit evidence, and agent-native MCP/Skill packaging. Its "Final Reference Blueprint" and "Final Architecture Lock" are the implementation contract: a small safety microkernel with replaceable adapters for shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, mesh, Coder, and DevPod. [Final Closure Blueprint](../architecture/FINAL_CLOSURE_BLUEPRINT.md) is the concise release-facing contract, and [Perfect End-State Architecture](../architecture/PERFECT_END_STATE.md) remains the broader blueprint.

## Maturity Gates

| Gate | Proves | Does not claim |
|---|---|---|
| `v0.1` Local Safety Kernel | signed jobs, host-side verification, denials, approvals, evidence, audit | production networking or OS service behavior |
| `v0.2` Windows Temporary Host | visible one-command enrollment, release verification, outbound-only repair | unattended managed-device operations |
| `v0.3` Managed Mac Coding | durable owned-host Codex workflow with workspace evidence | general multi-tenant fleet management |
| `v0.4` Managed Device Generalization | multi-OS services, durable storage, adapter SDK, reconnects | public protocol stability |
| `v1.0` Public Skillkit | stable schemas, safe defaults, signed releases, self-host docs | every possible adapter or remote-access product |

## v0.0.1 Foundation

- Repository skeleton.
- CLI skeleton.
- Doctor command.
- MCP tool contract listing.
- Agent Skills drafts.
- Architecture and security docs.

## v0.1.0 Local Safety Kernel

- Local gateway process.
- Local host process.
- In-memory tickets, hosts, jobs, artifacts, and audit events.
- MCP stdio server for tool calls.
- Development signed job envelopes.
- Demonstrable local temporary session.
- Development HTTPS long-poll host job transport.
- Scoped shell adapter with workspace boundary, allowlisted argv, timeouts, output caps, and failure reporting.
- Host identity storage wired into registration and job binding.
- Durable host trust bundle store.
- Trust-bundle key rotation/revocation flow.
- Host-bound trust bundle update checks for managed host refresh.
- Host-side artifact redaction.
- Hash-chained audit export verifier.
- Evidence bundle export.
- Gateway/API evidence bundle export directly from job ids.
- Skillkit bundle export for agent runtimes and mainstream framework notes.
- Strong symlink/workspace escape regression tests.
- Explainable denial and approval decisions.

Exit gate: local demo proves ticket, host registration, outbound host job wait through short-poll or long-poll, signed job execution, artifact storage, evidence export, Skillkit export, audit export, approval-token consumption, and host-side rejection of tampered, expired, wrong-host, replayed, non-allowlisted, and workspace-escaping jobs.

## v0.2.0 Windows Temporary Host

- Signed Windows binary.
- PowerShell bootstrap.
- Visible foreground support window or console UI.
- Outbound-only connection to gateway.
- Durable signing key storage and rotation.
- Shell and file scoped jobs.
- Host local audit spool and revocation handling.
- Authenticode verification policy.
- No-persistence inspection script.
- Local-user approval prompt for elevation, GUI, service, and destructive requests.
- Clean Windows acceptance run.

Exit gate: clean Windows 10/11 VM joins from one visible command, verifies signed artifacts before host execution, connects outbound only, enforces approvals, revokes cleanly, and leaves no service or autorun persistence.

## v0.3.0 Managed Mac Coding

- macOS LaunchAgent managed mode.
- LaunchAgent plist generation with explicit launchctl next steps.
- LaunchAgent plist status and safe uninstall commands.
- Host identity protected storage.
- Workspace lock manager and Git worktree preparation foundation.
- Workspace locks wired into hostrunner, host serve, and managed LaunchAgent arguments.
- Codex adapter MVP with hostrunner integration, `codex.run` and `git.diff` capability checks, locked-workspace execution, implicit approval preflight for high-risk external consequences, Git diff/status evidence, optional verification command evidence, `go test -json` report parsing, output caps, and redaction.
- Codex adapter conformance coverage for workspace canonicalization, write-scope escape rejection before execution, nonzero-exit evidence, host-side redaction, output truncation, and timeout cancellation evidence.
- Codex adapter cooperative cancellation through `ExecuteContext`, context-aware hostrunner execution, and host-side polling of gateway job cancellation state.
- Canceled Codex jobs append cancellation evidence artifacts without changing the gateway job's `canceled` terminal state.
- Git diff and test evidence bundles.
- Approval before push, merge, deploy, credential changes, or service changes.
- Managed install, health, stop, and uninstall commands.

Exit gate: Eitan's managed Mac reconnects after reboot, Lucky selects it through MCP, Codex runs in a locked worktree, and the result includes diff, tests, artifacts, audit slice, and residual risk.

## v0.4.0 Managed Device Generalization

- Windows Service.
- systemd and launchd support.
- Restart/recovery.
- Auto-update/rollback.
- Claude Code and ACP adapters.
- Artifact streaming.
- Durable hosted storage.
- WSS transport with HTTPS polling fallback.

Exit gate: one gateway manages multiple Mac/Windows/Linux hosts, trust rotation reaches managed hosts, audit/artifact spools survive reconnect, and a new adapter can be added without bypassing policy.

## v0.5.0 Mesh and GUI

- Tailscale/headscale adapter.
- RustDesk/MeshCentral optional GUI adapters.
- GUI consent/audit.
- Browser E2E adapter.

## v1.0.0

- Stable MCP contract.
- Stable host enrollment protocol.
- Stable `rdev skillkit export` package for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.
- Full audit trail.
- Signed releases.
- Installation docs for Hermes, Codex, Claude Code, and OpenCode.
- End-to-end acceptance demos for temporary Windows repair and managed coding.

Exit gate: external users can self-host or install the Skillkit without Hermes-specific assumptions, release artifacts verify, and the public threat model/security policy match the shipped behavior.
