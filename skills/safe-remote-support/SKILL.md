---
name: safe-remote-support
description: Use when an agent needs to create, operate, review, or revoke a consent-based Remote Dev Skillkit support session for a temporary third-party host or managed owned host, including invite creation, visible bootstrap, scoped jobs, runtime memory, approvals, evidence, and cleanup.
---

# Safe Remote Support

Use this skill when a user asks to connect to a remote machine for troubleshooting, repair, environment setup, or remote development through `rdev`.

## Rules

- Use attended temporary mode for third-party machines.
- For company or third-party machines, ask only for authorization first:
  confirm that policy and the device owner allow a visible temporary Remote Dev
  Skillkit support session. After confirmation, default to
  attended-temporary, no-persistence mode and let Connection Entry probes detect
  OS, architecture, shell, and connection path.
- If `rdev` is not found, do not stop. Recover from PATH/current executable,
  build the checkout with `go install ./cmd/rdev`, or use
  `go run ./cmd/rdev bootstrap agent-plan --repo-root .` as a temporary
  planner before asking the user for an `rdev` path.
- Run `rdev bootstrap agent-plan --repo-root .` when available and use it as
  the machine-readable contract for local MCP, `rdev` recovery, remote defaults,
  and ask/auto-probe boundaries.
- For every new visible support session, call `rdev.support_session.plan` over
  MCP or `rdev support-session plan` over CLI before writing any gateway,
  PowerShell, relay, nohup, approval, or bootstrap steps. Treat the returned
  `rdev.support-session-plan.v1` as the standard session package: build or find
  `rdev`, start the gateway with verified helper assets, create the invite,
  localize the target command, and use scoped attended-temporary auto-approval
  when enabled.
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
- Do not ask the human to choose target OS, temporary-vs-managed mode, ticket
  code, root key, gateway URL, transport, release root, checksum, or helper
  command when the safe default or Connection Entry metadata can determine it.
  Ask about managed persistence only when the target is operator-owned and
  recurring access is requested.
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
- If a missing user-space helper blocks an otherwise approved connection path,
  use only `rdev deps install` or a reviewed `RDEV_*_INSTALL_ACTION_JSON` with
  explicit URL, SHA-256, target platform, and user/workspace scope. Do not use
  hidden installation, execution-policy bypass, shell command-string wrappers,
  elevation, services/drivers, firewall/DNS/route mutation, or credential
  creation without explicit approval.
- For Windows temporary acceptance, generate the plan using a confirmed release
  bundle URL and output directory, then verify the emitted plan path before
  sending a one-command bootstrap to a target user.
- For any new target host, prefer a signed self-contained connection entry
  package or package-aware join link from the invite's `connection_entry_plan`
  before asking a human to install prerequisites, copy ticket codes, copy
  manifest roots, or hand-assemble network flags.
- Use `connection_entry.package_catalog` and the signed join manifest's
  `package_catalog` to select the target OS/architecture candidate. If package
  status shows planned assets or release inputs are missing, use the visible
  script fallback and report missing inputs to the operator instead of asking
  the target-side human to assemble raw parameters.
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
2. If `rdev` is unavailable, recover it from the checkout or safe clone before
   asking for help; use `rdev bootstrap agent-plan --repo-root .` or
   `go run ./cmd/rdev bootstrap agent-plan --repo-root .`.
3. Read scoped runtime memory and verify stale or high-impact facts.
4. Ask only for company/owner authorization first when the target is a
   third-party or company machine. Use visible attended-temporary mode unless
   the operator explicitly requests and approves managed persistence.
5. Ask for missing gateway, release, root, or approval details only when they
   cannot be supplied by the invite, signed manifest, Connection Entry plan, or
   local probes.
6. Create the standard support session plan with `rdev.support_session.plan` or
   `rdev support-session plan`; execute only reviewed argv steps from that plan.
7. Create an invite with `rdev.invites.create` when available so the Agent gets
   `connection_entry`, `connection_entry_plan`, manifest root, and transport
   fallback instructions together.
8. Materialize the invite with `rdev.connection_entry.plan` or
   `rdev connection-entry plan`; review `mode_decision`, `human_surface`,
   package-catalog candidate choice, `agent_metadata`, `missing_inputs`, and
   `runner_plan`/`entry_package_plan`.
9. Prefer the materialized Connection Entry runner when available. Dry-run it
   with `rdev connection-entry run --runner-manifest ... --dry-run` to select
   direct, proxy, LAN, relay, mesh, VPN, or SSH-assisted connectivity before the
   target user starts the visible session. If the runner reports a configured
   user/workspace dependency install action, let it install, verify, and use the
   helper binary, then record the install report as evidence. For Windows
   temporary support, generate and verify the acceptance plan or connection
   entry package, then review the launcher, release-verification requirements,
   no-persistence checks, and approval probes.
10. Explain the selected connection entry URL, visible script, or signed package
   and visible consent screen.
11. Wait for the host to appear. If the standard attended-temporary
    auto-approval contract was enabled, verify the host is active and expected;
    otherwise approve it with scoped capabilities.
12. Inspect capabilities with `rdev.hosts.capabilities`.
13. Create small scoped jobs with `rdev.jobs.create`.
14. Use `rdev.jobs.approve` for dangerous actions.
15. Read artifacts and audit evidence.
16. Update or invalidate runtime memory from reviewed evidence.
17. Revoke the ticket/host when finished and run no-persistence checks for temporary Windows hosts.
18. Package Windows acceptance evidence before claiming the run is release-ready.

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
