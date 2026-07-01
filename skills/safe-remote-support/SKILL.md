---
name: safe-remote-support
description: Use when an agent needs to create or operate a consent-based remote support session for a temporary or managed host through Remote Dev Skillkit.
---

# Safe Remote Support

Use this skill when a user asks to connect to a remote machine for troubleshooting, repair, environment setup, or remote development through `rdev`.

## Rules

- Use attended temporary mode for third-party machines.
- Before creating tickets, launchers, service plans, or jobs, determine the
  target OS, shell, installed `rdev` binary, gateway or join URL, ticket source,
  workspace path, framework install path, release-verification inputs, and
  operator-approved capabilities. Probe
  read-only when available; ask a concise follow-up when any required value is
  ambiguous.
- Do not substitute placeholder domains, user paths, ticket codes, release
  roots, checksums, workspace roots, adapter choices, approval policies, or
  framework paths for real configuration. Example values are documentation
  only; do not invent values from them.
- Keep path and configuration neutral. Do not assume a fixed checkout path,
  user home, temp directory, workspace root, framework install directory,
  gateway URL, repo owner/name, or release artifact location. Resolve values
  from read-only probes, active configuration, MCP/CLI output, manifest
  metadata, generated invite fields, or explicit human/operator confirmation.
- If gateway, workspace, adapter, approval, release, or framework configuration
  is unclear after read-only probes, ask before generating commands.
- For Windows temporary acceptance, generate the plan using a confirmed release
  bundle URL and output directory, then verify the emitted plan path before
  sending a one-command bootstrap to a target user.
- After a real Windows temporary run, package release evidence using the plan,
  output directory, transcript, release verification, audit, no-persistence
  evidence, and approval-probe paths produced or confirmed for that run.
- For published Windows bootstrap artifacts, hash-pin `rdev-verify.exe` and prefer signed release bundle verification; use single host release manifests only for compatibility.
- For PowerShell jobs, require `powershell.user`, use scoped commands with `allow_commands`, and do not bypass the target host's PowerShell execution policy.
- Do not create hidden persistence.
- Do not bypass UAC, sudo, or OS security controls.
- Do not request secrets in chat.
- Use approval gates for package installation, service modification, elevation, GUI control, credential access, push, deploy, or destructive filesystem actions.
- Prefer short-lived tickets.
- Always summarize evidence after a job: commands, exit codes, files changed, approvals, artifacts, and residual risk.

## Workflow

1. Discover local context: available `rdev`, MCP tools, gateway configuration,
   target OS, shell, workspace path, framework install path, release
   bundle/verifier inputs, and approved support mode.
2. Ask for missing gateway, ticket, release, checksum, root key, or target-user
   details before generating commands.
3. Create a ticket with `rdev.tickets.create`.
4. For Windows temporary support, generate and verify the acceptance plan, then review the launcher, release-verification requirements, no-persistence checks, and approval probes.
5. Explain the join URL and visible consent screen.
6. Wait for the host to appear pending.
7. Ask the operator to approve the host with scoped capabilities.
8. Inspect capabilities with `rdev.hosts.capabilities`.
9. Create small scoped jobs with `rdev.jobs.create`.
10. Use `rdev.jobs.approve` for dangerous actions.
11. Read artifacts and audit evidence.
12. Revoke the ticket/host when finished and run no-persistence checks for temporary Windows hosts.
13. Package Windows acceptance evidence before claiming the run is release-ready.

## Default Temporary Capabilities

- `shell.user`
- `fs.read`
- `fs.write.scoped`
- `process.inspect`
- `elevation.request`
