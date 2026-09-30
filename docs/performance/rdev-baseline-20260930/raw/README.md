# Raw evidence packaging

All data is synthetic, generated with seed20260930 and the real baseline gateway methods. JSON/CSV are actual numeric records, not fabricated fixtures/results. commands.json preserves executable argv/cwd/env/exit and cgroup snapshots. Go stdout/profile top and verification logs in this Git folder have ONLY trailing whitespace/newline formatting normalized for git diff --check; byte-for-byte original stdout logs plus synthetic .pprof are preserved in the durable evidence bundle. CSV CRLF->LF conversion was checked for identical parsed rows, and generators now useLF. No production state/profile, secret, test binary or core dump is committed.

RUN_METADATA.json hashes the actual measured test/runner version before this CSV-only generator formatting change; Go harness contents remained unchanged. A further bounded LF-output runner/summarizer smoke exercises the final generator. Publication receipt identifies the later documentation/test-only commit; do not equate baseline9af806f with an optimized implementation.

ENGINE_RECEIPT.json reports real outcomes. Claude error envelopes have no analysis or modelUsage and are not successful reviews. The two Claude timeouts remain escalated, not silently replaced with a claimed Claude report.
