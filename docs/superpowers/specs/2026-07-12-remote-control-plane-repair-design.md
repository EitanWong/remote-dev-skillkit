# Remote Control Plane Repair Design

## Goal

Repair failures found during real Windows attended-session testing without restoring merged-out host/job/ticket MCP protocols or weakening default permissions.

## Changes

- Add operator-facing HTTP routes for Control Plane v1 Agent event replay and artifact listing, and proxy `rdev.sessions.events/artifacts` to them.
- Remove the stale hidden `rdev.tickets.create/revoke` MCP dispatch and replace stale guidance with `rdev.sessions.close`.
- Normalize only loopback `joinUrl` and `manifestUrl` origins returned by a gateway to the explicitly supplied public gateway origin; preserve non-loopback URLs.
- Add explicit `--capabilities` support to standard support-session connect/start/create flows and generated commands. Empty capabilities retain temporary-mode defaults.
- Preserve current `rdev.sessions.interrupt` semantics as a bounded interrupt intent event; do not restore old `jobs.cancel` semantics.

## Safety

GUI permissions remain explicit per session. No UAC bypass, unattended access, persistence, firewall changes, or hidden bootstrap scripts are introduced.

## Verification

Focused regression tests, full Go tests, vet, Windows amd64 build, protocol drift scan, and real Windows regression when a public tunnel passes the required bootstrap probes.
