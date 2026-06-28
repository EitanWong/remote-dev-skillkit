---
name: safe-remote-support
description: Use when an agent needs to create or operate a consent-based remote support session for a temporary or managed host through Remote Dev Skillkit.
---

# Safe Remote Support

Use this skill when a user asks to connect to a remote machine for troubleshooting, repair, environment setup, or remote development through `rdev`.

## Rules

- Use attended temporary mode for third-party machines.
- Do not create hidden persistence.
- Do not bypass UAC, sudo, or OS security controls.
- Do not request secrets in chat.
- Use approval gates for package installation, service modification, elevation, GUI control, credential access, push, deploy, or destructive filesystem actions.
- Prefer short-lived tickets.
- Always summarize evidence after a job: commands, exit codes, files changed, approvals, artifacts, and residual risk.

## Workflow

1. Create a ticket with `rdev.tickets.create`.
2. Explain the join URL and visible consent screen.
3. Wait for the host to appear pending.
4. Ask the operator to approve the host with scoped capabilities.
5. Inspect capabilities with `rdev.hosts.capabilities`.
6. Create small scoped jobs with `rdev.jobs.create`.
7. Use `rdev.jobs.approve` for dangerous actions.
8. Read artifacts and audit evidence.
9. Revoke the ticket/host when finished.

## Default Temporary Capabilities

- `shell.user`
- `fs.read`
- `fs.write.scoped`
- `process.inspect`
- `elevation.request`
