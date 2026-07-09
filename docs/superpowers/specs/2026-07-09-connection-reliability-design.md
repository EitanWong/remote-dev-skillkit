# Connection Reliability Design

## Goal

Make attended temporary support-session connection feel like it just works: the target-side user runs one visible command, and rdev handles helper reuse, gateway selection, registration retry, transport fallback, reconnect status, and clear Agent-facing state without asking the user to diagnose network details.

## Scope

This first reliability pass covers attended temporary support sessions on Windows, macOS, and Linux. Managed service reuse is reported when detected, but this pass must not install, start, restart, or hide services automatically. Full runner capabilities remain gated by existing host registration, authorization, policy, and task authorization.

## Non-Goals

- Do not add hidden persistence.
- Do not bypass UAC, sudo, TCC, Gatekeeper, Windows Defender, enterprise firewall, proxy, or local security controls.
- Do not expose target hosts through inbound public ports.
- Do not make Cloudflare preferred-IP testing active by default.
- Do not treat preconnect or cached helper reuse as task authorization.

## Architecture

Add a small connection reliability layer around the existing flow instead of replacing the protocol. The layer has four responsibilities:

1. Build a deterministic connection plan from the signed join manifest.
2. Reuse verified local helpers before downloading.
3. Retry registration and runtime requests across signed gateway candidates.
4. Report state transitions in support-session status so Agents do not mistake progress for failure.

The target-side state machine is:

`preconnect -> helper-resolved -> manifest-verified -> gateway-selected -> registering -> registered -> authorized -> serving -> degraded -> reconnecting -> recovered`

States before `registered` are diagnostic only and never grant host task access.

## Components

### `internal/connectionhealth`

New small package that contains pure planning/status helpers:

- `GatewayCandidate`
- `Attempt`
- `Plan`
- `BuildPlan`
- `RecordAttempt`
- `StatusFromAttempts`

It must be testable without opening local sockets.

### `internal/assetdownload`

Continue owning verified helper reuse, Range resume, mirror fallback, retry, transcript, and SHA-256 promotion. This pass adds a default target-side cache path helper and makes cache-hit status easier for bootstrap and `rdev-bootstrap` to report.

### `internal/hostcmd`

Host registration becomes candidate-aware. If the selected gateway fails during manifest probe or registration, host serve tries the next signed candidate before returning a user-visible failure. `--transport=auto` chooses the best available runtime transport and falls back from `wss` to `long-poll` to `poll` when a higher transport fails.

### `internal/httpapi`

Bootstrap scripts reuse verified helpers from a user cache and report progress phases:

- `using-installed-helper`
- `using-cached-helper`
- `downloading-helper`
- `verifying-helper`
- `starting-full-helper`

The scripts continue to run visibly and use outbound-only gateway communication.

### `internal/supportsession`

Status and handoff contracts expose reliability state:

- gateway candidate attempts
- selected gateway
- helper source
- transport mode and fallback reason
- reconnecting/degraded/recovered state

Agent guidance must say to continue waiting during downloading, verifying, gateway switching, and reconnecting.

## Data Flow

1. Operator creates a support session and sends the target handoff command.
2. Target bootstrap sends `preconnect`.
3. Bootstrap resolves helper in this order: installed `rdev`, verified cache, download `.gz`, download raw.
4. Full helper verifies the signed manifest and builds a candidate list.
5. Host tries gateway candidates in order, recording failures and the selected gateway.
6. Host registers with an idempotency key derived from ticket code and host identity fingerprint.
7. Host waits for authorization and starts task transport.
8. If transport fails, host reports degraded/reconnecting and falls back to a lower transport or next gateway candidate.
9. Support-session status presents a user-safe summary and Agent-safe next action.

## Error Handling

Transient network failures use bounded retry with backoff and candidate fallback. Permanent policy errors, trust failures, checksum mismatch, expired tickets, revoked hosts, and authorization denial stop immediately and surface precise messages. Target clock skew must not decide ticket or manifest validity; gateway-authoritative time remains the long-term direction for enrollment and trust bootstrap.

## Testing

Tests must avoid real local port binding when possible. Use fake `http.RoundTripper` and pure state-machine tests for reliability logic.

Required gates:

- helper cache hit avoids asset download
- helper checksum mismatch forces redownload or failure
- registration falls back from a failing gateway candidate to the next candidate
- `--transport=auto` falls back from failing `wss` to `long-poll` or `poll`
- status reports downloading/verifying/reconnecting as progress, not failure
- wrong target system time does not break connection planning
- existing connected host does not trigger repeated helper download

## Safety Invariants

- `rdev-bootstrap` and preconnect cannot claim tasks.
- Cached helper must match gateway-provided SHA-256 before execution.
- Candidate gateway fallback must stay inside signed manifest candidates or explicit operator-provided gateway URLs.
- No automatic service installation or hidden background persistence in attended temporary mode.
