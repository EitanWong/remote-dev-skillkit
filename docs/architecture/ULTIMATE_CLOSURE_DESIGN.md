# Ultimate Closure Design

Date: 2026-06-29

This document is supporting implementation detail for Remote Dev Skillkit. The
canonical final architecture lock is
[Perfect Ending Solution](PERFECT_ENDING_SOLUTION.md), which defines the state
machines, subsystem blueprint, authority map, protocol objects, discovery model,
evidence gates, and final implementation order. Keep this file as rationale and
detail unless a new architecture decision explicitly replaces the canonical
lock.

## Final Outcome

Remote Dev Skillkit is an agent-native work-control system for real machines.

It is not:

- a hidden remote administration agent;
- an SSH, RDP, VNC, or remote desktop clone;
- a raw terminal MCP server;
- a way for an agent to own a host.

It is:

```text
agent intent
  -> typed Skill/MCP/API request
  -> gateway policy dry-run
  -> signed host-bound job envelope
  -> outbound host lease
  -> host-side validation
  -> locked workspace or visible session
  -> adapter execution
  -> redacted artifacts
  -> evidence bundle
  -> hash-chained audit
  -> review, approval, continuation, cancellation, or revocation
```

The perfect ending is maximum useful delegation after explicit consent and
minimum ambient authority by default.

## Product Constitution

The system is correct only when all statements remain true:

1. Agents request typed work. They do not receive host credentials or ambient
   host ownership.
2. The gateway signs only bounded, expiring, host-specific job envelopes.
3. The host independently verifies identity, signature, expiry, nonce, trust
   bundle, policy, capability, workspace, approval, and lock state before
   execution.
4. Adapters execute work but never own authorization, approval, persistence,
   identity, release trust, or revocation.
5. Dangerous operations require scoped approval from the operator or local user
   before side effects.
6. Completion proof is evidence plus audit, not model narration.
7. Temporary hosts are visible, foreground, TTL-bound, outbound-only, and
   non-persistent.
8. Managed hosts are explicitly installed, inspectable, stoppable, uninstallable,
   and revocable.
9. Bootstrap runs host code only after release manifest and artifact verification.
10. Tickets, hosts, jobs, approvals, trust bundles, keys, sessions, and releases
    have revocation and audit paths.

## Architecture Decision

The final product is a small safety microkernel with replaceable adapters.

```text
Agent runtimes
  Hermes/Lucky, Codex, Claude Code, OpenCode, Cursor-style agents
        |
        v
Skillkit and MCP/API surface
  safe workflows, typed tools, policy dry-runs, evidence review
        |
        v
rdev-gateway
  tickets, host registry, policy, signing, approvals, leases,
  artifacts, audit, trust bundles, revocation
        |
        v
Outbound host channel
  HTTPS long-poll fallback, WSS production path, optional mesh for owned hosts
        |
        v
rdev-host
  local identity, trust store, policy verifier, nonce and approval stores,
  workspace locks, adapter runner, local stop, local audit spool
        |
        v
Adapters
  shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI,
  Tailscale/headscale, SSH, Coder, DevPod, devcontainers
```

The gateway coordinates. The host remains sovereign over local execution. The
adapter layer is useful but never trusted as the security root.

## Plane Boundaries

| Plane | Owns | Must not own |
|---|---|---|
| Agent interface | Skills, MCP tools, typed requests, policy explanations, evidence review | host credentials, approval authority, default raw shell |
| Gateway governance | tickets, hosts, leases, policy, signing, approvals, artifacts, audit, revocation | local host execution, adapter trust roots, release signing by itself |
| Host sovereignty | local identity, trust bundle, nonce store, approval consumption, local policy, locks, stop control, adapter runner | broader authority than gateway and local policy grant |
| Adapter execution | shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, mesh, Coder, DevPod work | authorization, approval, persistence, identity, release trust |
| Review and release | evidence bundles, audit export, signed release manifests, checksums, rollback | runtime job authorization |

If a transport, CLI, GUI tool, coding agent, mesh network, or hosted workspace
becomes the trust root, the feature is outside the final architecture until it is
wrapped behind the safety kernel.

## Operating Modes

