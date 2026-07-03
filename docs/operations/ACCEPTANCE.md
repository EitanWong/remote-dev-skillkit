# Acceptance Operations

This document describes repeatable local acceptance commands. These commands are not a
substitute for the real Windows VM and managed Mac release gates in
`docs/project/ACCEPTANCE_TESTS.md`, but they give contributors a deterministic way to
exercise the same safety loop before a real-environment run.

The target behavior is defined in the canonical final architecture lock,
`docs/architecture/PERFECT_ENDING_SOLUTION.md`: typed intent, signed host-bound
envelopes, host-side validation, workspace locks, approval gates, evidence,
audit, and revocation.

## Fresh-Agent Support-Session Contract Gate

Run:

```bash
rdev acceptance fresh-agent-support-session \
  --out .rdev/acceptance/fresh-agent-support-session
```

This is a local contract gate for the AI-native support-session surface. It does
not start a gateway listener, contact a remote host, install a service, open a
tunnel, or prove real Codex/Claude Code/Hermes/OpenClaw behavior. Instead, it
checks that the standard tool payloads still let a fresh Agent do the intended
one-message flow:

1. call `rdev.support_session.connect` first;
2. return ready `user_handoff` when a gateway is reachable;
3. return `cli_start_now_command` for visible foreground `rdev support-session connect --start` when no gateway is running;
4. send only `user_handoff.message` plus `user_handoff.copy_paste` to the human;
5. read `ready_file.path` when foreground stdout is hard to parse;
6. expose foreground stderr feedback events so an Agent can report
   `event=connected` from the kept-open command;
7. wait for status with `rdev.support_session.status` as the fallback source of
   truth;
8. report `connected=true` through `connected_next_steps.user_report`;
9. avoid custom PowerShell, shell, relay, approval-polling, ticket, root,
   gateway, transport, or bootstrap glue.

The command writes `report.json` with schema
`rdev.acceptance.fresh-agent-support-session.v1`. A passing report proves the
local contract is intact; the real multi-harness and clean-machine acceptance
runs remain required before claiming production-grade connectivity.

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
  --repo . \
  --release-bundle /opt/rdev/release-bundle.json \
  --release-root-public-key release-root:... \
  --release-require-artifacts rdev,rdev-host,rdev-verify

rdev acceptance verify-managed-mac-service \
  --plan .rdev/acceptance/managed-mac-service/service-plan.json
```

The command writes `service-plan.json` with schema
`rdev.acceptance.managed-mac-service-plan.v1` and a LaunchAgent plist. The
verifier emits `rdev.acceptance-verification.managed-mac-service-plan.v1`. It
validates:

- plist is written with `0600` permissions;
- label matches the generated plist;
- `RunAtLoad` and `KeepAlive` are enabled for explicit managed mode;
- host arguments include `--mode managed`, `--once=false`, transport, identity,
  trust, nonce, approval, and workspace-lock stores;
- release-bundle startup gate arguments are present;
- enrollment uses either `--ticket-code` or `--manifest-url`;
- manual `rdev host service-control --execute` start/inspect/stop commands,
  managed coding acceptance, `rdev acceptance verify`, and uninstall commands are present.

This command does not execute `launchctl`. It produces the operator-reviewed plan
for the real LaunchAgent acceptance run. Use `rdev host service-control` without
`--execute` to preview the launchctl command and with `--execute` to run it.

After the real service-backed managed Mac run, package the collected evidence:

```bash
rdev acceptance package-managed-mac-service \
  --plan .rdev/acceptance/managed-mac-service/service-plan.json \
  --out .rdev/acceptance/managed-mac-service-evidence \
  --review-transcript review.txt \
  --start-transcript start.txt \
  --inspect-transcript inspect.txt \
  --logs launchagent.log \
  --release-gate release-gate.json \
  --audit audit.jsonl \
  --reconnect reconnect.txt \
  --managed-report .rdev/acceptance/managed-mac/report.json \
  --stop-transcript stop.txt \
  --uninstall-transcript uninstall.txt
