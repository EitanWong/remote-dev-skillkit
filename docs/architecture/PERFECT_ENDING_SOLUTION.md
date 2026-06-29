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

The **Final Architecture Closure** section below is the decision header. The
dated layers that follow are retained as rationale and implementation detail,
but future architecture changes should either preserve this closure or replace
it with an explicit architecture decision.

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

## Final Architecture Closure - 2026-06-30

This is the final perfect-ending design. It deliberately stops the project from
becoming a bigger remote-control stack, a tunnel manager, a coding CLI wrapper,
or a Hermes-only private integration.

Remote Dev Skillkit is a **permissioned delegation operating layer** for agentic
development work. Its value is the verified space between "an agent wants
something done" and "a real machine safely did it."

### Closure Thesis

The whole system has one shape:

```text
operator intent
  -> agent Skill/MCP plan
  -> gateway policy dry-run
  -> signed host-bound job envelope
  -> outbound host lease
  -> host sovereignty validation
  -> adapter lifecycle execution
  -> evidence bundle and audit chain
  -> review, approval, cancel, revoke, rotate, or continue
```

Every feature must map to this shape. If it cannot, it belongs outside the core
project.

### Seven Invariants

These are the final design laws:

1. Agents get typed tools, never standing host credentials.
2. Gateways govern work, but never execute local host work.
3. Hosts execute work, but never invent authority.
4. Adapters run tools, but never approve themselves or install persistence.
5. Temporary support is foreground, visible, TTL-bound, outbound-only, and
   non-persistent.
6. Managed support adds reconnect and recovery, not ambient permission.
7. No success claim is final until evidence, audit, and the relevant verifier
   can reconstruct it.

### Final Product Surfaces

The open-source project ships five surfaces and refuses to make any single edge
special:

| Surface | Final responsibility | Must stay replaceable |
|---|---|---|
| `rdev` | operator CLI, diagnostics, local demos, service lifecycle, evidence and release verification | deployment brand and agent runtime |
| `rdev-gateway` | tickets, hosts, jobs, policy, approvals, leases, artifacts, audit, revocation, signing, MCP/API | storage backend and hosting provider |
| `rdev-host` | attended temporary and explicit managed host runtime | transport, adapter set, and OS service manager |
| Skillkit bundle | portable instructions and MCP schemas for agents | Hermes, Codex, Claude Code, OpenCode, and future agent surfaces |
| Adapter SDK | safe extension path for Codex, Claude Code, ACP, shell, PowerShell, GUI, mesh, and workspace providers | concrete execution backend |

Hermes/Lucky and Lunflux are the reference deployment, not the dependency
boundary.

### The Six Closed Loops

A path is complete only when its loop closes with machine-readable proof:

| Loop | Opens with | Closes with |
|---|---|---|
| Distribution | bootstrap, install, release, or update instruction | signed release bundle verification, checksums, platform match, rollback policy |
| Enrollment | ticket, join page, bootstrap command, or managed install | host identity, capability inventory, trust bundle, operator approval, revocation handle |
| Authorization | agent intent or API/MCP request | policy dry-run plus signed host-bound expiring job envelope |
| Execution | host lease or claimed job | local trust, nonce, mode, capability, approval, workspace, lock, quota, and revocation checks |
| Proof | adapter result, denial, cancellation, or approval pause | evidence bundle, artifact index, redaction metadata, audit slice, verifier output |
| Recovery | timeout, crash, offline host, compromise, or user stop | lease expiry, local stop, spool replay, cancellation evidence, revoke, rotate, uninstall |

The perfect ending is not a demo where work succeeds. It is a product where
success, denial, cancellation, tamper, revoke, and uninstall all have a visible
and verifiable path.

### Three Golden Paths

The project should optimize for three golden paths before adding broad
secondary adapters.

#### 1. Attended Temporary Windows Repair

Someone runs one visible PowerShell command. The command downloads a bootstrap
and standalone verifier, verifies the signed release bundle, starts a foreground
host, connects outbound to the approved gateway, accepts only host-bound signed
jobs, asks for approval before risky effects, writes a transcript, and exits
without leaving a service, scheduled task, Run key, startup shortcut, firewall
change, or permanent execution-policy change.

This path is for short-lived help. It must feel easy, but it cannot be magical
or invisible.

#### 2. Managed Owned-Host Development

An explicitly installed managed host on Eitan-owned Mac, Windows, or Linux
reconnects after login or reboot, refreshes trust safely, locks a workspace or
worktree, runs Codex/Claude Code/ACP/shell/PowerShell through adapters, returns
diff/test/log evidence, and pauses before push, merge, deploy, publish,
credential changes, service management, GUI control, or elevation.

This path is for durable personal infrastructure. It may be reliable and
automatic; it may not become silently privileged.

#### 3. Third-Party Adapter Authoring

An adapter author scaffolds a lifecycle manifest, implements
`detect -> plan -> prepare -> run -> collect -> cleanup`, runs the runtime
lifecycle through `adapterkit.RunLifecycle`, and passes lifecycle, runtime,
result, cancellation, redaction, cleanup, and denial conformance before the
adapter is exposed to agents.

This path makes the open-source project broad without making the safety model
large.

### Eitan Reference Deployment

Eitan's personal ending is:

```text
Lucky on Hermes
  -> Remote Dev Skillkit Skill/MCP workflow
  -> https://api.lunflux.com/v1
  -> rdev-gateway
  -> https://agent.lunflux.com
  -> attended temporary hosts and explicitly managed owned hosts
```

`https://api.lunflux.com/v1` is the authenticated operator and agent API.
`https://agent.lunflux.com` is the human/host-facing edge for join, bootstrap,
release metadata, and relay. Lucky remains the coordinator; it does not need
root passwords, SSH passwords, permanent host credentials, or hidden
persistence.

### Final Close Order

From here, the project should close in this order:

