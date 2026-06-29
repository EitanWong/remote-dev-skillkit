# Remote Dev Skillkit

Agent-native remote development tools for safely delegating coding and repair work to enrolled Mac, Windows, and Linux hosts.

This project is the implementation home for the `rdev` toolchain:

- `rdev`: operator CLI and local debugging surface.
- `rdev-host`: target-machine agent for temporary or managed hosts.
- `rdev-gateway`: Hermes-side control plane for tickets, host registry, jobs, artifacts, and audit.
- `rdev-mcp`: MCP tools exposed to Hermes/Lucky, Codex, Claude Code, OpenCode, and other agents.
- `skills/`: Agent Skills that teach agents how to use the remote development workflow safely.

## Core Promise

Remote Dev Skillkit is not a hidden remote-control tool. It is consent-first infrastructure for visible, auditable, policy-bound remote support and remote coding.

Temporary third-party machines use foreground, time-limited support sessions. Long-lived unattended service mode is reserved for Eitan-owned or formally managed devices.

## Final Shape

The finished project is a universal safety layer for agents working on real machines:

```text
Agent Skills + MCP tools
        |
        v
rdev-gateway: tickets, hosts, jobs, approvals, artifacts, audit
        |
        v
outbound HTTPS/WSS host channels
        |
        v
rdev-host: identity, trust bundle, local policy, adapters
        |
        v
shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, mesh, Coder, DevPod
```

The project intentionally reuses mature ecosystems where they fit: MCP for agent tools, Tailscale/headscale or SSH for owned-host connectivity, Coder/DevPod for governed workspaces, RustDesk/MeshCentral for explicit GUI sessions, and platform/Sigstore-style signing for release trust. What `rdev` owns is the missing agent safety kernel: signed job envelopes, host-side policy, approval gates, workspace locks, redaction, evidence bundles, audit chains, and revocation.

The canonical endgame is locked in [Perfect Ending Solution](docs/architecture/PERFECT_ENDING_SOLUTION.md): the final architecture lock, subsystem blueprint, authority map, protocol objects, operating modes, permission model, discovery model, evidence gates, and implementation order. The design intentionally separates temporary attended repair from explicit managed service mode, and it treats Codex, Claude Code, ACP, GUI, mesh, Coder, DevPod, shell, and PowerShell as adapters behind the same signed-job, evidence, approval, and revocation contract.

[Final Closure Blueprint](docs/architecture/FINAL_CLOSURE_BLUEPRINT.md) is the concise release-facing summary. [Ultimate Closure Design](docs/architecture/ULTIMATE_CLOSURE_DESIGN.md) and [Final System Design](docs/architecture/FINAL_SYSTEM_DESIGN.md) remain supporting rationale and implementation detail.

## Current Status

This repository is in Phase 1: project foundation and safe MVP.

Implemented now:

