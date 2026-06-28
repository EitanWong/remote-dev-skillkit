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

## Current Status

This repository is in Phase 1: project foundation and safe MVP.

Implemented now:

- Project plan, architecture, security model, and versioning docs.
- Initial `rdev` CLI.
- `rdev doctor` capability detection.
- `rdev ticket create` local ticket preview.
- `rdev policy explain` local policy simulation.
- `rdev mcp tools` tool-contract listing.
- `rdev demo local` in-memory ticket, host approval, job, artifact, and audit flow.
- Foreground `rdev host serve --mode temporary` placeholder.
- Agent Skills drafts.

Not implemented yet:

- Real gateway networking.
- Host enrollment keys and certificates.
- Signed job execution.
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
go run ./cmd/rdev demo local
go run ./cmd/rdev mcp tools
go run ./cmd/rdev host serve --mode temporary
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
- [Project Plan](docs/project/PLAN.md)
- [Roadmap](docs/project/ROADMAP.md)
- [Versioning](docs/project/VERSIONING.md)
- [Threat Model](docs/security/THREAT_MODEL.md)
- [Bootstrap Design](docs/operations/BOOTSTRAP.md)
- [MCP Tools](mcp/tools.json)

## Sources

This project follows the Agent Skills progressive disclosure model and MCP tool exposure model:

- https://agentskills.io/specification
- https://modelcontextprotocol.io/specification/2025-11-25
- https://modelcontextprotocol.io/specification/draft/server/tools
