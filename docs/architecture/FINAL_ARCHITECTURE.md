# Final Architecture

Remote Dev Skillkit is an agent-native remote development and support system. Its final form is not a remote shell wrapper. It is a consent-first control plane that lets Hermes, Codex, Claude Code, OpenCode, and similar agents delegate work to enrolled machines through signed, policy-bound, auditable tasks.

For the canonical final architecture lock, including trust boundaries, protocols, acceptance tests, and the open-source package shape, see [Perfect Ending Solution](PERFECT_ENDING_SOLUTION.md). [Perfect End-State Architecture](PERFECT_END_STATE.md) and [Final System Design](FINAL_SYSTEM_DESIGN.md) remain supporting background. This file remains the implementation-oriented v1 architecture narrative.

## End State

The ideal user experience is:

1. An operator asks an agent to fix or develop something on a Mac, Windows, or Linux host.
2. The agent creates a scoped ticket or selects an already managed host.
3. The target host connects outbound to the gateway, proves its identity, and waits for authorization.
4. The agent submits small tasks through MCP tools instead of receiving unrestricted machine access.
5. The host validates every signed task locally, executes through an adapter, streams evidence, and rejects anything outside policy.
6. The agent summarizes diffs, logs, tests, authorizations, artifacts, and residual risk before the operator decides whether to push, merge, deploy, revoke, or continue.

The system must be useful for an operator's Hermes workflow first, but generic enough to publish as an open-source skill/toolkit for other agent stacks.

## Design Principles

- **Outbound-only by default.** Temporary hosts do not expose inbound ports.
- **Consent first.** Third-party temporary hosts run visibly in the foreground with stop/revoke controls.
- **No hidden persistence.** Long-lived service mode is only for owned or formally managed devices.
- **No silent elevation.** Admin, sudo, UAC, TCC, GUI control, credential changes, package installation, push, deploy, and destructive filesystem operations require explicit session interrupts.
- **No raw unrestricted shell.** Agents request typed tasks with policy, limits, and audit metadata.
- **Defense in depth.** Gateway policy and host-side policy both enforce authorization.
- **Evidence over trust.** Every task returns transcript, diff, exit codes, artifacts, authorizations, and audit events.
- **Portable core, pluggable edges.** The core protocol stays stable while transports, service managers, and coding CLIs are adapters.

## System Planes

```text
Agent Interface Plane
  Agent Skills, MCP tools, CLI, HTTP API
        |
Governance Plane
  identity, policy, authorization, audit, redaction, revocation
        |
Control Plane
  tickets, host registry, task queue, artifact store, event stream
        |
Transport Plane
  outbound WSS/mTLS relay, optional mesh, optional GUI adapter
        |
Execution Plane
  rdev-host, capability detector, local policy engine, adapters
```

## Components

### `rdev-gateway`

Runs near Hermes and owns server-side state.

Responsibilities:

- issue one-time enrollment tickets;
- register hosts and host public keys;
- keep host status, capabilities, mode, and leases;
- sign session tasks;
- enforce server-side policy;
- queue tasks and collect status;
- store artifacts and audit events;
- expose MCP, CLI, and HTTP APIs;
- revoke tickets, hosts, tasks, and signing keys.

Durable storage should be SQLite for local/single-user installs and Postgres for hosted or multi-tenant deployments. Artifacts should be stored in a filesystem path for local installs and object storage for larger deployments. Audit events are append-only and hash-chained.

### `rdev-host`

Runs on the target machine and owns local execution.

Responsibilities:

- generate and protect a per-host keypair;
- connect outbound to the gateway;
- report capabilities and health;
- verify every task signature, expiry, nonce, host binding, and policy;
- execute tasks through adapters;
- stream logs, artifacts, and status;
- keep a local audit spool when offline;
- enforce workspace locks;
- stop cleanly on revoke, cancel, expiry, or local user stop.

Temporary mode is foreground-only. Managed mode may install a service, but only through an explicit command and clear local consent.

### `rdev-mcp`

Exposes agent-facing tools. MCP is the primary interface for an agent and other agents.

Required tool groups:

- `rdev.sessions.*` for session creation, status, replayable events, task submission, interrupts, artifact metadata, and close;
- `rdev.audit.*` for audit queries;
- `rdev.policy.*` for dry-run policy explanation.

MCP tools should never expose "run arbitrary command" as the default primitive. They should create typed session tasks with explicit capabilities, workspace boundaries, limits, and interrupt/cancel semantics.

### `rdev` CLI

The CLI is the operator and debugging surface.

Important commands:

- `rdev doctor`
- `rdev ticket create`
- `rdev host serve`
- `rdev host install-service`
- `rdev host uninstall-service`
- `rdev gateway serve`
- `rdev mcp serve`
- `rdev policy explain`
- `rdev audit export`

### Agent Skills

Skills are the portable "agent install" surface. They should stay concise and procedural, with deeper references loaded only when needed.

Core skills:

- `safe-remote-support`: create and operate visible support sessions safely.
- `remote-vibe-coding`: delegate coding work to enrolled hosts.
- `host-triage`: inspect readiness before making changes.
- `remote-session-review`: verify session task evidence before declaring completion.

Future skills:

- `managed-host-onboarding`
- `windows-repair-session`
- `agent-cli-adapter-authoring`
- `remote-incident-break-glass`

## Host Modes

| Mode | Default user | Persistence | Transport | Authorization posture |
|---|---|---|---|---|
| `attended-temporary` | third-party or short-lived machine | none | outbound WSS relay | strict, short TTL, foreground |
| `managed` | owned or formally managed device | explicit service | relay or mesh | durable policy, revocable |
| `break-glass` | emergency repair | explicit, short-lived | relay | very short TTL, extra authorizations |

Temporary third-party mode must not install an unattended service, hide windows, bypass OS prompts, or silently request elevation.

## Connectivity

### Layer 0: Outbound Relay

Default for temporary and public-internet scenarios.

```text
rdev-host -> HTTPS/WSS/mTLS :443 -> rdev-gateway
```

Properties:

- works behind NAT and restrictive routers;
- no public inbound target-machine port;
- gateway can route signed tasks over an existing authenticated channel;
- host can disconnect immediately on stop, revoke, or ticket expiry.

### Layer 1: Mesh

Optional for owned devices.

Tailscale or headscale can provide private routing, ACLs, tags, and stable device identity. Mesh connectivity is an optimization, not the source of authorization. Tasks still require signatures and local host policy checks.

### Layer 2: GUI Adapter

Optional for tasks that truly need a desktop.

RustDesk or MeshCentral may be integrated as explicit adapters. GUI view/control is a separate capability, has a visible consent surface, and must produce audit events.

## Enrollment

### Temporary Host Flow

1. Agent creates a one-time ticket with mode, reason, TTL, requested capabilities, and operator identity.
2. Gateway returns a join URL.
3. Join page shows server, operator, reason, mode, expiration, requested capabilities, and stop instructions.
4. User runs a platform bootstrap.
5. Bootstrap downloads a signed manifest and signed `rdev-host` binary.
6. Bootstrap verifies checksum and signature before execution.
7. Host generates a keypair and submits public key plus capability inventory.
8. Gateway marks host as pending.
9. Operator authorizes scoped capabilities.
10. Host receives signed policy and waits for tasks.

### Managed Host Flow

Managed installation is separate:

```bash
rdev host install-service --mode managed --gateway https://api.example.com
```

Managed service installation must show what will be installed, which account it runs as, what paths it can touch, how to stop it, and how to uninstall it.

## Bootstrap Design

Bootstrap scripts must be boring and deterministic.

Windows requirements:

- PowerShell 5.1+ baseline, PowerShell 7 optional;
- no Node, Python, Git, or package manager dependency;
- download manifest and binary to a temp directory;
- verify SHA-256 and code signature before execution;
- run foreground temporary mode by default;
- use normal UAC prompts only when an authorized action requests elevation;
- leave no service behind in temporary mode.

macOS and Linux requirements:

- POSIX shell baseline;
- curl or platform fallback download path;
- checksum and signature verification;
- foreground temporary mode by default;
- managed mode maps to LaunchAgent, LaunchDaemon, or systemd only after explicit install.

The join page may offer a one-line command for convenience, but the script and manifest must be inspectable before execution.

## Task Envelope

Every executable request becomes a signed session task.

The current development gateway signs envelopes with an in-memory Ed25519 key. Production deployments must replace this with durable key storage, rotation, revocation, and host trust distribution.

Required fields:

```json
{
  "schema_version": "rdev.task.v1",
  "task_id": "task_...",
  "host_id": "hst_...",
  "ticket_id": "tkt_...",
  "operator_id": "op_...",
  "issued_at": "<iso8601-issued-at>",
  "expires_at": "<iso8601-expires-at>",
  "nonce": "...",
  "mode": "attended-temporary",
  "adapter": "codex",
  "intent": "fix tests in this repository",
  "workspace": {
    "root": "/path/to/repo",
    "write_scope": ["/path/to/repo"],
    "branch": "rdev/task-task_..."
  },
  "capabilities": ["fs.read", "fs.write.scoped", "git.diff", "shell.user"],
  "limits": {
    "max_duration_seconds": 1800,
    "max_output_bytes": 1048576,
    "network": "default-deny"
  },
  "authorizations_required": ["git.push", "elevation.request"],
  "payload": {},
  "signature": "..."
}
```

