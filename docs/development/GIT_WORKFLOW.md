# Git Workflow

This guide documents the approved issue-first Git branch and external worktree
workflow for Remote Dev Skillkit.

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

## Approved runnable path

For normal development, keep the main checkout on `main` and let the worktree
command create the branch and worktree together. Do not run `branch create` and
`worktree create` for the same branch.

```bash
gh issue create --title "Track worktree governance" --body "..."
go run ./cmd/rdev git worktree create --repo <main-checkout> --branch feat/123-worktree-governance --base main --root /Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit
go run ./cmd/rdev git worktree doctor --repo <main-checkout> --root /Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit
cd /Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-123-worktree-governance
go run ./cmd/rdev git policy check
go run ./cmd/rdev git pr plan
go run ./cmd/rdev git pr create --execute
```

Use `go run ./cmd/rdev git branch create` only when you intend to stay in the
current checkout and do not need a new external worktree for that branch.

Notes:

- `go run ./cmd/rdev git pr plan` is the review checkpoint. It shows the title,
  body, base, and head without mutating GitHub.
- `go run ./cmd/rdev git pr create --execute` is the external-mutation
  boundary for the approved CLI. Do not skip `--execute`.
- `gh issue create` is a manual external mutation and should only be run when
  the issue creation itself is intended and authorized.
- The example worktree path uses the actual branch name, with `/` normalized to
  `-`, so the worktree directory is `feat-123-worktree-governance`.

## Issue-first lifecycle

1. Create or confirm the tracking issue first.
2. Prefer the external-worktree path for normal development:

   ```bash
   go run ./cmd/rdev git worktree create --repo <main-checkout> --branch feat/123-worktree-governance --base main --root /Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit
   ```

3. Move into the external worktree before making changes:

   ```bash
   cd /Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-123-worktree-governance
   ```

4. Verify the worktree mapping from the stable/main checkout:

   ```bash
   go run ./cmd/rdev git worktree doctor --repo <main-checkout> --root /Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit
   ```

5. Run policy checks from the worktree or with `--repo` pointed at the
   worktree:

   ```bash
   go run ./cmd/rdev git policy check
   ```

6. Plan the PR:

   ```bash
   go run ./cmd/rdev git pr plan
   ```

7. Create the PR only when the branch is ready and the worktree is clean:

   ```bash
   go run ./cmd/rdev git pr create --execute
   ```

8. If you are intentionally staying in the current checkout, use the separate
   `branch create` path instead of creating an external worktree.

## Multi-worktree use

- Keep one issue-linked branch per task and one worktree per active branch.
- Use an external shared root such as `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit`.
- Keep worktree roots outside the repository tree; manager commands reject
  roots that live inside the repo.
- Run `go run ./cmd/rdev git worktree list --repo <main-checkout>` to see which
  worktree path belongs to which branch.
- Use the stable/main checkout as the manager boundary for branch and worktree
  lifecycle commands.
- If you are already inside the external worktree, omit `--repo` only for policy
  and PR commands.

Valid flag shapes:

- `go run ./cmd/rdev git branch create --type <type> --issue <n> --slug <slug> [--base <ref>] [--repo <checkout>]`
- `go run ./cmd/rdev git worktree create --repo <main-checkout> --branch <branch> [--base <ref>] [--root <developer-root>]`
- `go run ./cmd/rdev git worktree list --repo <main-checkout>`
- `go run ./cmd/rdev git worktree doctor --repo <main-checkout> [--root <developer-root>]`
- `go run ./cmd/rdev git worktree clean --repo <main-checkout> [--root <developer-root>]`
- `go run ./cmd/rdev git worktree remove --repo <main-checkout> --branch <branch> [--root <developer-root>] [--force]`
- `go run ./cmd/rdev git sync --repo <main-checkout> [--prune]`

Recommended evidence commands for PR notes:

```bash
git branch --show-current
git worktree list --porcelain | sed -n '1,8p'
```

## PR timing, review, and merge rules

- Treat the branch as draft-ready until `go run ./cmd/rdev git pr plan` has been
  reviewed and the worktree is clean.
- Open the PR only after the branch name, worktree state, issue linkage, and
  policy checks are clean.
