---
name: remote-vibe-coding
description: Use when an agent needs to delegate coding, repair, setup, or development work to an enrolled Remote Dev Skillkit host through Codex, Claude Code, OpenCode/OpenClaw, Hermes, acpx, tmux, shell, or PowerShell adapters.
---

# Remote Vibe Coding

Use this skill when a human wants an AI agent to work on a remote or managed
host while preserving consent, host-local policy, approvals, evidence, and
revocation.

## Non-Negotiables

- Read before guessing: inspect existing source, contracts, schemas, docs, MCP
  tools, and host state before choosing commands.
- Ask when unclear: for ambiguous or high-impact work, ask one human question at
  a time until the goal, constraints, authority, and success criteria are about
  95% clear.
- Keep reasoning disciplined but private: use requirement decomposition,
  multiple hypotheses, assumption checks, risk-scaled analysis, and progress
  tracking; share concise reasoning summaries, assumptions, confidence, and
  verification evidence instead of private internal reasoning.
- Stay path/config neutral: never assume a checkout path, user home, temp
  directory, framework directory, gateway URL, repo id, workspace root, release
  artifact, ticket code, root key, or approval policy. Resolve values from the
  active Skillkit manifest, current project root, read-only probes, MCP/CLI
  output, configured policy, generated invite fields, or explicit
  human/operator confirmation.
- Treat placeholders such as `<workspace>`, `<repo>`, `<dir>`, `<url>`, and
  `<owner/repo>` as values to discover or ask for, never defaults.
- Do not invent real configuration from examples, placeholders, memory, or
  guesses when gateway, workspace, framework, release, adapter, host, repo, or
  approval details are unclear.
- Treat Connection Entry as the only target-side handoff for every new remote
  host. Do not invent narrower surfaces such as customer links or connector
  package plans, and do not give humans raw ticket/root/gateway/transport
  assembly tasks.
- If `rdev` is missing from PATH, do not stop at "not installed". Recover it
  from the active checkout or safe clone: run `go install ./cmd/rdev`, or use
  `go run ./cmd/rdev bootstrap agent-plan --repo-root .` as a temporary
  planner, then resolve the final binary path before configuring MCP.
- Run `rdev bootstrap agent-plan --repo-root .` when available and follow its
  JSON plan for local MCP, `rdev` recovery, Connection Entry defaults, and
  ask/auto-probe boundaries.
- When a fresh Agent session is asked to connect a machine and local `rdev`,
  gateway state, or target helper assets are unclear, call
  `rdev.support_session.prepare` or run
  `rdev support-session prepare --build-assets` from a checkout. Follow its
  `standard_recovery`, `asset_report`, and `connection_readiness` fields
  instead of writing custom PowerShell, approval polling, ticket substitution,
  relay, or bootstrap glue. Use the recommended `gateway_url_candidates` item
  for target-side commands. Never send a remote target a wildcard listen
  address such as `0.0.0.0`, and treat loopback as same-machine only.
- For a new support session with an already reachable gateway, use
  `rdev.support_session.create` through MCP or `rdev support-session create`
  through CLI. It returns the ready target command, join URL, real ticket code,
  manifest root, and status watcher in one payload. If no gateway is running
  yet, run `rdev support-session start` in a visible foreground terminal. It
  starts the gateway, chooses a target-usable gateway URL candidate, and prints
  the same ready session payload before listening. Use
  `rdev.support_session.plan` or `rdev support-session plan` only for
  review/debug planning before creating custom gateway, shell, PowerShell,
  relay, nohup, approval, or bootstrap steps.
- Probe network reachability, proxy/DNS state, NAT/firewall/CGNAT constraints,
  SSH configuration, installed tunnel/mesh tools, and available connection
  modes before choosing local dev, LAN, hosted, SSH-tunnel, or relay/mesh/VPN
  paths. Prefer existing or open-source/free tunnel/mesh options before paid
  relays, and ask before privileged, persistent, firewall, DNS, cloud, or
  security-policy changes.