Host validation:

1. canonicalize envelope;
2. verify gateway signature;
3. verify host binding;
4. verify ticket or managed policy is still active;
5. verify expiry and nonce replay protection;
6. verify adapter and capabilities are allowed;
7. verify workspace boundary;
8. execute or reject with an audited reason.

## Policy Model

Capabilities are hierarchical and deny-by-default.

Examples:

- `fs.read`
- `fs.write.scoped`
- `shell.user`
- `shell.package_install`
- `process.inspect`
- `process.kill.scoped`
- `git.diff`
- `git.commit`
- `git.push`
- `network.fetch`
- `elevation.request`
- `service.manage`
- `gui.view`
- `gui.control`
- `secrets.read`

`secrets.read` should be denied by default. Agents should use host-native credential mechanisms without receiving raw secret values in prompts or logs.

Policy is enforced twice:

- gateway rejects tasks it should not sign;
- host rejects tasks it should not run.

Policy decisions must explain:

- requested action;
- matched rule;
- allow, deny, or require authorization;
- missing capability or authorization;
- audit event id.

## Authorization Gates

The following must require explicit authorization:

- privilege elevation;
- service installation or service modification;
- package manager installation when it changes system state;
- deleting files outside the workspace;
- credential or keychain access;
- GUI control;
- pushing, merging, deploying, or publishing;
- changing firewall, security, or remote access settings;
- long-running unattended work on third-party machines.

An agent may request authorization, but it must not authorize its own dangerous action.

## Execution Adapters

Adapters give agents useful power without collapsing into unrestricted shell.

Required adapter contract:

```text
detect() -> capability
prepare(task) -> workspace
run(task, prompt, limits) -> stream
verify(task) -> evidence
summarize(task) -> result
cleanup(task) -> status
```

Initial adapters:

- `shell`: safe command execution inside policy and workspace boundaries;
- `powershell`: Windows-first diagnostics and repair;
- `git`: branch, worktree, diff, commit evidence;
- `codex`: run Codex CLI in a controlled worktree;
- `claude-code`: run Claude Code in a controlled worktree;
- `acp`: use an Agent Client Protocol compatible adapter when available;
- `browser-e2e`: run Playwright or browser checks when policy allows.

For coding work, prefer adapter-native protocols over scraping terminal output whenever possible. PTY is still necessary for compatibility with existing CLIs.

## Workspace Model

Every write task has a workspace.

Rules:

- default workspace is a repository or explicit directory;
- one active writer per workspace unless separate worktrees are created;
- tasks create a branch or worktree by default;
- writes outside declared scope are blocked;
- generated artifacts go to a task-specific directory;
- final result includes diff, changed files, verification commands, and exit codes.

## Audit And Evidence

Audit is a first-class product feature.

Every event records:

- operator;
- ticket;
- host;
- task;
- mode;
- policy decision;
- command or adapter action;
- working directory;
- files read or written when available;
- process and elevation events;
- authorizations;
- artifacts;
- timestamps;
- hash of previous audit event.

Evidence returned to agents:

- transcript snippets;
- logs;
- exit codes;
- diffs;
- test reports;
- screenshots when GUI/browser tasks are authorized;
- artifact checksums.

Sensitive values must be redacted before leaving the host when possible, then redacted again before display.

## Reliability

The system must tolerate bad networks and messy target machines.

Host reliability:

- heartbeat with current task and load;
- reconnect with exponential backoff;
- local task inbox/outbox;
- idempotent task state transitions;
- local audit spool flushed on reconnect;
- cooperative cancellation;
- per-task timeout;
- crash recovery for managed mode;
- foreground stop button for temporary mode.

Gateway reliability:

- durable task queue;
- leases for running tasks;
- retry only when idempotent;
- operator-visible stale host detection;
- revocation propagation;
- audit writes before and after dangerous actions;
- diagnostics bundle command.

## Cross-Platform Notes

Windows is a primary target:

- ship signed amd64 and arm64 binaries;
- use ConPTY for interactive CLI adapters;
- support PowerShell 5.1 as baseline;
- detect winget, chocolatey, scoop, Git, Visual Studio Build Tools, WSL, Codex, Claude Code;
- respect Defender, enterprise policy, UAC, and execution policy;
- never require disabling security controls.

macOS:

- ship notarized arm64 and amd64 binaries;
- use LaunchAgent for user-level managed mode;
- use LaunchDaemon only when explicitly authorized;
- respect TCC and keychain prompts;
- detect Xcode, Homebrew, Git, Codex, Claude Code.

Linux:

- ship glibc and musl-compatible binaries where practical;
- use systemd user service by default for managed mode;
- detect distro, package manager, shell, Git, compilers, containers, Codex, Claude Code.

## API Shape

Production HTTP API:

- `POST /v1/sessions`
- `POST /v1/session-joins`
- `GET /v1/sessions/{id}`
- `GET /v1/sessions/{id}/events`
- `POST /v1/sessions/{id}/tasks`
- `POST /v1/sessions/{id}/tasks/{task_id}/result`
- `POST /v1/sessions/{id}/events`
- `POST /v1/sessions/{id}/artifacts`
- `POST /v1/sessions/{id}/close`
- `GET /v1/audit`
- `POST /v1/policy/explain`

All production APIs require authentication. Development APIs may bind to localhost without auth only under an explicit `--dev` flag.

## Deployment For Hermes

Recommended personal deployment:

```text
api.example.com
  rdev-gateway
  rdev-mcp
  Postgres or SQLite
  artifact store
  audit store

agent.example.com
  join page
  bootstrap manifest
  release binaries
  WSS relay endpoint

Hermes
  installs Agent Skills
  talks to rdev-mcp
```

This Mac should be the first managed host. Temporary Windows hosts should use attended temporary mode until the Windows bootstrap, signing, local policy, and WSS relay are complete.

## Release And Supply Chain

Open-source releases require:

- CI build matrix for macOS, Windows, Linux, amd64, arm64;
- reproducible release scripts where practical;
- checksums;
- signed binaries;
- signed manifests;
- SBOM;
- vulnerability scanning;
- upgrade rollback path;
- security policy and disclosure address.

## Roadmap To The Final State

### v0.1: Local Safety Foundation

- in-memory gateway;
- MCP stdio;
- local HTTP dev gateway;
- ticket, host, task, artifact, audit models;
- local demo;
- signed session task model;
- policy tests.

### v0.2: Temporary Windows MVP

- PowerShell bootstrap;
- signed manifest verification;
- foreground `rdev-host`;
- outbound WSS;
- ticket exchange;
- host authorization;
- shell and PowerShell tasks;
- artifact streaming;
- host-side audit spool.

### v0.3: Managed Hosts

- Windows Service;
- macOS LaunchAgent;
- Linux systemd;
- watchdog and restart;
- managed host policy;
- auto-update with rollback.

### v0.4: Agent Coding Adapters

- Codex adapter;
- Claude Code adapter;
- ACP-compatible adapter;
- workspace locks;
- git branch/worktree workflow;
- test and diff evidence.

### v0.5: Mesh And GUI

- Tailscale/headscale adapter;
- RustDesk or MeshCentral adapter;
- GUI consent and audit;
- browser/E2E artifacts.

### v1.0: Public Skillkit

- stable protocol;
- stable MCP tools;
- installable Agent Skills;
- signed releases;
- complete docs;
- security review;
- production deployment guide.

## Definition Of "Perfect Ending"

The project is done when the operator can tell an agent:

> Help this Windows machine fix its failing development environment.

Then an agent can create a short-lived join ticket, the remote user can run a visible signed bootstrap, the host can connect outbound-only, an agent can triage the machine, run bounded repair tasks, collect evidence, ask for authorizations when needed, and revoke access cleanly.

The same system should also let an operator say:

> Use my Mac to continue development in this repository with Codex, run the tests, and show me the diff.

an agent should select the managed Mac, create a signed coding task, run Codex or another adapter in a locked worktree, return diff and test evidence, and ask before push or merge.

That is the final product: a universal, secure, agent-native remote execution fabric for personal and professional development work.

## External Design Anchors

The architecture intentionally aligns with current public standards and platform behavior:

- MCP tools are treated as privileged external-system actions that need clear schemas, authorization, consent, and safety metadata: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- HTTP MCP deployments should use the MCP authorization model; stdio deployments should receive credentials from the local environment: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
- Tailscale/headscale mesh mode should use ephemeral and tagged keys for short-lived or non-human devices, with ACLs controlling access: https://tailscale.com/docs/features/access-control/auth-keys
- Windows bootstrap must respect PowerShell execution policy and enterprise Group Policy instead of trying to weaken local security settings: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_execution_policies
- Release and update integrity should use signed artifacts, checksums, manifests, and a verification flow compatible with Sigstore/Cosign-style signing: https://docs.sigstore.dev/cosign/verifying/verify/
