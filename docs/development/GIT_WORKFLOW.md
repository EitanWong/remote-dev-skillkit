# Git Workflow

This guide documents the implemented `rdev git` workflow for issue-first
branch creation, external worktrees, ready PRs, and recovery.

## Canonical branch format

Use `<type>/<issue>-<slug>`.

Allowed types are `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`,
`ci`, `hotfix`, and `release`.

The parser requires this exact shape:

```text
^(feat|fix|refactor|docs|test|chore|perf|ci|hotfix|release)/([0-9]+)-([a-z0-9]+(?:-[a-z0-9]+)*)$
```

- The issue segment must be a positive integer.
- The slug must be lowercase ASCII.
- Hyphens must separate slug words.

Examples:

- `feat/123-worktree-governance`
- `fix/456-main-pr-base`
- `hotfix/789-roll-back-bad-release`
- `release/1012-2026-07-15`

Do not create new `codex/*` branches for normal work. See the legacy migration
section for the only acceptable `codex/*` references.

## Canonical runnable path

For the external-worktree flow, use `worktree create` directly. Do not create a
branch in the current checkout and then create the same branch again in a new
worktree.

```bash
gh issue create --title "Track worktree governance" --body "..."
go run ./cmd/rdev git worktree create --repo . --branch feat/123-worktree-governance --base main --root ../.worktrees/remote-dev-skillkit
go run ./cmd/rdev git worktree doctor --repo . --root ../.worktrees/remote-dev-skillkit
cd ../.worktrees/remote-dev-skillkit/feat-123-worktree-governance
go run ./cmd/rdev git policy check
go run ./cmd/rdev git pr plan
go run ./cmd/rdev git pr create --execute
```

`go run ./cmd/rdev git pr create --execute` is the external-mutation boundary.
`go run ./cmd/rdev git pr plan` only plans the ready PR payload; it does not
create a PR.

## Issue-first lifecycle

1. Create or confirm the issue first.
2. Choose one supported path:
   - `go run ./cmd/rdev git worktree create --repo . --branch feat/123-worktree-governance --base main --root ../.worktrees/remote-dev-skillkit`
     for the external-worktree flow; or
   - `go run ./cmd/rdev git branch create --type feat --issue 123 --slug worktree-governance --base origin/main`
     for a local-checkout-only branch.
3. For the worktree flow, `cd` into the created external worktree before policy
   or PR commands.
4. Make changes in the worktree, not in the main checkout.
5. Run `go run ./cmd/rdev git worktree doctor --repo . --root
   ../.worktrees/remote-dev-skillkit` from the stable/main checkout before
   planning the PR.
6. Run `go run ./cmd/rdev git policy check` from inside the external worktree
   or with `--repo` pointing at that worktree before planning the PR.
7. Run `go run ./cmd/rdev git pr plan` to inspect the generated ready PR
   title, body, head, and base.
8. Run `go run ./cmd/rdev git pr create --execute` only when the branch is
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

`--root` applies only to worktree lifecycle commands. Valid flag shapes:

- `go run ./cmd/rdev git branch create --type <type> --issue <n> --slug <slug> [--base <ref>] [--repo <checkout>]`
- `go run ./cmd/rdev git worktree create --repo <main-checkout> --branch <branch> [--base <ref>] [--root <developer-root>]`
- `go run ./cmd/rdev git worktree list --repo <main-checkout> [--root <developer-root>]`
- `go run ./cmd/rdev git worktree doctor --repo <main-checkout> [--root <developer-root>]`
- `go run ./cmd/rdev git worktree clean --repo <main-checkout> [--root <developer-root>]`
- `go run ./cmd/rdev git worktree remove --repo <main-checkout> --branch <branch> [--root <developer-root>] [--force]`
- `go run ./cmd/rdev git sync --repo <main-checkout> [--prune]`

If you are already inside the external worktree, omit `--repo` only for policy
and PR commands. The stable/main checkout is still the manager boundary for
lifecycle commands because the current manager checkout is excluded and
refused.

## PR timing

- Open the ready PR only after the branch name, worktree state, and policy
  checks are clean.
- Use `go run ./cmd/rdev git pr plan` as the review point for title, body,
  base, and head before any external mutation.
- `go run ./cmd/rdev git pr create --execute` calls `gh pr create` with the exact argv shape
  `gh pr create --base <base> --head <head> --title <title> --body <body>`.
- The current implementation creates a normal PR with that exact argv.
- The CLI has no `--draft` flag. If a Draft PR is desired, mark it manually in
  the GitHub UI after creation.
- Squash merge after review. Do not use merge commits or rebase merges for
  this workflow.

## Cleanup and recovery

Run cleanup from the stable/main checkout that owns the manager repository, not
from the target external worktree.

- `go run ./cmd/rdev git worktree clean --repo <main-checkout> --root <root>`
  removes eligible merged-clean worktrees and their branches.
- `go run ./cmd/rdev git worktree remove --repo <main-checkout> --root <root> --branch <branch>`
  removes a specific eligible target that was not already cleaned.
- `go run ./cmd/rdev git worktree remove --repo <main-checkout> --root <root> --branch <branch> --force`
  is the dirty merged-worktree example. The current implementation still
  rejects unmerged branches even with `--force`.

Recovery rules:

- If the checkout is dirty, finish, stash, or discard local changes before
  `go run ./cmd/rdev git pr create --execute`.
- `go run ./cmd/rdev git worktree clean` removes eligible merged-clean
  worktrees and their branches from the stable checkout. Use
  `go run ./cmd/rdev git worktree remove --repo <main-checkout> --root <root> --branch <branch>`
  only for a specific eligible target that was not already cleaned.
- For dirty merged worktrees, use the `--force` form shown above. The current
  implementation still rejects unmerged branches even with `--force`.
- If metadata looks stale after branch deletion or a move, use Git’s repair
  path from the main checkout or a linked worktree: `git worktree move
  <old-path> <new-path>` for planned moves, `git worktree repair <path...>` for
  moved checkouts, and `git worktree prune` to clean stale administrative
  records after deletions.

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
- Migration steps for an existing `codex/*` branch:
  1. Link or confirm the issue that will own the branch.
  2. Rename the local branch to a conforming name, for example
     `git branch -m codex/123-old-name feat/123-worktree-governance`.
  3. Move the existing linked worktree instead of recreating the branch with
     `worktree create -b`:

     ```bash
     git worktree move <old-path> <new-path>
     git worktree repair <new-path>
     ```

  4. Reassociate the branch with the issue’s external worktree path and verify
     the new location with `git worktree list`.
  5. Push the conforming branch explicitly after human authorization:

     ```bash
     git push -u origin feat/123-worktree-governance
     ```

     Treat raw pushes as external mutations that require explicit human
     approval, consistent with the `--execute` boundary.
  6. Reassociate or recreate the PR from the conforming branch as needed.
  7. Delete the old remote branch only after human authorization:

     ```bash
     git push origin --delete codex/123-old-name
     ```

     Treat remote deletion as an external mutation that requires explicit
     human approval.
  8. Prune stale refs and worktrees with `git fetch --prune` and
     `git worktree prune`.
- Keep any `codex/*` reference clearly labeled as migration-only in docs and
  PR notes.
