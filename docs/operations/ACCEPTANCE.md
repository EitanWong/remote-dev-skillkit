# Acceptance Operations

This document describes repeatable local acceptance commands. These commands are not a
substitute for the real Windows VM and managed Mac release gates in
`docs/project/ACCEPTANCE_TESTS.md`, but they give contributors a deterministic way to
exercise the same safety loop before a real-environment run.

The target behavior is defined in
`docs/architecture/ULTIMATE_CLOSURE_DESIGN.md`: typed intent, signed host-bound
envelopes, host-side validation, workspace locks, approval gates, evidence, audit,
and revocation.

## Managed Mac Coding Harness

Run:

```bash
rdev acceptance managed-mac --out .rdev/acceptance/managed-mac --repo .
```

The command creates a local managed-mode acceptance run:

1. creates a managed ticket and approved host in the local safety kernel;
2. creates a Git worktree for the target repository;
3. runs an `adapter=codex` job in the worktree with workspace locking enabled;
4. collects Codex output, Git status, Git diff/stat, Git diff, and verification command evidence;
5. creates a second job that attempts `git push` and confirms `rdev.approval-required.v1`;
6. exports an evidence bundle for the coding job;
7. exports an evidence bundle for the approval-gate probe;
8. writes `report.json` with pass/fail checks and next steps.

If `--repo` is omitted, the command creates a fixture Git repository inside `--out`.
That fixture includes a tiny Go package, so the default verification commands include
`go test -json ./...` and the output contains `rdev.test-report.v1`.

For deterministic tests or demos without invoking a real Codex install, pass a fake
command:

```bash
rdev acceptance managed-mac \
  --out /tmp/rdev-managed-mac-acceptance \
  --codex-command /path/to/fake-codex
```

When `--codex-command` is omitted, the adapter uses the real Codex CLI default:

```bash
codex exec -C <workspace_root> --sandbox workspace-write --json <prompt>
```

## Outputs

The output directory must not exist or must be empty. The command writes:

| Path | Purpose |
|---|---|
| `report.json` | `rdev.acceptance.managed-mac.v1` report with checks and IDs |
| `evidence/` | `rdev.evidence-bundle.v1` bundle for the successful coding job |
| `approval-evidence/` | evidence bundle for the approval-required probe |
| `worktrees/` | generated Git worktree |
| `workspace-locks/` | workspace lock store used during the run |

The report is considered passing only when all checks are true:

- managed host mode is used;
- host is active;
- worktree was created;
- coding job succeeded;
- `rdev.codex-result.v1` artifact exists;
- Git diff evidence exists;
- verification evidence exists;
- fixture runs include `rdev.test-report.v1`;
- approval probe returns `rdev.approval-required.v1` for `git.push`;
- workspace lock is released after execution;
- evidence bundle is written.

## Acceptance Verification

After a run, verify the report and both evidence bundles:

```bash
rdev acceptance verify --report .rdev/acceptance/managed-mac/report.json
```

The verifier emits `rdev.acceptance-verification.managed-mac.v1` JSON and exits
nonzero if any release-gate check fails. It validates:

- report schema and generated acceptance checks;
- coding evidence bundle manifest checksums;
- approval evidence bundle manifest checksums;
- artifact index consistency;
- audit slice and hash-chained audit export;
- embedded report manifests against on-disk manifests;
- Codex result, diff, verification output, and fixture test-report evidence;
- `git.push` approval-required probe;
- workspace lock release after the job.

## Current Boundary

This harness proves the managed test-process path. It does not yet prove:

- macOS LaunchAgent installed and started with `launchctl`;
- reconnect after reboot;
- OS-protected identity/trust storage;
- real Codex authentication on Eitan's managed Mac;
- production gateway authentication.

Those remain real-environment acceptance gates.

The next managed Mac acceptance command should prove the LaunchAgent path: generate
the plist, start it with the documented `launchctl` command, confirm reconnect after
login or reboot, run the same locked-worktree Codex job, export service-backed
evidence, and uninstall the service without touching unrelated plists.
