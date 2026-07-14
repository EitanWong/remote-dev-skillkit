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
