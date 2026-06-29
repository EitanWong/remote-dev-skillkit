# Perfect Ending Solution

Date: 2026-06-29

This document is the canonical final architecture lock and execution spec for
Remote Dev Skillkit. If another architecture document conflicts with this file,
this file wins until a new architecture decision explicitly replaces it.

Supporting documents have narrower jobs:

- [Final Closure Blueprint](FINAL_CLOSURE_BLUEPRINT.md) is the concise release
  summary.
- [Ultimate Closure Design](ULTIMATE_CLOSURE_DESIGN.md) is historical
  implementation detail and rationale.
- [Final System Design](FINAL_SYSTEM_DESIGN.md) is the broad product reasoning
  record.

This file defines the product, the open-source project, and Eitan's
Hermes/Lucky deployment.

The goal is not "remote control." The goal is safe, useful agent delegation to
real machines.

## One-Sentence Outcome

An operator can say:

```text
Lucky, use that approved machine to solve this.
```

The system responds with typed intent, signed bounded execution, host-side
verification, approval gates, workspace isolation, evidence, audit, and
revocation.

## Final Product Shape

Remote Dev Skillkit ships one small safety kernel and many replaceable edges.

```text
Agent runtimes
  Hermes/Lucky, Codex, Claude Code, OpenCode, Cursor-style agents
        |
        v
Skillkit + MCP/API
  typed workflows, policy dry-runs, approval requests, evidence review
        |
        v
rdev-gateway
  tickets, hosts, jobs, approvals, leases, artifacts, audit, revocation
        |
        v
Outbound host channel
  HTTPS long-poll fallback, WSS production path, optional owned-host mesh
        |
        v
rdev-host
  identity, trust bundle, nonce store, local policy, locks, adapter runner
        |
        v
Adapters
  shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI,
  SSH, Tailscale/headscale, Coder, DevPod, devcontainers
```

The gateway coordinates. The host decides whether a job is safe to run locally.
Adapters are powerful tools, but they are never trust roots.

## Final Architecture Lock

The final system is a consent-first remote work control plane, not remote
control software. It must make useful remote action easy only after the action
has been converted into a typed, signed, policy-checked, locally verified,
auditable job.

```text
human/operator intent
  -> agent Skill/MCP workflow
  -> gateway policy dry-run
  -> signed host-bound job envelope
  -> outbound host lease
  -> host sovereignty guard
  -> adapter sandbox or visible session
  -> redacted evidence bundle
  -> hash-chained audit
  -> review, approval, continuation, cancellation, or revocation
```

The product boundary is deliberately narrow:

- `rdev` owns identity, consent, policy, signing, approval, execution bounds,
  evidence, audit, and revocation.
- Mature tools own their domains: Codex/Claude/ACP for coding, shell and
  PowerShell for bounded repair, Tailscale/headscale or SSH for optional owned
  host transport, RustDesk/MeshCentral or browser automation for explicit GUI,
  Coder/DevPod/devcontainers for disposable workspaces.
- Every mature tool is wrapped as an adapter. None of them becomes the
  authorization root.

The "perfect ending" is therefore not a bigger remote shell. It is the smallest
general kernel that makes many remote-development tools safe for agents to use.

## Final Closure Loops

The final architecture closes three loops. A release is incomplete if any loop
depends on agent narration instead of a verifier, artifact, or local host
decision.

| Loop | Question answered | Primary verifier |
|---|---|---|
| Connect | Is this host intentionally enrolled and reachable without exposing it? | ticket, join manifest, host identity, trust bundle |
| Execute | Is this specific action bounded, approved, and locally safe? | signed job envelope, host policy, workspace lock, approval token |
| Prove | Can another person or agent reconstruct what happened? | evidence package, checksums, audit chain, release verifier output |

### Connect Loop

Connect begins with an operator intent and ends with a host that can only receive
bounded work from a trusted gateway.

Required closure:

- temporary hosts enroll through short-lived, visible, outbound-only sessions;
- managed hosts enroll through explicit service install with stop and uninstall
  paths;
- join manifests and release metadata are signed by the correct trust roots;
- host identity and trust stores are local, rollback-protected, and revocable;
- no target host needs an inbound public listener to be useful.

### Execute Loop

Execute begins with a typed job request and ends with a structured terminal
state.

Required closure:

- agents submit typed intent through Skills, MCP tools, or the CLI bridge;
- the gateway performs a policy dry-run before signing;
- the job envelope is host-bound, expiring, nonce-protected, capability-scoped,
  workspace-scoped, and output-limited;
