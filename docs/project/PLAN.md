# Project Plan

## Objective

Create `remote-dev-skillkit`: a robust agent-native toolkit for remote coding and repair tasks across temporary and managed hosts.

## Success Criteria

The project is complete when:

1. Agents can install skills and discover `rdev-mcp` tools.
2. Hermes can create a support ticket.
3. A Windows target can run a visible bootstrap and connect outbound-only.
4. Hermes can authorize/revoke the session.
5. an agent can create policy-bound session tasks.
6. The host can execute allowed tasks, stream logs/artifacts, and reject disallowed actions.
7. Tasks produce audit evidence.
8. Managed devices can run as stable services with recovery.
9. Coding tasks can call Codex/Claude/OpenCode through an adapter.
10. The system has tests, signed release artifacts, install docs, and a threat model.

## Phase 1: Safe Local Foundation

- Establish repository, docs, license, security policy.
- Implement `rdev` CLI skeleton.
- Implement local capability detection.
- Define MCP tool contracts.
- Draft Agent Skills.
- Add tests and local CI script.
- Add GitHub Actions CI for tests, script syntax checks, and release dry-run smoke.

## Phase 2: Gateway and Ticket MVP

- Implement ticket creation and revocation.
- Add host registry.
- Add SQLite-backed session task queue.
- Add audit event store.
- Expose local HTTP API for development.
- Add MCP server transport.

Phase 2 starts with an in-memory gateway to validate ticket, session, endpoint, task, artifact, and audit state transitions before durable storage.

## Phase 3: Windows Temporary Host MVP

- Build self-contained Windows binary.
- Implement visible foreground host mode.
- Implement outbound WSS/mTLS channel.
- Implement one-time enrollment token exchange.
- Add shell.user and scoped filesystem tasks.
- Add artifact streaming and local audit spool.

## Phase 4: Managed Hosts

- Windows Service planning/control first, then real install/start/reconnect/stop/uninstall acceptance.
- Linux systemd unit.
- macOS LaunchAgent/LaunchDaemon.
- Watchdog/restart policy.
- Auto-update with signed manifests and rollback.

## Phase 5: Coding Adapters

- `acpx` adapter.
- Codex adapter.
- Claude Code adapter MVP is implemented; remaining adapter backlog focuses on ACP/acpx and production Adapter SDK hardening.
- Workspace locks.
- Git worktree/branch workflow.
- Test evidence collection.

## Phase 6: Mesh and GUI Adapters

- Tailscale/headscale optional adapter.
- RustDesk self-hosted adapter.
- MeshCentral adapter.
- GUI capability consent and audit.

## Phase 7: Open Source Release

- Reproducible builds.
- Signed binaries.
- Release notes.
- Public install docs.
- Security review.
- v0.1.0 tag.
