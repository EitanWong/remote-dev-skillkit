# Task Board

## Now

- [x] Create repository skeleton.
- [x] Add architecture, roadmap, versioning, and threat model docs.
- [x] Add final architecture freeze document.
- [x] Add initial `rdev` CLI skeleton.
- [x] Add `rdev doctor`.
- [x] Add MCP tool contracts.
- [x] Add Agent Skills drafts.
- [x] Add local preview ticket creation.
- [x] Add policy explanation command.
- [x] Add real MCP stdio server.
- [x] Add in-memory gateway for tickets, hosts, jobs, artifacts, and audit.
- [x] Add local demo flow for ticket-host-job-audit.
- [x] Add foreground temporary host local loop.
- [x] Add JSONL audit store.
- [x] Add local HTTP development gateway.
- [x] Add signed job envelope model.
- [x] Add foreground temporary host local registration loop.
- [x] Add local dev job polling and completion loop.
- [x] Add host-side dev envelope/policy checks.
- [x] Add dev trust bundle endpoint and host-side envelope signature verification.
- [x] Add host-reported job failure endpoint and audit event.
- [x] Add perfect end-state architecture blueprint.
- [x] Refine final system design for implementation, deployment, and open-source packaging.
- [x] Lock final system design state machines, trust protocols, discovery model, capability vocabulary, adapter contract, and release gates.
- [x] Add persistent dev gateway signing key and host trust pin checks.
- [x] Add signed dev join manifest endpoint and manifest-driven host registration.
- [x] Add initial release/bootstrap trust root verification for join manifests.
- [x] Add release artifact signature manifest and CLI verifier.
- [x] Wire release artifact signature verification into Windows bootstrap via hash-pinned verifier.
- [x] Add shell job policy explain engine and MCP tool.
- [x] Make host revocation cancel queued/running jobs and audit the stop.
- [x] Add host identity key storage and signed job identity binding.
- [x] Add host-side nonce replay cache.
- [x] Add hash-chained audit export verifier.
- [x] Reconcile final architecture into one canonical product contract.
- [x] Add structured host-side denial explanations for failed job validation.
- [x] Add structured host-side approval-required results for gated jobs.
- [x] Add signed approval token model for gateway-approved job operations.
- [x] Add durable host-side approval token consumption store.
- [x] Finalize endgame operating model, authority separation, maturity gates, and failure conditions in the canonical design.
- [x] Add final endgame solution layer with architecture scorecard, operating modes, authority map, golden paths, and v1.0 release definition.
- [x] Lock final refined architecture as a safety microkernel with replaceable adapters, runtime loops, scheduler rules, temporary/managed host boundaries, storage model, and release contract.
- [x] Add final reference blueprint that makes the endgame architecture a single implementation contract.
- [x] Add concise final closure blueprint for release-facing architecture decisions.
- [x] Add development HTTPS long-poll host job transport prototype.
- [x] Add portable agent Skillkit bundle export for mainstream frameworks.

## Next

