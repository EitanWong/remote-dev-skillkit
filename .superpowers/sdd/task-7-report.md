# Task 7 Report — GitHub Actions enforcement and repository templates

## Outcome

Implemented GitHub Actions git workflow enforcement, a shell policy wrapper with
TDD coverage, and contributor-facing template/documentation updates for the
Git branch/worktree governance feature.

## Files changed

- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/.github/workflows/git-policy.yml`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/scripts/ci/git-policy.sh`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/scripts/ci/git-policy_test.sh`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/.github/workflows/ci.yml`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/.github/PULL_REQUEST_TEMPLATE.md`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/CONTRIBUTING.md`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/docs/README.md`

## What changed

### 1. GitHub Actions enforcement
- Added a dedicated `Git Policy` workflow with stable job name `git-policy`.
- Configured the workflow to run on all branch pushes, all pull requests, and
  `workflow_dispatch`.
- Checked out the head branch ref so policy checks run against the actual branch
  name instead of a detached PR merge ref.
- Updated `CI` so it also runs on all branch pushes and all PRs.
- Renamed the Go verification job to stable name `go-checks`.
- Kept `release-smoke` behavior aligned with the prior intent by limiting it to
  pull requests and pushes to `main`.

### 2. Policy wrapper
- Added `scripts/ci/git-policy.sh`.
- Uses trusted repository code for validation:
  - `go run ./cmd/rdev git policy check --repo . --base origin/main`
  - `go run ./cmd/rdev git pr plan --repo . --base main --title ... --body ...`
- Rejects `codex/*` branches through the existing repository policy logic.
- Rejects pull requests whose base branch is not `main`.
- Validates PR title/body issue linkage during PR events.
- Skips branch-format enforcement for direct pushes to protected `main` so post-
  merge CI remains green.
- Creates a local branch ref from GitHub Actions metadata when needed so repo
  validation works on CI checkouts.

### 3. Tests
- Added `scripts/ci/git-policy_test.sh`.
- TDD flow performed:
  1. Wrote wrapper tests first.
  2. Ran them before implementation and confirmed RED because the wrapper did
     not exist.
  3. Implemented the wrapper and re-ran tests to GREEN.
- Coverage includes:
  - push on `main` skips safely
  - valid issue-linked branch passes
  - legacy `codex/123-old-name` branch fails
  - PR targeting a non-`main` base fails
  - valid PR metadata with matching issue linkage passes

### 4. Templates and docs
- Expanded the PR template to require:
  - issue linkage
  - branch/worktree evidence
  - tests
  - security review
  - release impact
- Updated `CONTRIBUTING.md` with conforming branch examples, `main`-only PR base
  guidance, issue-linkage expectations, and the local git-policy wrapper check.
- Added the branch/worktree/PR workflow entry to `docs/README.md`.

## Verification log

### RED phase
- `bash scripts/ci/git-policy_test.sh`
  - Result: failed as expected with `missing policy script .../scripts/ci/git-policy.sh`

### GREEN / follow-up verification
- `bash scripts/ci/git-policy_test.sh`
  - Result: PASS
- `bash -n scripts/ci/git-policy.sh`
  - Result: PASS
- `bash -n scripts/ci/git-policy_test.sh`
  - Result: PASS
- `git diff --check`
  - Result: PASS before commit

### Required full repo verification
- `./scripts/check.sh`
  - Result: FAIL
  - Failure observed in existing Go coverage gate:
    - `coverage_check_failed package=./internal/cli coverage=81.6 threshold=82.0`
  - I did not modify Go implementation files outside my ownership scope, so I
    left this pre-existing/full-repo failure unchanged and recorded it here.

## Self-review notes

- Verified stable job names are exactly `git-policy` and `go-checks`.
- Verified release-smoke still does not expand to every branch push.
- Verified docs/template examples use conforming branch names only.
- Verified the wrapper delegates validation to repository code instead of
  re-implementing PR checks in shell.
- Verified `main` push handling is exempted so protected-branch CI is not broken.

## Commit

- Commit: `09f5a88`
- Message: `ci: enforce git workflow policy on pull requests`

## Final state

- Working tree clean after commit.
- Report written to:
  `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/.superpowers/sdd/task-7-report.md`

---

# Task 7 Fix Report — Trusted Git Policy Workflow Hardening

## Status

Fixed the critical CI/security review issues in the same workspace by moving PR
policy enforcement to a base-trusted workflow path, restoring manual
`release-smoke`, and adding behavioral tests for trusted-checkout assumptions.

## Files changed

- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/.github/workflows/git-policy.yml`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/scripts/ci/git-policy.sh`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/scripts/ci/git-policy_test.sh`
- `/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance/.github/workflows/ci.yml`

## Fix summary

- Switched git-policy PR enforcement from `pull_request` to
  `pull_request_target`.
- Changed checkout to trusted base code only, with:
  - base ref on PRs
  - `persist-credentials: false`
  - `fetch-depth: 1`
- Confirmed the workflow never checks out PR head refs or SHAs.
- Treated PR head ref/SHA, base ref, title, and body as untrusted metadata only,
  passed into the trusted wrapper through narrow environment inputs.
- Kept branch-name, base-branch, and issue-linkage checks strict by continuing to
  delegate to trusted repository policy code.
- Added CI execution of:
  - `bash scripts/ci/git-policy_test.sh`
  - `bash -n scripts/ci/git-policy.sh scripts/ci/git-policy_test.sh`
  - `actionlint` when available
- Restored `workflow_dispatch` eligibility for `release-smoke`.
- Added tests asserting the workflow remains base-trusted and does not use
  untrusted head checkout.

## Tests run

- `bash scripts/ci/git-policy_test.sh` — PASS
- `bash -n scripts/ci/git-policy.sh` — PASS
- `bash -n scripts/ci/git-policy_test.sh` — PASS
- `actionlint` — not installed locally, skipped intentionally
- `./scripts/check.sh` — FAIL (existing unrelated coverage gate remains:
  `coverage_check_failed package=./internal/cli coverage=81.6 threshold=82.0`)
- `git diff --check` — PASS

## Concerns / notes

- The full repository verification failure remains the pre-existing internal CLI
  coverage gate and is unrelated to these workflow/script changes.
- The trusted wrapper still creates a local branch alias from untrusted metadata,
  but only after `git check-ref-format --branch` validation and always at the
  already-checked-out trusted base commit. No PR head code is fetched or
  executed.
- Fork PRs remain supported because validation uses `pull_request_target`
  metadata without checking out the fork head.

## Commit

- Fix committed as the latest HEAD in this workspace with message `fix: harden trusted git policy workflow`.