- the host repeats validation locally and can deny even if the agent or gateway
  made a bad request;
- adapters receive only prepared work and cannot authorize themselves;
- dangerous actions produce `rdev.approval-required.v1` before side effects.

### Prove Loop

Prove begins when work finishes or is refused and ends with exportable release
evidence.

Required closure:

- every success, denial, approval pause, failure, timeout, and cancellation has a
  schema-versioned artifact;
- transcripts, diffs, command output, release verifier output, no-persistence
  checks, and approval probes are redacted before archival;
- evidence manifests and checksums can be verified without trusting the agent;
- audit exports are hash-chained;
- release claims use package commands such as `rdev acceptance verify`,
  `rdev acceptance verify-windows-temporary`, and
  `rdev acceptance package-windows-temporary`.

This is the final product standard: connect deliberately, execute narrowly, and
prove independently.

## Final Control Plane Blueprint

The perfect ending has one control plane and many execution planes. The control
plane owns intent, consent, policy, signing, scheduling, approvals, evidence,
audit, and revocation. Execution planes own local facts: files, tools, OS
prompts, credentials, build caches, GUI state, and developer CLIs.

```text
agent intent
  -> typed workflow
  -> gateway governance
  -> host-bound signed work
  -> local host sovereignty
  -> adapter-specific execution
  -> evidence verifier
  -> operator or agent review
```

The separation is the architecture. If a feature collapses these planes, it is
not part of the final system.

| Plane | Owns | Must expose | Must never become |
|---|---|---|---|
| Agent workflow | intent, planning, explanation | typed tool calls and evidence review | standing host credential holder |
| Gateway governance | policy, tickets, jobs, approvals, audit | signed envelopes, leases, revocation, artifact indexes | remote shell |
| Host sovereignty | local verification and execution bounds | denials, approval pauses, evidence, local stop | blind gateway executor |
| Adapter edge | Codex, Claude, shell, GUI, mesh, workspace providers | conformance-tested plan/run/collect/cleanup | authorization root |
| Proof layer | release, acceptance, evidence, audit verification | machine-checkable pass/fail output | screenshot-only trust story |

This is why the system can stay general. New agents and new adapters attach to
stable protocol edges without receiving the power to rewrite the safety kernel.

## Final Product Cuts

The final design is allowed to be opinionated. It deliberately excludes several
tempting shortcuts:

- no internet-wide host discovery or network scanning;
- no hidden unattended mode for third-party temporary machines;
- no raw unrestricted shell as the agent-facing primitive;
- no gateway-only authorization without host-side verification;
- no service install that lacks status, stop, logs, uninstall, and revocation;
- no release artifact that cannot be independently verified before execution;
- no completion claim that cannot be reconstructed from evidence and audit;
- no Hermes-only assumption in public Skillkit, MCP contracts, or docs.

The open-source project should feel useful because it is narrow. It is a remote
work safety kernel, not another monolithic remote administration suite.

## Non-Negotiable Closure Contract

Every feature must preserve all of these statements:

1. Agents request typed work; they never receive raw host ownership by default.
2. The gateway signs only bounded, host-specific, expiring envelopes.
3. The host verifies every executable job independently of the agent and
   transport.
4. The host checks identity, trust bundle, signature, expiry, nonce,
   capabilities, workspace, approvals, and lock state before adapter execution.
5. High-risk operations pause before side effects and require scoped approval.
6. Every job ends as a structured denial, approval-required pause, or evidence
   bundle.
7. Temporary hosts are visible, foreground, TTL-bound, outbound-only, and
   non-persistent.
8. Managed hosts are explicitly installed, inspectable, stoppable,
   uninstallable, and revocable.
9. Bootstrap verifies signed release manifests and artifacts before running host
   code.
10. Completion claims require artifacts and audit, not agent narration.

If a feature cannot satisfy the contract, it belongs outside the core until it
is wrapped behind the safety kernel.

## Authority Map

No single actor receives all powers.

| Actor | Can do | Cannot do |
|---|---|---|
| Agent runtime | request tickets, jobs, evidence, and approvals through tools | approve its own danger, bypass policy, receive standing host credentials |
| Operator | approve hosts, scoped actions, revocation, and release decisions | bypass host-side validation |
| Gateway | issue tickets, sign bounded jobs, store artifacts, audit, revoke | execute on a host without host verification |
| Host runtime | verify and execute bounded work under local policy | broaden its own trust roots or policy |
| Adapter | run the declared operation inside limits | authorize itself, persist secretly, widen workspace, skip evidence |
| Release system | bless binaries and bootstrap artifacts | authorize runtime jobs or approvals |

