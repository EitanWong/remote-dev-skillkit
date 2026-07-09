# Control Plane v1 Design

## Goal

Control Plane v1 is the official communication protocol for Remote Dev Skillkit. Its first principle is that the connection feels fast, stable, and smooth: the user runs one visible command, then rdev handles helper resolution, gateway switching, transport fallback, reconnect, task delivery, and status explanation automatically.

Earlier ticket/host/task/preconnect routes were experimental architecture. They do not define a public contract. The refactored protocol is still v1.

## Design Priorities

1. Stability under weak, strange, and changing networks.
2. Low latency and low round-trip count.
3. Simple implementation on Windows, macOS, Linux, hosted gateways, and local dev gateways.
4. AI Agent-native status and task flow.
5. Broad scenario coverage through small generic primitives.
6. Safety boundaries that do not complicate the hot path.

This is not an authorization-heavy workflow protocol. Authorization, when needed by policy or local constraints, is represented as a small interrupt event. It is not a separate control-plane subsystem.

## Non-Negotiable Boundaries

These boundaries remain because they protect the product shape, not because the protocol is authorization-first:

- no hidden persistence in attended temporary mode;
- no bypass of UAC, sudo, TCC, Gatekeeper, Defender, enterprise firewall, proxy, execution policy, or OS consent prompts;
- no inbound public ports on target machines by default;
- no execution of cached helpers unless checksum matches the signed or gateway-provided expectation;
- no target wall-clock authority for gateway-issued validity decisions.

## Core Shape

Control Plane v1 has five primitives:

1. `Session`: the shared remote-work container.
2. `Endpoint`: anything connected to the session, such as target host, agent, operator, gateway, workspace, adapter bridge, or cloud worker.
3. `Lease`: a short renewable claim that an endpoint is currently alive and allowed to continue.
4. `Event`: the ordered, resumable stream of state changes and commands.
5. `Task`: the unit of work delivered through the event stream.

Everything else is payload, metadata, or artifact reference. The protocol stays simple by avoiding extra root objects for authorizations, channels, capability grants, host registration, task queues, and preconnect records.

## Required Protocol Invariants

These rules close the main real-world failure modes while keeping the protocol small:

- **Join by code:** the target command may start with only a join code or join URL. It must not need a session id.
- **Immutable events:** events are append-only. Delivery progress is tracked by endpoint cursors, not by mutating events.
- **At-least-once delivery:** event and task delivery can repeat after reconnect. Clients must dedupe by stable ids and idempotency keys.
- **Task attempts:** task execution is identified by `task_id` plus `attempt_id`; result submission is idempotent.
- **Single authority per candidate set:** gateway failover only works across URLs that resolve to the same session authority or replicated ledger.
- **Relative time on targets:** target runtimes use gateway-provided relative durations, not target wall-clock expiry decisions.
- **Snapshot compaction:** old event history may compact; clients recover by fetching a session snapshot and resuming from `snapshot_seq`.
- **Artifact references:** large data moves through resumable artifacts, not inline JSON events.
- **One envelope, many transports:** WSS, SSE, long-poll, poll, mesh, provider, and local transports all carry the same event envelope.
- **Lease-secret authentication:** endpoint control-plane requests after join use the current lease secret; secrets are never returned through Agent-facing status.
- **Bounded flow:** event batches, payload size, artifact chunk size, and retry cadence are bounded by server hints.
- **Structured errors:** every API and MCP error includes machine-readable recovery guidance.

## Generic Scenarios

The same primitives must cover:

- attended temporary support;
- managed operator-owned hosts;
- break-glass repair;
- cloud workspaces, Coder, DevPod, devcontainers, and CI workers;
- single-host and multi-host sessions;
- code, shell, PowerShell, file, process, browser, GUI, package, service, and custom adapter tasks;
- multi-agent workflows;
- restrictive proxies, wrong target clocks, sleep/reboot reconnect, lossy tunnels, and offline event replay.

Scenario-specific behavior lives in `profile`, `capabilities`, and task payloads, not in new protocols.

## Session

`Session` is the only root object.

Required fields:

- `schema_version`: `rdev.session.v1`
- `id`
- `join_code`
- `profile`: `attended-temporary`, `managed`, `break-glass`, `workspace-provider`, `ci-worker`, or extension profile
- `status`: `created`, `joining`, `online`, `busy`, `waiting`, `degraded`, `reconnecting`, `recovered`, `closed`, `failed`, or `revoked`
- `reason`
- `capabilities`
- `join_policy`
- `gateway_candidates`
- `selected_gateway_url`
- `endpoints`
- `last_seq`
- `snapshot_seq`
- `authority_id`
- `limits`
- `reconnect_grace_ms`
- `created_at`
- `updated_at`
- `expires_at`

`Session` is optimized for fast status rendering. A reconnecting client should fetch one session snapshot, then resume events from `snapshot_seq` or its endpoint `processed_seq`, whichever is newer and visible to that endpoint.

`join_policy` controls concurrent joins:

- `single-target`: default for attended temporary sessions; a matching endpoint resumes, a different target is rejected with a structured error.
- `multi-target`: allows multiple target/worker endpoints in the same session.
- `agent-only`: only agent/operator endpoints may join.

The policy is part of the session snapshot so Agents can explain why a repeated join resumed or was rejected.

## Endpoint

`Endpoint` replaces host registration as a generic concept.

Required fields:

- `schema_version`: `rdev.endpoint.v1`
- `id`
- `session_id`
- `role`: `target`, `agent`, `operator`, `gateway`, `worker`, `workspace`, or `adapter`
- `name`
- `platform`
- `identity_fingerprint`
- `capabilities`
- `state`: `joining`, `online`, `busy`, `degraded`, `reconnecting`, `offline`, `closed`, or `revoked`
- `transport`: `wss`, `sse`, `long-poll`, `poll`, `mesh`, `provider`, or `local`
- `received_seq`
- `processed_seq`
- `last_seen_at`

Endpoint join must be idempotent. If the same target reconnects after sleep, reboot, tunnel EOF, gateway switch, or helper restart, the gateway resumes the endpoint instead of creating a confusing duplicate.

The idempotent endpoint key is:

- `session_id`
- `role`
- `identity_fingerprint`
- `platform`

If an endpoint has no stable identity yet, the gateway may create a temporary endpoint, but the full runner must replace it with a stable identity before receiving task events.

## Lease

`Lease` keeps liveness cheap and gateway-authoritative.

Required fields:

- `schema_version`: `rdev.lease.v1`
- `id`
- `session_id`
- `endpoint_id`
- `generation`
- `secret`
- `transport`
- `selected_gateway_url`
- `renew_after`
- `expires_at`
- `server_time`
- `lease_ttl_ms`
- `renew_after_ms`
- `retry_after_ms`

Lease renewal should piggyback on event polling where possible. A target should not make a separate renewal call when an event request can refresh liveness.

Target runtimes must treat `expires_at` as display/debug metadata only. The operational timers are `lease_ttl_ms`, `renew_after_ms`, and `retry_after_ms` because target system clocks may be wrong.

After join, endpoint requests that read or mutate endpoint state use:

```text
Authorization: Bearer <lease.secret>
```

Lease secrets may rotate on renewal. Host/target responses may include the new secret only to that endpoint. Agent-facing status, session snapshots, MCP results, logs, events, artifacts, and audit exports must not include lease secrets.

During `reconnect_grace_ms`, the gateway may accept the previous lease secret only for resume, event polling, and lease renewal by the same endpoint identity. It must not accept an expired lease secret for creating new tasks or joining a different endpoint.

## Event

`Event` is the ordered stream. It carries progress, task offers, task results, artifacts, reconnect state, and small interrupts.

Required fields:

- `schema_version`: `rdev.event.v1`
- `id`
- `session_id`
- `seq`
- `type`
- `from_endpoint_id`
- `to_endpoint_id`
- `task_id`
- `idempotency_key`
- `payload`
- `created_at`

Built-in event types:

- `hello`
- `helper`
- `gateway`
- `transport`
- `status`
- `task`
- `task.result`
- `artifact`
- `interrupt`
- `close`

The type list is intentionally short. Details go in payload:

- `helper` payload can say `using-cache`, `downloading`, `verifying`, or `ready`.
- `gateway` payload can say `trying`, `selected`, or `failed`.
- `transport` payload can say `wss-failed`, `long-poll-selected`, or `poll-degraded`.
- `interrupt` payload can represent a lightweight authorization, local consent prompt, policy pause, or user stop.

Events must support replay from `after_seq`. Missing events should be recoverable by fetching a compact session snapshot.

The gateway assigns `id`, `seq`, and authoritative `created_at`. Clients provide `type`, endpoint ids, task id, idempotency key, and payload. When a client appends a batch, the gateway assigns contiguous seq values in request order.

Event append idempotency is mandatory. For the same `session_id`, `from_endpoint_id`, and `idempotency_key`, the gateway must return the original event and must not allocate a new `seq`. If the repeated request uses the same idempotency key with a different semantic payload, the gateway returns `idempotency_conflict`.

Idempotency records must live at least as long as the session event retention window. Compaction may remove old event bodies, but it must not allow a duplicate idempotency key to create a second event while the session is still readable.

### Event Visibility

Endpoint event reads use simple visibility rules:

- target/worker/adapter endpoints receive broadcast events where `to_endpoint_id` is empty plus events addressed to their own endpoint id;
- agent/operator endpoints may read all session events unless the deployment policy narrows visibility;
- gateway/internal events used only for state compaction may be omitted from endpoint reads if the session snapshot already reflects them.

These rules keep multi-agent and multi-host sessions predictable without adding channel objects.

Visibility-filtered event reads may contain seq gaps. Endpoint cursors therefore mean "highest visible seq returned/processed by this endpoint." The gateway treats non-visible events as implicitly skipped for that endpoint; hidden events must not prevent cursor advancement.

### Endpoint Cursors

Event acknowledgement is cursor-based:

- `received_seq`: highest contiguous event seq durably received by an endpoint.
- `processed_seq`: highest contiguous event seq fully processed by an endpoint.

Clients send cursors on event polling/appending. The gateway stores cursors per endpoint and may return an updated lease in the same response.

There is no per-event ack mutation on the event itself. This avoids conflicting acknowledgements across multiple agents, hosts, and workspace workers.

Cursors are monotonic per endpoint. Lower cursor values in a later request are ignored unless they reveal client state loss, in which case the gateway returns `snapshot_required` or `stale_cursor` with recovery guidance.

## Task

`Task` replaces task polling. It is deliberately small.

Required fields:

- `schema_version`: `rdev.task.v1`
- `id`
- `session_id`
- `target_endpoint_id`
- `target_selector`
- `adapter`
- `intent`
- `capabilities`
- `payload`
- `limits`
- `attempt_id`
- `idempotency_key`
- `status`: `queued`, `offered`, `running`, `paused`, `succeeded`, `failed`, or `canceled`
- `created_at`
- `started_at`
- `ended_at`

Tasks are delivered by a `task` event. Results return as `task.result` events. Artifacts return as `artifact` events with references and hashes, not giant inline logs.

Task payloads are adapter-defined, but all adapters must support concise evidence output and bounded logs.

Task delivery is at-least-once. Targets must dedupe task execution by `task_id` and `attempt_id`. The gateway must dedupe result submission by `task_id`, `attempt_id`, and `idempotency_key`.

By default, an endpoint executes one task at a time. Parallel execution requires an explicit task payload flag and must not be the default because workspaces, package managers, shells, and GUI adapters often have shared mutable state.

Task cancellation uses the same event stream. The gateway emits a `task` event with payload action `cancel`, `task_id`, and `reason`. The target makes a best-effort cancellation, then returns a `task.result` event with status `canceled`, `failed`, or `succeeded` depending on what actually happened. Cancellation is idempotent and may be repeated after reconnect.

### Task Routing And State

Task routing is explicit but concise:

- `target_endpoint_id` routes to one endpoint.
- `target_selector` routes to the best online endpoint matching role, platform, and capabilities.
- if both are empty, the gateway selects the default online target for single-target sessions.

The gateway must not offer a task to an endpoint whose capabilities do not cover the task capabilities. If no matching endpoint is online, task status remains `queued` or the API returns `capability_unavailable`, depending on the request mode.

Allowed task transitions:

- `queued -> offered`
- `offered -> running`
- `running -> paused`
- `paused -> running`
- `queued|offered|running|paused -> canceled`
- `running|paused -> succeeded`
- `running|paused -> failed`

Terminal task states are `succeeded`, `failed`, and `canceled`. Terminal tasks may accept idempotent duplicate results, but not new attempts unless the gateway creates a new task id.

## Terminal Session And Reconnect Semantics

Session terminal states are `closed`, `failed`, and `revoked`.

After a session becomes terminal:

- new joins are rejected;
- new tasks are rejected;
- new non-result events are rejected except duplicate idempotent appends;
- already-running endpoints may upload final `task.result` and artifact references during a bounded terminal grace period;
- event reads and snapshots remain available until retention expiry.

Lease expiry does not immediately make a session terminal. The endpoint becomes `reconnecting` until `reconnect_grace_ms` elapses. During grace, the endpoint may resume with the same identity and receive a new lease without user action.

## Artifact Transfer

Artifacts are not root control-plane objects, but the protocol defines the metadata and transfer rules because weak networks are common.

Artifact metadata fields:

- `schema_version`: `rdev.artifact-ref.v1`
- `id`
- `session_id`
- `task_id`
- `kind`
- `name`
- `size_bytes`
- `sha256`
- `content_type`
- `upload_offset`
- `complete`

Large artifact upload must support resume by offset and final SHA-256 verification. Event payloads should carry artifact references, not large inline stdout, screenshots, diffs, or binary data.

Recommended default limits:

- event payload: 64 KiB;
- event batch: 100 events;
- artifact chunk: 1 MiB;
- inline task result summary: 32 KiB.

Gateways may advertise smaller or larger limits in session snapshots and event responses. If a client exceeds a limit, the error must be structured and recoverable where possible.

## Gateway Candidate Authority

Gateway candidates are alternative URLs for the same control-plane authority, not arbitrary fallback servers.

Each candidate set must carry:

- `authority_id`
- `url`
- `priority`
- `kind`

Failover is automatic only when candidates share the same `authority_id` or explicitly advertise replicated session ledger support. A Quick Tunnel URL, LAN URL, loopback URL, hosted gateway URL, or named tunnel may all be valid candidates when they front the same gateway/session store.

If a candidate points to a different authority, rdev must report a clear error instead of silently creating a second session.

Replicated candidates must not move clients backwards. When reconnecting to a candidate, the candidate's visible `last_seq` must be greater than or equal to the client's `processed_seq`. If not, the gateway returns `stale_replica` with `recoverable=true` and `retry_after_ms`, and the client continues to another candidate or waits.

## Snapshot Contract

`GET /v1/sessions/{session_id}` returns a compact snapshot sufficient to recover without replaying compacted events.

Required snapshot content:

- session fields, including `last_seq`, `snapshot_seq`, `authority_id`, and `join_policy`;
- endpoints with cursor state and current transport;
- active and recently terminal tasks;
- artifact references;
- latest status summary;
- gateway candidates and selected gateway URL;
- limits, reconnect grace, and retry hints.

If a client requests events with an `after_seq` older than `snapshot_seq`, the gateway returns `snapshot_required=true`; the client fetches the snapshot and resumes from `snapshot_seq`.

## HTTP API

The semantic API is session-first:

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

There is no public contract requirement for old experimental host registration, task polling, or task authorization routes.

`POST /v1/session-joins` accepts a join code and endpoint metadata. It returns `session`, `endpoint`, `lease`, and the first event batch. This is the primary target-side entrypoint because the one-command bootstrap should not need to know `session_id` beforehand.

`GET /v1/sessions/{session_id}/events` accepts these optional query parameters:

- `after_seq`
- `received_seq`
- `processed_seq`
- `wait_ms`
- `limit`

The response includes:

- `events`
- `lease`
- `snapshot_required`
- `snapshot_seq`
- `retry_after_ms`

When `snapshot_required=true`, the client fetches `GET /v1/sessions/{session_id}` and resumes from the returned `snapshot_seq`.

## Structured Errors

HTTP and MCP errors use the same shape:

- `schema_version`: `rdev.error.v1`
- `code`
- `message`
- `recoverable`
- `retry_after_ms`
- `user_summary`
- `agent_next_action`
- `details`

