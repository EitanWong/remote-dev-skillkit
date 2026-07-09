# Control Plane v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the experimental host/task/preconnect surfaces with a simple, fast, session-first Control Plane v1.

**Architecture:** Build a thin `internal/controlplane` kernel around five primitives: Session, Endpoint, Lease, Event, and Task. Wire gateway, HTTP, MCP, status, and host loop to those primitives without preserving old experimental route/tool contracts.

**Tech Stack:** Go standard library, existing in-memory gateway, existing HTTP API server, existing MCP stdio server, existing host adapters.

## Global Constraints

- Official protocol version is v1; do not use v2 naming.
- Earlier host/task/preconnect contracts are experimental and do not define a public contract.
- First principles are stability, speed, efficiency, simplicity, and smooth Agent UX.
- Authorization is not a core subsystem; if needed, represent it as a small `interrupt` event.
- User runs one target-side command; rdev handles helper resolution, gateway failover, transport downgrade, reconnect, task delivery, and status explanation.
- Target join must work from join code or join URL; target commands must not require a pre-known session id.
- Events are immutable; endpoint progress is tracked with `received_seq` and `processed_seq` cursors.
- Event and task delivery are at-least-once; implementation must dedupe by stable ids and idempotency keys.
- Gateway failover is automatic only across candidates with the same `authority_id`.
- Target runtimes use relative `lease_ttl_ms`, `renew_after_ms`, and `retry_after_ms`, not target wall-clock expiry decisions.
- Large artifacts use resumable references with SHA-256, not inline JSON.
- Endpoint requests after join use lease-secret authentication; Agent-facing status must never expose lease secrets.
- Event append is idempotent for `session_id + from_endpoint_id + idempotency_key`.
- Endpoint event visibility is role-aware and does not require channel objects.
- Flow-control and all API/MCP errors use structured `rdev.error.v1`.
- Session snapshots must contain enough state to recover from compacted event history.
- Gateway assigns event ids, seq, and created_at; client append batches receive contiguous seq values.
- Reusing an idempotency key with different payload returns `idempotency_conflict`.
- Task routing must respect endpoint capabilities and explicit/default target selection.
- Terminal sessions reject new joins/tasks while allowing bounded final result/artifact upload.
- Lease expiry enters reconnecting grace before session failure.
- Previous lease secrets are accepted during reconnect grace only for same-endpoint resume/renewal.
- Visibility-filtered event streams may have seq gaps; cursors advance over visible seq without being blocked by hidden events.
- Idempotency records outlive event body compaction for the readable session retention window.
- Do not add hidden persistence or bypass local OS/enterprise security controls.
- Do not expose target hosts through inbound public ports by default.
- Use TDD for behavior changes.
- Prefer pure tests and fake transports over local listener tests.

---

### Task 1: Simple Control Plane Kernel

**Files:**
- Create: `internal/controlplane/session.go`
- Create: `internal/controlplane/session_test.go`

**Interfaces:**
- Produces: `const SessionSchemaVersion = "rdev.session.v1"`
- Produces: `const EndpointSchemaVersion = "rdev.endpoint.v1"`
- Produces: `const LeaseSchemaVersion = "rdev.lease.v1"`
- Produces: `const EventSchemaVersion = "rdev.event.v1"`
- Produces: `const TaskSchemaVersion = "rdev.task.v1"`
- Produces: `type Session`
- Produces: `type Endpoint`
- Produces: `type Lease`
- Produces: `type Event`
- Produces: `type Task`
- Produces: `type ArtifactRef`
- Produces: `type StatusSummary`
- Produces: `func NewSession(spec SessionSpec, now time.Time) (Session, error)`
- Produces: `func (s Session) WithEndpoint(endpoint Endpoint, now time.Time) Session`
- Produces: `func (s Session) WithEvent(event Event, now time.Time) Session`
- Produces: `func (s Session) DeriveStatus() StatusSummary`

