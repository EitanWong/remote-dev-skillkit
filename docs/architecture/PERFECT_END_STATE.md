# Perfect End-State Architecture

This document is supporting end-state background for Remote Dev Skillkit. The canonical final architecture lock is [Perfect Ending Solution](PERFECT_ENDING_SOLUTION.md). This file describes the product we are trying to finish, the safety boundaries that must never regress, and the phased path from the current local development gateway to a public open-source agent skillkit.

## One-Sentence Outcome

Remote Dev Skillkit lets an AI agent safely delegate development and repair work to Mac, Windows, and Linux machines through visible consent, outbound-only connectivity, signed job envelopes, local policy enforcement, approval gates, and durable audit evidence.

## Product Thesis

The project should not become another SSH wrapper, hidden remote-control agent, or broad terminal MCP server. Those primitives already exist and are too easy to misuse.

The missing open-source layer is a portable remote-work protocol for agents:

- create a consented session;
- identify and approve a host;
- express work as typed, bounded jobs;
- execute through adapters;
- collect evidence;
- require approval for dangerous steps;
- revoke access cleanly.

The "perfect ending" is a reusable skillkit that Hermes, Codex, Claude Code, OpenCode, Cursor Agent, and other agent stacks can install without each one reinventing unsafe remote execution.

## Standards Anchors

The design intentionally aligns with current public platform behavior:

- MCP tools are model-invoked external actions and should use clear schemas, structured results, access controls, rate limits, output sanitization, logging, and human confirmation for sensitive operations: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- HTTP MCP deployments should follow the MCP authorization model based on OAuth 2.1 roles, protected resource metadata, HTTPS, PKCE, and short-lived tokens: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
- Mesh mode should use Tailscale/headscale auth-key controls such as ephemeral devices, tags, pre-approval, ACLs, and revocation, but only as transport identity rather than job authorization: https://tailscale.com/docs/features/access-control/auth-keys
- Temporary Windows bootstrap must respect PowerShell execution policy and Group Policy precedence instead of weakening local security configuration: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_execution_policies
- Release integrity should use signed artifacts, signed manifests, checksums, and a verification flow compatible with Sigstore/Cosign-style blob or binary verification: https://docs.sigstore.dev/cosign/verifying/verify/

## Golden Paths

### Temporary Repair

an operator tells an agent:

> Help this Windows machine fix its failing development environment.

The final system should:

1. create a short-lived attended ticket;
2. show a join page with operator, reason, server identity, requested capabilities, TTL, and stop instructions;
3. let the remote user run one visible PowerShell bootstrap;
4. verify a signed manifest and signed `rdev-host` binary;
5. generate a per-host keypair locally;
6. connect outbound to the gateway over HTTPS/WSS on port 443;
7. wait in a pending state until the operator approves scoped capabilities;
8. run small signed jobs such as triage, package inspection, repair, tests, and log collection;
9. ask for approval before elevation, package installation, GUI control, service changes, credential access, destructive operations, push, deploy, or publish;
10. return transcript snippets, exit codes, changed files, diffs, screenshots when approved, artifacts, and audit events;
11. revoke the host and ticket when finished.

### Managed Coding

an operator tells an agent:

> Use my Mac to continue development in this repository with Codex, run the tests, and show me the diff.

The final system should:

1. select the managed Mac from the host registry;
2. lock the requested workspace;
3. create a branch or worktree;
4. create a signed coding job bound to that host and workspace;
5. run Codex, Claude Code, ACP/acpx, or another adapter inside policy limits;
6. capture diff, tests, logs, artifacts, and residual risk;
7. ask before push, merge, deploy, credential changes, or service changes.

### Open-Source Install

Another agent user should be able to install:

- Agent Skills for safe remote support and remote coding;
- an MCP server exposing stable `rdev.*` tools;
- a gateway binary or hosted gateway;
- a host binary for attended temporary or managed devices.

They should get safe defaults without knowing the entire security model.

## Non-Negotiable Invariants

- Temporary hosts expose no inbound ports.
- Third-party machines get no hidden persistence.
- Temporary sessions run visibly in the foreground and are TTL-bound.
- Managed service mode is separate and explicit.
- No UAC, sudo, TCC, Defender, Gatekeeper, enterprise policy, firewall, or execution-policy bypass.
- Agents do not receive raw unrestricted shell as the default primitive.
- Every executable request is a signed job envelope.
- Gateway policy and host policy both enforce authorization.
- Dangerous actions require approval from the operator or local user; an agent cannot approve its own dangerous action.
- Secrets are never requested in chat, embedded in prompts, or written to audit logs.
- Every result includes evidence sufficient for another agent or human to review.
- Revocation must be fast, visible, and audited.

