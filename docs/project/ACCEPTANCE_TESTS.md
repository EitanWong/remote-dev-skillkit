# Acceptance Tests

This document defines the acceptance gates for the two golden paths in Remote Dev Skillkit:

- temporary Windows repair;
- managed Mac coding.

It also defines the shared security evidence that must be collected before a release can claim support for either path. The acceptance model follows the canonical [Perfect Ending Solution](../architecture/PERFECT_ENDING_SOLUTION.md): every successful path must prove typed intent, session task authorization, host-side validation, interrupt gates, session evidence, audit, and revocation.

## Evidence Rules

Every acceptance run must record:

- release version or commit SHA;
- gateway URL and mode;
- host OS, version, architecture, and hostname alias;
- command transcript;
- ticket id, session id, and endpoint id;
- task ids;
- artifact ids and checksums;
- audit export path;
- pass/fail result for every gate.

Secrets, host usernames, public IPs, and customer-specific paths must be redacted before evidence is shared outside the operator account.

## Gate 0: Development Gateway Recovery Regression

Purpose: prove the local development gateway can survive a restart without
reissuing trust or losing already acknowledged tickets, sessions, endpoints, tasks, artifacts,
and audit events.

### Steps

1. Start `rdev gateway serve --dev` with both `--signing-key` and `--state`.
2. Create a ticket, join a target endpoint to a session, create a signed task,
   claim it, and complete it with an artifact.
3. Stop the gateway.
4. Restart the gateway with the same `--signing-key` and `--state`.
5. Confirm the restored snapshot uses `rdev.gateway-snapshot.v1`.
6. Confirm the restored task binding still verifies against the same gateway
   signing key and endpoint id.
7. Confirm artifacts and audit events are still present.
8. Start the gateway with a different signing key against the same snapshot and
   confirm restoration is rejected.

### Pass Criteria

- The snapshot file is written atomically with mode `0600` under a private
  parent directory.
- Restoration preserves tickets, hosts, tasks, artifacts, audit events, and the
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
10. Authorize the pending host with only scoped capabilities.
11. Run a diagnostic shell or PowerShell task that reads toolchain state.
12. Run a scoped write task inside a temporary workspace.
13. Attempt a non-allowlisted command and confirm rejection.
14. Attempt a workspace escape and confirm rejection.
15. Request package installation or elevation and confirm authorization is required.
16. Revoke the host from the gateway.
17. Confirm queued/running tasks are canceled.
18. Close the foreground host window.
19. Confirm no Windows Service, scheduled task, Run key, startup shortcut, or firewall rule was installed by temporary mode.

### Required Evidence

- Join page screenshot or saved HTML.
- Bootstrap transcript.
- `Get-FileHash` evidence for downloaded verifier and host binary.
- Release bundle or release manifest verification output.
- Authenticode verification output when enabled.
- Host registration and authorization audit events.
- Task artifacts with schema versions and redaction metadata.
- Policy denial evidence for non-allowlisted command and workspace escape.
- Authorization-required evidence for elevation/package install.
- Host revoke audit event and task cancellation audit event.
- Windows checks proving no persistence:
  - `Get-Service *rdev*`
  - `Get-ScheduledTask | Where-Object TaskName -match 'rdev|remote-dev'`
  - registry Run key inspection
  - startup folder inspection

### Pass Criteria

- The host only opens outbound connections.
- The host never exposes a public inbound listener.
- Temporary mode leaves no service or autorun persistence.
- Every executed task has a signed envelope and artifact evidence.
- Unsafe or out-of-policy actions are denied or require authorization.
- Revocation prevents future tasks and cancels outstanding tasks.

### Fail Criteria

- Bootstrap requires installing Node, Go, Git, Python, or a package manager.
- Bootstrap weakens PowerShell execution policy, Group Policy, Defender, UAC, or firewall policy.
- Host binary executes before release verification.
- Temporary mode installs hidden persistence.
- An agent can run arbitrary shell without allowlist and policy checks.
- A secret-like token appears unredacted in task artifacts.

## Gate B: Managed Mac Coding

Purpose: prove that an operator-owned Mac can run a agent-requested coding task through a managed host and return diff/test evidence without pushing or merging automatically.

### Required Environment