1. Clean Windows temporary acceptance on a real Windows 10/11 machine.
2. Service-backed managed Mac acceptance with LaunchAgent start, reconnect,
   locked-worktree Codex job, evidence verification, stop, and uninstall.
3. Real Linux systemd reboot/reconnect acceptance.
4. Production host identity and trust storage with OS protection, rollback
   rejection, authenticated refresh, revocation propagation, and emergency
   rotation.
5. WSS/mTLS transport while preserving HTTPS long-poll as the universal
   fallback.
6. Production Adapter SDK integration for built-in hostrunner adapters and
   executable lifecycle/cancellation fixtures.
7. Claude Code and ACP adapters.
8. Windows Service managed mode.
9. Optional GUI, browser, mesh, Coder, DevPod, and workspace-provider adapters.
10. Public release only after signed releases, install verification,
    acceptance packages, threat model, security policy, and docs match shipped
    behavior.

This order is part of the architecture. It keeps the project from optimizing
for broad adapter demos before the safety-critical distribution, enrollment,
execution, proof, and recovery loops are real.

### Definition Of The Perfect Ending

The perfect ending is reached when the safest path is also the easiest path:

- Lucky can coordinate real hosts without holding host credentials.
- Temporary machines can join fast, visibly, and without persistence.
- Managed hosts reconnect reliably without receiving ambient authority.
- All powerful tools are adapters behind the same signed-job and evidence
  contract.
- Self-hosted users can install without Hermes, Lunflux, macOS, GitHub, or
  Codex assumptions.
- Every important claim can be independently verified after the fact.

That is the final architecture: a small permissioned delegation layer around
powerful tools, designed so remote development becomes agent-native precisely
because it is consented, bounded, locally verified, revocable, and provable.

## Final Kernel Specification - 2026-06-30

This section is the implementation-level lock. It turns the final product
constitution into a small kernel that can accept or reject future design
changes.

### Kernel Thesis

Remote Dev Skillkit is a permissioned work-delegation kernel for agents. Its
core job is not to execute work itself; its core job is to make powerful
execution tools usable only through consent, scope, local verification,
evidence, audit, and revocation.

The kernel is complete only when these seven facts are machine-checkable:

1. who asked for the work;
2. which host intentionally joined;
3. what exact job was authorized;
4. why local host validation accepted or denied it;
5. what adapter did inside which boundary;
6. what evidence proves the result;
7. how the operator can stop, revoke, uninstall, rotate, or audit it.

### Minimal Core Objects

The final product should keep the protocol family small. New features should
prefer extending these objects over inventing parallel authority paths.

| Object | Purpose | Verifier expectation |
|---|---|---|
| `rdev.ticket.v1` | enrollment intent with mode, reason, TTL, and requested capability rings | expired or revoked tickets cannot enroll hosts |
| `rdev.host-registration.v1` | host identity, mode, capability inventory, and local stop contract | wrong ticket, duplicate misuse, or unsupported mode is rejected |
| `rdev.trust-bundle.v1` | active gateway keys, sequence, previous hash, and revocations | stale, rolled-back, wrong-root, or revoked keys are rejected |
| `rdev.job-envelope.v1` | host-bound executable intent with nonce, expiry, workspace/session, limits, and approvals | tamper, replay, wrong host, missing capability, or expired jobs are denied locally |
| `rdev.approval-token.v1` | one scoped exception for a dangerous operation | token reuse, broader scope, wrong subject, or expiry is rejected |
| `rdev.adapter-*.v1` | lifecycle, result, and cancellation evidence contracts | lifecycle, result, cancellation, redaction, and secret checks return structured reports |
| `rdev.evidence-bundle.v1` | reconstructable proof of the job outcome | checksums, artifact index, redaction metadata, and audit slice verify |
| `rdev.audit-chain.v1` | tamper-evident event history | missing or changed events break verification |
| `rdev.release-bundle.v1` | signed software distribution index | required binaries, manifests, hashes, and signer are verified before execution |

The system may add transport formats, storage backends, UI flows, or adapters,
but those additions must map back to these objects.

### Replaceable Edges

The core should stay boring so the edges can be creative.

| Edge | Examples | Rule |
|---|---|---|
| Agent runtime | Hermes/Lucky, Codex, Claude Code, OpenCode, generic MCP clients | agents receive typed tools, not standing host credentials |
| Transport | HTTPS long-poll, WSS/mTLS, optional owned-host mesh | transport delivers leases; it never authorizes work by itself |
| Adapter | shell, PowerShell, Codex, Claude Code, ACP, browser, GUI, Coder, DevPod | adapters execute one domain and cannot self-approve |
| Storage | local files, SQLite, Postgres, object store, KMS/HSM | storage persists state but does not become an authority |
| Distribution | GitHub Releases, self-hosted downloads, enterprise mirrors | release verification runs before target-host execution |

This is the final buy-vs-build boundary: reuse mature tools for execution and
connectivity, but keep consent, policy, signing, approval, evidence, and
revocation in `rdev`.

### Acceptance Matrix

Every release promise needs one archived proof package.

| Promise | Required proof |
|---|---|
| temporary Windows support | clean Windows 10/11 transcript, verified release bundle, outbound-only foreground host, approval probes, revocation, and no-persistence checks |
| managed Mac coding | service-backed LaunchAgent run, reconnect proof, locked-worktree Codex job, diff/test evidence, approval-required proof, stop, and uninstall |
| managed Linux support | systemd user-unit install/start/status/reboot/reconnect/stop/uninstall transcript and evidence spool proof |
| adapter extensibility | lifecycle, result, and cancellation conformance reports for built-in and third-party adapters |
| public release | signed release bundle, release candidate verification, post-release download/install verification, Skillkit verification, threat model, and security checklist |

The project should never claim a path is complete when it only has a plan. A
plan is useful. A verifier-backed transcript is the gate.

### Final Product Boundary

The final open-source project ships exactly five primary surfaces:

1. `rdev` operator CLI and verifier surface.
2. `rdev-gateway` self-hosted control plane and MCP/API server.
3. `rdev-host` temporary and managed host runtime.
4. Skillkit bundle for agent runtimes.
5. Adapter SDK and conformance suite.

Everything else is optional packaging, adapter work, or deployment-specific
integration. This keeps the public project universal instead of Hermes-only,
Lunflux-only, macOS-only, GitHub-only, or Codex-only.

## Final Product Constitution - 2026-06-30

This is the final architecture constitution. It does not replace the detailed
sections below; it makes them executable as one product contract. When future
features are proposed, they should be accepted only if they preserve this
constitution.

### Final Decision

Remote Dev Skillkit is an agent work delegation kernel. It is not remote
administration software, not a tunnel product, not a coding CLI, and not a
general computer-control framework.

The product exists to turn natural-language agent requests into bounded,
host-verified, approval-aware, evidence-producing work on real machines.

```text
intent
  -> typed plan
  -> policy dry-run
  -> signed host-bound envelope
  -> outbound host lease
  -> host sovereignty validation
  -> adapter lifecycle
  -> structured evidence
  -> audit and review
  -> continuation, approval, cancellation, or revocation
```

The "perfect ending" is reached when the safest path is also the easiest path
for agents, operators, temporary users, managed-host owners, and adapter
authors.

### Four Planes

The final system is separated into four planes. A feature that collapses these
planes is outside the core product.

| Plane | Owns | Primary artifacts | Hard boundary |
|---|---|---|---|
| Control plane | tickets, hosts, jobs, policy, approvals, leases, revocation | `rdev.ticket.v1`, `rdev.job-envelope.v1`, `rdev.approval-token.v1` | never executes local host work |
| Execution plane | host identity, trust, local validation, locks, adapter supervision | `rdev.host-denial.v1`, `rdev.approval-required.v1`, adapter result artifacts | never invents authority |
| Proof plane | evidence, audit, acceptance, redaction, verification | `rdev.evidence-bundle.v1`, `rdev.audit-chain.v1`, `rdev.acceptance-package.*.v1` | never relies on agent narration |
| Distribution plane | releases, bootstraps, checksums, bundle verification | `rdev.release-bundle.v1`, `rdev.release-candidate.v1`, verifier output | never authorizes runtime jobs |

This is the smallest stable kernel. Everything else attaches through adapters,
MCP tools, API clients, or deployment packaging.

### Reference Architecture

```text
Agent runtime
  Hermes/Lucky, Codex, Claude Code, OpenCode, generic MCP agent
        |
        v
Skillkit and MCP/API client
  typed intent, policy planning, approval request, evidence review
        |
        v
Gateway control plane
  auth, tickets, hosts, jobs, approvals, leases, signing, audit, artifacts
        |
        v
Outbound relay
  HTTPS long-poll fallback, WSS/mTLS production path
        |
        v
Host execution plane
  identity, trust bundle, nonce store, local policy, locks, local stop
        |
        v
Adapter lifecycle
  detect -> plan -> prepare -> run -> collect -> cleanup
        |
        v
Proof plane
  result artifact, evidence bundle, audit chain, verifier output
```

The gateway may be hosted by Lunflux, self-hosted by another user, or run
locally for development. The host rules do not change when the deployment
changes.

### Adapter Lifecycle Contract

The lifecycle-manifest, runtime-fixture, result-artifact, and cancellation
verifiers are the first public Adapter SDK slices. `adapterkit.RunLifecycle` is
the first executable lifecycle runner. The final production SDK must extend
those into hostrunner-integrated adapter wrappers:

| Phase | Adapter must provide | Kernel must enforce |
|---|---|---|
| `detect` | installed tool, version, OS support, declared capabilities | capability approval and host inventory freshness |
| `plan` | intended commands, files, external consequences, expected evidence | policy dry-run and approval calculation |
| `prepare` | workspace, worktree, session, temp files, dependency checks | canonical paths, locks, TTL, storage quotas |
| `run` | bounded execution with cancellation and timeout support | context cancellation, output caps, no privilege widening |
| `collect` | schema-versioned result, diff/test/log evidence, redaction metadata | conformance verification and artifact indexing |
| `cleanup` | lock release, temp cleanup, process cleanup, spool state | idempotent retry and visible failure evidence |

An adapter can be powerful, but it cannot authorize itself. Claude Code, Codex,
ACP, shell, PowerShell, Git, GUI, browser, mesh, Coder, DevPod, and future
execution backends all pass through this same lifecycle.

### Permission Lattice

The final permission model is not "all or nothing." It is a lattice with
monotonic escalation and revocation:

| Level | Meaning | Examples | Approval rule |
|---|---|---|---|
| L0 observe | read-only fact gathering | capability inventory, `git status`, logs | host approval is enough |
| L1 workspace | scoped project changes | edits, tests, builds, local generated files | workspace approval and lock required |
| L2 repair | machine-local repair | package repair, process cleanup, cache reset | scoped approval unless pre-granted managed policy |
| L3 privileged/visual | OS-level or GUI interaction | elevation, service changes, screen/GUI control | fresh per-operation approval |
| L4 external consequence | effects outside the host | push, merge, deploy, publish, paid action, credentials | approval after evidence review |

Managed mode may remember a host and reconnect it. Managed mode does not grant
L3 or L4 by default.

### Mode Invariants

| Mode | Non-negotiable invariant |
|---|---|
| `attended-temporary` | foreground, visible, outbound-only, TTL-bound, no service, no autorun, local stop wins |
| `managed` | explicit install, status/logs/start/stop/uninstall, durable reconnect, host-side validation unchanged |
| `workspace-provider` | provider lifecycle is bounded by signed jobs and destroyable workspaces |
| `break-glass` | shorter TTL, narrower scope, stronger approval, denser audit, no permanent weakening |

The same binary can support multiple modes, but the CLI, policy, audit, and UX
must keep them visibly distinct.

### Data And Storage Contract