## Architecture Planes

```text
Agent Interface Plane
  Agent Skills, MCP tools, CLI, optional web console

Governance Plane
  identity, policy, approvals, revocation, redaction, audit

Control Plane
  tickets, host registry, job queue, leases, artifacts, event stream

Transport Plane
  outbound HTTPS/WSS relay, optional mesh, optional GUI adapter

Execution Plane
  rdev-host, local policy engine, adapters, workspace locks, audit spool
```

Each lower plane must be useful without trusting the plane above it completely. For example, the gateway can refuse to sign a job, but the host must still verify signature, expiry, host binding, workspace scope, and local policy before execution.

## Trust Model

### Identities

| Identity | Key material | Purpose |
|---|---|---|
| Operator | gateway account/session | approves hosts, jobs, and dangerous actions |
| Agent client | MCP/OAuth identity or local stdio process | requests work through tools |
| Gateway | durable Ed25519 signing key plus TLS identity | signs job envelopes and policies |
| Host | per-host Ed25519 keypair generated on target | proves host identity and receives scoped jobs |
| Release | signing certificate/key or Sigstore identity | verifies bootstrap manifests and binaries |
| Approval | short-lived approval token | binds a human decision to a policy exception |

No shared host passwords or long-lived reusable tickets are allowed.

### Root Of Trust

Temporary bootstrap trusts:

1. HTTPS to the join/download endpoint;
2. a pinned or displayed gateway identity;
3. a signed release manifest;
4. checksum and signature verification for the host binary.

Managed hosts additionally pin the gateway signing key or a trust bundle and update it through a signed rotation protocol.

## Connectivity Layers

### Layer 0: Outbound Relay

Default for temporary and public-internet scenarios.

```text
rdev-host -> HTTPS/WSS :443 -> rdev-gateway
```

Requirements:

- works behind NAT and common routers;
- no inbound target-machine listener;
- authenticated host channel;
- heartbeat and reconnect with exponential backoff;
- job leases and idempotent completion;
- local stop/revoke handling.

If WSS fails, the host may fall back to short-polling HTTPS in temporary mode. Fallback reduces interactivity but improves field compatibility.

### Layer 1: Mesh

Optional for owned or formally managed devices.

Tailscale or headscale may provide private routing, ACLs, stable device names, and better diagnostics. Mesh identity is not sufficient authorization: every job still needs gateway signature and host-side policy checks.

### Layer 2: GUI Adapter

Optional and explicit.

RustDesk, MeshCentral, browser automation, or native screen-sharing integrations are adapters, not the default control path. GUI view and GUI control are separate capabilities with separate approvals, visible local consent, and screenshots or session metadata in audit evidence.

## Host Modes

| Mode | Target | Persistence | Default privilege | Required UX |
|---|---|---|---|---|
| `attended-temporary` | third-party or short-lived host | none | user-level | visible foreground, stop button, TTL |
| `managed` | owned/formally managed host | explicit service | least privilege | install summary, uninstall path, policy |
| `break-glass` | emergency repair | short-lived explicit access | JIT elevation only | extra warnings, short TTL, high audit |

Temporary mode must not install a service, hide a window, write registry persistence, weaken OS security settings, or auto-restart after user stop.

## Enrollment Protocol

### Ticket Creation

The agent requests a ticket through MCP:

```json
{
  "mode": "attended-temporary",
  "reason": "repair Windows development environment",
  "ttl_seconds": 1800,
  "requested_capabilities": ["fs.read", "fs.write.scoped", "shell.user", "process.inspect"],
  "operator_id": "eitan"
}
```

The gateway creates a one-time ticket with:

- ticket id and human-friendly code;
- mode and requested capabilities;
- TTL and expiry;
- join URL;
- nonce;
- operator identity;
- signed bootstrap metadata.

### Host Registration

The host bootstrap:

1. downloads and verifies the host binary;
2. shows the ticket details;
3. generates a host keypair;
4. detects OS, architecture, tools, security context, and network reachability;
5. submits a registration request signed by the host private key;
6. enters `pending`.

