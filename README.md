# Remote Dev Skillkit

Remote Dev Skillkit is a small, session-native control plane for agent-assisted development on Mac, Windows, and Linux hosts. Agents create scoped work through MCP; hosts join with a short-lived session join code, enforce local policy, and return events and artifacts.

## Current surface

- `rdev doctor`
- `rdev mcp tools`
- `rdev mcp serve`
- `rdev host serve --join-code CODE --gateway URL`
- `rdev gateway serve --dev` for loopback development
- Session MCP tools: `create`, `status`, `events`, `task`, `interrupt`, `artifacts`, and `close`
- Host directory MCP tools: `rdev.hosts.list` and `rdev.hosts.rename`

## Quick start

```bash
go test ./...
go run ./cmd/rdev doctor
go run ./cmd/rdev mcp tools
```

For local development, start a loopback gateway:

```bash
go run ./cmd/rdev gateway serve --dev
```

A host joins a current Control Plane endpoint with values created through MCP:

```bash
rdev host serve --join-code CODE --gateway https://gateway.example --once
```

The built-in gateway command listens on loopback only. Remote deployments require an operator-managed HTTPS endpoint; keep bearer material in a protected local file when starting remote MCP proxy mode.

## Safety boundaries

- Hosts do not expose inbound public listeners.
- Every task is policy-bound, scoped, auditable, and interruptible.
- Temporary work remains foreground and non-persistent.
- The toolkit does not bypass local security controls or provide unrestricted shell access.

## Verification

```bash
./scripts/check.sh
```

See [documentation](docs/README.md), [security boundaries](docs/security/BOUNDARIES.md), and [contributing](CONTRIBUTING.md).
