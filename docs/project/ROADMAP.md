# Roadmap

The roadmap implements the canonical [Perfect Ending Solution](../architecture/PERFECT_ENDING_SOLUTION.md): consent-first enrollment, outbound-only transport, signed job envelopes, host-side policy enforcement, workspace locks, approval gates, audit evidence, release-bundle verification, and agent-native MCP/Skill packaging. Future architecture changes should patch that decision layer instead of adding another dated "final" layer. The compressed end state is one signed control protocol, two host products, separated release/bootstrap/gateway/host trust authorities, explicit join/run/approve/prove paths, and replayable proof packages. [Final Closure Blueprint](../architecture/FINAL_CLOSURE_BLUEPRINT.md) is the concise release-facing summary, while [Ultimate Closure Design](../architecture/ULTIMATE_CLOSURE_DESIGN.md), [Final System Design](../architecture/FINAL_SYSTEM_DESIGN.md), and [Perfect End-State Architecture](../architecture/PERFECT_END_STATE.md) remain supporting context.

## Maturity Gates

| Gate | Proves | Does not claim |
|---|---|---|
| `v0.1` Local Safety Kernel | signed jobs, host-side verification, denials, approvals, evidence, audit | production networking or OS service behavior |
| `v0.2` Windows Temporary Host | visible one-command enrollment, release verification, outbound-only repair | unattended managed-device operations |
| `v0.3` Managed Mac Coding | durable owned-host Codex workflow with workspace evidence | general multi-tenant fleet management |
| `v0.4` Managed Device Generalization | multi-OS services, durable storage, adapter SDK, reconnects | public protocol stability |
| `v1.0` Public Skillkit | stable schemas, safe defaults, signed releases, self-host docs | every possible adapter or remote-access product |

## v0.0.1 Foundation

- Repository skeleton.
- CLI skeleton.
- Doctor command.
- MCP tool contract listing.
- Agent Skills drafts.
- Architecture and security docs.

## v0.1.0 Local Safety Kernel

- Local gateway process.
- Development gateway state snapshot restore for tickets, hosts, jobs, artifacts, audit events, and trust bundles when started with a persistent signing key.
- Local host process.
- In-memory tickets, hosts, jobs, artifacts, and audit events.
- MCP stdio server for tool calls.
- Development signed job envelopes.
- Demonstrable local temporary session.
- Development HTTPS long-poll host job transport.
- Production WSS host job transport through `rdev host serve --transport wss`
  and `GET /v1/ws/hosts/{host_id}`, including completion/failure/artifact
  acknowledgements and mTLS client certificate reuse.
- Development gateway TLS/mTLS listener through `rdev gateway serve --dev --tls-cert --tls-key [--client-ca]`, requiring client certificates when a client CA is configured while preserving signed-envelope and host-local authorization semantics.
- Host-side local dev gateway HTTPS/mTLS client support through `rdev host serve --gateway-ca [--gateway-client-cert --gateway-client-key]`, covering join manifests, registration, trust refresh, polling, completion, failure, and artifact appends over local HTTPS/mTLS.
- Scoped shell adapter with workspace boundary, allowlisted argv, timeouts, cooperative cancellation, output caps, and failure reporting.
- Host identity storage wired into registration and job binding.
- Durable host trust bundle store.
- Signed host registration proofs for identity-bearing registrations.
- Host enrollment certificate primitive through `rdev.host-enrollment-certificate.v1`, `rdev enrollment sign-certificate`, `rdev enrollment verify-certificate`, `rdev host serve --enrollment-certificate`, and `rdev gateway serve --dev --enrollment-root-public-key`, binding ticket code, mode, host metadata, capabilities, identity fingerprint, validity window, and enrollment root signature before registration when configured.
- Local operator auth foundation through `rdev.operator-auth.v1`, `rdev operator-auth init`, `rdev operator-auth verify`, and `rdev gateway serve --dev --operator-auth`, with hashed bearer-token storage and `admin` / `operator` / `issuer` / `auditor` role gates for control-plane and enrollment endpoints.
- Hosted operator auth foundation through `rdev.hosted-operator-auth.v1`,
  `rdev operator-auth verify-hosted`, and `rdev gateway serve --dev
  --hosted-operator-auth`, with generic EdDSA JWT issuer/audience/key/role
  validation and no provider-specific hardcoded domains.