- Include matching issue text in the PR body, for example `Closes #123`.
- GitHub required checks must remain stable as `git-policy` and `go-checks`.
- Open pull requests against `main` only for ordinary feature, fix, docs,
  chore, test, perf, and ci work.
- Squash merge after review. Do not use merge commits or rebase merges for this
  workflow.
- The approved CLI does not create a Draft PR state directly. If a GitHub Draft
  PR is required by process, create the PR only after review and then convert it
  in GitHub before handoff.

## Cleanup and recovery

Run cleanup from the stable/main checkout that owns the manager repository, not
from the target external worktree.

- `go run ./cmd/rdev git worktree clean --repo <main-checkout> --root <root>`
  removes worktrees merged to `main` and their branches.
- `go run ./cmd/rdev git worktree remove --repo <main-checkout> --root <root>
  --branch <branch>`
  removes a specific eligible target that was not already cleaned.
- `go run ./cmd/rdev git worktree remove --repo <main-checkout> --root <root>
  --branch <branch> --force`
  is the dirty merged-worktree example. The current implementation still
  rejects unmerged branches even with `--force`.
- Release and hotfix branches are maintainer-managed exceptions. Do not treat
  them as ordinary cleanup targets unless a maintainer explicitly directs a
  release or maintenance reconciliation.

Recovery rules:

- If the checkout is dirty, finish, stash, or discard local changes before
  `go run ./cmd/rdev git pr create --execute`.
- Do not let stale local state trigger a remote mutation. Re-run
  `go run ./cmd/rdev git worktree doctor --repo <main-checkout> --root /Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit`
  before planning the PR if the branch or path moved.
- If metadata looks stale after branch deletion or a move, use Git’s repair
  path from the main checkout or a linked worktree: `git worktree move
  <old-path> <new-path>` for planned moves, `git worktree repair <path...>` for
  moved checkouts, and `git worktree prune` to clean stale administrative
  records after deletions.

## Hotfix and release flow

Use the same branch format for urgent and release work:

- `hotfix/<issue>-<slug>` for production fixes.
- `release/<issue>-<slug>` for release tracking or stabilization.

Example issue-linked branches:

- `hotfix/456-rollback-bad-release`
- `release/789-2026-07-15-cut`

The lifecycle stays the same: create the issue, create the branch, create the
worktree, verify, plan the PR, and only then execute the PR creation step.
Normal PRs target `main`. Release and maintenance bases are maintainer-managed
exceptions for release/hotfix work only, and they still must use the same strict
`<type>/<issue>-<slug>` pattern.

## Agent use

- Use planner-style reasoning before branch or worktree changes that touch
  multiple files, multiple worktrees, or release state.
- Use reviewer-style validation before `go run ./cmd/rdev git pr create
  --execute`, especially for hotfix, release, cleanup, and recovery work.
- For docs-only changes, still run the same verification commands and a
  self-review before committing.
- Never let an agent bypass the `--execute` boundary for external mutations.

## GitHub Ruleset and external mutation boundaries

- GitHub branch protection or rulesets should continue to gate `main`; this
  workflow expects the required checks to block unsafe merges.
- `gh issue create` and raw `git push` / `git push --delete` migration commands
  are manual external mutations and require explicit human authorization.
- Any command that writes to GitHub, deletes remote refs, or publishes a PR is
  an external mutation and must be deliberate.
- Within the approved CLI flow, only `go run ./cmd/rdev git pr create
  --execute` crosses the external mutation boundary on your behalf for rdev PR
  and GitHub operations.
- Do not treat planning commands, local checks, or branch creation in the
  current checkout as a substitute for the external mutation step.

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

     Treat raw pushes as manual external mutations that require explicit human
     approval. They are outside the `rdev --execute` boundary.
  6. Reassociate or recreate the PR from the conforming branch as needed.
  7. Delete the old remote branch only after human authorization:

     ```bash
     git push origin --delete codex/123-old-name
     ```

     Treat remote deletion as a manual external mutation that requires explicit
     human approval. It is outside the `rdev --execute` boundary.
  8. Prune stale refs and worktrees with `git fetch --prune` and
     `git worktree prune`.
- Keep any `codex/*` reference clearly labeled as migration-only in docs and
  PR notes.
