# Remote Control Plane Repair Implementation Plan

**Goal:** Align real-host control paths with the current Control Plane v1 protocol.

1. Add failing tests and implement remote Agent event replay and artifact listing.
2. Remove stale ticket MCP dispatch and legacy guidance; retain explicit rejection tests for old ticket/host/job/artifact names.
3. Add failing tests and implement loopback-only invite origin normalization.
4. Add explicit capability propagation through CLI, MCP handoff, generated foreground commands, and final ticket creation.
5. Regenerate `mcp/tools.json`; run full tests, vet, Windows build, diff checks, and real-host regression when the tunnel readiness gate permits it.
