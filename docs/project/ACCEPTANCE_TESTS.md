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

## Gate 0: Development Gateway Recovery Regression

Purpose: prove the local development gateway can survive a restart without
reissuing trust or losing already acknowledged tickets, hosts, jobs, artifacts,
and audit events.

### Steps

1. Start `rdev gateway serve --dev` with both `--signing-key` and `--state`.
2. Create a ticket, register and approve a host, create a signed job, claim it,
   and complete it with an artifact.
3. Stop the gateway.
4. Restart the gateway with the same `--signing-key` and `--state`.
5. Confirm the restored snapshot uses `rdev.gateway-snapshot.v1`.
6. Confirm the restored job envelope still verifies against the same gateway
   signing key and host id.
7. Confirm artifacts and audit events are still present.
8. Start the gateway with a different signing key against the same snapshot and
   confirm restoration is rejected.

### Pass Criteria

- The snapshot file is written atomically with mode `0600` under a private
  parent directory.
- Restoration preserves tickets, hosts, jobs, artifacts, audit events, and the
  signed trust bundle.
- Restoration rejects schema mismatch, signing-key mismatch, duplicate ids,
  broken references, and audit sequence gaps.
- The gate is documented as development or single-user recovery only, not as
  production gateway storage.

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

Purpose: prove that an operator-owned Mac can run a agent-requested coding job through a managed host and return diff/test evidence without pushing or merging automatically.

### Required Environment

- macOS managed host owned by an operator.
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

## Gate D: Managed Windows Service

Purpose: prove that an owned or formally managed Windows host can run `rdev` as
an explicit Windows Service without borrowing the attended-temporary contract.

The local planning gate is:

```bash
rdev acceptance windows-managed-service \
  --out .rdev/acceptance/windows-managed-service \
  --binary 'C:\Program Files\rdev\rdev.exe' \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --release-bundle 'C:\Program Files\rdev\release-bundle.json' \
  --release-root-public-key release-root:... \
  --release-require-artifacts rdev.exe,rdev-host.exe,rdev-verify.exe

rdev acceptance verify-windows-managed-service \
  --plan .rdev/acceptance/windows-managed-service/windows-managed-service-plan.json
```

These commands generate and verify a plan only. They do not run PowerShell,
`sc.exe`, create a service, start a service, or delete a service.

### Required Evidence

- Verified `rdev.acceptance.windows-managed-service-plan.v1` output.
- Elevated PowerShell transcript for reviewed `sc.exe create` and
  `sc.exe description`.
- `sc.exe query` and `sc.exe qc` transcript after creation and after start.
- Release-bundle startup gate output before host registration.
- Gateway host registration, approval, trust refresh, and managed-host audit
  events.
- Reconnect evidence after login or reboot.
- Managed coding or repair evidence bundle with approval-required artifacts.
- `sc.exe stop` and `sc.exe delete` uninstall transcript.
- Evidence that attended-temporary mode remains non-persistent and separate.

### Pass Criteria

- The service uses `start= demand` unless a later explicit policy changes it.
- The service command runs `rdev host serve --mode managed --once=false`.
- Identity, trust, nonce, approval, workspace-lock, and release-bundle gate
  arguments are present.
- The host reconnects and rejects out-of-policy work locally.
- Stop and uninstall are explicit, transcripted, and auditable.

### Fail Criteria

- The plan or transcript weakens execution policy, firewall, Defender, UAC, or
  Group Policy.
- A Windows Service claim is made from a plan without real Windows host
  transcripts.
- The service silently grants L3/L4 operations or external consequences.
- Temporary-mode evidence is reused to claim managed Windows Service support.

## Gate E: Managed Linux systemd User Service

Purpose: prove that an owned or formally managed Linux host can run `rdev` as
an explicit systemd user service without borrowing the attended-temporary
contract.

The local planning gate is:

