# Final System Design

This is the final implementation decision for Remote Dev Skillkit. It refines the broader end-state architecture into a product-grade design that can guide implementation, release, self-hosted deployment, and future open-source packaging.

## Final Verdict

Remote Dev Skillkit should be a consent-first, agent-native remote work fabric, not a remote shell, remote desktop clone, or generic terminal MCP server.

The system should give agents broad usefulness only through narrow, signed, policy-checked jobs. It should never give agents ambient machine ownership.

The winning design is:

```text
Agent Skills + MCP tools
        |
        v
rdev-gateway control plane
        |
        v
outbound HTTPS/WSS host channels
        |
        v
rdev-host execution plane
        |
        v
typed adapters: shell, PowerShell, git, Codex, Claude Code, ACP, browser, GUI
```

The highest-level rule is:

> Maximum capability after explicit consent; minimum ambient authority by default.

That rule resolves the tension between remote repair convenience and safety. Temporary third-party machines get visible, time-limited, foreground sessions. Eitan-owned or formally managed machines can opt into durable service mode.

## Final Architecture Lock

This is the final refined architecture decision for the project. The product should be built as a small safety microkernel with many replaceable adapters around it.

```text
agent intent
  -> typed Skill/MCP request
  -> gateway policy dry-run
  -> signed, host-bound job envelope
  -> outbound host lease
  -> host-side validation
  -> locked workspace/session
  -> adapter execution
  -> redacted artifacts
  -> evidence bundle
  -> hash-chained audit
  -> approval, continuation, or revocation
```

The "perfect ending" is not maximum remote control. It is maximum useful delegation without ambient machine ownership.

### Final Product Boundary

Remote Dev Skillkit owns the agent safety kernel. It should not try to replace every mature remote-development product.

| Layer | Final decision |
|---|---|
| Agent workflow | Agent Skills plus MCP tools teach agents how to invite, approve, run, review, and revoke |
| Control plane | `rdev-gateway` owns tickets, host registry, policy, signing, approvals, artifacts, audit, and revocation |
| Host plane | `rdev-host` owns local identity, trust, policy validation, workspace locks, adapters, evidence, and local stop controls |
| Transport | outbound HTTPS/WSS first; mesh or SSH only as optional owned-host transports |
| Execution | shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, Coder, DevPod, and mesh are adapters |
| Proof | every job ends in structured denial, approval request, or evidence bundle |

This boundary keeps the core small enough to secure while leaving the ecosystem broad enough to be useful.

### Final Deployment Shape

Eitan's personal production deployment should be the reference implementation:

```text
Hermes/Lucky
  -> Remote Dev Skillkit skills
  -> MCP HTTP or local MCP bridge
  -> https://api.lunflux.com/v1
  -> rdev-gateway
  -> tickets, jobs, approvals, artifacts, audit, signing
  -> https://agent.lunflux.com
  -> join page, signed manifests, host relay, release downloads
  -> managed hosts or temporary attended hosts
```

The server can start as one binary on one VPS, but the architecture must preserve separate responsibilities:

- `api.lunflux.com/v1` is the authenticated agent/operator API surface.
- `agent.lunflux.com` is the human join, bootstrap, release, and host-relay surface.
- Hermes/Lucky holds an agent-client identity, not host credentials.
- Eitan approves high-risk operations through scoped approval tokens.
- The host verifies every executable job even if the gateway or client behaves incorrectly.

### Final Runtime Loops

The system has three loops. Every feature must fit one of them.

| Loop | Purpose | Final output |
|---|---|---|
| Enrollment loop | turn a human-approved machine into an identified host | ticket, host identity, capability inventory, host policy, trust bundle |
| Execution loop | turn agent intent into bounded machine work | signed envelope, lease, adapter run, denial or artifacts |
| Review loop | turn remote work into an auditable decision | evidence bundle, audit slice, approval request, continuation, or revoke |

The loops are intentionally boring. They make retries, reconnects, cancellation, and audit reconstruction possible.

### Final Host Selection Model

Lucky or another agent should never pick a host by hostname alone. Host selection should be a policy decision over a registry snapshot.

Selection inputs:

- host id and verified identity fingerprint;
- mode: `attended-temporary`, `managed`, `break-glass`, or `workspace-provider`;
- online status, last heartbeat, current lease, and load;
- platform, architecture, tools, adapters, and coding CLIs;
- approved workspace roots and capability rings;
- trust bundle sequence and revocation state;
- local stop status and remaining session TTL;
- workspace lock status and active worktrees.

Selection output:

- selected host id;
- selected adapter;
- selected workspace or new worktree;
- required approvals before execution;
- risk explanation when the best host is temporary, privileged, stale, or missing evidence.

### Final Scheduler And Concurrency Model

The gateway schedules intent. The host owns execution.

| Concern | Final rule |
|---|---|
| Job binding | every job is bound to one host id before execution |
| Leasing | a host must claim a bounded lease before running |
| Idempotency | job creation, claim, completion, artifact upload, and cancellation must be retry-safe |
| Workspace writes | one active writer per workspace root unless distinct worktrees are used |
| Approval tokens | consumed only after envelope, policy, nonce, capability, workspace, and lock checks pass |
| Cancellation | host, ticket, job, key, or approval revocation cancels queued work and requests cooperative stop for running work |
| Offline behavior | managed hosts may reconnect and flush audit/evidence; temporary hosts do not silently resurrect after stop or expiry |

The current workspace lock implementation is part of this contract: coding adapters should run only after the host has acquired the lock or prepared a separate worktree.

### Final Adapter Contract

Adapters are plugins behind the safety kernel. They may be powerful, but they do not define authorization.

```text
detect(context) -> adapter_capabilities
plan(job, host_policy) -> required_capabilities, approvals, workspace_plan
prepare(job) -> locked_workspace_or_session
run(job, limits) -> stream, artifacts, exit_status
collect(job) -> evidence_manifest
cleanup(job) -> cleanup_status
```

Required adapter guarantees:

- no execution before host validation;
- no writes outside declared workspace scope;
- no push, merge, deploy, publish, credential access, service mutation, GUI control, or elevation without approval;
- bounded logs and output redaction;
- stable artifacts with checksums;
- cancellation hooks;
- conformance tests for capability mapping, denials, approvals, evidence, and cleanup.

Initial adapter priority:

1. `shell` and `powershell` for controlled diagnostics and repair.
2. `git` for branches, worktrees, diffs, commits, and evidence.
3. `codex` for Eitan's managed Mac coding path.
4. `claude-code` and `acp` for broader coding-agent compatibility.
5. `browser-e2e`, GUI, mesh, Coder, and DevPod after the safety kernel is stable.

### Final Temporary Host Design

Temporary hosts are for third-party or short-lived machines. They must be useful without becoming hidden administration.

Final properties:

- visible foreground process or window;
- local stop control;
- short TTL;
- outbound-only HTTPS/WSS connection;
- no service install, autorun entry, registry persistence, launchd job, systemd unit, scheduled task, or hidden restart;
- signed join manifest and signed release artifact verification before host execution;
- local host key generated for the session;
- pending approval before first job;
- approval gates for elevation, GUI, service changes, package installation, destructive actions, credential access, push, merge, deploy, publish, and long unattended work;
- automatic cleanup and final audit/evidence upload when the session ends.

If a temporary host needs durable reconnect after reboot, the correct answer is not silent persistence. The correct answer is a separate managed enrollment with explicit consent.

### Final Managed Host Design

Managed hosts are for Eitan-owned or formally managed machines.

Final properties:

- explicit `rdev host install-service` enrollment;
- install summary, service account, paths, logs, trust roots, stop command, and uninstall command are visible;
- OS-native service manager: LaunchAgent or LaunchDaemon on macOS, systemd on Linux, Windows Service on Windows;
- durable host identity protected by OS storage where available;
- signed trust bundle update protocol;
- watchdog/restart only for managed mode;
- health/status/stop/uninstall commands;
- no inherited approval for external consequences such as push, merge, deploy, publish, paid actions, or credential changes.

Managed mode gives reliability, not unlimited authority.

### Final Policy Kernel

Policy should be deny-by-default and explainable. The same vocabulary must be used by gateway dry-runs, host validation, adapter planning, evidence bundles, and Skills.

| Ring | Default posture | Examples |
|---|---|---|
| Ring 0 observe | allowed after host approval | capability detection, git status, read-only logs |
| Ring 1 workspace | allowed when root is approved | scoped reads/writes, tests, build commands |
| Ring 2 repair | approval or managed narrow grant | package changes, dependency repair, process kill |
| Ring 3 privileged/visual | per-operation approval | elevation, GUI control, screenshots, service mutation |
| Ring 4 external consequence | per-operation approval after evidence review | push, merge, deploy, publish, paid actions, credential changes |

Denial is a valid successful outcome. The agent should receive `rdev.host-denial.v1` or `rdev.approval-required.v1` instead of a vague failure when the safe answer is "not yet."

### Final Storage And Evidence Model

The first production deployment can be single-operator and simple, but the object model must be future-proof.

| Data | First production storage | Final requirement |
|---|---|---|
| gateway state | SQLite with backups | Postgres-compatible schema later |
| artifacts | local filesystem with quota | S3-compatible object storage later |
| audit | append-only JSONL plus verifier | hash-chain reconstruction and export |
| gateway signing keys | locked file or OS store | rotation, revocation, KMS/HSM option |
| host identity | file-backed dev store | OS keychain/DPAPI/libsecret for managed mode |
| host trust | file-backed trust bundle | signed update protocol and rollback protection |
| approval tokens | gateway state plus host consumption store | single-use, scoped, expiring, auditable |

Evidence bundles are the unit of review. They should contain envelope, policy decisions, approval tokens used, adapter result, logs, diffs, test output, artifact checksums, redaction metadata, and an audit slice.

### Final Release And Bootstrap Model

One-command installation is acceptable only when verification is built in.

Release requirements:

- signed release manifest;
- artifact digest, size, platform, signer, and validity window;
- Windows Authenticode verification when policy requires it;
- macOS notarization for public macOS releases;
- checksums and detached signatures for all platforms;
- rollback strategy for managed hosts;
- security advisory and key-rotation process.

Bootstrap requirements:

- bootstrap script is inspectable;
- bootstrap verifies manifest and binary before execution;
- temporary mode starts foreground by default;
- managed mode uses a separate explicit command;
- no weakening of UAC, sudo, TCC, Gatekeeper, Defender, enterprise policy, firewall, or PowerShell execution policy.

### Final Open-Source Package Shape

The public project should ship five things:

| Package | User value |
|---|---|
| `rdev` CLI | local demo, operator workflows, diagnostics, service install, evidence export |
| `rdev-gateway` | self-hosted control plane and MCP/API server |
| `rdev-host` | cross-platform temporary and managed host runtime |
| Skillkit bundle | portable install surface for Hermes, Codex, Claude Code, OpenCode, and generic MCP agents |
| Adapter SDK and conformance tests | safe extension surface for new execution backends |

Success means other users can adopt the safe workflow without adopting Hermes, while Eitan's Hermes/Lucky deployment remains the first reference environment.