- macOS managed host owned by an operator.
- `rdev-host` installed as an explicit LaunchAgent or foreground managed test process.
- Codex CLI installed and authenticated locally.
- Git installed.
- Test repository with a clean baseline.
- Gateway has the host authorized as `managed`.

### Steps

1. Register or select the managed Mac host.
2. Confirm host capabilities include:
   - `git`
   - `shell.user`
   - Codex or the selected coding adapter
   - workspace roots allowed by policy.
3. Create a coding task bound to one repository.
4. Confirm the host locks the workspace.
5. Confirm the host creates a task branch or Git worktree.
6. Run the Codex adapter or documented shell-backed coding adapter.
7. Run verification commands.
8. Collect changed files, diff summary, test commands, exit codes, and artifacts.
9. Attempt a second writer for the same workspace and confirm lock rejection.
10. Attempt write outside the workspace and confirm rejection.
11. Attempt `git push`, merge, deploy, or credential change and confirm authorization is required.
12. Cancel or complete the task and confirm the lock is released.

### Real LaunchAgent Extension

The local `rdev acceptance managed-mac` harness proves the safety loop without requiring service installation. The production managed Mac gate additionally requires:

1. Install the host with `rdev host install-service --platform macos`.
2. Review the generated plist, service label, logs path, trust root, workspace lock store, stop command, and uninstall command.
3. Start it through the documented `launchctl` command.
4. Confirm the host reconnects after logout/login or reboot.
5. Run the same Codex locked-worktree task through the managed service.
6. Export the session evidence and audit slice from the service-backed run.
7. Stop and uninstall the LaunchAgent.
8. Confirm no unrelated plist or service entry is modified.

### Required Evidence

- Host capability inventory.
- Host identity key id, public key fingerprint, and evidence that the session task is bound to the same endpoint identity.
- Managed install or managed test-mode transcript.
- Session task record showing endpoint and workspace binding.
- Workspace lock evidence.
- Branch or worktree name.
- Diff output.
- Test command output and exit codes.
- Task artifact with `rdev.shell-result.v1` or adapter-specific result schema.
- Policy denial evidence for workspace escape.
- Authorization-required evidence for push/merge/deploy.
- Audit export covering ticket or managed policy, host, task, artifacts, authorizations, and completion.

### Pass Criteria

- Coding task runs only in the authorized workspace.
- One writer holds the workspace lock.
- Diff and tests are returned before completion.
- Push, merge, deploy, service changes, and credential changes require authorization.
- The agent cannot authorize its own dangerous action.

### Fail Criteria

- The managed host writes outside authorized roots.
- The task runs on an unauthorized host.
- A second writer can mutate the same workspace without an isolated worktree.
- Push, merge, or deploy happens without authorization.
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

The host must reject or require authorization for:

- workspace escape;
- symlink escape;
- non-allowlisted command;
- package install;
- elevation;
- GUI control;
- service changes;
- credential access;
- destructive filesystem operation outside the workspace;
- push, merge, deploy, publish, or paid task.

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
- host authorization;
- policy decisions;
- task creation;
- task lease or claim;
- task completion/failure/cancel;
- artifact creation;
- authorization decisions;
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
- Gateway session join, endpoint trust refresh, and managed-host audit
  events.
- Reconnect evidence after login or reboot.
- Managed coding or repair session evidence with host-denial probe artifacts.
- `sc.exe stop` and `sc.exe delete` uninstall transcript.
- Evidence that attended-temporary mode remains non-persistent and separate.

### Pass Criteria

- The service uses `start= demand` unless a later explicit policy changes it.
- The service command runs `rdev host serve --mode managed --once=false`.
- Identity, trust, workspace-lock, and release-bundle gate
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
  --session-evidence-dir session-evidence \
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
- Gateway session join, endpoint trust refresh, and managed-host audit
  events.
- Reconnect evidence after logout or reboot, including any separately authorized
  linger/setup transcript if required.
- Managed coding or repair session evidence with host-denial probe artifacts.
- `systemctl --user disable --now <unit>` and uninstall transcript.

### Pass Criteria

- The unit runs `rdev host serve --mode managed --once=false`.
- Identity, trust, workspace-lock, and release-bundle gate
  arguments are present.