The production system needs durable stores, but no store is allowed to become a
hidden authority.

| Store | Contents | Required property |
|---|---|---|
| Gateway relational store | tickets, hosts, jobs, approvals, leases, artifact indexes | idempotent transitions and recovery after restart |
| Gateway audit store | append-only events and hash-chain exports | tamper-evident export and verifier support |
| Object store | evidence, transcripts, diffs, release and acceptance packages | checksum-addressed, quota-bound, redacted |
| Gateway key store | job signing, approval signing, trust-bundle signing references | rotation, revocation, separation from release signing |
| Host protected store | host identity, trust bundle, nonce cache, approval consumption | rollback rejection and OS protection in production |
| Host spool | not-yet-uploaded evidence and cancellation artifacts | retryable upload without false success |

Development may use local files. Production must harden the same logical stores
without changing protocol semantics.

### API And MCP Contract

The public API and MCP surface should stay small:

1. `plan` before `run`.
2. structured approval requests before dangerous effects.
3. signed envelopes rather than raw commands.
4. host-side denials as first-class results.
5. evidence export before completion claims.
6. verifier tools for releases, Skillkits, adapter results, acceptance, and
   audit.

Agents get tools for intent, planning, approval, execution, evidence review, and
verification. They do not get standing host credentials or self-approval power.

### Eitan Reference Ending

The personal deployment is:

```text
Hermes/Lucky
  -> Remote Dev Skillkit
  -> https://api.lunflux.com/v1
  -> rdev-gateway
  -> https://agent.lunflux.com
  -> managed Eitan hosts and attended temporary hosts
```

`https://api.lunflux.com/v1` is the authenticated agent/operator API. It owns
typed jobs, policy planning, approval, evidence, audit, and MCP-compatible
access.

`https://agent.lunflux.com` is the human and host-facing edge. It owns join
pages, release metadata, verified bootstrap downloads, and outbound host relay.

Lucky remains the coordinator. It should never need root passwords, SSH
passwords, permanent host credentials, or hidden persistence on someone else's
machine.

### v1.0 Definition

The public v1.0 release is allowed only when all of the following are true:

1. A clean Windows 10/11 temporary host joins from one visible verified command,
   connects outbound only, enforces approvals, revokes cleanly, and leaves no
   service or autorun persistence.
2. A managed Mac runs a real service-backed coding acceptance: start, inspect,
   reconnect, locked-worktree Codex job, evidence verification, stop, and
   uninstall.
3. A managed Linux host proves systemd user-unit reboot/reconnect acceptance.
4. Host identity and trust storage have production OS protection and rollback
   rejection.
5. WSS/mTLS exists while HTTPS long-poll remains the universal fallback.
6. The full Adapter SDK verifies lifecycle, result artifacts, cancellation,
   redaction, cleanup, and denial behavior.
7. Signed releases, release candidates, post-release install verification,
   Skillkit verification, acceptance packages, and audit-chain verification all
   produce machine-readable `ok=true` evidence.
8. The public threat model and security policy match shipped behavior.
9. Self-hosted users can install gateway, host, MCP tools, Skillkit, and
   adapters without Hermes or Lunflux assumptions.
10. The easiest documented workflow is also the safest workflow.

### Final Build Order

The remaining work should follow this order:

1. Real clean Windows temporary acceptance.
2. Real service-backed managed Mac acceptance.
3. Real Linux systemd reboot/reconnect acceptance.
4. Production host identity/trust storage and authenticated trust refresh.
5. WSS/mTLS transport with long-poll fallback preserved.
6. Full Adapter SDK lifecycle and cancellation conformance.
7. Claude Code and ACP adapters.
8. Windows Service managed mode.
9. Optional GUI, mesh, Coder, DevPod, and browser adapters.
10. Approved public release with signed artifacts, verified Skillkit, acceptance
    packages, security docs, and release notes.

This order is the final answer because it prevents the project from optimizing
for demos before it proves the safety-critical host, release, and evidence
loops.

## Final 2026-06-30 Refinement

This section is the final architecture decision layer. It resolves the last
product ambiguity: Remote Dev Skillkit is not a remote administration suite,
not a tunnel manager, and not a coding CLI replacement. It is the safety layer
that lets agents use real machines through explicit consent, bounded authority,
local verification, and reviewable proof.

### Final Thesis

The perfect ending is a permissioned delegation fabric:

```text
operator intent
  -> agent Skill or MCP workflow
  -> gateway policy dry-run
  -> host-bound signed job
  -> outbound host lease
  -> local host sovereignty guard
  -> adapter execution
  -> evidence and audit
  -> review, continuation, approval, cancellation, or revocation
```

The core product owns the part mature tools do not solve for agents:

- consent and enrollment;
- typed intent instead of raw access;
- host-bound authorization;
- host-side validation;
- approval pauses before high-risk effects;
- workspace and session isolation;
- redaction, evidence, and audit;
- release trust and bootstrap verification;
- revocation across tickets, hosts, jobs, approvals, and keys.

Everything else is an adapter. Codex, Claude Code, ACP, shell, PowerShell, Git,
Tailscale, SSH, Coder, DevPod, browser automation, RustDesk, MeshCentral, and
future tools are useful execution edges. None of them becomes the trust root.

### Golden Path

The stable golden path has four actors and one proof trail:

| Actor | Final role | Must never become |
|---|---|---|
| Agent | plan, request, explain, review evidence | holder of standing host credentials or self-approval power |
| Gateway | govern tickets, hosts, jobs, approvals, leases, artifacts, audit, and signing | remote shell or hidden executor |
| Host | verify local facts and execute bounded jobs | blind worker that trusts agent narration |
| Adapter | run one domain tool under limits | authorization, persistence, or approval authority |
| Proof trail | make every claim reconstructable | screenshot-only or log-only trust story |

This is the final product shape: agents ask, gateways sign, hosts decide, adapters
run, evidence proves.

### Product Lines