### Final Implementation Finish Line

The final architecture is done when these are true:

1. A temporary Windows machine can join from a visible verified command, run bounded repair jobs, and leave no persistence.
2. Eitan's managed Mac can reconnect after reboot, receive a Codex job, lock a worktree, return diff/test evidence, and require approval before push or merge.
3. Every executable job is a signed, host-bound, nonce-protected, expiring envelope.
4. Gateway policy and host policy both enforce the same capability vocabulary.
5. Adapters cannot bypass workspace locks, approval gates, evidence, redaction, or audit.
6. Revocation stops future work and cancels queued or running jobs where possible.
7. Skillkit export installs cleanly into Hermes, Codex, Claude Code, OpenCode, and generic MCP environments.
8. Public releases verify signed manifests and binaries before host execution.

Everything else is an adapter, transport, UI, or deployment improvement.

## Endgame Solution Layer

This section is the final scorecard for the "perfect ending." Later sections explain the mechanics; this section decides whether a feature belongs in the product.

The endgame is a remote-development control plane where agents can cause useful work on real machines without ever receiving ambient ownership of those machines. The complete system is therefore a set of locked boundaries:

| Boundary | Final decision | Regression if violated |
|---|---|---|
| User consent | temporary sessions are visible, foreground, TTL-bound; managed service is explicit | support tool becomes hidden remote admin |
| Agent authority | agents request typed work through Skills/MCP/API | agent receives raw SSH/RDP/VNC or unrestricted shell by default |
| Gateway authority | gateway owns policy, signing, approvals, artifacts, audit, revocation | a transport or adapter becomes the security root |
| Host authority | host independently verifies every signed job before execution | gateway/client compromise directly runs code on hosts |
| Adapter authority | adapters execute inside bounded workspace, capability, approval, and evidence rules | Codex/shell/GUI/mesh bypasses the safety kernel |
| Evidence authority | completion requires artifacts and audit, not narration | agent claims success without reviewable proof |
| Release authority | bootstrap verifies signed manifests and artifacts before running host code | one-command install becomes supply-chain blind trust |
| Revocation authority | tickets, hosts, jobs, approvals, and keys can be stopped and audited | mistakes or compromise keep running after stop |

The product is correct only when all boundaries hold at the same time.

### Final Reference Architecture

```text
Agent Runtime
  Hermes/Lucky, Codex, Claude Code, OpenCode, Cursor-style agents
        |
        v
Skillkit + MCP/API Surface
  typed tools, safe workflows, policy dry-runs, evidence review
        |
        v
rdev Gateway
  tickets, host registry, policy, signing, approvals, leases,
  artifacts, audit, trust bundles, revocation
        |
        v
Outbound Host Channel
  HTTPS polling fallback, WSS production path, optional mesh for owned hosts
        |
        v
rdev Host Runtime
  local identity, trust store, policy verifier, nonce/approval stores,
  workspace locks, adapter runner, local audit spool
        |
        v
Adapters
  shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI,
  Coder, DevPod, SSH, Tailscale/headscale
```

The gateway may coordinate, but the host must remain sovereign over local execution. The host's default answer to anything ambiguous is deny or approval-required.

### Final Operating Modes

The same kernel supports four modes. Modes must stay visibly separate in CLI, policy, audit, and install UX.

| Mode | Target | Persistence | Who consents | Typical work | Hard stop |
|---|---|---:|---|---|---|
| `attended-temporary` | third-party or short-lived machine | none | local user plus operator | repair, diagnostics, scoped fix | TTL, stop, ticket revoke |
| `managed` | Eitan-owned or formally managed device | explicit service | operator during install and policy approval | durable coding, tests, scheduled maintenance | host revoke, policy revoke, service uninstall |
| `break-glass` | emergency repair | no hidden persistence; short-lived | stronger local/operator approval | urgent recovery | short TTL and dense audit |
| `workspace-provider` | Coder/DevPod/cloud workspace | provider-managed | operator and workspace policy | disposable coding environment | workspace destroy/revoke |

Temporary mode never upgrades itself into managed mode. Managed mode never inherits approval to push, merge, deploy, publish, change credentials, or control GUI without a scoped approval.

### Final Authority Map

No single actor receives all powers.

| Actor | Can do | Cannot do |
|---|---|---|
| Agent runtime | request tickets/jobs, explain approvals, review evidence | approve its own dangerous action, bypass policy, receive raw host credentials |
| Operator | approve hosts, approvals, revocations, policies | bypass host-side verification |
| Gateway | sign bounded jobs and approval tokens | execute locally on a host without host validation |
| Host runtime | verify and execute bounded work | broaden its own policy or trust roots |
| Adapter | run a declared operation | create private persistence, widen workspace, skip evidence |
| Release system | bless binaries and scripts | authorize jobs or approvals |

This separation is the core defense against both prompt injection and ordinary operational mistakes.

### Final Golden Paths

The project is finished when these paths work without special pleading.

| Path | Final user experience | Required proof |
|---|---|---|
| Temporary Windows repair | Eitan sends a join link/command; the user runs a visible verified bootstrap; Lucky triages and repairs through signed jobs | no persistence, outbound only, release verification, approval pauses, denials, artifacts, audit, revoke |
| Managed Mac coding | Lucky selects Eitan's Mac, locks a Git worktree, runs Codex, runs tests, returns diff/evidence | host identity, workspace lock, Codex adapter result, diff/tests, audit slice, approval before push/merge |
| Public Skillkit install | another agent user installs skills and MCP contracts without Hermes assumptions | exported Skillkit bundle, stable schemas, self-host docs, signed releases, threat model |
| Adapter extension | contributor adds a new adapter | conformance tests prove capability mapping, workspace enforcement, cancellation, redaction, evidence, and audit |

### Final Architecture Test

Before adding or accepting any feature, answer these questions:

1. Does the agent request a typed operation instead of raw access?
2. Does the gateway sign only a bounded, host-specific, expiring envelope?
3. Can the host independently reject the job if the gateway, client, or transport behaves badly?
4. Are capabilities, workspace, limits, approvals, and redaction explicit?
5. Does the action produce evidence and audit events sufficient for another reviewer?
6. Can the operator revoke the ticket, host, job, approval, or key and see the result?
7. Does temporary mode remain visible, foreground, outbound-only, and non-persistent?
8. Does managed mode remain explicitly installed with health/status/uninstall paths?

If any answer is no, the feature is outside the final architecture until redesigned.

### Final Release Definition

`v1.0` is not "all adapters exist." `v1.0` is the first public release where the safety kernel, protocol contracts, and install story are stable enough for other agent ecosystems.

`v1.0` requires:

- signed and verifiable release artifacts for supported platforms;
- stable schema versions for tickets, join manifests, trust bundles, jobs, approvals, denials, artifacts, evidence bundles, and audit events;
- MCP tools and Agent Skills that expose typed workflows, not raw shells;
- local demo, self-host deployment, temporary Windows repair, and managed coding documentation;
- conformance tests for every built-in adapter;
- threat model, release key lifecycle, security policy, and emergency revocation process;
- acceptance evidence for Temporary Windows Repair and Managed Mac Coding.

The perfect ending is reached when Eitan can say "Lucky, use that approved machine to solve this," and the system responds with bounded execution, local verification, approval gates, evidence, audit, and revocation instead of trust-me automation.

## Endgame Operating Model

The final product should be understood as one operating model rather than a pile of integrations:

```text
agent intent
  -> typed MCP/CLI request
  -> gateway policy decision
  -> signed job envelope
  -> host lease over outbound channel
  -> host-side verification
  -> adapter execution
  -> redacted artifacts
  -> hash-chained audit
  -> human or agent review
  -> approval, continuation, or revocation
```

This loop is the product. Everything else is implementation detail.

### Endgame Thesis

Remote Dev Skillkit wins only if it becomes the safety and orchestration layer agents use before touching real machines. It should not compete with every remote access, mesh, coding CLI, or cloud workspace tool. Instead, it should make those tools safe to delegate to:

- MCP exposes structured external actions to agents.
- The gateway turns allowed actions into signed intent.
- The host proves local identity and enforces local policy.
- Adapters execute useful work without owning trust.
- Evidence lets another agent or human review the result.

The final system therefore has one stable core and many replaceable edges. The core is identity, policy, envelope signing, host validation, approvals, revocation, audit, and evidence. The edges are transports, coding CLIs, GUI tools, mesh networks, hosted workspaces, and future agent runtimes.

### Final Product Form

The complete open-source product should ship as four installable surfaces and one shared protocol:

| Surface | Primary user | Final responsibility |
|---|---|---|
| Agent Skills | Hermes/Lucky, Codex, Claude Code, OpenCode, Cursor-style agents | teach safe workflows: invite, triage, job, approval, evidence, revoke |
| MCP/API gateway | operator or self-hosted service | own tickets, host registry, policy, signing, approvals, artifacts, audit, revocation |
| Host runtime | target Mac, Windows, Linux machine | connect outbound, validate every job, run adapters, spool evidence |
| Operator CLI | human operator and developer | bootstrap, debug, inspect, export evidence, verify releases |
| Protocol contracts | all components | schema-versioned tickets, manifests, envelopes, tokens, artifacts, audit events |

The protocol is more important than any single binary. If another gateway, hosted service, or agent runtime implements the same contracts without weakening the safety kernel, it is part of the ecosystem rather than a fork in spirit.

### Final Delivery Blueprint

The finished project should ship as one coherent system with five deliverables. Each deliverable is independently useful, but the full safety guarantee exists only when they run together.

| Deliverable | Shipped as | Final contract |
|---|---|---|
| Skillkit Bundle | `rdev skillkit export` | portable skills, MCP tool contracts, framework install notes, and checksummed manifest |
| Gateway | `rdev gateway serve` plus HTTP/MCP API | tickets, host registry, policies, signing, approvals, artifacts, audit, revocation |
| Host Runtime | `rdev host serve` plus managed service installers | outbound channel, host identity, local policy, adapters, local audit spool |
| Adapter SDK | Go interfaces, schemas, conformance tests | shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, mesh, workspace integrations |
| Evidence Tooling | `rdev evidence export`, `rdev audit export`, verifiers | portable review bundles and tamper-evident audit reconstruction |

The final open-source install flow should therefore be:

```text
install rdev
  -> export or install Skillkit
  -> configure MCP stdio/HTTP
  -> start or connect gateway
  -> invite or approve host
  -> run typed job
  -> review evidence
  -> approve continuation or revoke
```

This is the universal shape for Hermes/Lucky, Codex, Claude Code, OpenCode, Cursor-style agents, and generic MCP agents. A runtime may present different UI, but it must preserve the same protocol objects and approval boundaries.

### Final Capability Rings

Capabilities should be organized into rings so agents and operators can reason about risk consistently across adapters.

| Ring | Examples | Default posture |
|---|---|---|
| Ring 0: observe | host info, tool versions, git status, read-only logs | allowed after host approval |
| Ring 1: scoped workspace | read/write inside approved repo or temp directory, run allowlisted tests | allowed by explicit host policy |
| Ring 2: environment repair | package manager, dependency changes, service inspection, network diagnostics | approval required unless managed policy grants it narrowly |
| Ring 3: privileged or visual | elevation, GUI control, screenshots, browser profile access, service mutation | per-operation approval required |
| Ring 4: external consequence | push, merge, deploy, publish, paid action, credential change | per-operation approval and evidence review required |