- Gateway state-store provider boundary through `--storage-provider`,
  `--storage-path`, and `rdev gateway storage verify`, with the built-in file
  provider preserving `rdev.gateway-snapshot.v1`.
- Built-in Postgres gateway state-store runtime through
  `--storage-provider postgres`, `psql`/libpq schema bootstrap, JSONB snapshot
  upsert/load, runtime probe verification, inline-password rejection for
  connection info, and hosted provider package gateway args for
  `postgres` + `hosted-ed25519-jwt`.
- Built-in Redis stream gateway state-store runtime through
  `--storage-provider redis-stream`, `redis-cli` snapshot key load/save,
  Redis stream append/probe events, inline-credential rejection for Redis URLs,
  runtime probe verification, and hosted provider package gateway args for
  `redis-stream` + `hosted-ed25519-jwt`.
- Hosted provider package generation and verification through
  `rdev.hosted-provider-package.v1`, `rdev hosted-provider package`,
  `rdev.hosted-provider-package-verification.v1`, and
  `rdev hosted-provider verify`, producing reviewable storage/auth deployment
  metadata, gateway args, env templates, checksums, and no-private-surface
  evidence without bundling credentials or private endpoints.
- Provider-specific hosted runtime contracts through
  `rdev.hosted-provider-runtime-contract.v1`, `runtime-contract.json`, and
  `HOSTED_PROVIDER_RUNTIME.md`, covering Postgres, S3-compatible object
  storage, Redis streams, OIDC/JWKS, and SAML runtime evidence requirements
  for verification, backup, restore, retention, role mapping, failure-mode
  probes, audit, operator approval, and unsupported production claims.
- Relay adapter package generation and verification through
  `rdev.relay-adapter-package.v1`, `rdev relay-adapter package`,
  `rdev.relay-adapter-package-verification.v1`, `rdev relay-adapter verify`,
  and MCP tools `rdev.relay_adapter.package` / `rdev.relay_adapter.verify`,
  producing Chisel/frpc `RDEV_RELAY_*`, SSH tunnel `RDEV_SSH_*`,
  headscale/Tailscale-compatible `RDEV_MESH_*`, and WireGuard `RDEV_VPN_*`
  runner metadata, safe helper argv, reviewed dependency or manual-review
  install actions, approval boundaries, checksums, and no-private-surface
  evidence without bundling relay endpoints, SSH identities, mesh auth keys,
  WireGuard keys, credentials, private IPs, or local paths.
- Relay adapter acceptance packaging and verification through
  `rdev.acceptance-package.relay-adapter.v1`,
  `rdev acceptance package-relay-adapter`,
  `rdev.acceptance-verification.relay-adapter-package.v1`, and
  `rdev acceptance verify-relay-adapter-package`, archiving real runner,
  helper, gateway, host, connection-status, audit, redaction, and checksum
  evidence while requiring a standard connectivity adapter `selected_path`
  (`existing-frp-or-chisel-relay`, `existing-ssh-tunnel`,
  `existing-headscale-tailscale-mesh`, or `existing-wireguard-vpn`) and
  `connected=true`.
- Hosted provider runtime acceptance packaging and verification through
  `rdev.acceptance-package.hosted-provider-runtime.v1`,
  `rdev acceptance package-hosted-provider-runtime`,
  `rdev.acceptance-verification.hosted-provider-runtime-package.v1`, and
  `rdev acceptance verify-hosted-provider-runtime-package`, archiving a
  verified hosted-provider package, gateway startup transcript, storage/auth
  verification, backup, restore, retention, role-mapping authorization probes,
  failure-mode probes, audit, redaction, and checksums. Built-in `file` plus
  `hosted-ed25519-jwt` evidence remains scoped as a single-node hosted smoke;
  external durable provider support still requires deployed provider evidence.