The gateway records the host public key but does not run jobs until the operator approves a scoped policy.

### Approval

Approval binds:

- host id;
- ticket id or managed policy id;
- capabilities;
- workspace roots;
- adapter set;
- max duration;
- network posture;
- required approval gates.

The host receives a signed policy bundle and enforces it locally.

## Job Protocol

### Job Envelope

Every executable request becomes a canonical, signed envelope:

```json
{
  "schema_version": "rdev.job.v1",
  "job_id": "job_...",
  "host_id": "hst_...",
  "ticket_id": "tkt_...",
  "operator_id": "op_...",
  "issued_at": "<iso8601-issued-at>",
  "expires_at": "<iso8601-expires-at>",
  "nonce": "...",
  "mode": "attended-temporary",
  "adapter": "shell",
  "intent": "inspect toolchain and repair test failure",
  "workspace": {
    "root": "C:\\Users\\Alice\\project",
    "write_scope": ["C:\\Users\\Alice\\project"],
    "branch": "rdev/job-job_..."
  },
  "capabilities": ["fs.read", "fs.write.scoped", "shell.user"],
  "limits": {
    "max_duration_seconds": 600,
    "max_output_bytes": 1048576,
    "network": "default-deny"
  },
  "approvals_required": ["elevation.request", "package.install", "git.push"],
  "payload": {
    "argv": ["git", "status", "--short"]
  },
  "signature": "..."
}
```

The signature covers every field except the signature itself. The host validates canonical JSON, key id, signature, expiry, nonce replay, host binding, adapter permission, capability permission, workspace boundary, limits, and approval state.

### State Machine

```text
created -> queued -> leased -> running -> succeeded
                         |          |-> failed
                         |          |-> canceled
                         |          |-> timed_out
                         |-> lease_expired -> queued | canceled
```

State transitions must be idempotent and audited. A host can only complete or fail a job that it leased and that is bound to its host id.

### Result Contract

Each terminal job returns:

- status and failure reason if any;
- command or adapter actions;
- exit codes;
- stdout/stderr excerpts with redaction;
- artifact ids and checksums;
- changed file list;
- diff summary when applicable;
- verification commands and outcomes;
- approvals consumed;
- residual risk.

## Policy Model

Policy is deny-by-default and capability-based.

Core capabilities:

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
- `secrets.use.scoped`

`secrets.read` should not be a normal capability. Agents may ask the host to use an existing credential through the OS or toolchain, but raw secret values should not leave the host.

Policy decisions must be explainable:

```json
{
  "decision": "requires_approval",
  "capability": "shell.package_install",
  "matched_rule": "temporary package installation changes system state",
  "approval_kind": "operator",
  "audit_event_id": "aud_..."
}
```

## Approval Gates

Approval is required for:

- elevation;
- package installation or uninstall;
- service installation, removal, restart, or recovery-policy changes;
- credential/keychain access;
- GUI control;
- destructive filesystem operations outside the workspace;
- process kill outside the job tree;
- firewall, remote-access, security-tool, or execution-policy changes;
- pushing, merging, deploying, publishing, or opening paid jobs;
- long-running unattended work on third-party machines.

The agent can request approval and explain the need. It cannot self-approve.

## Execution Adapters

Adapters expose useful work without collapsing into unrestricted shell.

Required contract:

```text
detect() -> capabilities
prepare(job) -> workspace
run(job, limits) -> stream
verify(job) -> evidence
summarize(job) -> result
cleanup(job) -> status
```

Initial adapters:

| Adapter | Role | Safety posture |
|---|---|---|
| `shell` | allowlisted argv execution | no interpolation, workspace cwd, timeout/output caps |
| `powershell` | Windows diagnostics and repair | constrained scripts, explicit elevation gates |
| `git` | status, diff, branch, worktree, commit evidence | push/merge require approval |
| `codex` | run Codex CLI in locked workspace | no hidden credentials, evidence required |
| `claude-code` | run Claude Code in locked workspace | same as Codex |
| `acp` | structured agent-client protocol bridge | preferred when available |
| `browser-e2e` | Playwright/browser checks | screenshots as artifacts |
| `gui` | RustDesk/MeshCentral/native GUI bridge | separate visible consent |

