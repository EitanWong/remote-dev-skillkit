# Formal Release Acceptance Gate Design

## Status

Approved design. This document defines the release-validation boundary before
implementation.

## Goal

Keep day-to-day candidate creation fast for a solo developer while making
formal release planning depend on independently verifiable, real-environment
acceptance evidence. A local build, unit test suite, or fixture must never be
presented as proof of a real Windows host, agent runtime, restrictive network,
or managed service run.

## Scope

The existing `rdev release prepare-candidate` and ordinary
`rdev release verify-candidate` commands remain the fast development path.
They continue to verify staged artifacts, the release bundle, Skillkit, SBOM,
provenance, and checksums without requiring access to external test machines.

Formal release planning gains a strict gate. It accepts only a verified release
evidence index and fails closed when any required real-run evidence is missing,
invalid, or does not identify the candidate being planned. The gate applies to
formal release planning and release CI, not normal development candidates.

This change does not automate installation on third-party devices, publish a
GitHub Release, or implement new host persistence. Operators still perform
real environment runs with explicit authorization.

## Evidence Matrix

The formal-release evidence index must record and verify these matrix entries:

1. A clean Windows Connection Entry run, including bootstrap verification,
   join, status, session close, and no-persistence cleanup.
2. Fresh runs using at least two distinct supported agent runtimes.
3. A restrictive-network connection run through a supported connectivity
   adapter.
4. Managed-service lifecycle evidence: Windows Service install/start/stop/
   uninstall and Linux systemd user-unit start/reboot-or-reconnect/stop/
   uninstall.
5. A post-release-download verification package for the planned release
   version and platform assets.

Each entry is an existing or dedicated acceptance package whose verifier checks
its manifest, checksums, transcript, audit chain, and release-signature output.
The index stores only the verified package reference, digest, result summary,
candidate version, and artifact or bundle identity. Raw transcripts and audits
remain in the acceptance archive, outside release assets.

## Data Flow

1. A developer creates and locally verifies a candidate as today.
2. Authorized operators execute real acceptance runs and package evidence.
3. `rdev acceptance release-evidence-index` verifies each package and writes a
   checksummed index.
4. Formal candidate verification validates the candidate plus that index. It
   requires every matrix entry, distinct agent runtime identities, and matching
   candidate version/bundle identity.
5. GitHub Release planning consumes only a successful formal verification
   report. Any failed check produces machine-readable failures and blocks the
   plan.

## CLI and Compatibility

The implementation should use an explicit formal mode or a distinct formal
verification command. The ordinary candidate commands must preserve their
current behaviour so fast local iteration remains possible.

The release-planning command must require a successful formal verification
report. It must not offer a flag that suppresses failed evidence checks. A
development-only release plan, if retained, must be visibly marked as
non-publishable and must not be usable by the formal CI workflow.

## Failure Semantics

Verification fails closed for a missing package, package verification failure,
missing matrix category, fewer than two distinct agent runtimes, untrusted
digest, candidate version mismatch, bundle identity mismatch, or evidence
marked as simulated. Errors identify the failed matrix entry and the package
check that failed, without emitting sensitive transcript content.

## Testing

Unit and CLI contract tests will cover the development fast path, each missing
or malformed matrix entry, duplicate agent runtimes, candidate binding
mismatches, successful formal verification, and blocked release planning.
Fixture packages will prove verifier behaviour only; they cannot satisfy the
formal release CI gate. CI will run the fast path on pull requests and require
the formal evidence index only for an explicitly designated release workflow.

## Deferred Work

Collecting the actual external evidence, production trust lifecycle, real
Windows Service execution, and real Linux reboot evidence remains separate
operator work. This gate makes their absence visible and release-blocking; it
does not fabricate or replace them.