The open-source release should remain five installable surfaces:

| Surface | Purpose | First-class user |
|---|---|---|
| `rdev` | operator CLI, local demos, diagnostics, evidence/release verification, service lifecycle | human operator and project maintainer |
| `rdev-gateway` | self-hosted API, MCP, tickets, hosts, jobs, approvals, relay, artifacts, audit, revocation | self-hoster or Lunflux deployment |
| `rdev-host` | target-machine runtime for attended temporary and explicit managed modes | local machine owner or managed-device operator |
| Skillkit bundle | portable agent instructions and MCP tool contracts for Hermes, Codex, Claude Code, OpenCode, and generic agents | agent runtime maintainer |
| Adapter SDK | conformance-tested extension point for execution backends | tool/adaptor author |

Hermes/Lucky is the reference deployment. The public project must not require
Hermes, Lunflux, macOS, GitHub, or any single coding CLI.

### Mode Separation

Modes are separate safety products sharing one kernel. They must not blur.

| Mode | Who it is for | Persistence | Strongest allowed promise |
|---|---|---:|---|
| `attended-temporary` | third-party or short-lived repair machine | none | visible, outbound-only, TTL-bound help |
| `managed` | Eitan-owned or formally managed machine | explicit service | durable reconnect and governed coding |
| `workspace-provider` | disposable cloud or devcontainer workspace | provider-owned | lifecycle-wrapped ephemeral workspace |
| `break-glass` | urgent recovery under pressure | short-lived only | narrower TTL, stronger approvals, denser audit |

Temporary mode never installs hidden persistence, never opens public inbound
ports, and never upgrades itself into managed mode. Managed mode adds recovery
and reconnect behavior, not automatic permission to push, merge, deploy,
publish, mutate services, control GUI, change credentials, or elevate.

### Final Trust Boundary

No feature may collapse these authorities:

| Authority | Signs or proves | Cannot grant |
|---|---|---|
| Release key | software artifacts and release bundles | runtime job authorization |
| Gateway job key | host-bound job envelopes | release trust, host identity, or operator approval |
| Trust-bundle key | active gateway keys and revocations | adapter execution by itself |
| Approval key | one-use scoped exceptions | broad host access or future approvals |
| Host identity key | local machine identity and registration continuity | gateway authority |
| Audit chain | event integrity | permission to act |

The system stays safe because compromise of one authority does not grant all
three powers of publishing software, enrolling hosts, and authorizing execution.

### Protocol Closure

Every supported path must close six loops:

| Loop | Opens with | Closes only when |
|---|---|---|
| Distribution | bootstrap or install instruction | release verifier accepts signed bundle, hashes, platform, and rollback policy |
| Enrollment | ticket or managed install | host identity, capability inventory, trust bundle, and operator approval are recorded |
| Authorization | typed agent job request | gateway signs a host-bound, expiring, scoped envelope after policy dry-run |
| Execution | host lease | host independently validates trust, nonce, approvals, workspace, capabilities, mode, and revocation |
| Proof | adapter result or denial | evidence bundle and audit slice verify without trusting narration |
| Recovery | failure, cancel, offline, or compromise | lease expiry, local stop, spool replay, revoke, rotation, or uninstall is visible and auditable |

A feature that demonstrates only the happy path is not complete. A feature is
complete when its denial, revocation, timeout, cancellation, tamper, and
redaction behavior can also be verified.

### One-Command Host Rule

The one-command host experience must be fast, but never magical. For Windows,
the target baseline is PowerShell 5.1 on clean Windows 10/11 with no Node, Go,
Git, Python, package manager, permanent execution-policy change, firewall
change, service install, scheduled task, Run key, or startup shortcut in
temporary mode.

The command may download a small bootstrap and a standalone verifier. It must
then verify the release bundle or host manifest before starting the host. Any
dependency installation needed for repair work is a job-level action and must
go through policy and approval gates.

This is how "only my server can access" is achieved:

- the target opens no public inbound listener;
- the host connects outbound to the relay over 443;
- the host accepts only trusted gateway keys from a signed trust bundle;
- every job is bound to the host identity and short expiry;
- replayed, tampered, wrong-host, wrong-key, stale, revoked, or broader-scope
  jobs are denied locally.

### Managed Coding Rule

Managed coding is the durable form of the same kernel. The service may reconnect
after login or reboot, but it still runs jobs only after signed-envelope and
host-side validation. Coding adapters must use workspace locks or prepared
worktrees, collect diffs and verification output, and pause before external
consequences.

The managed-host experience is complete only when install, status, logs, start,
stop, restart, trust refresh, evidence spool, and uninstall are inspectable.

### Final Implementation Spine

From this point, implementation should follow one spine rather than many broad
parallel ambitions:

1. Prove the real clean Windows temporary path from one visible verified
   command through no-persistence evidence.
2. Prove the real service-backed managed Mac path through LaunchAgent reconnect,
   locked-worktree Codex execution, evidence verification, stop, and uninstall.
3. Harden production host identity and trust with OS-protected storage,
   authenticated refresh, rollback rejection, revocation propagation, and
   emergency rotation.
4. Add WSS/mTLS transport while preserving HTTPS long-poll as the universal
   fallback.
5. Extract the Adapter SDK and make built-in adapters pass shared conformance
   fixtures.
6. Add Claude Code, ACP, PowerShell, Windows Service, systemd, workspace
   provider, mesh, and GUI adapters behind the same kernel.
7. Publish only after release artifacts, install verification, acceptance
   packages, threat model, security policy, and public docs match reality.

This order keeps the project honest: first prove the two golden paths, then
generalize.

### Perfect Ending Definition

The perfect ending is reached when the easiest documented path is also the safe
path:

- Lucky can use `https://api.lunflux.com/v1` and `https://agent.lunflux.com`
  without holding host credentials.
- A temporary Windows machine can join quickly, visibly, and without hidden
  persistence.
