# Reliability architecture: no disconnection incidents

Status: design (incident-driven, 2026-08-06)

## Why this document exists

2026-08-06 incident: a single oversized task payload (~40 KB) wedged the
entire queue of a managed Windows host. The host stayed online (heartbeat
advancing) but never executed anything after the poison task; the operator
closed the session to clear the queue, which then **orphaned the host** —
its connector held a per-session join code that pointed at a closed session,
and no mechanism existed for it to discover a replacement. Result: attended
recovery (browser handoff) was required to restore a host that had never
gone offline.

This is a failure class, not a one-off. This document classifies every known
disconnection class, states the architecture principles that prevent each,
and sequences the implementation.

## 1. Failure taxonomy

| # | Class | Mechanism | Observed |
|---|---|---|---|
| C1 | Queue poisoning | One task that cannot complete (oversized result, adapter crash, foreground service update) blocks the poll cursor; host replays it forever; every later task starves | 2026-08-06 (this incident) |
| C2 | Session orphaning | Host holds a single per-session join code; closing/revoking the session invalidates it; host cannot discover a replacement session | 2026-08-06 (this incident) |
| C3 | Cursor replay storm | Host restarts with cursor 0; non-terminal tasks replay on every reconnect; combined with a wedged task this is an infinite loop | skill runbook; same incident mechanism |
| C4 | Connector process death | Service stops (bad artifact, SCM failure); no auto-restart/watchdog; host offline until attended | 2026-08 ELF-build outage |
| C5 | Gateway restart invalidates credentials | Handoffs are memory-state; a gateway restart kills every pending handoff; operator must re-mint | documented in WEB_HANDOFF |
| C6 | Result delivery rejection | Gateway rejects an oversized result event; host retries the POST forever; task never completes | 2026-08-06 (root of C1) |

## 2. Architecture principles

**P1 — Delivery progress is decoupled from execution outcome.**
The host acknowledges an offer (advances the cursor) when it accepts the
task, not when the task completes. Execution results are reported
separately and idempotently (keyed by task + attempt). A poison task can
then never block the queue: acceptance is the only thing the cursor
depends on, and acceptance always succeeds.

**P2 — Results are never rejected; size goes to artifacts.**
The gateway accepts any result within budget. Oversized content travels
through the existing chunked artifact path (1 MB chunks); the inline event
carries a bounded summary + artifact refs. Result rejection ceases to
exist as a failure mode (inline summary budget
`inline_task_result_summary_bytes` is enforced — it exists today but was
never wired).

**P3 — Host identity is decoupled from session identity.**
A managed host is a durable identity (fingerprint, trust, state root). A
session is a lease. The host must be able to follow an assignment: on
lease revocation it re-registers and the control plane directs it to its
assigned (or a waiting replacement) session. Closing a session then rotates
a lease; it never orphans a host.

**P4 — State is durable and restart-safe.**
Host checkpoints its cursor (and session assignment) to the state root, so
restart resumes at the last acknowledged position instead of cursor 0.
Gateway persists handoffs (or clearly invalidates and re-mints them as a
first-class operation), so a restart cannot silently destroy pending
recovery paths.

**P5 — Task execution is supervised.**
Hard timeout = `max_duration_seconds` + grace. Interrupt means kill the
adapter process, not merely deliver an event. The gateway runs a task
watchdog: a task with no result past its deadline is auto-failed with a
typed result, so "offered forever" is impossible.

**P6 — Health is observable.**
Heartbeat is already surfaced. Add task-staleness signals (offered >
deadline, heartbeat lost) to the session status/audit so an operator or
agent sees a forming incident before it becomes a recovery exercise.

## 3. Implementation layers

### L0 — done (fix/task-queue-wedge, commit 84f9591)
- Gateway: oversized result payloads are truncated to the inline summary
  budget with an `output_truncated` marker (never rejected).
- Host: non-transient task failure advances the cursor instead of exiting
  the poll loop; transient gateway errors still retry the poll.

### L1 — next gateway/host release
- Host accepts an offer by advancing the cursor immediately after fetch
  (ack), then executes and posts the result by task id (P1).
- Host checkpoints `after_seq` and session binding to the state root on
  every ack (P4, C3).
- Gateway task watchdog: auto-fail tasks past `max_duration_seconds` +
  grace with a typed `no_result` result (P5, C1/C3).
- Gateway result path: bounded summary inline + chunked artifact upload
  for full content (P2, C6).
- Handoffs: persist or explicitly re-mint after gateway restart, and
  document the invalidation loudly in session status (P4, C5).

### L2 — session assignment model (design change)
- Host directory becomes the source of truth for assignment: operator
  creates a session assigned to a host_id (or the control plane
  auto-reassigns when the current session closes).
- Host, on lease revocation/expiry, polls an assignment endpoint with its
  durable identity and follows the assignment (P3, C2).
- `rdev.sessions.close` for an assigned managed host requires a
  replacement assignment or fails loudly — the operation cannot orphan a
  host by construction.
- Connector service watchdog: if the host process exits, the service
  restarts it with bounded backoff (P5, C4).

## 4. Operator discipline (until L2 ships)

Until session assignment exists, closing a managed session is a key-revocation
operation: it requires a replacement session + host-side rejoin (attended).
Sequence: create replacement session → mint handoff → rejoin → close old.
Never close first.