- Dev hosted enrollment issuance primitive through `POST /v1/enrollment/certificates`, `rdev gateway serve --dev --enrollment-key`, and `rdev enrollment issue-certificate --gateway ... --root-public-key ...`, issuing pinned-root-verified certificates from a configured gateway issuer while preventing requested certificate capabilities from exceeding the ticket capabilities.
- Operator-auth protection for dev hosted enrollment issuance through `rdev gateway serve --dev --operator-auth` and `rdev enrollment issue-certificate --operator-token-file`, keeping tokens out of command output while requiring an `issuer` role token.
- Local enrollment certificate renewal primitive through `rdev enrollment renew-certificate`, preserving the existing certificate scope, requiring the current certificate to verify, optionally checking signed revocation lists before renewal, and emitting a new certificate fingerprint and validity window.
- Dev hosted enrollment renewal primitive through `POST /v1/enrollment/certificates/renew` and `rdev enrollment renew-certificate --gateway ... --root-public-key ...`, preserving certificate scope and requiring pinned-root, previous-fingerprint, renewed-signature, and renewed-fingerprint verification before local write.
- Host-side hosted enrollment renewal before registration through `rdev host serve --renew-enrollment-certificate --enrollment-root-public-key`, using the gateway/mTLS-aware host client, optional `--operator-token-file`, near-expiry threshold checks, pinned-root hosted renewal verification, certificate-file replacement, and gateway rejection when the current certificate is listed in signed revocations.
- Signed enrollment certificate revocation-list primitive through `rdev.host-enrollment-revocations.v1`, `rdev enrollment init-revocations`, `rdev enrollment revoke-certificate`, `rdev enrollment verify-revocations`, `rdev enrollment verify-certificate --revocations`, and `rdev gateway serve --dev --enrollment-revocations`, publishing a signed empty baseline before any revocation exists and rejecting revoked certificates before registration when configured.
- Dev enrollment revocation-list distribution through `GET /v1/enrollment/revocations` and `rdev enrollment fetch-revocations`, requiring pinned enrollment-root verification before writing the fetched list to disk.
- Operator-auth protection for dev hosted enrollment revocation refresh, using `--operator-auth` on the gateway and `--operator-token-file` on CLI and host fetch paths.
- Host-side hosted enrollment revocation refresh through `rdev host serve --fetch-enrollment-revocations --enrollment-root-public-key`, requiring pinned enrollment-root verification of the signed gateway list and local certificate before registration, then refusing locally revoked certificates before sending the registration payload.
- Agent-side enrollment certificate verification through MCP tool `rdev.enrollment.verify_certificate`, returning `rdev.enrollment-certificate-verification.v1` reports so Skillkit workflows can reject missing, expired, wrong-root, tampered, stale-revocation-list, or revoked certificates before trusting a host registration.
- Enrollment authority lifecycle evidence through `rdev enrollment lifecycle
  key-custody`, `fleet-renewal-plan`, and `emergency-drill`, producing
  `rdev.enrollment-key-custody.v1`,
  `rdev.enrollment-fleet-renewal-plan.v1`, and
  `rdev.enrollment-emergency-drill.v1`.
