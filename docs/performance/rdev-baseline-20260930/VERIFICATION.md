# Verification and environment diagnosis

Baseline9af806f; no production code changes. Raw logs and JSON receipts accompany this file.

1. Initial full check and race were actually run with requested profile scratch alias `/opt/data/profiles/perf/cache/scratch`; both exit1. Failures occur when t.TempDir returns that lexical path and Git/path scope helpers return physical `/srv/agent-os/state/hermes/profiles/perf/cache/scratch`. fileadapter project_context_test.go:38 compares Git.TopLevel to root; hostrunner/workspace compare worktree/write-scope paths. The Branch=master field in failure output was incidental, not a main-branch assertion. Initial comment hypothesizing default-branch mismatch was corrected on the card.
2. Scoped first probe canonicalized scratch and temporarily set Git init.defaultBranch=main; affected3 packages pass. This was NOT accepted as isolating the branch effect.
3. Second probe removed all GIT_CONFIG_COUNT/KEY_0/VALUE_0 overrides and changed ONLY TMPDIR to canonical path of the SAME perf scratch; `go test -race -count=1 ./internal/fileadapter ./internal/hostrunner ./internal/workspace` exit0. Thus path aliases, not branch settings, explain the failure.
4. Entire `bash scripts/check.sh` rerun with canonical same scratch exit0 in50.154s; `go test -race -count=1 ./...` exit0 in70.082s. No assertion/policy/source changes, no blind /tmp relocation. Full canonical argv/env/exit receipts retain the prior exit1 evidence.
5. Tagged new fixture-integrity/HTTPmatrix `-race` test,1k synthetic audit20/40clients2polls: exit0 in6.674s. Expected performance RED not selected by correctness-race command.

Go commands useGOMAXPROCS=2,GOMEMLIMIT=512MiB,GOFLAGS=-p=1,GOPROXY=off,GOSUMDB=off,GOCACHE withinperf scratch. Runtime limit is soft; no increased machine/cgroup memory allowance was introduced. Repository check includes format/unit/vet/coverage/public-surface/skill/UXsmoke gates as defined by actual scripts/check.sh. Not a substitute for independent final-candidate QA or Windows live E2E.

Canonicalization is the explicit reproducible environment convention for downstream workers. A source-level policy-path alias resilience change is outside this baseline scope; do not broaden a secure write scope to silence this fixture issue.