- [ ] Write failing tests:
  - `TestNewSessionUsesV1SchemaAndFastSnapshotFields`
  - `TestWithEventAdvancesLastSeqAndDerivesHelperProgress`
  - `TestDeriveStatusReportsGatewaySwitchingAsRecoverable`
  - `TestDeriveStatusReportsTransportDegradedAsOnline`
  - `TestEventIsImmutableAndEndpointCursorsTrackProgress`
  - `TestLeaseUsesRelativeDurationsForWrongTargetClocks`
  - `TestLeaseSecretIsEndpointOnlyAndStatusRedactsIt`
  - `TestStructuredErrorCarriesAgentAndUserRecoveryFields`
  - `TestSessionSnapshotIncludesLimitsAndReconnectGrace`
  - `TestTaskStateTransitionsRejectInvalidMoves`
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/controlplane -count=1`
- [ ] Implement minimal immutable-style types and status derivation.
- [ ] Run the same test command and confirm pass.

### Task 2: In-Memory Session Store

**Files:**
- Create: `internal/controlplane/store.go`
- Create: `internal/controlplane/store_test.go`

**Interfaces:**
- Consumes: Control Plane v1 types from Task 1.
- Produces: `type MemoryStore`
- Produces: `func NewMemoryStore(clock func() time.Time) *MemoryStore`
- Produces: `func (s *MemoryStore) CreateSession(spec SessionSpec) (Session, error)`
- Produces: `func (s *MemoryStore) JoinByCode(joinCode string, spec EndpointSpec) (Session, Endpoint, Lease, []Event, error)`
- Produces: `func (s *MemoryStore) JoinSession(sessionID string, spec EndpointSpec) (Session, Endpoint, Lease, error)`
- Produces: `func (s *MemoryStore) AppendEvent(sessionID string, event Event) (Event, error)`
- Produces: `func (s *MemoryStore) EventsAfter(sessionID string, cursor EventCursor, limit int) ([]Event, Lease, EventReplayState, error)`
- Produces: `func (s *MemoryStore) SubmitTask(sessionID string, spec TaskSpec) (Task, Event, error)`
- Produces: `func (s *MemoryStore) CompleteTask(sessionID, taskID string, result map[string]any) (Task, Event, error)`
- Produces: `func (s *MemoryStore) UpsertArtifact(sessionID string, ref ArtifactRef) (ArtifactRef, Event, error)`

- [ ] Write failing tests:
  - `TestJoinByCodeReturnsSessionEndpointLeaseAndInitialEvents`
  - `TestJoinSessionIsIdempotentForEndpointIdentity`
  - `TestSingleTargetJoinPolicyRejectsDifferentTarget`
  - `TestEventsAfterReturnsReplayAndPiggybackLease`
  - `TestEventsAfterAppliesEndpointVisibility`
  - `TestVisibilityFilteredSeqGapsDoNotBlockEndpointCursor`
  - `TestEventsAfterOldSequenceRequiresSnapshot`
  - `TestAppendEventIsIdempotentForEndpointAndKey`
  - `TestIdempotencySurvivesEventBodyCompaction`
  - `TestSubmitTaskAppendsTaskEvent`
  - `TestRepeatedTaskEventDoesNotCreateSecondAttempt`
  - `TestCancelTaskUsesTaskEventAndIsIdempotent`
  - `TestCompleteTaskAppendsResultEvent`
  - `TestCompleteTaskIsIdempotentForAttemptAndKey`
  - `TestUpsertArtifactResumesOffsetAndVerifiesHash`
  - `TestPayloadAndBatchLimitsReturnStructuredErrors`
  - `TestAppendEventBatchAssignsContiguousServerSequences`
  - `TestIdempotencyKeyWithDifferentPayloadReturnsConflict`
  - `TestTaskRoutingUsesDefaultTargetAndCapabilityMatch`
  - `TestTerminalSessionRejectsNewTasksButAcceptsFinalResultDuringGrace`
  - `TestExpiredLeaseEntersReconnectGraceBeforeFailure`
  - `TestPreviousLeaseSecretCanOnlyResumeSameEndpointDuringGrace`
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/controlplane -count=1`
- [ ] Implement mutex-backed store with monotonic session-local event sequence.
- [ ] Run the same test command and confirm pass.

### Task 3: Gateway Session Delegation

**Files:**
- Modify: `internal/gateway/memory.go`
- Modify: `internal/gateway/memory_test.go`

**Interfaces:**
- Consumes: `controlplane.MemoryStore`.
- Produces gateway methods:
  - `CreateSession`
  - `Session`
  - `JoinSessionByCode`
  - `JoinSession`
  - `AppendSessionEvent`
  - `SessionEventsAfter`
  - `SubmitSessionTask`
  - `CompleteSessionTask`
  - `UpsertSessionArtifact`

- [ ] Write failing gateway tests for session create, join by code, idempotent join, lease-secret auth, event replay, contiguous event seq, idempotency conflict, snapshot-required replay, stale replica rejection, task routing/cancellation/result, terminal grace, and artifact resume.
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/gateway -run Session -count=1`
- [ ] Add a session store field to `MemoryGateway`.
- [ ] Delegate session methods to the store.
- [ ] Run focused gateway tests.

### Task 4: HTTP Session API

**Files:**
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/server_test.go`

**Interfaces:**
- Produces:
  - `POST /v1/sessions`
  - `POST /v1/session-joins`
  - `GET /v1/sessions/{session_id}`
  - `POST /v1/sessions/{session_id}/join`
  - `GET /v1/sessions/{session_id}/events?after_seq=N&wait_ms=M&limit=K`
  - `POST /v1/sessions/{session_id}/events`
  - `POST /v1/sessions/{session_id}/tasks`
  - `POST /v1/sessions/{session_id}/tasks/{task_id}/result`
  - `POST /v1/sessions/{session_id}/artifacts`
  - `POST /v1/sessions/{session_id}/close`