This is the main defense against prompt injection, compromised clients, buggy
adapters, and ordinary operator mistakes.

## Subsystem Blueprint

The implementation should stay modular even if early deployments run as one
binary.

### Agent Package

Responsibilities:

- portable Skills for Hermes, Codex, Claude Code, OpenCode, and generic agents;
- MCP stdio and HTTP tool contracts;
- policy planning prompts and evidence-review workflows;
- local CLI bridge for environments that cannot host an HTTP MCP server.

Must not own:

- host credentials;
- standing approval authority;
- raw unrestricted terminal access.

### Gateway

Responsibilities:

- auth and operator sessions;
- ticket creation, join manifests, and host registry;
- policy dry-runs and capability planning;
- job envelope signing and key rotation;
- host leases, cancellation, and reconnect handling;
- approval ledger and one-use scoped approval tokens;
- artifact store, audit chain, evidence export, and redaction metadata;
- release catalog, signed bundle references, and bootstrap metadata;
- MCP/API surface for agent runtimes.

Must not own:

- local host execution;
- release signing keys by default;
- the ability to bypass host-side validation.

### Host Runtime

Responsibilities:

- local host key generation and protected identity storage;
- trust bundle storage, sequence checks, rollback rejection, and revocation;
- outbound HTTPS/WSS transport with long-poll fallback;
- local policy validation, nonce replay cache, and approval-token consumption;
- workspace locks, worktree preparation, and visible temporary-session stop;
- adapter supervisor, cancellation propagation, output caps, and timeouts;
- evidence collection, redaction, local spool, upload, and cleanup;
- managed-mode watchdog only when explicitly installed.

Must not own:

- broader authority than the approved gateway policy and local policy grant;
- hidden persistence in temporary mode;
- silent privilege escalation.

### Adapter SDK

Responsibilities:

- make one domain useful while obeying the kernel contract;
- expose deterministic capability detection and plan output;
- produce structured evidence for success, failure, denial, timeout, and
  cancellation.

Must not own:

- authorization;
- persistence;
- approval decisions;
- trust roots.

This split gives the project its open-source shape: anyone can add an adapter
without weakening the remote work safety kernel.

## Operating Modes

Modes are separate products in one kernel. They must stay visibly separate in
CLI, policy, audit, service lifecycle, and UX.

| Mode | Target | Persistence | Default authority | Hard stop |
|---|---|---:|---|---|
| `attended-temporary` | third-party or short-lived repair host | none | visible foreground, scoped, TTL-bound | TTL, local stop, ticket revoke |
| `managed` | Eitan-owned or formally managed host | explicit OS service | durable reconnect under approved roots | host revoke, policy revoke, service stop/uninstall |
| `break-glass` | urgent recovery | short-lived only | narrow emergency operations | shorter TTL, stronger approval, dense audit |
| `workspace-provider` | Coder, DevPod, devcontainers, cloud workspaces | provider-owned | bounded workspace lifecycle | workspace destroy or provider revoke |

Temporary mode never upgrades itself into managed mode. Managed mode gives
reliability, not standing approval for push, merge, deploy, publish, paid
actions, credential changes, service mutation, GUI control, or elevation.

## Core State Machines

### Ticket

```text
created -> issued -> joined -> approved -> active -> expired
                                      \-> revoked
```

Rules:

- Tickets are mode-bound and TTL-bound.
- A ticket creates an intent to enroll, not a right to execute arbitrary work.
- Revoking a ticket prevents future host joins and cancels queued/running jobs
  bound to that ticket where possible.

### Host

```text
registered -> pending_approval -> active -> draining -> revoked
                              \-> denied
```

Rules:

- The host registers a local public key and capability inventory.
- The operator approves capabilities and workspace roots.
- A host can be active only with current trust and non-revoked identity.
- Local stop wins over gateway scheduling.

### Job

```text
planned -> signed -> queued -> leased -> running -> succeeded
                                             |-> failed
                                             |-> denied
                                             |-> approval_required
                                             |-> canceled
```

Rules:

- Job creation, claim, artifact upload, completion, cancellation, and
  revocation are idempotent.
- A terminal canceled job may still receive cancellation evidence, but terminal
  state does not revert.
- Denial and approval-required are successful safety outcomes, not opaque
  failures.

### Approval

```text
requested -> approved -> embedded_in_envelope -> consumed
                |-> denied
                |-> expired
                |-> revoked
```

Rules:

- Approval tokens are one-use, scoped, expiring, host/job/operation-bound, and
  auditable.