- [x] Define ticket data model.
- [x] Define host data model.
- [x] Define signed job envelope.
- [x] Define job/artifact/audit models.
- [x] Implement JSONL audit store.
- [x] Implement local HTTP gateway for development.
- [x] Add PowerShell bootstrap draft.
- [x] Add Windows capability detector.
- [x] Add dev gateway job create/status/claim/complete endpoints.
- [x] Add policy explain engine.
- [ ] Add production key rotation/revocation and host trust update flow.
- [x] Add production release/bootstrap trust root for join manifests.
- [x] Add release artifact signature verification primitive.
- [x] Add production release key lifecycle policy.
- [x] Add platform-native Windows Authenticode verification policy.
- [x] Add real scoped shell adapter execution.
- [x] Add acceptance-test checklist for temporary Windows repair and managed Mac coding.
- [x] Add durable shell adapter audit schema and redaction rules.
- [x] Add signed trust bundle rotation/revocation primitive.
- [x] Add development trust bundle read/update endpoint.
- [x] Wire host-side job verification to signed trust bundle active keys.
- [x] Add durable host trust bundle file store.
- [x] Add durable host identity key store.
- [x] Add durable host nonce replay store.
- [x] Add hash-chained audit export and verification.
- [x] Add final buy-vs-build boundary for MCP, Tailscale/headscale, SSH, Coder, DevPod, VS Code Remote Tunnels, RustDesk/MeshCentral, and Sigstore-style release trust.
- [x] Add explicit exit criteria for v0.1 through v1.0 gates.
- [x] Add stronger workspace and symlink escape tests.
- [x] Add local evidence bundle export.
- [x] Add policy explanation for every host-side denial result.
- [x] Add approval-required results for signed jobs with unsatisfied approval requirements.
- [x] Add signed approval tokens for package install, elevation, GUI, service changes, push, merge, and deploy operations.
- [x] Add durable approval-token consumption and persistence.
- [x] Add gateway/API evidence bundle export directly from job ids.
- [x] Add HTTPS long-poll fallback before WSS/mTLS transport.
- [x] Add installable Skillkit bundle generation with manifest checksums and framework notes.
- [x] Add host-bound managed trust bundle update checks for trust-store refresh.
- [x] Add macOS LaunchAgent plist generation for explicit managed host install.
- [x] Add macOS LaunchAgent status inspection and safe plist uninstall.
- [x] Add workspace lock manager and Git worktree preparation foundation.
- [x] Wire workspace locks into hostrunner, host serve, and managed LaunchAgent arguments.
- [x] Add Codex adapter MVP with locked-workspace execution, Git diff/status evidence, optional verification command evidence, output caps, and redaction.
- [x] Add Codex adapter implicit approval preflight for push, merge, deploy, publish, credential, and service intents before adapter execution.
- [x] Add Codex adapter `go test -json` parsing into `rdev.test-report.v1` verification summaries.
- [x] Add Codex adapter conformance coverage for workspace canonicalization, write-scope escapes, failure evidence, redaction, truncation, and timeout cancellation evidence.
- [x] Add Codex adapter cooperative cancellation from host/job context and gateway job cancellation polling.
- [x] Add canceled-job artifact reporting without changing the gateway job's canceled terminal state.
- [x] Add shared implicit approval preflight for risky shell and Codex actions.
- [x] Add managed Mac coding acceptance harness with locked-worktree evidence export.

## Later

- [ ] Add WSS/mTLS transport.
- [ ] Add authenticated production managed host trust lifecycle with OS-protected storage.
- [ ] Add OS-protected managed host identity storage and registration proof.
- [ ] Add Windows Service mode.
- [ ] Add launchctl start/stop execution and systemd mode.
- [ ] Add acpx adapter.
- [ ] Generalize cooperative cancellation across shell, PowerShell, and future adapter SDK implementations.
- [ ] Add Claude Code adapter.
- [ ] Add Tailscale/headscale adapter.
- [ ] Add RustDesk/MeshCentral adapter.
- [ ] Add Coder workspace adapter.
- [ ] Add DevPod/devcontainer workspace adapter.
- [ ] Add adapter SDK and conformance tests.

## Final End-State Gates

- [ ] Temporary Windows host joins from one visible PowerShell command and leaves no service behind.
- [ ] Managed Mac runs a Codex coding job in a locked worktree and returns diff/test evidence.
- [x] Tampered, expired, wrong-host, or replayed envelopes are rejected host-side.
- [x] Workspace escape and non-allowlisted command attempts are rejected host-side.
- [x] Host-side denials return structured `rdev.host-denial.v1` artifacts.
- [x] Unsatisfied job approvals return structured `rdev.approval-required.v1` artifacts before adapter execution.
- [x] Gateway-approved jobs carry signed `rdev.approval-token.v1` tokens.
- [x] Host-side approval token consumption is persisted and rejects token reuse.
- [x] Package install, elevation, GUI control, service changes, push, merge, and deploy require approval.
- [x] Revocation stops future jobs and is recorded in audit.
- [x] Agent Skillkit can be exported as a checksummed bundle for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.
- [x] Hostrunner can execute `adapter=codex` jobs after signed envelope, identity, nonce, approval, capability, workspace, and lock checks, returning `rdev.codex-result.v1` artifacts.
- [x] Codex jobs that request push, merge, deploy, publish, credential changes, or service changes pause with `rdev.approval-required.v1` before adapter execution unless a matching approval token is present.
- [ ] Production releases verify signed manifests and binaries before host execution.

## Definition Of Done For v0.1.0

- Local gateway and local host can complete a demo ticket/job flow.
- MCP stdio server exposes the Phase 1 tools.
- All jobs produce audit records.
- Audit records can be exported as a verifiable hash chain.
- Jobs can be exported as local evidence bundles with manifest, checksums, artifacts, envelope, policy decision, audit slice, and audit chain.
- Gateway/API can export the same evidence bundle directly from a job id.
- Agent Skills and MCP contracts can be exported as a portable Skillkit bundle with checksums and framework install notes.
- Host-side denials and approval-required pauses are structured artifacts, not opaque errors.
- Signed approval tokens are scoped, expiring, and consumed once by the host.
- Tests cover policy gates and command contracts.
- README has a working local demo.