- Project plan, architecture, security model, and versioning docs.
- Initial `rdev` CLI.
- `rdev doctor` capability detection.
- `rdev ticket create` local ticket preview.
- `rdev policy explain` local policy simulation.
- `rdev policy explain-shell` shell job policy preflight explanation.
- `rdev mcp tools` tool-contract listing.
- `rdev mcp serve` minimal MCP stdio server for initialize, tools/list, and tools/call.
- `rdev gateway serve --dev` local HTTP development gateway.
- `rdev demo local` in-memory ticket, host approval, job, artifact, and audit flow.
- Development signed job envelopes using Ed25519 in-memory gateway keys.
- Local dev host registration, job polling, and job completion loop.
- Development HTTPS long-poll host job transport via `rdev host serve --transport long-poll`.
- Development trust bundle endpoint for host-side envelope signature verification.
- Host-bound trust bundle update checks for managed host trust-store refresh.
- macOS LaunchAgent plist generation, status inspection, and safe plist removal via `rdev host install-service`, `rdev host service-status`, and `rdev host uninstall-service`.
- macOS LaunchAgent dry-run and opt-in lifecycle control via `rdev host service-control --action start|inspect|stop`, with `--execute` required before running `launchctl`.
- Persistent development gateway signing key files plus host trust pin checks.
- File-backed host identity key store with registration fingerprint preservation and signed job identity binding.
- Host-side nonce replay cache with in-memory and file-backed development stores.
- Hash-chained audit export and verification via `rdev audit export` / `rdev audit verify`.
- Local evidence bundle export via `rdev evidence export`.
- Gateway-backed evidence bundle export from a job id via `rdev evidence export --gateway ... --job-id ...`.
- Skillkit bundle export and verification via `rdev skillkit export` / `rdev skillkit verify` for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.
- Managed Mac coding acceptance harness via `rdev acceptance managed-mac`, producing a report, locked-worktree Codex evidence bundle, and approval-gate evidence bundle.
- Acceptance report verification via `rdev acceptance verify --report ...`, including evidence manifest checksums, artifact index validation, audit-chain verification, approval-gate evidence, and workspace-lock release checks.
- Managed Mac LaunchAgent acceptance planning via `rdev acceptance managed-mac-service`, producing a checked plist, service plan, launchctl commands, service-backed acceptance commands, and uninstall steps without auto-starting launchd.
- Windows temporary acceptance planning and verification via `rdev acceptance windows-temporary` and `rdev acceptance verify-windows-temporary`, producing and checking a machine-readable plan, reviewed PowerShell launcher, signed release manifest or bundle verification requirements, approval probes, no-persistence inspection commands, and required evidence checklist without executing PowerShell.
- Windows temporary acceptance evidence packaging via `rdev acceptance package-windows-temporary`, collecting a verified plan, launcher, transcript, release verifier output, audit, approval probes, and no-persistence evidence into a redacted checksummed package.
- Workspace lock and Git worktree foundation via `rdev workspace lock`, `rdev workspace status`, `rdev workspace unlock`, and `rdev workspace prepare-worktree`.
- Host job execution can enforce one-writer workspace locks through `rdev host serve --workspace-lock-store`.
- Codex adapter MVP through `adapter=codex`: runs `codex exec` or a signed payload-provided command inside the validated workspace, requires `codex.run` and `git.diff`, gates push/merge/deploy/publish/credential/service intents on approval, and captures `rdev.codex-result.v1` evidence with Codex output, Git status, Git diff/stat, optional verification command results, `go test -json` test reports, output caps, and redaction.
- Codex adapter conformance coverage for canonical workspace roots, write-scope escape rejection before execution, failure evidence, redaction, output truncation, and timeout cancellation evidence.
- Codex adapter cooperative cancellation through context-aware hostrunner execution and host-side gateway job status monitoring.
- Canceled Codex jobs can append cancellation evidence artifacts while preserving the gateway job's `canceled` terminal state.
- Structured host-side denial artifacts via `rdev.host-denial.v1` for missing envelopes, wrong host, identity mismatch, expired/tampered/replayed envelopes, unsupported adapters, missing capabilities, missing workspaces, non-allowlisted commands, and workspace escapes.
- Structured host-side approval-required artifacts via `rdev.approval-required.v1`; jobs with unsatisfied signed approval requirements pause before adapter execution, and gateway-approved jobs receive signed `rdev.approval-token.v1` tokens.
- Built-in shell and Codex jobs run an implicit approval preflight before adapter execution for package installation, elevation, GUI control, service management, push, merge, deploy, publish, and credential changes.
- Durable host-side approval token consumption stores with in-memory and file-backed development modes, exposed through `rdev host serve --approval-store`.
- Signed development join manifest endpoint for manifest-driven temporary host registration.
- Join manifests can be signed by a separate bootstrap/release trust root and verified by hosts with a pinned root public key.
- Release artifact signing and verification primitives via `rdev release sign` / `rdev release verify`.
- Signed release bundle indexes via `rdev release create-bundle` / `rdev release verify-bundle`, checking the signed index, every artifact manifest, artifact and manifest SHA-256/size, and required artifact presence before publishing.
- The standalone `rdev-verify` helper can verify either a single signed artifact manifest or a full signed release bundle before host execution.
- Release candidate packaging via `rdev release prepare-candidate`, staging built artifacts, signed manifests, a signed release bundle, a verified Skillkit bundle, checksums, and `release-candidate.json`.
- Release candidate verification via `rdev release verify-candidate`, checking a staged or downloaded candidate's summary, checksums, signed bundle, manifests, artifacts, Skillkit bundle, required artifacts, and unlisted files.
- Windows bootstrap can hash-pin `rdev-verify.exe` and use it to verify either the signed `rdev-host.exe` release manifest or the signed release bundle before starting the host.
- Host-reported failed jobs with audit events.
- Real development scoped shell adapter execution with allowlisted argv, workspace checks, timeouts, output caps, schema-versioned redacted evidence, and failure artifacts.
- Foreground `rdev host serve --mode temporary` placeholder.
- Agent Skills drafts.

