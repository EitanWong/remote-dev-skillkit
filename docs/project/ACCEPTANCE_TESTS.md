# Acceptance Tests

This document defines the acceptance gates for the two golden paths in Remote Dev Skillkit:

- temporary Windows repair;
- managed Mac coding.

It also defines the shared security evidence that must be collected before a release can claim support for either path. The acceptance model follows the canonical [Perfect Ending Solution](../architecture/PERFECT_ENDING_SOLUTION.md): every successful path must prove typed intent, signed host-bound envelopes, host-side validation, approval gates, evidence bundles, audit, and revocation.

## Evidence Rules

Every acceptance run must record:

- release version or commit SHA;
- gateway URL and mode;
- host OS, version, architecture, and hostname alias;
- command transcript;
- ticket id and host id;
- job ids;
- artifact ids and checksums;
- audit export path;
- pass/fail result for every gate.

Secrets, host usernames, public IPs, and customer-specific paths must be redacted before evidence is shared outside the operator account.

## Gate A: Temporary Windows Repair

Purpose: prove that a third-party Windows machine can join from one visible command, run a bounded repair session, and leave no service behind.

### Required Environment

- Clean Windows 10 or Windows 11 VM.
- Standard non-admin user.
- PowerShell 5.1 available.
- No preinstalled Node, Go, Git, Python, or package manager requirement for bootstrap.
- Network allows outbound HTTPS to the gateway.
- Gateway has a valid signed release bundle or host release manifest, verifier binary, host binary, and join manifest.

### Steps

1. Start the gateway in production-like mode or documented dev acceptance mode.
2. Create an attended temporary ticket with a TTL no longer than 30 minutes.
3. Open the join page and confirm it displays:
   - operator;
   - server identity;
   - reason;
   - TTL;
   - requested capabilities;
   - stop instructions.
4. Run the one visible PowerShell command from the join page.
5. Confirm bootstrap downloads to a temporary location.
6. Confirm `rdev-verify.exe` is hash-pinned before execution.
7. Confirm `rdev-host.exe` is verified through the signed release bundle or signed host release manifest.
8. Confirm Authenticode is checked when the release policy requires it.
9. Confirm the host starts in foreground temporary mode.
10. Approve the pending host with only scoped capabilities.
11. Run a diagnostic shell or PowerShell job that reads toolchain state.
12. Run a scoped write job inside a temporary workspace.
13. Attempt a non-allowlisted command and confirm rejection.
14. Attempt a workspace escape and confirm rejection.
15. Request package installation or elevation and confirm approval is required.
16. Revoke the host from the gateway.
17. Confirm queued/running jobs are canceled.
18. Close the foreground host window.
19. Confirm no Windows Service, scheduled task, Run key, startup shortcut, or firewall rule was installed by temporary mode.

### Required Evidence

- Join page screenshot or saved HTML.
- Bootstrap transcript.
- `Get-FileHash` evidence for downloaded verifier and host binary.
- Release bundle or release manifest verification output.
- Authenticode verification output when enabled.
- Host registration and approval audit events.
- Job artifacts with schema versions and redaction metadata.
- Policy denial evidence for non-allowlisted command and workspace escape.
- Approval-required evidence for elevation/package install.
- Host revoke audit event and job cancellation audit event.
- Windows checks proving no persistence:
  - `Get-Service *rdev*`
  - `Get-ScheduledTask | Where-Object TaskName -match 'rdev|remote-dev'`
  - registry Run key inspection
  - startup folder inspection

### Pass Criteria

- The host only opens outbound connections.
- The host never exposes a public inbound listener.
- Temporary mode leaves no service or autorun persistence.
- Every executed job has a signed envelope and artifact evidence.
- Unsafe or out-of-policy actions are denied or require approval.
- Revocation prevents future jobs and cancels outstanding jobs.

### Fail Criteria