Adapters must map their local actions into these rings before execution. Unknown actions default to denied or approval-required.

### Final Protocol Set

The end state is complete only when these schemas are stable, versioned, documented, and covered by conformance tests:

| Schema | Purpose |
|---|---|
| `rdev.skillkit-bundle.v1` | portable agent installation bundle |
| `rdev.ticket.v1` | temporary or managed invitation intent |
| `rdev.join-manifest.v1` | bootstrap trust, downloads, release roots, and ticket binding |
| `rdev.release-artifact.v1` | signed artifact digest, size, platform policy, and verification metadata |
| `rdev.trust-bundle.v1` | gateway job-signing key set, rotation sequence, and revocation state |
| `rdev.trust-bundle-update.v1` | host-bound trust-store refresh check for managed hosts |
| `rdev.host-policy.v1` | approved capabilities, adapters, workspaces, limits, and approval gates |
| `rdev.job.v1` | canonical signed executable intent |
| `rdev.approval-token.v1` | scoped human decision for one high-risk operation |
| `rdev.host-denial.v1` | structured refusal with safer next step |
| `rdev.approval-required.v1` | structured pause before dangerous execution |
| `rdev.evidence-bundle.v1` | review package containing artifacts, policy, envelope, audit slice, and checksums |
| `rdev.audit-event.v1` | append-only event for tamper-evident reconstruction |

The schemas are the product's durable API. CLIs, hosted deployments, UI consoles, and agent frameworks can evolve around them.

### Final Eitan Deployment

Eitan's personal production topology should stay single-operator first:

```text
Hermes/Lucky
  -> Remote Dev Skillkit skills
  -> rdev MCP HTTP at https://api.lunflux.com/v1
  -> rdev-gateway on the server
  -> job signing, approvals, artifacts, audit
  -> host relay/join surface at https://agent.lunflux.com
  -> managed Eitan hosts or attended temporary hosts
```

The server may initially run one binary, but the public contract should keep `api.lunflux.com` and `agent.lunflux.com` separate in responsibility:

- `api.lunflux.com/v1`: authenticated agent/operator API, MCP tools, jobs, approvals, artifacts, audit.
- `agent.lunflux.com`: human join page, bootstrap manifests, release downloads, outbound host relay.

Hermes/Lucky should hold an agent-client identity with tool permissions. It should not hold reusable host credentials, raw SSH passwords, or approval power for dangerous operations.

### Final Public Install Experience

The public project should support three complete install stories:

| Path | Command shape | User outcome |
|---|---|---|
| Local demo | `rdev demo local` and `rdev mcp serve` | one developer can understand the safety kernel locally |
| Self-host | `rdev gateway serve` plus release/host setup | a power user can run their own gateway and hosts |
| Skillkit only | `rdev skillkit export --gateway-url ...` | an existing agent platform can install the workflows and tool schemas |

The Skillkit path is important because it lets other agent ecosystems adopt the safe workflow before they adopt the full runtime. The package must contain no Hermes-only assumptions.

### Final Implementation Order

The remaining path to the perfect ending should be implemented in this order:

1. Finish the local safety kernel and bundle export so the protocol is installable and testable.
2. Harden temporary Windows bootstrap because it is the riskiest and most universal support path.
3. Build managed Mac coding because it proves Lucky can safely delegate real development to Codex on Eitan-owned hardware.
4. Generalize managed services across Windows, macOS, and Linux with protected identity storage and trust updates.
5. Add adapter SDK and conformance tests so Codex, Claude Code, ACP, GUI, mesh, Coder, and DevPod integrations cannot bypass the kernel.
6. Stabilize schemas, signed releases, docs, threat model, and public install paths for v1.0.

Any shortcut that makes a demo easier but weakens signed envelopes, local host validation, approval gates, evidence, or revocation is not on the final path.

### Authority Separation

No final deployment may collapse all authority into one bearer token, one SSH credential, or one remote desktop session.

| Authority | Grants | Does not grant |
|---|---|---|
| Agent client auth | permission to request tools | permission to execute on hosts or approve danger |
| Operator session | permission to approve scoped actions | permission to bypass host validation |
| Gateway job signing key | executable intent for a bounded job | release trust or host identity |
| Host identity key | proof of the enrolled machine | permission to broaden policy |
| Approval token key | one scoped exception | reusable standing privilege |
| Release signing key | software artifact trust | job execution authority |
| Audit chain | tamper evidence | authorization by itself |

This separation is the reason the system can be useful without becoming ambient machine ownership.

### The Perfect Ending In One Sentence

An agent can safely say "use that machine to solve this" because the machine only accepts signed, scoped, reviewable work that a human can approve, stop, audit, and revoke.

### Failure Conditions

The architecture is considered to have failed if any of these become normal behavior:

- an agent receives raw long-lived SSH/RDP/VNC credentials as the default path;
- a temporary third-party host installs a background service or autorun entry;
- a mesh ACL, hostname, IP address, or remote desktop session authorizes job execution without a signed envelope;
- an adapter runs outside workspace policy, approval gates, artifact redaction, or audit;
- package install, elevation, GUI control, service mutation, push, merge, deploy, publish, paid action, or credential change happens without scoped approval;
- evidence is replaced by a natural-language summary without artifacts and audit proof;
- release/bootstrap verification is treated as optional for one-command installation.

## Final Safety Kernel

The final architecture is intentionally split into a small non-negotiable kernel and a large replaceable adapter surface.

The kernel is what makes the product safe:

```text
identity -> ticket/policy -> signed envelope -> host validation -> adapter run
        -> artifact/evidence -> audit hash chain -> review/revoke
```

Everything in this path must be deterministic, schema-versioned, tested, and boring. It should not depend on a specific coding CLI, transport vendor, mesh provider, remote desktop tool, or hosted workspace product.

The replaceable surface is what makes the product useful:

```text
Codex, Claude Code, ACP, shell, PowerShell, Git, browser, GUI, SSH,
Tailscale/headscale, Coder, DevPod, local tmux, future agent runtimes
```

Adapters may be powerful, but they do not get to define trust. They receive bounded jobs from the kernel, return evidence to the kernel, and are revoked by the kernel.

### Kernel Responsibilities

| Kernel area | Final responsibility | Why it cannot be delegated |
|---|---|---|
| identity | operator, agent client, gateway, host, release, approval identities | every other decision depends on knowing who asked and who executes |
| policy | capability rings, workspace scope, approval gates, limits | adapters and prompts are not reliable security boundaries |
| envelope | canonical signed executable request | prevents ambiguous intent, tampering, wrong-host execution, and replay |
| host validation | local verification before every run | gateway approval alone is not enough when clients or transports fail |
| approval | human decision bound to one operation | agents can request escalation but cannot grant it to themselves |
| audit | append-only hash-chained events | remote work must be reviewable after the fact |
| evidence | per-job artifacts, diffs, test output, denials, redaction metadata | the operator needs proof, not a confident summary |
| revocation | cancel jobs, disable tickets, hosts, approvals, and keys | the operator must be able to stop the system quickly |

### Adapter Responsibilities

Adapters own execution details only:

- detect local capability;
- prepare a workspace or session;
- run the requested operation within signed limits;
- stream bounded output;
- redact before upload;
- return schema-versioned artifacts;
- clean up or leave reviewable state.

An adapter that needs extra authority must request a capability or approval. It must never create a private bypass around signed jobs, workspace locks, redaction, approval, or audit.

## Ultimate End-State Solution

The final product is a universal remote-work safety layer for agents. It lets an operator say "use that machine to solve this problem" without handing the agent SSH credentials, a raw desktop session, or broad terminal ownership.

The end state is made of five cooperating products:

1. **Skillkit**: small Agent Skills that teach Hermes/Lucky, Codex, Claude Code, OpenCode, Cursor Agent, and similar systems to create tickets, request jobs, ask for approvals, read evidence, and revoke sessions.
2. **MCP/API Gateway**: a control plane that exposes typed tools, owns tickets, host registry, policies, approvals, signing, artifacts, audit, and revocation.
3. **Host Runtime**: a cross-platform binary that runs on Mac, Windows, and Linux in either visible temporary mode or explicit managed service mode.
4. **Adapter SDK**: a stable way to plug in shell, PowerShell, Git, Codex, Claude Code, ACP, browser, mesh, SSH, GUI, Coder, DevPod, or future coding environments without weakening the core safety model.
5. **Evidence System**: hash-chained audit plus per-job evidence bundles that make remote work reviewable by a human or another agent.

The "perfect ending" is not that rdev controls every machine in every possible way. The perfect ending is that every supported path is consented, bounded, replay-safe, revocable, and reviewable.

### Final Architecture Contract

The system is complete only when all of these statements are true:

1. An agent can create an attended temporary ticket without learning host credentials.
2. A Windows user can join from one visible command that verifies signed bootstrap and release artifacts before running the host.
3. Temporary hosts connect outbound only, run in the foreground, have a visible stop path, and leave no service or autorun persistence.
4. Managed hosts install through a separate explicit command, persist host identity and trust safely, reconnect after reboot, and have health/stop/uninstall commands.
5. Every executable action is represented as a canonical signed job envelope with host binding, nonce, expiry, capabilities, workspace scope, limits, and approvals.
6. The gateway refuses unsafe jobs before signing, and the host independently refuses unsafe jobs before execution.
7. Dangerous operations become approval requests, not side effects. Agents may request and explain approvals but cannot grant them.
8. Coding jobs run through a locked workspace or worktree and return diff, test output, logs, artifacts, and residual risk before push, merge, deploy, or publish.
9. Audit export proves ticket, host, policy, approval, job, artifact, trust update, revocation, and release-verification events without trusting mutable logs.
10. Other users can self-host the same stack with safe defaults, stable MCP schemas, installable skills, signed releases, and documented threat boundaries.

### Personal Perfect Ending For Eitan

Eitan's finished personal system should work like this:

```text
Lucky on Hermes
  -> rdev MCP tools at https://api.lunflux.com/v1
  -> rdev-gateway signs bounded jobs
  -> Eitan approves scoped host/session/workspace policy
  -> target host connects outbound to https://agent.lunflux.com
  -> rdev-host executes through a typed adapter
  -> Lucky reads evidence and summarizes next decisions
```

For Eitan-owned machines, Lucky should be able to select a managed Mac, Windows host, or Linux host by capability, repository root, current load, and last heartbeat. For third-party machines, Lucky should create a short-lived join link and wait for explicit operator approval before the first job.

The personal deployment should optimize for one operator first, not premature multi-tenant complexity:

1. `api.lunflux.com` exposes the authenticated rdev API and MCP HTTP surface.
2. `agent.lunflux.com` exposes the join page, signed manifests, release downloads, and host relay.
3. Hermes/Lucky holds an agent-client identity with tool permissions, not host credentials.
4. Managed Eitan devices keep durable host identity and trust bundles.
5. Third-party devices use temporary session-scoped identity and no persistence.
6. Every remote coding session ends in an evidence bundle Lucky can summarize before Eitan decides whether to continue, push, merge, deploy, or revoke.

