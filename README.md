# Remote Dev Skillkit

[中文](README.zh-CN.md)

**Agent-native remote development.** Let Claude Code, Codex, Hermes, OpenCode, or any MCP-capable agent work on your Mac, Windows, and Linux machines — scoped, audited, interruptible.

```text
Agent (MCP client) ── rdev mcp serve ──> Control Plane ── long-poll ──> Host
                                          (rdev gateway)             (rdev host serve)
```

## Why

Agents have models and code ability — but no machine of their own, and you shouldn't hand them the keys to everything. Remote Dev Skillkit is the controlled channel between them.

- **Scoped sessions** — hosts join with short-lived join codes; every task is bounded by policy and capability ceilings.
- **Audited & interruptible** — every action is recorded and can be interrupted or revoked at any time.
- **Managed hosts** — Windows hosts install as a service via a browser handoff (copy-paste PowerShell), auto-reconnect, control-plane host updates.
- **Event-driven** — agents get push events (webhooks) instead of polling for host status, task results, and artifacts.
- **No exposure** — outbound-only connections, no inbound ports, no hidden persistence, no bypassing UAC, TCC, Gatekeeper, or Defender.

## Quick start

**Host side** — let an agent use this machine:

```bash
go install github.com/EitanWong/remote-dev-skillkit/cmd/rdev@latest
rdev host serve --join-code CODE --gateway https://your-gateway
```

**Agent side** — connect to hosts through the control plane:

```bash
rdev mcp serve --gateway-url URL --operator-token-file PATH
```

Register `rdev mcp serve` in your MCP client. Tools are self-describing with safety notes and agent guidance.

**Gateway operator** — run a multi-host control plane:

```bash
rdev gateway serve --dev                                    # local trial
rdev gateway serve --operator-auth-file ops.token --state-file state.json \
    --signing-key-file key.pem --public-base-url https://gw.example
```

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/EitanWong/remote-dev-skillkit/main/scripts/install.sh | bash
```

Manual install requires Go 1.25+. Windows target hosts need no manual download — the browser handoff fetches and verifies the host binary.

## Security model

- Outbound-only connections; no inbound public ports.
- Policy-bound, scoped, auditable, interruptible tasks; temporary sessions are non-persistent by default.
- Never bypasses local security controls (UAC, sudo, TCC, Gatekeeper, Windows Defender).

## Documentation

- Architecture: [SESSION_CONTROL_PLANE.md](docs/architecture/SESSION_CONTROL_PLANE.md)
- Safety boundaries: [BOUNDARIES.md](docs/security/BOUNDARIES.md)
- Quality matrix (live E2E status): [QUALITY_MATRIX.md](docs/development/QUALITY_MATRIX.md)
- Host update runbook: [UPDATE_RUNBOOK.md](docs/operations/UPDATE_RUNBOOK.md)
- Quality gate: `./scripts/check.sh`

## License

MIT — see [LICENSE](LICENSE).
