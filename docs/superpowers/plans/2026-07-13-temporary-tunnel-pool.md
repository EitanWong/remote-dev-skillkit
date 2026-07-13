# Temporary Tunnel Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Maintain a bounded pool of healthy temporary public tunnels, replenish failed routes with backoff, and let the Windows connector fail over across the signed candidate list.

**Architecture:** Extend the existing `tunnel.Runtime` so it can supervise replacement handles and periodic probes without changing the existing `Snapshot`, `Changes`, and `Stop` contract. Configure support-session startup with four initial candidates, bounded replacement, and liveness probes. Extend the Windows host task loop to rotate across signed manifest candidates when the current gateway returns a route-level failure.

**Tech Stack:** Go standard library, existing `internal/tunnel`, `internal/cli`, `internal/hostcmd`, signed `model.JoinManifest` candidates, and the existing fake-provider test helpers.

## Global Constraints

- Keep the visible attended connector model and never add hidden persistence.
- Do not bypass UAC, Windows Defender, firewall policy, or other host security controls.
- Do not log raw tunnel URLs, ticket codes, identities, or provider secrets beyond existing redacted contracts.
- Publish only candidates that pass the existing gateway and bootstrap probes.
- Treat pool replenishment as best-effort; when every published candidate is unavailable, fail closed and require a new handoff.
- Preserve existing behavior when pool supervision is disabled in unit tests or custom callers.

---

### Task 1: Add bounded Runtime pool supervision

**Files:**
- Modify: `internal/tunnel/manager.go`
- Test: `internal/tunnel/manager_test.go`

**Interfaces:**
- Consumes: existing `Manager`, `Selection`, `StartRequest`, `ProbeFunc`, and `Handle` interfaces.
- Produces: `Manager.PoolTarget`, `Manager.LivenessInterval`, `Manager.LivenessFailures`, `Manager.ReplacementBackoff`, and `Manager.ReplacementMaxBackoff` configuration fields; existing `Runtime.Snapshot`, `Runtime.Changes`, and `Runtime.Stop` remain source-compatible.

- [ ] **Step 1: Write failing tests** for unexpected provider exit, liveness failure, replacement success, backoff, and shutdown reaping. Use fake handles whose `Wait` channel can be closed by the test and whose `Stop` call is counted.
- [ ] **Step 2: Run the focused tests** with `go test ./internal/tunnel -run 'TestRuntime.*Pool|TestRuntime.*Replacement|TestRuntime.*Liveness' -count=1` and confirm the new expectations fail.
- [ ] **Step 3: Add Runtime supervision state**: the parent context/cancel function, original selections/request, current handle per selection, replacement-in-flight flags, consecutive probe failures, and supervisor completion state.
- [ ] **Step 4: Implement initial pool configuration** so `PoolTarget` starts all eligible selections up to the configured target while preserving the current `MaxActive` behavior when `PoolTarget` is zero.
- [ ] **Step 5: Implement replacement scheduling** with per-selection exponential backoff capped by `ReplacementMaxBackoff`, a small random jitter, and cancellation-aware timers. A replacement is published only after `startOne` passes its probe.
- [ ] **Step 6: Implement periodic liveness probing** for healthy candidates. Mark a candidate degraded after the configured consecutive-failure threshold, stop and reap its handle, then schedule a replacement.
- [ ] **Step 7: Make shutdown cancel supervision first** and wait for all active/replacement handles to be reaped exactly once.
- [ ] **Step 8: Run the focused tests** and `go test ./internal/tunnel -count=1`; expected result: PASS.
- [ ] **Step 9: Commit** with `git commit -m "feat: supervise temporary tunnel pool"`.

### Task 2: Configure support-session startup for a resilient pool

