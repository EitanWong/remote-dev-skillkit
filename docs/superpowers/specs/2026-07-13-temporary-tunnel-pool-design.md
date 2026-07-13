# Temporary Tunnel Pool And Best-Effort Session Survivability

## Goal

Increase the reliability of attended temporary support sessions when no stable public gateway is available. The system should keep multiple independently sourced public tunnels healthy, preserve overlap during replacement, and let a target try pre-published gateway candidates after a single route fails.

This is a best-effort availability feature. Without a stable rendezvous endpoint, it cannot guarantee zero-interaction recovery after every initially published candidate has been reclaimed or after all tunnel providers are unavailable at the same time.

## Constraints

- Keep the visible, attended connector model. Do not add hidden persistence or unattended access.
- Do not bypass UAC, Windows Defender, firewall policy, or other host security controls.
- Keep tunnel URLs, tickets, identities, and provider material out of logs and user-facing diagnostics unless already covered by the existing redacted contract.
- Prefer standard-library Go and the existing tunnel/provider abstractions.
- Treat tunnel providers as untrusted, expiring public edges. Every candidate must pass the existing DNS, TCP, TLS, gateway health, and bootstrap probes before publication.
- Do not claim that a temporary tunnel pool is equivalent to a stable hosted gateway.

## Architecture

### Pool supervisor

Add a bounded supervisor around the existing tunnel provider selections. The supervisor maintains a target pool size and a minimum healthy size:

- target pool size: 4 candidates;
- minimum healthy size: 2 candidates;
- maximum concurrent starts: 2;
- replacement starts only after the replacement has passed all probes;
- an existing healthy candidate is never removed merely to make room for an unverified replacement.

The supervisor owns provider start, probe, liveness, exit observation, replacement scheduling, and shutdown. The existing `tunnel.Runtime` remains the source of immutable availability snapshots and change notifications; the supervisor adds replacement behavior around it rather than exposing provider handles to callers.

### Provider diversity

The initial selection order should prefer independent failure domains. The supervisor must not fill the pool with multiple candidates from one provider when another eligible provider is available. Provider policy, credentials, SSH trust pins, and regional eligibility continue to be enforced by the existing registry.

### Keepalive and staggered rotation

Each healthy candidate receives periodic liveness and gateway probes. Probe failures are debounced with a consecutive-failure threshold before removal. The supervisor also tracks candidate age and rotates candidates proactively with overlap:

1. start a replacement;
2. verify it end-to-end;
3. publish it as healthy;
4. only then retire the oldest candidate when the pool exceeds its target size.

The rotation interval must be configurable internally and bounded by a conservative default. It is a refresh heuristic, not a claim about an undocumented provider TTL. Backoff with jitter is required after failed starts so a provider outage does not create a tight retry loop.

### Candidate publication and target failover

The Connection Entry must publish the complete set of healthy candidates available at handoff creation. The target bootstrap/connector path should try candidates in order, verify the gateway contract, and keep the candidate set for reconnect attempts. A candidate that fails is locally quarantined for the current connection attempt.

Candidate replacement after handoff publication is useful for future entries and supervisor health, but it cannot be reliably delivered to an already disconnected target without a stable control plane. Therefore the system must:

- keep the original candidate set valid for the session lifetime where possible;
- preserve host identity and session event replay semantics for reconnects;
- report an explicit new-handoff action when every published candidate is unavailable;
- never report a dead session as healthy merely because the local supervisor process remains alive.

### Failure behavior

- One candidate fails while at least one other candidate is healthy: remove the failed candidate, start a replacement, and keep the session available.
- A primary candidate fails before target connection while a secondary remains healthy: retain the handoff and continue waiting.
- All published candidates fail before target connection: revoke/invalidate the handoff and emit a new-handoff diagnostic.
- A connected target loses its current route: attempt the next published candidate using the same host identity and session identity when the connector supports it.
- All candidates fail after connection: fail closed, preserve audit evidence, and report that a new handoff is required.
- Context cancellation or explicit operator stop always wins over replacement work and reaps every provider process.

## Data flow

```text
provider registry
    -> eligible selections with failure domains
    -> pool supervisor starts bounded candidates
    -> DNS/TCP/TLS/gateway/bootstrap probes
    -> healthy candidate snapshot
    -> signed Connection Entry with candidate list
    -> target bootstrap tries candidates and joins session
    -> supervisor liveness/age monitor
       -> remove failed candidate
       -> start and verify replacement
       -> publish availability change
```

## Testing

Unit tests must cover:

- pool reaches the target size when providers start successfully;
- a failed handle is removed and replaced after the replacement passes probes;
- a failed replacement uses bounded exponential backoff and does not spin;
- a secondary failure keeps a healthy primary published;
- an unhealthy replacement is never published;
- age-based rotation preserves at least the minimum healthy pool;
- all candidates failing produces a fail-closed diagnostic;
- shutdown cancels and reaps every active provider exactly once;
- candidate ordering prefers distinct provider failure domains;
- candidate lists are redacted and signed according to existing contracts.

Integration tests must cover a temporary support session with three fake providers, one provider exit, replacement publication, and target-side candidate fallback. The existing live Windows acceptance remains the final evidence for screenshot, window inspection, application launch/close, and keyboard control.

## Explicit limitation

No design using only ephemeral public URLs can guarantee continuous connectivity if every published URL is simultaneously reclaimed and there is no stable endpoint through which the target can learn replacement URLs. The pool, active probes, pre-warmed spare, staggered rotation, provider diversity, and candidate failover maximize availability under that constraint; a stable hosted gateway remains the architectural solution for stronger guarantees.
