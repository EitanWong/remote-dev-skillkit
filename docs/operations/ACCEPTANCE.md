# Acceptance Operations

This document describes repeatable local acceptance commands. These commands are not a
substitute for the real Windows VM and managed Mac release gates in
`docs/project/ACCEPTANCE_TESTS.md`, but they give contributors a deterministic way to
exercise the same safety loop before a real-environment run.

The target behavior is defined in the canonical final architecture lock,
`docs/architecture/PERFECT_ENDING_SOLUTION.md`: typed intent, signed host-bound
task payloads, host-side validation, workspace locks, host-denial probes, evidence,
audit, and revocation.

## Evidence Plan Scaffolding

Hosted provider packages write `runtime-evidence-plan.json`. Relay and
connectivity adapter packages write `acceptance-evidence-plan.json`. Before a
real deployed-provider or restrictive-network acceptance run, scaffold the
evidence directory from the package directory so Agents do not hand-pick
internal plan paths:

```bash
rdev acceptance scaffold-evidence \
  --hosted-provider-package hosted-provider \
  --out .rdev/acceptance/hosted-provider-runtime-evidence

rdev acceptance scaffold-evidence \
  --relay-adapter-package relay-adapter \
  --out .rdev/acceptance/relay-adapter-evidence
```

The lower-level `--plan <runtime-evidence-plan.json |
acceptance-evidence-plan.json>` input remains available for reviewed operator
overrides. Fresh Agents should prefer `--hosted-provider-package` or
`--relay-adapter-package`.

The command writes:

| Path | Purpose |
|---|---|
| `AGENT_CHECKLIST.md` | Human/Agent checklist with exact preflight, runner, package, and verify commands |
| `scaffold-report.json` | `rdev.acceptance-evidence-scaffold.v1` report with plan kind, evidence files, commands, checks, and next actions |
| copied plan JSON | The original machine-readable evidence plan archived next to the scaffold |

After a real run starts filling the scaffold, check readiness before packaging:

```bash
rdev acceptance evidence-status \
  --scaffold .rdev/acceptance/hosted-provider-runtime-evidence

rdev acceptance evidence-status \
  --scaffold .rdev/acceptance/relay-adapter-evidence
```

The status command emits `rdev.acceptance-evidence-status.v1` and exits
nonzero until every required evidence file exists, is non-empty, and is not a
scaffold placeholder. Agents should run the CLI-only
`rdev acceptance evidence-status` command so they can report exactly which
evidence files are still missing or placeholder-backed before attempting
`rdev acceptance package-*`.

By default the scaffold does not create placeholder evidence files. Use
`--create-placeholders` only when an Agent or operator explicitly wants empty
slots to fill during a real run. Placeholder files are marked as placeholders
and must be replaced with real redacted evidence before running any package
command. The scaffold always reports `ready_for_packaging=false`; production
claims require the later `rdev acceptance package-*` command and matching
`rdev acceptance verify-*` command to pass with `ok=true`. Hosted-provider
runtime and relay/connectivity acceptance packagers reject scaffold placeholder
files; verification also fails if placeholder markers appear in archived
`evidence/` files.

Package hosted-provider runtime evidence through the same directory-level
contract:

```bash
rdev acceptance package-hosted-provider-runtime \
  --hosted-provider-package hosted-provider \
  --out .rdev/acceptance/hosted-provider-runtime-package \
  --evidence-dir .rdev/acceptance/hosted-provider-runtime-evidence
```

The package command reads the standard hosted runtime file names from the
evidence directory, including `gateway-startup.txt`,
`storage-verification.json`, `auth-verification.json`,
`backup-evidence.txt`, `restore-evidence.txt`, `retention-evidence.txt`,
`role-mapping-evidence.json`, `failure-mode-evidence.json`, and `audit.jsonl`.
`failure-mode-evidence.json` must prove a negative probe, not just a generic
success marker: include `failure_mode_tested=true` plus a field such as
`rejected=true`, `denied=true`, `unavailable=true`, `accepted=false`, or
`authorized=false`.
Agents should not pass those as separate file flags unless a reviewed operator
override is needed.

Agents should run the CLI-only `rdev acceptance scaffold-evidence` command, then
collect the listed files instead of writing custom PowerShell,
shell, relay, gateway, or evidence-layout scripts.

## Post-Release Download Evidence Scaffolding

After generating and verifying a post-release install plan, scaffold the public
download evidence directory before GitHub Release download verification:

```bash
rdev acceptance scaffold-post-release-download \
  --post-release-install-dir post-release-install \
  --out .rdev/acceptance/post-release-download-evidence
```

