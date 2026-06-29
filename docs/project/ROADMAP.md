# Roadmap

The roadmap implements the canonical [Perfect End-State Architecture](../architecture/PERFECT_END_STATE.md): consent-first enrollment, outbound-only transport, signed job envelopes, host-side policy enforcement, approval gates, audit evidence, and agent-native MCP/Skill packaging.

## v0.0.1 Foundation

- Repository skeleton.
- CLI skeleton.
- Doctor command.
- MCP tool contract listing.
- Agent Skills drafts.
- Architecture and security docs.

## v0.1.0 Local Demo

- Local gateway process.
- Local host process.
- In-memory tickets, hosts, jobs, artifacts, and audit events.
- MCP stdio server for tool calls.
- Development signed job envelopes.
- Demonstrable local temporary session.
- Scoped shell adapter with workspace boundary, allowlisted argv, timeouts, output caps, and failure reporting.
- Host identity storage wired into registration and job binding.
- Durable host trust bundle store.
- Trust-bundle key rotation/revocation flow.
- Host-side artifact redaction.
- Hash-chained audit export verifier.
- Evidence bundle export.

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

## v0.3.0 Managed Mac Coding

- macOS LaunchAgent managed mode.
- Host identity protected storage.
- Workspace locks and Git worktrees.
- Codex adapter.
- Git diff and test evidence bundles.
- Approval before push, merge, deploy, credential changes, or service changes.
- Managed install, health, stop, and uninstall commands.

## v0.4.0 Managed Device Generalization

- Windows Service.
- systemd and launchd support.
- Restart/recovery.
- Auto-update/rollback.
- Claude Code and ACP adapters.
- Artifact streaming.
- Durable hosted storage.
- WSS transport with HTTPS polling fallback.

## v0.5.0 Mesh and GUI

- Tailscale/headscale adapter.
- RustDesk/MeshCentral optional GUI adapters.
- GUI consent/audit.
- Browser E2E adapter.

## v1.0.0

- Stable MCP contract.
- Stable host enrollment protocol.
- Full audit trail.
- Signed releases.
- Installation docs for Hermes, Codex, Claude Code, and OpenCode.
- End-to-end acceptance demos for temporary Windows repair and managed coding.