The safe shell adapter is a bootstrap adapter, not the product's main abstraction. Coding jobs should prefer ACP or agent-native adapters when possible.

## Workspace Model

Every write job has a workspace root.

Rules:

- canonicalize paths before comparing boundaries;
- reject missing, symlink-escaping, or non-directory workspace roots;
- one active writer per workspace unless separate worktrees are created;
- generated artifacts go to a job-specific artifact directory;
- destructive Git operations require approval;
- final results include changed files, diff summary, and verification evidence.

## Audit And Evidence

Audit is an append-only product surface, not debug logging.

Events should be hash-chained:

```text
event_hash = hash(canonical_event_without_hash + previous_event_hash)
```

Events record:

- operator;
- agent client;
- ticket;
- host;
- job;
- policy decision;
- approvals;
- adapter action;
- working directory;
- command argv or adapter operation;
- files touched when available;
- process/elevation events;
- artifact ids and checksums;
- timestamps;
- previous event hash.

The host keeps a local audit spool and flushes it after reconnect. Redaction happens on the host before upload and again at display time.

## Storage

| Data | Local/single-user | Hosted/multi-user |
|---|---|---|
| control state | SQLite | Postgres |
| job queue | SQLite leases | Postgres leases or queue |
| artifacts | filesystem | object storage |
| audit | append-only JSONL + hash chain | append-only table/log + export |
| signing keys | OS keychain/file with permissions | KMS/HSM or sealed secrets |

The development gateway can remain in-memory, but production must have durable keys, durable job state, and auditable recovery.

## Reliability

Host:

- heartbeat with version, policy, active job, and load;
- exponential reconnect;
- local inbox/outbox;
- local audit spool;
- cooperative cancellation;
- per-job timeout;
- crash recovery in managed mode;
- visible stop in temporary mode.

Gateway:

- durable queues and leases;
- idempotent APIs;
- stale host detection;
- retry only when safe;
- revoke ticket, host, policy, job, or signing key;
- diagnostics bundle for support.

## Cross-Platform Requirements

### Windows

- PowerShell 5.1 baseline; PowerShell 7 optional.
- Self-contained signed amd64/arm64 binaries.
- ConPTY for interactive CLI adapters.
- Detect winget, Chocolatey, Scoop, Git, Visual Studio Build Tools, WSL, Codex, Claude Code, ACP tools, browsers, Defender posture.
- Respect UAC, Defender, execution policy, and enterprise Group Policy.
- Windows Service only in explicit managed mode.

### macOS

- Notarized arm64/amd64 binaries.
- LaunchAgent for user-level managed mode.
- LaunchDaemon only with explicit approval.
- Respect TCC, keychain prompts, Gatekeeper, and code signing.
- Detect Xcode, Homebrew, Git, Codex, Claude Code, ACP tools, browsers.

### Linux

- glibc and musl-compatible binaries where practical.
- systemd user service by default for managed mode.
- Detect distro, package manager, shell, Git, compilers, containers, Codex, Claude Code, ACP tools, browsers.
- Avoid assuming root; use sudo only through explicit approval.

## Agent Interface

MCP is the primary agent interface. Stdio mode is for local development or local single-user installs. HTTP mode must use the MCP authorization model and normal web authentication.

Stable tool groups:

- `rdev.tickets.create`
- `rdev.tickets.revoke`
- `rdev.hosts.list`
- `rdev.hosts.get`
- `rdev.hosts.capabilities`
- `rdev.hosts.approve`
- `rdev.hosts.revoke`
- `rdev.jobs.create`
- `rdev.jobs.status`
- `rdev.jobs.stream`
- `rdev.jobs.cancel`
- `rdev.jobs.approve`
- `rdev.artifacts.list`
- `rdev.artifacts.read`
- `rdev.audit.query`
- `rdev.policy.explain`

Tools should be narrow, typed, and schema-rich. There should be no default `run_anything` tool.

## Open-Source Package Shape

```text
remote-dev-skillkit/
  cmd/rdev/                  operator CLI, gateway, host, MCP entrypoint
  internal/gateway/          ticket, host, job, audit state machine
  internal/hostrunner/       host-side validation and execution
  internal/adapters/         shell, powershell, git, codex, claude, acp
  mcp/tools.json             stable tool contract
  skills/                    agent-installable workflows
  scripts/bootstrap/         visible bootstrap scripts
  docs/                      architecture, operations, security, release
```

