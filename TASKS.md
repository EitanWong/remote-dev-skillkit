# Task Board

## Now

- [x] Create repository skeleton.
- [x] Add architecture, roadmap, versioning, and threat model docs.
- [x] Add initial `rdev` CLI skeleton.
- [x] Add `rdev doctor`.
- [x] Add MCP tool contracts.
- [x] Add Agent Skills drafts.
- [x] Add local preview ticket creation.
- [x] Add policy explanation command.
- [x] Add real MCP stdio server.
- [x] Add in-memory gateway for tickets, hosts, jobs, artifacts, and audit.
- [x] Add local demo flow for ticket-host-job-audit.
- [ ] Add foreground temporary host local loop.
- [x] Add JSONL audit store.
- [x] Add local HTTP development gateway.

## Next

- [x] Define ticket data model.
- [x] Define host data model.
- [ ] Define signed job envelope.
- [x] Define job/artifact/audit models.
- [x] Implement JSONL audit store.
- [x] Implement local HTTP gateway for development.
- [ ] Add PowerShell bootstrap draft.
- [ ] Add Windows capability detector.
- [ ] Add policy explain engine.

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

## Definition Of Done For v0.1.0

- Local gateway and local host can complete a demo ticket/job flow.
- MCP stdio server exposes the Phase 1 tools.
- All jobs produce audit records.
- Tests cover policy gates and command contracts.
- README has a working local demo.
