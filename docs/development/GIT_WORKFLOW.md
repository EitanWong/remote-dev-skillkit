# Git Workflow

This guide documents the implemented `rdev git` workflow for issue-first branch
creation, external worktrees, Draft PRs, and recovery.

## Canonical branch format

Use `<type>/<issue>-<slug>`.

Allowed types are `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`,
`ci`, `hotfix`, and `release`.

Examples:

- `feat/123-worktree-governance`
- `fix/456-main-pr-base`
- `hotfix/789-roll-back-bad-release`
- `release/1012-2026-07-15`

Do not create new `codex/*` branches for normal work. See the legacy migration
section for the only acceptable `codex/*` references.

## Canonical runnable path

Run the issue-first flow in this order:

```bash
gh issue create --title "Track worktree governance" --body "..."
go run ./cmd/rdev git branch create --type feat --issue 123 --slug worktree-governance --base origin/main
go run ./cmd/rdev git worktree create --branch feat/123-worktree-governance
go run ./cmd/rdev git worktree doctor
go run ./cmd/rdev git pr plan
go run ./cmd/rdev git pr create --execute
```

`go run ./cmd/rdev git pr create --execute` is the external-mutation boundary.
`git pr plan` only plans the Draft PR payload; it does not create a PR.

## Issue-first lifecycle

1. Create or confirm the issue first.
2. Create the branch with the issue number in the branch name.
3. Create a developer worktree outside the repository tree.
4. Make changes in the worktree, not in the main checkout.
5. Run `go run ./cmd/rdev git worktree doctor` and `go run ./cmd/rdev git
   policy check` before planning the PR.
6. Run `go run ./cmd/rdev git pr plan` to inspect the generated Draft PR
   title, body, head, and base.
7. Run `go run ./cmd/rdev git pr create --execute` only when the branch is
   clean, reviewed, and ready for the external PR mutation.

## Multi-worktree use

- Keep one branch per task and one worktree per active branch.
- Use an external shared root such as `../.worktrees/remote-dev-skillkit`.
- Worktree roots must stay outside the repository tree; the manager rejects
  roots that live inside the repo.
- Use `go run ./cmd/rdev git worktree list --root ../.worktrees/remote-dev-skillkit`
  to confirm which path belongs to which branch.

Recommended commands:

```bash
go run ./cmd/rdev git worktree create --branch feat/123-worktree-governance --root ../.worktrees/remote-dev-skillkit
go run ./cmd/rdev git worktree list --root ../.worktrees/remote-dev-skillkit
go run ./cmd/rdev git worktree doctor --root ../.worktrees/remote-dev-skillkit
```

## Draft PR timing

- Open the Draft PR only after the branch name, worktree state, and policy
  checks are clean.
- Use `go run ./cmd/rdev git pr plan` as the review point for title, body,
  base, and head before any external mutation.
- Keep the PR in Draft until the reviewer has approved the branch and the
  implementation is complete.
- Squash merge after review. Do not use merge commits or rebase merges for
  this workflow.

## Cleanup and recovery

Clean up in this order when work is finished or abandoned:

```bash
go run ./cmd/rdev git worktree clean --root ../.worktrees/remote-dev-skillkit
go run ./cmd/rdev git worktree remove --branch feat/123-worktree-governance --root ../.worktrees/remote-dev-skillkit
```

Recovery rules:

- If the checkout is dirty, finish, stash, or discard local changes before
  `go run ./cmd/rdev git pr create --execute`.
- If local metadata is stale after branch deletion or remote pruning, run
  `go run ./cmd/rdev git sync --prune` and then `go run ./cmd/rdev git worktree
  doctor`.
- Use `go run ./cmd/rdev git worktree clean` to reconcile stale recorded state
  before removing the worktree entry.

## Hotfix and release flow

Use the same branch format for urgent and release work:

- `hotfix/<issue>-<slug>` for production fixes.
- `release/<issue>-<slug>` for release tracking or stabilization.

The lifecycle stays the same: create the issue, create the branch, create the
worktree, verify, plan the PR, and only then execute the PR creation step.

## Agent use

- Use planner-style reasoning before branch or worktree changes that touch
  multiple files or release state.
- Use reviewer-style validation before `go run ./cmd/rdev git pr create
  --execute`, especially for hotfix, release, cleanup, and recovery work.
- For docs-only changes, still run the same verification commands and a
  self-review before committing.
- Never let an agent bypass the `--execute` boundary for external mutations.

## Legacy `codex/*` migration only

- `codex/*` is legacy migration territory only.
- Do not create new `codex/*` branches for normal work.
- If you encounter an old migration branch such as `codex/123-old-name`, treat
  it as a migration-only case and replace it with a conforming branch before
  continuing.
- Keep any `codex/*` reference clearly labeled as migration-only in docs and
  PR notes.