| Mode | Target | Persistence | Default authority | Hard rule |
|---|---|---:|---|---|
| `attended-temporary` | third-party or short-lived support | none | foreground, TTL-bound, scoped repair | no service, autorun, hidden restart, or silent resurrection |
| `managed` | Eitan-owned or formally managed devices | explicit service | durable reconnect with approved roots | reliability never grants approval for external consequences |
| `break-glass` | urgent recovery | short-lived only | narrow emergency actions | shorter TTL, stronger approval, denser audit |
| `workspace-provider` | Coder, DevPod, devcontainers, disposable cloud workspaces | provider-owned | bounded workspace lifecycle | provider identity never replaces rdev job authorization |

Temporary mode never upgrades itself into managed mode. Managed mode never
inherits approval for push, merge, deploy, publish, paid actions, credential
changes, GUI control, service mutation, package installation, or elevation.

## Identity And Trust

The design avoids a single master credential.

| Authority | Grants | Does not grant |
|---|---|---|
| Agent client auth | permission to request tools | permission to run work on hosts or approve danger |
| Operator session | permission to approve hosts and scoped operations | permission to bypass host verification |
| Gateway job-signing key | one bounded executable envelope | release trust, host identity, or standing approval |
| Approval token key | one scoped exception for one operation | reusable privilege |
| Host identity key | proof of an enrolled host | permission to broaden local policy |
| Trust bundle key | controlled trust update | runtime execution by itself |
| Release signing key | software artifact trust | job authority |
| Audit chain | tamper evidence | authorization |

Compromising any one authority must not grant all three powers of enrolling
hosts, authorizing runtime execution, and publishing trusted software.

## Protocol Objects

Every protocol object is schema-versioned, auditable, and has a clear owner.

| Object | Owner | Critical fields | Rejection rules |
|---|---|---|---|
| `rdev.ticket.v1` | gateway | mode, reason, TTL, requested capabilities, join code | expired, revoked, mode mismatch |
| `rdev.join-manifest.v1` | gateway/release root | ticket, gateway identity, bootstrap URL, release manifest reference | bad signature, stale sequence, wrong audience |
| `rdev.host-registration.v1` | host | host public key, capability inventory, platform, local policy | ticket missing, duplicate identity misuse, unsupported mode |
| `rdev.trust-bundle.v1` | gateway trust authority | active keys, revoked keys, sequence, validity window | bad signature, rollback, expired bundle, revoked key |
| `rdev.job-envelope.v1` | gateway | host id, job id, adapter, policy, nonce, expiry, limits | tamper, wrong host, replay, expiry, missing capability |
| `rdev.approval-token.v1` | gateway/operator | operation, subject, TTL, job/host binding, one-use id | wrong subject, expired, reused, broader scope |
| `rdev.host-denial.v1` | host | reason, failing check, safe next action | never treated as opaque failure |
| `rdev.approval-required.v1` | host | required operation, scope, evidence needed | no adapter side effect before token |
| `rdev.evidence-bundle.v1` | host/gateway | envelope, policy, artifacts, checksums, redaction, audit slice | missing manifest, checksum mismatch, unverifiable audit |
| `rdev.release-manifest.v1` | release system | artifact digest, size, platform, signer, validity, rollback | unsigned, wrong digest, wrong platform, revoked signer |

The stable v1 contract is not only JSON shape. It is the behavior around expiry,
replay, redaction, revocation, and audit.

## Capability Rings

Policy is deny-by-default and shared by gateway dry-runs, host validation,
adapter planning, evidence, and Skills.

| Ring | Default posture | Examples |
|---|---|---|
| Ring 0 observe | allowed after host approval | host capability detection, `git status`, read-only logs |
| Ring 1 workspace | allowed when root is approved | scoped reads/writes, tests, build commands |
| Ring 2 repair | approval or narrow managed grant | package repair, dependency fix, process kill |
| Ring 3 privileged/visual | per-operation approval | elevation, screenshots, GUI control, service mutation |
| Ring 4 external consequence | per-operation approval after evidence review | push, merge, deploy, publish, paid action, credential change |

High-risk built-in intents are always approval-gated before adapter execution:
package install, elevation, GUI control, service management, git push, git merge,
deploy, publish, credential changes, and paid actions.

## Transport Closure

The default transport is outbound host connectivity on port 443.

