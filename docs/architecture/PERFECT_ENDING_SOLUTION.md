# Perfect Ending Solution

Date: 2026-06-29

This document is the final architecture closure solution for Remote Dev
Skillkit. It does not replace [Ultimate Closure Design](ULTIMATE_CLOSURE_DESIGN.md);
it turns that design into an execution spec for the product, the open-source
project, and Eitan's Hermes/Lucky deployment.

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
5. Bootstrap verifies a pinned verifier, release manifest, host binary digest,
   signature, and platform policy before execution.
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

## v1.0 Evidence Gates

`v1.0` is reached when these gates pass with saved transcripts:

1. Clean Windows 10/11 temporary host joins from one visible verified command,
   connects outbound only, runs bounded repair, enforces approvals, revokes
   cleanly, and leaves no persistence.
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

## Final Implementation Order

Finish in this order:

1. Real service-backed managed Mac acceptance: plan, start, inspect, reboot or
   login reconnect, locked-worktree Codex run, verify evidence, stop, uninstall.
2. Windows temporary bootstrap acceptance: signed release, foreground console,
   outbound-only host loop, no-persistence inspection, approval probes, revoke.
3. Production trust lifecycle: authenticated trust updates, revocation
   propagation, OS-protected host identity and trust storage.
4. WSS/mTLS host channel with HTTPS long-poll fallback.
5. Adapter SDK extraction and public conformance fixtures.
6. Claude Code and ACP adapters.
7. Windows Service and systemd managed modes.
8. Signed public releases, platform signing, security policy, release transcript
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
