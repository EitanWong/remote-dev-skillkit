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
- [x] Add persistent dev gateway signing key and host trust pin checks.
- [x] Add signed dev join manifest endpoint and manifest-driven host registration.
- [x] Add initial release/bootstrap trust root verification for join manifests.
- [x] Add release artifact signature manifest and CLI verifier.
- [x] Wire release artifact signature verification into Windows bootstrap via hash-pinned verifier.
- [x] Add shell job policy explain engine and MCP tool.
- [x] Make host revocation cancel queued/running jobs and audit the stop.

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
- [ ] Add production release key lifecycle policy.
- [ ] Add platform-native Windows Authenticode verification policy.
- [x] Add real scoped shell adapter execution.
- [ ] Add acceptance-test checklist for temporary Windows repair and managed Mac coding.
- [ ] Add durable shell adapter audit schema and redaction rules.

## Later

- [ ] Add WSS/mTLS transport.
- [ ] Add host identity key storage.
- [ ] Add Windows Service mode.
- [ ] Add systemd and launchd modes.
- [ ] Add acpx adapter.
- [ ] Add Codex adapter.
- [ ] Add Claude Code adapter.
- [ ] Add Tailscale/headscale adapter.
- [ ] Add RustDesk/MeshCentral adapter.

## Final End-State Gates

- [ ] Temporary Windows host joins from one visible PowerShell command and leaves no service behind.
- [ ] Managed Mac runs a Codex coding job in a locked worktree and returns diff/test evidence.
- [ ] Tampered, expired, wrong-host, or replayed envelopes are rejected host-side.
- [ ] Workspace escape and non-allowlisted command attempts are rejected host-side.
- [ ] Package install, elevation, GUI control, service changes, push, merge, and deploy require approval.
- [x] Revocation stops future jobs and is recorded in audit.
- [ ] Production releases verify signed manifests and binaries before host execution.

## Definition Of Done For v0.1.0

- Local gateway and local host can complete a demo ticket/job flow.
- MCP stdio server exposes the Phase 1 tools.
- All jobs produce audit records.
- Tests cover policy gates and command contracts.
- README has a working local demo.