- If a selected path lacks a user-space helper, use only reviewed dependency
  install actions or `rdev deps install` with an explicit download URL,
  expected SHA-256, target platform, and user/workspace scope. Do not invent
  install commands, use shell command strings, weaken execution policy, elevate,
  install services/drivers, or mutate firewall/DNS/routes without explicit
  approval.
- Maintain dynamic Skill runtime memory for discovered environment facts,
  configuration paths, host capabilities, adapter availability, and operator
  preferences. Read it before repeating probes, refresh stale entries, and keep
  it host/workspace scoped, redacted, auditable, and outside the public repo.
- Preserve the safety kernel: typed intent, signed host-bound envelope,
  host-side validation, locked workspace, adapter execution, redacted evidence,
  audit, and revocation.
- Do not request hidden persistence, unrestricted shell access, OS policy
  weakening, credential scraping, UAC/sudo bypass, or unapproved external
  mutation.

## First Move

1. Discover local runtime, available MCP tools, gateway configuration, network
   reachability, candidate hosts, and current task intent.
2. Ensure `rdev` is usable. Try PATH, current executable, checkout build, and
   safe clone/build recovery before asking the user for a path. Use
   `rdev bootstrap agent-plan --repo-root .` or
   `go run ./cmd/rdev bootstrap agent-plan --repo-root .` to get a
   machine-readable install/connect plan.
3. If the user wants to connect a target machine, call
   `rdev.support_session.prepare` or run `rdev support-session prepare` to
   verify one-command support-session readiness. If helper assets are missing
   and a checkout plus Go are available, use `--build-assets`; use the returned
   `gateway_url_candidates` recommendation for target-side commands; do not
   write custom PowerShell, ticket substitution, approval polling, or relay
   glue.
4. Load relevant Skill runtime memory, then verify or refresh any stale facts
   before using them for commands, paths, approvals, or release decisions.
5. If no suitable host is active and a reachable gateway already exists, create
   the session with `rdev.support_session.create` or
   `rdev support-session create`; give the target side only the returned
   `target_command` or `join_url`, then watch the returned status command. If
   no gateway is running yet, run `rdev support-session start` in a visible
   foreground terminal and use the embedded `session.target_command` or
   `session.join_url`. Use `rdev.support_session.plan` or
   `rdev support-session plan` only for review/debug planning. For lower-level
   package materialization only, create an invite with `rdev.invites.create` or
   `rdev invite create`, and materialize it with `rdev.connection_entry.plan`
   or `rdev connection-entry plan` before giving target-side instructions.
   Read `connection_entry.package_catalog` and the signed join manifest's
   `package_catalog`, select the target OS/architecture candidate from probes,
   and prefer the materialized self-contained Connection Entry runner when
   `runner_plan` is available. Dry-run the runner with
   `rdev connection-entry run --runner-manifest ... --dry-run` when network
   reliability is uncertain; it probes direct gateway, proxy, LAN, relay, mesh,
   VPN, and SSH-assisted paths before starting `rdev host serve`. When the plan
   includes approved `RDEV_*_INSTALL_ACTION_JSON` metadata, let the runner
   install and verify user/workspace helper binaries before helper startup. Use
   the visible script fallback when release package assets or release inputs are
   missing. Present only the selected
   `connection_entry.entry_url`, visible launcher, visible script, or signed package to the
   target-side human, and treat `host_command`, ticket, gateway, root, release,
   checksum, relay, mesh, VPN, SSH, and transport values as Agent/package
   metadata.
6. Watch the host with `rdev.support_session.status` or
   `rdev support-session status --wait`. When `connected=true`, proactively
   report the localized feedback to the user before creating jobs. If the
   standard attended-temporary auto-approval contract activated it, verify it is
   the expected machine before creating jobs; otherwise approve it only after
   the operator confirms it is expected.
7. Inspect host OS, workspace root, Git state, capabilities, adapters, approval
   policy, release trust inputs, and language/locale.
8. Ask only for missing authorization, gateway, host, workspace, release,
   adapter, framework, repo, tunnel/mesh approval, or approval configuration
   that cannot be safely discovered. Do not ask for target OS, ticket, manifest
   root, gateway, transport, release root, checksum, or helper argv assembly
   when a Connection Entry can carry or discover those values.