| Layer | Use | Final decision |
|---|---|---|
| HTTPS long-poll | compatibility fallback and development | keep as a stable fallback |
| WSS | production interactive host channel | add mTLS or equivalent host-channel authentication |
| Mesh | owned or managed hosts | optional adapter or transport assist, not job authorization |
| SSH | owned infrastructure | optional adapter, never default for temporary third-party hosts |
| GUI relay | explicit attended visual work | separate view/control capabilities, approval, local visibility, audit |

Temporary hosts must not expose inbound ports. Managed hosts may use mesh or SSH
only after explicit enrollment and still require signed jobs and host-side
validation.

## Host Runtime Closure

The host runtime has one job: protect local sovereignty while doing useful work.

Required host services:

- local host identity generation and protected storage;
- trust bundle pinning, rotation, rollback protection, and revocation;
- nonce replay cache;
- approval token consumption store;
- workspace root validation and symlink escape rejection;
- one-writer workspace locks and isolated worktree preparation;
- local stop and TTL enforcement;
- adapter preflight before side effects;
- output caps and redaction for argv, prompts, stdout, stderr, diffs, files, and screenshots;
- local evidence/audit spool that flushes after reconnect in managed mode.

Temporary hosts run visibly in the foreground. Managed hosts use OS-native
service managers:

- macOS: LaunchAgent or LaunchDaemon, with visible plist, logs, status, stop,
  and uninstall commands;
- Windows: Windows Service only in managed mode, with explicit recovery policy,
  logs, status, stop, and uninstall commands;
- Linux: systemd user or system service, with explicit unit, logs, status, stop,
  and uninstall commands.

## Adapter SDK Closure

All adapters implement the same safety wrapper:

```text
detect(context) -> adapter_capabilities
plan(job, host_policy) -> required_capabilities, approvals, workspace_or_session_plan
prepare(job, locks, limits) -> prepared_workspace_or_session
run(job, cancellation, limits) -> events, raw_result
collect(job, raw_result) -> redacted_artifacts, checksums, evidence_manifest
cleanup(job, result) -> cleanup_status
```

Conformance tests must prove:

- explicit deny-by-default capability mapping;
- no execution before envelope and host validation;
- workspace canonicalization and symlink escape rejection;
- approval-required operations pause before side effects;
- cancellation is honored where the underlying tool allows it;
- output caps and redaction apply consistently;
- nonzero exit, timeout, cancellation, and adapter failure still produce reviewable
  artifacts or structured denials;
- cleanup is visible and auditable.

Adapter priority:

1. `shell` and `powershell` for controlled diagnostics and repair.
2. `git` for branches, worktrees, diffs, commits, and evidence.
3. `codex` for managed Mac coding.
4. `claude-code` and `acp` for broader coding-agent compatibility.
5. `browser-e2e`, GUI, mesh, Coder, DevPod, and devcontainers after the kernel is stable.

## Agent Surface

The public agent surface is a portable Skillkit plus MCP/API contract. The agent
should learn workflows, not raw power.

Required MCP tools:

| Tool | Purpose |
|---|---|
| `rdev.ticket.create` | create attended or managed enrollment intent |
| `rdev.host.list` | select hosts from policy-aware registry snapshots |
| `rdev.host.approve` | approve host capabilities and workspace roots |
| `rdev.job.plan` | dry-run policy, approvals, host fit, and expected evidence |
| `rdev.job.run` | create a signed job envelope |
| `rdev.job.status` | inspect leases, artifacts, denials, and terminal state |
| `rdev.job.cancel` | cancel queued or running work |
| `rdev.approval.create` | create scoped one-use approval tokens |
| `rdev.evidence.export` | export evidence bundles |
| `rdev.audit.verify` | verify hash-chained audit exports |
| `rdev.release.verify` | verify bootstrap and release artifacts |

Skills should teach agents to ask for the smallest useful capability, prefer
evidence over narration, request approvals only with a clear reason, and revoke
when work is complete.

## Eitan Reference Deployment

Eitan's production deployment is the golden path, not a private fork.

```text
Hermes/Lucky
  -> Remote Dev Skillkit skills
  -> MCP HTTP or local bridge
  -> https://api.lunflux.com/v1
  -> rdev-gateway
  -> tickets, hosts, jobs, approvals, artifacts, audit, signing
  -> https://agent.lunflux.com
  -> join page, signed manifests, release downloads, outbound host relay
  -> managed Eitan hosts and attended temporary hosts
```

