# Git Branch and Worktree Governance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Implement strict issue-linked Git branch naming, safe developer worktree lifecycle commands, GitHub-native PR governance, CI enforcement, documentation, and migration support.

**Architecture:** Add a focused internal/gitworkflow package with deterministic policy validation, Git command execution, worktree inspection, and GitHub CLI planning. Expose it from the existing rdev git command group. Reuse the same policy in local CLI and CI, keep GitHub mutations explicit, and leave runtime .rdev/worktrees behavior in internal/workspace unchanged.

**Tech Stack:** Go standard library, existing internal/cli command dispatch, Git CLI, GitHub CLI (gh) when explicitly requested, GitHub Actions, and Bash scripts already used by the repository.

## Global Constraints

- Every non-main branch must match ^(feat|fix|refactor|docs|test|chore|perf|ci|hotfix|release)/[0-9]+-[a-z0-9]+(-[a-z0-9]+)*$.
- The issue number is mandatory for all branch types, including docs, release, and hotfix.
- New developer worktrees live under ../.worktrees/<repository>/<branch-slug>/.
- Do not alter internal/workspace runtime task worktree or lock behavior.
- Do not write hidden workflow state files; use Git native worktree metadata and remote tracking state.
- Push, PR creation, merge, remote branch deletion, and GitHub settings changes require an explicit execution flag.
- Never log credentials, tokens, complete environments, or unredacted sensitive command output.
- Use Go standard library only unless an existing dependency is already required by the repository.
- Preserve Windows, macOS, and Linux path behavior.
- Test-first: each behavior gets a failing focused test before implementation.

---

## File Map

Create:

- internal/gitworkflow/policy.go and policy_test.go: strict branch, issue, slug, commit, and PR policy.
- internal/gitworkflow/git.go and git_test.go: Git runner, repository discovery, evidence, and temporary-repository tests.
- internal/gitworkflow/worktree.go and worktree_test.go: developer worktree path planning and lifecycle.
- internal/gitworkflow/github.go and github_test.go: PR plan model and explicit gh boundary.
- internal/cli/gitworkflow.go and gitworkflow_test.go: rdev git flag parsing and CLI integration tests.
- .github/CODEOWNERS: ownership for Go, governance, security, and release surfaces.
- .github/governance/branch-ruleset.json: reviewed target main protection configuration.
- .github/governance/commit-policy.json: reviewed branch and commit policy data.
- .github/workflows/git-policy.yml: GitHub-native policy check entrypoint.
- scripts/ci/git-policy.sh and git-policy_test.sh: CI wrapper and shell tests.
- scripts/github/plan-git-governance.sh: dry-run Ruleset report.
- scripts/github/apply-git-governance.sh: explicit --execute Ruleset application.
- docs/development/GIT_WORKFLOW.md: developer and Agent workflow reference.

Modify:

- internal/cli/cli.go and cli_test.go: add git dispatch and usage coverage.
- CONTRIBUTING.md and docs/README.md: publish and index the workflow.
- .github/PULL_REQUEST_TEMPLATE.md: require Issue, branch/worktree, checks, and security evidence.
- .github/workflows/ci.yml: use branch-wide triggers and stable required job names.
- scripts/check.sh: include policy script syntax and governance validation if needed.

---

### Task 1: Implement strict branch and commit policy

**Files:** Create internal/gitworkflow/policy.go and policy_test.go.

**Produces:** BranchRef, PolicyCheck, PolicyReport, ParseBranch,
ValidateCommitSubject, ValidatePRTitle, and IssueFromBranch.

- [ ] Step 1: Write failing table-driven tests for valid names

Use these cases:

~~~
feat/123-worktree-governance -> type feat, issue 123, slug worktree-governance
docs/7-git-guide -> type docs, issue 7, slug git-guide
release/42-v1-2-0 -> type release, issue 42, slug v1-2-0
~~~

Add invalid cases codex/123-agent-work, feat/worktree-governance,
feat/abc-worktree, feat/123_Worktree, main, feature/123-worktree, and feat/123-.
Add commit cases feat: add worktree doctor and fix(cli): reject main edits as
valid; add add worktree doctor, Feat: uppercase type, and feat: as invalid.

- [ ] Step 2: Run the focused tests and verify RED

~~~
go test ./internal/gitworkflow -run 'TestParseBranch|TestValidateCommitSubject' -count=1
~~~

Expected: compilation fails because the package and functions do not exist.

- [ ] Step 3: Implement the minimal policy API

Use regexp, strconv, and strings. Define:

~~~
const SchemaVersion = "rdev.git-workflow.v1"
type BranchRef struct { Name string; Type string; Issue int64; Slug string }
type PolicyCheck struct { Name string; Passed bool; Detail string }
func ParseBranch(name string) (BranchRef, error)
func ValidateCommitSubject(subject string) error
func ValidatePRTitle(title string) error
func IssueFromBranch(name string) (int64, error)
~~~

