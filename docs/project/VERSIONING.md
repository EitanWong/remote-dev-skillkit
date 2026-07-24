# Versioning

Remote Dev Skillkit uses semantic versioning after v1.0.0.

Before v1.0.0:

- Minor versions may change contracts.
- Patch versions should remain backward-compatible within the same minor line.
- Security fixes may land on any active pre-1.0 branch.
- Development versions are staged as `0.0.N-dev` until the first public
  readiness line. Each `0.0.N-dev` marks one coherent engineering gate, not a
  calendar snapshot.

## Version-commit discipline

- Each completed development gate is represented by one verified Conventional
  Commit that updates `CHANGELOG.md` for that version. On the protected main
  branch, that is normally the PR squash commit.
- Do not mix independent completed gates in one version commit. A defect found
  after a gate has been recorded advances the next development version rather
  than rewriting published history.
- Every version commit must carry its focused and full verification evidence in
  the issue or PR before it is integrated.

## Ephemeral cloud acceptance

- Provision a temporary cloud host only when local or cross-platform evidence
  is insufficient for the acceptance target.
- Keep credentials, connection tickets, and raw host metadata out of source,
  changelogs, and audit artifacts. Record only sanitized acceptance evidence.
- Release temporary cloud hosts and their dependent resources after the test,
  including on failure. Confirm deletion with an independent provider readback
  rather than treating a command exit code as proof of cleanup.

- `0.1.0-dev` marks the public-readiness line after the local safety kernel,
  Skillkit packaging, release evidence, public-surface audits, and local
  operator-auth foundation exist.
- `0.1.1-dev` marks hosted storage/auth foundation.
- `0.1.2-dev` marks enrollment authority lifecycle evidence.
- `0.1.3-dev` marks production WSS/mTLS host transport.
- `0.1.4-dev` marks AI-native invite creation, auto transport fallback, and
  connection-entry guidance.
- `0.1.5-dev` marks universal Connection Entry materialization through MCP/CLI
  and generic package-plan output.
- `0.1.6-dev` marks owned managed Connection Entry package materialization for
  reviewed macOS LaunchAgent, Linux systemd user-service, and Windows Service
  plans.
- `0.1.7-dev` marks Connection Entry as the universal target-side handoff
  contract with required invite materialization and generic package-plan
  metadata for owned, third-party, LAN, hosted, relay, mesh, SSH, and
  VPN-assisted scenarios.

Compatibility promises:

- MCP tool names are experimental until v1.0.0.
- Host enrollment protocol is experimental until v1.0.0.
- Agent Skills can evolve independently but should remain backward-compatible where possible.

Release artifacts should include:

- source archive;
- `rdev` binaries for macOS, Windows, Linux;
- SHA-256 checksums;
- signatures;
- signed release manifest index;
- signed `rdev.release-bundle.v1` index verified by `rdev release verify-bundle`;
- Windows Authenticode signatures for Windows artifacts;
- changelog;
- upgrade notes.