Skills remain concise and procedural; the CLI and MCP server carry deterministic behavior.

## example deployment Deployment Target

Recommended personal deployment:

```text
https://api.example.com
  rdev-gateway HTTP API
  rdev-mcp HTTP/SSE or streamable HTTP endpoint
  durable database
  artifact store
  audit store

https://agent.example.com
  join page
  bootstrap manifest
  signed release downloads
  WSS relay endpoint

Hermes
  installs Agent Skills
  invokes rdev MCP tools
```

`api.example.com` is the API/control endpoint. `agent.example.com` is the human join and host transport endpoint. They may be served by the same binary, but keeping the conceptual boundary helps security review and future hosted deployment.

## Threat Control Matrix

| Threat | Control |
|---|---|
| Public scanning of temporary hosts | no inbound listener; outbound relay only |
| Leaked ticket | one-time, short TTL, pending approval, scoped capabilities, revoke |
| Malicious agent prompt | typed MCP tools, deny-by-default policy, approval gates |
| Gateway signing key compromise | key ids, rotation, revocation, audit, emergency stop |
| Binary replacement | TLS, signed manifest, checksum, release signature |
| Host compromise | per-host identity, scoped policy, revoke, artifact redaction |
| Credential exfiltration | no raw secret capability, redaction, host-native credential use |
| Audit tampering | append-only storage, hash chain, exportable verification |
| Accidental broad shell | no default unrestricted shell, allowlisted argv, workspace boundary |
| Hidden persistence accusation | visible temporary mode, explicit managed install, uninstall path |

## Release Requirements

Public releases require:

- CI build matrix for Windows, macOS, Linux, amd64, arm64;
- checksums and signed manifests;
- signed and notarized binaries where platform-appropriate;
- SBOM;
- vulnerability scanning;
- upgrade rollback;
- security policy and disclosure channel;
- reproducible release steps where practical.

## Phased Path

### v0.1 Local Safety Foundation

Complete the current local model:

- in-memory gateway;
- MCP stdio;
- local HTTP dev gateway;
- ticket, host, job, artifact, audit models;
- signed envelopes;
- host trust bundle;
- host registration, approval, polling, completion, failure;
- scoped shell adapter;
- workspace boundary tests.

### v0.2 Temporary Windows MVP

- visible PowerShell bootstrap;
- signed manifest and binary verification;
- outbound WSS or HTTPS polling;
- host key storage for the session;
- PowerShell/shell diagnostics jobs;
- artifact streaming;
- local audit spool;
- revocation.

### v0.3 Durable Gateway And Managed Hosts

- SQLite/Postgres storage;
- durable signing keys and rotation;
- Windows Service;
- macOS LaunchAgent;
- Linux systemd;
- watchdog, restart, rollback;
- managed policy templates.

### v0.4 Coding Adapters

- workspace locks;
- Git worktree/branch workflow;
- Codex adapter;
- Claude Code adapter;
- ACP/acpx adapter;
- verification and diff evidence;
- approval gates for push/merge/deploy.

### v0.5 Mesh And GUI

- Tailscale/headscale adapter for managed devices;
- RustDesk/MeshCentral/native GUI adapters;
- GUI consent and audit;
- browser/E2E evidence.

### v1.0 Public Skillkit

- stable MCP contract;
- stable enrollment and job protocol;
- signed releases;
- installable Agent Skills;
- production deployment guide;
- external security review;
- end-to-end demos for temporary Windows repair and managed coding.

## Acceptance Tests For The Perfect Ending

- A temporary Windows host can join from a single visible PowerShell command, run foreground-only, and leave no service behind.
- A managed Mac can receive a coding job, create a worktree, run Codex, return diff and tests, and require approval before push.
- A job with a tampered envelope is rejected by the host.
- A job outside workspace scope is rejected by the host.
- A non-allowlisted shell command is rejected.
- A package install job pauses for approval.
- A revoked host receives no further jobs.
- A gateway restart does not lose durable jobs or audit history.
- A host reconnect flushes local audit spool.
- A release binary fails bootstrap verification if checksum or signature is wrong.

## Final Definition Of Done

The project is complete when a competent third-party agent can install the skills and MCP server, enroll a temporary or managed host, run useful development work through bounded adapters, verify the evidence, and cleanly revoke access without ever needing broad hidden remote control.
