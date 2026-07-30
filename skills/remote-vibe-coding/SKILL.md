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
3. For a configured managed HTTPS gateway, create `rdev.sessions.handoff` and
   send its returned browser URL to the intended target. This is the single
   delivery form: do not select a web/PowerShell mode or claim the link on the
   operator's behalf. The page localizes and detects its opened system. On
   Windows it directly shows numbered copy/paste steps and the command. The
   target user runs the copied command, approves visible UAC, and receives a
   hash-verified auto-start Windows service without a manual launcher download.
   Otherwise start the host with the returned join code and current gateway URL.
4. Wait for an endpoint through `rdev.sessions.status` and `rdev.sessions.events`.
5. Submit the smallest task that states adapter, workspace, capability, limits, and expected verification.
6. Inspect terminal state, events, artifacts, changed files, and test output before reporting completion.

## Boundaries

- Resolve values from current MCP/CLI output; do not invent gateway URLs, join codes, workspace paths, or capabilities.
- Keep agent work inside declared workspace and host policy.
- Use `interrupt` for an in-flight task and `close` only when the operator asks.
- Do not create hidden persistence, public target-host listeners, or unrestricted execution paths. A browser handoff bootstrap may install only its visible UAC-approved managed service after the target user runs it.
- Do not hand-author, claim, or split an alternate bootstrap when the configured
  browser handoff exists; its adaptive page is the session-native delivery surface.

## Adaptive configuration

Before acting, probe the current gateway, network reachability, tunnel and mesh
availability, workspace, and host environment. If a required value is unclear,
ask a concise question; never invent a configuration value.

## Verification

A task is complete only when its terminal state, relevant events, artifacts, and requested checks agree.