- The unit uses systemd user service scope, `Restart=on-failure`,
  `NoNewPrivileges=true`, and `PrivateTmp=true`.
- The host reconnects after the documented logout/reboot acceptance step and
  rejects out-of-policy work locally.
- Stop and uninstall are explicit, transcripted, and auditable.

### Fail Criteria

- The plan or transcript writes to `/etc/systemd/system`, requires unreviewed
  `sudo`, weakens firewall policy, installs cron persistence, or changes login
  linger policy without a separate authorization transcript.
- A Linux managed-service claim is made from a plan without real Linux host
  start/status/reboot/reconnect/stop/uninstall transcripts.
- The service silently grants L3/L4 operations or external consequences.
- Temporary-mode evidence is reused to claim managed Linux systemd support.

## Current Automation Coverage

The local test suite currently covers:

- session task creation and host-side validation;
- signed trust bundle rotation, rollback rejection, previous-hash validation, and key revocation checks;
- tamper and expiry rejection;
- session task execution;
- scoped shell execution;
- allowlist rejection;
- workspace write-scope rejection;
- symlink write-scope escape rejection, including missing child paths below symlinks;
- shell policy explain;
- host revocation canceling queued/running tasks;
- release artifact signing and verification primitives;
- Windows bootstrap verifier handoff logic;
- shell artifact schema and redaction metadata;
- signed trust bundle HTTP read/update flow with rollback rejection;
- host-bound trust bundle update checks through `rdev.trust-bundle-update.v1`, including current/update-available responses and CLI trust-store persistence;
- signed host registration proof through `rdev.host-registration-proof.v1` for registrations that present identity keys;
- signed host enrollment certificate checks through `rdev.host-enrollment-certificate.v1`, including CLI signing/verification, gateway enforcement when an enrollment root is configured, host registration attachment, and rejection of missing, expired, wrong-root, or tampered host metadata/capability bindings;
- dev hosted enrollment issuance checks through `POST /v1/enrollment/certificates` and `rdev enrollment issue-certificate`, including configured issuer requirement, operator-auth issuer-role enforcement, operator-token file header propagation, pinned-root verification before local write, certificate fingerprint checks, and rejection of requested capabilities outside the ticket capability set;
- local enrollment certificate renewal checks through `rdev enrollment renew-certificate`, including current-certificate verification, unchanged ticket/mode/host/identity/capability scope, changed certificate fingerprint, extended validity, renewed certificate verification, and rejection when the current certificate is listed in signed revocations;
- dev hosted enrollment renewal checks through `POST /v1/enrollment/certificates/renew` and `rdev enrollment renew-certificate --gateway`, including operator-auth issuer-role enforcement, pinned-root verification, previous certificate fingerprint checks, renewed certificate fingerprint checks, renewed signature verification, and unchanged certificate scope;
- host-side hosted enrollment renewal checks through `rdev host serve --renew-enrollment-certificate --enrollment-root-public-key`, including explicit flag requirements, near-expiry renewal, fresh-certificate skip behavior, token-file support through the gateway client, certificate-file replacement, success summaries, and gateway rejection when the current certificate is listed in signed revocations;
- signed enrollment certificate revocation-list checks through `rdev.host-enrollment-revocations.v1`, including `rdev enrollment init-revocations` empty baselines, CLI revoke/verify, `verify-certificate --revocations`, gateway enforcement when a revocation list is configured, and rejection of revoked certificates;
- dev revocation-list distribution checks through `GET /v1/enrollment/revocations` and `rdev enrollment fetch-revocations`, including empty and non-empty signed lists, operator-auth issuer-role enforcement, CLI operator-token propagation, pinned-root verification, and wrong-root rejection before the fetched list is used by CLI or MCP workflows;
- host-side hosted revocation refresh checks through `rdev host serve --fetch-enrollment-revocations --enrollment-root-public-key`, including explicit flag requirements, optional operator-token propagation through the gateway client, signed list verification, local certificate signature verification against the pinned root, success summaries, and revoked-certificate rejection before registration is attempted;
- MCP enrollment certificate verification through `rdev.enrollment.verify_certificate`, including structured `rdev.enrollment-certificate-verification.v1` success/failure reports, certificate fingerprints, optional revocation-list checks, revoked-certificate failure evidence, and wrong-root failure evidence before an agent trusts a host registration;
- host-side task verification through signed trust bundle active keys, with legacy trust fallback only for retired internal evidence fixtures.
- durable host trust bundle file storage with 0600 permissions, update verification, rollback rejection, and stored-bundle fallback.
- durable host identity key file storage with 0600 permissions, session endpoint identity preservation, task identity binding, and host-side fingerprint mismatch rejection.
- host-side replay rejection with in-memory and file-backed stores.
- hash-chained audit export and verification through `rdev audit export` / `rdev audit verify`.
- session evidence export from Control Plane v1 session/task/artifact state, without any retired task gateway route, retired evidence-export CLI, or task-id gateway mode.
- Skillkit bundle export and verification through `rdev skillkit export` and `rdev skillkit verify`, including manifest checksums, required skills, MCP contracts, framework install notes, safe bundle paths, listed file SHA-256/size checks, and unlisted-file detection.
- Skillkit framework install planning through `rdev skillkit plan-install` and `rdev skillkit verify-install-plan`, including bundle verification, per-framework shell/PowerShell scripts, no external mutation, no silent MCP config writes, no overwrite without `RDEV_SKILLKIT_FORCE=1`, explicit generic MCP target env, listed file SHA-256/size checks, and unlisted-file detection.
- Skillkit direct installation through `rdev skillkit install`, including dry-run-by-default reports, explicit `--execute` before local mutation, bundle verification before copy, target root refusal, generic target enforcement, conflict refusal unless `--force`, copied skills/reference files, and `external_mutation=false`.
- Hosted provider package generation and verification through `rdev hosted-provider package` and `rdev hosted-provider verify`, including schema checks, storage/auth provider declarations, gateway argument templates, environment declarations, provider-specific `rdev.hosted-provider-runtime-contract.v1` files for Postgres/S3-compatible/Redis/OIDC-JWKS/SAML, checksums/sizes, unlisted-file rejection, `external_mutation=false`, and no-private-surface checks. `postgres`, `redis-stream`, and `s3-compatible` plus `hosted-ed25519-jwt`, `oidc-jwks`, or `saml-assertion` use built-in `rdev gateway serve --storage-provider ...` runtime args; other external combinations remain reviewed runtime-contract packages until implemented.
- Built-in Postgres state-store runtime checks through `gateway.PostgresStateStore` and `rdev gateway storage verify --provider postgres`, covering `psql`/libpq schema bootstrap, JSONB snapshot upsert/load, runtime probe readback, cleanup, and rejection of inline passwords in connection info.
- Built-in Redis stream state-store runtime checks through `gateway.RedisStreamStateStore` and `rdev gateway storage verify --provider redis-stream`, covering `redis-cli` snapshot key load/save, stream append/probe events, runtime probe readback, cleanup, and rejection of inline credentials in Redis URLs.
- Built-in S3-compatible state-store runtime checks through `gateway.S3CompatibleStateStore` and `rdev gateway storage verify --provider s3-compatible`, covering `aws s3api` snapshot object put/get, runtime probe readback/delete, and rejection of credentials, query strings, and fragments in `s3://bucket/prefix` locations.
- Built-in OIDC/JWKS operator-auth runtime checks through `operatorauth.OIDCJWKSVerifier`, `rdev operator-auth verify-oidc-jwks`, and `rdev gateway serve --oidc-jwks-operator-auth`, covering RS256 JWKS fetch, JWT signature verification, issuer/audience/expiry/not-before/role checks, and rejection of unsafe JWKS URLs.
- Built-in SAML operator-auth runtime checks through `operatorauth.SAMLVerifier`, `rdev operator-auth verify-saml`, and `rdev gateway serve --saml-operator-auth`, covering signed SAMLResponse verification, IdP issuer, audience, assertion consumer recipient, time-condition warnings promoted to failures, SHA-256-or-better XML signature enforcement, certificate trust, subject mapping, role-attribute checks, and rejection of private key material in certificate config.
- Hosted provider runtime evidence planning, packaging, and verification through `rdev.hosted-provider-runtime-evidence-plan.v1`, `runtime-evidence-plan.json`, `rdev acceptance package-hosted-provider-runtime --evidence-dir`, and `rdev acceptance verify-hosted-provider-runtime-package`, covering verified hosted provider package archival, standard gateway startup/storage/auth/backup/restore/retention/role/failure/audit evidence file names, package/verify commands, storage verification, hosted auth verification, role-mapping probes with both authorized and denied decisions, failure-mode probes, audit JSONL archival, redaction, checksums, and no-private-surface checks. Built-in `file` plus `hosted-ed25519-jwt` evidence can satisfy a single-node smoke gate; complete external provider evidence verifies as `external-durable-hosted-runtime-evidence`, but durable production claims still require deployed provider evidence.
- Hosted provider and relay/connectivity evidence scaffolding and readiness status through `rdev.acceptance-evidence-scaffold.v1`, `rdev acceptance scaffold-evidence --hosted-provider-package`, `rdev acceptance scaffold-evidence --relay-adapter-package`, MCP tool `rdev.acceptance.scaffold_evidence`, `rdev.acceptance-evidence-status.v1`, `rdev acceptance evidence-status`, and MCP tool `rdev.acceptance.evidence_status`, covering package-level scaffold inputs, generated package runbooks, evidence-plan Agent rules, verifier checks for scaffold-first guidance, reviewed `--plan` overrides, standard checklist/report generation, and missing, empty, and placeholder evidence detection before any `rdev acceptance package-*` command can be treated as ready for real release evidence.
- Connectivity adapter package generation and verification through `rdev relay-adapter package` and `rdev relay-adapter verify`, including Chisel/frpc `RDEV_RELAY_*`, SSH tunnel `RDEV_SSH_*`, headscale/Tailscale-compatible mesh `RDEV_MESH_*`, and WireGuard `RDEV_VPN_*` adapter schemas, safe helper argv, safe dependency or manual-review install actions, checksums/sizes, unlisted-file rejection, `external_mutation=false`, and no-private-surface checks. The safe dependency path covers SHA-256 verified user/workspace-scoped `chisel`, `frpc`, `tailscale`, and `wg` helper binaries; SSH identities, mesh enrollment, VPN profiles, keys, routes, DNS, firewall, services, drivers, and privileged network changes remain outside automatic install and require authorization.
- Relay adapter acceptance evidence planning, packaging, and verification through `rdev.relay-adapter-acceptance-evidence-plan.v1`, `acceptance-evidence-plan.json`, `rdev acceptance package-relay-adapter --evidence-dir`, and `rdev acceptance verify-relay-adapter-package`, covering verified relay adapter package archival, standard `rdev connection-entry run --evidence-dir` runner capture, runner-generated helper transcript evidence, helper transcript redaction, runner-generated gateway/host/connection status archival, runner-generated audit JSONL archival, checksums, a standard connectivity adapter `selected_path` (`existing-frp-or-chisel-relay`, `existing-ssh-tunnel`, `existing-headscale-tailscale-mesh`, or `existing-wireguard-vpn`), and `connected=true`.
- Release candidate packaging through `rdev release prepare-candidate`, covering staged built artifacts, signed artifact manifests, signed release bundle verification, Skillkit export/verify, SPDX 2.3 SBOM, local provenance, checksums, `connection-entry-release.zip`, visible launchers that verify the packaged signed release bundle before packaged execution, and `rdev.release-candidate.v1` summary generation without publishing to GitHub or leaking local output paths.
- release candidate verification through `rdev release verify-candidate`, covering relocated candidates, summary schema, root public key, checksums, signed release bundle verification, required artifacts, Skillkit verification, SBOM coverage, provenance coverage, Connection Entry release archive schema/checksums/no-private-parameter policy, launcher `rdev-verify --bundle --root-public-key --require-artifacts` checks, listed file hashes/sizes, and unlisted-file rejection.
- real artifact build smoke through `scripts/release/build-artifacts.sh`, covering actual Go-built `rdev`, `rdev-host`, and `rdev-verify` binaries, `rdev.build-artifacts.v1`, SPDX 2.3 SBOM, `provenance.json`, checksums, and release smoke candidate packaging from real Windows artifacts rather than placeholder files.
- per-platform release candidate smoke through `scripts/release/prepare-platform-candidates.sh`, covering `rdev.build-artifacts.v1` target grouping, required command-to-artifact mapping for Unix and Windows suffixes, one verified candidate per target, `rdev.platform-release-candidates.v1`, and `external_mutation=false`.
- multi-platform GitHub Release dry-run planning through `scripts/github/plan-platform-release.sh`, covering per-platform candidate re-verification, unique Linux/Windows platform archives, `rdev.platform-release-index.v1`, `rdev.github-platform-release-verification.v1`, `INSTALL_PLATFORMS.md`, unique asset names, `gh release` command previews, and `external_mutation=false`.
- GitHub Release dry-run planning through `scripts/github/plan-release.sh`, covering candidate verification, generated release notes, concise asset plan, Skillkit tarball generation, commands preview, and `external_mutation=false`.
- post-release download acceptance packaging and verification through `rdev acceptance package-post-release-download` and `rdev acceptance verify-post-release-download-package`, covering `rdev.post-release-install-plan.v1`, `rdev.post-release-install-verification.v1`, per-platform download transcripts, per-platform `rdev release verify-candidate` outputs with `ok=true`, per-platform `rdev-verify --bundle` outputs with `ok=true`, Skillkit transcript/verification evidence when included, redaction, checksums, and no-private-surface checks. This gate archives real public download evidence after a GitHub Release exists; it does not publish release assets by itself.
- post-release download evidence scaffolding and readiness status through `rdev acceptance scaffold-post-release-download --post-release-install-dir`, `rdev acceptance post-release-evidence-status`, and MCP tools, covering directory-level post-release install inputs, reviewed explicit plan overrides, stable platform/Skillkit evidence directories, placeholder opt-in, missing/empty/placeholder detection, a fail-closed `ready_for_packaging=true` gate before `rdev acceptance package-post-release-download --scaffold <dir>`, plus package/verify rejection if platform, Skillkit, or post-release verification placeholder evidence reaches the archive path.
- combined release evidence indexing through `rdev acceptance release-evidence-index` and MCP tool `rdev.acceptance.release_evidence_index`, covering hosted provider runtime package verification, relay/connectivity package verification, post-release download package verification, package-manifest hashing without copying source-path-heavy package manifests, fail-closed missing-gate reports, and `release-evidence-index.json` plus `checksums.txt` output.
- GitHub Actions CI through `.github/workflows/ci.yml`, covering `./scripts/check.sh` and `./scripts/ci/release-smoke.sh`.
- structured host-side denial artifacts through `rdev.host-denial.v1`, covering missing envelope, wrong host, identity mismatch, expired/tampered envelopes, replayed nonce, unsupported adapter, missing capability, missing workspace, non-allowlisted command, and workspace escape.
- CLI host session execution reports host-side denials to the dev gateway as task result artifacts.
- development HTTPS long-poll session event transport through `GET /v1/sessions/{session_id}/events?after_seq=...` and `rdev host serve --transport long-poll`.
- development gateway TLS/mTLS pre-production transport proof through `rdev gateway serve --dev --tls-cert --tls-key --client-ca` plus `rdev host serve --gateway https://127.0.0.1:<port> --gateway-ca --gateway-client-cert --gateway-client-key`, proving local HTTPS session joins and task event calls work with a CA-signed client certificate and fail without one.
- structured session interrupt artifacts through `rdev.sessions.interrupt`; pause, cancel, and local consent are represented as replayable Control Plane events rather than a separate task authorization subsystem.
- shared preflight for built-in shell and Codex session tasks, covering package installation, elevation, GUI control, service management, push, merge, deploy, publish, and credential changes before adapter execution.
- durable interrupt/idempotency handling for repeated pause/cancel events, including replay after reconnect and duplicate idempotency-key suppression.
- macOS LaunchAgent plist generation through `rdev host install-service --platform macos`, including managed host arguments, `0600` plist permissions, and explicit launchctl next steps without auto-starting the service.
- macOS LaunchAgent plist status and safe uninstall through `rdev host service-status` and `rdev host uninstall-service`, including label-mismatch refusal to avoid deleting unrelated plists.
- Linux systemd user-unit generation through `rdev host install-service --platform linux`, including managed host arguments, `0600` unit permissions, basic hardening flags, explicit `systemctl --user` next steps, status inspection, dry-run service-control planning, unit mismatch refusal, and safe uninstall without auto-starting the service.
- Linux managed-service acceptance planning and verification through `rdev acceptance linux-managed-service` and `rdev acceptance verify-linux-managed-service`, covering written `0600` systemd user units, managed args, hardening flags, release-bundle gate requirements, required evidence checklist, forbidden policy-weakening command rejection, missing-unit rejection, and no `systemctl` execution.
- Linux managed-service acceptance evidence packaging through `rdev acceptance package-linux-managed-service`, covering verified plan and unit archival, plan-verifier output, redacted start/status/log/release-gate/audit/reconnect/stop/uninstall transcripts, managed task session evidence archival, interrupt-required proof, checksums, and release-blocking failure when release-gate output is not `ok=true`.
- Windows Service managed-host planning through `rdev host install-service --platform windows`, including managed host arguments, release-bundle gate arguments, `start= demand`, reviewed `sc.exe create` / `description` command plans, dry-run status/control/uninstall command planning, explicit `service-control --execute` before invoking `sc.exe`, and no claim of real Windows Service acceptance yet.
- Windows managed-service acceptance planning and verification through `rdev acceptance windows-managed-service` and `rdev acceptance verify-windows-managed-service`, covering reviewed `sc.exe` create/description/query/qc/start/stop/delete plans, managed args, `start= demand`, release-bundle gate requirements, required evidence checklist, forbidden policy-weakening command rejection, and no PowerShell or `sc.exe` execution.
- workspace lock and Git worktree preparation foundation through `rdev.workspace-lock.v1`, `rdev.git-worktree-plan.v1`, `rdev workspace lock/status/unlock`, and `rdev workspace prepare-worktree`, including one-writer rejection, expired-lock replacement, owner-checked unlock, lock cleanup on failed worktree creation, and real `git worktree add` coverage.
- workspace lock enforcement during host execution through `rdev host serve --workspace-lock-store`, including hostrunner lock acquire/release, `workspace_locked` structured denial artifacts, lock release after adapter denial, and managed LaunchAgent `--workspace-lock-store` argument generation.
- managed Mac coding harness through `rdev acceptance managed-mac`, covering managed endpoint registration, Git worktree creation, Codex adapter execution with workspace locking, diff/test evidence export, interrupt-required probe for `git.push`, workspace lock release, and session evidence creation.
- acceptance report verification through `rdev acceptance verify`, covering report checks, coding and interrupt session evidence checksums, artifact index consistency, audit-chain verification, interrupt-required probe evidence, and workspace-lock release.
- managed Mac LaunchAgent acceptance planning and verification through `rdev acceptance managed-mac-service` and `rdev acceptance verify-managed-mac-service`, covering checked plist generation, managed host arguments, release-bundle gate requirements, `rdev host service-control --execute` command plan, service-backed acceptance command plan, evidence verification command plan, forbidden policy-weakening command rejection, and uninstall guidance without auto-starting launchd.
- managed Mac LaunchAgent acceptance evidence packaging through `rdev acceptance package-managed-mac-service`, covering verified plan and plist archival, plan-verifier output, redacted review/start/inspect/log/release-gate/audit/reconnect/stop/uninstall transcripts, verified managed Mac report/session evidence archival, interrupt-required proof, checksums, and release-blocking failure when release-gate output is not `ok=true` or the managed Mac report fails verification.
- macOS LaunchAgent lifecycle control through `rdev host service-control`, covering dry-run by default, explicit `--execute` for launchctl, plist label checks, and start/inspect/stop action planning.
- Windows layered-entry acceptance planning and verification through `rdev acceptance windows-temporary` and `rdev acceptance verify-windows-temporary`, covering the real handoff ZIP digest/size gate, bounded full-entry integrity reads, protected Windows DACL/0600 output, required archive entries, PowerShell-first fallback order, bootstrap-only launchers, interrupt probes, no-persistence commands, and required evidence without executing the launcher.
- Windows layered-entry evidence packaging through `rdev acceptance package-windows-temporary`, covering the non-sensitive plan, protected output files, redacted transcript and verifier output, strict cold/warm layered reports, audit capture, interrupt-probe and no-persistence evidence, checksums, and exclusion of the private handoff ZIP.
- release bundle verification through `rdev release create-bundle`, `rdev release verify-bundle`, and standalone `rdev-verify --bundle`, covering signed bundle index verification, per-artifact signed manifest verification, artifact and manifest SHA-256/size checks, required artifact presence, and tamper failure evidence.
- shell adapter cooperative cancellation through `ExecuteContext` and hostrunner context propagation, returning `rdev.shell-result.v1` artifacts with `canceled=true` instead of timeout evidence.
- PowerShell adapter MVP through `adapter=powershell`, covering `powershell.user` capability enforcement, allowlisted PowerShell executable execution, no `-ExecutionPolicy Bypass`, `rdev.powershell-result.v1` evidence, redaction, workspace-lock release, interrupt-required service-management detection, and context cancellation.
- canceled-task artifact reporting for built-in shell, PowerShell, Codex, Claude Code, and acpx adapters, preserving the Control Plane task's `canceled` terminal state while adding reviewable cancellation evidence.
- public adapterkit lifecycle-manifest, runtime-fixture, result-artifact, and cancellation-artifact conformance through `pkg/adapterkit`, `adapterkit.RunLifecycle`, `rdev adapter scaffold`, `rdev adapter verify-lifecycle`, `rdev adapter verify-runtime`, `rdev adapter verify-result`, `rdev adapter verify-cancellation`, and MCP tools `rdev.adapter.verify_lifecycle` / `rdev.adapter.verify_runtime` / `rdev.adapter.verify_result` / `rdev.adapter.verify_cancellation`, covering generated adapter templates, executable adapter phase fixtures, adapter phases, safety boundaries, cancellation, cleanup, result schemas, and built-in shell, PowerShell, Codex, Claude Code, and acpx artifacts for schema, timing, redaction metadata, command evidence, canceled-vs-timeout proof, and secret-pattern rejection.
- hostrunner runtime fixture capture through `rdev host serve --capture-runtime-fixture`, including primary adapter result preservation plus appended `rdev.adapter-runtime-fixture.v1` artifacts for shell, PowerShell, Codex, Claude Code, acpx, and canceled shell cleanup.