- Bootstrap requires installing Node, Go, Git, Python, or a package manager.
- Bootstrap weakens PowerShell execution policy, Group Policy, Defender, UAC, or firewall policy.
- Host binary executes before release verification.
- Temporary mode installs hidden persistence.
- An agent can run arbitrary shell without allowlist and policy checks.
- A secret-like token appears unredacted in job artifacts.

## Gate B: Managed Mac Coding

Purpose: prove that an Eitan-owned Mac can run a Lucky-requested coding job through a managed host and return diff/test evidence without pushing or merging automatically.

### Required Environment

- macOS managed host owned by Eitan.
- `rdev-host` installed as an explicit LaunchAgent or foreground managed test process.
- Codex CLI installed and authenticated locally.
- Git installed.
- Test repository with a clean baseline.
- Gateway has the host approved as `managed`.

### Steps

1. Register or select the managed Mac host.
2. Confirm host capabilities include:
   - `git`
   - `shell.user`
   - Codex or the selected coding adapter
   - workspace roots allowed by policy.
3. Create a coding job bound to one repository.
4. Confirm the host locks the workspace.
5. Confirm the host creates a task branch or Git worktree.
6. Run the Codex adapter or documented shell-backed coding adapter.
7. Run verification commands.
8. Collect changed files, diff summary, test commands, exit codes, and artifacts.
9. Attempt a second writer for the same workspace and confirm lock rejection.
10. Attempt write outside the workspace and confirm rejection.
11. Attempt `git push`, merge, deploy, or credential change and confirm approval is required.
12. Cancel or complete the job and confirm the lock is released.

### Real LaunchAgent Extension

The local `rdev acceptance managed-mac` harness proves the safety loop without requiring service installation. The production managed Mac gate additionally requires:

1. Install the host with `rdev host install-service --platform macos`.
2. Review the generated plist, service label, logs path, trust root, workspace lock store, stop command, and uninstall command.
3. Start it through the documented `launchctl` command.
4. Confirm the host reconnects after logout/login or reboot.
5. Run the same Codex locked-worktree job through the managed service.
6. Export the evidence bundle and audit slice from the service-backed run.
7. Stop and uninstall the LaunchAgent.
8. Confirm no unrelated plist or service entry is modified.

### Required Evidence

- Host capability inventory.
- Host identity key id, public key fingerprint, and evidence that the job envelope is bound to the same fingerprint.
- Managed install or managed test-mode transcript.
- Job envelope showing host and workspace binding.
- Workspace lock evidence.
- Branch or worktree name.
- Diff output.
- Test command output and exit codes.
- Job artifact with `rdev.shell-result.v1` or adapter-specific result schema.
- Policy denial evidence for workspace escape.
- Approval-required evidence for push/merge/deploy.
- Audit export covering ticket or managed policy, host, job, artifacts, approvals, and completion.

### Pass Criteria

- Coding job runs only in the approved workspace.
- One writer holds the workspace lock.
- Diff and tests are returned before completion.
- Push, merge, deploy, service changes, and credential changes require approval.
- The agent cannot approve its own dangerous action.

### Fail Criteria

- The managed host writes outside approved roots.
- The job runs on an unapproved host.
- A second writer can mutate the same workspace without an isolated worktree.
- Push, merge, or deploy happens without approval.
- Artifacts omit the diff/test evidence needed for review.

## Gate C: Shared Security Regression

These checks apply to every release candidate.

### Envelope Validation

The host must reject:

- tampered envelope payload;
- expired envelope;
- wrong host id;
- wrong signing key id;
- replayed nonce;
- missing required capability;
- unsupported adapter.

### Policy Validation

The host must reject or require approval for:

- workspace escape;
- symlink escape;
- non-allowlisted command;
- package install;
- elevation;
- GUI control;
- service changes;
- credential access;
- destructive filesystem operation outside the workspace;
- push, merge, deploy, publish, or paid job.

### Release Validation

Bootstrap must reject:

- missing release bundle or release manifest;
- wrong SHA-256;
- wrong release root;
- tampered host binary;
- tampered verifier binary;
- invalid Authenticode signature when policy requires it.

### Audit Validation

Audit export must prove:

