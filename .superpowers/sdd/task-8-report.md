# Task 8 Report

## Files updated
- `docs/development/GIT_WORKFLOW.md`
- `CONTRIBUTING.md`
- `docs/README.md`

## What changed
- Documented the issue-first Git branch and external worktree lifecycle.
- Added strict `<type>/<issue>-<slug>` naming guidance and valid examples.
- Documented the approved runnable path, including `gh issue create`, `rdev git branch create`, `rdev git worktree create`, `rdev git worktree doctor`, `rdev git pr plan`, and `rdev git pr create --execute`.
- Clarified multi-worktree use, Draft PR timing, review/merge rules, cleanup/recovery, hotfix/release handling, agent use, GitHub Ruleset boundaries, and legacy `codex/*` migration.
- Updated cross-links in `CONTRIBUTING.md` and `docs/README.md`.

## Verification
- `rtk rg -n "feat/[0-9]+-|rdev git|worktree|Squash|--execute|codex/" CONTRIBUTING.md docs/README.md docs/development/GIT_WORKFLOW.md`
- `rtk git diff --check`

## Review follow-up

- **Status:** Fixed the documented command inconsistencies called out in review.
- **Files:** `docs/development/GIT_WORKFLOW.md`, `CONTRIBUTING.md`
- **Commit:** `docs: fix git workflow command consistency`
- **Tests / checks:** `rtk rg -n "worktree list --root|branch create.*worktree create|worktree create.*branch create|gh issue create|git push --delete|git push -u origin|--execute|main only|maintainer-managed|absolute/path/to/.worktrees/remote-dev-skillkit" CONTRIBUTING.md docs/README.md docs/development/GIT_WORKFLOW.md`; `rtk git diff --check`
- **Concerns:** No remaining documented `git worktree list --root` calls; release/hotfix cleanup is now described as maintainer-managed, and raw `gh issue create` / `git push` / `git push --delete` actions are explicitly manual external mutations.
