# Skillkit Install

Remote Dev Skillkit is meant to be installable by many agent runtimes without requiring Hermes-specific assumptions.

The portable install surface is a generated bundle:

```bash
rdev skillkit export \
  --source-root . \
  --out dist/remote-dev-skillkit \
  --gateway-url https://api.example.com/v1

rdev skillkit verify \
  --bundle dist/remote-dev-skillkit
```

The bundle uses schema `rdev.skillkit-bundle.v1` and contains:

- `manifest.json` with checksums and skill metadata;
- `skills/` with the agent-loadable workflows;
- `mcp/tools.json` with the stable tool contract metadata;
- `INSTALL.md` with generic install steps;
- `frameworks/` with notes for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.

Verification emits schema `rdev.skillkit-bundle-verification.v1` and checks the
manifest schema, required skills, required framework notes, safe relative paths,
listed file SHA-256/size, and absence of unlisted bundle files. Do not install a
bundle into any agent runtime until verification returns `ok=true`.

## Generic Agent Runtime

1. Install or build the `rdev` binary.
2. Verify the exported or downloaded bundle with `rdev skillkit verify --bundle <bundle-dir>`.
3. Copy `skills/*` into the agent runtime's skill or instruction directory.
4. Configure MCP stdio with `rdev mcp serve`, or configure MCP HTTP/API against the deployed gateway.
5. Keep these skills installed together:
   - `safe-remote-support`;
   - `host-triage`;
   - `remote-vibe-coding`;
   - `remote-job-review`.
6. Require the agent to export or read a `rdev.evidence-bundle.v1` bundle before claiming remote work is complete.

## Framework Notes

- Codex: install the skill folders into the Codex skill path and configure the MCP command to run `rdev mcp serve`.
- Claude Code: install the skill files as project or user instructions and configure MCP stdio/HTTP through the runtime's MCP surface.
- Hermes/Lucky: install the skills into the Hermes agent profile and point tools at the deployed rdev gateway.
- OpenClaw/OpenCode: install the same skill folders and MCP tool contract; no Hermes-only dependency is required.
- Generic MCP agents: use `mcp/tools.json` as the stable schema reference and call the rdev MCP server.

## Safety Contract

An agent runtime is compatible only if it preserves the safety contract:

- agents request typed jobs, not raw host ownership;
- temporary hosts use attended foreground mode;
- high-risk actions require approval;
- host-side validation remains mandatory;
- evidence and audit are reviewed before completion;
- temporary sessions are revoked when finished.

## Current Limits

The generated bundle is a packaging surface for the current local/dev implementation. Production hosted install still needs:

- authenticated MCP HTTP;
- production gateway storage;
- signed multi-platform release artifacts;
- OS-native managed service install/uninstall;
- production WSS/mTLS transport.