### Public Perfect Ending For The Open-Source Project

The open-source package should give a new user three install paths:

| Path | User | Outcome |
|---|---|---|
| `rdev local` | one developer | local MCP stdio and local host/gateway demo |
| `rdev self-host` | power user or small team | gateway, join page, artifacts, audit, signed host runtime |
| `rdev skillkit` | agent ecosystem user | Agent Skills plus MCP tool contracts for an existing gateway |

The public package should not require adopting Hermes. Hermes/Lucky is the first-class personal deployment, but the project succeeds only if Codex, Claude Code, OpenCode, and other agent stacks can use the same tools.

## Perfect Ending

The perfect ending is not a single feature. It is a stable operating model:

1. Eitan can ask Hermes/Lucky to work on any approved Mac, Windows, or Linux host.
2. Lucky can discover or enroll a host through MCP tools without receiving raw machine ownership.
3. A temporary third-party Windows machine can join from one visible command, verify the host binary before running it, connect outbound only, and leave no persistence behind.
4. A managed Mac can reconnect after reboot, receive a Codex coding job, work inside a locked Git worktree, and return diff/test evidence before any push or merge.
5. Every executable action is a signed, host-bound, time-bound, policy-bound envelope.
6. The gateway refuses unsafe jobs, and the host independently refuses unsafe jobs.
7. Dangerous actions become explicit approval requests with evidence and scope, not hidden side effects.
8. Revocation stops future work and cancels outstanding work.
9. Audit export can reconstruct who requested what, which host ran it, which policy allowed or denied it, which artifacts were produced, and which human approvals were used.
10. Other agent users can install the same skills, MCP server, gateway, and host runtime as an open-source toolkit without inheriting Eitan-specific assumptions.

If a shortcut makes any of those statements false, it is outside the final design.

## Product Boundaries

### This Project Is

- an installable skillkit for agents;
- an MCP and CLI tool surface for remote development work;
- a gateway/host protocol for typed jobs;
- a cross-platform bootstrap and host runtime;
- an auditable approval, policy, and evidence system.

### This Project Is Not

- a stealth remote administration tool;
- a hidden persistence mechanism for third-party machines;
- an unrestricted shell exposed to LLMs;
- a way to bypass UAC, sudo, TCC, Gatekeeper, Defender, execution policy, or enterprise controls;
- a replacement for SSH, Tailscale, RustDesk, MeshCentral, Codex, or Claude Code.

Those tools can be adapters or transports. Remote Dev Skillkit is the governing layer above them.

## Standards Anchors

The final design intentionally aligns with public platform behavior instead of inventing every primitive:

| Area | Anchor | Design consequence |
|---|---|---|
| Agent tool surface | MCP tools are model-invoked external actions with explicit schemas | expose typed `rdev.*` tools, not a generic remote shell |
| Windows bootstrap | PowerShell execution policy and Authenticode are part of the platform security model | do not weaken execution policy; verify signatures and hashes before running binaries |
| Mesh transport | Tailscale/headscale auth keys, ephemeral devices, tags, ACLs, and revocation | use mesh only as optional connectivity, never as job authorization |
| Linux managed service | systemd restart and watchdog behavior | only managed mode may use restart-on-failure semantics |
| macOS managed service | launchd LaunchAgents/LaunchDaemons | install only after explicit managed enrollment with uninstall instructions |
| SSH fallback | OpenSSH certificates and principals can scope access | SSH is an adapter/transport for owned hosts, not the primary temporary path |
| Cloud dev environments | Coder and DevPod solve governed or reproducible workspaces | integrate as workspace/adapters instead of rebuilding cloud IDE platforms |
| Remote tunnels | VS Code Remote Tunnels shows the value of outbound tunnel UX | rdev keeps the outbound shape but adds agent policy, signing, approvals, and audit |
| GUI remote support | RustDesk/MeshCentral can provide screen-level control | use as explicit GUI adapters, never as default agent authority |
| Supply-chain signing | Sigstore/Cosign-style verification and platform code signing | signed manifests, checksums, Authenticode/notarization policy, and release roots |

Useful references:

- MCP Tools: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- MCP Authorization: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
- PowerShell execution policies: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_execution_policies
- Get-AuthenticodeSignature: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.security/get-authenticodesignature
- Tailscale auth keys: https://tailscale.com/docs/features/access-control/auth-keys
- Tailscale ephemeral nodes: https://tailscale.com/docs/features/ephemeral-nodes
- Sigstore Cosign verification: https://docs.sigstore.dev/cosign/verifying/verify/
- Coder Agents: https://coder.com/docs/ai-coder/agents
- DevPod devcontainers: https://devpod.sh/docs/developing-in-workspaces/devcontainer-json
- VS Code Remote Tunnels: https://code.visualstudio.com/docs/remote/tunnels
- RustDesk self-host: https://rustdesk.com/docs/en/self-host/
- systemd service units: https://www.freedesktop.org/software/systemd/man/systemd.service.html
- Apple launchd jobs: https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html
- OpenSSH sshd_config: https://man.openbsd.org/sshd_config

## Buy Versus Build Boundary

The project should reuse mature transport, workspace, GUI, and signing ecosystems where they are strong. It must build the agent-specific safety kernel because that is the missing layer.

### Build In rdev

| Area | Why rdev owns it |
|---|---|
| ticket and enrollment model | agents need consented, scoped, revocable sessions rather than ambient access |
| canonical signed job envelopes | existing remote tools do not bind agent intent, host identity, policy, nonce, expiry, and approvals into one execution contract |
| host-side policy engine | gateway approval alone is not enough when prompts, clients, or transports fail |
| approval gates | dangerous operations need human decisions that agents cannot self-grant |
| evidence bundles | remote coding needs diff/test/artifact/audit evidence, not only terminal output |
| adapter contract | Codex, Claude Code, shell, PowerShell, Git, GUI, and workspace platforms need one safety wrapper |
| audit hash chain | users need independent verification of what happened after a remote session |
| safe bootstrap | one-command convenience must still verify release trust and avoid hidden persistence |

### Reuse Or Integrate

| Existing layer | Use it for | Do not use it as |
|---|---|---|
| MCP | agent-facing tool invocation and schema contracts | the authorization model for host execution |
| OAuth/OIDC | HTTP MCP and operator/client authentication | a substitute for host-bound job signatures |
| Tailscale/headscale | managed-device reachability and inventory hints | job authorization |
| SSH/OpenSSH certs | owned-host fallback and admin diagnostics | temporary third-party default |
| Coder | governed cloud workspaces and agent workspaces | the entire rdev control/audit/approval model |
| DevPod/devcontainers | reproducible disposable workspaces | host enrollment or approvals |
| VS Code Remote Tunnels | inspiration for outbound tunnel UX | an agent job protocol |
| RustDesk/MeshCentral | explicit GUI view/control adapter | hidden default control channel |
| Sigstore/Cosign/platform signing | release verification and supply-chain trust | runtime job approval |

This boundary prevents the project from rebuilding every remote development product while still solving the part those products do not solve: safe delegation from autonomous agents to real machines.

## Reference Deployment

For Eitan's personal deployment:

```text
Hermes / Lucky
  - installs Remote Dev Skillkit Agent Skills
  - calls rdev MCP tools
  - summarizes evidence and asks for approvals

https://api.lunflux.com
  - authenticated rdev API
  - MCP HTTP endpoint
  - tickets, hosts, jobs, artifacts, audit
  - policy, approval, signing, revocation

https://agent.lunflux.com
  - human join page
  - signed bootstrap manifests
  - signed release downloads
  - outbound WSS relay endpoint

Managed hosts
  - Eitan's Mac, Windows, and Linux machines
  - explicit service mode with watchdog and uninstall path

Temporary hosts
  - third-party or short-lived repair machines
  - foreground-only, no service, TTL-bound
```

`api.lunflux.com` and `agent.lunflux.com` may be served by the same binary at first, but their responsibilities should stay conceptually separate.

### Personal Production Topology

The first real deployment should be single-operator and production-shaped:

```text
Hermes/Lucky
  -> MCP HTTP or local MCP stdio bridge
  -> https://api.lunflux.com/v1
  -> durable rdev-gateway
  -> Postgres or SQLite, object/artifact store, append-only audit
  -> outbound host channels on https://agent.lunflux.com
```

Recommended first production choices:

| Layer | First production choice | Later option |
|---|---|---|
| Gateway state | SQLite on the server with backups | Postgres |
| Artifacts | local filesystem path with quota | S3-compatible object storage |
| Audit | append-only JSONL plus export verifier | append-only database table plus transparency-style export |
| Gateway keys | locked file or OS key store | KMS/HSM |
| Host trust | file store for dev, OS keychain/DPAPI/libsecret for managed | hardware-backed storage when available |
| Transport | HTTPS polling plus WSS | WSS with mTLS and optional mesh |
| Authentication | operator API token and session auth | OAuth/OIDC for multi-user |

The deployment may start with one binary and one VPS, but it must preserve separable responsibilities: API, join page, relay, artifact storage, audit export, signing, and policy.

## Architecture Planes

### 1. Agent Interface Plane

Purpose: give Hermes/Lucky, Codex, Claude Code, OpenCode, and similar agents a safe way to request remote work.

Surfaces:

- Agent Skills with concise workflows and progressive references;
- MCP stdio for local/single-user use;
- MCP HTTP for production deployments;
- `rdev` CLI for operators and debugging.

Design rule: agents request typed jobs, not raw machine access.

### 2. Governance Plane

Purpose: decide what is allowed, what requires approval, and what must be denied.

Owns:

- operator identity;
- agent client identity;
- gateway signing keys;
- host identities;
- release trust roots;
- policy bundles;
- approval tokens;
- revocation;
- audit;
- redaction.

This is the security kernel of the product.

### 3. Control Plane

Purpose: coordinate work without directly trusting the agent.

Owns:

- tickets;
- host registry;
- capability inventory;
- job queue;
- leases;
- artifacts;
- event stream;
- durable audit log.

State transitions must be idempotent and auditable.

### 4. Transport Plane

Purpose: move signed jobs and evidence without exposing temporary hosts to the public internet.

Default:

```text
rdev-host -> HTTPS/WSS :443 -> rdev-gateway
```

Fallback:

- short-polling HTTPS for hostile networks or temporary support.

Optional managed-device transports:

- Tailscale/headscale mesh;
- SSH over private network;
- future relay providers.

Transport identity is never enough to authorize execution. Every job still needs a signed envelope and host-side policy validation.

### 5. Execution Plane

Purpose: run bounded work on the target host.

Owns:

- host keypair;
- local trust bundle;
- local policy engine;
- capability detector;
- workspace lock manager;
- adapter runtime;
- local audit spool;
- foreground stop/revoke controls;
- managed service watchdog, only in managed mode.

The host must remain capable of saying no even if the gateway asks for something unsafe.

## Canonical Object Model

