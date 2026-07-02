---
name: safe-remote-support
description: Use when an agent needs to create, operate, review, or revoke a consent-based Remote Dev Skillkit support session for a temporary third-party host or managed owned host, including invite creation, visible bootstrap, scoped jobs, runtime memory, approvals, evidence, and cleanup.
---

# Safe Remote Support

Use this skill when a user asks to connect to a remote machine for troubleshooting, repair, environment setup, or remote development through `rdev`.

## Rules

- Use attended temporary mode for third-party machines.
- Load scoped runtime memory before creating a new support session, but verify
  stale host, gateway, workspace, release, adapter, and approval facts before
  using them.
- Before creating tickets, launchers, service plans, or jobs, determine the
  target OS, shell, installed `rdev` binary, gateway or join URL, ticket source,
  workspace path, framework install path, network reachability, proxy/DNS
  state, NAT/firewall/CGNAT constraints, SSH configuration, installed
  tunnel/mesh tools, release-verification inputs, and operator-approved
  capabilities. Probe read-only when available; ask a concise follow-up when
  any required value is ambiguous.
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
  is unclear after read-only probes, ask before generating commands. If a
  tunnel or mesh path is needed, prefer existing or open-source/free options
  before paid relays, and ask before privileged, persistent, firewall, DNS,
  cloud, or security-policy changes.
- For Windows temporary acceptance, generate the plan using a confirmed release
  bundle URL and output directory, then verify the emitted plan path before
  sending a one-command bootstrap to a target user.
- For any new target host, prefer a signed self-contained connection entry
  package or package-aware join link from the invite's `connection_entry_plan`
  before asking a human to install prerequisites, copy ticket codes, copy
  manifest roots, or hand-assemble network flags.
- For every new target host, create an invite first and then materialize it with
  `rdev.connection_entry.plan` or `rdev connection-entry plan` before sending
  target-side instructions. Treat Connection Entry as the universal handoff
  name, and `entry_package_plan` as the generic package surface for Windows,
  macOS, Linux, managed, LAN, hosted, relay, mesh, SSH, or VPN variants. If
  release/package inputs are missing, report those missing inputs to the
  operator instead of asking the target-side human to assemble raw connection
  parameters. For operator-owned durable machines, use the materialized reviewed
  managed-service plan; for third-party support, keep the session
  attended-temporary with no persistence by default.
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
- Write runtime memory only for reusable support facts that are safe to retain,
  such as detected OS family, adapter availability, proxy requirement, verifier
  availability, and approved workspace scope. Do not store target-side secrets,
  private hostnames, unredacted transcripts, ticket codes, operator tokens, or
  broad filesystem inventories.

## Workflow

1. Discover local context: available `rdev`, MCP tools, gateway configuration,
   target OS, shell, workspace path, framework install path, release
   bundle/verifier inputs, and approved support mode.
2. Read scoped runtime memory and verify stale or high-impact facts.
3. Ask for missing gateway, ticket, release, checksum, root key, or target-user
   details before generating commands.
4. Create an invite with `rdev.invites.create` when available so the Agent gets
   `connection_entry`, `connection_entry_plan`, manifest root, and transport
   fallback instructions together.
5. Materialize the invite with `rdev.connection_entry.plan` or
   `rdev connection-entry plan`; review `mode_decision`, `human_surface`,
   `agent_metadata`, `missing_inputs`, and `entry_package_plan`.
6. For Windows temporary support, generate and verify the acceptance plan or
   connection entry package, then review the launcher, release-verification
   requirements, no-persistence checks, and approval probes.
7. Explain the selected connection entry URL, visible script, or signed package
   and visible consent screen.
8. Wait for the host to appear pending.
9. Ask the operator to approve the host with scoped capabilities.
10. Inspect capabilities with `rdev.hosts.capabilities`.
11. Create small scoped jobs with `rdev.jobs.create`.
12. Use `rdev.jobs.approve` for dangerous actions.
13. Read artifacts and audit evidence.
14. Update or invalidate runtime memory from reviewed evidence.
15. Revoke the ticket/host when finished and run no-persistence checks for temporary Windows hosts.
16. Package Windows acceptance evidence before claiming the run is release-ready.

## Output

Return stable field names:

- `session_mode`;
- `invite_or_ticket`;
- `connection_entry_plan`;
- `host_status`;
- `capabilities`;
- `approvals`;
- `jobs_run`;
- `memory_used`;
- `memory_updates`;
- `evidence_refs`;
- `cleanup_or_revocation`;
- `remaining_risk`.

## Default Temporary Capabilities

- `shell.user`
- `fs.read`
- `fs.write.scoped`
- `process.inspect`
- `elevation.request`
