# Git Branch and Worktree Governance Design

**Date:** 2026-07-14
**Status:** Approved for specification review
**Scope:** Local Git workflow, worktree lifecycle, GitHub collaboration, CI enforcement, and migration of existing development branches.

## 1. Goal

Make branch and worktree management professional, repeatable, cross-platform,
and friendly to both humans and coding agents. GitHub remains the authority for
remote collaboration and merge governance. Local tooling makes the safe path
fast without introducing hidden state or implicit external mutations.

Success means:

- every non-`main` development branch has a strict, issue-linked name;
- every development worktree has a predictable location and safe lifecycle;
- local policy checks and GitHub Actions use the same rules;
- `main` can only receive reviewed, passing, squash-merged PRs;
- external operations such as push, PR creation, merge, and GitHub rule changes
  require explicit execution flags;
- the existing `codex/*` branch is migrated and no longer accepted.

## 2. Principles and non-goals

### Principles

1. Prefer GitHub-native Issues, Draft PRs, Actions, CODEOWNERS, Rulesets, and
   automatic head-branch deletion over custom coordination systems.
2. Keep policy validation deterministic and reusable locally and in CI.
3. Use Git's native worktree metadata as the source of truth; do not add hidden
   manifests or persistence files.
4. Treat Windows as a first-class platform.
5. Make destructive or networked operations explicit and auditable.
6. Keep the existing runtime task worktree implementation separate from the
   developer worktree workflow.

### Non-goals

- replacing GitHub Issues, PRs, or Actions;
- silently pushing branches, opening PRs, merging, or changing repository
  settings;
- rewriting commit history during branch migration;
- changing runtime `.rdev/worktrees` behavior;
- requiring signed commits before the project has a signing migration plan.

## 3. Governance model

### Protected branch

`main` is the only long-lived integration branch. Direct pushes, force-pushes,
and deletion are prohibited. Changes enter through PRs, require at least one
approval and passing required checks, and are merged using squash merge only.
Conversations must be resolved before merge. Maintainer bypass is reserved for
documented emergencies and must be explained in the PR.

### Branch names

Every non-`main` branch must match:

```text
^(feat|fix|refactor|docs|test|chore|perf|ci|hotfix|release)/[0-9]+-[a-z0-9]+(-[a-z0-9]+)*$
```

Examples:

```text
feat/123-worktree-governance
fix/456-stale-worktree-cleanup
docs/789-git-contributing-guide
hotfix/901-release-blocker
```

The issue number is mandatory for every type, including `docs`, `release`, and
`hotfix`. The slug is lowercase, ASCII, and hyphen-separated. Identity
prefixes such as `codex/`, `agent/`, or personal names are not valid.

### Commit and PR titles

Commits and PR titles follow:

```text
<type>(<optional-scope>): <imperative summary>
```

Allowed types are the branch types above. The policy check validates commit
messages in the PR range and the PR title independently; squash merge uses the
validated PR title as the final main-branch commit subject.

### Standard lifecycle

```text
Issue -> branch from origin/main -> worktree -> Draft PR -> checks/review
      -> Ready PR -> squash merge -> automatic remote deletion -> local cleanup
```

## 4. Worktree model

Developer worktrees live outside the repository:

```text
../.worktrees/<repository>/<branch-slug>/
```

For example:

```text
../.worktrees/remote-dev-skillkit/feat-123-worktree-governance/
```

The branch slash is converted to a hyphen only for the filesystem directory.
The Git branch name remains unchanged. A branch may be attached to only one
worktree. The main checkout is reserved for `main`; feature work happens in a
dedicated worktree.

The existing runtime path `.rdev/worktrees` is not changed. Runtime task
worktrees and developer worktrees have different ownership, locking, and
lifecycle rules.

Safe removal requires the worktree to be clean and the branch to be merged or
explicitly selected with `--force`. Removal must not delete a path outside the
calculated worktree root.

## 5. Local command surface

The workflow is exposed through the existing `rdev` CLI using a dedicated
`git` namespace:

```text
rdev git branch create --type feat --issue 123 --slug worktree-governance --base origin/main
rdev git worktree create --branch feat/123-worktree-governance
rdev git worktree list
rdev git worktree doctor
rdev git worktree clean
rdev git worktree remove --branch feat/123-worktree-governance
rdev git policy check
rdev git sync
rdev git pr plan
rdev git pr create --execute
rdev git pr checks
rdev git pr merge --squash --execute
```

Command rules:

- branch creation requires `--type`, numeric `--issue`, and lowercase `--slug`;
- the default base is `origin/main`, and the command must report the selected
  base explicitly;
- worktree creation rejects duplicate paths, duplicate branch bindings,
  detached HEADs, and attempts to use `main` for development;
- `doctor` reports cleanliness, upstream, ahead/behind, stale metadata,
  detached states, path safety, and branch-policy failures;
- `clean` removes only merged and clean worktrees by default;
- dirty or unmerged removal requires `--force`;
- `sync` performs the safe fetch/prune path by default; rebasing is an explicit
  option and never implicit;
- `pr plan` has no external side effect;
- `pr create` and `pr merge` require `--execute` and use the official `gh`
  command when available;
