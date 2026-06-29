# GitHub Project Management

This document defines the GitHub operating model for Remote Dev Skillkit. It is intentionally concrete so the repository can move from local implementation loops to public, trackable engineering work.

## Current State

As of 2026-06-29:

- local repository path: `/Users/eitan/Documents/Codex/2026-06-28/pe/work/remote-dev-skillkit`
- current branch: `main`
- latest local commit: `05f9c12 feat: export local evidence bundles`
- GitHub CLI account: `EitanWong`
- local git remote: none configured
- matching remote repository under `EitanWong`: not found by read-only `gh repo list`

No GitHub repository, issues, labels, milestones, or project board should be created until Eitan explicitly approves that external change.

Recommended remote repository:

```text
EitanWong/remote-dev-skillkit
```

Recommended visibility while the safety model is still pre-v1:

```text
private
```

## Milestones

| Milestone | Purpose | Exit Gate |
|---|---|---|
| `v0.1 Local Safety Kernel` | prove signed local job execution, policy checks, evidence, audit, and review loop | local demo plus tests pass for signed envelope, host identity, nonce replay, workspace/symlink rejection, evidence bundle, audit chain |
| `v0.2 Temporary Windows MVP` | one-command attended Windows repair session | clean Windows VM joins visibly, verifies signed artifacts, connects outbound only, leaves no persistence |
| `v0.3 Managed Mac Coding` | Eitan-owned managed Mac runs Codex jobs safely | LaunchAgent/managed test mode, workspace lock/worktree, Codex adapter, diff/test evidence, approval before push |
| `v0.4 Multi-Host Runtime` | generalize to Mac/Windows/Linux managed hosts and adapters | durable gateway storage, service modes, WSS, trust rotation, artifact streaming, adapter SDK |
| `v1.0 Public Skillkit` | stable open-source release | stable MCP schemas, install docs, signed releases, threat model, acceptance demos, security policy |

## Labels

| Label | Color | Description |
|---|---|---|
| `area:gateway` | `1f77b4` | gateway state, APIs, queues, leases, storage |
| `area:host` | `2ca02c` | host runtime, identity, trust, execution loop |
| `area:policy` | `d62728` | policy decisions, approval gates, denial explanations |
| `area:evidence` | `9467bd` | artifacts, evidence bundles, audit export, redaction |
| `area:transport` | `17becf` | HTTPS polling, WSS, mTLS, reconnect |
| `area:adapter` | `ff7f0e` | shell, PowerShell, Git, Codex, Claude Code, ACP, GUI, mesh |
| `area:bootstrap` | `8c564b` | join manifest, release verification, Windows/macOS/Linux bootstrap |
| `area:docs` | `7f7f7f` | architecture, operations, release docs, runbooks |
| `kind:feature` | `0e8a16` | new product capability |
| `kind:security` | `b60205` | security boundary, exploit prevention, trust model |
| `kind:test` | `5319e7` | automated or real-environment acceptance coverage |
| `kind:ops` | `0052cc` | deployment, service management, release operations |
| `priority:p0` | `b60205` | blocks safe use or release |
| `priority:p1` | `fbca04` | required for current milestone |
| `priority:p2` | `c5def5` | useful but not blocking current milestone |

## Initial Issues

### v0.1 Local Safety Kernel

1. **Add host-side denial policy explanations**
   - Labels: `area:policy`, `area:host`, `kind:security`, `priority:p1`
   - Acceptance:
     - every hostrunner denial returns a structured explanation code and human summary;
     - missing workspace, missing capability, unsupported adapter, non-allowlisted command, workspace escape, identity mismatch, expired/tampered/replayed envelopes are covered;
     - CLI/HTTP failure reporting includes the explanation in artifacts or failure payload.

2. **Export evidence bundles directly from gateway job ids**
   - Labels: `area:evidence`, `area:gateway`, `kind:feature`, `priority:p1`
   - Acceptance:
     - dev gateway can export a job evidence bundle without manually assembling JSON input files;
     - bundle includes job, envelope, artifacts, audit slice, chain, checksums, manifest;
     - tests cover succeeded and failed jobs.

3. **Add local demo evidence bundle command**
   - Labels: `area:evidence`, `area:docs`, `kind:test`, `priority:p2`
   - Acceptance:
     - README quick start can produce a demo bundle end to end;
     - bundle can be inspected and audit chain verifies.

4. **Complete v0.1 release checklist**
   - Labels: `area:docs`, `kind:ops`, `priority:p1`
   - Acceptance:
     - `./scripts/check.sh` passes;
     - all v0.1 Definition of Done bullets are backed by command evidence;
     - CHANGELOG entry and tag plan are ready.

### v0.2 Temporary Windows MVP

5. **Implement outbound host transport prototype**
   - Labels: `area:transport`, `area:host`, `area:gateway`, `kind:feature`, `priority:p1`
   - Acceptance:
     - host can claim/complete jobs over HTTPS polling without local-only restrictions;
     - reconnect/backoff behavior is tested;
     - cancellation/revocation checked between claims and during long-running jobs.

6. **Build Windows foreground temporary host UX**
   - Labels: `area:host`, `area:bootstrap`, `kind:feature`, `priority:p1`
   - Acceptance:
     - Windows host runs visibly in foreground;
     - shows operator, reason, TTL, gateway, stop instructions;
     - no service/scheduled-task/Run-key persistence in temporary mode.

7. **Harden Windows bootstrap verification**
   - Labels: `area:bootstrap`, `kind:security`, `priority:p1`
   - Acceptance:
     - bootstrap verifies signed manifest and host binary before execution;
     - Authenticode policy path is documented and testable;
     - bootstrap does not weaken execution policy, Defender, UAC, firewall, or Group Policy.

### v0.3 Managed Mac Coding

8. **Add workspace lock manager**
   - Labels: `area:host`, `area:policy`, `kind:feature`, `priority:p1`
   - Acceptance:
     - one writer per repo root unless isolated worktree is created;
     - stale locks expire or release through cancel;
     - lock acquire/release are audited.

9. **Implement Git worktree evidence flow**
   - Labels: `area:adapter`, `area:evidence`, `kind:feature`, `priority:p1`
   - Acceptance:
     - managed coding job creates branch/worktree;
     - captures status, diff stat, full diff artifact, verification commands;
     - push/merge/tag requires approval.

10. **Implement Codex adapter MVP**
    - Labels: `area:adapter`, `area:host`, `kind:feature`, `priority:p1`
    - Acceptance:
      - runs Codex CLI inside locked workspace;
      - streams bounded output;
      - returns adapter result, diff/test evidence, residual risk.

## Bootstrap Script

The repository includes `scripts/github/bootstrap-project.sh` as a proposed automation for labels, milestones, and initial issues. Run it only after Eitan approves external GitHub changes.

Recommended sequence after approval:

```bash
gh repo create EitanWong/remote-dev-skillkit --private --source . --remote origin --push
scripts/github/bootstrap-project.sh EitanWong/remote-dev-skillkit
```

If the repository already exists:

```bash
git remote add origin https://github.com/EitanWong/remote-dev-skillkit.git
git push -u origin main
scripts/github/bootstrap-project.sh EitanWong/remote-dev-skillkit
```