**Files:**
- Modify: `internal/cli/tunnel.go`
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/tunnel_live_test.go`
- Test: `internal/cli/support_session_availability_watch_test.go`

**Interfaces:**
- Consumes: Runtime pool supervision from Task 1 and the existing static/bootstrap/final probe callbacks.
- Produces: support-session startup with a target pool of four, minimum healthy publication of two, periodic liveness, and replacement health events visible through the existing availability snapshot.

- [ ] **Step 1: Add failing configuration assertions** that the default support-session manager requests four candidates, uses a bounded replacement backoff, and enables liveness supervision.
- [ ] **Step 2: Run the focused CLI tests** and confirm they fail against the existing `MaxActive: 2` configuration.
- [ ] **Step 3: Configure the default manager** with `MaxActive: 4`, `PoolTarget: 4`, a conservative liveness interval, a three-failure threshold, and bounded replacement backoff.
- [ ] **Step 4: Keep the existing bootstrap probe gate** before ticket publication; ensure replacement candidates update availability snapshots but never bypass static bootstrap validation for the published handoff.
- [ ] **Step 5: Adapt availability watcher behavior** so an alternate published candidate keeps the handoff valid before connection, while all-candidate loss still invalidates it. Preserve the connected-session fail-closed behavior when the target cannot yet rotate routes.
- [ ] **Step 6: Run the focused CLI tests**, the live tunnel tests that do not require external providers, and `go test ./internal/cli -count=1`; expected result: PASS.
- [ ] **Step 7: Commit** with `git commit -m "feat: configure resilient support tunnel pool"`.

### Task 3: Add target-side candidate gateway failover

**Files:**
- Modify: `internal/hostcmd/hostcmd.go`
- Test: `internal/hostcmd/hostcmd_test.go`

**Interfaces:**
- Consumes: signed `JoinManifest.GatewayCandidates`, existing gateway trust verification, session lease, identity fingerprint, event cursor, and task idempotency.
- Produces: candidate rotation for manifest-based `--once=false` sessions without changing the session identity or event cursor.

- [ ] **Step 1: Write failing tests** for selecting the next signed candidate after a route-level 404/5xx/network failure, preserving the event cursor, refreshing trust, and stopping after every candidate is quarantined.
- [ ] **Step 2: Run the focused host tests** with `go test ./internal/hostcmd -run 'Test.*Gateway.*Failover|Test.*Candidate.*Rotation' -count=1` and confirm failure.
- [ ] **Step 3: Add route-failure classification** for 404/502/503/504 and transport failures while preserving authentication, policy, malformed-task, and invalid-session errors as terminal.
- [ ] **Step 4: Add a candidate rotation helper** that uses only manifest-signed candidates, tracks per-candidate cooldown, verifies the trust bundle on the candidate, and updates the local gateway URL only after verification succeeds.
- [ ] **Step 5: Wrap event fetch, task fetch, and task-result submission** so a route failure rotates candidates and retries the same idempotent operation; never duplicate task execution or reset `afterSeq`.
- [ ] **Step 6: Run focused host tests and `go test ./internal/hostcmd -count=1`; expected result: PASS.
- [ ] **Step 7: Commit** with `git commit -m "feat: fail over host sessions across gateway candidates"`.

### Task 4: Verify pool and failover contracts end to end

**Files:**
- Modify: `internal/acceptance/fresh_agent_support_session.go` only if the acceptance report needs new pool evidence.
- Test: existing `internal/acceptance`, `internal/cli`, `internal/tunnel`, and `internal/hostcmd` suites.

**Interfaces:**
- Consumes: all previous tasks.
- Produces: evidence that one temporary tunnel can be removed and replaced while another candidate remains usable, plus explicit all-candidates-failed diagnostics.

- [ ] **Step 1: Add a fake multi-provider acceptance scenario** with three candidates, one provider exit, one liveness failure, replacement success, and target candidate rotation.
- [ ] **Step 2: Run `go test ./...` and `go test -race ./internal/tunnel ./internal/hostcmd ./internal/cli`**; expected result: PASS with no race reports.
- [ ] **Step 3: Run `bash scripts/ci/release-smoke.sh`**; expected result: JSON output contains `"ok": true` and no external mutation.
- [ ] **Step 4: Run the real Windows smoke/GUI path** with the existing attended connector, confirming screenshot, window inspection, application launch/close, keyboard input, and session continuity.
- [ ] **Step 5: Review the final diff** for secret leakage, unbounded retries, duplicate task risk, and accidental changes to the visible connector policy.
- [ ] **Step 6: Commit** with `git commit -m "test: verify temporary tunnel pool recovery"`.

## Residual limitation

Without a stable gateway, the target can only fail over across candidates published in its signed manifest. A replacement URL created after all original candidates fail cannot be delivered to that disconnected target automatically. The implementation must report this explicitly instead of claiming unlimited availability.
