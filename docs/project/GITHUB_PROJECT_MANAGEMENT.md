# GitHub Project Management

This document defines the GitHub operating model for Remote Dev Skillkit. It is
kept local until an operator explicitly approves external GitHub mutations.

## Current State

This repository is prepared for a public GitHub launch, but local evidence must
be regenerated before each external mutation:

- local repository path: `REPO_ROOT`
- expected branch: `main`
- recommended repository format: `OWNER/remote-dev-skillkit`
- GitHub CLI account: `OWNER`

No GitHub repository, issues, labels, milestones, project board, release, or push
should be created until an operator explicitly approves that external change.

Local project readiness is machine-checkable:

```bash
scripts/github/audit-project-readiness.sh \
  --repo OWNER/remote-dev-skillkit \
  --out dist/github-project-readiness.json
```

The report uses schema `rdev.github-project-readiness.v1`, runs
`scripts/github/bootstrap-project.sh --dry-run`, checks the local docs,
templates, CI workflow, and GitHub/release planning scripts, and preserves
`external_mutation=false`.

The audit also checks public-project hygiene: `CONTRIBUTING.md`,
`CODE_OF_CONDUCT.md`, `docs/project/PROJECT_STRUCTURE.md`, multilingual quick
starts under `docs/i18n/`, and README links for Codex, Claude Code, Hermes,
OpenClaw/OpenCode, Apache-2.0, project structure, and multilingual entry points.

Recommended remote repository:

```text
OWNER/remote-dev-skillkit
```

Recommended visibility for the current open-source launch target:

```text
public
```

Real Windows, Linux, and Mac acceptance evidence remains required before making
platform-production claims. The project operator should collect that target-machine evidence
manually; public launch readiness should still keep the acceptance-plan and
evidence-package tooling visible so external contributors can reproduce the
checks.

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
| `v0.3 Managed Mac Coding` | operator-owned managed Mac runs Codex jobs safely | service-backed LaunchAgent transcript proves reconnect, locked worktree, diff/test evidence, approval gates |
| `v0.4 Managed Device Generalization` | generalize managed runtime across Mac, Windows, Linux | Windows Service, systemd, WSS/mTLS, OS-protected storage, adapter SDK |
| `v1.0 Public Skillkit` | stable open-source release for mainstream agent frameworks | signed releases, stable schemas, install docs, threat model, security policy, public acceptance transcripts |

## Local Release Evidence Before External GitHub Mutation

Before creating or updating GitHub releases, archive local evidence for the
agent-framework install path:

1. `rdev skillkit export --source-root . --out <empty-skillkit-dir> --gateway-url <url>`
2. `rdev skillkit verify --bundle <skillkit-dir>`
3. `rdev skillkit plan-install --bundle <skillkit-dir> --out <empty-install-plan-dir>`
4. `rdev skillkit verify-install-plan --plan <install-plan-dir>/install-plan.json`
5. `rdev skillkit install --bundle <skillkit-dir> --framework <framework> --target <temporary-dir>`
6. `rdev skillkit install --bundle <skillkit-dir> --framework <framework> --target <temporary-dir> --execute`

The install plan evidence must include `rdev.skillkit-install-plan.v1`,
`rdev.skillkit-install-plan-verification.v1`, `INSTALL_COMMANDS.md`, all
generated framework scripts, and the verification output proving
`external_mutation=false`. Direct installer evidence must include
`rdev.skillkit-install-report.v1` dry-run and execute reports proving dry-run
has `local_mutation=false`, execute has `local_mutation=true`, and both keep
`external_mutation=false`. This is a release gate for the `area:skillkit` and
`area:release` surfaces.

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
- real build artifact generation with `rdev.build-artifacts.v1` and checksums;
- per-platform release candidate automation with `rdev.platform-release-candidates.v1`;
- multi-platform GitHub Release dry-run planning with platform archives, release index, install guide, verification summary, and command previews;
- GitHub Actions CI for `./scripts/check.sh` and real build artifact / per-platform release candidate / multi-platform GitHub release-plan smoke;
- HTTPS long-poll host job transport prototype;
- host trust-bundle update checks;
- macOS LaunchAgent plist generation, status, safe uninstall, and service-control;
- workspace lock manager and Git worktree preparation foundation;
- Codex, Claude Code, and ACP/acpx adapter MVPs with diff/test evidence, redaction, truncation, timeout, and cancellation evidence;
- shared risky-action approval preflight for shell, Codex, Claude Code, and acpx;
- managed Mac local acceptance harness and independent evidence verifier;
- managed Mac LaunchAgent service acceptance plan;
- public `pkg/adapterkit`, `adapterkit.RunLifecycle`, `rdev adapter scaffold`, `rdev adapter verify-result`, `rdev adapter verify-lifecycle`, `rdev adapter verify-cancellation`, `rdev adapter verify-runtime`, `rdev.adapter.verify_result`, `rdev.adapter.verify_lifecycle`, `rdev.adapter.verify_cancellation`, and `rdev.adapter.verify_runtime` onboarding/conformance flow for lifecycle manifests, runtime fixtures, result artifacts, and cancellation artifacts; shell, PowerShell, Codex, Claude Code, and acpx tests use the result and cancellation checks, and hostrunner can append built-in adapter runtime fixtures with `--capture-runtime-fixture`.

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
     - package release evidence with `rdev acceptance package-managed-mac-service`;
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
     - Keychain, DPAPI, libsecret, and keyctl or documented fallback are supported;
     - file-backed dev stores remain available for tests;
     - rollback and revocation checks still pass.

