# Acceptance Tests

This document defines the acceptance gates for the two golden paths in Remote Dev Skillkit:

- temporary Windows repair;
- managed Mac coding.

It also defines the shared security evidence that must be collected before a release can claim support for either path.

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
- Gateway has a valid release manifest, verifier binary, host binary, and join manifest.

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
7. Confirm `rdev-host.exe` is verified through the signed release manifest.
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
- Release manifest verification output.
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

- missing release manifest;
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
- shell artifact schema and redaction metadata.
- signed trust bundle HTTP read/update flow with rollback rejection.
- host-side job verification through signed trust bundle active keys, with legacy trust fallback.
- durable host trust bundle file storage with 0600 permissions, update verification, rollback rejection, and stored-bundle fallback.
- durable host identity key file storage with 0600 permissions, registration identity preservation, signed job envelope identity binding, and host-side fingerprint mismatch rejection.
- host-side nonce replay rejection with in-memory and file-backed stores.
- hash-chained audit export and verification through `rdev audit export` / `rdev audit verify`.
- local evidence bundle export through `rdev evidence export`, including manifest, checksums, signed envelope, policy decision, artifacts, audit slice, and audit chain.
- structured host-side denial artifacts through `rdev.host-denial.v1`, covering missing envelope, wrong host, identity mismatch, expired/tampered envelopes, replayed nonce, unsupported adapter, missing capability, missing workspace, non-allowlisted command, and workspace escape.
- CLI host polling reports host-side denials to the dev gateway as failed-job artifacts.

The following remain real-environment acceptance tests:

- clean Windows one-command temporary bootstrap;
- Windows Authenticode verification against a real code-signing certificate;
- no-persistence inspection on Windows;
- managed Mac LaunchAgent install/uninstall;
- Codex adapter execution in a locked worktree;
- OS-protected managed host trust and identity storage beyond file-backed dev mode;
- WSS/mTLS transport under NAT.