Keep the branch regexp anchored and compiled once. Reject empty values, main
through ParseBranch, uppercase/non-ASCII slug characters, unknown types, missing
issues, and malformed separators. Add JSON tags in the implementation.

- [ ] Step 4: Run the focused tests and verify GREEN

~~~
gofmt -w internal/gitworkflow/policy.go internal/gitworkflow/policy_test.go
go test ./internal/gitworkflow -run 'TestParseBranch|TestValidateCommitSubject' -count=1
~~~

Expected: PASS.

- [ ] Step 5: Commit with feat: enforce issue-linked git branch policy.

### Task 2: Add Git command execution and repository discovery

**Files:** Create internal/gitworkflow/git.go and git_test.go.

**Produces:** Runner, ExecRunner, CommandEvidence, Repo, and temporary Git
repository helpers for later tasks.

- [ ] Step 1: Write failing tests for canonical repository discovery, successful
command argv/dir/exit evidence, non-zero exit evidence, and absence of
environment/token output.
- [ ] Step 2: Run go test ./internal/gitworkflow -run 'TestDiscoverRepo|TestExecRunner' -count=1 and verify RED.
- [ ] Step 3: Implement:

~~~
type CommandEvidence struct { Argv []string; Dir string; Stdout string; Stderr string; ExitCode int }
type Runner interface { Run(ctx context.Context, dir string, args ...string) (CommandEvidence, error) }
type ExecRunner struct{}
func (ExecRunner) Run(ctx context.Context, dir string, args ...string) (CommandEvidence, error)
func DiscoverRepo(ctx context.Context, r Runner, cwd string) (Repo, error)
~~~

Invoke Git as git -C <dir> ..., use exec.CommandContext, capture stdout/stderr,
return only trimmed Git stderr in errors, and canonicalize with filepath.Abs
and filepath.EvalSymlinks. Do not use shell evaluation or environment dumps.
- [ ] Step 4: Run gofmt and the focused tests; expect PASS.
- [ ] Step 5: Commit with feat: add structured git command runner.

### Task 3: Implement developer worktree lifecycle

**Files:** Create internal/gitworkflow/worktree.go and worktree_test.go.

**Produces:** WorktreeManager, WorktreeEntry, WorktreeReport,
DefaultWorktreeRoot, Create, List, Doctor, Clean, and Remove.

- [ ] Step 1: Write failing temporary-repository tests named
TestDefaultWorktreeRootIsOutsideRepository,
TestCreateDeveloperWorktreeUsesNormalizedBranchDirectory,
TestCreateRejectsMainAndDuplicateBinding, TestDoctorReportsDirtyAndDetachedWorktrees,
TestRemoveRejectsDirtyWorktreeWithoutForce, and TestCleanRemovesMergedCleanWorktreeOnly.
- [ ] Step 2: Run the named tests and verify RED.
- [ ] Step 3: Implement:

~~~
type WorktreeEntry struct {
    Path string; Head string; Branch string; Detached bool; Bare bool
    Clean bool; Merged bool; Ahead int; Behind int
}
type WorktreeManager struct { RepoRoot string; Root string; Git Runner }
func DefaultWorktreeRoot(repoRoot string) (string, error)
func NewWorktreeManager(repoRoot, root string, git Runner) (WorktreeManager, error)
func (m WorktreeManager) Create(ctx context.Context, branch, base string) (WorktreeReport, error)
func (m WorktreeManager) List(ctx context.Context) ([]WorktreeEntry, []CommandEvidence, error)
func (m WorktreeManager) Doctor(ctx context.Context) (WorktreeReport, error)
func (m WorktreeManager) Clean(ctx context.Context) (WorktreeReport, error)
func (m WorktreeManager) Remove(ctx context.Context, branch string, force bool) (WorktreeReport, error)
~~~

Parse git worktree list --porcelain, use git status --porcelain=v1 for
cleanliness, and git merge-base --is-ancestor for merged detection. Normalize
the root to ../.worktrees/<repo-name>, reject paths outside it, and refuse
main, detached creation, duplicate bindings, dirty removal without force, and
unmerged cleanup. Preserve command evidence.
- [ ] Step 4: Run gofmt and the focused worktree tests; expect PASS.
- [ ] Step 5: Commit with feat: manage developer git worktrees safely.

### Task 4: Add policy reports and GitHub PR planning

**Files:** Modify internal/gitworkflow/policy.go; create github.go and
github_test.go.

**Produces:** PolicyReport, PRPlan, CheckPolicy, PlanPR, and ExecutePR.