- Trust-bundle key rotation/revocation flow.
- Trust lifecycle operator workflow through `rdev trust init`, `rdev trust rotate`, `rdev trust revoke`, and `rdev trust verify`, producing signed trust bundles with sequence, previous-hash, key rotation, key retirement, revocation, and pinned-root verification.
- Host-bound trust bundle update checks for managed host refresh.
- Host-side artifact redaction.
- Hash-chained audit export verifier.
- Evidence bundle export.
- Gateway/API evidence bundle export directly from job ids.
- Skillkit bundle export for agent runtimes and mainstream framework notes.
- Skillkit bundle verification for required skills, required framework notes, file checksums, safe paths, and unlisted-file detection.
- Skillkit adaptive configuration contract through `rdev.adaptive-configuration-contract.v1`, requiring agents to probe `rdev`, MCP tools, OS/shell, service manager, gateway, workspace, adapters, framework paths, and permissions before acting, and to ask when configuration is unclear instead of inventing values.
- Skillkit framework install planning and verification through `rdev skillkit plan-install` and `rdev skillkit verify-install-plan`, generating reviewed shell/PowerShell scripts for Codex, Claude Code, Hermes, OpenClaw, OpenCode, and generic MCP agents without external mutation or hidden config writes.
- Skillkit direct install through `rdev skillkit install`, dry-running by default, requiring `--execute` before local copy, refusing existing skill conflicts unless `--force`, and preserving `external_mutation=false`.
- Release candidate packaging that stages built artifacts, signed manifests, signed release bundle, verified Skillkit bundle, SPDX 2.3 SBOM, local provenance attestation, checksums, verifiable `connection-entry-release.zip` archives, and `release-candidate.json` with package-relative public paths.
- Release candidate verification that checks staged or downloaded candidates, including SBOM coverage/hash consistency, provenance subject/hash consistency, Connection Entry release archive schema/checksums/no-private-parameter policy, and no leaked local candidate paths before publication or installation.
- Host startup release gates through `rdev host serve --release-bundle --release-root-public-key --release-require-artifacts`, verifying signed release bundles before host registration or job polling.
- Real build artifact generation through `scripts/release/build-artifacts.sh`, producing target-specific binaries, `rdev.build-artifacts.v1`, per-artifact `cgo_enabled` metadata, SPDX 2.3 SBOM, `provenance.json`, and checksums before candidate packaging.
- Per-platform release candidate automation through `scripts/release/prepare-platform-candidates.sh`, grouping real build artifacts by `GOOS/GOARCH`, producing one verified candidate per target, and writing `rdev.platform-release-candidates.v1`.
- Multi-platform GitHub Release dry-run planning through `scripts/github/plan-platform-release.sh`, producing unique platform archives, `rdev.platform-release-index.v1`, `rdev.github-platform-release-verification.v1`, `INSTALL_PLATFORMS.md`, and command previews without external mutation.
- GitHub project readiness auditing through `scripts/github/audit-project-readiness.sh`, producing `rdev.github-project-readiness.v1` from local docs, templates, CI, release scripts, and `bootstrap-project.sh --dry-run` without external mutation.
- Post-release install verification planning through `scripts/github/plan-post-release-install.sh`, producing `rdev.post-release-install-plan.v1`, `VERIFY_INSTALL.md`, generated platform verification scripts, and Skillkit verification commands from a local GitHub Release dry-run plan without external mutation.
- Post-release install plan verification through `scripts/github/verify-post-release-install-plan.sh`, checking the generated plan and scripts before they are archived as release evidence and rejecting tampered verification scripts in CI smoke.
- Post-release download acceptance packaging and verification through
  `rdev.acceptance-package.post-release-download.v1`,
  `rdev acceptance package-post-release-download`,
  `rdev.acceptance-verification.post-release-download-package.v1`, and
  `rdev acceptance verify-post-release-download-package`, archiving real
  published-release download transcripts, per-platform candidate verification,
  per-platform signed bundle verification, Skillkit verification, redaction,
  checksums, and no-private-surface evidence after a GitHub Release exists.
