---
name: safe-remote-support
description: Use when an agent needs to create or operate a consent-based remote support session for a temporary or managed host through Remote Dev Skillkit.
---

# Safe Remote Support

Use this skill when a user asks to connect to a remote machine for troubleshooting, repair, environment setup, or remote development through `rdev`.

## Rules

- Use attended temporary mode for third-party machines.
- For Windows temporary acceptance, prefer `rdev acceptance windows-temporary --release-bundle-url <url> --out <empty-dir> ...`, then verify it with `rdev acceptance verify-windows-temporary --plan <out>/windows-temporary-plan.json` before sending a one-command bootstrap to a target user.
- After a real Windows temporary run, package release evidence with `rdev acceptance package-windows-temporary --plan <plan> --out <empty-dir> --transcript <file> --release-verification <file> --audit <file> --no-persistence-dir <dir> --approval-probes-dir <dir>`.
- For published Windows bootstrap artifacts, hash-pin `rdev-verify.exe` and prefer signed release bundle verification; use single host release manifests only for compatibility.
- For PowerShell jobs, require `powershell.user`, use scoped commands with `allow_commands`, and do not bypass the target host's PowerShell execution policy.
- Do not create hidden persistence.
- Do not bypass UAC, sudo, or OS security controls.
- Do not request secrets in chat.
- Use approval gates for package installation, service modification, elevation, GUI control, credential access, push, deploy, or destructive filesystem actions.
- Prefer short-lived tickets.
- Always summarize evidence after a job: commands, exit codes, files changed, approvals, artifacts, and residual risk.

## Workflow

1. Create a ticket with `rdev.tickets.create`.
2. For Windows temporary support, generate and verify the acceptance plan, then review the launcher, release-verification requirements, no-persistence checks, and approval probes.
3. Explain the join URL and visible consent screen.
4. Wait for the host to appear pending.
5. Ask the operator to approve the host with scoped capabilities.
6. Inspect capabilities with `rdev.hosts.capabilities`.
7. Create small scoped jobs with `rdev.jobs.create`.
8. Use `rdev.jobs.approve` for dangerous actions.
9. Read artifacts and audit evidence.
10. Revoke the ticket/host when finished and run no-persistence checks for temporary Windows hosts.
11. Package Windows acceptance evidence before claiming the run is release-ready.

## Default Temporary Capabilities

- `shell.user`
- `fs.read`
- `fs.write.scoped`
- `process.inspect`
- `elevation.request`
