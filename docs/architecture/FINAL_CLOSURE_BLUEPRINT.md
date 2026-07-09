# Final Closure Blueprint

This is the concise closure architecture for Remote Dev Skillkit. The canonical
final architecture lock is [Perfect Ending Solution](PERFECT_ENDING_SOLUTION.md).
This file is the short release-facing summary of that contract. The broader
[Final System Design](FINAL_SYSTEM_DESIGN.md) and
[Ultimate Closure Design](ULTIMATE_CLOSURE_DESIGN.md) remain supporting rationale
and implementation detail.

## North Star

Remote Dev Skillkit is an agent work fabric for real machines.

It is not a remote desktop clone, a hidden support agent, an SSH wrapper, or a raw
terminal MCP server. Its task is to let agents cause useful work only through visible
consent, typed intent, host-side verification, scoped execution, evidence, audit, and
revocation.

The final product promise is:

```text
maximum useful delegation after explicit consent;
minimum ambient authority by default.
```

## Closure Formula

Every supported workflow must reduce to the same loop:

```text
agent intent
  -> typed Skill/MCP/API request
  -> gateway policy dry-run
  -> signed host-bound envelope
  -> outbound host lease
  -> host-side validation
  -> locked workspace or visible session
  -> adapter execution
  -> redacted artifacts
  -> session evidence
  -> hash-chained audit
  -> review, authorization, continuation, cancellation, or revocation
```

If a feature cannot fit this loop, it is an integration idea rather than a core
product feature.

## Product Constitution

The system is correct only when all of these statements remain true:

1. Agents request typed work; they do not receive ambient host ownership.
2. The gateway signs only bounded, host-specific, expiring session tasks.
3. The host independently verifies identity, signature, expiry, nonce, policy,
   capabilities, workspace scope, authorizations, and locks before execution.
4. Adapters execute work but never own authorization, persistence, authorization, or trust.
5. Dangerous operations require scoped authorization from the operator or local user.
6. Completion claims require artifacts and audit; natural-language confidence is not proof.
7. Temporary hosts are visible, foreground, TTL-bound, outbound-only, and non-persistent.
8. Managed hosts are explicitly installed, inspectable, stoppable, and uninstallable.
9. Release bootstrap verifies signed manifests and binaries before running host code.
10. Tickets, hosts, tasks, authorizations, keys, and sessions can be revoked and audited.

## Final Planes

| Plane | Owns | Must not own |
|---|---|---|
| Agent interface | Skills, MCP tools, typed requests, policy explanations, evidence review | host credentials, authorization authority, raw default shells |
| Gateway governance | tickets, hosts, leases, policy, signing, authorizations, artifacts, audit, revocation | local host execution, adapter trust roots, release signing by itself |
| Host sovereignty | local identity, trust bundle, nonce and authorization stores, local policy, locks, adapter runner, stop control | broader authority than the gateway and local policy grant |
| Adapter execution | shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, mesh, Coder, DevPod work | authorization, persistence, authorization, identity, release trust |
| Review and release | session evidence, audit export, release manifests, checksums, signatures, rollback | runtime task authorization |

This authority split is the project. If a transport, GUI tool, coding CLI, or agent
runtime becomes the security root, the architecture has regressed.

## Operating Modes

| Mode | Use case | Persistence | Default authority | Hard rule |
|---|---|---:|---|---|
| `attended-temporary` | third-party support and short-lived repair | none | foreground, TTL-bound, scoped | no service, autorun, hidden restart, or silent resurrection |
| `managed` | operator-owned or formally managed machines | explicit service | durable reconnect with authorized roots | reliability never implies authorization for external consequences |
| `break-glass` | urgent recovery | short-lived | narrow emergency actions | shorter TTL, stronger authorization, denser audit |
| `workspace-provider` | Coder, DevPod, devcontainers, disposable cloud workspaces | provider-owned | bounded workspace lifecycle | provider identity never replaces rdev authorization |

Temporary mode never upgrades itself into managed mode. Managed mode never inherits
authorization for push, merge, deploy, publish, paid actions, credential changes, GUI
control, service mutation, or elevation.

## Golden Reference Deployment

an operator's deployment is the first production shape:

```text
Hermes
  -> Remote Dev Skillkit skills
  -> MCP HTTP or local bridge
  -> https://api.example.com/v1
  -> rdev-gateway
  -> tickets, hosts, tasks, authorizations, artifacts, audit, signing
  -> https://agent.example.com
  -> join page, signed manifests, release downloads, outbound host relay
  -> managed operator hosts and attended temporary hosts
```

`api.example.com` is the authenticated agent/operator API surface. `agent.example.com`
is the human join, bootstrap, release-download, and host-relay surface. They can start
in one binary, but they must stay separate in responsibility.

## Public Open-Source Shape

The public release ships five installable deliverables:

| Deliverable | Purpose |
|---|---|
| `rdev` CLI | local demo, diagnostics, operator workflows, service lifecycle, release and evidence verification |
| `rdev-gateway` | self-hosted control plane and MCP/API server |
| `rdev-host` | cross-platform attended temporary and managed runtime |
| Skillkit bundle | portable Skills and MCP contracts for Hermes, Codex, Claude Code, OpenCode, and generic agents |
| Adapter SDK and conformance suite | safe extension point for new execution backends |

Hermes is the reference environment, not a required dependency.

## Protocol Closure

The stable protocol family should include schema-versioned objects for:

- tickets and join manifests;
- host registration, host identity, capability inventory, and host policy;
- trust bundles, trust updates, signing keys, and revocations;
- tasks, signed envelopes, leases, nonces, and cancellations;
- session interrupt requests, session interrupts, and host-side interrupt acknowledgement;
- denials, session-interrupt-required pauses, adapter results, and session evidence;
- artifacts, redaction metadata, audit events, and audit-chain exports;
- release manifests, artifact signatures, checksums, and rollback metadata.

Every protocol object needs a clear owner, expiry rule, replay story, redaction story,
and audit event.

## Reliability Closure

Remote work should be retry-safe and reconstructable:

- task creation, claim, completion, cancellation, and artifact upload are idempotent;
- leases have bounded lifetime and explicit recovery behavior;
- host reconnect can flush local audit/evidence in managed mode;
- temporary hosts stop at TTL, local stop, or revoke and do not silently resurrect;
- workspace locks enforce one writer unless isolated worktrees are used;
- cancellation is cooperative when possible and still produces artifacts when the host
  collected evidence before stopping;
- terminal state and evidence are separate: a canceled task remains `canceled`, but the
  host may append a cancellation artifact for review.

## Adapter Closure

Every adapter must implement the same safety wrapper:

```text
detect(context) -> capabilities
plan(task, host_policy) -> required_capabilities, authorizations, workspace_or_session_plan
prepare(task, locks, limits) -> prepared_workspace_or_session
run(task, cancellation, limits) -> events, raw_result
collect(task, raw_result) -> redacted_artifacts, checksums, evidence_manifest
cleanup(task, result) -> cleanup_status
```

Conformance tests must prove deny-by-default capability mapping, workspace and symlink
escape rejection, session interrupt pauses before side effects, cancellation hooks, redaction,
output caps, evidence on failure, and visible cleanup.

## Security Closure

The project should align with existing public security primitives instead of inventing
all trust infrastructure:

- MCP tools remain structured model-invoked external actions with schema validation,
  access control, output sanitization, human confirmation for sensitive operations,
  rate limits, and audit.
- HTTP MCP deployments use HTTPS and short-lived authorization tokens.
- Mesh tools such as Tailscale/headscale are optional owned-host transports, not task
  authorization.
- Windows bootstrap respects execution policy, Group Policy, Defender, UAC, firewall,
  and enterprise controls.
- Release trust uses signed manifests, checksums, platform signing when public, and a
  documented key-rotation and advisory process.

## Things Not To Build

These are explicit non-goals for the core:

- hidden remote administration;
- public inbound listeners on temporary hosts;
- raw unrestricted shell as the default agent primitive;
- agent self-authorization for dangerous operations;
- automatic privilege escalation or OS security weakening;
- silent persistence on third-party machines;
- GUI control without local/operator authorization and audit;
- direct host credential sharing with agent runtimes;
- a bespoke mesh network, remote desktop stack, or hosted IDE platform when mature tools
  can be wrapped as adapters.

## v1.0 Closure Gates

`v1.0` is reached when the safety kernel and install story are stable, not when every
adapter exists.

Required gates:

1. A clean Windows 10/11 host joins from one visible verified command, connects outbound
   only, runs bounded repair tasks, enforces authorizations, revokes cleanly, and leaves no
   service or autorun persistence.
2. an operator's managed Mac reconnects after reboot, runs a agent-requested Codex task in a
   locked worktree, returns diff/test/cancellation evidence, and requires authorization before
   push, merge, deploy, credentials, or service changes.
3. Tampered, expired, wrong-host, wrong-key, replayed, non-allowlisted, missing-capability,
   workspace-escaping, and unsigned-release flows are rejected with structured evidence.
4. Session evidence and hash-chained audit exports let another human or agent reconstruct
   what happened.
5. Built-in adapters pass conformance tests and cannot bypass the safety kernel.
6. The Skillkit export installs cleanly into mainstream agent runtimes without
   Hermes-specific assumptions.
7. Public release docs, threat model, security policy, release key lifecycle, and
   acceptance transcripts match shipped behavior.

## Final Sentence

The perfect ending is reached when the operator can say:

> an agent, use that authorized machine to solve this.

And the system responds with bounded execution, local verification, session interrupts,
evidence, audit, and revocation instead of trust-me automation.