- The host consumes approval only after all other validation passes.
- Agents cannot approve their own dangerous operations.

### Evidence

```text
spooled -> uploaded -> indexed -> verified -> retained
                       |-> redaction_failed
                       |-> checksum_failed
```

Rules:

- Evidence is the completion proof.
- Every evidence bundle includes schema version, envelope, policy decisions,
  adapter result, checksums, redaction metadata, and audit slice.
- Release claims require verifier output, not screenshots alone.

## Capability Rings

Policy is deny-by-default and shared by gateway dry-runs, host validation,
adapter planning, Skills, and evidence.

| Ring | Default posture | Examples |
|---|---|---|
| Ring 0 observe | allowed after host approval | capabilities, `git status`, read-only logs |
| Ring 1 workspace | allowed for approved roots | scoped reads/writes, tests, builds |
| Ring 2 repair | approval or narrow managed grant | dependency repair, process kill, package changes |
| Ring 3 privileged/visual | per-operation approval | elevation, screenshots, GUI control, service mutation |
| Ring 4 external consequence | approval after evidence review | push, merge, deploy, publish, paid action, credential change |

Ring 4 is never granted by "managed" status alone.

## Permission Model

"Maximum permission" means maximum permission that can be obtained safely and
explicitly, not maximum ambient authority.

The final permission model is:

1. Start with no execution permission.
2. Grant a ticket or managed policy only for a mode, reason, TTL, host, adapter,
   capability set, workspace/session boundary, and output limits.
3. Let the host independently narrow or reject the gateway grant.
4. Require approval tokens for privileged or externally consequential actions.
5. Let the local OS still show its normal prompts for elevation, TCC,
   credentials, screen control, firewall, service changes, or enterprise policy.
6. Record the approval, the side effect, and the evidence.

For temporary third-party machines, this means powerful repair is possible only
inside a visible foreground session. For managed Eitan-owned machines, durable
reconnect is possible, but push, merge, deploy, publish, paid actions,
credential changes, GUI control, service mutation, and elevation still require
fresh scoped approval.

## Protocol Objects

The stable v1 protocol family should be schema-versioned and behavior-versioned.
The JSON shape is not enough; expiry, replay, redaction, revocation, and audit
rules are part of the contract.

| Object | Owner | Purpose | Required rejection checks |
|---|---|---|---|
| `rdev.ticket.v1` | gateway | one enrollment intent | expired, revoked, wrong mode, reused when one-time |
| `rdev.join-manifest.v1` | gateway/release trust | signed bootstrap metadata | bad signature, wrong audience, stale sequence, wrong gateway |
| `rdev.host-registration.v1` | host | host key and capability inventory | missing ticket, duplicate misuse, unsupported mode |
| `rdev.trust-bundle.v1` | gateway trust authority | active keys and revocations | rollback, expired bundle, revoked key, wrong previous hash |
| `rdev.job-envelope.v1` | gateway | executable signed intent | tamper, wrong host, replay, expiry, missing capability |
| `rdev.job-lease.v1` | gateway/host | bounded claim of queued work | expired lease, wrong host, stale generation |
| `rdev.approval-token.v1` | gateway/operator | one scoped exception | wrong subject, expired, reused, broader scope |
| `rdev.host-denial.v1` | host | safe refusal with reason | never treated as an unstructured crash |
| `rdev.approval-required.v1` | host | pause before side effect | adapter side effect before token is a failure |
| `rdev.adapter-result.v1` | adapter | raw bounded outcome | missing schema, over limit, missing redaction metadata |
| `rdev.evidence-bundle.v1` | host/gateway | completion proof | checksum mismatch, missing manifest, unverifiable audit slice |
| `rdev.acceptance-package.*.v1` | acceptance tooling | release-ready real-environment evidence package | failed verifier, missing transcript, missing approval/no-persistence evidence, redaction gaps |
| `rdev.audit-chain.v1` | gateway/exporter | tamper-evident event history | broken chain, missing required event, changed event hash |
| `rdev.release-bundle.v1` | release system | signed artifact index | unsigned index, wrong digest, missing required artifact |

Every protocol object needs an owner, signer when applicable, expiry behavior,
revocation behavior, audit event, verifier command, and conformance fixture.

## Eitan Reference Deployment

Eitan's deployment is the golden reference, not a private fork.

```text
Hermes/Lucky
  -> Remote Dev Skillkit Skills
  -> MCP HTTP or local bridge
  -> https://api.lunflux.com/v1
  -> rdev-gateway
  -> tickets, hosts, jobs, approvals, artifacts, audit, signing
  -> https://agent.lunflux.com
  -> join page, signed manifests, release downloads, outbound host relay
  -> managed Eitan hosts and attended temporary hosts
```

