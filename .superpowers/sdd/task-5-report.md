# Task 5 Report: Expose the rdev git CLI

## Status

Implemented and committed successfully in the isolated worktree.

## Changed files

- `internal/cli/gitworkflow.go`
  - Added the `rdev git` command group.
  - Implemented branch creation, worktree lifecycle commands, policy checks,
    sync/fetch, PR planning, and guarded PR creation.
  - Uses `flag.NewFlagSet`, repository discovery, existing
    `internal/gitworkflow` APIs, structured JSON output, and actionable errors.
  - `git pr create` rejects missing `--execute` before invoking GitHub tooling.
- `internal/cli/gitworkflow_test.go`
  - Added the required focused tests:
    - `TestRunGitPolicyCheckEmitsJSON`
    - `TestRunGitBranchCreateRequiresIssue`
    - `TestRunGitWorktreeCreateRejectsMain`
    - `TestRunGitPRPlanDoesNotExecuteGH`
- `internal/cli/cli.go`
  - Added `git` dispatch and top-level/group usage entries.
- `internal/cli/cli_test.go`
  - Added `TestGitCommandGroupUsage`.

Existing `internal/workspace` runtime worktree behavior was not changed.

## Commit

- Commit: `HEAD (fix: harden git workflow CLI)`
- Message: `feat: expose git workflow commands`

## Tests and verification

- `go test ./internal/cli -run 'TestRunGit' -count=1` — PASS
- `go test ./internal/cli -run 'TestRunGit|TestGitCommandGroupUsage' -count=1` — PASS
- `go test ./internal/cli -count=1` — PASS
- `go test ./internal/gitworkflow -count=1` — PASS
- `gofmt -w ...` — PASS
- `git diff --check` — PASS
- `go run ./cmd/rdev git --help` — PASS
- Worktree status after commit — clean

## Concerns

- Branch creation uses `git switch -c` and therefore requires the selected
  base reference to exist locally.
- PR title and body remain required by the existing `PlanPR` validation even
  though the CLI flags are optional in the command synopsis.

## Task 5 Fix Follow-up

- Status: completed
- Files: `internal/cli/cli.go`, `internal/cli/gitworkflow.go`, `internal/cli/gitworkflow_test.go`, `internal/gitworkflow/policy.go`
- Commit: `HEAD (fix: harden git workflow CLI)`
- Tests: `go test ./internal/cli -run 'TestRunGit|TestGitCommandGroupUsage' -count=1`, `go test ./...`, `go vet ./...`, `git diff --check`
- Concerns: exported `internal/gitworkflow.ValidateBaseRef` to reuse the existing validation logic across all CLI `--base` paths while preserving the `GitHubExecutor`/runtime-workspace boundaries.

## Task 5 Fix Pass 2

- Status: completed
- Commit: `feat: expose git workflow commands`
- Files:
  - `internal/cli/cli.go`
  - `internal/cli/gitworkflow.go`
  - `internal/cli/gitworkflow_test.go`
- Tests:
  - `go test ./internal/cli -run 'TestRunGit|TestGitCommandGroupUsage' -count=1`
  - `go test ./internal/cli -count=1`
  - `go test ./internal/gitworkflow -count=1`
  - `go vet ./internal/cli`
  - `go vet ./internal/gitworkflow`
  - `go run ./cmd/rdev git --help`
  - `git diff --check`
- Concerns:
  - `git pr plan --execute` is rejected by flag parsing, so the exact error text is Go-version dependent, but execution is still blocked before GitHub invocation.