- all commands emit structured JSON evidence, with concise human-readable
  errors on stderr.

The implementation belongs in a focused `internal/gitworkflow` package. It
should separate branch validation, worktree operations, policy checks, GitHub
CLI integration, and evidence formatting. Existing `internal/workspace` code
continues to own runtime task worktrees and locks.

## 6. Shared policy and evidence

Local and CI checks share one policy implementation. The JSON envelope is:

```json
{
  "schema": "rdev.git-workflow.v1",
  "ok": true,
  "repo_root": "...",
  "branch": "feat/123-worktree-governance",
  "issue": 123,
  "base": "origin/main",
  "worktree": "...",
  "checks": [],
  "commands": []
}
```

Command evidence records argv, working directory, redacted stdout/stderr,
exit code, and timestamps where the existing project evidence model supports
them. Tokens, credentials, full environments, and unredacted sensitive output
must never be emitted.

## 7. GitHub integration

### GitHub-native controls

Use a Ruleset or equivalent branch protection for `main` with:

- pull request required;
- required status checks;
- human review optional for the single-maintainer project;
- branch must be up to date before merge;
- force-push and deletion disabled;
- squash merge only;
- automatic head-branch deletion enabled.

The repository should add or update:

- `.github/CODEOWNERS` for core Go, workflow/governance, security/policy, and
  release surfaces;
- `.github/PULL_REQUEST_TEMPLATE.md` with issue, branch, worktree, test,
  security, and release evidence sections;
- a GitHub Actions `git-policy` job;
- stable required jobs named `git-policy` and `go-checks`;
- the existing release smoke job, keeping its check name stable.

### GitHub Actions

CI runs on every branch push, every PR, `main` pushes, and manual dispatch.
`git-policy` checks:

- strict branch naming;
- PR base is `main`;
- PR references the issue encoded in the branch;
- Conventional PR title and commit messages;
- forbidden local state, secrets, and generated artifacts;
- no old `codex/*` branch is used by a PR.

The existing Go test, vet, and repository check commands remain the source for
the Go validation job. Job names are treated as API because GitHub required
checks depend on exact names.

### Declarative governance plan

Add reviewed target configuration under:

```text
.github/governance/branch-ruleset.json
.github/governance/commit-policy.json
```

Provide dry-run-first scripts:

```text
scripts/github/plan-git-governance.sh
scripts/github/apply-git-governance.sh --execute
```

The plan command compares desired and current GitHub configuration. The apply
command requires explicit execution, uses `gh` or the GitHub API, and prints a
redacted evidence summary. No workflow may silently mutate repository settings.

## 8. Migration plan

The existing branch:

```text
codex/phase1-regional-tunnel-availability
```

is migrated after creating or associating a GitHub Issue:

1. choose the issue and branch type;
2. rename the local branch to `feat/<issue>-regional-tunnel-availability`;
3. move the attached worktree to the standard external path;
4. push the new branch and set its upstream;
5. update or recreate the Draft PR association;
6. verify policy, worktree, and CI status;
7. delete the remote `codex/*` branch;
8. run `git worktree prune` through the workflow tool.

History is not rewritten. The old branch name is not retained as a supported
compatibility exception. Future policy checks fail closed on `codex/*` and any
other nonconforming name.

## 9. Testing and verification

Tests must cover:

- valid and invalid branch names;
- issue and slug parsing;
- Conventional Commit validation;
- worktree path normalization and repository-boundary protection;
- duplicate branch/path detection;
- clean, dirty, merged, unmerged, detached, and stale worktree states;
- safe removal and explicit force removal;
- JSON evidence stability and redaction;
- missing `gh`, unauthenticated `gh`, and failed GitHub commands;
- Linux, macOS, and Windows path behavior;
- regression coverage for existing runtime workspace worktrees.

Verification includes focused package tests, a temporary Git repository
integration suite, `go test ./...`, `go vet ./...`, `./scripts/check.sh`,
`git diff --check`, and the existing release smoke when the affected surfaces
require it.

## 10. Rollout order

1. Add the policy package, commands, tests, and documentation.
2. Run the local tool against the current repository and migrate the existing
   `codex/*` worktree.
3. Add CI policy checks and update the PR template/CODEOWNERS.
4. Run the GitHub governance plan and review the diff.
5. Explicitly apply the `main` Ruleset.
6. Mark stable CI jobs as required checks.
7. Enable automatic branch deletion and communicate the workflow.

The implementation must not apply GitHub settings, push branches, create PRs,
or delete remote branches as part of ordinary tests or local setup.

## 11. Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Existing agents use free-form branch names | Require issue/type/slug in the CLI and fail CI early with a repair command. |
| Required check names drift | Treat job names as a documented compatibility surface and test workflow policy. |
| Worktree removal loses changes | Refuse dirty removal by default and show recovery commands. |
| GitHub CLI is unavailable | Keep planning local and provide the exact manual/`gh` command. |
| Branch migration breaks a Draft PR | Migrate only after Issue association and verify head/base state. |
| Local and runtime worktrees collide | Keep developer worktrees outside the repo and leave `.rdev/worktrees` unchanged. |
