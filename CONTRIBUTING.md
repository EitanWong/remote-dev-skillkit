# Contributing

Thank you for helping improve Remote Dev Skillkit. This project is safety-first
infrastructure for agent-driven remote development, so contributions must
preserve consent, auditability, and host-local control.

## Development Setup

```bash
go test ./...
go vet ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

Use focused tests while developing, then run the full verification set before
opening a pull request.

## Branch and Pull Request Workflow

- See [Git Workflow](docs/development/GIT_WORKFLOW.md) for the exact runnable
  issue, branch, worktree, PR, recovery, hotfix, release, and migration flow.
- Create issue-linked branches that match `<type>/<issue>-<slug>`, such as
  `feat/123-git-policy-workflow` or `fix/456-main-pr-base`.
- The parser requires the exact shape
  `^(feat|fix|refactor|docs|test|chore|perf|ci|hotfix|release)/([0-9]+)-([a-z0-9]+(?:-[a-z0-9]+)*)$`.
  The issue must be a positive integer, the slug must be lowercase ASCII, and
  hyphen separators are required between slug words.
- Use the approved CLI path: `go run ./cmd/rdev git branch create`, `go run
  ./cmd/rdev git worktree create`, `go run ./cmd/rdev git worktree doctor`,
  `go run ./cmd/rdev git pr plan`, then `go run ./cmd/rdev git pr create
  --execute` only when ready.
- `--execute` is required for the external PR mutation. Planning commands,
  branch creation, and local checks are not substitutes for that boundary.
- Keep developer worktrees outside the repository tree. A shared root such as
  `../.worktrees/remote-dev-skillkit` is valid. `branch create` and `git sync`
  use `--repo` only. Worktree lifecycle commands use `--repo` and optional
  `--root` from the stable/main checkout because the manager checkout is
  excluded and refused. Only policy and PR commands may omit `--repo` when run
  from inside the external worktree.
- Open pull requests against `main` only.
- GitHub required checks must stay stable as `git-policy` and `go-checks`.
- Include matching issue text in the PR body, for example `Closes #123`.
- Prefer Squash merge after review. Do not use merge commits or rebase merges
  for this workflow.
- Capture branch and worktree evidence in the pull request using:

```bash
git branch --show-current
git worktree list --porcelain | sed -n '1,8p'
```

- Before opening a PR, run `./scripts/ci/git-policy.sh` to verify the branch,
  PR base, and PR issue linkage checks enforced in GitHub Actions and
  GitHub Ruleset protection.
- Before committing CI workflow or wrapper changes, run:

```bash
bash scripts/ci/git-policy_test.sh
bash -n scripts/ci/git-policy.sh scripts/ci/git-policy_test.sh
```
- For cleanup, use `go run ./cmd/rdev git worktree clean --repo <main-checkout>
  --root <root>` for eligible merged-clean worktrees from the stable/main
  checkout; use `go run ./cmd/rdev git worktree remove --repo <main-checkout>
  --root <root> --branch <branch> [--force]` only for a specific eligible
  target that was not already cleaned. `--force` bypasses the dirty check for
  merged worktrees but does not override the unmerged safety check.

## Contribution Rules

- Follow the Agent engineering discipline:
  - Be ashamed of guessing interfaces; be proud of reading the source,
    contracts, docs, schemas, and tests first.
  - Be ashamed of vague execution; be proud of asking for confirmation when
    requirements, environment, authority, or risk are unclear.
  - Before giving a final plan or answer for ambiguous or high-impact work, ask
    the human one question at a time, continue from the answer with the next
    single question, and proceed only when there is about 95% confidence in the
    real goal, constraints, and success criteria.
  - Be ashamed of inventing business intent; be proud of getting human
    confirmation for product goals, customer impact, and policy decisions.
  - Be ashamed of creating unnecessary interfaces; be proud of reusing existing
    APIs, protocol objects, skills, MCP tools, adapters, and project patterns.
  - Be ashamed of skipping verification; be proud of proactively running
    focused tests, full tests, release smoke, readiness checks, and privacy
    scans when relevant.
  - Be ashamed of breaking architecture; be proud of following the signed-task,
    host-policy, authorization, evidence, audit, and release-trust contracts.
  - Be ashamed of pretending to understand; be proud of saying what is unknown
    and how it will be checked.
  - Be ashamed of blind modification; be proud of cautious refactoring that is
    scoped, reversible, reviewed by tests, and aligned with existing structure.
- Use deep reasoning discipline without exposing private chain-of-thought:
  - Rephrase the request, identify explicit and implicit requirements, and map
    knowns, unknowns, constraints, risks, and success criteria before acting.
  - Keep multiple interpretations and implementation paths alive until evidence
    or human confirmation makes the right path clear.
  - Scale analysis to task risk: streamline obvious one-line fixes, but expand
    reasoning for security, release, transport, enrollment, hosted auth/storage,
    connection entry, or remote-control changes.
  - Test assumptions against source, contracts, schemas, docs, and existing
    behavior; correct course when new evidence contradicts the initial plan.
  - Track progress explicitly during complex work: what is established, what is
    still uncertain, current confidence, and what verification remains.
  - Share concise, auditable reasoning summaries and evidence with humans, not
    private internal reasoning or chain-of-thought.
- Keep temporary support visible, foreground, revocable, and non-persistent.
- Keep managed service mode explicit, inspectable, stoppable, and uninstallable.
- Do not add hidden persistence, inbound public listeners on target hosts, UAC or
  sudo bypasses, credential scraping, policy weakening, or unrestricted shell
  access.
- New host capabilities must be signed, host-validated, auditable, revocable, and
  covered by tests.
- High-risk operations such as package installation, elevation, GUI control,
  service changes, publishing, deployment, credential changes, push, and merge
  must go through authorization gates.
- Public adapter integrations must use the adapterkit conformance surfaces when
  possible.

## Pull Request Checklist

- [ ] The change is scoped to the requested behavior.
- [ ] Tests cover success and failure paths.
- [ ] `go test ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] `./scripts/check.sh` passes.
- [ ] `./scripts/ci/git-policy.sh` passes for the branch and PR metadata.
- [ ] `./scripts/ci/release-smoke.sh` passes when release, Skillkit, GitHub,
      enrollment, or adapter behavior changes.
- [ ] Documentation, Skillkit notes, and release checklists are updated when user
      behavior changes.
- [ ] No secrets, tokens, private keys, local `.rdev/` state, or target-machine
      transcripts are committed.

## External Actions

Scripts under `scripts/github/` default to dry-run or plan generation where
possible. Do not create repositories, publish releases, push tags, mutate issues,
or upload artifacts unless the maintainer has explicitly authorized that external
operation.
