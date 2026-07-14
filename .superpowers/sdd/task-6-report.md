# Task 6 Report: GitHub governance configuration and scripts

## Status
Done. The governance files and scripts are in place and verified.

## Files
- `.github/governance/branch-ruleset.json`
- `.github/governance/commit-policy.json`
- `.github/CODEOWNERS`
- `scripts/github/plan-git-governance.sh`
- `scripts/github/apply-git-governance.sh`
- `scripts/github/git-governance_test.sh`

## Verification
- `bash -n scripts/github/plan-git-governance.sh`
- `bash -n scripts/github/apply-git-governance.sh`
- `bash -n scripts/github/git-governance_test.sh`
- `bash scripts/github/git-governance_test.sh`

## Results
- Plan script emits read-only JSON and does not invoke `gh`.
- Apply script rejects missing `--execute` without calling `gh`.
- Apply with `--execute` uses only the fake `gh` in tests and redacts fake credentials.
- Ruleset targets `main`, requires PRs, one approval, `git-policy` and `go-checks`, conversation resolution, up-to-date branches, no force-push/deletion, squash-only merging, and automatic head-branch deletion.

## Commit
- `chore: declare github git governance`

## Concerns
- None.

## Fix report

### Status
- Applied the requested safety fixes for malformed GitHub API responses, token redaction, repo validation, and CODEOWNERS ownership.

### Files
- `.github/CODEOWNERS`
- `scripts/github/apply-git-governance.sh`
- `scripts/github/git-governance_test.sh`

### Commit
- Pending after this report update.

### Tests
- `bash -n scripts/github/plan-git-governance.sh`
- `bash -n scripts/github/apply-git-governance.sh`
- `bash -n scripts/github/git-governance_test.sh`
- `bash scripts/github/git-governance_test.sh`
- `git diff --check`

### Concerns
- None.
