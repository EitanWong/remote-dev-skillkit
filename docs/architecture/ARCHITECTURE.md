# Architecture

## Product Shape

Remote Dev Skillkit has four surfaces:

- Agent Skills: instruct agents how to request and review remote jobs safely.
- CLI: install, inspect, enroll, debug, and operate the system.
- MCP tools: expose host, ticket, job, artifact, audit, and policy actions.
- Host/Gateway services: connect target machines to Hermes/Lucky.

## Connectivity Layers

### Layer 0: Relay Mode

Default for temporary third-party machines.

`rdev-host` opens an outbound HTTPS/WebSocket/mTLS connection to the gateway on port 443. The target host does not listen on public inbound ports.

### Layer 1: Mesh Mode

Optional for managed devices.

Tailscale/headscale can provide private connectivity with ephemeral/tagged keys and ACLs. The mesh is a transport optimization, not the core authorization model.

### Layer 2: GUI Adapter Mode

Optional remote desktop support through RustDesk or MeshCentral. GUI control is a separate capability with explicit consent and audit.

## Control Flow

```text
Agent -> MCP tool -> rdev-gateway -> signed job -> rdev-host -> adapter -> evidence
```

## Host Modes

- `attended-temporary`: visible, foreground, TTL-bound, third-party default.
- `managed`: service mode for owned or formally managed devices.
- `break-glass`: short TTL and strict approval for incident repair.

## Trust Model

- Tickets are one-time and short-lived.
- Hosts generate per-device keypairs.
- Gateway signs jobs.
- Host verifies job signatures and local policy.
- Audit is written on server and host.