Not implemented yet:

- Real gateway networking.
- Production host enrollment certificates and registration proofs.
- Production signing key storage and rotation.
- Production key rotation/revocation authentication and durable managed host trust lifecycle.
- Full production bootstrap trust root lifecycle and release signing policy.
- Platform-native code signing / Authenticode policy for Windows releases.
- Production WSS host transport.
- Production-grade shell adapter hardening beyond the development scoped executor.
- OS-protected managed host identity and trust storage beyond file-backed dev mode.
- Artifact streaming.
- Windows service installation.
- launchctl start/stop execution and systemd service lifecycle commands.
- Tailscale/headscale adapter.
- GUI adapter.

## Quick Start

```bash
go test ./...
go run ./cmd/rdev version
go run ./cmd/rdev doctor
go run ./cmd/rdev ticket create --ttl-seconds 7200 --reason "repair Windows dev environment"
go run ./cmd/rdev policy explain --mode attended-temporary --capability shell.user
go run ./cmd/rdev policy explain-shell --policy-json '{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}'
go run ./cmd/rdev demo local
go run ./cmd/rdev mcp tools
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}' | go run ./cmd/rdev mcp serve
go run ./cmd/rdev gateway serve --dev --addr 127.0.0.1:8787
go run ./cmd/rdev audit export --input .rdev/audit/events.jsonl --out .rdev/audit/audit-chain.json
go run ./cmd/rdev audit verify --input .rdev/audit/audit-chain.json
go run ./cmd/rdev evidence export --job-json job.json --artifacts-json artifacts.json --audit-jsonl .rdev/audit/events.jsonl --out job_evidence
go run ./cmd/rdev evidence export --gateway http://127.0.0.1:8787 --job-id job_... --out job_evidence
go run ./cmd/rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
go run ./cmd/rdev skillkit verify --bundle dist/remote-dev-skillkit
go run ./cmd/rdev acceptance managed-mac --out .rdev/acceptance/managed-mac --repo .
go run ./cmd/rdev acceptance managed-mac-service --out .rdev/acceptance/managed-mac-service --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --repo .
go run ./cmd/rdev acceptance windows-temporary --out .rdev/acceptance/windows-temporary --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --download-url https://agent.example.com/rdev-host.exe --expected-sha256 <sha256> --release-bundle-url https://agent.example.com/release-bundle.json --release-root-public-key release-root:... --verifier-download-url https://agent.example.com/rdev-verify.exe --verifier-sha256 <sha256>
go run ./cmd/rdev acceptance verify-windows-temporary --plan .rdev/acceptance/windows-temporary/windows-temporary-plan.json
go run ./cmd/rdev acceptance package-windows-temporary --plan .rdev/acceptance/windows-temporary/windows-temporary-plan.json --out .rdev/acceptance/windows-temporary-evidence --transcript transcript.txt --release-verification rdev-verify.json --audit audit.jsonl --no-persistence-dir no-persistence --approval-probes-dir approval-probes
go run ./cmd/rdev acceptance verify --report .rdev/acceptance/managed-mac/report.json
go run ./cmd/rdev workspace lock --repo . --host-id hst_... --job-id job_... --adapter codex
go run ./cmd/rdev workspace status --repo .
go run ./cmd/rdev workspace unlock --repo . --job-id job_...
go run ./cmd/rdev workspace prepare-worktree --repo . --host-id hst_... --job-id job_... --adapter codex
curl -s -X POST http://127.0.0.1:8787/v1/jobs -H 'content-type: application/json' -d '{"host_id":"hst_...","adapter":"codex","intent":"update README","policy":{"workspace_root":".","capabilities":["codex.run","git.diff"],"prompt":"Update README and run checks.","verification_commands":[["git","status","--short"]],"allow_verification_commands":["git"],"max_duration_seconds":1800,"max_output_bytes":1048576}}'
go run ./cmd/rdev release sign --artifact ./rdev-host.exe --key .rdev/keys/release-root.json
go run ./cmd/rdev-verify --artifact ./rdev-host.exe --manifest ./rdev-host.exe.rdev-release.json --root-public-key release-root:...
go run ./cmd/rdev release create-bundle --dir dist --artifacts rdev,rdev-host.exe,rdev-verify.exe --require-artifacts rdev-host.exe,rdev-verify.exe --key .rdev/keys/release-root.json
go run ./cmd/rdev release verify-bundle --bundle dist/release-bundle.json --root-public-key release-root:...
go run ./cmd/rdev-verify --bundle dist/release-bundle.json --root-public-key release-root:... --require-artifacts rdev-host.exe,rdev-verify.exe
go run ./cmd/rdev release prepare-candidate --source-root . --out dist/release-candidate --version v0.1.0 --artifacts ./rdev,./rdev-host.exe,./rdev-verify.exe --require-artifacts rdev-host.exe,rdev-verify.exe --key .rdev/keys/release-root.json --gateway-url https://api.example.com/v1
go run ./cmd/rdev release verify-candidate --candidate dist/release-candidate --require-artifacts rdev-host.exe,rdev-verify.exe
go run ./cmd/rdev host serve --mode temporary
go run ./cmd/rdev host serve --mode temporary --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234 --once=false --transport long-poll --workspace-lock-store .rdev/workspace-locks
go run ./cmd/rdev host install-service --platform macos --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --workspace-lock-store ~/.rdev/host/workspace-locks --plist-out ./com.remote-dev-skillkit.host.plist
go run ./cmd/rdev host service-status --platform macos --plist ./com.remote-dev-skillkit.host.plist
go run ./cmd/rdev host service-control --platform macos --action start --plist ./com.remote-dev-skillkit.host.plist
go run ./cmd/rdev host uninstall-service --platform macos --plist ./com.remote-dev-skillkit.host.plist
```

