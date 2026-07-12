# Phase 1 Tunnel Security Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the final whole-branch security blockers before any mainland Windows handoff is generated or the Phase 1 branch is considered mergeable.

**Architecture:** Replace enrollment-ticket-based tunnel preflight with a non-enrollment static bootstrap probe, make final ticket publication transactional, continuously invalidate dead routes, protect all policy/evidence/known-hosts inputs, and ensure logs never expose raw provider output or candidate URLs. Keep dynamic MCP explicit gateways non-sendable in Phase 1 because MCP cannot safely create and verify a ticket on an arbitrary remote gateway.

**Tech Stack:** Go 1.25 standard library, existing `internal/tunnel`, `internal/httpapi`, `internal/gateway`, `internal/cli`, `internal/mcpstdio`, table tests, `httptest`, race detector.

## Global Constraints

- No normal enrollment ticket may be used only to probe an untrusted tunnel.
- No real ticket is created without at least one currently healthy candidate.
- A real ticket remains rollback-protected until its handoff/ready output is durably written.
- Dynamic MCP explicit/configured gateways remain `ready_to_send=false` unless the actual remote gateway creates the ticket and passes exact health/bootstrap verification; Phase 1 does not implement that remote transaction.
- “Allow none” and “no restriction” are distinct provider-policy states. Allowed/disabled overlap is rejected.
- Every referenced policy, evidence, or known-hosts file is opened first, validated from the open handle, regular/non-symlinked, bounded to 1 MiB, strictly decoded with EOF, and protected.
- SSH providers fail closed on Windows until a reviewed ACL validator is implemented.
- Raw provider output, URLs, IPs, ticket codes, credentials, and sensitive argv values never reach shareable logs.
- All production changes require a witnessed RED test before implementation.

---

### Task 7: Provider restriction semantics and protected input loader

**Files:**
- Modify: `internal/tunnel/types.go`, `internal/tunnel/region.go`, `internal/tunnel/region_test.go`
- Modify: `internal/cli/tunnel.go`, `internal/cli/tunnel_test.go`, `internal/cli/cli_test.go`
- Modify: `internal/mcpstdio/server.go`, `internal/mcpstdio/server_test.go`

**Interfaces:**
- Add `Policy.RestrictProviders bool` so an empty allowed set can mean “allow none.”
- Add one shared protected bounded JSON file reader in `internal/tunnel` used by CLI and MCP.

- [ ] Write/retain failing tests for all-disabled policy, allowed/disabled overlap, writable evidence, symlink/device input, trailing JSON, unknown fields, and >1 MiB content.
- [ ] Run focused tests and confirm failures are caused by missing restriction/loader behavior.
- [ ] Implement `RestrictProviders`; reject overlap/duplicates; make `providerAllowed` return false when restricted and allowed is empty.
- [ ] Implement open-first `ReadProtectedJSONFile(path, dst)` using `os.Open`, `file.Stat`, regular-file check, POSIX `perm &^ 0600 == 0`, `io.LimitReader(limit+1)`, `json.Decoder.DisallowUnknownFields`, and mandatory EOF. On Windows, accept policy/evidence only from the explicit protected path and report ACL validation unavailable rather than claiming protected status.
- [ ] Make CLI and MCP use the shared loader and canonical provider IDs.
- [ ] Change MCP explicit/configured gateway handling so it never sets `ready_to_send=true`; return the foreground CLI path and an explicit `remote_ticket_and_probe_required` reason.
- [ ] Replace the remaining watcher `bytes.Buffer` with `synchronizedBuffer` and verify the focused race test passes.
- [ ] Run tunnel/CLI/MCP tests, race, vet, full tests, Windows build; commit `fix: enforce tunnel policy restrictions`.

### Task 8: Non-enrollment bootstrap probe endpoint

**Files:**
- Modify: `internal/httpapi/server.go`, `internal/httpapi/server_test.go`
- Modify: `internal/tunnel/probe.go`, `internal/tunnel/manager_test.go`
- Modify: `internal/cli/cli.go`, `internal/cli/cli_test.go`

**Interfaces:**
- Add unauthenticated `GET /v1/support-session/bootstrap-probe.ps1` serving a static, non-enrolling PowerShell probe containing the existing stable bootstrap marker and gateway instance header.
- Add `tunnel.ProbeBootstrapTemplate` for this endpoint; it never accepts a ticket code.