Responsibilities:

- `api.lunflux.com/v1`: authenticated agent/operator API and MCP-compatible surface.
- `agent.lunflux.com`: human join page, bootstrap instructions, release download,
  signed manifest hosting, and host relay.
- Hermes/Lucky: agent workflow orchestration through typed tools.
- `rdev-gateway`: policy, signing, approvals, jobs, artifacts, audit, and revocation.
- `rdev-host`: local verification and execution.

One binary can serve the first production deployment, but the authority boundaries
must remain separate in code and protocol.

## Golden Paths

### Temporary Windows Repair

1. Operator creates an `attended-temporary` ticket with reason, TTL, requested
   capabilities, and server identity.
2. Remote user opens the join page and runs one visible bootstrap command.
3. Bootstrap verifies the release manifest and host binary before execution.
4. Host generates a local keypair and registers capability inventory.
5. Operator approves scoped capabilities and workspace roots.
6. Lucky runs triage and repair jobs through signed envelopes.
7. Host pauses before elevation, package install, GUI control, service changes,
   credential access, destructive actions, push, deploy, or publish.
8. Evidence bundles show commands, outputs, diffs, logs, redaction metadata, and audit.
9. Ticket and host are revoked. The machine has no service, autorun, or hidden restart.

### Managed Mac Coding

1. Managed Mac is explicitly installed with visible service status, logs, stop, and
   uninstall commands.
2. After reboot, the host reconnects and refreshes trust state.
3. Lucky selects it from registry policy, not by hostname alone.
4. Gateway signs a Codex job bound to host, workspace, worktree, TTL, and limits.
5. Host validates envelope, identity, nonce, capabilities, approval tokens, and locks.
6. Codex runs inside the locked worktree and returns diff, tests, logs, and residual risk.
7. Push, merge, deploy, credentials, service mutation, and publish pause for approval.

### Public Skillkit Install

1. User installs `rdev` and exports a Skillkit bundle.
2. Agent runtime installs Skills and MCP tool definitions.
3. Gateway is self-hosted or configured as an authenticated service.
4. Host binaries are verified before temporary or managed enrollment.
5. Acceptance transcripts prove safety behavior matches public docs.

## Storage And Reliability

| Data | First production | Later scale target |
|---|---|---|
| Gateway state | SQLite with backups | Postgres-compatible schema |
| Artifacts | local filesystem with quotas | S3-compatible object storage |
| Audit | append-only JSONL plus hash-chain verifier | append-only store with export and retention policy |
| Gateway signing keys | locked file or OS store | KMS/HSM option |
| Host identity | file-backed development store | Keychain, DPAPI, libsecret, TPM-backed storage where available |
| Host trust | file-backed bundle | signed updates, rollback protection, revocation |
| Approval tokens | gateway state plus host consumption store | single-use, scoped, expiring, auditable store |

Reliability rules:

- job create, claim, complete, artifact upload, cancel, and revoke are idempotent;
- leases are bounded and recoverable;
- managed reconnect flushes local audit and evidence;
- temporary sessions stop at TTL, local stop, or revoke and never silently resurrect;
- terminal job state and evidence are separate;
- cancellation may append cancellation evidence without overwriting a canceled terminal state.

## Release And Bootstrap

Release trust is separate from runtime job trust.

Required release flow:

1. Build reproducible artifacts where practical.
2. Generate manifest with artifact digest, size, platform, signer, validity, and rollback metadata.
3. Sign manifest and artifacts.
4. Verify in bootstrap before running host code.
5. Use Authenticode for public Windows releases when policy requires it.
6. Use macOS signing/notarization for public macOS releases.
7. Publish security advisory, key rotation, and emergency revocation procedures.

Bootstrap rules:

- scripts must be inspectable;
- temporary bootstrap starts foreground by default;
- managed install is a separate explicit command;
- no weakening UAC, sudo, TCC, Gatekeeper, Defender, firewall, enterprise policy,
  or persistent PowerShell execution policy;
- process-scoped execution-policy choices are acceptable only when visible and
  necessary for the current bootstrap session.

## Buy Versus Build Boundary