- Eitan-owned hosts can reconnect reliably without gaining ambient authority.
- Codex, Claude Code, ACP, shell, PowerShell, Git, GUI, mesh, Coder, and DevPod
  all operate as adapters, not roots.
- Other agent users can self-host and install the Skillkit without adopting
  Hermes or Lunflux.
- Every success, denial, approval pause, cancellation, failure, release, and
  revoke claim is backed by evidence, audit, and verifier output.

That is the final answer: a small safety microkernel around powerful tools,
where remote development becomes agent-native because every action is consented,
bounded, locally verified, revocable, and provable.

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

## Final Delivery Architecture

The final refinement is a sealed-loop delivery architecture. The project should
ship as six contracts that compose into one safe remote work fabric:

| Contract | Question it answers | Primary owner |
|---|---|---|
| Intent contract | What does the agent want, and what is the smallest safe shape of that work? | Skillkit, MCP/API, gateway policy planner |
| Enrollment contract | Is this host intentionally connected, current, and revocable? | tickets, join manifests, host identity, trust bundle |
| Authorization contract | Is this exact job signed, scoped, approved, fresh, and host-bound? | gateway signing, host validation, approval ledger |
| Execution contract | Can the local machine run the work inside explicit boundaries? | host sovereignty guard, locks, adapter supervisor |
| Proof contract | Can a reviewer reconstruct the result without trusting narration? | evidence bundles, redaction metadata, audit chain |
| Distribution contract | Can a target machine verify the software before running it? | release bundle, verifier, checksums, release key lifecycle |

No feature is complete until it closes the relevant contracts. For example, a
Windows one-command join does not finish at "the script starts." It finishes
only when enrollment, authorization, execution, proof, distribution, revocation,
and no-persistence checks all have verifier output.

### System Components

The final component model is intentionally small:

| Component | Runtime form | Owns | Exposes |
|---|---|---|---|
| Skillkit | files plus MCP tool schemas | agent workflow, planning rules, evidence review instructions | portable installs for Hermes, Codex, Claude Code, OpenCode, and generic MCP agents |
| API edge | `rdev-gateway` HTTP/MCP surface | auth, typed requests, rate limits, schema validation | `/v1` API and MCP-compatible tools |
| Gateway kernel | gateway service module | tickets, hosts, jobs, leases, approvals, signing, audit, artifact index, revocation | signed envelopes, job leases, evidence export |
| Relay | gateway host channel | outbound host connectivity, leases, cancellation delivery | HTTPS long-poll fallback and WSS production path |
| Host kernel | `rdev-host` | host identity, trust, local policy, nonce store, approval consumption, locks, adapter supervisor | local denials, approvals, evidence, stop/uninstall controls |
| Adapter layer | built-in and third-party adapters | domain execution only | shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, mesh, workspace providers |
| Release verifier | `rdev-verify` and `rdev release verify-*` | release integrity checks | machine-readable pass/fail output for bootstrap and release gates |

The gateway is allowed to coordinate but not execute. The host is allowed to
execute but not invent authority. The adapter is allowed to be useful but not
trusted. The release system is allowed to bless software but not authorize jobs.

### Runtime Topology

The reference production topology is:

```text
Hermes/Lucky or another agent runtime
  -> installed Skillkit and MCP/API client
  -> https://api.lunflux.com/v1
  -> gateway API edge
  -> gateway policy, scheduler, signing, approvals, audit, artifact store
  -> outbound relay at https://agent.lunflux.com
  -> rdev-host on a temporary, managed, or workspace-provider host
  -> host sovereignty guard
  -> adapter-specific work
  -> local spool, redaction, evidence upload
  -> reviewer-visible evidence and audit
```

The self-hosted open-source topology is the same shape with the user's own
domain, storage, and release keys. Hermes is the reference deployment, not a
hard dependency.

### Request Lifecycle

Every real task should move through this lifecycle:

1. The agent converts natural language into a typed plan with requested host
   class, adapter, capabilities, workspace/session boundary, TTL, output limits,
   and expected evidence.
2. The gateway runs policy dry-run and returns required approvals, host fit, and
   possible denial reasons before execution.
3. The operator approves enrollment or dangerous operations only through scoped
   approvals. The agent cannot approve itself.
4. The gateway signs a host-bound envelope with job id, host id, adapter,
   capabilities, workspace/session scope, expiry, nonce, approvals, and limits.
5. The host claims a short lease over an outbound channel.
6. The host independently validates trust bundle, signature, expiry, nonce,
   host binding, capabilities, workspace/session scope, approvals, lock state,
   local mode, and revocation status.
7. The host prepares the workspace or visible session and starts exactly one
   adapter through the safety wrapper.
8. The adapter runs under cancellation, timeout, output, redaction, and cleanup
   constraints.
9. The host emits a structured terminal artifact: denial, approval-required,
   evidence bundle, failure, timeout, or cancellation evidence.
10. The gateway indexes artifacts, appends audit events, and makes the result
    reviewable by another human or agent.

There is no privileged side path for "just run this command." Urgent work uses
break-glass mode, shorter TTLs, stronger approvals, and denser audit, not a
weaker kernel.

### Host Kernel Layers

The host runtime should be structured as a local sovereignty stack:

| Layer | Purpose | Fail-closed behavior |
|---|---|---|
| Identity | generate and protect host key material | refuse jobs without matching host binding |
| Trust | store signed gateway keys and revocations | reject stale, rolled-back, expired, or wrong-root bundles |
| Lease client | receive jobs outbound only | let leases expire rather than run stale work |
| Validator | verify envelope, nonce, approval, capability, workspace, and mode | return `rdev.host-denial.v1` or `rdev.approval-required.v1` |
| Lock manager | enforce one writer or prepared worktree | deny concurrent writes unless isolated |
| Adapter supervisor | run domain tools under limits | cancel, timeout, cap output, and preserve evidence |
| Evidence spool | collect, redact, checksum, upload, retry | never claim success without verifiable artifacts |
| Local control | stop, status, logs, uninstall | local stop beats gateway scheduling |