Responsibilities:

- `https://api.lunflux.com/v1`: authenticated agent/operator API and
  MCP-compatible surface.
- `https://agent.lunflux.com`: human join, bootstrap, release download, signed
  manifest hosting, and outbound host relay.
- Hermes/Lucky: orchestration through typed tools, not host credentials.
- `rdev-gateway`: governance, policy, signing, approvals, evidence, audit, and
  revocation.
- `rdev-host`: local verification, execution, stop control, and audit/evidence
  spool.

The production boundary is:

| Surface | Public role | Authentication | Notes |
|---|---|---|---|
| `https://api.lunflux.com/v1` | agent/operator API and MCP-compatible HTTP | operator/session/OAuth-style tokens | no human bootstrap scripts here |
| `https://agent.lunflux.com` | join page, downloads, relay, release metadata | ticket, host identity, channel auth | no unrestricted admin API here |
| local MCP stdio | single-machine development bridge | local process boundary | useful for Codex/Claude local installs |
| `rdev` CLI | diagnostics, release/evidence verification, service lifecycle | local operator auth where needed | should prove every release claim |

`api.lunflux.com` and `agent.lunflux.com` may share infrastructure at first, but
their responsibilities must stay separable for later hardening.

## Discovery And Connection Model

The system must not discover machines by scanning the public internet. Discovery
is intentional and consent-based.

| Host class | Discovery path | Connection path |
|---|---|---|
| Temporary third-party host | operator creates short ticket; local user opens join page or runs one visible command | host connects outbound to `agent.lunflux.com` over 443 |
| Eitan managed host | explicit service install enrolls host identity and capability inventory | service reconnects outbound and refreshes trust |
| Workspace provider | operator creates disposable workspace through adapter | provider lifecycle is wrapped behind signed jobs |
| Owned mesh host | optional inventory from Tailscale/headscale adapter | mesh assists transport but does not authorize jobs |

There are no inbound ports on temporary hosts. Managed hosts may use mesh or SSH
only as optional transports after enrollment, and still require signed jobs and
host-side verification.

## One-Command Temporary Host Experience

The final Windows temporary support path should feel like this:

1. Operator creates an attended ticket with reason, TTL, requested capabilities,
   and gateway identity.
2. Local user opens `https://agent.lunflux.com/j/<code>`.
3. Join page shows operator, reason, TTL, requested capability rings, server
   identity, stop instructions, and a visible command.
4. Bootstrap downloads only to a temporary location.
5. Bootstrap verifies a pinned verifier, signed release bundle or manifest,
   host binary digest, signature, and platform policy before execution.
6. Host starts foreground, displays local stop instructions, generates a
   session key, registers capabilities, and waits for host approval.
7. Lucky runs bounded diagnosis and repair jobs.
8. Elevation, package install, GUI control, service mutation, destructive
   actions, credentials, push, merge, deploy, publish, or paid actions pause for
   approval.
9. Revocation or TTL ends the session.
10. No service, scheduled task, Run key, startup shortcut, firewall rule, or
    hidden restart remains.

The one command may be convenient, but the script must be inspectable and the
binary must be verified before execution.

Implementation constraints:

- Windows bootstrap targets PowerShell 5.1 and .NET already present on Windows
  10/11; it must not require Node, Python, Go, Git, package managers, or
  permanent execution-policy changes.
- The bootstrap downloads to a temporary directory, verifies the pinned
  `rdev-verify.exe`, verifies the signed release bundle or host release
  manifest, then starts `rdev-host.exe`.
- Dependency installation for repair work is a job-level action, not a
  bootstrap prerequisite, and therefore goes through policy and approvals.
- Temporary reconnect is bounded by ticket/session TTL and visible foreground
  process lifetime. If a user closes the session, local stop wins.
- No inbound ports are opened on the target. The host connects outbound to the
  relay over 443 and authenticates with its generated host key.
- "Only my server can access" is achieved by avoiding target inbound listeners,
  using host-bound signed envelopes, checking gateway trust bundles locally, and
  rejecting jobs from any untrusted gateway key.

## Managed Coding Experience

The final managed Mac/Linux/Windows coding path should feel like this:

1. Operator explicitly installs a managed service and reviews service label,
   identity store, trust store, logs, stop command, and uninstall command.
