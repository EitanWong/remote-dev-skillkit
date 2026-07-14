# Task 7 Report — GitHub Actions enforcement and repository templates

## Outcome

Updated the Task 7 governance surface in owned files only:
- explicit all-branch push/PR workflow triggers
- stable required jobs `git-policy` and `go-checks`
- trusted-base PR validation coverage in shell tests
- PR template / contributing / docs index updates
- preserved existing `release-smoke` gating behavior

## Files changed

- `.github/workflows/git-policy.yml`
- `.github/workflows/ci.yml`
- `scripts/ci/git-policy_test.sh`
- `.github/PULL_REQUEST_TEMPLATE.md`
- `CONTRIBUTING.md`
- `docs/README.md`

## Implementation notes

### Workflow enforcement
- Kept `git-policy` as the stable job name in `.github/workflows/git-policy.yml`.
- Kept `go-checks` as the stable job name in `.github/workflows/ci.yml`.
- Made branch coverage explicit with `push.branches: ['**']` and PR coverage explicit with:
  - `pull_request.branches: ['**']` in CI
  - `pull_request_target.branches: ['**']` in git policy
- Left `release-smoke` behavior unchanged: it still runs only for `workflow_dispatch`, pull requests, and pushes to `main`.
- Continued using trusted repository code for PR validation by checking out the PR base ref with `persist-credentials: false` and passing PR metadata into `scripts/ci/git-policy.sh`.

### Wrapper tests
- Disabled local git hooks inside test repos with `core.hooksPath=/dev/null` to avoid global-hook noise and keep tests local-only.
- Added workflow assertions for:
  - trusted `pull_request_target` usage
  - explicit all-branch trigger declarations
  - stable `git-policy` and `go-checks` job names
  - no untrusted PR-head checkout
- Kept behavioral coverage for the required wrapper cases:
  - `codex/123-old-name` fails
  - `feat/123-valid-name` passes
  - non-`main` PR base fails
  - trusted PR validation does not mutate HEAD

### Contributor-facing updates
- PR template now explicitly requires linked issue confirmation and pasted branch/worktree command output.
- `CONTRIBUTING.md` now calls out the stable required checks and local wrapper verification commands.
- `docs/README.md` now indexes the contributing guide as the source for GitHub workflow enforcement, PR template requirements, and local verification commands.

## Verification

Executed from:
`/Users/eitan/Documents/Projects/Go/.worktrees/remote-dev-skillkit/feat-2-git-branch-worktree-governance`

- `rtk bash -n scripts/ci/git-policy.sh scripts/ci/git-policy_test.sh` — PASS
- `rtk bash scripts/ci/git-policy_test.sh` — PASS
- `rtk git diff --check` — PASS
- `rtk proxy ./scripts/check.sh` — FAIL
  - existing repository coverage gate failure:
    - `coverage_check_failed package=./internal/cli coverage=81.7 threshold=82.0`
  - no Go code was modified for Task 7, so this was recorded and left unchanged.

## Commit

Committed with:
- `ci: enforce git workflow policy on pull requests`

## Final state

Task 7 changes are implemented in the approved ownership surface, with the known unrelated `./scripts/check.sh` coverage failure documented above.
