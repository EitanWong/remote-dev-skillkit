# GitHub Project Management

This document defines the GitHub operating model for Remote Dev Skillkit. It is
kept local until Eitan explicitly approves external GitHub mutations.

## Current State

As of 2026-06-29:

- local repository path: `/Users/eitan/Documents/Codex/2026-06-28/pe/work/remote-dev-skillkit`
- current branch: `main`
- latest audited local baseline before this project-management update:
  `f5bedc6 docs: add perfect ending solution`
- GitHub CLI account: `EitanWong`
- local git remote: none configured
- `gh repo view EitanWong/remote-dev-skillkit`: no repository found at audit time

No GitHub repository, issues, labels, milestones, project board, release, or push
should be created until Eitan explicitly approves that external change.

Recommended remote repository:

```text
EitanWong/remote-dev-skillkit
```

Recommended visibility while the safety model is still pre-v1:

```text
private
```

## Project Board Model

Use one GitHub project board with these views:

| View | Purpose |
|---|---|
| Roadmap | group by milestone from v0.2 through v1.0 |
| Current Iteration | only issues actively being implemented or verified |
| Security Gates | `kind:security` plus approval, trust, bootstrap, and persistence work |
| Acceptance Evidence | real-environment transcripts and release-gate evidence |
| Public Launch | docs, installers, signed releases, examples, community readiness |

Recommended fields:

| Field | Values |
|---|---|
| Status | Backlog, Ready, In Progress, In Review, Blocked, Done |
| Priority | P0, P1, P2 |
| Milestone | v0.2, v0.3, v0.4, v1.0 |
| Surface | gateway, host, transport, adapter, skillkit, release, docs |
| Evidence | missing, partial, automated, real-environment, release-ready |

## Milestones

| Milestone | Purpose | Exit Gate |
|---|---|---|
| `v0.1 Local Safety Kernel` | signed local job execution, host-side verification, approvals, evidence, audit, Skillkit export | substantially implemented locally; keep open only for release-note/evidence cleanup |
| `v0.2 Temporary Windows Host` | one visible verified Windows command starts an attended outbound host | clean Windows VM joins, verifies signed artifacts, enforces approvals, revokes, leaves no persistence |
| `v0.3 Managed Mac Coding` | Eitan-owned managed Mac runs Codex jobs safely | service-backed LaunchAgent transcript proves reconnect, locked worktree, diff/test evidence, approval gates |
| `v0.4 Managed Device Generalization` | generalize managed runtime across Mac, Windows, Linux | Windows Service, systemd, WSS/mTLS, OS-protected storage, adapter SDK |
| `v1.0 Public Skillkit` | stable open-source release for mainstream agent frameworks | signed releases, stable schemas, install docs, threat model, security policy, public acceptance transcripts |

## Labels

| Label | Color | Description |
|---|---|---|
| `area:gateway` | `1f77b4` | gateway state, APIs, queues, leases, storage |
| `area:host` | `2ca02c` | host runtime, identity, trust, execution loop |
| `area:policy` | `d62728` | policy decisions, approval gates, denial explanations |
| `area:evidence` | `9467bd` | artifacts, evidence bundles, audit export, redaction |
| `area:transport` | `17becf` | HTTPS polling, WSS, mTLS, reconnect |
| `area:adapter` | `ff7f0e` | shell, PowerShell, Git, Codex, Claude Code, ACP, GUI, mesh |
| `area:bootstrap` | `8c564b` | join manifest, release verification, platform bootstrap |
| `area:service` | `c27ba0` | launchd, Windows Service, systemd, lifecycle control |
| `area:skillkit` | `7057ff` | agent skills, MCP contracts, install bundles |
| `area:release` | `5319e7` | signed artifacts, changelog, release evidence, distribution |
| `area:docs` | `7f7f7f` | architecture, operations, release docs, runbooks |
| `kind:feature` | `0e8a16` | new product capability |
| `kind:security` | `b60205` | security boundary, exploit prevention, trust model |
| `kind:test` | `5319e7` | automated or real-environment acceptance coverage |
| `kind:ops` | `0052cc` | deployment, service management, release operations |
| `kind:docs` | `6f42c1` | documentation, examples, runbooks, public packaging |
| `priority:p0` | `b60205` | blocks safe use or release |
| `priority:p1` | `fbca04` | required for current milestone |
| `priority:p2` | `c5def5` | useful but not blocking current milestone |

## Completed Local Capabilities

These should not be opened as new GitHub issues unless regression work is needed:

- host-side structured denials and approval-required artifacts;
- gateway-backed evidence bundle export;
- hash-chained audit export and verification;
- portable Skillkit bundle export and verification;
- local release candidate packaging with signed artifacts, release bundle, verified Skillkit, checksums, and summary JSON;
- local GitHub Release dry-run planning from verified candidates with generated release notes, Skillkit archive, asset manifest, and commands preview;
- HTTPS long-poll host job transport prototype;
- host trust-bundle update checks;
- macOS LaunchAgent plist generation, status, safe uninstall, and service-control;
- workspace lock manager and Git worktree preparation foundation;
- Codex adapter MVP with diff/test evidence, redaction, truncation, timeout, and cancellation evidence;
- shared risky-action approval preflight for shell and Codex;
- managed Mac local acceptance harness and independent evidence verifier;
- managed Mac LaunchAgent service acceptance plan.

