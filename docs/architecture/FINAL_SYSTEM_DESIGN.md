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

### Gate 1: Complete the Local Safety Kernel

- durable shell result schema;
- host-side redaction;
- hash-chained audit;
- stronger workspace escape tests;
- nonce replay tests;
- policy explanation for every denial/approval.

### Gate 2: Build the Temporary Windows MVP

- release-key lifecycle policy;
- Authenticode/notarization policy;
- signed Windows bootstrap;
- outbound HTTPS polling then WSS;
- foreground console/UI with stop;
- local audit spool;
- E2E temporary repair acceptance test.

### Gate 3: Build Managed Mac Coding

- host identity protected storage;
- LaunchAgent managed mode;
- workspace locks and worktrees;
- Codex adapter;
- git diff/test evidence;
- approval before push/merge.

### Gate 4: Generalize To Multi-Host

- durable gateway storage;
- key rotation and trust-bundle update;
- Linux systemd;
- Windows Service;
- Claude Code and ACP adapters;
- artifact streaming;
- mesh transport adapter.

### Gate 5: Public v1.0 Skillkit

- stable MCP schemas;
- stable protocol versions;
- installable Agent Skills;
- signed multi-platform releases;
- security review;
- deployment guide;
- acceptance demos;
- disclosure policy.

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