- Public adapter onboarding and conformance through `pkg/adapterkit`, `adapterkit.RunLifecycle`, `rdev adapter scaffold`, `rdev adapter verify-result`, `rdev adapter verify-lifecycle`, `rdev adapter verify-cancellation`, `rdev adapter verify-runtime`, and MCP tools `rdev.adapter.verify_result` / `rdev.adapter.verify_lifecycle` / `rdev.adapter.verify_cancellation` / `rdev.adapter.verify_runtime`, with generated lifecycle manifest templates, runtime lifecycle fixtures, lifecycle checks for required phases, safety boundaries, cancellation, cleanup, and result schemas, and built-in shell, PowerShell, Codex, Claude Code, and acpx result/cancellation tests checking schema, timing, redaction metadata, command evidence, canceled-vs-timeout proof, and secret-pattern rejection.
- Hostrunner-integrated runtime fixture capture for built-in shell, PowerShell, Codex, Claude Code, and acpx adapters through `rdev host serve --capture-runtime-fixture`, preserving primary adapter result artifacts while appending `rdev.adapter-runtime-fixture.v1` evidence for completed, failed, or canceled jobs.
- Strong symlink/workspace escape regression tests.
- Explainable denial and approval decisions.
- Final operational architecture index covering topology, permanent separations, mode contracts, trust keys, protocol spine, permission lattice, host sovereignty, adapter contract, storage/transport, reliability, acceptance gates, and implementation spine.

Exit gate: local demo proves ticket, host registration, outbound host job wait through short-poll or long-poll, signed job execution, artifact storage, evidence export, Skillkit export/verify, release candidate package generation and verification, audit export, approval-token consumption, and host-side rejection of tampered, expired, wrong-host, replayed, non-allowlisted, and workspace-escaping jobs.

## v0.2.0 Windows Temporary Host

- Signed Windows binary.
- PowerShell bootstrap.
- PowerShell adapter MVP with `powershell.user` capability, allowlisted executable execution, no execution-policy bypass, approval preflight, redacted `rdev.powershell-result.v1` evidence, and cooperative cancellation.
- Windows temporary acceptance planning and verification through `rdev acceptance windows-temporary` and `rdev acceptance verify-windows-temporary`, including reviewed launcher generation, signed release manifest or release bundle verification requirements, launcher safety checks, approval probes, no-persistence inspection commands, and required evidence checklist without executing PowerShell.
- Windows temporary acceptance evidence packaging through `rdev acceptance package-windows-temporary`, turning real clean-VM transcripts, release verifier output, audit, approval probes, and no-persistence checks into a redacted checksummed release artifact.
- Visible foreground support window or console UI.
- Outbound-only connection to gateway.
- Durable signing key storage and rotation.
- Shell and file scoped jobs.
- Host local audit spool and revocation handling.
- Authenticode verification policy.
- Signed release bundle index creation and verification through `rdev release create-bundle` and `rdev release verify-bundle`.
- No-persistence inspection script.
- Local-user approval prompt for elevation, GUI, service, and destructive requests.
- Clean Windows acceptance run.

Exit gate: clean Windows 10/11 VM joins from one visible command, verifies signed artifacts before host execution, connects outbound only, enforces approvals, revokes cleanly, leaves no service or autorun persistence, and exports a passing Windows temporary acceptance package.

## v0.3.0 Managed Mac Coding