- ticket creation;
- host registration;
- host approval;
- policy decisions;
- job creation;
- job lease or claim;
- job completion/failure/cancel;
- artifact creation;
- approval decisions;
- host revocation.

## Current Automation Coverage

The local test suite currently covers:

- signed job envelope creation and host verification;
- signed trust bundle rotation, rollback rejection, previous-hash validation, and key revocation checks;
- tamper and expiry rejection;
- host-bound job execution;
- scoped shell execution;
- allowlist rejection;
- workspace write-scope rejection;
- symlink write-scope escape rejection, including missing child paths below symlinks;
- shell policy explain;
- host revocation canceling queued/running jobs;
- release artifact signing and verification primitives;
- Windows bootstrap verifier handoff logic;
- shell artifact schema and redaction metadata;
- signed trust bundle HTTP read/update flow with rollback rejection;
- host-bound trust bundle update checks through `rdev.trust-bundle-update.v1`, including current/update-available responses and CLI trust-store persistence;
- host-side job verification through signed trust bundle active keys, with legacy trust fallback.
- durable host trust bundle file storage with 0600 permissions, update verification, rollback rejection, and stored-bundle fallback.
- durable host identity key file storage with 0600 permissions, registration identity preservation, signed job envelope identity binding, and host-side fingerprint mismatch rejection.
- host-side nonce replay rejection with in-memory and file-backed stores.
- hash-chained audit export and verification through `rdev audit export` / `rdev audit verify`.
- local evidence bundle export through `rdev evidence export`, including manifest, checksums, signed envelope, policy decision, artifacts, audit slice, and audit chain.
- gateway/API evidence bundle export from job ids through `GET /v1/jobs/{job_id}/evidence-bundle` and `rdev evidence export --gateway ... --job-id ...`.
- Skillkit bundle export through `rdev skillkit export`, including manifest checksums, skills, MCP contracts, and framework install notes.
- structured host-side denial artifacts through `rdev.host-denial.v1`, covering missing envelope, wrong host, identity mismatch, expired/tampered envelopes, replayed nonce, unsupported adapter, missing capability, missing workspace, non-allowlisted command, and workspace escape.
- CLI host polling reports host-side denials to the dev gateway as failed-job artifacts.
- development HTTPS long-poll host job transport through `GET /v1/hosts/{host_id}/jobs/next?wait_seconds=...` and `rdev host serve --transport long-poll`.
- structured host-side approval-required artifacts through `rdev.approval-required.v1`; unsatisfied signed approvals pause before adapter execution, and gateway-approved jobs carry signed `rdev.approval-token.v1` tokens in the re-signed job envelope.
- shared implicit approval preflight for built-in shell and Codex jobs, covering package installation, elevation, GUI control, service management, push, merge, deploy, publish, and credential changes before adapter execution.
- durable host-side approval token consumption through in-memory and file-backed `rdev.host-approval-store.v1`, including 0600 file permissions, 0700 directory permissions, expiry pruning, and CLI `--approval-store` wiring.
- macOS LaunchAgent plist generation through `rdev host install-service --platform macos`, including managed host arguments, `0600` plist permissions, and explicit launchctl next steps without auto-starting the service.
- macOS LaunchAgent plist status and safe uninstall through `rdev host service-status` and `rdev host uninstall-service`, including label-mismatch refusal to avoid deleting unrelated plists.
- workspace lock and Git worktree preparation foundation through `rdev.workspace-lock.v1`, `rdev.git-worktree-plan.v1`, `rdev workspace lock/status/unlock`, and `rdev workspace prepare-worktree`, including one-writer rejection, expired-lock replacement, owner-checked unlock, lock cleanup on failed worktree creation, and real `git worktree add` coverage.
- workspace lock enforcement during host execution through `rdev host serve --workspace-lock-store`, including hostrunner lock acquire/release, `workspace_locked` structured denial artifacts, lock release after adapter denial, and managed LaunchAgent `--workspace-lock-store` argument generation.
- managed Mac coding harness through `rdev acceptance managed-mac`, covering managed host registration, Git worktree creation, Codex adapter execution with workspace locking, diff/test evidence export, approval-required probe for `git.push`, workspace lock release, and evidence bundle creation.
- acceptance report verification through `rdev acceptance verify`, covering report checks, coding and approval evidence bundle checksums, artifact index consistency, audit-chain verification, approval-required probe evidence, and workspace-lock release.
- managed Mac LaunchAgent acceptance planning through `rdev acceptance managed-mac-service`, covering checked plist generation, managed host arguments, `rdev host service-control --execute` command plan, service-backed acceptance command plan, evidence verification command plan, and uninstall guidance without auto-starting launchd.
- macOS LaunchAgent lifecycle control through `rdev host service-control`, covering dry-run by default, explicit `--execute` for launchctl, plist label checks, and start/inspect/stop action planning.
- Windows temporary acceptance planning and verification through `rdev acceptance windows-temporary` and `rdev acceptance verify-windows-temporary`, covering reviewed PowerShell launcher generation, bootstrap script hash capture, signed release manifest or release bundle verifier requirements, launcher safety checks, approval probes for package/elevation/service/GUI/credential operations, no-persistence inspection commands, and required evidence checklist without executing PowerShell.
- Windows temporary acceptance evidence packaging through `rdev acceptance package-windows-temporary`, covering verified plan and launcher archival, redacted transcript and verifier output, audit capture, approval-probe evidence coverage, no-persistence evidence coverage, checksums, and release-blocking failure when verifier output is not `ok=true`.
- release bundle verification through `rdev release create-bundle`, `rdev release verify-bundle`, and standalone `rdev-verify --bundle`, covering signed bundle index verification, per-artifact signed manifest verification, artifact and manifest SHA-256/size checks, required artifact presence, and tamper failure evidence.