Temporary hosts run these layers in a foreground process with TTL and no service
install. Managed hosts may add an explicit OS service, watchdog, reconnect,
spool replay, and update channel, but the validator rules do not become weaker.

### API And MCP Product Surface

The public API should remain small and typed:

| Surface | Required verbs |
|---|---|
| Tickets | create, inspect, approve/deny, revoke |
| Hosts | register, list, approve capabilities, refresh trust, revoke |
| Jobs | plan, create, lease, status, cancel, complete |
| Approvals | request, approve, deny, revoke, consume |
| Artifacts | upload, index, download, verify |
| Evidence | export, verify, redact report |
| Audit | append, export, verify chain |
| Releases | catalog, verify bundle, verify candidate |

MCP tools should wrap these verbs with agent-safe defaults. They should prefer
`plan` before `run`, structured approval requests before side effects, and
evidence export before completion claims.

### Final Open-Source Product Line

The public project should be released as one toolkit with five install surfaces:

1. `rdev` for operators: local demos, diagnostics, release verification,
   evidence verification, service lifecycle, and acceptance packaging.
2. `rdev-gateway` for self-hosters: API, MCP, tickets, hosts, jobs, approvals,
   relay, artifacts, audit, and revocation.
3. `rdev-host` for target machines: attended temporary and explicit managed
   runtime.
4. Skillkit bundle for agents: installable instructions and tool schemas for
   Hermes, Codex, Claude Code, OpenCode, and generic MCP clients.
5. Adapter SDK: conformance fixtures and wrappers for new execution backends.

The open-source promise is not "control any computer." The promise is: install
this safety layer and give your agents a reviewed, revocable, evidence-producing
way to use real machines.

### Final Personal Deployment

Eitan's deployment is the canonical production slice:

| Domain | Role |
|---|---|
| `https://api.lunflux.com/v1` | authenticated agent/operator API, MCP-compatible HTTP, policy planning, job creation, approval, evidence, audit |
| `https://agent.lunflux.com` | join page, signed release metadata, bootstrap download, outbound host relay, human-facing temporary session UX |
| Hermes/Lucky | central planner and reviewer that uses typed tools, not host credentials |
| Managed Eitan hosts | durable owned machines with explicit service install, workspace locks, coding adapters, local stop/uninstall |
| Attended temporary hosts | third-party or short-lived machines with one visible command, no inbound listener, no persistence, TTL, and local stop |

This deployment should stay boring and inspectable: clear logs, clear status,
clear stop commands, clear uninstall commands, and no hidden machine ownership.

### Reliability Architecture

The system should choose at-least-once delivery with idempotent state transitions
over fragile exactly-once promises.

Required reliability rules:

- job creation, lease, completion, cancellation, artifact upload, and audit
  append are idempotent;
- leases expire and can be reclaimed only when safe;
- managed hosts reconnect with jittered backoff and flush local evidence spools;
- temporary hosts do not auto-resurrect after TTL, local stop, or ticket revoke;
- running jobs receive cooperative cancellation and still upload collected
  cancellation evidence where possible;
- gateway restarts recover from durable state and audit, not process memory;
- artifact upload is checksum-addressed, retryable, quota-bound, and never
  silently truncated;
- redaction failure blocks clean-success claims.

This model makes the system robust without hiding persistence on machines that
did not opt into managed mode.

### Security Architecture

The final security posture is least standing authority plus explicit escalation.

Separate keys and ledgers are required for:

- agent/client auth;
- operator sessions;
- gateway job signing;
- approval token signing;
- trust bundle signing;
- host identity;
- release signing;
- audit chain verification.

Compromising one authority must not grant all three powers of publishing
software, enrolling hosts, and authorizing execution.

The most important default-deny gates are:

- no public inbound listener on temporary hosts;
- no hidden service install in temporary mode;
- no unverified binary execution;
- no unrestricted raw shell as an agent primitive;
- no agent self-approval for package install, elevation, GUI, service changes,
  push, merge, deploy, publish, credentials, or paid actions;
- no workspace write without canonical path checks and a lock or isolated
  worktree;
- no completion claim without evidence and audit.

### Final Close Plan

The perfect ending should be closed in this order:

1. Publish a verified local pre-release with real multi-platform artifacts,
   release bundles, Skillkit bundle, release candidate verification, and dry-run
   GitHub plan.
2. Run a clean Windows 10/11 temporary acceptance transcript from one visible
   verified command, including release verification, outbound-only connection,
   approval probes, revocation, no-persistence checks, and packaged evidence.
3. Run a real service-backed managed Mac acceptance, including LaunchAgent
   start, inspect, reconnect after login/reboot, locked-worktree Codex job,
   evidence verification, stop, and uninstall.
4. Harden production trust lifecycle with authenticated trust refresh,
   OS-protected host identity/trust storage, rollback rejection, revocation, and
   emergency key rotation.
5. Ship WSS/mTLS host transport while preserving HTTPS polling fallback.
6. Extract Adapter SDK and require conformance reports for built-in adapters.
7. Add Claude Code, ACP, PowerShell, GUI, mesh, Coder, DevPod, Windows Service,
   and systemd as adapters or managed-mode extensions.
8. Execute an approved public release only after the release plan, downloads,
   verifier output, acceptance packages, security docs, and threat model all
   match the shipped behavior.

The final state is reached when the easiest path is also the safe path.

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

