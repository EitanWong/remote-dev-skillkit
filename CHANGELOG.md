# Changelog

## Unreleased

### Changed

- The project now exposes a single session Control Plane for host joining, scoped task execution, events, artifacts, interruption, and closure.
- The CLI, HTTP API, MCP contract, host runtime, acceptance coverage, documentation, and skills use the current session surface.
- Development verification is `go test ./...`, `go vet ./...`, and `./scripts/check.sh`.