Required error codes:

- `invalid_join_code`
- `join_policy_rejected`
- `unauthorized_endpoint`
- `lease_expired`
- `stale_replica`
- `snapshot_required`
- `payload_too_large`
- `too_many_events`
- `artifact_offset_mismatch`
- `checksum_mismatch`
- `task_not_found`
- `task_already_terminal`
- `session_closed`
- `authority_mismatch`
- `idempotency_conflict`
- `capability_unavailable`
- `endpoint_not_found`
- `stale_cursor`
- `terminal_session`

## Transport Strategy

The protocol is transport-independent, but the default priority is:

1. WSS for lowest latency when available.
2. Long-poll as the canonical broad-network fallback.
3. SSE when the deployment supports it well.
4. Short-poll where long-lived connections fail.
5. Mesh/provider/local transports when explicitly configured.

Transport changes are reported as `transport` events. Authority does not change when transport changes.

Speed rules:

- combine lease renewal with event polling;
- allow event append batching;
- use idempotency keys on every mutation;
- support `after_seq`, `wait_ms`, and `limit`;
- send `received_seq` and `processed_seq` cursors with event requests;
- keep hot-path payloads small;
- store large artifacts out-of-band with SHA-256;
- return structured flow-control errors instead of dropping connections silently;
- prefer one session snapshot plus event replay over many object fetches.

## MCP API

MCP is Agent-facing and should be small:

- `rdev.sessions.create`
- `rdev.sessions.status`
- `rdev.sessions.events`
- `rdev.sessions.task`
- `rdev.sessions.interrupt`
- `rdev.sessions.artifacts`
- `rdev.sessions.close`

MCP results must be built for AI Agents:

- stable schema versions;
- compact status;
- `agent_next_action`;
- `user_summary`;
- `recoverable`;
- `retry_after_ms`;
- task and artifact references;
- no unbounded raw logs by default.

## Target Loop

The target runner does this loop:

1. Resolve verified helper.
2. Build signed/operator-provided gateway candidates.
3. Join or resume a session.
4. Open best available transport.
5. Poll or stream events from `after_seq`.
6. Execute task events.
7. Return task result and artifact references.
8. Send `received_seq` and `processed_seq` cursors.
9. Piggyback lease renewal on event polling.
10. On failure, switch gateway or downgrade transport automatically.
11. Continue until close, revoke, expiry, or local stop.

The target user should see progress, not instructions to debug the network.

## Status Semantics

Status responses should compress event history into clear states:

- `joining`
- `helper-downloading`
- `helper-verifying`
- `gateway-switching`
- `online`
- `busy`
- `transport-degraded`
- `reconnecting`
- `recovered`
- `waiting`
- `failed`
- `closed`

Each status includes:

- `status`
- `user_summary`
- `agent_next_action`
- `selected_gateway_url`
- `transport`
- `last_seq`
- `snapshot_seq`
- `latest_event`
- `recoverable`
- `retry_after_ms`

## Extension Rules

Extensions must not add new root protocols. They may add:

- session profiles;
- endpoint roles;
- adapter task payloads;
- event payload fields;
- artifact kinds;
- transport names.

Unknown optional fields are ignored. Unknown required extensions fail closed with a structured error.

## Tests

Required gates:

- one session snapshot plus event replay reconstructs state;
- endpoint join is idempotent across reconnect;
- event stream resumes from `after_seq`;
- endpoint cursors track received and processed seq without mutating events;
- lease renewal piggybacks on event polling;
- target timing uses relative lease and retry durations;
- helper download/verifying reports progress;
- first gateway failure switches to the next candidate without user action;
- gateway failover rejects candidates with different authority ids;
- WSS failure downgrades to long-poll/poll without changing authority;
- repeated task event does not execute the same `task_id` and `attempt_id` twice;
- task result submission is idempotent;
- task event produces task result and artifact reference;
- artifact upload resumes by offset and verifies SHA-256;
- stale `after_seq` returns `snapshot_required`;
- interrupt event pauses only the affected task, not the entire protocol;
- MCP exposes session-first tools only;
- attended, managed, workspace-provider, and multi-host cases fit the same primitives.