- [ ] Write failing HTTP tests for session create, join by code, idempotent join, lease-secret auth, role-aware event visibility, event replay with cursors, idempotent event append conflict, snapshot-required replay, structured errors, task submit/routing, task cancellation/result idempotency, terminal session behavior, and artifact resume.
- [ ] Run focused HTTP tests and confirm failure.
- [ ] Implement route dispatch and JSON handlers.
- [ ] Keep tests focused on session v1, not old experimental routes.
- [ ] Run focused HTTP tests.

### Task 5: MCP Session Tools

**Files:**
- Modify: `internal/contracts/tools.go`
- Modify: `mcp/tools.json`
- Modify: `internal/contracts/tools_test.go`
- Modify: `internal/mcpstdio/server.go`
- Modify: `internal/mcpstdio/server_test.go`

**Interfaces:**
- Produces MCP tools:
  - `rdev.sessions.create`
  - `rdev.sessions.status`
  - `rdev.sessions.events`
  - `rdev.sessions.task`
  - `rdev.sessions.interrupt`
  - `rdev.sessions.artifacts`
  - `rdev.sessions.close`

- [ ] Write failing contract tests proving old experimental host/task authorization tool names are absent and session tools are present.
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/contracts ./internal/mcpstdio -run 'Sessions|Tools' -count=1`
- [ ] Replace tool list and handlers with session-first tools.
- [ ] Return compact Agent-native results with `status`, `agent_next_action`, `user_summary`, `recoverable`, and references.
- [ ] Ensure MCP event responses expose `snapshot_required`, `retry_after_ms`, `last_seq`, and artifact references instead of unbounded logs.
- [ ] Ensure MCP never exposes lease secrets and maps errors to `rdev.error.v1`.
- [ ] Run focused contract/MCP tests.

### Task 6: Host Session Client And Loop

**Files:**
- Modify: `internal/hostcmd/hostcmd.go`
- Modify: `internal/hostcmd/hostcmd_test.go`

**Interfaces:**
- Consumes: HTTP session API from Task 4.
- Produces target loop:
  - join/resume endpoint;
  - receive piggyback lease from event polling;
  - read events from `after_seq`;
  - send `received_seq` and `processed_seq` cursors;
  - execute `task` events;
  - dedupe task execution by `task_id` and `attempt_id`;
  - return `task.result`;
  - emit helper/gateway/transport/status events;
  - switch gateway candidates and downgrade transport automatically.

- [ ] Write failing fake transport test proving host can join with only join code through `/v1/session-joins`.
- [ ] Write failing fake transport test proving host sends event cursors and accepts piggyback lease.
- [ ] Write failing fake transport test proving first gateway failure switches to second candidate only when `authority_id` matches.
- [ ] Write failing fake transport test proving stale replica is rejected when candidate `last_seq` is behind `processed_seq`.
- [ ] Write failing fake transport test proving WSS failure records `transport` degradation and continues on long-poll/poll.
- [ ] Write failing fake transport test proving repeated task events are not executed twice.
- [ ] Write failing fake transport test proving task cancellation stops or reports the running task once.
- [ ] Write failing fake transport test proving lease expiry reconnects during grace without asking the user to rerun the command.
- [ ] Implement minimal session client helpers.
- [ ] Implement session loop for `--once=true` and bounded task execution first.
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/hostcmd -count=1`

### Task 7: Status And Bootstrap Alignment

**Files:**
- Modify: `internal/supportsession/plan.go`
- Modify: `internal/supportsession/plan_test.go`
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/server_test.go`

**Interfaces:**
- Consumes: Control Plane v1 status summaries and events.
- Produces status copy for:
  - helper downloading;
  - helper verifying;
  - gateway switching;
  - online;
  - busy;
  - transport degraded;
  - reconnecting;
  - recovered;
  - waiting;
  - failed.

- [ ] Write failing tests for status summaries from v1 events, including recoverable retry, stale `after_seq`, gateway authority mismatch, structured errors, and redacted lease secrets.
- [ ] Emit bootstrap progress as simple `helper` events.
- [ ] Run support-session and HTTP focused tests.

### Task 8: Verification

**Files:**
- No planned source edits.

- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/controlplane ./internal/gateway ./internal/httpapi ./internal/contracts ./internal/mcpstdio ./internal/hostcmd ./internal/supportsession -count=1`
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./... -count=1`
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go vet ./...`
- [ ] Run: `git diff --check`
- [ ] Build: `GOCACHE=$PWD/.tmp/go-build go build -o .tmp/bin/rdev ./cmd/rdev`
- [ ] Build: `GOCACHE=$PWD/.tmp/go-build go build -o .tmp/bin/rdev-host ./cmd/rdev-host`
- [ ] Build: `GOCACHE=$PWD/.tmp/go-build go build -o .tmp/bin/rdev-bootstrap ./cmd/rdev-bootstrap`
