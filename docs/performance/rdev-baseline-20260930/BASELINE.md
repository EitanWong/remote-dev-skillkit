# Rdev isolated baseline — measured, not release PASS

Task t_f44014f8; baseline production source SHA `9af806f0ffd0f6e82e0e8040e86be14c9d9d7484`.
Toolchain: go version go1.26.5 linux/amd64; synthetic data seed 20260930. Fixture event timestamps frozen at 2026-09-30T00:00:00Z; ephemeral signing keys/lease secrets random and never logged.

## Scope and reproduction

`python3 scripts/perfdiag/run_baseline.py --out <absolute-output-directory> --scratch <absolute-profile-scratch>`

No online connection, production state, actual host or production implementation changes. The harness uses real MemoryGateway/FileStateStore and nonpreview event-poll HTTP handlers; no alternate persistence implementation. GOMAXPROCS=2, GOMEMLIMIT=512MiB soft runtime limit, go -p=1, GOPROXY/GOSUMDB off. Shared Linux host has 4 logical AMD EPYC CPUs; environment.json carries memory/cgroup facts. Timing setup and verification excluded from benchmem; whole-command profiles include fixture setup/verification and must not be read as pure per-operation percentages. HTTP direct-handler timings exclude TCP/network/MCP/host and use zero long-poll wait. No task dispatch/ACK/result measurement is claimed.

Three repetitions of 3 audit sizes × 2 active endpoint counts = 18 main scenarios, 1080 requests and exactly 1080 persist calls. One client = one synthetic session = one endpoint; endpoint-directory count is not used as an active-client proxy. Workload is closed-loop burst with 2 polls/client, not a fixed arrival rate or production duty cycle.

## Go benchmem (three-repeat median; absolute values)

| Seam / history | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| BenchmarkPerfHistory/AuditEvents/1000-2 | 29793 | 98472 | 4 |
| BenchmarkPerfHistory/AuditEvents/10000-2 | 465713 | 966824 | 4 |
| BenchmarkPerfHistory/AuditEvents/20000-2 | 884508 | 1925288 | 4 |
| BenchmarkPerfHistory/LoadSnapshot/1000-2 | 2426374 | 599040 | 4083 |
| BenchmarkPerfHistory/LoadSnapshot/10000-2 | 27267065 | 7829952 | 40091 |
| BenchmarkPerfHistory/LoadSnapshot/20000-2 | 47394932 | 16541504 | 80094 |
| BenchmarkPerfHistory/SaveSnapshot/1000-2 | 1196145 | 249270 | 2038 |
| BenchmarkPerfHistory/SaveSnapshot/10000-2 | 8617145 | 2413936 | 20041 |
| BenchmarkPerfHistory/SaveSnapshot/20000-2 | 17936666 | 4812962 | 40044 |
| BenchmarkPerfHistory/Snapshot/1000-2 | 19959 | 98616 | 8 |
| BenchmarkPerfHistory/Snapshot/10000-2 | 439098 | 966968 | 8 |
| BenchmarkPerfHistory/Snapshot/20000-2 | 800077 | 1925432 | 8 |
| BenchmarkPerfEventOnlySave/1000-2 | 4729291 | 1914735 | 16098 |
| BenchmarkPerfEventOnlySave/10000-2 | 60232252 | 48142116 | 160133 |

Event-only control holds audit=2, sessions=1, endpoints=1 while growing actual status event/idempotency history. It independently tests the control-plane clone/JSON cost. Task history was not independently varied.

## Concurrent nonpreview poll and IO (median of three distributions, not pooled percentiles)

| Audit history | active clients | p95 ms | p99 ms | req/s | allocated B/request | emitted B/persist | /proc write_bytes B/persist | frozen handler budget |
|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 1000 | 20 | 28.421 | 29.705 | 726.85 | 393746.0 | 232249.8 | 233472.0 | WITHIN_SMALL_SAMPLE_ONLY |
| 1000 | 40 | 92.536 | 96.952 | 473.22 | 533054.7 | 304951.5 | 307148.8 | WITHIN_SMALL_SAMPLE_ONLY |
| 10000 | 20 | 257.319 | 259.324 | 80.73 | 2583235.6 | 1672292.7 | 1675161.6 | FAIL |
| 10000 | 40 | 546.777 | 548.936 | 75.38 | 2730641.8 | 1745032.5 | 1747507.2 | FAIL |
| 20000 | 20 | 498.895 | 499.352 | 42.63 | 4985084.8 | 3282290.8 | 3284889.6 | FAIL |
| 20000 | 40 | 1048.691 | 1049.175 | 39.18 | 5132462.9 | 3355033.5 | 3357542.4 | FAIL |

