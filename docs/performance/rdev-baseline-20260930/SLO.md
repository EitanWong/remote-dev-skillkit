# Rdev frozen performance budgets and measurement contract

Task: t_f44014f8. This document is a baseline/diagnostic deliverable, NOT a release PASS. Baseline source: origin/main 9af806f0ffd0f6e82e0e8040e86be14c9d9d7484, verified by fetch and ls-remote. No production data, production profiles, real credentials, deployment, or implementation changes are allowed here.

## Frozen release budgets (from TASK_PACK; not adjusted by this worker)

- Representative 20 active endpoints, explicitly distinguished from endpoint-directory count and session count.
- Steady-state RSS p95 <= 512 MiB; observed peak <= 1 GiB INCLUDING cold recovery and snapshot. PSS, Go heap/in-use/allocations, GC pauses/count, goroutines, FD, CPU, IO and cgroup events must also be recorded. No continuous unbounded growth.
- Local non-long-poll server handling p95 <= 200 ms, p99 <= 500 ms. Long-poll intentional waiting, network RTT and task execution are separate distributions.
- Task dispatch to ACK (excluding task execution) p95 <= 2 s, p99 <= 5 s. A handler-only test cannot substitute for gateway -> MCP -> actual managed host evidence.
- Stable observation window: zero crashes, unplanned restarts, lost tasks, data corruption. Intentional kill/failure-injection windows are separate and never included in stability PASS.
- Baseline and final candidate: identical machine/load/toolchain, >=3 short repetitions plus >=60-minute soak, history near 700,000 events, 1x/2x active load, history growth. Final candidate, independent tester/reviewer and CI must bind the same SHA.
- CPU/IO/network absolute quantities, baseline comparison and regression gates are required. No passing by dropping audit/state, weakening auth, raising memory without evidence, or forcing GC in the measured operation.

## This diagnostic phase

- Current shared host: 4 logical CPUs; go1.26.5 linux/amd64. Initial MemAvailable approximately 2.05 GiB; worker cgroup memory.max 3,918,663,680 bytes. Shared host available RAM, not worker maximum, is the practical limit.
- Small-history baseline first: 1,000 / 10,000 / 20,000 synthetic audit events; fixed event-data seed 20260930 and fixed test clock. Generated ephemeral keys stay in memory and are never logged. Large histories require an explicit opt-in and exclusive resource coordination; never silently run 700k on this shared machine.
- GOMAXPROCS=2, go build/test -p=1, GOMEMLIMIT=512MiB (a soft Go runtime limit, NOT a kernel hard cap). Avoid overlapping heavy benchmarks; inspect MemAvailable before each large step. Record actual command, exit code, scope, source SHA, environment and seed in raw receipts.
- Microbenchmarks: real MemoryGateway snapshot, audit read, file save and file load seams, setup excluded. JSON streaming and whole-history copies are distinguished by CPU/heap profiles. Allocation-growth assertions are deterministic diagnostic ratchets rather than timing-based production SLO claims.
- Initial diagnostic growth budget: 10x audit history must not cause >2x bytes allocated per hot state mutation/persistence operation. Full-history archive/export APIs intrinsically materialize full results and are not interchangeable with hot-path persistence. Any export-growth RED must be labeled diagnostic rather than a standalone release requirement.
- Active-load scenarios: 20 endpoints/20 sessions and 40 endpoints/40 sessions, concurrent in-process HTTP handler calls with persistent FileStateStore and identical operation mix. Handler timing excludes network RTT. Such tests are synthetic isolation, not 20/40 real hosts, not MCP E2E. Report request count, duration and achieved request rate; do not label closed-loop concurrency an externally fixed arrival rate.

## Proposed ratchets for Lucky/reviewer approval

Preserve all frozen absolute budgets. For unchanged environment/load, compare three-repeat medians for ns/op, B/op, allocs/op, CPU and IO/request: any regression must be investigated, not averaged away. Suggested alert threshold >5% for time/CPU/IO (noise allowance, NOT permission to declare a known slowdown acceptable); allocation ratchets use exact baseline counts/bytes. For hot memory and single-write work, require bounded dependence on growing cold history. Validate slope over at least 100k/700k/1.4m under dedicated QA resources; no extrapolation from a small sample counts as full-scale PASS.

## Required downstream evidence

700k and larger-history matrix, >=60-minute baseline/candidate soak, real HTTP/TCP and MCP/host ACK/result/RTT decomposition, cold recovery peak, fault recovery/atomicity/fsync, correctness/security/race/full repository gates, Windows real-host compatibility, final candidate and release identity remain independent gates. UNKNOWN is not PASS. Budget changes need Lucky's explicit approval based on evidence before retesting.