- macOS LaunchAgent managed mode.
- LaunchAgent plist generation with explicit launchctl next steps.
- LaunchAgent plist status and safe uninstall commands.
- macOS Keychain protected-store references for managed host identity and trust bundle persistence.
- Workspace lock manager and Git worktree preparation foundation.
- Workspace locks wired into hostrunner, host serve, and managed LaunchAgent arguments.
- Codex adapter MVP with hostrunner integration, `codex.run` and `git.diff` capability checks, locked-workspace execution, implicit approval preflight for high-risk external consequences, Git diff/status evidence, optional verification command evidence, `go test -json` report parsing, output caps, and redaction.
- Codex adapter conformance coverage for workspace canonicalization, write-scope escape rejection before execution, nonzero-exit evidence, host-side redaction, output truncation, and timeout cancellation evidence.
- Claude Code adapter MVP with hostrunner integration, `claude-code.run` and `git.diff` capability checks, locked-workspace execution, implicit approval preflight for high-risk external consequences, Git diff/status evidence, optional verification command evidence, `go test -json` report parsing, output caps, redaction, and runtime fixture capture.
- ACP/acpx adapter MVP with hostrunner integration, `acpx.run` and `git.diff` capability checks, default `acpx --cwd <workspace> codex exec <prompt>` execution, signed payload overrides for `acpx_command` / `acpx_agent` / `acpx_args`, locked-workspace execution, implicit approval preflight, Git diff/status evidence, optional verification command evidence, `go test -json` report parsing, output caps, redaction, cooperative cancellation, and runtime fixture capture.
- Codex adapter cooperative cancellation through `ExecuteContext`, context-aware hostrunner execution, and host-side polling of gateway job cancellation state.
- Claude Code adapter cooperative cancellation through `ExecuteContext`, context-aware hostrunner execution, and host-side polling of gateway job cancellation state.
- acpx adapter cooperative cancellation through `ExecuteContext`, context-aware hostrunner execution, and host-side polling of gateway job cancellation state.
- Shell adapter cooperative cancellation through `ExecuteContext`, context-aware hostrunner execution, and `rdev.shell-result.v1` artifacts with explicit `canceled` state.
- PowerShell adapter cooperative cancellation through context-aware hostrunner execution and `rdev.powershell-result.v1` artifacts with explicit `canceled` state.
- Canceled shell, PowerShell, Codex, Claude Code, and acpx jobs append cancellation evidence artifacts without changing the gateway job's `canceled` terminal state.
- Managed Mac coding acceptance harness through `rdev acceptance managed-mac`, producing a managed-mode report, locked-worktree Codex evidence bundle, and approval-gate evidence bundle.
- Acceptance report verification through `rdev acceptance verify`, including evidence bundle checksum validation, artifact index validation, audit-chain verification, approval-gate checks, and workspace-lock release checks.
- Managed Mac LaunchAgent acceptance planning and verification through `rdev acceptance managed-mac-service` and `rdev acceptance verify-managed-mac-service`, producing a verified plist, release-bundle startup gate, launchctl start/inspect/stop commands, service-backed coding acceptance commands, verification command, and safe uninstall guidance without auto-starting launchd.
- Managed Mac LaunchAgent acceptance evidence packaging through `rdev acceptance package-managed-mac-service`, turning real LaunchAgent start/inspect/log/release-gate/audit/reconnect/stop/uninstall transcripts plus a verified managed Mac report/evidence bundle into a redacted checksummed release artifact.
- macOS LaunchAgent lifecycle control through `rdev host service-control`, dry-running by default and requiring `--execute` before invoking launchctl start, inspect, or stop.
- Managed macOS LaunchAgent and Linux systemd service definitions can carry the host startup release-bundle gate, so service restarts verify signed release artifacts before registration.
- Git diff and test evidence bundles.
- Shared implicit approval preflight before package install, elevation, GUI control, service changes, push, merge, deploy, publish, or credential changes for built-in shell, Codex, Claude Code, and acpx jobs.
- Managed install, health, stop, and uninstall commands.

Exit gate: an operator's managed Mac reconnects after reboot, an agent selects it through MCP, Codex runs in a locked worktree, and the result includes diff, tests, artifacts, audit slice, and residual risk.

## v0.4.0 Managed Device Generalization

