# Acceptance Operations

This document describes repeatable local acceptance commands. These commands are not a
substitute for the real Windows VM and managed Mac release gates in
`docs/project/ACCEPTANCE_TESTS.md`, but they give contributors a deterministic way to
exercise the same safety loop before a real-environment run.

The target behavior is defined in the canonical final architecture lock,
`docs/architecture/PERFECT_ENDING_SOLUTION.md`: typed intent, signed host-bound
envelopes, host-side validation, workspace locks, approval gates, evidence,
audit, and revocation.

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

## Managed Mac LaunchAgent Plan

Before running a real service-backed acceptance, generate a checked LaunchAgent plan:

```bash
rdev acceptance managed-mac-service \
  --out .rdev/acceptance/managed-mac-service \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --repo .
```

The command writes `service-plan.json` with schema
`rdev.acceptance.managed-mac-service-plan.v1` and a LaunchAgent plist. It validates:

- plist is written with `0600` permissions;
- label matches the generated plist;
- `RunAtLoad` and `KeepAlive` are enabled for explicit managed mode;
- host arguments include `--mode managed`, `--once=false`, transport, identity,
  trust, nonce, approval, and workspace-lock stores;
- enrollment uses either `--ticket-code` or `--manifest-url`;
- manual `rdev host service-control --execute` start/inspect/stop commands,
  managed coding acceptance, `rdev acceptance verify`, and uninstall commands are present.

This command does not execute `launchctl`. It produces the operator-reviewed plan
for the real LaunchAgent acceptance run. Use `rdev host service-control` without
`--execute` to preview the launchctl command and with `--execute` to run it.

## Windows Temporary Host Plan

Before running a real clean-VM Windows acceptance, generate a checked temporary
host plan:

```bash
rdev acceptance windows-temporary \
  --out .rdev/acceptance/windows-temporary \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --download-url https://agent.example.com/rdev-host.exe \
  --expected-sha256 <rdev-host-sha256> \
  --release-manifest-url https://agent.example.com/rdev-host.exe.rdev-release.json \
  --release-root-public-key release-root:... \
  --verifier-download-url https://agent.example.com/rdev-verify.exe \
  --verifier-sha256 <rdev-verify-sha256>
```

The command writes `windows-temporary-plan.json` with schema
`rdev.acceptance.windows-temporary-plan.v1` and `run-windows-temporary.ps1`. It
validates:

- local or URL bootstrap script availability;
- bootstrap script SHA-256 availability;
- gateway URL, ticket code, host download URL, and host SHA-256;
- release manifest, release root, verifier download URL, and verifier SHA-256;
- approval probes for package install, elevation, service management, GUI
  control, and credential changes;
- no-persistence inspection commands for services, scheduled tasks, Run keys,
  startup folders, and firewall rules.

This command does not execute PowerShell. It produces the operator-reviewed plan
for a real Windows VM or support-host acceptance run. The generated launcher is
intentionally visible and foreground-only; it does not install a service or
autorun entry.

Verify the generated plan before sending or running the launcher:

```bash
rdev acceptance verify-windows-temporary \
  --plan .rdev/acceptance/windows-temporary/windows-temporary-plan.json
```

The verifier emits `rdev.acceptance-verification.windows-temporary-plan.v1` JSON
and exits nonzero if any preflight check fails. It validates:

- plan schema and generated plan checks;
- launcher existence, private file mode, and parameter agreement with the plan;
- launcher absence of forbidden persistence or policy-weakening operations such
  as `Set-ExecutionPolicy`, service creation, scheduled-task registration,
  Run-key mutation, firewall-rule creation, or elevation through `runas`;
- local bootstrap script SHA-256 when the script path is available, or a pinned
  bootstrap SHA-256 when the launcher downloads the script by URL;
- release manifest, release root, verifier URL, host SHA-256, and verifier
  SHA-256 inputs;
- foreground run command, transcript commands, no-persistence checks, approval
  probes, and required evidence checklist.

## Current Boundary

This harness proves the managed test-process path. It does not yet prove:

- Windows clean-VM execution of the generated temporary-host plan;
- Windows no-persistence inspection output from a real machine;
- macOS LaunchAgent installed and started with `rdev host service-control --execute` after reviewing the generated plan;
- reconnect after reboot;
- OS-protected identity/trust storage;
- real Codex authentication on Eitan's managed Mac;
- production gateway authentication.

Those remain real-environment acceptance gates.

The next managed Mac acceptance command should prove the LaunchAgent path: generate
the plist, start it with the documented `launchctl` command, confirm reconnect after
login or reboot, run the same locked-worktree Codex job, export service-backed
evidence, and uninstall the service without touching unrelated plists.

The next Windows acceptance run should execute the generated Windows plan on a
clean Windows 10/11 VM, collect release-verification output, approval-required
probe evidence, revocation transcript, and no-persistence inspection output.
