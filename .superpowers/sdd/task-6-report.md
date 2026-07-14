# Task 6 Report: GitHub governance configuration and scripts

## Completed
- Added `.github/governance/branch-ruleset.json` to enforce:
  - PR-only `main`
  - 1 approval
  - code owner review
  - conversation resolution
  - strict/up-to-date status checks for `git-policy` and `go-checks`
  - no force-push / no deletion
  - linear history plus squash-only merge posture
- Added `.github/governance/commit-policy.json` to enforce conventional commit message policy on `main`.
- Added `.github/CODEOWNERS`.
- Added `scripts/github/plan-git-governance.sh` as a read-only JSON planner.
- Added `scripts/github/apply-git-governance.sh` with:
  - `set -euo pipefail`
  - `--execute` gate before any `gh` invocation
  - `gh api` usage with safe argv
  - output redaction for fake credentials
- Added `scripts/github/git-governance_test.sh` using a fake `gh` executable.

## Verification
- `bash -n scripts/github/plan-git-governance.sh scripts/github/apply-git-governance.sh scripts/github/git-governance_test.sh`
- `scripts/github/git-governance_test.sh`
- `git diff --check`

## Notes
- Tests do not contact GitHub or mutate real settings.
- The apply script only proceeds with `--execute`.

## Fix report

### Status
- Implemented the requested review fixes for Task 6.
- Verified the governance scripts remain read-only in plan mode and fake-gh only in tests.

### Files changed
- `.github/governance/branch-ruleset.json`
- `scripts/github/apply-git-governance.sh`
- `scripts/github/git-governance_test.sh`
- `.superpowers/sdd/task-6-report.md`

### Commit
- `715bfef` — `fix: tighten github governance scripts`

### Tests
- `bash -n scripts/github/plan-git-governance.sh scripts/github/apply-git-governance.sh scripts/github/git-governance_test.sh`
- `scripts/github/git-governance_test.sh`
- JSON validation for `.github/governance/branch-ruleset.json` and `.github/governance/commit-policy.json`
- `git diff --check`

### Concerns
- None.

## Hygiene fix report

### Status
- Reworked `scripts/github/git-governance_test.sh` to run against an isolated temporary repository copy.
- Removed the unused `plan_out` variable.
- Preserved the original repository tree by avoiding direct writes to the tracked `.git-governance.plan.json` path.

### Files changed
- `scripts/github/git-governance_test.sh`
- `.superpowers/sdd/task-6-report.md`

### Commit
- Pending at time of report append.

### Tests
- `bash -n scripts/github/plan-git-governance.sh scripts/github/apply-git-governance.sh scripts/github/git-governance_test.sh`
- `scripts/github/git-governance_test.sh`
- JSON validation for `.github/governance/branch-ruleset.json` and `.github/governance/commit-policy.json`
- `shellcheck` on the three governance scripts, when available
- `git diff --check`

### Concerns
- None.