- [ ] Write failing tests proving preflight currently creates/exposes a normal ticket and the static route is absent.
- [ ] Implement the static endpoint with body/content-type/body-size invariants and no ticket/session mutation.
- [ ] Add the SSRF-safe probe function using the existing controlled resolver/dial path and exact instance marker.
- [ ] Replace provisional-ticket filtering with static bootstrap-template probing.
- [ ] Assert gateway ticket count/host count remain unchanged during provider filtering.
- [ ] Run HTTP/tunnel/CLI tests and race; commit `fix: probe tunnels without enrollment tickets`.

### Task 9: Transactional final ticket and survivor loop

**Files:**
- Modify: `internal/cli/cli.go`, `internal/cli/cli_test.go`
- Modify: `internal/gateway/memory.go` only if a rollback helper is required, with matching gateway tests.

**Interfaces:**
- Final ticket metadata contains only candidates that pass the final ticket-specific probe.
- A rollback guard revokes and persists revocation until stdout plus ready/handoff files succeed.

- [ ] Write failing tests for zero candidates, one-of-two final failure, repeated survivor failure, ready-file failure, handoff-file failure, stdout failure, and host registration during rollback.
- [ ] Do not create a final ticket when no candidate survives static preflight.
- [ ] Create a candidate-bound final ticket, probe it, remove failures, revoke it, and recreate with survivors; bound iterations by initial candidate count.
- [ ] Install a rollback defer immediately after final ticket creation. Every error before durable handoff commit revokes the ticket and saves gateway state.
- [ ] If a host registers before rollback, revoke endpoints/host authorization associated with the ticket or fail closed with a gateway-level rollback helper covered by tests.
- [ ] Run CLI/gateway tests and race; commit `fix: publish support tickets transactionally`.

### Task 10: Runtime candidate invalidation and handoff revocation

**Files:**
- Modify: `internal/tunnel/manager.go`, `internal/tunnel/manager_test.go`
- Modify: `internal/cli/cli.go`, `internal/cli/cli_test.go`

**Interfaces:**
- Exited/stopped/degraded handles are removed from `AvailabilitySet.Candidates` atomically.
- Foreground support session monitors runtime availability until the target connects.

- [ ] Write failing tests proving an exited provider remains in candidates/readiness and that last-route death leaves a sendable handoff/ticket.
- [ ] Remove candidates on exit and derive snapshots from healthy live handles only.
- [ ] Re-snapshot immediately before writing handoff.
- [ ] Monitor availability before host connection; if the last route dies, atomically mark status non-sendable, remove/invalidate handoff text, revoke/save the ticket, and report the provider/candidate hash plus error class only.
- [ ] Stop monitoring after the target connects or session ends.
- [ ] Run tunnel/CLI race tests; commit `fix: invalidate dead tunnel handoffs`.

### Task 11: Reviewed known-hosts validation

**Files:**
- Modify: `internal/cli/tunnel.go`, `internal/cli/tunnel_test.go`

**Interfaces:**
- Add `validateKnownHostsFile(path, destination string, port int) error`.

- [ ] Write failing tests for missing, empty, symlink, directory, FIFO/device, writable mode, oversized file, wrong destination, and Windows fail-closed behavior.
- [ ] Reuse the protected open-handle loader rules; reject environment-only fallback on Windows.
- [ ] Require a plaintext host pattern matching `host` or `[host]:port` and a supported key type with non-empty base64 key. Hashed entries remain ineligible until verified via an explicitly designed helper.
- [ ] Run CLI tests/race/vet/Windows build; commit `fix: validate SSH host pins`.

### Task 12: Provider log redaction and final security verification

**Files:**
- Modify: `internal/cli/tunnel.go`, `internal/cli/tunnel_test.go`
- Modify: `internal/cli/cli.go`, `internal/cli/cli_test.go`
- Update: `docs/operations/MCP_STDIO.md` or the closest existing support-session operations document.

**Interfaces:**
- Add structural argv redaction for split and `--flag=value` forms.
- Add line redaction that replaces URLs/IPs/ticket-like values with stable hashes/error classes before logging.

- [ ] Write failing tests for `--token=value`, credentials contents, URL userinfo/query secrets, raw tunnel URLs, IPv4/IPv6, and provider output containing secrets.
- [ ] Stop forwarding raw provider stdout/stderr. Scan internally for assigned URL, then emit only provider ID, candidate hash, lifecycle phase, and sanitized error class.
- [ ] Allowlist safe configured argv preview fields; redact paths and all credential-bearing values.
- [ ] Run full tests, vet, Windows build, `git diff --check`, relevant full race suites, final code review, and final security review.
- [ ] Commit `fix: redact tunnel provider diagnostics`.