5. **Extract full runtime adapter SDK and conformance suite**
   - Labels: `area:adapter`, `area:policy`, `area:evidence`, `kind:feature`, `kind:test`, `priority:p1`
   - Acceptance:
     - shared adapter interface covers detect, plan, prepare, run, collect, cleanup;
     - shell, PowerShell, Codex, Claude Code, and acpx keep passing shared lifecycle, runtime fixture, result, cancellation artifact, and runtime cancellation conformance fixtures;
     - future adapters use the same hostrunner runtime fixture capture path instead of bespoke evidence formats;
     - new adapter authors can run local conformance tests.

6. **Harden ACP/acpx adapter behind the safety kernel**
   - Labels: `area:adapter`, `area:skillkit`, `kind:feature`, `priority:p2`
   - Acceptance:
     - adapter runs only after signed envelope, host validation, workspace lock, and approval preflight;
     - real upstream acpx CLI smoke covers default `acpx --cwd <workspace> codex exec <prompt>` behavior or documents a required payload override while acpx remains alpha;
     - diff/test evidence matches Codex and Claude Code expectations where possible;
     - dangerous external consequences pause before execution;
     - runtime fixture, result-artifact, cancellation, redaction, truncation, and timeout evidence keep passing conformance.

7. **Add Windows Service and systemd managed host lifecycle**
   - Labels: `area:service`, `area:host`, `kind:feature`, `kind:ops`, `priority:p1`
   - Acceptance:
     - `rdev host install-service` supports Linux systemd units and Windows Service command planning;
     - install/status/service-control/stop/uninstall are inspectable, with Windows real install/start/reconnect/stop/uninstall acceptance tracked separately from dry-run `sc.exe` plans;
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
     - `scripts/github/plan-platform-release.sh` produces `rdev.github-platform-release-plan.v1` without external mutation;
     - checksums, signed artifact manifests, and signed release index verify;
     - redacted acceptance transcripts, report JSON, audit verification, and evidence checksums are packaged.

## Bootstrap Script

Preview the GitHub changes locally:

```bash
scripts/github/bootstrap-project.sh --dry-run OWNER/remote-dev-skillkit
```

Audit the local GitHub project-management surface before opening or mutating
anything on GitHub:

```bash
scripts/github/audit-project-readiness.sh \
  --repo OWNER/remote-dev-skillkit \
  --out dist/github-project-readiness.json
```

Preview GitHub Release publication from a verified local candidate:

```bash
scripts/github/plan-release.sh \
  --candidate dist/release-candidate \
  --repo OWNER/remote-dev-skillkit \
  --require-artifacts rdev-host.exe,rdev-verify.exe
```

This generates a local plan, release notes, verification JSON, and command
preview. It does not create a release or upload assets.

Preview a multi-platform GitHub Release publication from verified platform
candidates:

```bash
scripts/github/plan-platform-release.sh \
  --platform-candidates dist/release-candidates/platform-candidates.json \
  --repo OWNER/remote-dev-skillkit
```

Preview post-release download and install verification steps from that local
release plan:

```bash
scripts/github/plan-post-release-install.sh \
  --release-plan dist/release-candidates/github-platform-release-plan/plan.json
```

This writes `rdev.post-release-install-plan.v1`, `VERIFY_INSTALL.md`, one
platform verification script per archive, and a Skillkit verification script. It
does not publish or download anything during planning.

Verify that the generated post-release install plan and scripts are internally
consistent before archiving them as release evidence:

```bash
scripts/github/verify-post-release-install-plan.sh \
  --plan dist/release-candidates/github-platform-release-plan/post-release-install/post-release-install-plan.json
```

This generates platform archives, `platform-release-index.json`,
`platform-release-verification.json`, `INSTALL_PLATFORMS.md`, release notes, and
command previews. It does not create a release or upload assets.

Recommended sequence after explicit approval:

```bash
gh repo create OWNER/remote-dev-skillkit --public --source . --remote origin --push
scripts/github/bootstrap-project.sh OWNER/remote-dev-skillkit
```

If the repository already exists:

```bash
git remote add origin https://github.com/OWNER/remote-dev-skillkit.git
git push -u origin main
scripts/github/bootstrap-project.sh OWNER/remote-dev-skillkit
```

If a private staging repository was used earlier, switch visibility deliberately:

```bash
gh repo edit OWNER/remote-dev-skillkit --visibility public
```

Do this only after the security policy, release signing, manual acceptance
evidence status, and install docs match the shipped behavior.
