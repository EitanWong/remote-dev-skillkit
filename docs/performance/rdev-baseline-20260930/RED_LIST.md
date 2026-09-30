# RED list (baseline-only; no implementation changes)

Baseline: 9af806f0ffd0f6e82e0e8040e86be14c9d9d7484. All generated data synthetic, seed 20260930. Tagged perfdiag tests are opt-in diagnostic ratchets. Default CI must not run expected-RED tests. No candidate GREEN is claimed.

## Observed before-fix evidence

- EXPORT-COPY diagnostic: initial prototype `TestPerfHistorySnapshotGrowthBudget` was actually run three times by the parent and exited1: allocation ratios9.8057/9.8054/9.8054 for1,000->10,000 audit events,98,616->966,968..967,001B/op. This was full-export mechanism evidence, not a standalone production requirement. Final runnable test is `TestPerfHistorySnapshotGrowthDiagnostic` and reports growth without failing an inherently materializing export API. The hot `SaveGrowthBudget` and poll rewrite RED below remain strict and unweakened.
- FIXTURE-REPRESENTATION: initial reflect.DeepEqual roundtrip check failed on all sizes; unrelated harness bug, NOT counted as performance RED. Canonical JSON equivalence plus explicit count/sequence/time/permission checks must pass before save/load timing is accepted.

## Real hot-path RED, executed by parent on final harness

Canonical environment prefix: `TMPDIR=<canonical-perf-scratch> GOCACHE=<canonical-perf-scratch>/go-build GOMAXPROCS=2 GOMEMLIMIT=512MiB`.

1. `go test -p=1 -tags=perfdiag ./internal/gateway -run '^TestPerfHistorySaveGrowthBudget$' -benchtime=100ms -count=3 -timeout=60s -v`: exit1, ratios9.6856/9.6849/9.6858 against2x. Fixture/roundtrip/permission checks passed before RED.
2. `go test -p=1 -tags=perfdiag ./internal/httpapi -run '^TestPerfHTTPPollWriteAmplificationGrowth$' -count=3 -timeout=60s -v`: exit1 three times,163239->1603242B written snapshot per identical nonpreview poll.
3. `PERF_DIAG_EVENTS=20000 PERF_DIAG_CLIENTS=20 PERF_DIAG_POLLS=2 PERF_DIAG_ENFORCE_SLO=1 go test -p=1 -tags=perfdiag ./internal/httpapi -run '^TestPerfHTTPActiveHistoryMatrix$' -count=3 -timeout=60s -v`: exit1 on frozen200ms/500ms handling budgets. This is actual handler timing, not intentional long-poll wait or hostACK. Raw per-request/profile data retained.
4. Event-only control (audit2,client1): three-repeat `BenchmarkPerfEventOnlySave` shows1,914,735->48,142,116B/op when events/idempotency1000->10000; bench exits0 because it measures, not because growth is compliant. CP clone/Encode profile confirms independent cost.

One full feedback/evidence command: `python3 scripts/perfdiag/run_baseline.py --out <absolute-output> --scratch <absolute-perf-scratch>`; actual exit0,28 underlying command receipts include three real RED command exits1. Runner validates exact RED symptom and rejects missing-test matches/unrelated failures; runner success is NOT release PASS.

## Profile-backed mechanisms and strict downstream ratchets

- SAVE-COPY: real FileStateStore.SaveFrom B/op growth for 1,000 -> 10,000 audit history, exact audit preservation and file permissions. Fixed >2x threshold must fail on whole-history hot persistence; timing remains diagnostic.
- POLL-REWRITE: actual nonpreview HTTP event poll renews lease and persists; with identical one-client session and history growth, rewrite bytes per poll must not scale with full cold audit. File bytes proxy counts bytes emitted by current full-rewrite implementation; also capture /proc IO and call stacks. Not task ACK.
- CONTENTION: 20 vs 40 synthetic active endpoints/sessions at same audit history, concurrent non-long-poll handler calls, CPU/heap/block/mutex profiles. Published p95/p99 are local handler-only distributions; no TCP/MCP/host claim.
- CP-HISTORY: event-only history at fixed audit/client counts. Profile deep clone and control_plane JSON encode independently from audit streaming.
- COLD-LOAD: real LoadSnapshot/LoadInto cold recovery allocation and RSS/PSS peak. Do not confuse total allocated bytes with retained heap or production OOM cause.

## Ranked falsifiable hypotheses (after observed EXPORT-COPY RED)

1. Audit full-copy/full-rewrite: audit-only growth increases SaveFrom allocation and bytes, CPU/alloc profile points at snapshotLocked/writeSnapshotFile.
2. Control-plane full-copy/whole JSON: event-only growth at fixed audit increases SaveFrom allocations with cloneEvents/cloneJSONValue/encoding/json stack evidence.
3. Global persistence serialization: doubling active concurrency at fixed history grows waiting; block/mutex profile identifies stateMu in persistStateInternal, distinguished from fixture/harness waits.
4. Cold restoration materialization: LoadSnapshot allocations exceed hot snapshot and profile shows ReadFile/json.Unmarshal/RestoreSnapshot copies. This alone cannot attribute online SIGKILL.

## Unknown or downstream-only (never fake PASS)

Full-scale ~700k history/1.4m growth, >=60-minute baseline/candidate soak, real-host ACK/result semantics and RTT split, fault-injection durability/fsync/rollback, production SIGKILL attribution, final candidate GREEN and independent QA/reviewer/CI remain open. Host submission HTTP 202 and health 200 are not ACK or availability proof.