## Next Automation Targets

- Real managed Mac LaunchAgent acceptance execution: review generated plan, start/inspect/stop with `rdev host service-control --execute`, reconnect after reboot, run locked-worktree Codex, verify evidence, package the run with `rdev acceptance package-managed-mac-service`, and uninstall.
- Windows layered-entry acceptance execution: verify the delivered ZIP, run the visible PowerShell/CMD paths on Windows 10/11, exercise Range resume, route reselection, ACL/reparse and Defender-lock behavior, then collect cleanup, no-persistence, denial, and revocation evidence.
- Release transcript packaging: publish redacted acceptance command transcript, verified report JSON, Windows temporary evidence package when applicable, and session evidence checksums for each release candidate.
- Windows Service managed-host acceptance execution: run the verified reviewed `sc.exe` plan on a clean Windows machine, verify service creation/status/start/stop/delete, confirm release-bundle startup gate, reconnect behavior, local stop/uninstall, and absence of temporary-mode persistence leakage.
- Restrictive-network relay acceptance execution: run a verified Chisel/frpc, SSH tunnel, headscale/Tailscale-compatible mesh, or WireGuard connectivity adapter package against a deployed or already configured route on clean Windows/macOS/Linux targets, capture the Connection Entry runner evidence directory, then package it with `rdev acceptance package-relay-adapter --evidence-dir` and verify it with `rdev acceptance verify-relay-adapter-package`.
- Hosted provider runtime acceptance execution: run a hosted gateway with a verified provider package, capture the standard hosted runtime evidence directory with gateway startup, `rdev gateway storage verify`, `rdev operator-auth verify-hosted`, `rdev operator-auth verify-oidc-jwks`, `rdev operator-auth verify-saml`, or equivalent hosted auth verification, backup, restore, retention, role-mapping authorization probes, failure-mode probes, and audit JSONL, then package it with `rdev acceptance package-hosted-provider-runtime --evidence-dir` and verify it with `rdev acceptance verify-hosted-provider-runtime-package`.
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