```bash
rdev acceptance linux-managed-service \
  --out .rdev/acceptance/linux-managed-service \
  --binary /opt/rdev/rdev \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --release-bundle /opt/rdev/release-bundle.json \
  --release-root-public-key release-root:... \
  --release-require-artifacts rdev,rdev-host,rdev-verify

rdev acceptance verify-linux-managed-service \
  --plan .rdev/acceptance/linux-managed-service/linux-managed-service-plan.json
```

These commands generate and verify a plan only. They write a local reviewed
unit file in the acceptance output directory, but they do not run `systemctl`,
enable a service, start a service, stop a service, or remove a service.

After a real Linux host run, package release evidence with:

```bash
rdev acceptance package-linux-managed-service \
  --plan .rdev/acceptance/linux-managed-service/linux-managed-service-plan.json \
  --out .rdev/acceptance/linux-managed-service-evidence \
  --start-transcript start.txt \
  --status-transcript status.txt \
  --logs journal.txt \
  --release-gate release-gate.json \
  --audit audit.jsonl \
  --reconnect reconnect.txt \
  --job-evidence-dir job-evidence \
  --stop-transcript stop.txt \
  --uninstall-transcript uninstall.txt
```

This command packages and checks evidence only. It does not execute
`systemctl`, start or stop the service, or claim acceptance if required evidence
is missing.

### Required Evidence

- Verified `rdev.acceptance.linux-managed-service-plan.v1` output.
- Written `0600` systemd user unit reviewed before execution.
- `systemctl --user daemon-reload` transcript.
- `systemctl --user enable --now <unit>` transcript.
- `systemctl --user status <unit>` transcript after start.
- `journalctl --user -u <unit>` service log excerpt.
- Release-bundle startup gate output before host registration.
- Gateway host registration, approval, trust refresh, and managed-host audit
  events.
- Reconnect evidence after logout or reboot, including any separately approved
  linger/setup transcript if required.
- Managed coding or repair evidence bundle with approval-required artifacts.
- `systemctl --user disable --now <unit>` and uninstall transcript.

### Pass Criteria

- The unit runs `rdev host serve --mode managed --once=false`.
- Identity, trust, nonce, approval, workspace-lock, and release-bundle gate
  arguments are present.
- The unit uses systemd user service scope, `Restart=on-failure`,
  `NoNewPrivileges=true`, and `PrivateTmp=true`.
- The host reconnects after the documented logout/reboot acceptance step and
  rejects out-of-policy work locally.
- Stop and uninstall are explicit, transcripted, and auditable.

### Fail Criteria

- The plan or transcript writes to `/etc/systemd/system`, requires unreviewed
  `sudo`, weakens firewall policy, installs cron persistence, or changes login
  linger policy without a separate approval transcript.
- A Linux managed-service claim is made from a plan without real Linux host
  start/status/reboot/reconnect/stop/uninstall transcripts.
