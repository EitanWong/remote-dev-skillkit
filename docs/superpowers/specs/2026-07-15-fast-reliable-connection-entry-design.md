# Fast, Reliable Connection Entry Design

## Status

Approved design. This document defines the connection performance and update
boundary before implementation.

## Goal

Make first connection fast and reliable while allowing installed hosts to gain
new capabilities through explicit, verified updates. Connection success,
recovery, and time to first active host take priority over adding new helper
features.

## Current Constraint

The current Connection Entry archive embeds every staged candidate artifact,
its manifests, release metadata, and launchers. The target must obtain this
large archive before the visible launcher can verify and start `rdev`. Existing
`rdev update check` and `rdev update plan` only report an available release;
they do not provide an installed host with an atomic, policy-controlled update
path.

## Design

### Bootstrap Layer

Each platform receives a small Connection Entry bootstrapper. It contains only
the logic needed to read a signed release manifest, select a platform runtime,
resume a download, verify a signature and digest, and start a verified runtime.
It never carries ticket codes, gateway URLs, credentials, or persistent service
configuration.

The bootstrapper uses HTTPS only, follows an allowlisted redirect policy, and
rejects an unsigned or expired manifest. It caches verified content by digest.
On a cache hit it starts the verified runtime without a network download.

### Runtime and Helper Layers

The host runtime is a separate, signed platform asset. It is the only runtime
asset required to join a temporary session. Relay helpers, desktop helpers, and
future adapter dependencies are separate signed components identified by
platform, version, digest, and capability.

The runtime requests optional components only after the signed join manifest,
host policy, and selected connection path require them. Components are cached
in a user-scoped content-addressed directory and shared only when their digest
and platform match. A helper never becomes a first-connection prerequisite
merely because it is available in a release.

### Update and Rollback

An update downloads to a new version directory and verifies the release
manifest, signature, digest, platform, and executable health before activation.
Activation is an atomic pointer or directory switch. If the new runtime fails
the startup health check, the bootstrapper continues with the last known-good
runtime and records a structured failure event.

Temporary mode performs updates only as part of a visible, authorized
Connection Entry run. It must not create a background updater or service.
Managed mode may use an explicit policy-controlled update schedule, but every
check, download, activation, rollback, and component installation is audited.

### Connection Strategy

The bootstrapper starts the cached runtime as soon as its signature is
verified. The runtime probes signed gateway candidates concurrently within
bounded timeouts and preserves the existing preferred transport and fallback
behaviour. Optional helper discovery/download runs only when the selected
connection path needs it.

Connection reports include stage durations and outcomes for cache lookup,
manifest fetch, runtime download/resume, signature verification, gateway probe,
transport selection, host registration, reconnect, and rollback. Reports
redact URLs, tickets, tokens, and local sensitive paths.

## Non-Goals

- Binary delta patching is deferred. Small independently-versioned assets and
  content-addressed caching achieve most of the benefit with less risk.
- This design does not add background persistence to temporary hosts.
- This design does not bypass platform security controls or make third-party
  hosts reachable through inbound public ports.
- It does not claim real network performance until measured acceptance evidence
  exists.

## Compatibility

The existing self-contained archive remains supported during migration. New
releases produce a bootstrap-compatible signed asset manifest in parallel.
Connection Entry prefers the layered path when all required assets are present
and falls back to the verified archive otherwise. The fallback is removed only
after cross-platform acceptance evidence proves parity.

## Failure Semantics

Missing, unsigned, expired, platform-incompatible, or digest-mismatched assets
fail closed before execution. A partial download is retained only as resumable
untrusted data and is never executed. Failed optional helpers must not prevent
core host connection when the selected policy permits another connection path.
No available verified runtime produces a clear, structured bootstrap failure
with recommended recovery actions.

## Testing and Acceptance

Unit and CLI contract tests cover manifest selection, cache hits, resumable
downloads, redirect restrictions, signature and digest failures, optional
component deferral, atomic activation, rollback, and audit redaction.

Acceptance evidence measures cold and warm first connection on Windows first,
then macOS and Linux. It records archive/bootstrap bytes downloaded, time to
host registration, reconnect time, cache hit rate, failed helper isolation, and
successful rollback. These real-run packages become inputs to the formal
release acceptance gate.

## Delivery Order

1. Define the signed layered asset manifest and content-addressed cache API.
2. Implement the Windows bootstrapper download, verification, and runtime
cache, retaining the archive fallback.
3. Add optional-helper manifests and lazy installation after connection-path
selection.
4. Add atomic runtime activation, health checks, rollback, and audited update
reports.
5. Add cross-platform compatibility and real acceptance measurements.
