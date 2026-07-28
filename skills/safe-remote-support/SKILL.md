---
name: safe-remote-support
description: Use when an agent needs to connect to, operate, or audit a remote machine through the current Remote Dev Skillkit session Control Plane.
---

# Safe Remote Support

## Before a session

1. Read `rdev doctor`, `rdev mcp tools`, and `rdev host serve --help`.
2. Resolve the active gateway from operator configuration or current MCP output.
3. Confirm the requested task, workspace boundary, and required capability.

## Session flow

1. Create the session through MCP.
2. For a configured managed HTTPS gateway, create `rdev.sessions.handoff` and
   send its short-lived browser URL to the Windows operator. Otherwise give the
   host only its current join code and gateway URL.
3. Confirm endpoint readiness with session status and events.
4. Submit a narrow task.
5. Inspect events and artifact metadata before saying work is complete.
6. Interrupt or close only on an explicit operator decision.

## Network and trust

- `rdev gateway serve --dev` is loopback-only development infrastructure.
- Use an operator-managed HTTPS gateway for a remote host.
- A managed pilot may use `--operator-auth-file PATH`; it loads hashed
  principals while the MCP bearer token stays in a separate protected local
  file. Gateway restart ends active pilot sessions.
- Host trust is verified through the signed trust-bundle endpoint.
- Use `rdev mcp serve --gateway-url URL --operator-token-file PATH` for a configured remote MCP proxy; the token remains only in the protected local file.
- A web handoff URL carries an opaque fragment proof only; the browser claims it
  once, receives a PowerShell bootstrap, and downloads the hash-checked host
  binary with a short-lived header ticket. It is not a host listener or a
  substitute for session policy.

## Adaptive configuration

Before acting, probe the current gateway, network reachability, tunnel and mesh
availability, workspace, and host environment. If a required value is unclear,
ask a concise question; never invent a configuration value.

## Hard boundaries

- Do not expose a host through a public inbound listener.
- Do not add hidden persistence or weaken local security controls.
- Do not substitute hand-written remote shell glue for scoped session tasks.
- Do not report success without terminal state, evidence, and requested verification.
