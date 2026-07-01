# Versioning

Remote Dev Skillkit uses semantic versioning after v1.0.0.

Before v1.0.0:

- Minor versions may change contracts.
- Patch versions should remain backward-compatible within the same minor line.
- Security fixes may land on any active pre-1.0 branch.
- Development versions are staged as `0.0.N-dev` until the first public
  readiness line. Each `0.0.N-dev` marks one coherent engineering gate, not a
  calendar snapshot.
- `0.1.0-dev` is reserved for the current public-readiness line after the local
  safety kernel, Skillkit packaging, release evidence, public-surface audits,
  and local operator-auth foundation exist.

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