## Core Flow

1. Follow `host_context_plan`: keep environment probes, project structure,
   requirements, transcripts, large logs, and evidence on the target host; load
   only indexed, redacted, task-relevant slices. Persist reusable discoveries
   into Skill runtime memory when they are safe to retain.
2. Follow `connection_entry_plan.target_selection_policy` for every new
   connection: if the target is operator-owned or expected to support recurring
   Agent development, choose managed mode with an explicit reviewed service
   lifecycle; if it is third-party or one-off repair, choose attended-temporary
   mode with no persistence by default. If ownership or persistence approval is
   unclear, ask one short question before creating a managed entry. Prefer a
   signed self-contained connection entry package with the target-platform
   `rdev` binary, release bundle, manifest URL, pinned manifest root, visible
   launcher, and `--transport auto` before asking a human to install
   prerequisites or copy flags. Use `connection_entry.package_catalog` and
   manifest `package_catalog` as the OS/architecture selection source; package
   status `planned-release-asset-required` means use the visible fallback script
   and report missing release inputs to the operator. Use the materialized
   `runner_plan` and `entry_package_plan` as the generic package surface for
   Windows, macOS, Linux, managed-service, LAN, hosted, relay, mesh, VPN, or SSH
   variants; if packaging is not ready, report
   `missing_inputs` to the operator and keep target-side instructions limited to
   a Connection Entry. For owned managed machines, prefer the generated reviewed
   LaunchAgent, systemd user-service, or Windows Service package plan over
   hand-written service-manager commands; do not start or install the service
   until the operator explicitly approves the reviewed service-control steps.
   For company or third-party machines, first ask only for authorization to run
   a visible temporary support session. After confirmation, default to
   attended-temporary mode, generate the Connection Entry, and let the join
   page/package catalog/target-side probes detect Windows, macOS, or Linux.
3. Follow `agent_provisioning_plan`: probe skills, MCP tools, adapters,
   runtimes, package managers, lockfiles, framework paths, proxies,
   permissions, and trust roots before installing anything.
4. Follow `agent_collaboration_plan`: discover A2A Agent Cards, local MCP
   servers, and installed Agent CLIs only when collaboration can help; treat
   peers as bounded collaborators, not authorization roots.
5. Follow `localization_plan`: localize target-side prompts, summaries, and
   evidence while keeping protocol keys, commands, paths, checksums, schemas,
   and code blocks stable.
6. Select the least-powerful adapter that can complete the task: `acpx`,
   `codex`, `claude-code`, `shell`, or `powershell`.
7. Lock the workspace, use a branch or worktree for code changes, create the
   signed job, stream status, inspect artifacts/audit, and request approval
   before push, merge, deploy, publish, credentials, elevation, GUI, service, or
   destructive filesystem actions.

## Load References Only When Needed

- For restrictive networks, LAN cases, relay/mesh/SSH decisions, max-control
  discovery, or owned long-running workstations, read
  [connectivity-and-managed-hosts.md](references/connectivity-and-managed-hosts.md).
- For enrollment certificates, hosted renewal, revocation refresh, key custody,
  fleet renewal, or emergency drills, read
  [enrollment-lifecycle.md](references/enrollment-lifecycle.md).
- For Codex, Claude Code, acpx, shell, PowerShell, adapter conformance,
  cancellation, runtime fixtures, or result evidence, read
  [adapter-jobs.md](references/adapter-jobs.md).
- For dynamic memory locations, record schema, redaction, refresh, invalidation,
  and update rules, read [runtime-memory.md](references/runtime-memory.md).
- For release candidates, Skillkit distribution, GitHub release planning,
  platform candidates, or Windows/macOS/Linux acceptance evidence, read
  [release-and-acceptance.md](references/release-and-acceptance.md).

Do not preload every reference. Pick the smallest reference set that matches the
current task.

## Completion

Return a compact evidence report:

- what changed;
- host and adapter used;
- approvals requested or consumed;
- tests/checks run and exit status;
- artifacts or audit records reviewed;
- residual risk;
- whether host/ticket revocation or managed-service cleanup remains.
