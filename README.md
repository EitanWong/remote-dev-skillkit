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
- Persistent development gateway signing key files plus host trust pin checks.
- File-backed host identity key store with registration fingerprint preservation and signed job identity binding.
- Host-side nonce replay cache with in-memory and file-backed development stores.
- Hash-chained audit export and verification via `rdev audit export` / `rdev audit verify`.
- Local evidence bundle export via `rdev evidence export`.
- Gateway-backed evidence bundle export from a job id via `rdev evidence export --gateway ... --job-id ...`.
- Skillkit bundle export via `rdev skillkit export` for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.
- Structured host-side denial artifacts via `rdev.host-denial.v1` for missing envelopes, wrong host, identity mismatch, expired/tampered/replayed envelopes, unsupported adapters, missing capabilities, missing workspaces, non-allowlisted commands, and workspace escapes.
- Structured host-side approval-required artifacts via `rdev.approval-required.v1`; jobs with unsatisfied signed approval requirements pause before adapter execution, and gateway-approved jobs receive signed `rdev.approval-token.v1` tokens.
- Durable host-side approval token consumption stores with in-memory and file-backed development modes, exposed through `rdev host serve --approval-store`.
- Signed development join manifest endpoint for manifest-driven temporary host registration.
- Join manifests can be signed by a separate bootstrap/release trust root and verified by hosts with a pinned root public key.
- Release artifact signing and verification primitives via `rdev release sign` / `rdev release verify`.
- Windows bootstrap can hash-pin `rdev-verify.exe` and use it to verify the signed `rdev-host.exe` release manifest before starting the host.
- Host-reported failed jobs with audit events.
- Real development scoped shell adapter execution with allowlisted argv, workspace checks, timeouts, output caps, schema-versioned redacted evidence, and failure artifacts.
- Foreground `rdev host serve --mode temporary` placeholder.
- Agent Skills drafts.

Not implemented yet:

- Real gateway networking.
- Production host enrollment certificates and registration proofs.
- Production signing key storage and rotation.
- Production key rotation/revocation and managed host trust bundle update flow.
- Full production bootstrap trust root lifecycle and release signing policy.
- Platform-native code signing / Authenticode policy for Windows releases.
- Production WSS host transport.
- Production-grade shell adapter hardening beyond the development scoped executor.
- OS-protected managed host identity and trust storage beyond file-backed dev mode.
- Artifact streaming.
- Windows service installation.
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
go run ./cmd/rdev release sign --artifact ./rdev-host.exe --key .rdev/keys/release-root.json
go run ./cmd/rdev-verify --artifact ./rdev-host.exe --manifest ./rdev-host.exe.rdev-release.json --root-public-key release-root:...
go run ./cmd/rdev host serve --mode temporary
go run ./cmd/rdev host serve --mode temporary --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234 --once=false --transport long-poll
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
- [Perfect End-State Architecture](docs/architecture/PERFECT_END_STATE.md)
- [Final System Design](docs/architecture/FINAL_SYSTEM_DESIGN.md) — canonical product contract
- [Final Architecture](docs/architecture/FINAL_ARCHITECTURE.md)
- [Project Plan](docs/project/PLAN.md)
- [Acceptance Tests](docs/project/ACCEPTANCE_TESTS.md)
- [Roadmap](docs/project/ROADMAP.md)
- [Versioning](docs/project/VERSIONING.md)
- [Threat Model](docs/security/THREAT_MODEL.md)
- [Release Key Lifecycle](docs/security/RELEASE_KEY_LIFECYCLE.md)
- [Bootstrap Design](docs/operations/BOOTSTRAP.md)
- [Development Gateway](docs/operations/DEV_GATEWAY.md)
- [MCP Stdio](docs/operations/MCP_STDIO.md)
- [Skillkit Install](docs/operations/SKILLKIT_INSTALL.md)
- [MCP Tools](mcp/tools.json)

## Sources

This project follows the Agent Skills progressive disclosure model and MCP tool exposure model:

- https://agentskills.io/specification
- https://modelcontextprotocol.io/specification/2025-11-25
- https://modelcontextprotocol.io/specification/draft/server/tools
