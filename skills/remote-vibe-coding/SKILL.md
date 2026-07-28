---
name: remote-vibe-coding
description: Use when an agent needs to delegate coding, repair, setup, or development work to a Remote Dev Skillkit session host through the current MCP Control Plane.
---

# Remote Vibe Coding

## When to use

Use for scoped engineering work on a host that has joined the active Control Plane.

## Workflow

1. Discover the real surface with `rdev doctor` and `rdev mcp tools`.
2. Create a session through MCP and retain its session identifier, endpoint state, and gateway source.
3. Start the host with the returned join code and current gateway URL.
4. Wait for an endpoint through `rdev.sessions.status` and `rdev.sessions.events`.
5. Submit the smallest task that states adapter, workspace, capability, limits, and expected verification.
6. Inspect terminal state, events, artifacts, changed files, and test output before reporting completion.

## Boundaries

- Resolve values from current MCP/CLI output; do not invent gateway URLs, join codes, workspace paths, or capabilities.
- Keep agent work inside declared workspace and host policy.
- Use `interrupt` for an in-flight task and `close` only when the operator asks.
- Do not create persistence, public target-host listeners, or unrestricted execution paths.

## Adaptive configuration

Before acting, probe the current gateway, network reachability, tunnel and mesh
availability, workspace, and host environment. If a required value is unclear,
ask a concise question; never invent a configuration value.

## Verification

A task is complete only when its terminal state, relevant events, artifacts, and requested checks agree.