- The service silently grants L3/L4 operations or external consequences.
- Temporary-mode evidence is reused to claim managed Linux systemd support.

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
- signed host registration proof through `rdev.host-registration-proof.v1` for registrations that present identity keys;
- signed host enrollment certificate checks through `rdev.host-enrollment-certificate.v1`, including CLI signing/verification, gateway enforcement when an enrollment root is configured, host registration attachment, and rejection of missing, expired, wrong-root, or tampered host metadata/capability bindings;
- dev hosted enrollment issuance checks through `POST /v1/enrollment/certificates` and `rdev enrollment issue-certificate`, including configured issuer requirement, optional bearer-token enforcement, token-file header propagation, pinned-root verification before local write, certificate fingerprint checks, and rejection of requested capabilities outside the ticket capability set;
- local enrollment certificate renewal checks through `rdev enrollment renew-certificate`, including current-certificate verification, unchanged ticket/mode/host/identity/capability scope, changed certificate fingerprint, extended validity, renewed certificate verification, and rejection when the current certificate is listed in signed revocations;
- dev hosted enrollment renewal checks through `POST /v1/enrollment/certificates/renew` and `rdev enrollment renew-certificate --gateway`, including optional bearer token reuse, pinned-root verification, previous certificate fingerprint checks, renewed certificate fingerprint checks, renewed signature verification, and unchanged certificate scope;
- host-side hosted enrollment renewal checks through `rdev host serve --renew-enrollment-certificate --enrollment-root-public-key`, including explicit flag requirements, near-expiry renewal, fresh-certificate skip behavior, token-file support through the gateway client, certificate-file replacement, success summaries, and gateway rejection when the current certificate is listed in signed revocations;
- signed enrollment certificate revocation-list checks through `rdev.host-enrollment-revocations.v1`, including `rdev enrollment init-revocations` empty baselines, CLI revoke/verify, `verify-certificate --revocations`, gateway enforcement when a revocation list is configured, and rejection of revoked certificates;
- dev revocation-list distribution checks through `GET /v1/enrollment/revocations` and `rdev enrollment fetch-revocations`, including empty and non-empty signed lists, optional bearer-token enforcement, CLI token-file propagation, pinned-root verification, and wrong-root rejection before the fetched list is used by CLI or MCP workflows;
- host-side hosted revocation refresh checks through `rdev host serve --fetch-enrollment-revocations --enrollment-root-public-key`, including explicit flag requirements, optional issuer-token propagation through the gateway client, signed list verification, local certificate signature verification against the pinned root, success summaries, and revoked-certificate rejection before registration is attempted;
- MCP enrollment certificate verification through `rdev.enrollment.verify_certificate`, including structured `rdev.enrollment-certificate-verification.v1` success/failure reports, certificate fingerprints, optional revocation-list checks, revoked-certificate failure evidence, and wrong-root failure evidence before an agent trusts a host registration;
- host-side job verification through signed trust bundle active keys, with legacy trust fallback.
- durable host trust bundle file storage with 0600 permissions, update verification, rollback rejection, and stored-bundle fallback.
- durable host identity key file storage with 0600 permissions, registration identity preservation, signed job envelope identity binding, and host-side fingerprint mismatch rejection.
- host-side nonce replay rejection with in-memory and file-backed stores.
- hash-chained audit export and verification through `rdev audit export` / `rdev audit verify`.
- local evidence bundle export through `rdev evidence export`, including manifest, checksums, signed envelope, policy decision, artifacts, audit slice, and audit chain.
- gateway/API evidence bundle export from job ids through `GET /v1/jobs/{job_id}/evidence-bundle` and `rdev evidence export --gateway ... --job-id ...`.
- Skillkit bundle export and verification through `rdev skillkit export` and `rdev skillkit verify`, including manifest checksums, required skills, MCP contracts, framework install notes, safe bundle paths, listed file SHA-256/size checks, and unlisted-file detection.
- Skillkit framework install planning through `rdev skillkit plan-install` and `rdev skillkit verify-install-plan`, including bundle verification, per-framework shell/PowerShell scripts, no external mutation, no silent MCP config writes, no overwrite without `RDEV_SKILLKIT_FORCE=1`, explicit generic MCP target env, listed file SHA-256/size checks, and unlisted-file detection.
- Skillkit direct installation through `rdev skillkit install`, including dry-run-by-default reports, explicit `--execute` before local mutation, bundle verification before copy, target root refusal, generic target enforcement, conflict refusal unless `--force`, copied skills/reference files, and `external_mutation=false`.
- Release candidate packaging through `rdev release prepare-candidate`, covering staged built artifacts, signed artifact manifests, signed release bundle verification, Skillkit export/verify, SPDX 2.3 SBOM, local provenance, checksums, and `rdev.release-candidate.v1` summary generation without publishing to GitHub or leaking local output paths.
- release candidate verification through `rdev release verify-candidate`, covering relocated candidates, summary schema, root public key, checksums, signed release bundle verification, required artifacts, Skillkit verification, SBOM coverage, provenance coverage, listed file hashes/sizes, and unlisted-file rejection.
- real artifact build smoke through `scripts/release/build-artifacts.sh`, covering actual Go-built `rdev`, `rdev-host`, and `rdev-verify` binaries, `rdev.build-artifacts.v1`, SPDX 2.3 SBOM, `provenance.json`, checksums, and release smoke candidate packaging from real Windows artifacts rather than placeholder files.
- per-platform release candidate smoke through `scripts/release/prepare-platform-candidates.sh`, covering `rdev.build-artifacts.v1` target grouping, required command-to-artifact mapping for Unix and Windows suffixes, one verified candidate per target, `rdev.platform-release-candidates.v1`, and `external_mutation=false`.
- multi-platform GitHub Release dry-run planning through `scripts/github/plan-platform-release.sh`, covering per-platform candidate re-verification, unique Linux/Windows platform archives, `rdev.platform-release-index.v1`, `rdev.github-platform-release-verification.v1`, `INSTALL_PLATFORMS.md`, unique asset names, `gh release` command previews, and `external_mutation=false`.
- GitHub Release dry-run planning through `scripts/github/plan-release.sh`, covering candidate verification, generated release notes, concise asset plan, Skillkit tarball generation, commands preview, and `external_mutation=false`.
- GitHub Actions CI through `.github/workflows/ci.yml`, covering `./scripts/check.sh` and `./scripts/ci/release-smoke.sh`.
- structured host-side denial artifacts through `rdev.host-denial.v1`, covering missing envelope, wrong host, identity mismatch, expired/tampered envelopes, replayed nonce, unsupported adapter, missing capability, missing workspace, non-allowlisted command, and workspace escape.
- CLI host polling reports host-side denials to the dev gateway as failed-job artifacts.
- development HTTPS long-poll host job transport through `GET /v1/hosts/{host_id}/jobs/next?wait_seconds=...` and `rdev host serve --transport long-poll`.
- development gateway TLS/mTLS pre-production transport proof through `rdev gateway serve --dev --tls-cert --tls-key --client-ca` plus `rdev host serve --gateway https://127.0.0.1:<port> --gateway-ca --gateway-client-cert --gateway-client-key`, proving local HTTPS registration and host job HTTP calls work with a CA-signed client certificate and fail without one.
- structured host-side approval-required artifacts through `rdev.approval-required.v1`; unsatisfied signed approvals pause before adapter execution, and gateway-approved jobs carry signed `rdev.approval-token.v1` tokens in the re-signed job envelope.
- shared implicit approval preflight for built-in shell and Codex jobs, covering package installation, elevation, GUI control, service management, push, merge, deploy, publish, and credential changes before adapter execution.
- durable host-side approval token consumption through in-memory and file-backed `rdev.host-approval-store.v1`, including 0600 file permissions, 0700 directory permissions, expiry pruning, and CLI `--approval-store` wiring.
- macOS LaunchAgent plist generation through `rdev host install-service --platform macos`, including managed host arguments, `0600` plist permissions, and explicit launchctl next steps without auto-starting the service.
- macOS LaunchAgent plist status and safe uninstall through `rdev host service-status` and `rdev host uninstall-service`, including label-mismatch refusal to avoid deleting unrelated plists.
- Linux systemd user-unit generation through `rdev host install-service --platform linux`, including managed host arguments, `0600` unit permissions, basic hardening flags, explicit `systemctl --user` next steps, status inspection, dry-run service-control planning, unit mismatch refusal, and safe uninstall without auto-starting the service.
- Linux managed-service acceptance planning and verification through `rdev acceptance linux-managed-service` and `rdev acceptance verify-linux-managed-service`, covering written `0600` systemd user units, managed args, hardening flags, release-bundle gate requirements, required evidence checklist, forbidden policy-weakening command rejection, missing-unit rejection, and no `systemctl` execution.
- Linux managed-service acceptance evidence packaging through `rdev acceptance package-linux-managed-service`, covering verified plan and unit archival, plan-verifier output, redacted start/status/log/release-gate/audit/reconnect/stop/uninstall transcripts, managed job evidence bundle archival, approval-required proof, checksums, and release-blocking failure when release-gate output is not `ok=true`.
- Windows Service managed-host planning through `rdev host install-service --platform windows`, including managed host arguments, release-bundle gate arguments, `start= demand`, reviewed `sc.exe create` / `description` command plans, dry-run status/control/uninstall command planning, explicit `service-control --execute` before invoking `sc.exe`, and no claim of real Windows Service acceptance yet.
- Windows managed-service acceptance planning and verification through `rdev acceptance windows-managed-service` and `rdev acceptance verify-windows-managed-service`, covering reviewed `sc.exe` create/description/query/qc/start/stop/delete plans, managed args, `start= demand`, release-bundle gate requirements, required evidence checklist, forbidden policy-weakening command rejection, and no PowerShell or `sc.exe` execution.
- workspace lock and Git worktree preparation foundation through `rdev.workspace-lock.v1`, `rdev.git-worktree-plan.v1`, `rdev workspace lock/status/unlock`, and `rdev workspace prepare-worktree`, including one-writer rejection, expired-lock replacement, owner-checked unlock, lock cleanup on failed worktree creation, and real `git worktree add` coverage.
- workspace lock enforcement during host execution through `rdev host serve --workspace-lock-store`, including hostrunner lock acquire/release, `workspace_locked` structured denial artifacts, lock release after adapter denial, and managed LaunchAgent `--workspace-lock-store` argument generation.
- managed Mac coding harness through `rdev acceptance managed-mac`, covering managed host registration, Git worktree creation, Codex adapter execution with workspace locking, diff/test evidence export, approval-required probe for `git.push`, workspace lock release, and evidence bundle creation.
- acceptance report verification through `rdev acceptance verify`, covering report checks, coding and approval evidence bundle checksums, artifact index consistency, audit-chain verification, approval-required probe evidence, and workspace-lock release.
- managed Mac LaunchAgent acceptance planning and verification through `rdev acceptance managed-mac-service` and `rdev acceptance verify-managed-mac-service`, covering checked plist generation, managed host arguments, release-bundle gate requirements, `rdev host service-control --execute` command plan, service-backed acceptance command plan, evidence verification command plan, forbidden policy-weakening command rejection, and uninstall guidance without auto-starting launchd.
- managed Mac LaunchAgent acceptance evidence packaging through `rdev acceptance package-managed-mac-service`, covering verified plan and plist archival, plan-verifier output, redacted review/start/inspect/log/release-gate/audit/reconnect/stop/uninstall transcripts, verified managed Mac report/evidence bundle archival, approval-required proof, checksums, and release-blocking failure when release-gate output is not `ok=true` or the managed Mac report fails verification.
- macOS LaunchAgent lifecycle control through `rdev host service-control`, covering dry-run by default, explicit `--execute` for launchctl, plist label checks, and start/inspect/stop action planning.
- Windows temporary acceptance planning and verification through `rdev acceptance windows-temporary` and `rdev acceptance verify-windows-temporary`, covering reviewed PowerShell launcher generation, bootstrap script hash capture, signed release manifest or release bundle verifier requirements, launcher safety checks, approval probes for package/elevation/service/GUI/credential operations, no-persistence inspection commands, and required evidence checklist without executing PowerShell.
- Windows temporary acceptance evidence packaging through `rdev acceptance package-windows-temporary`, covering verified plan and launcher archival, redacted transcript and verifier output, audit capture, approval-probe evidence coverage, no-persistence evidence coverage, checksums, and release-blocking failure when verifier output is not `ok=true`.
- release bundle verification through `rdev release create-bundle`, `rdev release verify-bundle`, and standalone `rdev-verify --bundle`, covering signed bundle index verification, per-artifact signed manifest verification, artifact and manifest SHA-256/size checks, required artifact presence, and tamper failure evidence.
- shell adapter cooperative cancellation through `ExecuteContext` and hostrunner context propagation, returning `rdev.shell-result.v1` artifacts with `canceled=true` instead of timeout evidence.
- PowerShell adapter MVP through `adapter=powershell`, covering `powershell.user` capability enforcement, allowlisted PowerShell executable execution, no `-ExecutionPolicy Bypass`, `rdev.powershell-result.v1` evidence, redaction, workspace-lock release, approval-required service-management detection, and context cancellation.
- canceled-job artifact reporting for built-in shell, PowerShell, Codex, Claude Code, and acpx adapters, preserving the gateway job's `canceled` terminal state while adding reviewable cancellation evidence.
- public adapterkit lifecycle-manifest, runtime-fixture, result-artifact, and cancellation-artifact conformance through `pkg/adapterkit`, `adapterkit.RunLifecycle`, `rdev adapter scaffold`, `rdev adapter verify-lifecycle`, `rdev adapter verify-runtime`, `rdev adapter verify-result`, `rdev adapter verify-cancellation`, and MCP tools `rdev.adapter.verify_lifecycle` / `rdev.adapter.verify_runtime` / `rdev.adapter.verify_result` / `rdev.adapter.verify_cancellation`, covering generated adapter templates, executable adapter phase fixtures, adapter phases, safety boundaries, cancellation, cleanup, result schemas, and built-in shell, PowerShell, Codex, Claude Code, and acpx artifacts for schema, timing, redaction metadata, command evidence, canceled-vs-timeout proof, and secret-pattern rejection.
- hostrunner runtime fixture capture through `rdev host serve --capture-runtime-fixture`, including primary adapter result preservation plus appended `rdev.adapter-runtime-fixture.v1` artifacts for shell, PowerShell, Codex, Claude Code, acpx, and canceled shell cleanup.