## Seed Issues To Create After Approval

The current bootstrap script creates the following seed backlog.

### v0.3 Managed Mac Coding

1. **Run service-backed managed Mac acceptance transcript**
   - Labels: `area:service`, `area:host`, `area:evidence`, `kind:test`, `priority:p1`
   - Acceptance:
     - generate and review a managed Mac LaunchAgent plan;
     - start and inspect with `rdev host service-control --execute`;
     - confirm reconnect after login or reboot;
     - run service-backed managed Mac Codex acceptance;
     - verify evidence with `rdev acceptance verify`;
     - stop and uninstall without touching unrelated plists.

### v0.2 Temporary Windows Host

2. **Build Windows foreground temporary host no-persistence acceptance**
   - Labels: `area:bootstrap`, `area:host`, `area:evidence`, `kind:test`, `priority:p1`
   - Acceptance:
     - clean Windows 10/11 VM joins from one visible command;
     - bootstrap verifies pinned verifier and signed host artifact before execution;
     - host is foreground, outbound-only, revocable, and approval-gated;
     - no service, scheduled task, Run key, startup shortcut, or firewall rule remains.

### v0.4 Managed Device Generalization

3. **Add production WSS host channel with authenticated fallback**
   - Labels: `area:transport`, `area:gateway`, `area:host`, `kind:feature`, `kind:security`, `priority:p1`
   - Acceptance:
     - authenticated WSS supports interactive job status and artifact events;
     - HTTPS long-poll remains a stable fallback;
     - reconnect, lease expiry, cancellation, and revocation are tested.

4. **Add OS-protected managed host identity and trust storage**
   - Labels: `area:host`, `area:policy`, `kind:security`, `priority:p1`
   - Acceptance:
     - Keychain, DPAPI, and libsecret or documented fallback are supported;
     - file-backed dev stores remain available for tests;
     - rollback and revocation checks still pass.

5. **Extract adapter SDK and conformance suite**
   - Labels: `area:adapter`, `area:policy`, `area:evidence`, `kind:feature`, `kind:test`, `priority:p1`
   - Acceptance:
     - shared adapter interface covers detect, plan, prepare, run, collect, cleanup;
     - shell and Codex pass shared conformance fixtures;
     - new adapter authors can run local conformance tests.

6. **Implement Claude Code and ACP adapters behind the safety kernel**
   - Labels: `area:adapter`, `area:skillkit`, `kind:feature`, `priority:p2`
   - Acceptance:
     - adapters run only after signed envelope, host validation, workspace lock, and approval preflight;
     - diff/test evidence matches Codex expectations where possible;
     - dangerous external consequences pause before execution.

7. **Add Windows Service and systemd managed host lifecycle**
   - Labels: `area:service`, `area:host`, `kind:feature`, `kind:ops`, `priority:p1`
   - Acceptance:
     - `rdev host install-service` supports Windows Service and systemd;
     - install, status, service-control, stop, and uninstall are inspectable;
     - temporary mode cannot install persistence through these commands.

### v1.0 Public Skillkit

8. **Package public Skillkit install paths for Codex Claude Code OpenCode Hermes and generic MCP**
   - Labels: `area:skillkit`, `area:docs`, `kind:docs`, `priority:p1`
   - Acceptance:
     - one install path each for Codex, Claude Code, OpenCode/OpenClaw, Hermes, and generic MCP;
     - exported bundle includes manifest checksums and framework notes;
     - a self-host user can create a ticket, run a job, and export evidence without Hermes-specific assumptions.

9. **Prepare signed release pipeline and public acceptance transcript package**
   - Labels: `area:release`, `area:evidence`, `kind:ops`, `kind:security`, `priority:p1`
   - Acceptance:
     - staged artifacts exist for macOS, Linux, and Windows;
     - `rdev release prepare-candidate` produces `rdev.release-candidate.v1`;
     - `rdev release verify-candidate` returns `ok=true`;
     - `scripts/github/plan-release.sh` produces `rdev.github-release-plan.v1` without external mutation;
     - checksums, signed artifact manifests, and signed release index verify;
     - redacted acceptance transcripts, report JSON, audit verification, and evidence checksums are packaged.

## Bootstrap Script

Preview the GitHub changes locally:

```bash
scripts/github/bootstrap-project.sh --dry-run EitanWong/remote-dev-skillkit
```

Preview GitHub Release publication from a verified local candidate:

```bash
scripts/github/plan-release.sh \
  --candidate dist/release-candidate \
  --repo EitanWong/remote-dev-skillkit \
  --require-artifacts rdev-host.exe,rdev-verify.exe
```

This generates a local plan, release notes, verification JSON, and command
preview. It does not create a release or upload assets.

Recommended sequence after explicit approval:

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

After public launch, switch visibility deliberately:

```bash
gh repo edit EitanWong/remote-dev-skillkit --visibility public
```

Do this only after the security policy, release signing, acceptance transcripts,
and install docs match the shipped behavior.
