# HANDOFF — t_f44014f8 measured baseline

## Status and scope

MEASURED_BASELINE_WITH_RED; not an optimization/release PASS. Baseline production source SHA9af806f0ffd0f6e82e0e8040e86be14c9d9d7484. Policy-compliant independent branch `perf/53-rdev-baseline-t-f44014f8`, GitHub issue53. Initial `perf/rdev-baseline-t_f44014f8` was retained for audit after its GitPolicy missing-numeric-issue failure; no shared history rewritten. Only tagged perf tests, reproducible Python runners and report/raw numeric evidence. Main repository fix/task-queue-wedge58e9431 left unchanged. Remote branch exact final SHA and same-head CI are recorded in publication receipt after commit/push, not guessed or embedded in its own commit.

## Acceptance delivered

TASK_PACK A/B baseline portions: BASELINE.md/SLO.md/RED_LIST.md/ROOT_CAUSE.md, three-repeat benchmem and closed-loop20/40 active-client histories1k/10k/20k; event-only1k/10k negative control with audit2/client1; CPU/heap/alloc/inuse/block/mutex profiles; raw CSV/JSON and full argv/env/exit receipts; 100ms RSS/PSS/FD/CPU/IO samples; actual per-persist count/size/frequency and /proc/self/io, heap/GC/goroutine metrics; tight hot-write/latency RED and correctness/roundtrip checks. One endpoint/client/session per synthetic client, not directory-online count. No real credentials or production state. Profiles are synthetic-only and kept out of git.

Run: `python3 scripts/perfdiag/run_baseline.py --out <absolute-output> --scratch <absolute-profile-scratch>` then `python3 scripts/perfdiag/summarize.py --raw <output> --out <report-directory>`.

Default bounded sample is agent-runnable in seconds to about one minute; actual final runner exits0 with28 nested commands, three expectedRED command exits1. Red tests are opt-in `-tags=perfdiag`; never interpret runner success or a correctness-only matrix exit0 as SLO PASS. Frozen latency budgets200ms/500ms enforce via PERF_DIAG_ENFORCE_SLO=1. A final candidate must execute the same actual hot-poll seam, not replace it with a shallow save/export-only test.

Full-history script option: `--large --events 350000,700000 --clients 20,40` supports growth toward700k but requires exclusive resource coordination and sufficient measured RAM. It was NOT run here. Go's512MiB limit is soft; runner also terminates if observed direct-test RSS>1GiB or command deadline expires. Such termination is ABORT/UNKNOWN, not evidence ofGo/kernelOOM. Beyond700k, including proposed1.4m growth, requires explicit downstream harness/resource approval/extension; this diagnostic runner deliberately refuses it.

## Verification (real exits)

- final evidence runner: exit0; SaveGrowthBudget/PollWriteAmplificationGrowthBudget/HTTPLatencyBudget each exit1 with exact symptoms. SUMMARY.json declares programmatically verified45 raw bench rows,18 main scenarios1080requests1080persists,1360 total request/persist records including budget/profile windows.
- summarize.py: exit0 with programmatic assertions of repeat/scenario/request/persist counts.
- original `bash scripts/check.sh`: exit1; original `go test -race ./...`: exit1. Failures were symlink-TMPDIR literal vscanonical paths, not perf changes.
- Same perf scratch canonical path `/srv/agent-os/state/hermes/profiles/perf/cache/scratch`, no /tmp relocation, no test-assertion changes and noGitdefaultBranch workaround: `bash scripts/check.sh` exit0; `go test -race -count=1 ./...` exit0.
- New tagged fixture/matrix race on1k/20+40client: exit0.
- Branch/profile metadata and publication receipt identify exact code/harness blobs. Measurement logs predate documentation-only commit; baseline production source is explicitly unchanged.

## Engineering/QA handoff

Prioritize actual persist frequency × full-rewrite bytes. Rootchain confirmed: lease-poll -> synchronousSaveFrom -> cold-history snapshot copy/encoding/full-file rewrite -> globalstateMu wait. Independently event/idempotency history also grows allocation/Encode costs. Merely streaming audit JSON peak is insufficient. ROOT_CAUSE.md has numeric stack evidence and compatibility/safety constraints.

StateStore.SaveFrom returns a full Snapshot; preserving that legacy full-export return semantics while making hot persistence bounded may require a separate hot durable-persist API. Do not force an incompatible shallow API change merely to green SaveGrowthBudget. The unchanged actual nonpreview-poll write/latency RED is the primary release-growth seam; Snapshot export growth remains diagnostic. Architect/coder must settle the persistence/migration API before implementation and reviewer checks the redesigned call-site equivalent seam.

No implementation/candidate GREEN,700k/1.4m execution,>=60-minute baseline+candidate soak, fixed-arrival/rate/longterm load, trueTCP/MCP/managed-hostACK/result/RTT, task-onlyhistory/reconnectstorms, failure injection/fsync/durable migration+rollback, realWindows or deployment is supplied. Existing downstream QA/reviewer graph owns these gates; no newduplicate cards were created. Operator reports panel-manualSIGKILL rather thanOOM; cited supplied context only, not independently re-investigated here.

## Engine execution and CEO-scoped acceptance decision

Codex CLI0.144.6 actual run banner modelgpt-6-astra/providerlunflux; first broad call timeout300 exit124 left a fixture prototype; two bounded followups exit0 created/fixed gateway+HTTP tagged tests. Parent independently compiled/ran all accepted evidence and added IO/event-only instrumentation/runners. CLI model metadata fallback/MCP-auth startup warning preserved in internal logs; no assumption about upstream hidden model routing.

Claude Code2.1.215 analysis requested `--model opus`: first IS_SANDBOX=1 --print with allowedRead150s timeout exit124, second no-tools/numeric-only input120s timeout exit124. Both emitted only an error envelope after timeout: subtype=error_during_execution, terminal_reason=aborted_streaming, duration_api_ms=0, empty modelUsage and0input/outputtokens; errors=`[ede_diagnostic] result_type=user last_content_type=n/a stop_reason=null`. No analysis or actual model receipt. Claude analysis status is FAILED / effective analysis NOT_RUN, actual Claude model UNKNOWN. Two failures escalated; no third same-path request. At2026-09-30 16:04 Beijing time, Lucky/CEO explicitly approved this baseline-only card to deliver verified commands/raw data and this worker's own analysis, with existing independent reviewer t_1b876885 deciding diagnostic sufficiency. This scoped decision is NOT a dual-engine PASS and does NOT relax Coder's actual Codex+Claude, final same-SHA Tester/Reviewer/CI or>=60minsoak release requirements. See CEO_DECISION.md; the two error envelopes remain raw evidence.
