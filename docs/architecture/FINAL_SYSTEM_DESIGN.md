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

Useful references:

- MCP Tools: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- PowerShell execution policies: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_execution_policies
- Get-AuthenticodeSignature: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.security/get-authenticodesignature
- Tailscale auth keys: https://tailscale.com/docs/features/access-control/auth-keys
- Tailscale ephemeral nodes: https://tailscale.com/docs/features/ephemeral-nodes
- systemd service units: https://www.freedesktop.org/software/systemd/man/systemd.service.html
- Apple launchd jobs: https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html
- OpenSSH sshd_config: https://man.openbsd.org/sshd_config

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

Still required:

- host identity storage wired into registration and job binding;
- nonce replay cache;
- hash-chained audit export verifier;
- stronger workspace and symlink escape tests;
- policy explanation for every denial/approval;
- local evidence bundle export.

### Gate 2: Build the Temporary Windows MVP (`v0.2`)

- release-key lifecycle policy;
- Authenticode/notarization policy;
- signed Windows bootstrap;
- outbound HTTPS polling then WSS;
- foreground console/UI with stop;
- local audit spool;
- E2E temporary repair acceptance test;
- no-persistence inspection script;
- local-user approval prompt for elevation/GUI/service requests.

### Gate 3: Build Managed Mac Coding (`v0.3`)

- host identity protected storage;
- LaunchAgent managed mode;
- workspace locks and worktrees;
- Codex adapter;
- git diff/test evidence;
- approval before push/merge;
- managed host health and uninstall command;
- artifact bundle review flow.

### Gate 4: Generalize To Multi-Host (`v0.4`)

- durable gateway storage;
- key rotation and trust-bundle update;
- Linux systemd;
- Windows Service;
- Claude Code and ACP adapters;
- artifact streaming;
- mesh transport adapter.

### Gate 5: Public v1.0 Skillkit (`v1.0`)

- stable MCP schemas;
- stable protocol versions;
- installable Agent Skills;
- signed multi-platform releases;
- security review;
- deployment guide;
- acceptance demos;
- disclosure policy.

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