Every poll renews one lease and synchronously calls real FileStateStore.SaveFrom once. Test-only decorator captures save latency, resulting snapshot size, /proc/self/io wchar and write_bytes before/after each save without changing the underlying writer/state locks. Process-level physical IO can include concurrent handler work and kernel block rounding, not exact device fsync durability. Snapshots emitted per second and byte totals are absolute raw metrics. It is not valid to compare these short-burst throughput rates with the operator-reported production sustained ~45MB/s as equivalent workloads.

| Audit history | clients | persists/request | rewrite bytes / net state-file growth | net file growth B | write_bytes B/s |
|---:|---:|---:|---:|---:|---:|
| 1000 | 20 | 1.0 | 3545.8 | 2620 | 169698048 |
| 1000 | 40 | 1.0 | 4655.7 | 5240 | 145350231 |
| 10000 | 20 | 1.0 | 25531.2 | 2620 | 135238126 |
| 10000 | 40 | 1.0 | 26641.7 | 5240 | 131722882 |
| 20000 | 20 | 1.0 | 50111.3 | 2620 | 140018707 |
| 20000 | 40 | 1.0 | 51221.9 | 5240 | 131559049 |

Amplification denominator is net snapshot file-size growth across the measured burst, NOT true logical changed-record bytes; zero/negative net growth is reported as null. Lease grace-secret retention grows the file at fixed test clock. Exact per-persist bytes/count/frequency are in PERSIST_SAMPLES and IO_SUMMARIES JSON/CSV. IO amplification and per-save allocation, not only RSS, are the release-critical finding.

## Process resource observations (not gateway steady-state SLO)

| Command | observed RSS max B | observed PSS max B | sampled user CPU s | sampled system CPU s | FD max |
|---|---:|---:|---:|---:|---:|
| gateway-bench-3 | 73043968 | 71572480 | 27.46 | 2.55 | 6 |
| event-only-bench-3 | 119951360 | 118479872 | 3.01 | 0.31 | 6 |
| http-matrix-3 | 38408192 | 36936704 | 12.12 | 1.62 | 6 |
| profile-SaveSnapshot | 62468096 | 60996608 | 2.20 | 0.22 | 7 |
| profile-LoadSnapshot | 58707968 | 57269248 | 2.39 | 0.16 | 7 |
| profile-http | 39682048 | 38210560 | 3.48 | 0.46 | 7 |

Samples every 100ms measure the direct test binary, not the compiler tree. These are observed maxima/HWM and may miss sub-100ms transients. Test fixtures retain comparison snapshots and validation buffers; none of these small-history observations proves a 700k cold-recovery peak, steady-state RSS p95 or bounded long-term growth. HTTP_SUMMARIES holds real heap-before/after, totalalloc, GC count/pause totals and goroutine counts. Profile inuse_space is sampled test-process memory, not live gateway retained heap. Network bytes are N/A for direct handler calls, not measured zero.

## Evidence and gate status

commands.json preserves every argv/cwd/env/exit/elapsed/cgroup before/after. Raw BENCH_RESULTS, HTTP_REQUESTS, HTTP_SUMMARIES, PERSIST_SAMPLES, IO_SUMMARIES and PROCESS_SAMPLES JSON/CSV are authoritative. Profile .pprof files are synthetic-only local artifacts; text top outputs identify call stacks. Expected diagnostic RED tests have exit1; runner exit0 means evidence collection succeeded, NOT that SLOs passed. Positive fixture/matrix tests verify JSON roundtrip, audit/event count+sequence, session/endpoint/lease restoration and permissions.

This card delivers TASK_PACK A/B baseline portions only. It does NOT supply before/after optimized GREEN, full-scale ~700k/1.4m, >=60-minute baseline/candidate soak, actual MCP/managed-host ACK+RTT, fault-injection durability, migration/rollback, real Windows verification, final candidate/CI or deployment. These remain independent downstream gates. Operator reports SIGKILL was panel-manual, not OOM; kill attribution is supplied context, not independent investigation. The operator-provided ops archive statistical CSV was subsequently read and exact window rate calculated: see OPS_WINDOW.md (21 samples over599.842782s, write delta23,150,555,136B). This supersedes uptime-derived~45MB/s estimates; no production state/profile was accessed.
