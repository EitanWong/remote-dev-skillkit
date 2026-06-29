# Versioning

Remote Dev Skillkit uses semantic versioning after v1.0.0.

Before v1.0.0:

- Minor versions may change contracts.
- Patch versions should remain backward-compatible within the same minor line.
- Security fixes may land on any active pre-1.0 branch.

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
