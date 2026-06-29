# Roadmap

The roadmap implements the canonical [Perfect Ending Solution](../architecture/PERFECT_ENDING_SOLUTION.md): consent-first enrollment, outbound-only transport, signed job envelopes, host-side policy enforcement, workspace locks, approval gates, audit evidence, release-bundle verification, and agent-native MCP/Skill packaging. That file is the final architecture lock for the safety microkernel and replaceable adapters for shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, mesh, Coder, DevPod, and future runtimes. [Final Closure Blueprint](../architecture/FINAL_CLOSURE_BLUEPRINT.md) is the concise release-facing summary, while [Ultimate Closure Design](../architecture/ULTIMATE_CLOSURE_DESIGN.md), [Final System Design](../architecture/FINAL_SYSTEM_DESIGN.md), and [Perfect End-State Architecture](../architecture/PERFECT_END_STATE.md) remain supporting context.

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
- Skillkit bundle verification for required skills, required framework notes, file checksums, safe paths, and unlisted-file detection.
- Release candidate packaging that stages built artifacts, signed manifests, signed release bundle, verified Skillkit bundle, checksums, and `release-candidate.json`.
- Release candidate verification that checks staged or downloaded candidates before publication or installation.
- Real build artifact generation through `scripts/release/build-artifacts.sh`, producing target-specific binaries, `rdev.build-artifacts.v1`, and checksums before candidate packaging.
- Per-platform release candidate automation through `scripts/release/prepare-platform-candidates.sh`, grouping real build artifacts by `GOOS/GOARCH`, producing one verified candidate per target, and writing `rdev.platform-release-candidates.v1`.
- Strong symlink/workspace escape regression tests.
- Explainable denial and approval decisions.

Exit gate: local demo proves ticket, host registration, outbound host job wait through short-poll or long-poll, signed job execution, artifact storage, evidence export, Skillkit export/verify, release candidate package generation and verification, audit export, approval-token consumption, and host-side rejection of tampered, expired, wrong-host, replayed, non-allowlisted, and workspace-escaping jobs.

## v0.2.0 Windows Temporary Host

- Signed Windows binary.
- PowerShell bootstrap.
- Windows temporary acceptance planning and verification through `rdev acceptance windows-temporary` and `rdev acceptance verify-windows-temporary`, including reviewed launcher generation, signed release manifest or release bundle verification requirements, launcher safety checks, approval probes, no-persistence inspection commands, and required evidence checklist without executing PowerShell.
- Windows temporary acceptance evidence packaging through `rdev acceptance package-windows-temporary`, turning real clean-VM transcripts, release verifier output, audit, approval probes, and no-persistence checks into a redacted checksummed release artifact.
- Visible foreground support window or console UI.
- Outbound-only connection to gateway.
- Durable signing key storage and rotation.
- Shell and file scoped jobs.
- Host local audit spool and revocation handling.
- Authenticode verification policy.
- Signed release bundle index creation and verification through `rdev release create-bundle` and `rdev release verify-bundle`.
- No-persistence inspection script.
- Local-user approval prompt for elevation, GUI, service, and destructive requests.
- Clean Windows acceptance run.

Exit gate: clean Windows 10/11 VM joins from one visible command, verifies signed artifacts before host execution, connects outbound only, enforces approvals, revokes cleanly, leaves no service or autorun persistence, and exports a passing Windows temporary acceptance package.

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
- Managed Mac coding acceptance harness through `rdev acceptance managed-mac`, producing a managed-mode report, locked-worktree Codex evidence bundle, and approval-gate evidence bundle.
- Acceptance report verification through `rdev acceptance verify`, including evidence bundle checksum validation, artifact index validation, audit-chain verification, approval-gate checks, and workspace-lock release checks.
- Managed Mac LaunchAgent acceptance planning through `rdev acceptance managed-mac-service`, producing a verified plist, launchctl start/inspect/stop commands, service-backed coding acceptance commands, verification command, and safe uninstall guidance without auto-starting launchd.
- macOS LaunchAgent lifecycle control through `rdev host service-control`, dry-running by default and requiring `--execute` before invoking launchctl start, inspect, or stop.
- Git diff and test evidence bundles.
- Shared implicit approval preflight before package install, elevation, GUI control, service changes, push, merge, deploy, publish, or credential changes for built-in shell and Codex jobs.
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
- Stable `rdev skillkit export` and `rdev skillkit verify` package for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.
- Full audit trail.
- Signed releases.
- Local release candidate packaging before GitHub Release publication.
- Local GitHub Release dry-run planning from verified release candidates.
- GitHub Actions CI for tests, shell syntax, real build artifact smoke, per-platform release candidate verification, and release-plan smoke.
- Installation docs for Hermes, Codex, Claude Code, and OpenCode.
- End-to-end acceptance demos for temporary Windows repair and managed coding.

Exit gate: external users can self-host or install a verified Skillkit without Hermes-specific assumptions, release artifacts verify, and the public threat model/security policy match the shipped behavior.
