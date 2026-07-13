# MCP Current Protocol Convergence

## Objective

Make the MCP stdio server expose one coherent current protocol surface. The
server will advertise and accept only the current `rdev.sessions.*` tools. Old
support-session, invite, acceptance, adapter, audit, and policy MCP tool names
will no longer be callable through `tools/call`.

## Design

- `internal/contracts.Tools()` remains the single source of truth for the
  eight current session tools.
- `tools/list`, `rdev mcp tools`, and `mcp/tools.json` must be generated from
  that same registry.
- `tools/call` must reject every retired tool with the standard MCP method-not-
  found error instead of silently retaining an unadvertised compatibility
  route.
- The MCP protocol version remains `2025-11-25`, which is the current official
  specification path used by the project. This change is a tool-surface
  migration, not a JSON-RPC protocol-version migration.
- Tests will compare advertised names, accepted names, and static metadata, and
  will explicitly cover rejection of retired names.

## Related Correctness Work

- Add focused behavioral coverage for `hostrunner`, `gateway`, `mcpstdio`, and
  `cli`, then enforce an 80% package threshold for core runtime packages in
  `scripts/check.sh`.
- Keep `rdev doctor` truthful and refresh the local Skillkit installation from
  this checkout instead of weakening stale-install diagnostics.
- On non-Windows platforms, do not advertise desktop capabilities until a
  native backend exists; continue to fail closed at execution time.

## Non-Goals

- No compatibility alias period for retired MCP tool names.
- No replacement macOS/Linux desktop automation backend in this change.
- No changes to the Control Plane v1 session schemas or gateway HTTP API.