2. Host reconnects after login/reboot and refreshes trust.
3. Lucky selects host by policy snapshot, not hostname alone.
4. Gateway dry-runs policy and signs a Codex/Claude/ACP job bound to host,
   workspace, adapter, TTL, nonce, limits, and approvals.
5. Host validates envelope, trust, nonce, capabilities, approvals, workspace,
   symlinks, and lock state.
6. Host prepares a worktree or lock, runs the adapter, captures diff/tests/logs,
   redacts output, and uploads evidence.
7. Push, merge, deploy, publish, credential changes, service mutation, GUI
   control, and elevation require separate scoped approvals.
8. Operator or agent reviews evidence and decides continue, approve, cancel, or
   revoke.

Managed mode adds reliability, not invisibility:

- install, status, logs, stop, restart, and uninstall commands are generated and
  reviewable;
- macOS uses LaunchAgent first, Linux uses systemd, Windows uses Windows
  Service only after explicit managed enrollment;
- restart/watchdog behavior is allowed only for managed hosts;
- managed services reconnect with backoff, refresh trust bundles, flush local
  evidence/audit spools, and honor revocation;
- auto-update must verify signed release bundles and keep a rollback path.

## Skillkit And MCP Surface

The public agent surface teaches workflows, not raw power.

Required tools:

| Tool | Purpose |
|---|---|
| `rdev.ticket.create` | create temporary, managed, or break-glass enrollment intent |
| `rdev.host.list` | list hosts from a policy-aware registry snapshot |
| `rdev.host.approve` | approve host capabilities and workspace roots |
| `rdev.job.plan` | dry-run policy, host fit, approvals, and expected evidence |
| `rdev.job.run` | create a signed job envelope |
| `rdev.job.status` | inspect state, leases, denials, approvals, and artifacts |
| `rdev.job.cancel` | cancel queued or running work |
| `rdev.approval.create` | create scoped one-use approval tokens |
| `rdev.evidence.export` | export evidence bundles |
| `rdev.audit.verify` | verify hash-chained audit exports |
| `rdev.release.verify` | verify bootstrap and release artifacts |

Skills should instruct agents to:

- ask for the smallest useful capability;
- choose managed hosts for durable coding and temporary hosts for attended
  repair;
- prefer plan/dry-run before run;
- request approvals only with a clear reason and evidence;
- never push, merge, deploy, publish, or change credentials without approval;
- revoke temporary sessions when work is done;
- cite evidence bundle ids when claiming completion.

## Adapter SDK Contract

All adapters implement the same wrapper:

```text
detect(context) -> adapter_capabilities
plan(job, host_policy) -> required_capabilities, approvals, workspace_or_session_plan
prepare(job, locks, limits) -> prepared_workspace_or_session
run(job, cancellation, limits) -> events, raw_result
collect(job, raw_result) -> redacted_artifacts, checksums, evidence_manifest
cleanup(job, result) -> cleanup_status
```

Conformance tests must prove:

- no execution before envelope and host validation;
- deny-by-default capability mapping;
- workspace canonicalization and symlink escape rejection;
- approval-required operations pause before side effects;
- cancellation is honored where the underlying tool allows it;
- stdout, stderr, prompts, argv, diffs, files, and screenshots are capped and
  redacted;
- failure, timeout, denial, cancellation, and nonzero exit still produce
  structured evidence or denials;
- cleanup is visible and auditable.

Adapter priority:

1. `shell`, `powershell`, and `git`.
2. `codex` for Eitan's managed Mac coding path.
3. `claude-code` and `acp` for cross-agent compatibility.
4. `browser-e2e`, GUI, Coder, DevPod, devcontainers.
5. Tailscale/headscale, SSH, RustDesk, MeshCentral as optional adapters, not
   security roots.

## Reliability And Failure Model

| Failure | Required behavior |
|---|---|
| Host goes offline | lease expires; managed host may reconnect and flush spool; temporary host does not silently resurrect after TTL/stop |
| Gateway restarts | idempotent state recovery; jobs remain reconstructable from state and audit |
| Job cancellation | queued jobs stop; running jobs receive cooperative cancellation; collected evidence may append |
| Trust update rollback | host rejects stale sequence or wrong previous hash |
| Approval reuse | host rejects consumed approval token |
| Workspace contention | host denies second writer unless separate worktree exists |
| Redaction failure | job cannot claim clean success without redaction metadata |
| Artifact tamper | verifier fails checksum and release gate fails |
| Release compromise | revocation and key rotation path blocks future bootstrap |

Robustness is allowed to restart managed services. It is not allowed to create
hidden persistence on temporary machines.

## Release And Bootstrap Trust

The release path is part of the architecture, not packaging polish.