- [ ] Step 1: Write failing tests showing a valid branch produces ok=true,
codex/* produces a failed named check, matching Issue text is required, and PR
planning contains Closes #123 without executing gh.
- [ ] Step 2: Run go test ./internal/gitworkflow -run 'TestPolicyReport|TestPlanPR' -count=1 and verify RED.
- [ ] Step 3: Implement:

~~~
type PolicyReport struct { Schema string; OK bool; RepoRoot string; Branch string; Base string; Checks []PolicyCheck; Commands []CommandEvidence }
type PRPlan struct { Schema string; Base string; Head string; Title string; Body string; Args []string }
func CheckPolicy(ctx context.Context, repo Repo, r Runner, base string) (PolicyReport, error)
func PlanPR(repo Repo, branch BranchRef, title, body string) (PRPlan, error)
func ExecutePR(ctx context.Context, r Runner, repoRoot string, plan PRPlan) (CommandEvidence, error)
~~~

Run gh pr create with argv, never a shell string. Fail clearly when gh is
missing or unauthenticated and redact token-like output. Add JSON tags.
- [ ] Step 4: Run gofmt and focused tests; expect PASS.
- [ ] Step 5: Commit with feat: add git policy reports and pr plans.

### Task 5: Expose the rdev git CLI

**Files:** Create internal/cli/gitworkflow.go and gitworkflow_test.go; modify
internal/cli/cli.go and cli_test.go.

**Produces:** rdev git branch, worktree, policy, sync, and pr command groups.

- [ ] Step 1: Write App.Run tests named TestRunGitPolicyCheckEmitsJSON,
TestRunGitBranchCreateRequiresIssue, TestRunGitWorktreeCreateRejectsMain, and
TestRunGitPRPlanDoesNotExecuteGH. Use bytes.Buffer and temporary repositories.
- [ ] Step 2: Run go test ./internal/cli -run 'TestRunGit' -count=1 and verify RED.
- [ ] Step 3: Add case git: return a.git(ctx, args[1:]) to App.Run, add usage
entries, and use the existing flag.NewFlagSet style. Implement exactly:

~~~
git branch create --type TYPE --issue N --slug SLUG [--base REF] [--repo PATH]
git worktree create --branch BRANCH [--base REF] [--repo PATH] [--root PATH]
git worktree list [--repo PATH]
git worktree doctor [--repo PATH] [--root PATH]
git worktree clean [--repo PATH] [--root PATH]
git worktree remove --branch BRANCH [--repo PATH] [--root PATH] [--force]
git policy check [--repo PATH] [--base REF]
git sync [--repo PATH] [--prune]
git pr plan [--repo PATH] [--title TITLE] [--body BODY] [--base REF]
git pr create --execute [--repo PATH] [--title TITLE] [--body BODY] [--base REF]
~~~

Every success emits one JSON object with schema rdev.git-workflow.v1. pr create
must reject calls without --execute before invoking gh. Keep workspace
prepare-worktree unchanged.
- [ ] Step 4: Run gofmt, focused tests, and go run ./cmd/rdev git --help; expect PASS.
- [ ] Step 5: Commit with feat: expose git workflow commands.

### Task 6: Add GitHub governance configuration and scripts

**Files:** Create .github/governance/branch-ruleset.json,
commit-policy.json, .github/CODEOWNERS, scripts/github/plan-git-governance.sh,
apply-git-governance.sh, and git-governance_test.sh.

- [ ] Step 1: Write shell tests using a fake gh executable. Plan must produce
JSON without mutation. Apply without --execute must fail without calling gh.
Apply with --execute must call only the fake gh and redact fake credentials.
- [ ] Step 2: Run bash scripts/github/git-governance_test.sh and verify RED.
- [ ] Step 3: Encode PR-only main, one approval, required git-policy and
go-checks, conversation resolution, up-to-date branch, no force-push/deletion,
squash-only merge, and automatic head deletion. Use set -euo pipefail.
plan-git-governance.sh is read-only. apply-git-governance.sh rejects missing
--execute and uses gh api only after that flag.
- [ ] Step 4: Run the shell test and bash -n on both scripts; expect PASS.
- [ ] Step 5: Commit with chore: declare github git governance.

### Task 7: Add GitHub Actions enforcement and repository templates

**Files:** Create .github/workflows/git-policy.yml and scripts/ci/git-policy.sh
and git-policy_test.sh. Modify .github/workflows/ci.yml,
.github/PULL_REQUEST_TEMPLATE.md, CONTRIBUTING.md, and docs/README.md.

- [ ] Step 1: Write shell assertions that the wrapper fails for
codex/123-old-name, passes for feat/123-valid-name, and rejects a non-main PR
base.
- [ ] Step 2: Run bash scripts/ci/git-policy_test.sh and verify RED.
- [ ] Step 3: Run CI on all branch pushes and all PRs. Keep existing Go checks
under stable job name go-checks and add stable job name git-policy. Make the PR
template require Issue linkage, branch/worktree output, tests, security review,
and release impact. Add the workflow document to docs/README.md.
- [ ] Step 4: Run the wrapper test, bash -n, ./scripts/check.sh, and git diff --check; expect PASS.
- [ ] Step 5: Commit with ci: enforce git workflow policy on pull requests.

### Task 8: Write developer and Agent workflow documentation

**Files:** Create docs/development/GIT_WORKFLOW.md; modify CONTRIBUTING.md and
docs/README.md.

- [ ] Step 1: Document this runnable path:

~~~
gh issue create --title "Track worktree governance" --body "..."
go run ./cmd/rdev git branch create --type feat --issue 123 --slug worktree-governance --base origin/main
go run ./cmd/rdev git worktree create --branch feat/123-worktree-governance
go run ./cmd/rdev git worktree doctor
go run ./cmd/rdev git pr plan
go run ./cmd/rdev git pr create --execute
~~~

Include multi-worktree use, Draft PR timing, review/merge rules, cleanup,
dirty-worktree recovery, stale metadata recovery, hotfix/release flow, and
codex/* migration. State that --execute is required for external mutations.
- [ ] Step 2: Run rg for feat/[0-9]+-, rdev git, worktree, Squash, --execute,
and codex/ references; run git diff --check. Every command must match the CLI.
- [ ] Step 3: Commit with docs: document git branch and worktree workflow.

### Task 9: Migrate the existing development worktree

**Files:** Modify local Git refs and worktree metadata only after an Issue
number exists. Do not modify source files.

- [ ] Step 1: Record git status --short --branch, git worktree list --porcelain,
git branch -vv, and git log --oneline --decorate -5
codex/phase1-regional-tunnel-availability.
- [ ] Step 2: Create or associate the GitHub Issue and record its numeric ID.
- [ ] Step 3: Rename and move using actual paths:

~~~
git branch -m codex/phase1-regional-tunnel-availability feat/<issue>-regional-tunnel-availability
git worktree move <old-path> ../.worktrees/remote-dev-skillkit/feat-<issue>-regional-tunnel-availability
git worktree repair
~~~

Stop if dirty or destination exists.
- [ ] Step 4: After local verification, explicitly push the new branch and
delete the old remote ref:

~~~
git push --set-upstream origin feat/<issue>-regional-tunnel-availability
git push origin --delete codex/phase1-regional-tunnel-availability
~~~

- [ ] Step 5: Verify rdev git policy check, rdev git worktree doctor, git branch
-r, and git worktree list. Expected: new branch passes, old remote ref is gone,
and worktree is external and standardized. Do not create a migration commit.

### Task 10: Full verification and GitHub Ruleset activation

- [ ] Step 1: Run go test ./internal/gitworkflow -count=1 and
go test ./internal/cli -run 'TestRunGit' -count=1.
- [ ] Step 2: Run go test ./..., go vet ./..., ./scripts/check.sh, and
git diff --check. Expected: all exit 0.
- [ ] Step 3: Run rdev git policy check, rdev git worktree doctor, and
bash scripts/github/plan-git-governance.sh --repo EitanWong/remote-dev-skillkit.
Expected: local checks pass and GitHub output is plan-only.
- [ ] Step 4: Review git status, git diff origin/main...HEAD --stat, and the
full .github/internal/scripts/docs/CONTRIBUTING diff for secrets, unintended
external operations, and runtime workspace regressions.
- [ ] Step 5: Only after explicit approval run
bash scripts/github/apply-git-governance.sh --repo EitanWong/remote-dev-skillkit --execute,
then verify Ruleset required checks, squash-only merge, and auto-delete.
- [ ] Step 6: If verification finds a concrete defect, add only the affected
files and commit fix: close git governance verification gaps.

## Plan self-review

- Branch policy: Tasks 1 and 7 cover strict names, issue IDs, and CI rejection.
- Worktree path/lifecycle: Tasks 3 and 9 cover creation, inspection, cleanup, and migration.
- Evidence/redaction: Tasks 2-4 cover the shared output model and command boundary.
- CLI surface: Task 5 covers every approved command.
- GitHub governance: Tasks 6, 7, and 10 cover Ruleset, Actions, CODEOWNERS, PR template, and activation.
- Documentation: Task 8 covers developer and Agent workflows.
- Tests: Tasks 1-7 and 10 include focused, integration, shell, and repository-wide verification.
- Runtime isolation: the global constraints and Tasks 3/10 prevent changes to internal/workspace behavior.
- Migration: Task 9 requires an Issue, rename, move, remote synchronization, and verification.

No TODO or TBD markers are used. The migration-specific placeholder is <issue>,
where the real Issue number must be supplied after the approved GitHub Issue
exists; other angle-bracket values are explicit command-interface placeholders
such as repository paths and Git refs.
