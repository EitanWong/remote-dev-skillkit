## Task 8 review fix

- Status: completed
- Files:
  - `docs/development/GIT_WORKFLOW.md`
  - `CONTRIBUTING.md`
  - `docs/README.md`
- Commit: `docs: correct git workflow guidance`
- Tests / checks:
  - `rtk proxy rg -n "feat/[0-9]+-|rdev git|worktree|Squash|--execute|codex/" CONTRIBUTING.md docs/README.md docs/development/GIT_WORKFLOW.md`
  - `rtk git diff --check`
- Concerns:
  - The docs now describe the current normal PR flow; there is still no Draft PR support in the implemented CLI.
  - `git worktree remove --force` remains blocked on unmerged branches by the implementation, so the docs call that out explicitly.
