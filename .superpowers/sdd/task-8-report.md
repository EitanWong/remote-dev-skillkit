## Task 8 review fix

- Status: completed
- Files:
  - `docs/development/GIT_WORKFLOW.md`
  - `CONTRIBUTING.md`
  - `docs/README.md`
- Commit: `docs: tighten git workflow cleanup guidance`
- Tests / checks:
  - `rtk proxy rg -n "feat/[0-9]+-|rdev git|worktree|Squash|--execute|codex/" CONTRIBUTING.md docs/README.md docs/development/GIT_WORKFLOW.md`
  - `rtk git diff --check`
- Concerns:
  - Cleanup and lifecycle guidance must continue to distinguish the stable/main checkout manager boundary from the external worktree.
  - Draft PRs remain a GitHub UI/manual choice only; the CLI still creates a normal PR.