The first public SDK slices are already narrower and intentionally boring:
`pkg/adapterkit`, `rdev adapter scaffold`, `rdev adapter verify-lifecycle`,
`rdev adapter verify-result`, `rdev adapter verify-cancellation`,
`rdev adapter verify-runtime`, and MCP tools `rdev.adapter.verify_lifecycle` /
`rdev.adapter.verify_result` / `rdev.adapter.verify_cancellation` /
`rdev.adapter.verify_runtime` generate and verify lifecycle manifests,
runtime-fixture JSON, result-artifact JSON, and cancellation evidence.
Lifecycle conformance checks required phases, safety boundaries, cancellation,
cleanup, and result schema declarations. Runtime fixture conformance checks
actual phase order, phase evidence, timing, cleanup, optional collected result
artifacts, and optional cancellation behavior. Result conformance checks
adapter/schema identity, timing, redaction metadata, command evidence,
cancellation/timeout exclusivity, and common secret-pattern rejection.
Cancellation conformance first runs result checks, then requires canceled
command evidence to prove `canceled=true`, `timed_out=false`, an `exit_code`,
and `output_truncated` metadata. Shell, PowerShell, and Codex use the shared
result and cancellation verifiers in tests, so third-party adapter authors have
concrete declaration and evidence contracts before full production hostrunner
integration is extracted.

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

## Release Artifact Reality Contract

Public artifacts must reflect executable reality. A release is not allowed to
ship text placeholders for bootstrap-critical binaries or describe a production
capability that the artifact cannot start.

Current executable split:

| Binary | Current role | Boundary |
|---|---|---|
| `rdev` | full operator CLI and local development surface | canonical command surface |
| `rdev-host` | thin host entrypoint over `rdev host ...`, defaulting to `host serve` | production transport still follows the host runtime maturity gates |
| `rdev-gateway` | thin gateway entrypoint over `rdev gateway ...`, defaulting to `gateway serve` | production persistence/auth/WSS remain explicit gaps |
| `rdev-mcp` | thin MCP entrypoint over `rdev mcp ...`, defaulting to `mcp serve` | inherits the same tool contracts as `rdev mcp` |
| `rdev-verify` | standalone release verifier for bootstrap paths | intentionally small and hash-pinnable |

Build output is a verifiable pre-release artifact, not a publication decision:

```text
scripts/release/build-artifacts.sh
  -> rdev.build-artifacts.v1
  -> target directories such as windows-amd64/
  -> checksums.txt
  -> scripts/release/prepare-platform-candidates.sh
  -> per-target release prepare-candidate
  -> per-target release verify-candidate
  -> scripts/github/plan-platform-release.sh
  -> GitHub Release dry-run plan with platform archives and install guide
```

The current multi-platform layout is explicit platform slicing: one verified
release candidate per `GOOS/GOARCH` target, summarized by
`rdev.platform-release-candidates.v1`. This avoids duplicate basenames such as
`rdev-host` across Unix targets while keeping each candidate independently
verifiable. The Windows temporary bootstrap slice must include real
`rdev-host.exe` and `rdev-verify.exe` binaries built from the repository before
bundle signing and candidate verification.

The aggregate multi-platform release plan is also local evidence, not
publication. It packages each verified platform candidate into a uniquely named
archive, writes `rdev.platform-release-index.v1`, writes
`rdev.github-platform-release-verification.v1`, generates
`INSTALL_PLATFORMS.md`, and produces `gh release` command previews. Executing
those commands remains a separate operator-approved external mutation.

## Final Release Ladder

The project should advance through release ladders rather than broad rewrites.
Each rung has one proof artifact that can be archived and independently checked.

| Rung | Release promise | Required proof |
|---|---|---|
| `v0.1` Local safety kernel | typed jobs, signed envelopes, host validation, denials, approvals, evidence, audit, Skillkit, release candidates | `go test ./...`, `rdev skillkit verify`, `rdev release prepare-candidate`, `rdev release verify-candidate`, audit/evidence verifier output |
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
| Real Windows temporary run | the plan, verifier, bootstrap, and packaging exist; the user-facing promise needs a clean VM transcript | clean Windows 10/11 run archived as `rdev.acceptance-package.windows-temporary.v1` |
| Real managed Mac service run | LaunchAgent planning/control exists; durable reconnect must be proven outside a dry-run | service-backed run survives logout/reboot, completes Codex job, verifies evidence, stops and uninstalls |
| Production host trust lifecycle | development trust stores exist; managed fleets need authenticated updates and rollback-safe storage | OS-protected host identity/trust stores, authenticated trust refresh, revocation propagation |
| Production transport | long-poll works as fallback; WSS/mTLS is the durable production channel | WSS host channel with lease semantics and HTTPS fallback |
| Adapter SDK extraction | lifecycle-manifest, runtime-fixture, result-artifact, and cancellation-artifact conformance exist; `adapterkit.RunLifecycle` provides the first executable lifecycle runner; built-in shell, PowerShell, and Codex hostrunner jobs can append runtime fixtures with `--capture-runtime-fixture` | full production SDK integration for future adapters, executable lifecycle/cancellation fixtures, and docs for Claude Code, ACP, GUI, mesh, and workspace providers |
| Multi-platform release UX | aggregate dry-run plan, platform archives, index, and install guide exist; public release still needs real download verification after publication | verified public download commands and post-release install transcript |
| Public release execution | real build artifacts, release candidates, and dry-run GitHub release plans exist; actual publication still needs explicit approval and release evidence | approved GitHub Release execution, verified downloads, archived release evidence |

Anything not in this ledger is secondary until these are closed.

## Final Implementation Order

Finish in this order:

1. Prove temporary Windows acceptance end-to-end: signed release, foreground
   console, outbound-only host loop, no-persistence inspection, approval probes,
   revoke.
2. Prove real service-backed managed Mac acceptance: plan, start, inspect,
   login/reboot reconnect, locked-worktree Codex run, verify evidence, stop,
   uninstall.
3. Production trust lifecycle: authenticated trust updates, revocation
   propagation, OS-protected host identity and trust storage.
4. WSS host channel with HTTPS long-poll fallback and clear lease semantics.
5. Adapter SDK extraction and public conformance fixtures.
6. Claude Code and ACP adapters.
7. Windows Service, systemd managed modes, auto-update, and rollback.
8. Approved signed public releases, platform signing, security policy, release transcript
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
