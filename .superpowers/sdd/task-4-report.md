# Task 4 Report: Health Marker and Direct-Readiness Contract

## Outcome

Implemented the Phase 1 gateway health instance marker and honest direct-tunnel readiness contract. Created, connected, started, and fresh-agent payloads now expose `ready_to_send`, `ready_to_activate`, and `ready_to_execute`, while the existing `ready_to_send_to_human` and `ready_to_send_human` aliases remain mapped to `ReadyToSend`.

No Task 5 wiring or behavior was implemented.

## TDD evidence

### Gateway instance marker

- RED (previous worker): `go test ./internal/httpapi -run TestHealthzIncludesGatewayInstanceMarker -count=1` failed because `Server.GatewayInstance` and the `X-Rdev-Gateway-Instance` response header did not exist.
- GREEN: constructors now generate a stable random 128-bit hex marker per server, `/healthz` returns it, and `GatewayInstance()` exposes the exact value for supervisor verification.

### Direct readiness

- RED (previous worker): `go test ./internal/supportsession -run 'TestDirectAvailability|TestBuildConnect.*Readiness' -count=1` failed to build because `AvailabilityReadiness`, `DirectAvailability`, and readiness payload fields did not exist.
- GREEN: direct public candidates remain `degraded-single-entry`; they are sendable only with the explicit override and are never ready to activate or execute. No candidate, LAN-only candidate, invalid schema, and zero-value inputs fail closed.

### Fail-open normalization regression

- RED (this review): `go test ./internal/supportsession -run TestNormalizeAvailabilityReadinessRejectsSendableStateWithoutValidAvailabilitySet -count=1` failed because a forged v2 `degraded-single-entry` value with an empty availability set retained `ReadyToSend=true`.
- GREEN: normalization now re-derives readiness from the embedded `AvailabilitySet`; malformed or inconsistent values fail closed.

### Compatibility expectation updates

- RED (this review): the first full `go test ./... -count=1` run failed in `internal/acceptance`, `internal/mcpstdio`, and `internal/cli` because they still required the old script-first `ready=true` behavior.
- GREEN: those expectations now require the Phase 1 fail-closed readiness states while continuing to validate the existing handoff and compatibility surfaces.

## Implementation summary

- Added a per-server gateway instance marker generated with `crypto/rand`, with a time-plus-atomic-counter fallback that exposes no error or secret material.
- Added `supportsession.AvailabilityReadiness` and `DirectAvailability`.
- Added readiness to `CreatedOptions` and `StartedOptions`.
- Mapped readiness into `BuildCreated`, `BuildConnectFromCreated`, `BuildStarted`, and `freshAgentConnectContract`.
- Preserved compatibility fields and made them derive from `ReadyToSend`.
- Kept stable and managed direct URLs non-sendable by default in Phase 1 because the signed connector/rendezvous path is not yet present.
- Updated stale acceptance, MCP, and CLI assertions exposed by full-suite verification.

## Final verification

All commands completed successfully:

```text
go test ./internal/httpapi -run TestHealthzIncludesGatewayInstanceMarker -count=1
go test ./internal/supportsession -run 'TestDirectAvailability|TestNormalizeAvailabilityReadiness|TestBuildConnect.*Readiness' -count=1
go test ./internal/httpapi ./internal/supportsession -count=1
go test ./... -count=1
go test -race ./internal/httpapi ./internal/supportsession -count=1
go vet ./...
git diff --check
```

## Review notes

- The gateway marker is stable for one `Server`, distinct across constructed servers, and contains only a 128-bit hexadecimal identifier.
- Readiness normalization does not trust caller-supplied booleans without a valid availability set.
- The implementation preserves existing payload fields; no Phase 1 compatibility field was removed.
- No secrets, persistence, privilege bypass, inbound public-port behavior, or unrestricted shell surface was added.

## Concerns

None for Task 4. Task 5 still needs to supply real availability sets and explicit override decisions at the appropriate orchestration boundaries.