The implementation should keep these objects explicit instead of hiding them inside ad hoc JSON blobs. These are the stable nouns agents, APIs, audits, and adapters should share.

| Object | Owner | Purpose | Must be auditable |
|---|---|---|---|
| `Owner` | gateway | tenant/operator boundary; starts as `default` for single-user installs | yes |
| `Operator` | gateway/auth provider | human who approves hosts and dangerous actions | yes |
| `AgentClient` | MCP/API auth layer | agent runtime calling rdev tools | yes |
| `Ticket` | gateway | temporary invitation, TTL, reason, requested capabilities | yes |
| `Host` | host/gateway | registered machine identity and capability inventory | yes |
| `HostPolicy` | gateway and host | approved capabilities, roots, adapters, limits, gates | yes |
| `TrustBundle` | gateway and host | active/retired/revoked job-signing keys and sequence | yes |
| `JoinManifest` | gateway/release trust | signed bootstrap instructions for a ticket/session | yes |
| `ReleaseManifest` | release trust | signed artifact metadata, digests, sizes, platform policy | yes |
| `Approval` | operator/local user | scoped decision for one high-risk operation | yes |
| `JobEnvelope` | gateway | signed executable intent bound to host, policy, nonce, expiry | yes |
| `JobLease` | gateway/host | bounded claim that one host may run one job | yes |
| `AdapterRun` | host | concrete execution attempt through shell, Codex, GUI, etc. | yes |
| `Artifact` | host/gateway | redacted output, diff, tests, screenshots, files, logs | yes |
| `Denial` | gateway or host | structured explanation for refused work | yes |
| `EvidenceBundle` | gateway/exporter | review package for one job/session | yes |
| `AuditEvent` | gateway and host spool | immutable event in hash chain | yes |

Object relationships:

```text
Owner
  -> Operator
  -> AgentClient
  -> Ticket -> Host -> HostPolicy
  -> TrustBundle
  -> Approval
  -> JobEnvelope -> JobLease -> AdapterRun -> Artifact
  -> EvidenceBundle
  -> AuditEvent*
```

The host must store only the subset it needs to validate and execute: host identity, trust bundle, host policy, nonce cache, local audit spool, leases in progress, and adapter state. The gateway stores the authoritative control state.

## Closed Control Loop

Remote development should follow the same loop every time, regardless of adapter:

```text
1. Discover or invite host
2. Register host identity
3. Approve scoped policy
4. Dry-run policy decision
5. Sign job envelope
6. Lease job to bound host
7. Validate again locally
8. Run adapter
9. Produce evidence
10. Review result
11. Approve escalation or finish
12. Revoke or leave managed policy in place
```

This loop is the core product. A transport, coding CLI, or GUI tool can improve a step, but it cannot skip one.

### Denial Is A First-Class Result

A denied job is not an exception to hide in logs. It is a successful safety outcome and must produce a structured result with:

- stable denial code;
- human summary;
- matched policy rule or validation step;
- host id, job id, adapter, and capability when known;
- retryability;
- safer next action;
- audit event id;
- optional evidence artifact.

Required denial families:

| Family | Example codes |
|---|---|
| envelope | `job_envelope_required`, `envelope_invalid`, `envelope_expired`, `envelope_signature_invalid` |
| identity | `wrong_host`, `host_identity_mismatch`, `signing_key_mismatch` |
| trust | `trust_public_key_invalid`, `trust_bundle_revoked`, `nonce_replay` |
| policy | `unsupported_adapter`, `missing_capability`, `approval_required`, `operation_denied` |
| workspace | `workspace_required`, `workspace_escape`, `symlink_escape`, `workspace_locked` |
| adapter | `command_not_allowlisted`, `timeout`, `output_limit_exceeded`, `artifact_rejected` |

Agents should be able to recover from denials by asking for a safer job, requesting approval, narrowing scope, or showing the denial to the operator.

## Canonical State Machines

State machines are part of the product contract. They keep retries, reconnects, revocation, and audit explainable.

### Ticket

```text
created -> active -> expired
       \-> revoked
```

Rules:

- `created`: ticket exists but has not enrolled a host.
- `active`: at least one host registration is pending or approved under the ticket.
- `expired`: TTL elapsed; no new host may register.
- `revoked`: operator or policy stopped the ticket; all related pending hosts and queued jobs are canceled.

### Host

```text
registered -> pending_approval -> approved -> online -> offline
                         \          \-> revoked
                          \-> rejected
```

Rules:

- `registered`: host identity and capability inventory received.
- `pending_approval`: host is visible but cannot execute jobs.
- `approved`: scoped policy has been issued.
- `online`: active authenticated channel or recent heartbeat.
- `offline`: no active channel, but managed hosts may reconnect.
- `rejected`: enrollment denied; no jobs.
- `revoked`: all future claims denied and queued/running jobs canceled.

Temporary hosts that go offline after stop, expiry, or revoke are not auto-resurrected. Managed hosts may reconnect because service mode was explicit.

### Job

```text
created -> signed -> queued -> leased -> running -> completed
                                      \-> failed
                                      \-> canceled
                                      \-> expired
```

Rules:

- `created`: requested by agent or operator.
- `signed`: gateway policy accepted and signed the envelope.
- `queued`: waiting for the bound host.
- `leased`: host claimed the job for a bounded lease.
- `running`: adapter started.
- `completed`: result and artifacts uploaded idempotently.
- `failed`: adapter or policy failed with an audited reason.
- `canceled`: operator, ticket, host, or key revocation stopped it.
- `expired`: no valid lease or envelope remains.

### Approval

```text
requested -> granted -> consumed
          \-> denied
          \-> expired
          \-> revoked
```

Rules:

- approvals are scoped to one host, job, capability, time window, and operation;
- agents can request and explain approvals but cannot grant them;
- approval tokens are signed and auditable;
- one approval must not silently authorize a broader future operation.

## Discovery And Enrollment

The product needs "active discovery" without becoming a network scanner or surprise remote admin tool.

### Discovery Sources

| Source | Applies to | Behavior |
|---|---|---|
| Managed registry | Eitan-owned devices | gateway lists approved hosts, last heartbeat, capabilities, roots, and health |
| Invitation ticket | temporary support | gateway creates join URL and one-time code |
| Mesh inventory | owned devices on Tailscale/headscale | import device metadata as hints, then require rdev host enrollment |
| Local LAN hinting | same-network owned devices | optional mDNS/Bonjour advertisement in managed mode only |
| Manual host claim | unknown device | user runs bootstrap and host enters pending approval |

Discovery is not authorization. A discovered host cannot receive jobs until it has a host identity, an approved policy, and a valid trust bundle.

### Enrollment Patterns

#### Temporary Pull Enrollment

Best for third-party machines:

```text
Lucky creates ticket
  -> Eitan sends join URL or command
  -> remote user runs visible bootstrap
  -> host connects outbound
  -> Eitan approves scoped capability set
  -> jobs run until TTL, stop, revoke, or completion
```

This is the safest default for machines Eitan does not own.

#### Managed Push Enrollment

Best for Eitan-owned devices:

```text
operator installs rdev-host
  -> host generates durable identity
  -> gateway approves managed policy
  -> service starts with watchdog and reconnect
  -> Lucky can later select the host for jobs
```

This requires explicit local install and an uninstall path.

#### Mesh-Assisted Enrollment

Best for already owned devices inside a private network:

```text
mesh shows device
  -> rdev invites or installs host over an approved admin channel
  -> rdev host still generates its own identity
  -> gateway still signs jobs
```

Mesh identity helps reachability and inventory. It does not replace rdev identity, policy, approval, or audit.

### Enrollment Anti-Patterns

- no internet-wide scanning;
- no automatic installation on discovered machines;
- no credential reuse from SSH, RDP, SMB, or browser sessions;
- no hidden persistence on temporary hosts;
- no "trust this host forever" shortcut for third-party machines;
- no capability approval based only on hostname or IP address.

## Host Modes

| Mode | Use Case | Persistence | Privilege | UX Requirement |
|---|---|---:|---|---|
| `attended-temporary` | third-party repair, one-off machine | none | normal user | visible foreground, stop button, TTL |
| `managed` | Eitan-owned or formally managed machine | explicit service | least privilege | install summary, health, uninstall path |
| `break-glass` | emergency repair | explicit short-lived session | JIT elevation only | extra warnings, short TTL, dense audit |

The modes must not silently upgrade into each other.

Temporary mode can support powerful repair actions, but each high-risk action requires an operator or local-user approval gate. Managed mode can reconnect after reboot, but only after explicit installation.

## Trust Roots And Keys

The final system has separate keys for separate jobs.

| Trust Material | Holder | Purpose |
|---|---|---|
| Release signing key | maintainer/KMS | signs release manifests and artifacts |
| Bootstrap manifest key | maintainer/KMS | signs join/bootstrap metadata |
| Gateway job signing key | gateway | signs executable job envelopes |
| Host identity key | host | proves host identity and binds jobs |
| Approval token key | gateway | binds human decisions to exceptions |
| Audit hash chain | gateway and host spool | detects tampering and gaps |

Key separation matters. A release-signing compromise should not automatically authorize jobs, and a gateway job-signing rotation should not invalidate all release artifacts.

Managed hosts receive trust updates through signed trust-bundle rotation. Temporary hosts pin trust through the join manifest and session bootstrap.

### Identity And Trust Protocols

#### Release Trust

Release trust answers: "Is this software artifact produced by the project?"

Required objects:

- `rdev.release-artifact.v1` for each binary or script;
- release manifest index for a complete release;
- SHA-256 digest and size for every artifact;
- key id, signature, and validity window;
- Windows Authenticode evidence for Windows executables and scripts when policy requires it.

Bootstrap must verify release trust before executing `rdev-host`.

#### Bootstrap Trust

Bootstrap trust answers: "Is this join instruction for this session and this gateway?"

Required objects:

- signed join manifest;
- ticket code or ticket id;
- gateway URL;
- server identity;
- mode and TTL;
- requested capabilities;
- gateway job-signing trust bundle or pin;
- release artifact URLs and expected digests;
- bootstrap manifest signer id and signature.

Temporary hosts should treat the signed join manifest as session-scoped trust. Managed hosts use it only for first enrollment or explicit re-enrollment.

#### Gateway Job Trust

Gateway job trust answers: "May this gateway issue executable work?"

Required objects:

- `rdev.trust-bundle.v1`;
- active, retired, and revoked job-signing keys;
- monotonically increasing sequence;
- previous bundle hash;
- validity window;
- signature by an already trusted active key or configured root.

Hosts verify every job envelope against the current active key in the local trust bundle. Revoked keys must reject new jobs even if the signature mathematically verifies.

#### Host Identity Trust

Host identity answers: "Is this the same enrolled host?"

Required objects:

- per-host Ed25519 keypair generated locally;
- host key id;
- public key and fingerprint in registration;
- registration proof signed by the host key;
- protected local key storage;
- managed host identity continuity across restarts.

Temporary host identities are session-scoped by default. Managed host identities are durable and revocable.

#### Approval Trust

Approval trust answers: "Did a human authorize this specific exception?"

Required objects:

- approval id;
- operator id or local-user approval source;
- job id and host id;
- exact capability or operation;
- expiration;
- signed approval token;
- audit event for request and decision.

Approval tokens are not ambient permissions. They are consumed by one bounded action.

### Trust Update Rules

Managed hosts accept a trust bundle update only when all checks pass:

1. canonical JSON parses as `rdev.trust-bundle.v1`;
2. signature verifies against the currently trusted active key or root;
3. sequence is greater than the stored sequence;
4. `previous_bundle_hash` matches the stored bundle hash;
5. bundle validity window is current;
6. signing key is active and not revoked;
7. authority scope matches the bundle's intended use.

Temporary hosts do not perform background trust updates. They get a fresh join manifest for each session.

### Compromise Boundaries

| Compromise | Blast radius | Required response |
|---|---|---|
| Agent client token | can request jobs until revoked; cannot self-approve dangerous actions | revoke client token, audit requests |
| Gateway job-signing key | can sign jobs until hosts receive revocation | publish trust-bundle revocation, cancel jobs, rotate key |
| Bootstrap signer | can mint malicious join manifests | revoke active tickets/manifests, rotate signer |
| Release signer | can bless malicious artifacts | freeze releases, revoke signer, re-issue release |
| Host identity key | attacker may impersonate that host | revoke host, rotate host identity through re-enrollment |
| Artifact store | may hide or alter evidence | verify artifact checksums and audit chain |

No single compromise should grant all three powers: publish software, enroll hosts, and authorize execution.

## Bootstrap Protocol

### Temporary Windows

The target user receives a join page with:

- operator identity;
- server identity;
- reason;
- mode;
- TTL;
- requested capabilities;
- stop instructions;
- inspectable command/script.

The one-command path should:

1. download a signed bootstrap or release manifest;
2. verify the verifier or host binary by SHA-256 and signature;
3. run `rdev-host` in foreground temporary mode;
4. generate a local host keypair;
5. connect outbound to `agent.lunflux.com`;
6. wait in `pending` until Eitan approves;
7. leave no service behind.

The bootstrap must not weaken execution policy, disable Defender, bypass UAC, or install persistence in temporary mode.

### Bootstrap Decision Matrix

| Platform | Temporary bootstrap | Managed install | Baseline dependency |
|---|---|---|---|
| Windows | one visible PowerShell command, foreground console | explicit Windows Service install | PowerShell 5.1, outbound HTTPS |
| macOS | POSIX shell/curl foreground host | explicit LaunchAgent or LaunchDaemon | `/bin/sh`, curl or fallback |
| Linux | POSIX shell/curl foreground host | explicit systemd user/system service | `/bin/sh`, curl/wget fallback |

Temporary bootstrap rules:

- download into a temporary directory;
- verify the verifier first when a verifier binary is used;
- verify the host binary before execution;
- show operator, reason, gateway, TTL, and stop instruction;
- generate host identity locally;
- connect outbound only;
- remove temporary files on normal exit when possible;
- never install a service, scheduled task, LaunchAgent, systemd unit, Run key, login item, or firewall rule.

Managed bootstrap rules:

- require an explicit managed command, separate from temporary mode;
- write a service definition only after local confirmation;
- use least-privilege account defaults;
- persist host identity and trust bundle in protected storage;
- register restart policy and watchdog only in managed mode;
- install an explicit uninstall command and audit the installation.

### Managed Machines

Managed install is a separate explicit command:

```bash
rdev host install-service --mode managed --gateway https://api.lunflux.com
```

It must display:

- what service or LaunchAgent/systemd unit will be installed;
- which account it runs as;
- what roots it may access;
- how updates work;
- how to stop and uninstall;
- the pinned gateway/release trust roots.

### One-Command Temporary Windows Target

The user-facing flow can be one command, but it should not be opaque magic. The join page should also expose the script body and verification material.

The command shape should be:

```powershell
powershell -NoProfile -Command "$p=Join-Path $env:TEMP 'rdev-bootstrap-<code>.ps1'; Invoke-WebRequest 'https://agent.lunflux.com/j/<code>/bootstrap.ps1' -OutFile $p; $s=Get-AuthenticodeSignature $p; if ($s.Status -ne 'Valid') { throw 'Invalid bootstrap signature' }; & $p"
```

This is only acceptable when the downloaded script is Authenticode-signed, performs release checks before executing `rdev-host`, and does not weaken machine policy. The project should not rely on `Invoke-Expression` as the recommended path. For stricter environments, the join page should offer a download-and-inspect flow:

```powershell
iwr https://agent.lunflux.com/j/<code>/bootstrap.ps1 -OutFile .\rdev-bootstrap.ps1
Get-Content .\rdev-bootstrap.ps1
powershell -NoProfile -File .\rdev-bootstrap.ps1
```

The inspectable path is mandatory for trust-sensitive use even if the one-command path exists for speed.

## Job Protocol

Every executable action is a signed job envelope.

Required job properties:

- schema version;
- job id;
- host id;
- ticket id or managed policy id;
- operator id;
- agent client id;
- issued/expiry timestamps;
- nonce;
- adapter;
- intent;
- workspace root and write scope;
- capabilities;
- limits;
- approval requirements;
- payload;
- key id and signature.

Host validation order:

1. parse canonical envelope;
2. verify signature and key id;
3. verify host binding;
4. verify expiry;
5. verify nonce replay protection;
6. verify active ticket or managed policy;
7. verify adapter is allowed;
8. verify capabilities are allowed;
9. verify workspace boundary and symlink behavior;
10. verify required approvals;
11. execute or reject with an audited reason.

The gateway should refuse to sign unsafe jobs. The host should still reject unsafe jobs. Both are required.

## Capability Rings

Capabilities are deny-by-default and grouped by risk.

| Ring | Examples | Default |
|---|---|---|
| Ring 0: observe | capability scan, git status, read logs, doctor | allowed when ticket permits |
| Ring 1: scoped workspace | write inside approved repo, run tests, create diff | allowed with workspace policy |
| Ring 2: system mutation | package install, process kill, service restart, elevation, GUI control | requires approval |
| Ring 3: external consequence | push, merge, deploy, publish, paid API/job, credential change | requires stronger approval |

Agents may request approval and explain the need. Agents cannot approve their own escalation.

`secrets.read` should not be a normal capability. The host may use local credentials through OS keychains, Git credential managers, or tool-native auth, but raw secret values should not leave the host.

### Capability Vocabulary

The stable vocabulary should stay boring and reviewable.

| Capability | Ring | Meaning |
|---|---:|---|
| `host.inspect` | 0 | read OS/tool/network readiness metadata |
| `fs.read.scoped` | 0 | read files under approved roots |
| `logs.read.scoped` | 0 | read approved logs |
| `git.status` | 0 | inspect repo state |
| `git.diff` | 0 | produce diffs and changed-file lists |
| `fs.write.scoped` | 1 | write only under approved roots |
| `shell.user` | 1 | run allowlisted argv as the current user |
| `powershell.user` | 1 | run allowlisted PowerShell commands/scripts |
| `tests.run` | 1 | run approved verification commands |
| `agent.codex` | 1 | run Codex adapter inside workspace policy |
| `agent.claude_code` | 1 | run Claude Code adapter inside workspace policy |
| `pkg.install` | 2 | install or update packages |
| `process.control` | 2 | kill, restart, or signal processes |
| `service.control` | 2 | install, restart, or remove services |
| `elevation.request` | 2 | request admin/sudo elevation |
| `gui.view` | 2 | view screen or screenshots |
| `gui.control` | 2 | control keyboard/mouse/window state |
| `git.push` | 3 | push commits or tags |
| `git.merge` | 3 | merge branches or open merge/pull requests |
| `deploy.run` | 3 | deploy, publish, release, or change production |
| `credentials.change` | 3 | modify tokens, keys, passwords, or secret stores |
| `paid_action` | 3 | start paid API calls, jobs, or purchases |

Every capability must map to one of three default decisions: allow, approval required, or deny. Unknown capabilities default to deny.

### Approval Policy Defaults

| Operation | Temporary host | Managed host |
|---|---|---|
| observe/triage | ticket-scoped allow | policy-scoped allow |
| workspace writes | approval or ticket-scoped allow | workspace-policy allow |
| package install | approval required | approval required |
| elevation | local user and operator approval | operator approval plus platform prompt |
| GUI view | local user and operator approval | operator approval, local indicator when possible |
| GUI control | local user and operator approval | stronger approval |
| service install/remove | denied in temporary mode | approval required |
| push/merge/deploy/publish | approval required | approval required |
| credential read/export | deny | deny |
| credential use through local tool | approval required when high impact | policy/approval scoped |

## Adapter Model

Adapters are the product's compatibility layer.

Required contract:

```text
detect() -> capability inventory
prepare(job) -> workspace/session
run(job, limits) -> event stream
verify(job) -> evidence
summarize(job) -> result
cleanup(job) -> status
```

Adapter priority:

1. `shell` and `powershell` for safe diagnostics and repair primitives;
2. `git` for workspace, branch, worktree, diff, and commit evidence;
3. `codex` for Eitan's current Mac-based coding workflow;
4. `claude-code` for alternate coding agents;
5. `acp` or agent-native protocols when available;
6. `browser-e2e` for Playwright/browser evidence;
7. `gui` for explicit screen-level support.

The safe shell adapter is necessary, but it is not the final abstraction. Coding work should prefer agent-native adapters or structured protocols wherever possible.

### Adapter Taxonomy

Adapters fall into five families. The family determines default approvals, expected evidence, and how much ambient authority the adapter may hold.

| Family | Adapters | Primary use | Default authority |
|---|---|---|---|
| diagnostic | `host.inspect`, `shell`, `powershell`, `logs` | understand the machine before changing it | observe and bounded commands |
| workspace | `git`, `filesystem`, `tests`, `browser-e2e` | change and verify project workspaces | scoped write with locks |
| coding-agent | `codex`, `claude-code`, `opencode`, `acp` | delegate implementation to a local coding CLI/protocol | scoped workspace plus evidence |
| infrastructure | `coder`, `devpod`, `ssh`, `mesh` | reach or provision owned development environments | transport/workspace only |
| interactive | `gui`, `rustdesk`, `meshcentral`, native screen APIs | last-resort visual repair and support | explicit view/control approval |

Adding an adapter means implementing the adapter contract and mapping its actions to capabilities. It must not add a private side channel around policy, approvals, workspace locks, redaction, or audit.

### Adapter Execution Rules

All adapters must implement the same safety shell:

- declare required capabilities before execution;
- receive a canonical workspace root and write scope;
- refuse symlink/path escapes after resolving real paths;
- stream bounded events;
- honor cancellation;
- enforce duration, output, and artifact limits;
- redact host-side before upload;
- return schema-versioned result artifacts;
- emit audit events for prepare, start, finish, cleanup, and denial.

Adapter-specific policy:

| Adapter | Final role | Non-negotiable rule |
|---|---|---|
| `shell` | portable diagnostics and narrow repairs | argv allowlist, no shell interpolation by default |
| `powershell` | Windows diagnostics and repairs | signed script blocks or allowlisted commands, no execution-policy weakening |
| `git` | branch, worktree, diff, commit evidence | push/merge/tag/delete require approval |
| `codex` | primary managed Mac coding flow | run in locked worktree, return diff/tests, no auto-push |
| `claude-code` | alternate coding CLI | same workspace and approval rules as Codex |
| `acp` | protocol-native agent coding | prefer structured messages over raw shell |
| `browser-e2e` | browser evidence and tests | scoped URLs and artifact redaction |
| `gui` | last-resort support | visible consent, separate view/control approvals |
| `mesh` | connectivity helper | never authorizes execution by itself |
| `coder` | governed remote workspace integration | workspace provider only; rdev still signs jobs and audits evidence |
| `devpod` | disposable devcontainer workspace integration | provision/run workspace, not host enrollment authority |

### Third-Party Runtime Integration

Existing products should be integrated as runtimes behind rdev, not made the security root.

| Runtime | Integration shape | Why |
|---|---|---|
| Coder | `workspace` adapter that creates/selects a Coder workspace, then runs an rdev host or agent adapter inside it | Coder is strong at governed workspaces and agent infrastructure |
| DevPod | `workspace` adapter that provisions a devcontainer and exposes it as a bounded workspace | DevPod is strong at reproducible disposable environments |
| VS Code Remote Tunnels | optional operator/developer access path for owned hosts | useful tunnel UX, but not an agent execution contract |
| DesktopCommanderMCP-like tools | local-host adapter for filesystem/process primitives | useful capability backend, but must sit behind rdev policy |
| SSH MCP tools | managed-host fallback transport | useful for owned hosts; unsafe as temporary third-party default |
| RustDesk/MeshCentral | GUI adapter | use only with visible consent and separate approvals |

The integration rule is simple: rdev can call other systems, but other systems do not get to bypass rdev's signed jobs, capability rings, approval gates, and audit.

### Coding Adapter Contract

Coding adapters should expose a higher-level job than "run this command":

```json
{
  "task": "fix failing tests",
  "repo_root": "/approved/repo",
  "base_ref": "main",
  "worktree": ".rdev/worktrees/job_...",
  "allowed_commands": ["go", "npm", "pnpm", "pytest"],
  "verification": ["go test ./...", "npm test"],
  "deliverables": ["diff", "test_results", "risk_summary"]
}
```

The adapter may call Codex, Claude Code, or another CLI internally, but the host policy still owns workspace boundaries, approvals, evidence, and cleanup.

## Workspace Model

Every write job needs a workspace.

Rules:

- canonicalize paths before policy checks;
- reject symlink escapes;
- write only inside declared roots;
- one active writer per workspace;
- use Git worktrees for parallel jobs;
- capture changed files and diff summary;
- require approval before destructive Git operations;
- require approval before push, merge, deploy, or publish.

For Eitan's managed Mac, the primary golden path is:

```text
select managed Mac
  -> lock repo
  -> create rdev worktree/branch
  -> run Codex adapter
  -> run verification
  -> return diff/test evidence
  -> ask before push/merge
```

### Workspace Lock Semantics

Workspace locks prevent multiple agents from trampling the same checkout.

Required lock fields:

- lock id;
- host id;
- job id;
- canonical repo root;
- worktree path if used;
- base ref;
- created timestamp;
- lease expiry;
- owner adapter;
- cleanup status.

Rules:

- one writer may own a repo root unless isolated worktrees are enabled;
- stale locks can be recovered only after lease expiry or explicit cancel;
- lock acquisition and release are audit events;
- failed cleanup leaves a visible diagnostic artifact;
- a job cannot widen its workspace after signing.

### Git Worktree Strategy

Managed coding should prefer:

```text
repo/.rdev/worktrees/job_<job_id>
repo/.rdev/branches/rdev/job_<job_id>
```

The adapter should:

1. verify the baseline is clean or record dirty state;
2. create a task branch;
3. run the coding CLI in the worktree;
4. run verification commands;
5. collect `git status --short`, `git diff --stat`, and full diff artifact;
6. leave branch/worktree for review unless cleanup is explicitly requested;
7. require approval before push, merge, rebase of protected branches, tag, or force operation.

Temporary support sessions may use a scratch workspace when the target machine is not a development host.

## Audit And Evidence

Audit is a product surface, not debug logging.

Every meaningful step emits an event:

- ticket creation and expiry;
- host registration, approval, rejection, revoke;
- policy decision;
- approval request and approval result;
- job enqueue, lease, run, complete, fail, cancel;
- adapter action;
- artifact creation;
- redaction decision;
- trust-bundle update;
- release verification;
- service install/uninstall.

Audit events should be append-only and hash-chained:

```text
event_hash = hash(canonical_event_without_hash + previous_event_hash)
```

Artifacts should include:

- schema version;
- adapter name/version;
- command argv or structured operation;
- working directory;
- start/end timestamps;
- exit code;
- redacted stdout/stderr excerpts;
- redaction counts;
- file checksums;
- diff/test evidence where applicable.

Raw secrets must not be uploaded when redaction can happen host-side.

### Evidence Bundle

Every completed or failed job should be reviewable through a compact evidence bundle:

```text
job_<id>/
  envelope.json
  policy-decision.json
  adapter-result.json
  stdout.redacted.txt
  stderr.redacted.txt
  files.json
  diff.patch
  tests.json
  approvals.json
  audit-slice.jsonl
  checksums.txt
```

Not every adapter produces every file, but every bundle must include:

- signed envelope or envelope hash;
- policy decision;
- adapter result;
- artifact checksums;
- redaction metadata;
- audit slice from job creation through terminal state.

### Redaction Rules

Redaction happens first on the host, then optionally again at the gateway.

The host should redact:

- API keys and bearer tokens;
- GitHub tokens;
- AWS access key ids and secret-like assignments;
- PEM private keys;
- common `password`, `token`, `api_key`, `secret`, and `authorization` values;
- user-configured patterns.

The gateway should refuse artifact upload when the artifact exceeds size limits, omits schema version, or declares redaction failure for known high-risk content.

## Reliability Model

The system should assume bad networks and messy hosts.

Host requirements:

- heartbeat;
- exponential reconnect;
- local inbox/outbox;
- local audit spool;
- cooperative cancellation;
- per-job timeout;
- crash recovery in managed mode;
- visible stop control in temporary mode;
- idempotent completion upload.

Gateway requirements:

- durable state;
- leases;
- idempotent APIs;
- retry only safe operations;
- cancel jobs on host revocation;
- revoke tickets, hosts, policies, approvals, jobs, and keys;
- exportable diagnostics bundle.

Temporary mode should not auto-recover into hidden persistence. Managed mode may use OS-native service recovery because the user explicitly installed it.

### Transport Reliability

The transport must support two levels:

| Level | Use case | Behavior |
|---|---|---|
| HTTPS polling | maximum compatibility, temporary repair | simple claim/complete loop, longer latency |
| WSS | normal production | bidirectional events, faster cancel/revoke, streaming artifacts |

WSS is the preferred final transport, but HTTPS polling should remain as a compatibility fallback for restrictive networks.

Connection behavior:

- host authenticates the channel with its host identity after registration;
- gateway authenticates through TLS and pinned trust material;
- reconnect uses exponential backoff with jitter;
- heartbeats include host id, policy id, trust bundle hash, adapter health, and current job id;
- gateway treats missed heartbeats as offline, not revoked;
- cancellation and revocation must be checked before each job claim and during long-running jobs;
- completion upload is idempotent by job id and result hash.

### Managed Service Recovery

Managed mode may use native recovery:

| Platform | Service model | Recovery behavior |
|---|---|---|
| Windows | Windows Service | restart on failure, event log entry, explicit uninstall |
| macOS | LaunchAgent for user context, LaunchDaemon only when justified | KeepAlive only after managed install, log path disclosed |
| Linux | systemd user or system service | `Restart=on-failure`, watchdog when supported |

Temporary mode must not use these recovery paths.

### Offline And Crash Recovery

Managed hosts should maintain:

- local audit spool;
- local outbox for completion artifacts;
- local nonce/replay cache;
- local trust bundle;
- current job checkpoint when the adapter supports it.

After restart, the host should:

1. load identity and trust;
2. upload pending audit/outbox items;
3. report interrupted jobs;
4. refuse expired envelopes;
5. reacquire or release locks based on gateway state.

No adapter should silently resume a destructive operation after crash without a fresh policy decision.

## Security Operating Model

The final system must be secure by product shape, not by warnings.

### Default Deny

Default-deny applies to:

- unknown hosts;
- unknown capabilities;
- unknown adapters;
- expired tickets;
- expired approvals;
- wrong host ids;
- revoked keys;
- workspace paths outside policy;
- artifact schemas without known versions.

### Human Approval Gates

The approval UI, CLI, or MCP surface must show:

- requesting agent;
- target host;
- exact operation;
- capability ring;
- workspace/path scope;
- command or structured operation;
- expected side effect;
- expiry;
- safer alternative if available.

Approvals should be boring, explicit, and narrow.

### Local User Consent

For third-party temporary support, some actions need both Eitan/operator approval and local user visibility:

- elevation;
- GUI view/control;
- service changes;
- credential changes;
- destructive operations outside an approved scratch workspace.

If local user consent cannot be obtained, the host should deny rather than hide the action.

### Supply Chain

Production releases require:

- reproducible or at least clean CI builds from tags;
- signed release manifests;
- checksum verification;
- Windows Authenticode for Windows artifacts when distributed publicly;
- macOS notarization when distributing macOS binaries publicly;
- SBOM for public releases;
- documented key rotation and emergency revocation.

### Privacy

The product should collect enough evidence to review work, not enough to surveil a machine.

Rules:

- no blanket filesystem indexing;
- no background screen capture;
- no credential exfiltration;
- no uploading unrelated user files;
- no silent host inventory beyond capability metadata;
- artifacts default to excerpts and checksums when full content is unnecessary.

## Data Stores

| Data | Local/Solo | Hosted/Multi-User |
|---|---|---|
| control state | SQLite | Postgres |
| jobs and leases | SQLite | Postgres or queue-backed leases |
| artifacts | filesystem | object storage |
| audit | JSONL/hash chain | append-only table/log plus export verifier |
| signing keys | OS keychain or locked file | KMS/HSM/sealed secret |
| host keys | OS keychain/DPAPI/keychain/libsecret | OS-native protected storage |

The current in-memory gateway should remain a development mode only.

### Production Storage Boundaries

The gateway should separate:

- control state: tickets, hosts, jobs, approvals, leases;
- trust state: keys, bundles, revocations, release roots;
- audit state: append-only events;
- artifacts: job outputs and evidence bundles;
- configuration: policies, limits, adapter allowlists.

Backups must include control state, trust state, audit state, and artifact metadata. Artifact blobs may use lifecycle retention, but audit records and checksums must remain.

### Multi-Tenant Future

The first product is single-operator. The schema should still avoid painting itself into a corner:

- every row/event includes tenant or owner id, even if only `default`;
- every host belongs to one owner;
- every agent client belongs to one owner;
- every trust bundle has an authority scope;
- every artifact has retention and classification metadata.