The command writes `rdev.post-release-download-evidence-scaffold.v1`,
`AGENT_CHECKLIST.md`, copied plan/verification JSON, platform evidence
directories, Skillkit evidence directories when required, and the exact
`rdev acceptance package-post-release-download` / verify commands. Use
`--create-placeholders` only to create obvious local collection slots.
The lower-level `--plan <post-release-install-plan.json>` and
`--plan-verification <post-release-install-verification.json>` inputs remain
available for reviewed operator overrides. Fresh Agents should prefer
`--post-release-install-dir`.

Before packaging, check readiness:

```bash
rdev acceptance post-release-evidence-status \
  --scaffold .rdev/acceptance/post-release-download-evidence
```

The status command emits `rdev.post-release-download-evidence-status.v1` and
exits nonzero until every planned platform transcript, candidate verification,
bundle verification, and required Skillkit evidence file exists, is non-empty,
and is not a scaffold placeholder. Agents should run the CLI-only
`rdev acceptance scaffold-post-release-download` and
`rdev acceptance post-release-evidence-status` commands.

The post-release download evidence packager and verifier also reject scaffold
placeholders under archived platform evidence, Skillkit evidence, and the
post-release install verification evidence path. The readiness command is the
recommended early check, but it is not the only guard; the package and verify
commands must still fail closed if placeholder evidence reaches them.

When readiness reports `ready_for_packaging=true`, package the scaffold
directly:

```bash
rdev acceptance package-post-release-download \
  --scaffold .rdev/acceptance/post-release-download-evidence \
  --out .rdev/acceptance/post-release-download-package
```

Keep the lower-level `--plan`, `--plan-verification`, `--evidence-dir`, and
`--skillkit-evidence-dir` flags for reviewed operator overrides only. Fresh
Agents should use the scaffold-level command so they do not assemble evidence
paths by hand.

## Release Evidence Index

After real hosted-provider, relay/connectivity, and post-release download
evidence packages verify, build one local release-evidence index:

```bash
rdev acceptance release-evidence-index \
  --out .rdev/acceptance/release-evidence-index \
  --hosted-provider-runtime-package .rdev/acceptance/hosted-provider-runtime-package \
  --relay-adapter-package .rdev/acceptance/relay-adapter-package \
  --post-release-download-package .rdev/acceptance/post-release-download-package
```

The command emits `rdev.acceptance-release-evidence-index.v1`, writes
`release-evidence-index.json` and `checksums.txt`, and fails closed until all
three release-blocking evidence tracks verify. It records package manifest
hashes and verification summaries instead of copying each package manifest, so
the index does not re-archive local source paths from package metadata. Agents
should run the CLI-only `rdev acceptance release-evidence-index` command.

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

1. call `rdev.sessions.connect` first;
2. return ready `target_handoff_envelope.full_text` and compatibility
   `user_handoff` when a gateway is reachable;
3. return `cli_start_now_command` for visible foreground `rdev support-session connect --start` when no gateway is running;
4. forward `target_handoff_envelope.full_text` verbatim to the human, falling
   back to `user_handoff.message` plus `user_handoff.copy_paste` only for older
   payloads;
5. read `ready_file.path` when foreground stdout is hard to parse;
6. expose `handoff_text_file.path` for the exact plain-text target-side handoff
   so a fresh Agent can forward one file verbatim instead of parsing JSON;
7. expose `status_file.path` for the latest machine-readable foreground event
   when terminal output is unavailable, with regression coverage that drives the
   foreground watcher from `waiting` to `connected` after host registration;
8. expose `connected_report_file.path` for the exact plain-text success report
   after the target connects, so an Agent can proactively tell the user the
   connection is established before submitting session tasks;
9. expose foreground stderr feedback events so an Agent can report
   `event=connected` from the kept-open command;
10. wait for status with `rdev.sessions.status` as the fallback source of
   truth;
11. report `connected=true` through `connected_next_steps.user_report`;
12. fetch a local join page, Windows `bootstrap.ps1`, macOS/Linux
   `bootstrap.sh`, and `/assets/*.sha256` endpoints from a `httptest` gateway
   to prove clean targets can download and SHA-256 verify bootstrap binaries
   instead of being told to install `rdev` manually;
13. configure `RDEV_RELAY_GATEWAY_URL` during the local gate and prove the
    high-level handoff auto-selects that stable gateway, the target command uses
    the relay join URL, `connection_continuity_policy.stable_after_lan_change`
    is true, and the Agent runbook reports the stable fallback instead of
    asking the Agent to write relay/mesh/VPN/SSH glue;
14. include signed runtime gateway candidates in the generated target bootstrap
    URL, so the fetched join manifest can carry ordered gateway candidates to
   `rdev-bootstrap`; the verified core can select a reachable signed candidate
   before registration and later switch routes without registering again;
