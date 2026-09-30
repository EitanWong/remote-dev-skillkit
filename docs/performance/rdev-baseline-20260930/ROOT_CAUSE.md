# Measured root-cause ranking and implementation handoff

Task t_f44014f8; baseline production source 9af806f0ffd0f6e82e0e8040e86be14c9d9d7484. All measurement below is synthetic isolated execution, not production attribution. No production implementation changed and no optimized GREEN supplied by this card.

## 1 — Hot poll persistence rewrites cold audit history (CONFIRMED locally)

A minimal real HTTP nonpreview poll, fixed one session/one endpoint and no new application event, rewrites a complete snapshot. History 1,000 -> 10,000: emitted snapshot 163,239 -> 1,603,242 bytes; three independent executions give the same values and FAIL the fixed 2x growth budget. Request status, fresh lease generation/binding, audit sequence, restored sessions/events and 0600 permissions are verified before reporting RED. This seam is the user's hot-write pattern rather than an archival/export API assertion.

In the main matrix, 1,080 polls produce exactly 1,080 real SaveFrom calls (one per poll); including latency gate and profile windows, there are 1,360 per-persist numeric records. At 20,000 historical audit /20 clients, three-repeat medians: emitted 3,282,290.8 B/persist, /proc/self/io write_bytes 3,284,889.6 B/persist, wchar 3,282,290.8 B/persist. Net file growth across the 40-request burst is only 2,620 bytes; emitted/net-growth ratio is 50,111.3. This denominator is net file-size growth, not logical changed-record bytes. Physical process IO is sampled around each SaveFrom and includes kernel block rounding/concurrent process work; it is not device fsync proof.

Call chain: HTTP sessionEventsAfter -> persistState -> persistStateInternal -> FileStateStore.SaveFrom -> SaveSnapshot -> Snapshot/snapshotLocked -> writeSnapshotFile. Existing audit streaming lowers serialization-buffer peak but still encodes every audit record and rewrites the full file on each renewal. Source: internal/httpapi/server.go:398-403,822-827; internal/gateway/snapshot.go:106-121,153-228.

## 2 — Whole-history allocation/deep clone, independently audit and event (CONFIRMED locally)

Real SaveFrom B/op growth RED at fixed 1,000 ->10,000 audit: ratios 9.6856 /9.6849 /9.6858 against fixed 2x budget, three exits1. SaveSnapshot bench medians: 249,270 ->2,413,936 ->4,812,962 B/op for 1k/10k/20k audit. Save time 1.196 /8.617 /17.937 ms/op. Audit read and Snapshot export full copies are mechanism diagnostics only; their full-result APIs must not be misrepresented as standalone SLO failures.

The event-only negative control holds audit=2, endpoint=1, session=1. Increasing actual status event/idempotency history 1k ->10k changes SaveSnapshot allocations 1,914,735 ->48,142,116 B/op and time 4.729 ->60.232 ms/op (three-repeat medians). This disproves an audit-only explanation. Exact slope/ratio is synthetic payload/toolchain dependent; do not extrapolate 10k to700k or call it a proven asymptotic complexity bound.

HTTP allocation profile: snapshotLocked flat298.79MB(36.81%), writeSnapshotFile flat298.53MB(36.78%), time.Time.MarshalJSON149.01MB(18.36%). Event-only alloc profile: controlplane.cloneMap278.57MB(26.00%), bytes.growSlice256.47MB(23.94%), MemoryStore.Snapshot cumulative323.95MB(30.24%), Encoder.Encode cumulative353.97MB(33.04%). Call stacks support audit slice copying, CP events/idempotency deep copies, and whole control_plane encoding. These are whole-command sampled profiles including setup/verification, not mutually additive exclusive operation costs.

## 3 — Global persistence lock serializes client bursts (CONFIRMED locally)

20k audit/20 active clients: median handler p95=498.895ms >200ms; 40 clients: p95=1,048.691ms and p99=1,049.175ms >500ms. Three repeat fixed 20-client frozen-latency assertions also exit1; exact logs preserved. These direct-handler requests have wait_ms=0, so intentional long-poll waiting and network/host execution are not inside this distribution.

40-client profile: aggregate block delay124.13s; persistStateInternal cumulative113.72s(91.61%) and Mutex.Lock113.91s(91.76%). Those are aggregate goroutine wait-seconds over a ~3.44s window, NOT elapsed wall time. Mutex profile assigns the contention to persistStateInternal unlock path. Fixture channel/WaitGroup waits are separately visible and are not claimed as production bottlenecks.

Increasing concurrency without eliminating full-history synchronous writes cannot fix this serial IO/work chain. Do not remove locking merely to make the profile green: atomic writes, ordering and durable response semantics must be preserved.

## 4 — Cold recovery materializes file + JSON + restored copies (CONFIRMED cost, production peak UNKNOWN)

20k audit LoadSnapshot median16,541,504 B/op,47.395ms/op;1000 audit599,040 B/op. Load profile shows ReadFile, json.Unmarshal/reflect.growslice and RestoreSnapshot. Measured test RSS/PSS includes setup/validation and retained comparison snapshots; no700k recovery or bounded long-term gateway RSS is inferred. Operator now reports SIGKILL was panel-manual, notOOM. We did not access production or independently obtain ops attachments; do not repurpose kill-9 as heap evidence.

## Proposed implementation direction (not implemented; architect/coder own decisions)

1. Preserve complete durable audit/event history while decoupling cold archive from bounded hot state. Use evidence to decide durable append/change segments and indexing/checkpoints rather than simply deleting/truncating history. Maintain auth, trust, lease grace, cursor, task idempotency, cancellation and replay contracts.
2. Stop lease-only/nonpreview polls from full-rewriting unchanged historical sections. Dirty/change-segment persistence or bounded group commit can be tested with identical seeds/load; exact post-success durability/ordering is mandatory. Do not batch-and-ACK before persistence, omit audit, drop state on restart or force GC to mask growth.
3. Shorten lock critical sections with a design that preserves consistent snapshots and write ordering. Validate incremental write bytes/persist, CPU/allocs/request, latency and concurrency, not just RSS.
4. Specify JSON compatibility, versioned migration, checkpoint+log atomicity/fsync and power-failure behavior, rollback/replay/duplicate semantics before implementation. Failure to restore equivalent task/lease/audit semantics blocks rollout.
5. Rerun the unchanged RED command/fixtures against the candidate on this same toolchain/machine, then independent QA does700k/1.4m growth and >=60-minute baseline/candidate soak under coordinated resources. Explicitly instrument real offer->received->started->result; submitHTTP202 is nothostACK.

## Remaining uncertainties

Task-only growth, reconnection storms/backoff, true TCP/MCP/managed-host RTT andACK, Windows real machine compatibility, disk slow/failure/crash injection, full history resources, >=60min stability and rollout remain UNKNOWN/downstream. No production change, release, deployment or online stability PASS is authorized by this report.