## Agent Skills And MCP Shape

The open-source package should make agents safer by default.

Core skills:

- `remote-vibe-coding`: use managed hosts for coding work;
- `safe-remote-support`: run temporary repair sessions;
- `host-triage`: inspect host readiness before changes;
- `remote-job-review`: evaluate evidence before completion;
- `adapter-authoring`: add new adapters safely.

Each skill should keep `SKILL.md` small and use references for scenarios such as Windows repair, managed Mac coding, GUI support, and production deployment.

Stable MCP tool groups:

- `rdev.tickets.*`
- `rdev.hosts.*`
- `rdev.jobs.*`
- `rdev.approvals.*`
- `rdev.artifacts.*`
- `rdev.audit.*`
- `rdev.policy.*`
- `rdev.release.*`

There should be no default `run_anything` tool.

### MCP Tool Decisions

The MCP surface should optimize for agent reliability and safety:

| Tool group | Examples | Notes |
|---|---|---|
| `rdev.tickets.*` | create, get, revoke | temporary sessions start here |
| `rdev.hosts.*` | list, inspect, approve, revoke | approval is operator-only |
| `rdev.jobs.*` | create, get, cancel, tail | creates typed jobs, not shell access |
| `rdev.approvals.*` | request, list, resolve | agents may request; humans resolve |
| `rdev.artifacts.*` | list, get, export_bundle | evidence retrieval |
| `rdev.audit.*` | query, export, verify_chain | review and compliance |
| `rdev.policy.*` | explain, dry_run, list_capabilities | preflight before job creation |
| `rdev.release.*` | verify_manifest, list_roots | bootstrap/release validation |

Tool responses should be structured JSON with:

- stable schema version;
- machine-readable status;
- human-readable summary;
- next safe actions;
- artifact ids instead of huge inline blobs;
- redaction status;
- audit event ids.

### Agent Skill Packaging

The open-source skill package should be installable by different agent runtimes:

```text
skills/
  remote-vibe-coding/
    SKILL.md
    references/managed-mac-codex.md
    references/workspace-policy.md
  safe-remote-support/
    SKILL.md
    references/windows-temporary.md
    references/approval-gates.md
  host-triage/
    SKILL.md
    references/capability-inventory.md
  remote-job-review/
    SKILL.md
    references/evidence-review.md
  adapter-authoring/
    SKILL.md
    references/adapter-contract.md
```

Each `SKILL.md` should be short enough for an agent to load eagerly. Scenario references carry detailed runbooks.

### Agent Behavior Rules

Skills should teach agents to:

- triage before changing;
- ask for a temporary ticket rather than SSH credentials;
- prefer managed hosts for coding;
- use policy dry-run before job creation;
- request approvals with concrete reasons;
- read artifacts and audit before claiming success;
- avoid asking users to paste secrets;
- revoke temporary sessions at the end.

## Open-Source Package Shape

```text
remote-dev-skillkit/
  cmd/
    rdev/
    rdev-host/
    rdev-gateway/
    rdev-mcp/
    rdev-verify/
  internal/
    gateway/
    hostrunner/
    policy/
    envelope/
    release/
    adapters/
    audit/
    approval/
    transport/
  skills/
    remote-vibe-coding/
    safe-remote-support/
    host-triage/
    remote-job-review/
  mcp/
    tools.json
  scripts/
    bootstrap/
  docs/
    architecture/
    operations/
    security/
    project/
```

Go remains the right implementation language for the host/gateway core because it produces cross-platform single binaries and has good support for TLS, Ed25519, Windows services, systemd, and CLI tooling.

## Implementation Order To Finish The Product

Implementation should follow maturity gates, not feature excitement. Each gate proves one layer of the operating model and leaves the next layer as an explicit integration problem.

| Gate | What it proves | What it must not pretend to prove |
|---|---|---|
| `v0.1` local safety kernel | signed jobs, host validation, denials, approval tokens, evidence, audit | production networking or OS service behavior |
| `v0.2` temporary Windows | one-command visible enrollment, release verification, outbound-only repair | unattended managed-device operations |
| `v0.3` managed Mac coding | durable owned-host workflow with Codex and workspace evidence | general multi-tenant fleet management |
| `v0.4` managed device generalization | multi-OS service managers, durable storage, adapter SDK, reconnects | public protocol stability |
| `v1.0` public Skillkit | stable schemas, safe defaults, signed releases, self-host docs | every possible adapter or remote-access product |

The implementation order deliberately brings the safety kernel before rich transports and GUI control. If a later feature requires weakening the kernel, the feature is wrong.

### Gate 1: Complete the Local Safety Kernel (`v0.1`)

Goal: local gateway and local host prove the core policy model.

Done or nearly done:

- signed job envelopes;
- dev HTTP gateway;
- host-side trust bundle verification;
- durable host trust bundle file store;
- scoped shell adapter;
- host-side shell artifact redaction;
- revocation canceling queued/running jobs;
- acceptance-test checklist.
- host identity storage wired into registration and job binding;
- nonce replay cache;
- hash-chained audit export verifier.
- stronger workspace and symlink escape tests;
- local evidence bundle export.

Still required:

- gateway/API evidence bundle export directly from job ids.

Exit criteria:

- `./scripts/check.sh` passes;
- local demo creates ticket, registers host, signs job, executes allowlisted job, stores artifact, exports verifiable audit chain;
- host rejects tampered, expired, wrong-host, wrong-key, replayed, non-allowlisted, and workspace-escaping jobs;
- host returns structured denials and approval-required artifacts instead of opaque failures;
- approval tokens are signed, scoped, expiring, and consumed once by the host;
- evidence can be exported from both local files and gateway job ids;
- README quick start can be followed by a fresh developer.

### Gate 2: Build the Temporary Windows MVP (`v0.2`)

- release-key lifecycle policy;
- Authenticode/notarization policy;
- signed Windows bootstrap;
- outbound HTTPS polling then WSS;
- foreground console/UI with stop;
- session-scoped host identity and trust bundle;
- local audit spool;
- E2E temporary repair acceptance test;
- no-persistence inspection script;
- local-user approval prompt for elevation/GUI/service requests.

Exit criteria:

- clean Windows 10/11 VM joins from one visible command;
- bootstrap verifies verifier and host binary before execution;
- host connects outbound only over HTTPS/WSS or polling fallback;
- package install/elevation/GUI/service requests pause for approval;
- host revoke cancels work;
- no Windows Service, scheduled task, Run key, startup shortcut, or firewall rule is left by temporary mode.
- a third-party user can inspect the script and stop the session without understanding rdev internals.

### Gate 3: Build Managed Mac Coding (`v0.3`)

- host identity protected storage;
- LaunchAgent managed mode;
- workspace locks and worktrees;
- Codex adapter;
- optional Claude Code or ACP adapter spike only if it does not delay Codex evidence flow;
- git diff/test evidence;
- approval before push/merge;
- managed host health and uninstall command;
- artifact bundle review flow.

Exit criteria:

- Eitan's managed Mac reconnects after reboot;
- Lucky can select the host through MCP;
- Codex runs in a locked Git worktree;
- result includes changed files, diff, tests, adapter artifact, audit slice, and residual risk;
- push, merge, deploy, credential changes, and service changes require approval.
- a failed coding job still produces enough evidence for Lucky or another reviewer to continue safely.

### Gate 4: Generalize To Multi-Host (`v0.4`)

- durable gateway storage;
- key rotation and trust-bundle update;
- OS-protected managed host identity and trust storage;
- Linux systemd;
- Windows Service;
- Claude Code and ACP adapters;
- artifact streaming;
- mesh transport adapter.

Exit criteria:

- one gateway manages multiple Mac/Windows/Linux hosts;
- service install/uninstall works per OS;
- trust-bundle rotation reaches managed hosts and rejects rollback;
- artifact streaming and audit spool survive reconnects;
- adapter SDK can add a new adapter without changing the safety kernel.
- temporary and managed host modes remain visibly separate in commands, policies, and audit.

### Gate 5: Public v1.0 Skillkit (`v1.0`)

- stable MCP schemas;
- stable protocol versions;
- installable Agent Skills;
- adapter conformance tests;
- signed multi-platform releases;
- security review;
- deployment guide;
- acceptance demos;
- disclosure policy.

Exit criteria:

- external user can install skills and MCP server without Hermes-specific assumptions;
- hosted and self-hosted docs are complete;
- release artifacts are signed and verifiable;
- golden path videos or transcripts exist for Temporary Windows Repair and Managed Coding;
- security policy, threat model, and release process are public and current.
- project users can bring Hermes, Codex, Claude Code, OpenCode, or another agent runtime without changing the trust model.

## Final Architecture Decisions

These decisions are now fixed unless a security review proves them wrong:

1. Go is the host/gateway implementation language.
2. The default target connection is outbound HTTPS/WSS from host to gateway.
3. Temporary mode is foreground-only and leaves no persistence.
4. Managed mode is explicit and uses OS-native service managers.
5. Every executable job is a signed envelope.
6. Host-side policy is mandatory even when gateway policy already approved the job.
7. Capabilities are deny-by-default and ringed by risk.
8. Approvals are scoped, signed, expiring, and auditable.
9. Agent Skills and MCP tools are the primary agent surface.
10. Shell is a constrained adapter, not the product abstraction.
11. Codex/Claude Code/ACP are coding adapters under workspace policy.
12. Tailscale/headscale, SSH, RustDesk, and MeshCentral are optional adapters/transports.
13. Release trust, bootstrap trust, gateway job trust, host identity, and approval trust are separate authorities.
14. Evidence bundles and audit export are product features, not logs.
15. Open-source distribution must ship safe defaults, not just powerful primitives.
16. Denials are structured policy results, not opaque errors.
17. The safety kernel is smaller than the adapter surface and never depends on a single runtime vendor.

## Acceptance Tests For The Perfect Ending

The project is done when all of these pass:

1. A third-party Windows host joins from one visible command, verifies signed release artifacts, connects outbound only, runs foreground temporary mode, and leaves no service behind.
2. A managed Mac receives a Lucky-requested coding job, runs Codex in a locked worktree, returns diff/test evidence, and waits for approval before push or merge.
3. Tampered, expired, wrong-host, wrong-key, or replayed envelopes are rejected by the host.
4. Workspace escapes, symlink escapes, non-allowlisted commands, and missing capabilities are rejected locally with explainable policy output.
5. Package installation, elevation, GUI control, service changes, push, merge, deploy, publish, paid jobs, and credential changes require approval.
6. Host revocation cancels queued/running jobs and prevents future claims.
7. Release manifests, host binaries, and verifier binaries are signature and checksum verified before execution.
8. Audit export can prove the sequence of ticket, host, job, approval, artifact, and revocation events.
9. Host-side redaction prevents common secret patterns from leaving the host in logs or artifacts.
10. The Agent Skills are small enough for agents to load, but complete enough to prevent unsafe remote execution habits.

## Final Product Sentence

Remote Dev Skillkit is the universal safety and orchestration layer that lets an AI agent work on real machines through explicit consent, outbound-only connections, signed jobs, local policy enforcement, approval gates, and reviewable evidence.