Required chain:

```text
release key
  -> signed release bundle index
  -> signed artifact manifests
  -> SHA-256 and size for every artifact
  -> pinned standalone verifier
  -> host binary execution
```

Release candidates add one more local gate before any external publication:

```text
built artifacts
  -> signed artifact manifests
  -> signed release bundle
  -> verified Skillkit bundle
  -> checksums
  -> rdev.release-candidate.v1 summary
  -> human or CI decision to publish
```

Rules:

- the bootstrap must fail closed on missing verifier, missing required artifact,
  bad hash, bad signature, wrong platform, revoked release key, or rollback;
- the standalone verifier must be small enough to audit and hash-pin in
  bootstrap scripts;
- a release candidate is local evidence, not publication; GitHub Release,
  package registries, and public downloads require a separate operator decision;
- public Windows releases should add Authenticode, but Authenticode does not
  replace the `rdev` release bundle verification contract;
- release keys, gateway job-signing keys, trust-bundle keys, and approval-token
  keys are separate authorities;
- every public release should archive verifier output, checksums, signed
  manifests, signed bundle index, SBOM when available, redacted acceptance
  packages, and verifier transcripts.

## Data And Storage

| Data | First production | Scale target |
|---|---|---|
| Gateway state | SQLite with backups | Postgres-compatible schema |
| Artifacts | local filesystem with quotas | S3-compatible storage |
| Audit | append-only JSONL plus hash-chain verifier | append-only exportable store |
| Gateway signing keys | locked file or OS store | KMS/HSM option |
| Host identity | file-backed development mode | Keychain, DPAPI, libsecret, TPM where available |
| Host trust | signed file-backed bundle | signed updates, rollback protection, revocation |
| Approval consumption | gateway state plus host store | durable one-use scoped token ledger |

The data model should remain single-operator friendly first, but schema choices
must not prevent future hosted or team deployments.

## Public Release Shape

The open-source project ships five deliverables:

| Deliverable | Purpose |
|---|---|
| `rdev` CLI | local demo, diagnostics, operator workflows, service lifecycle, release/evidence verification |
| `rdev-gateway` | self-hosted control plane and MCP/API server |
| `rdev-host` | cross-platform temporary and managed runtime |
| Skillkit bundle | portable Skills and MCP contracts for Hermes, Codex, Claude Code, OpenCode, and generic agents |
| Adapter SDK and conformance suite | safe extension point for new execution backends |

Hermes/Lucky is the reference environment, not a required dependency.

## Final Release Ladder

The project should advance through release ladders rather than broad rewrites.
Each rung has one proof artifact that can be archived and independently checked.

| Rung | Release promise | Required proof |
|---|---|---|
| `v0.1` Local safety kernel | typed jobs, signed envelopes, host validation, denials, approvals, evidence, audit, Skillkit, release candidates | `go test ./...`, `rdev skillkit verify`, `rdev release prepare-candidate`, audit/evidence verifier output |
| `v0.2` Temporary Windows help | one visible command, outbound-only, verified binary, approval pauses, no persistence | clean Windows 10/11 transcript plus `rdev.acceptance-package.windows-temporary.v1` |
| `v0.3` Managed Mac coding | explicit LaunchAgent, reconnect, locked-worktree Codex run, diff/tests, approvals | service-backed `rdev.acceptance.managed-mac.v1` plus `rdev acceptance verify` |
| `v0.4` Managed device generalization | macOS, Windows Service, systemd, trust storage, reconnect, adapter SDK | multi-OS acceptance matrix and adapter conformance reports |
| `v1.0` Public self-hosted Skillkit | stable schemas, self-host docs, signed releases, installer trust, threat model match | release candidate, signed downloads, acceptance transcripts, security checklist, verifier logs |

This ladder prevents the project from declaring "universal remote development"
before the boring parts are proven: installers, revocation, evidence, and
failure behavior.

## v1.0 Evidence Gates

`v1.0` is reached when these gates pass with saved transcripts:

1. Clean Windows 10/11 temporary host joins from one visible verified command,
   connects outbound only, runs bounded repair, enforces approvals, revokes
   cleanly, leaves no persistence, and exports
   `rdev.acceptance-package.windows-temporary.v1` evidence.
2. Eitan's managed Mac reconnects after reboot, receives a Lucky-requested Codex
   job, locks a Git worktree, returns diff/test/cancellation evidence, and
   requires approval before push, merge, deploy, credentials, or service changes.