15. avoid custom PowerShell, shell, relay, authorization-polling, ticket, root,
   gateway, transport, or bootstrap glue;
16. include `rdev.support-session-target-handoff-envelope.v1` on created,
   connected, and started payloads, so Agents no longer need to reconstruct the
   human-facing text from separate fields;
17. include `agent_connection_runbook.fresh_agent_failure_prevention`, a
   machine-readable regression guard for real fresh-Agent failures such as
   manual gateway/invite/bootstrap assembly, missing bootstrap assets or signed
   layered metadata, background gateway workarounds, custom authorization polling,
   and Agent-written PowerShell/shell setup.

The command writes `report.json` with schema
`rdev.acceptance.fresh-agent-support-session.v1`. A passing report proves the
local contract is intact; the real multi-harness and clean-machine acceptance
runs remain required before claiming production-grade connectivity.

## Connection Entry Runner Evidence

When collecting real relay, mesh, VPN, or SSH acceptance evidence, generate the
runner result from the standard runner instead of writing JSON by hand. Start
from `rdev acceptance scaffold-evidence --relay-adapter-package <package>` so
the package decides the standard file names and package/verify commands. Read
`acceptance-evidence-plan.json` directly only for a reviewed override or
debugging session.

```bash
rdev connection-entry run \
  --runner-manifest connection-entry-runner.json \
  --evidence-dir .rdev/acceptance/relay-adapter-evidence
```

The runner writes the standard acceptance files in that directory:
`runner-result.json`, `helper-transcript.txt`, `gateway-status.json`,
`host-status.json`, `connection-status.json`, `audit.jsonl`, and
`evidence-report.json`. Package the shareable evidence through the same
directory-level contract:

```bash
rdev acceptance package-relay-adapter \
  --relay-package relay-adapter \
  --out .rdev/acceptance/relay-adapter-package \
  --evidence-dir .rdev/acceptance/relay-adapter-evidence
```

The package archives the shareable standard evidence files and does not require
Agents to pass six individual file flags. The result uses schema
`rdev.connection-entry.runner-result.v1` and records the selected path, gateway
override, helper-start status, transport, bootstrap argv, probe results, and
manual-action requirements. The helper transcript, status files, and audit JSONL
are standard evidence generated by the runner from dependency install,
helper-start, gateway-probe, bootstrap, and cleanup events, so Agents should
not hand-write them.

If a helper is missing, the runner accepts dependency repair only through
`RDEV_*_INSTALL_ACTION_JSON` with schema
`rdev.connection-entry.dependency-install-action.v1` and the standard
`rdev deps install --tool ... --scope ... --url ... --expected-sha256 ...
--execute` argv. The action's tool, scope, and SHA-256 must match the argv.
Do not put `curl`, PowerShell, shell command strings, or plan-only install
commands in helper install metadata; the runner rejects them before startup.

## Managed Mac Coding Harness

Run:

```bash
rdev acceptance managed-mac --out .rdev/acceptance/managed-mac --repo .
```

The command creates a local managed-mode acceptance run:

1. creates a managed session and target endpoint in the local control plane;
2. creates a Git worktree for the target repository;
3. runs an `adapter=codex` session task in the worktree with workspace locking enabled;
4. collects Codex output, Git status, Git diff/stat, Git diff, and verification command evidence;
5. creates a second task that attempts `git push` and confirms a structured host-denial artifact;
6. exports session evidence for the coding task;
7. exports session evidence for the side-effect probe;
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
| `evidence/` | `rdev.session-evidence.v1` manifest for the successful coding task |
| `side-effect-probe-evidence/` | session evidence for the host-denial side-effect probe |
| `worktrees/` | generated Git worktree |
| `workspace-locks/` | workspace lock store used during the run |

The report is considered passing only when all checks are true:

- managed host mode is used;
- host is active;
- worktree was created;
- coding task succeeded;
- `rdev.codex-result.v1` artifact exists;
- Git diff evidence exists;
- verification evidence exists;
- fixture runs include `rdev.test-report.v1`;
- side-effect probe returns `rdev.host-denial.v1` for `git.push`;
- workspace lock is released after execution;
- session evidence is written.

## Acceptance Verification

After a run, verify the report and both session evidence directories:

```bash
rdev acceptance verify --report .rdev/acceptance/managed-mac/report.json
```

The verifier emits `rdev.acceptance-verification.managed-mac.v1` JSON and exits
nonzero if any release-gate check fails. It validates:

- report schema and generated acceptance checks;
- coding session evidence manifest checksums;
- side-effect probe evidence manifest checksums;
- artifact index consistency;
- audit slice and hash-chained audit export;
- embedded report manifests against on-disk manifests;
- Codex result, diff, verification output, and fixture test-report evidence;
- `git.push` host-denial side-effect probe;
- workspace lock release after the task.

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
  trust, nonce, authorization, and workspace-lock stores;
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
audit, reconnect proof, and verified managed Mac session evidence, then redacts
copied evidence. It fails closed until release-gate output contains `ok=true`,
the managed Mac report verifies through `rdev acceptance verify`, and the
host-denial proof is present in the bundled evidence.

## Windows Temporary Host Plan

Before running a real clean-VM Windows acceptance, generate a checked temporary
host plan:

```bash
rdev acceptance windows-temporary \
  --out .rdev/acceptance/windows-temporary \
  --handoff-archive ./Windows-ConnectionEntry.zip
```

The command writes `windows-temporary-plan.json` with schema
`rdev.acceptance.windows-temporary-plan.v1` and a private copy of the measured
handoff ZIP. It
validates:

- the closed ZIP SHA-256, hard size limit of 1,048,576 bytes, and bounded full
  entry reads (including compressed-entry integrity) before any evidence is written;
- the exact PowerShell, process-scoped PowerShell retry, and native CMD order;
- required `rdev-bootstrap`, signed manifest, checksum, verification, and
  explicit recovery-profile entries;
- bootstrap-only launcher content with no direct full-helper startup;
- host-denial probes for package install, elevation, service management, GUI
  control, and credential changes;
- no-persistence inspection commands for services, scheduled tasks, Run keys,
  startup folders, and firewall rules.

This command does not execute PowerShell. It produces a non-sensitive summary
plan for a real Windows VM or support-host run; the private ZIP remains outside
the public evidence package.

Verify the generated plan before sending or running the launcher:

```bash
rdev acceptance verify-windows-temporary \
  --plan .rdev/acceptance/windows-temporary/windows-temporary-plan.json
```

The verifier emits `rdev.acceptance-verification.windows-temporary-plan.v1` JSON
and exits nonzero if any preflight check fails. It validates:

- plan schema and generated plan checks;
- private handoff ZIP existence, protected private file ACL, SHA-256, size, bounded
  full-entry integrity, and required entries;
- PowerShell-first selection, fallback order, bootstrap-only command, and
  non-automatic recovery;
- foreground run command, transcript commands, no-persistence checks,
  host-denial probes, and required evidence checklist.

After the real Windows VM or support-host run, package the collected evidence:

```bash
rdev acceptance package-windows-temporary \
  --plan .rdev/acceptance/windows-temporary/windows-temporary-plan.json \
  --out .rdev/acceptance/windows-temporary-evidence \
  --transcript transcript.txt \
  --release-verification rdev-verify.json \
  --audit audit.jsonl \
  --no-persistence-dir no-persistence \
  --denial-probes-dir denial-probes \
  --cold-layered-run cold-layered-run.json \
  --warm-layered-run warm-layered-run.json \
  --layered-entry-evidence layered-entry-evidence.json
```

The package command emits `rdev.acceptance-package.windows-temporary.v1` JSON,
writes `package.json` and `checksums.txt`, copies the non-sensitive plan, redacts
transcripts and verifier output, and fails closed until all required release
evidence is present:

- PowerShell/CMD attempt transcript and exactly one core-start transition;
- standalone `rdev-verify` output with `"ok": true`;
- session registration, route reselection, task, revoke, and cancellation
  audit evidence;
- cold Range-resume and warm-cache layered run reports;
- one no-persistence evidence file for services, scheduled tasks, HKCU/HKLM Run
  keys, startup folders, and firewall rules;
- one host-denial probe evidence file for package install, elevation, service
  management, GUI control, and credential change.

The private handoff ZIP is deliberately excluded from this public evidence
directory. Do not publish a Windows temporary acceptance claim from screenshots
or raw transcripts alone.

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
  --session-evidence-dir session-evidence \
  --stop-transcript stop.txt \
  --uninstall-transcript uninstall.txt
```

The package command emits `rdev.acceptance-package.linux-managed-service.v1`,
writes `package.json` and `checksums.txt`, copies the plan, generated unit,
plan-verifier output, transcripts, logs, release-gate output, audit, reconnect
proof, and managed session evidence, then redacts copied evidence. It fails closed
until release-gate output contains `ok=true`, session evidence contains a manifest
and host-denial proof, and all required service transcripts are present.

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
login or reboot, run the same locked-worktree Codex task, export service-backed
evidence, and uninstall the service without touching unrelated plists.

The next Windows acceptance run should execute the generated Windows plan on a
clean Windows 10/11 VM, collect release-verification output, host-denial
probe evidence, revocation transcript, and no-persistence inspection output.