## Next Automation Targets

- Real managed Mac LaunchAgent acceptance execution: review generated plan, start/inspect/stop with `rdev host service-control --execute`, reconnect after reboot, run locked-worktree Codex, verify evidence, package the run with `rdev acceptance package-managed-mac-service`, and uninstall.
- Windows temporary host acceptance execution: verify the generated plan, run it on a clean Windows VM, verify signed release artifacts, run visible foreground bootstrap, confirm outbound-only host loop, collect no-persistence inspection output, approval-required probes, and revocation transcript.
- Release transcript packaging: publish redacted acceptance command transcript, verified report JSON, Windows temporary evidence package when applicable, and evidence bundle checksums for each release candidate.
- Windows Service managed-host acceptance execution: run the verified reviewed `sc.exe` plan on a clean Windows machine, verify service creation/status/start/stop/delete, confirm release-bundle startup gate, reconnect behavior, local stop/uninstall, and absence of temporary-mode persistence leakage.
- Full production Adapter SDK integration and executable lifecycle/cancellation fixtures for future third-party adapters beyond the built-in hostrunner fixture path.

The following remain real-environment acceptance tests:

- clean Windows one-command temporary bootstrap;
- Windows Authenticode verification against a real code-signing certificate;
- no-persistence inspection on Windows;
- managed Mac LaunchAgent install/uninstall and reconnect after reboot;
- managed Linux systemd user-unit install/uninstall and reconnect after reboot;
- managed Windows Service install/start/reconnect/stop/uninstall on a real Windows host;
- cooperative cancellation generalized across future Adapter SDK lifecycle implementations;
- OS-protected managed host trust and identity storage beyond file-backed dev mode;
- WSS/mTLS transport under NAT.