## Design Invariants

- No hidden persistence.
- No UAC bypass or silent privilege escalation.
- No inbound ports on temporary target hosts.
- No raw unrestricted shell for agents.
- Every future remote job must be signed, policy-checked, auditable, and revocable.
- Destructive actions and high-risk capabilities require explicit approval gates.

## Documentation

- [Architecture](docs/architecture/ARCHITECTURE.md)
- [Perfect Ending Solution](docs/architecture/PERFECT_ENDING_SOLUTION.md) — canonical final architecture lock and execution spec
- [Final Closure Blueprint](docs/architecture/FINAL_CLOSURE_BLUEPRINT.md) — concise release-facing closure summary
- [Ultimate Closure Design](docs/architecture/ULTIMATE_CLOSURE_DESIGN.md) — supporting implementation detail and rationale
- [Final System Design](docs/architecture/FINAL_SYSTEM_DESIGN.md) — broad product reasoning record
- [Perfect End-State Architecture](docs/architecture/PERFECT_END_STATE.md)
- [Final Architecture](docs/architecture/FINAL_ARCHITECTURE.md)
- [Project Plan](docs/project/PLAN.md)
- [Acceptance Tests](docs/project/ACCEPTANCE_TESTS.md)
- [GitHub Project Management](docs/project/GITHUB_PROJECT_MANAGEMENT.md)
- [Release Checklist](docs/project/RELEASE_CHECKLIST.md)
- [Roadmap](docs/project/ROADMAP.md)
- [Versioning](docs/project/VERSIONING.md)
- [Threat Model](docs/security/THREAT_MODEL.md)
- [Release Key Lifecycle](docs/security/RELEASE_KEY_LIFECYCLE.md)
- [Bootstrap Design](docs/operations/BOOTSTRAP.md)
- [Acceptance Operations](docs/operations/ACCEPTANCE.md)
- [Development Gateway](docs/operations/DEV_GATEWAY.md)
- [MCP Stdio](docs/operations/MCP_STDIO.md)
- [Skillkit Install](docs/operations/SKILLKIT_INSTALL.md)
- [MCP Tools](mcp/tools.json)

## Sources

This project follows the Agent Skills progressive disclosure model and MCP tool exposure model:

- https://agentskills.io/specification
- https://modelcontextprotocol.io/specification/2025-11-25
- https://modelcontextprotocol.io/specification/draft/server/tools