- Windows Service managed-host planning through `rdev host install-service --platform windows`, status command planning, dry-run `service-control`, and uninstall command planning, emitting reviewed `sc.exe` create/query/qc/start/stop/delete commands without auto-installing or auto-starting a service.
- Windows managed-service acceptance planning and verification through `rdev acceptance windows-managed-service` and `rdev acceptance verify-windows-managed-service`, emitting a machine-readable checked plan with reviewed `sc.exe` create/description/query/qc/start/stop/delete commands, managed host args, `start= demand`, release-bundle startup gate, required evidence checklist, and no PowerShell or `sc.exe` execution.
- Linux systemd user-unit generation, status inspection, dry-run/opt-in lifecycle control, and safe uninstall.
- Linux managed-service acceptance planning and verification through `rdev acceptance linux-managed-service` and `rdev acceptance verify-linux-managed-service`, emitting a machine-readable checked plan with a written `0600` systemd user unit, reviewed `systemctl --user daemon-reload/enable --now/status/disable --now` commands, managed host args, hardening flags, release-bundle startup gate, required evidence checklist, and no `systemctl` execution.
- Linux managed-service acceptance evidence packaging through `rdev acceptance package-linux-managed-service`, turning real systemd user-service transcripts, release-gate output, audit, reconnect proof, and managed job evidence into a redacted checksummed release artifact.
- Real Linux systemd reboot/reconnect acceptance proof.
- launchd support.
- Restart/recovery.
- Auto-update/rollback.
- Full production adapter SDK beyond the first runtime lifecycle runner, built-in hostrunner runtime fixture capture, and lifecycle/result/cancellation/runtime-fixture conformance.
- Windows DPAPI protected-store references for managed host identity/trust persistence, preserving the same host fingerprint, trust sequence, rollback rejection, and host-bound update semantics as file-backed and macOS Keychain stores.
- Linux libsecret protected-store references for managed host identity/trust persistence on hosts with `secret-tool` and a reachable Secret Service.
- Linux keyctl protected-store references for headless managed host identity/trust runtime storage on hosts with a user keyring; real reboot persistence/reconnect proof remains a separate acceptance gate.
- Hardware-backed or fleet-managed protected stores beyond the current macOS Keychain, Windows DPAPI, Linux libsecret, Linux keyctl, and file-backed paths.
- Optional third-party hosted storage/auth provider runtime integrations beyond
  the built-in file provider, provider-neutral hosted JWT verifier, and
  provider-package verification surface.
- ACP/acpx adapter MVP.
- Artifact streaming.
- Durable third-party hosted storage/auth runtime integrations beyond the
  built-in Postgres and Redis stream state-store paths, current
  provider-specific runtime contracts, and hosted runtime evidence packager,
  including deployed Postgres/Redis backup or replay/restore, retention,
  role-mapping, and failure-mode evidence plus S3-compatible/OIDC/SAML gateway
  operation evidence.

Exit gate: one gateway manages multiple Mac/Windows/Linux hosts, trust rotation reaches managed hosts, audit/artifact spools survive reconnect, Windows Service has real install/start/reconnect/stop/uninstall acceptance evidence beyond dry-run plans, and a new adapter can be added without bypassing policy.

## v0.5.0 Mesh and GUI

- Tailscale/headscale adapter.
- RustDesk/MeshCentral optional GUI adapters.
- GUI consent/audit.
- Browser E2E adapter.

## v1.0.0

- Stable MCP contract.
- Stable host enrollment protocol.
- Stable `rdev skillkit export`, `rdev skillkit verify`, `rdev skillkit plan-install`, `rdev skillkit verify-install-plan`, and dry-run-by-default `rdev skillkit install` package path for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.
- Full audit trail.
- Signed releases.
- Local release candidate packaging before GitHub Release publication.
- Local GitHub Release dry-run planning from verified release candidates.
- GitHub Actions CI for tests, shell syntax, real build artifact smoke, per-platform release candidate verification, multi-platform release-plan smoke, and no-mutation command previews.
- Local GitHub project readiness audit for labels, milestones, seed issues, issue templates, PR template, CI, and release planning scripts.
- Installation docs for Hermes, Codex, Claude Code, and OpenCode.
- End-to-end acceptance demos for temporary Windows repair and managed coding.

Exit gate: external users can self-host or install a verified Skillkit without Hermes-specific assumptions, release artifacts verify, and the public threat model/security policy match the shipped behavior.