```

The package command emits `rdev.acceptance-package.managed-mac-service.v1`,
writes `package.json` and `checksums.txt`, copies the verified plan, LaunchAgent
plist, plan-verifier output, service transcripts, logs, release-gate output,
audit, reconnect proof, and verified managed Mac evidence bundle, then redacts
copied evidence. It fails closed until release-gate output contains `ok=true`,
the managed Mac report verifies through `rdev acceptance verify`, and the
approval-required proof is present in the bundled evidence.

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
  --release-bundle-url https://agent.example.com/release-bundle.json \
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
- signed release manifest or signed release bundle, release root, verifier
  download URL, verifier SHA-256, and bundle required artifacts when bundle mode
  is used;
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
- signed release manifest or signed release bundle, release root, verifier URL,
  host SHA-256, verifier SHA-256, and bundle required-artifact inputs;
- foreground run command, transcript commands, no-persistence checks, approval
  probes, and required evidence checklist.

After the real Windows VM or support-host run, package the collected evidence:

```bash
rdev acceptance package-windows-temporary \
  --plan .rdev/acceptance/windows-temporary/windows-temporary-plan.json \
  --out .rdev/acceptance/windows-temporary-evidence \
  --transcript transcript.txt \
  --release-verification rdev-verify.json \
  --audit audit.jsonl \
  --no-persistence-dir no-persistence \
  --approval-probes-dir approval-probes
```

The package command emits `rdev.acceptance-package.windows-temporary.v1` JSON,
writes `package.json` and `checksums.txt`, copies the plan and launcher, redacts
transcripts and verifier output, and fails closed until all required release
evidence is present:

- PowerShell transcript from bootstrap and foreground host startup;
- standalone `rdev-verify` output with `"ok": true`;
- host registration, approval, job, approval-required, revoke, and cancellation
  audit evidence;
- one no-persistence evidence file for services, scheduled tasks, HKCU/HKLM Run
  keys, startup folders, and firewall rules;
- one approval-probe evidence file for package install, elevation, service
  management, GUI control, and credential change.

Use the packaged directory as the release-candidate artifact. Do not publish a
Windows temporary acceptance claim from screenshots or raw transcripts alone.

## Linux Managed Service

Generate and verify a Linux systemd user-service acceptance plan before running
service-manager commands on a real Linux host:

```bash
rdev acceptance linux-managed-service \
  --out .rdev/acceptance/linux-managed-service \
  --binary /opt/rdev/rdev \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --release-bundle /opt/rdev/release-bundle.json \
  --release-root-public-key release-root:... \
  --release-require-artifacts rdev,rdev-host,rdev-verify

rdev acceptance verify-linux-managed-service \
  --plan .rdev/acceptance/linux-managed-service/linux-managed-service-plan.json
```

These commands write and verify `rdev.acceptance.linux-managed-service-plan.v1`
and a reviewed `0600` systemd user unit. They do not run `systemctl`.

After the real Linux host run, package the collected evidence:

```bash
rdev acceptance package-linux-managed-service \
  --plan .rdev/acceptance/linux-managed-service/linux-managed-service-plan.json \
  --out .rdev/acceptance/linux-managed-service-evidence \
  --start-transcript start.txt \
  --status-transcript status.txt \
  --logs journal.txt \
  --release-gate release-gate.json \
  --audit audit.jsonl \
  --reconnect reconnect.txt \
  --job-evidence-dir job-evidence \
  --stop-transcript stop.txt \
  --uninstall-transcript uninstall.txt
```

The package command emits `rdev.acceptance-package.linux-managed-service.v1`,
writes `package.json` and `checksums.txt`, copies the plan, generated unit,
plan-verifier output, transcripts, logs, release-gate output, audit, reconnect
proof, and managed job evidence, then redacts copied evidence. It fails closed
until release-gate output contains `ok=true`, job evidence contains a manifest
and approval-required proof, and all required service transcripts are present.

Use the packaged directory as the Linux managed-service release evidence
artifact. Do not publish Linux managed-service support from a generated plan
alone.

## Current Boundary

This harness proves the managed test-process path. It does not yet prove:

- Windows clean-VM execution of the generated temporary-host plan;
- Windows no-persistence inspection output from a real machine;
- Linux systemd user-service execution, reconnect, and packaged evidence from a
  real Linux host;
- macOS LaunchAgent installed and started with `rdev host service-control --execute` after reviewing the generated plan;
- reconnect after reboot;
- OS-protected identity/trust storage;
- real Codex authentication on an operator's managed Mac;
- production gateway authentication.

Those remain real-environment acceptance gates.

The next managed Mac acceptance command should prove the LaunchAgent path: generate
the plist, start it with the documented `launchctl` command, confirm reconnect after
login or reboot, run the same locked-worktree Codex job, export service-backed
evidence, and uninstall the service without touching unrelated plists.

The next Windows acceptance run should execute the generated Windows plan on a
clean Windows 10/11 VM, collect release-verification output, approval-required
probe evidence, revocation transcript, and no-persistence inspection output.