3. Tampered, expired, wrong-host, wrong-key, replayed, non-allowlisted,
   missing-capability, workspace-escaping, and unsigned-release flows are
   rejected host-side with structured artifacts.
4. Revocation of tickets, hosts, jobs, approvals, and keys prevents future work
   and cancels queued/running work where possible.
5. Evidence bundles and hash-chained audit exports let another human or agent
   reconstruct what happened.
6. Built-in adapters pass conformance tests and cannot bypass the safety kernel.
7. Skillkit export installs cleanly into mainstream agent runtimes without
   Hermes-specific assumptions.
8. Threat model, release key lifecycle, security policy, public docs, and
   acceptance transcripts match shipped behavior.

## Remaining Gap Ledger

The architecture is locked, but the implementation is not finished. These are
the remaining gaps that matter before public confidence:

| Gap | Why it matters | Finish line |
|---|---|---|
| Candidate verification after download | publishers can prepare candidates, but consumers and CI need a single candidate verifier | `rdev release verify-candidate --candidate <dir|json>` validates summary, checksums, signed bundle, manifests, artifacts, and Skillkit |
| Real Windows temporary run | the plan, verifier, bootstrap, and packaging exist; the user-facing promise needs a clean VM transcript | clean Windows 10/11 run archived as `rdev.acceptance-package.windows-temporary.v1` |
| Real managed Mac service run | LaunchAgent planning/control exists; durable reconnect must be proven outside a dry-run | service-backed run survives logout/reboot, completes Codex job, verifies evidence, stops and uninstalls |
| Production host trust lifecycle | development trust stores exist; managed fleets need authenticated updates and rollback-safe storage | OS-protected host identity/trust stores, authenticated trust refresh, revocation propagation |
| Production transport | long-poll works as fallback; WSS/mTLS is the durable production channel | WSS host channel with lease semantics and HTTPS fallback |
| Adapter SDK extraction | adapters are still mostly internal implementations | public SDK, fixtures, conformance tests, and docs for Codex, Claude Code, ACP, shell, PowerShell |
| Public release automation | local release candidates exist; publication still needs safe automation | dry-run GitHub release script first, explicit approval before external mutation, archived release evidence |

Anything not in this ledger is secondary until these are closed.

## Final Implementation Order

Finish in this order:

1. Add standalone release candidate verification so downloaded or staged
   candidates can be checked without trusting the preparer.
2. Prove temporary Windows acceptance end-to-end: signed release, foreground
   console, outbound-only host loop, no-persistence inspection, approval probes,
   revoke.
3. Prove real service-backed managed Mac acceptance: plan, start, inspect,
   login/reboot reconnect, locked-worktree Codex run, verify evidence, stop,
   uninstall.
4. Production trust lifecycle: authenticated trust updates, revocation
   propagation, OS-protected host identity and trust storage.
5. WSS host channel with HTTPS long-poll fallback and clear lease semantics.
6. Adapter SDK extraction and public conformance fixtures.
7. Claude Code and ACP adapters.
8. Windows Service, systemd managed modes, auto-update, and rollback.
9. Signed public releases, platform signing, security policy, release transcript
   packaging, and open-source launch.

## Architecture Acceptance Test

Before merging any feature, ask:

1. Is the agent requesting typed work instead of raw access?
2. Is execution bound to a signed, host-specific, expiring envelope?
3. Can the host reject the job if the client, gateway, adapter, or transport
   behaves badly?
4. Are capabilities, workspace, limits, approvals, and redaction explicit?
5. Does the action produce evidence and audit sufficient for a reviewer?
6. Can the operator revoke ticket, host, job, approval, or key and see the
   result?
7. Does temporary mode stay visible, foreground, outbound-only, and
   non-persistent?
8. Does managed mode stay explicit, inspectable, stoppable, uninstallable, and
   revocable?

Any "no" means the feature is not part of the perfect ending yet.

## Final Definition Of Done

The project reaches its perfect ending when Remote Dev Skillkit becomes the
portable safety layer agents use before touching real machines:

- Eitan can use Lucky/Hermes against `api.lunflux.com/v1` and
  `agent.lunflux.com`.
- A third-party Windows machine can be helped quickly without hidden
  persistence or inbound exposure.
- Eitan-owned managed hosts can reconnect reliably without gaining ambient
  authority.
- Codex, Claude Code, ACP, shell, PowerShell, GUI, mesh, Coder, and DevPod are
  adapters behind one safety kernel.
- Other agent users can install the Skillkit and self-host the gateway without
  adopting Hermes.
- Every important claim is backed by evidence, audit, and verifier output.

That is the final architecture: not bigger control, but better delegation.
