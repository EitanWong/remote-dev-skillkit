## Task 8 review fix

- Status: completed
- Files:
  - `docs/development/GIT_WORKFLOW.md`
  - `CONTRIBUTING.md`
  - `docs/README.md`
- Commit: `docs: refine git workflow recovery guidance`
- Tests / checks:
  - `rtk proxy rg -n "feat/[0-9]+-|rdev git|worktree|Squash|--execute|codex/" CONTRIBUTING.md docs/README.md docs/development/GIT_WORKFLOW.md`
  - `rtk git diff --check`
- Concerns:
  - The docs should stay aligned with the implemented CLI and only describe Draft PRs as a manual GitHub UI choice, not a CLI feature.
  - Cleanup/migration examples must keep the main checkout as the manager boundary and avoid recreating already-bound branches.