## Next Automation Targets

- Real managed Mac LaunchAgent acceptance execution: review generated plan, start/inspect/stop with `rdev host service-control --execute`, reconnect after reboot, run locked-worktree Codex, verify evidence, and uninstall.
- Windows temporary host acceptance execution: verify the generated plan, run it on a clean Windows VM, verify signed release artifacts, run visible foreground bootstrap, confirm outbound-only host loop, collect no-persistence inspection output, approval-required probes, and revocation transcript.
- Release transcript packaging: publish redacted acceptance command transcript, verified report JSON, Windows temporary evidence package when applicable, and evidence bundle checksums for each release candidate.
- Codex adapter MVP through `adapter=codex`, including `codex.run` and `git.diff` capability checks, locked-workspace hostrunner execution, implicit approval preflight for push/merge/deploy/publish/credential/service intents, `rdev.codex-result.v1` artifacts, Git status/diff evidence, optional allowlisted verification command evidence, `rdev.test-report.v1` parsing for `go test -json`, output caps, redaction, and lock release after execution.
- Codex adapter conformance tests through `internal/codexadapter/conformance_test.go`, covering canonical workspace roots, write-scope escape rejection before execution, nonzero Codex exits that still produce evidence, prompt/argv/stdout/stderr/diff redaction, output truncation flags, and duration-timeout cancellation evidence.
- Codex adapter cooperative cancellation through `ExecuteContext`, hostrunner context propagation, and `rdev host serve` gateway job status monitoring that stops a running Codex process when the job becomes `canceled`.
- Canceled-job artifact reporting through `POST /v1/jobs/{job_id}/artifact`, preserving the gateway job's `canceled` terminal state while adding reviewable cancellation evidence.

The following remain real-environment acceptance tests:

- clean Windows one-command temporary bootstrap;
- Windows Authenticode verification against a real code-signing certificate;
- no-persistence inspection on Windows;
- managed Mac LaunchAgent install/uninstall and reconnect after reboot;
- cooperative cancellation generalized across shell, PowerShell, and future adapter SDK implementations;
- OS-protected managed host trust and identity storage beyond file-backed dev mode;
- WSS/mTLS transport under NAT.