| Area | Decision |
|---|---|
| MCP | use as the agent tool protocol; do not invent a parallel agent tool standard |
| OAuth/TLS | use established web auth and HTTPS for hosted MCP/API |
| Tailscale/headscale | optional owned-host transport and diagnostics, not job authorization |
| SSH | optional adapter for owned infrastructure, not temporary-host default |
| Coder/DevPod/devcontainers | wrap as workspace-provider adapters |
| RustDesk/MeshCentral/native screen share | wrap as explicit GUI adapters with approval and audit |
| Sigstore/platform signing | align release verification with mature signing and checksum workflows |
| Remote desktop platform | do not build one in core |
| Mesh network | do not build one in core |
| Hosted IDE | do not build one in core |

## What Not To Build

- hidden remote administration;
- public inbound listeners on temporary hosts;
- raw unrestricted shell as the default agent primitive;
- automatic privilege escalation or OS policy weakening;
- agent self-approval for high-risk operations;
- silent persistence on third-party machines;
- GUI control without approval, local visibility, and audit;
- direct host credential sharing with agent runtimes;
- broad fleet-management features before the safety kernel is stable.

## v1.0 Closure Gates

`v1.0` is achieved when the safety kernel, protocol contracts, install story, and
acceptance evidence are stable.

Required gates:

1. A clean Windows 10/11 host joins from one visible verified command, connects
   outbound only, runs bounded repair, enforces approvals, revokes cleanly, and
   leaves no service or autorun persistence.
2. Eitan's managed Mac reconnects after reboot, receives a Lucky-requested Codex
   job, locks a worktree, returns diff/test/cancellation evidence, and requires
   approval before push, merge, deploy, credentials, or service changes.
3. Tampered, expired, wrong-host, wrong-key, replayed, non-allowlisted,
   missing-capability, workspace-escaping, and unsigned-release flows are rejected
   with structured artifacts.
4. Evidence bundles and hash-chained audit exports let another human or agent
   reconstruct what happened.
5. Built-in adapters pass conformance tests and cannot bypass the safety kernel.
6. Skillkit export installs cleanly into Hermes, Codex, Claude Code, OpenCode, and
   generic MCP environments without Hermes-specific assumptions.
7. Threat model, release key lifecycle, security policy, public docs, and acceptance
   transcripts match the shipped behavior.

## Immediate Engineering Closure

The next implementation nodes should be completed in this order:

1. Real managed Mac LaunchAgent acceptance: install, start, reconnect after reboot,
   run Codex in locked worktree, export evidence, uninstall.
2. Windows temporary bootstrap acceptance: verified release, foreground UI/console,
   outbound-only host loop, no-persistence inspection, approval gate transcript.
3. Production trust lifecycle: authenticated trust-bundle rotation, host trust
   refresh, revocation propagation, OS-protected host identity.
4. WSS host channel: keep HTTPS long-poll fallback, add authenticated streaming
   transport and reconnect semantics.
5. Adapter SDK: extract the current shell and Codex safety wrapper into a reusable
   interface with conformance fixtures.
6. Public release hardening: signed artifacts, checksums, platform signing, release
   verification docs, security policy, and acceptance transcript publication.

## Final Sentence

The perfect ending is reached when an operator can say:

```text
Lucky, use that approved machine to solve this.
```

And the system responds with bounded execution, local verification, approval
gates, evidence, audit, and revocation instead of trust-me automation.

## External Anchors

The design aligns with these public platform contracts:

- MCP tools should expose clear tool surfaces, structured results, and
  human-in-the-loop controls for tool invocation:
  https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- Hosted MCP authorization should use OAuth-style protected resource metadata,
  resource-specific tokens, token validation, and HTTPS transport:
  https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
- Tailscale auth keys can be one-off, expiring, tagged, ephemeral, pre-approved,
  and revoked, which makes them useful as optional transport enrollment primitives
  for owned hosts:
  https://tailscale.com/docs/features/access-control/auth-keys
- Windows PowerShell execution policy has explicit scopes and Group Policy
  precedence; bootstrap must not weaken persistent or enterprise policy:
  https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_execution_policies
- macOS launchd jobs use visible plist configuration and OS-managed service
  behavior, which fits explicit managed mode:
  https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html
- Sigstore/Cosign-style verification supports signed blob and artifact verification
  with bundles, keys, and certificate identity checks:
  https://docs.sigstore.dev/cosign/verifying/verify/
