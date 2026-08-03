# Quality Matrix

The project treats reliability as a release gate. This matrix is the living
catalog of corner cases every surface must consider, mapped to the layer that
proves them. New surfaces extend this file **before** merge; CI does not
enforce the file, code review does.

## Three verification layers

| Layer | Mechanism | Proves |
|---|---|---|
| L1 Unit / contract | `go test` per package | Parsing, validation, policy decisions, auth logic, pure functions, error paths |
| L2 Control-plane integration | `httptest` servers, in-process gateway + MCP stdio, `scripts/ci/release-smoke.sh` | Session lifecycle, event replay, leases, persistence, HTTP contracts |
| L3 Live E2E | Real enrolled hosts (Mac/Windows/Linux), `internal/acceptance` | Managed install, reboot, sleep/wake, lock screen, tunnel rotation, real OS integration |

Rule of thumb: every branch in a security-critical package needs an L1 test;
every MCP tool needs an L2 flow test; every managed-host claim needs L3
evidence before it is reported complete.

## Module corner-case catalog

Status: ✅ covered · 🟡 partial · ❌ gap · ⚙️ live-only (real host/system required)

| Module | Corner cases to consider | Status |
|---|---|---|
| `internal/update` | version compare (pre-release, v-prefix, malformed, empty), URL building (trailing slash, escaping, bad repo), HTTP non-200 / bad JSON / unreachable, token header, asset selection (dash vs underscore slug, case, no match), digest presence, shell quoting of adversarial names, plan when no update | ✅ 98% |
| `internal/operatorauth` | file load errors (missing/bad JSON/wrong schema), JWKS fetch failure at load, claim types (aud string/array/mixed/nil, exp float64/int64/Number/garbage), roles claim forms (`[]any`/`[]string`/space-separated string/non-string), hash validation (prefix/length/hex), clock skew boundaries (OIDC exp/nbf at skew edge; hosted exp/nbf strict), wrong audience/issuer, expired/nbf token, duplicate key IDs, SAML response corners (expired assertion, wrong recipient, bad signature, empty response) | ✅ 78.0% |
| `internal/hosttrust` | noop store, file missing/corrupt/wrong schema, atomic write + 0600, rollback rejection, same-sequence content tamper, signature from stored root (not caller-supplied), protected-store backends (keychain/DPAPI/libsecret), malformed protected ref | ✅ 78.8% |
| `internal/httpapi` | session create/join/close/revoke, event replay after cursor, long-poll wait parsing, artifact write authorization (operator path + endpoint lease + task ownership), task resume (operator role, checkpoint/idempotency validation, unknown task), persist-state failure (failing StateStore → 500) | ✅ 71.0% — artifact auth (#19), join/resume, persist-failure covered; `persistStateNoResponse` removed as dead code |
| `internal/protectedstore` | ref parsing (URL-like, missing account, unknown prefix), backend fallthrough, per-platform backends (keychain/DPAPI/libsecret/keyctl/TPM/MDM), empty service/account, backend error propagation | 🟡 36.8% — platform backends are ⚙️ live-only (real keyring/TPM) or need mock seam; parse/open/store logic ✅ |
| `internal/policy` | capability checks, shell allow/deny, scoping, unknown capability handling | 🟡 73.2% |
| `internal/audit` | chain integrity, JSONL append, redaction of secrets, tamper detection | ✅ 76.5% |
| `internal/model` | trust bundle validity windows, key status transitions, hash consistency | ✅ 71.7% |
| `internal/contracts` | tool schema round-trip, required fields, enum constraints, MCP surface parity with `mcp/tools.json` | ✅ 77.6% |
| `internal/hostidentity` | key generation, fingerprint, validation of malformed keys | 🟡 69.0% |
| `internal/workspace` | worktree create/cleanup/rollback, lock contention, write-scope enforcement (absolute/`..`/drive-letter paths, escaping symlinks, scope membership), snapshot diffing (change detection, truncation at 200 files, .git exclusion, escaping scopes), dirty policy | ✅ 71.7% |
| `internal/toolchain` + `internal/depsinstall` | node/toolchain bootstrap, idempotency, failure mid-install, archive security (zip-slip, escaping symlinks, byte limits, HTTPS-only sources + same-host redirects, SHA-256 verify), retry classification and retry loops, atomic copy | ✅ 67–69% — network fetch paths covered with httptest |
| `internal/hostcmd` | managed service start/stop/retry, route pool concurrency, exit codes | ✅ 75.7% |
| `internal/gateway` + `internal/controlplane` | session state machine, lease binding, reconnect, revocation, persistence, snapshot/event sequencing | ✅ 80–81% |
| `internal/hostrunner` | engineering loop, progress, limits (duration/output/attempts), isolation, runtime profiles | ✅ 81.2% |
| `internal/shelladapter` + `internal/powershelladapter` | process groups, redaction, output caps, verification commands | ✅ 68–78% |
| `internal/hostawake` | wake on LAN / platform wake, error fallback | ⚙️ 15.8% — live-only |
| `internal/acceptance` | managed Mac/Windows verification reports, session evidence | ⚙️ 7.3% — live E2E harness, exercised on real hosts |
| `cmd/*` mains | flag parsing, wiring | ✅ thin wrappers; covered via CLI tests where logic exists |

## Coverage floors

`scripts/check-coverage.sh` gates the packages below. Thresholds sit a few
points under measured values; raise them as gaps close. Adding a package to
the gate is part of adding a surface.

| Package | Floor |
|---|---|
| gateway, hostrunner, mcpstdio | 80 |
| cli | 82 |
| update | 90 |
| operatorauth, hosttrust, audit, contracts | 70 |
| policy, model, workspace | 65 |
| httpapi, hostidentity | 60 |
| toolchain, depsinstall | 60 |
| protectedstore | 30 |

## AI-Native surface (2026-08)

Agent-facing push notifications: every session carries an optional `notify_url`
(HTTPS only — loopback HTTP allowed — operator-scoped, audited). When set, the
gateway POSTs each appended session event as `rdev.notification.v1` — host
online/offline (`hello`/`status`), task lifecycle, artifacts — so
Hermes/OpenClaw-class agents react without polling. Manage via MCP
`rdev.sessions.notify` (set/clear/get) or `POST /v1/sessions/{id}/notify`.

Signed deliveries: a `secret` query parameter on the registered URL is stored
separately and sent as `X-Gitlab-Token` on every POST (verified by Hermes'
webhook platform); it never appears in snapshots, MCP responses, or audit
records. See `docs/development/AI_NATIVE_INTEGRATION.md` for the verified
Hermes wiring and the OpenClaw plan.

Delivery is fire-and-forget with a 5s timeout; failures are audited as
`session.notify.delivery_failed`. No retry queue — add one when delivery
guarantees are required. `notify_url` persists in session snapshots.

## Known gaps (ordered by priority)

All L1/L2 gaps are closed. Remaining items require live infrastructure:

1. `internal/protectedstore` platform backends (keyctl/libsecret/TPM) — L3 on a real Linux desktop with keyring available.
2. `internal/hostawake` — L3 on real hosts (part of the managed-host E2E runbook).
3. Windows managed-host E2E regression cadence — every handoff/service change must re-run the live runbook (boot time, sleep/wake, lock screen evidence) before merge.
4. Raise floors as coverage grows: workspace 65→70, toolchain/depsinstall 60→65, protectedstore 30→50 after L3.

## How to add a surface

1. Implement policy + audit design first (AGENTS.md Safety Boundaries).
2. Add the module to this catalog with its corner cases.
3. L1: table-driven tests for parsing/validation/policy/auth branches.
4. L2: one integration flow per MCP tool or HTTP endpoint.
5. Add the package to `scripts/check-coverage.sh` with a realistic floor.
6. L3: run the live runbook on a real enrolled host; attach evidence (task, boot time, reconnect) to the report.
7. CI must pass `./scripts/check.sh` before merge.
