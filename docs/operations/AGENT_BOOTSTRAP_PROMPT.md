# Agent Bootstrap Prompt

Copy this prompt into Codex, Claude Code, Hermes, OpenClaw/OpenCode, or another
MCP-capable agent when you want the agent to install Remote Dev Skillkit for its
own runtime.

If you reached this file from the README short prompt, treat this file as the
source of truth. Prefer reading it from the local cloned checkout so relative
paths, scripts, and docs can be inspected directly.

The prompt is intentionally agent-facing. It asks the agent to probe first,
install only verified files, avoid hardcoded local paths, and ask one short
question when a required value cannot be discovered safely.

## Copy-Paste Prompt

```text
Install and connect Remote Dev Skillkit for this agent runtime.

Repository:
https://github.com/EitanWong/remote-dev-skillkit

Goal:
Make this agent able to use Remote Dev Skillkit skills and MCP tools for safe,
auditable remote development sessions.

Rules:
- Clone or update the repository first in a safe user/workspace location unless
  a current checkout already exists. Read this file from the checkout before
  executing the remaining steps.
- Probe before acting. Do not guess paths, framework names, gateway URLs, or MCP
  config locations.
- For a personal computer Agent install, default to local MCP stdio with
  `rdev mcp serve`. A hosted gateway URL is optional, not required.
- If `rdev` is not found in PATH, do not stop at "rdev is not installed".
  Recover in this order: use an already running/local rdev executable when
  known, build from the checked-out repository with `go install ./cmd/rdev`, use
  `go run ./cmd/rdev ...` as a temporary bootstrap fallback, clone then build if
  no checkout exists, or use a signed GitHub Release only after release-bundle
  verification is available.
- When remote hosts are needed, detect and choose the safest available
  connection mode: local dev gateway, LAN-reachable gateway, hosted gateway,
  relay/mesh/VPN, or SSH tunnel. Ask before using paid, external, privileged,
  persistent, or security-policy-changing resources.
- If NAT, firewall, CGNAT, or missing public DNS prevents direct hosted/LAN
  reachability, evaluate tunneling or mesh options automatically. Prefer
  open-source and free options first, such as frp, Chisel, headscale/Tailscale
  compatible mesh, or WireGuard, when they fit the operator's environment.
  Treat managed commercial tunnels as fallback choices that need explicit
  approval.
- If a required value is unclear, ask exactly one short question, wait for the
  answer, then continue.
- Do not ask humans for values the Connection Entry can discover or carry:
  target OS before join, ticket codes, manifest roots, gateway URLs, transport
  flags, release roots, checksums, helper argv, or package-candidate selection.
- Do not hardcode private server addresses, personal paths, secrets, dates, or
  machine-specific values.
- Do not weaken OS security policy, create hidden persistence, or install
  system-wide components unless I explicitly approve that specific action.
- Prefer user-scoped or workspace-scoped installation.
- Verify before copying files. Dry-run before execute when the command supports it.

Steps:
1. Detect the current OS, shell, working directory, installed Git, installed Go,
   and this agent framework. Identify whether this runtime is Codex, Claude Code,
   Hermes, OpenClaw, OpenCode, or a generic MCP-capable agent.
2. Clone or update `https://github.com/EitanWong/remote-dev-skillkit` into a
   safe user/workspace location. If a checkout already exists, inspect it and
   update only after checking for local changes.
3. Read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md`,
   `docs/operations/SKILLKIT_INSTALL.md`, and the README from the checkout.
4. Check whether `rdev` is already installed with `rdev doctor`. If available,
   run `rdev bootstrap agent-plan --repo-root .` and follow the JSON plan. If
   `rdev` is not available, build/install the CLI with the repository's
   documented Go workflow (`go install ./cmd/rdev`) or use
   `go run ./cmd/rdev bootstrap agent-plan --repo-root .` as the temporary
   bootstrap planner. Do not ask me where `rdev` is until PATH, current
   executable, checkout build, and safe clone/build recovery have been tried.
5. Run:
   - `rdev doctor`
   - `rdev mcp tools`
6. Create and verify a portable Skillkit bundle from the checked-out repository.
   For local Agent installs, omit `--gateway-url`; local MCP stdio with
   `rdev mcp serve` is enough to connect this Agent to the Skillkit tools. If I
   have provided a hosted gateway URL, include it as bundle metadata. Treat
   `https://api.example.com/v1` only as an optional hosted-gateway placeholder,
   never as a required value.
7. Generate and verify an install plan for all mainstream frameworks:
   `codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent`.
8. Determine the correct skill/instruction target directory for this current
   agent runtime. Use environment variables, framework docs, existing config,
   and installed runtime layout. If the target is still unclear, ask me one
   short question instead of inventing it.
9. Run `rdev skillkit install` as a dry-run for the detected framework and
   target directory. Review the JSON report.
10. If the dry-run is safe and there are no overwrite conflicts, run the same
   install with `--execute`. If there are conflicts, ask before using any force
   option.
11. Configure this agent's MCP client to run `rdev mcp serve`, or produce the
   exact MCP config snippet and file path if the framework requires manual
   review. Do not silently overwrite existing MCP config.
12. Run `rdev update check --repo EitanWong/remote-dev-skillkit` and
    `rdev update plan --repo EitanWong/remote-dev-skillkit` to see whether a
    newer GitHub Release is available. Treat update output as review-only
    unless I explicitly ask you to upgrade. Before replacing any binary or
    restarting a managed service, verify the selected archive checksum and
    signed `release-bundle.json` with the configured release root.
13. If the user wants to control a remote host, always build a Connection
    Entry. This is the universal target-side handoff for every scenario: owned
    durable workstation, third-party temporary repair, LAN, hosted, relay, mesh,
    SSH, or VPN-assisted connectivity. Do not ask a human to assemble ticket
    codes, manifest roots, gateway URLs, transports, release roots, or checksum
    flags. First call `rdev.support_session.connect` through MCP or run
    `rdev support-session connect`. If it returns `ready_to_send_to_human=true`,
    forward `target_handoff_envelope.full_text` verbatim when present; fall
    back to `user_handoff.message` plus `user_handoff.copy_paste` only for
    older payloads. If it returns `ready_to_send_to_human=false`,
    run the returned `cli_start_now_command` visible foreground `rdev support-session connect --start` command,
    read `handoff_text_file.path` when present and forward that plain text
    verbatim to the target-side human. If that file is not present, read
    `ready_file.path` when terminal stdout is hard to parse, then send only the
    started payload's top-level `target_handoff_envelope.full_text`, falling
    back to top-level `user_handoff.message` plus
    `user_handoff.copy_paste` only for older payloads. Read `status_file.path`
    for the latest machine-readable connection event when terminal output is
    unavailable, and read `connected_report_file.path` when it appears after
    connection establishment so you can report the ready plain-text success
    message before creating jobs. Always
    read `fresh_agent_connect_contract` when present; it is the
    machine-readable standard path for fresh Agents. It tells you how to recover
    missing `rdev`, what not to ask humans for, and which custom PowerShell,
    shell, tunnel, approval, or polling scripts are forbidden. If you are about
    to improvise setup code, stop and follow that contract instead. If local
    `rdev`, gateway state, or target helper assets are unclear, call
    `rdev.support_session.prepare` through MCP or run
    `rdev support-session prepare --build-assets` from the checkout. Follow its
    `connection_readiness`, `asset_report`, `missing_inputs`, and
    `standard_recovery` fields instead of writing custom setup code. Use its
    `agent_connection_runbook.standard_entry_tool` as the source of truth for
    ordinary "connect this computer" requests, and obey
    `agent_connection_runbook.low_level_entry_rule`: do not start with
    `rdev.invites.create`, `rdev invite create`,
    `rdev.connection_entry.plan`, or `rdev connection-entry plan` unless the
    operator explicitly asks for reviewed package materialization, approved
    managed owned-host planning, or a high-level support-session recovery
    payload names that path.
    Use its
    recommended `gateway_url_candidates` entry for target-side commands instead
    of asking me to choose a gateway URL; never hand a remote target `0.0.0.0`
    or loopback unless the target is the same machine. If this runtime has
    `RDEV_HOSTED_GATEWAY_URL`, `RDEV_RELAY_GATEWAY_URL`,
    `RDEV_MESH_GATEWAY_URL`, `RDEV_VPN_GATEWAY_URL`, or
    `RDEV_SSH_GATEWAY_URL` configured, treat them as Agent/tool metadata:
    `rdev` appends them to `gateway_url_candidates` after direct/LAN candidates
    and before loopback so the target command can fail over without custom
    relay or tunnel code. `rdev.support_session.connect`,
    `rdev.support_session.handoff`, and `rdev.support_session.create` can use the first configured
    `RDEV_*_GATEWAY_URL` when no explicit `gateway_url` was supplied, so do not
    ask me to choose a gateway URL when the runtime already has one configured.
    Read `connectivity_helper_preflight` as well: it reports configured
    `RDEV_SSH_TUNNEL_START_ARGV_JSON`, `RDEV_RELAY_START_ARGV_JSON`,
    `RDEV_MESH_START_ARGV_JSON`, `RDEV_VPN_START_ARGV_JSON`, and matching
    `RDEV_*_INSTALL_ACTION_JSON` helper metadata for SSH, relay, mesh, and VPN
    paths, validates tool allow-lists, and flags unsafe argv such as shell
    command strings, encoded commands, elevation, or `ExecutionPolicy Bypass`
    without executing anything. If helper execution is needed, use
    `rdev.connection_entry.plan` plus `rdev connection-entry run --dry-run`
    and the returned runner metadata instead of writing tunnel scripts.
    When a created or connected support-session payload includes
    `connection_entry_runner_recommendation`, use that field for durable,
    long-running, or restrictive-network support: it carries the inline invite
    JSON, the standard `rdev.connection_entry.plan` call, and the
    `rdev connection-entry run --dry-run` template. Do not reconstruct invite,
    ticket, manifest root, gateway, relay, mesh, VPN, SSH, or checksum metadata
    by hand.
    If a lower-level explicit gateway workflow is needed, call
    `rdev.support_session.create` through MCP, or
    `rdev support-session create` through the CLI, to create the session and
    obtain the ready target command, join URL, manifest root, real ticket code,
    and status watcher in one payload. The target command already tries ordered
    Connection Entry URLs on the target machine with the returned
    `connection_attempt_policy` timeout/retry behavior; do not write your own
    PowerShell, shell, relay, or approval-polling fallback. Ordinary attended
    `/join/.../bootstrap.*` handoffs use `rdev host serve --transport
    long-poll` for stable HTTPS-only connectivity. Use `--transport auto` only
    for managed or explicit advanced runner paths where WSS fallback has been
    validated. Read
    `agent_connection_runbook` first; it is the machine-readable order of
    operations for connecting, waiting, reporting, and recovering without
    custom scripts. Read
    `agent_connection_runbook.fresh_agent_failure_prevention` before writing
    any setup code: it captures known bad fresh-Agent failure patterns such as
    manual `gateway serve` plus `invite create`, missing helper assets that make
    Windows say `rdev is required`, background gateway glue, custom approval
    polling, and Agent-written PowerShell/shell bootstraps. If you are about to
    write one of those workarounds, stop and use the returned
    `cli_start_now_command`, `ready_file.path`, `status_file.path`,
    `connection_supervision`, or `rdev.support_session.prepare` recovery path
    instead. Read
    `gateway_candidate_preflight` before asking me network questions or writing
    probes; it classifies LAN/direct, same-machine, operator-provided, and
    configured hosted/relay/mesh/VPN/SSH candidates and gives the standard next
    action for each candidate. Read
    `connection_continuity_policy`: if `stable_after_lan_change=false`, treat
    LAN as an opportunistic first path and prefer a configured hosted, relay,
    mesh, VPN, or SSH gateway before promising durable connectivity. Read
    `connection_supervision` after sending the handoff: use its watch call or
    command, report `connected_next_steps.user_report` immediately when
    connected, and use its standard upgrade/recovery paths instead of writing
    network scripts when a LAN-only path times out. Treat
    `connection_supervision.automatic_downgrade_boundaries` as the source of
    truth for post-registration signed gateway candidate failover. Prefer
    `target_handoff_envelope.full_text` when telling me what to run on the
    target machine; it is already the complete localized plain-text handoff.
    Use `user_handoff.message` plus `user_handoff.copy_paste` only for older
    payloads. When `target_handoff_envelope.target` or `user_handoff.target` is
    `auto`, follow the returned `auto_target_rule`: send the join URL first,
    and use the returned platform command only if I ask for a terminal command
    or cannot open the page. Also read `target_bootstrap_requirements` and, for CLI
    create calls, `target_bootstrap_readiness`. If an existing gateway cannot
    serve the verified helper assets for the selected platform, use the
    standard `rdev support-session connect --start` or
    `rdev support-session prepare --build-assets` recovery path instead of
    asking the target-side human to install `rdev` manually or writing a custom
    downloader. If no suitable
    gateway is running yet, run `rdev support-session connect --start` in a visible
    foreground terminal. It
    prepares verified Windows/macOS/Linux helper assets when a checkout and Go
    are available, starts the local gateway, and prints
    `rdev.support-session-started.v1` with top-level
    `target_handoff_envelope`, `user_handoff`,
    `target_command`, `join_url`, `connection_supervision`, status watcher,
    asset report, recommended gateway URL candidates, and connection readiness
    plus `agent_connection_runbook` and `gateway_candidate_preflight` before listening. It keeps the full created session under `session` for
    compatibility, but fresh Agents should send only the top-level
    `target_handoff_envelope.full_text`, then use top-level
    `connection_supervision` to wait, report, and recover. Also read
    `foreground_feedback`: the foreground command emits machine-readable stderr
    lines prefixed with `rdev support session event: `, and `event=connected`
    means you should immediately tell me the connection has been established.
    It also writes the
    same JSON payload to
    `ready_file.path`
    (`support-session-ready.json` in the session work directory by default);
    read that file when the long-running foreground terminal makes stdout hard
    to parse. It also writes `handoff_text_file.path`
    (`target-handoff.txt` by default), the exact plain-text target-side handoff
    to forward verbatim without parsing JSON or rewriting commands. It also
    writes `status_file.path`
    (`support-session-status.json` by default) with the latest foreground event;
    read it and report immediately when it shows `event=connected` or
    `status.connected=true`. When the target connects, it writes
    `connected_report_file.path` (`connected-report.txt` by default), the exact
    plain-text success report to send to the user before creating jobs. Use
    `rdev.support_session.plan` or
    `rdev support-session plan`
    only for review/debug planning or when the operator asks for the underlying
    gateway argv.
    After giving the target command to the human, call
    `rdev.support_session.status` with `wait=true` through MCP or
    `rdev support-session status --wait` through CLI. Created session payloads
    include `watch_connection_status_configured_gateway`; use that returned
    command when configured gateway metadata is present, otherwise use
    `watch_connection_status`. When the status returns
    `connected=true`, proactively tell me that the connection has been
    established before creating any jobs. Then follow `connected_next_steps`:
    send `user_report`, inspect `rdev.hosts.capabilities`, and create only the
    smallest scoped job for my task. If status waiting times out or the
    target does not appear, read `connection_recovery` and follow its
    `agent_next_actions`, `standard_tools`, and `forbidden` fields. Do not
    write a custom polling loop.
    Do not write ad hoc PowerShell, shell, relay, nohup, approval, or bootstrap
    code when the plan can provide it. Do not manually combine
    `rdev gateway serve` plus `rdev invite create` for a normal support session;
    use `rdev support-session connect --start` so ready/status files,
    auto-approval, and helper assets are created together. If a low-level dev
    gateway is explicitly required, keep the default
    `--auto-build-rdev-assets` behavior enabled from a valid checkout with Go,
    or configure helper assets with `--rdev-assets-dir` / platform-specific
    asset flags before generating human-facing target commands. For lower-level package materialization
    or when `rdev.support_session.create` is not sufficient, use
    `rdev.invites.create` or `rdev invite create` so the Agent receives
    `connection_entry`, `connection_entry.package_catalog`,
    `connection_entry_plan`, manifest root, and transport fallback instructions
    together. Verify the signed join manifest, read its `package_catalog`, and
    use target OS/architecture probes to select the package candidate. Then call
    `rdev.connection_entry.plan`
    through MCP or `rdev connection-entry plan` through the CLI to materialize
    the generic Connection Entry Package Plan. Present the target-side human
    with only the selected human surface: `connection_entry.entry_url`, a visible
    script, or a signed package. When package assets are not published or
    release inputs are missing, use the catalog's visible script fallback rather
    than asking the target-side human to assemble raw values. Keep low-level
    connection parameters in Agent/tool metadata and report `missing_inputs`
    when package materialization needs more release data.
    Select host mode with `connection_entry_plan.target_selection_policy`: use
    `managed` for my own personal/fleet machines that need durable development
    access; use `attended-temporary` for third-party or one-off repair machines.
    Ask one short question before managed mode when ownership or persistence
    approval is unclear.
    If the remote machine is a company or third-party device, ask only the
    authorization question first: "Please confirm that company policy and the
    device owner allow a visible temporary Remote Dev Skillkit support session
    on this machine." After confirmation, proceed with an attended-temporary
    Connection Entry by default. Let the join page, package catalog, and
    target-side probes detect Windows/macOS/Linux instead of asking for OS
    up front.
    - If the Agent and host are on the same machine, use local MCP stdio and
      local/dev gateway flows.
    - If the Agent and host share a LAN or VPN, prefer a LAN-reachable gateway
      URL and outbound host connection.
    - If SSH to a reachable machine is already configured and authorized,
      consider an SSH tunnel.
    - If NAT/firewall/CGNAT blocks reachability, prefer open-source/free tunnel
      or mesh options first: frp for reverse proxy/NAT traversal, Chisel for
      HTTP(S)-based TCP/UDP tunneling, headscale when a self-hosted
      Tailscale-compatible control plane is appropriate, or WireGuard for a
      direct VPN. Probe whether these are already installed before suggesting
      installation.
    - If no open/free option is viable, ask before using paid hosted relay,
      cloud tunnel, DNS, firewall, service, or persistent network changes.
14. Verify the installed skill folders exist, verify `.remote-dev-skillkit/mcp/tools.json`
    exists, and run any available framework command that lists skills or MCP
    tools.
15. Report:
    - detected framework
    - installed skill target
    - whether MCP was configured or the exact snippet I need to add
    - verification commands run
    - whether the installed `rdev` is current, newer release available, or
      update status unknown
    - selected connection mode for local use
    - selected remote connection mode, if remote-host work was requested
    - selected Connection Entry mode: managed owned host or attended temporary
      target host
    - Connection Entry Package Plan status, generated package/launcher path if
      available, and missing release inputs if packaging is not ready
    - whether any tunnel/mesh/relay is needed, which option was chosen, and why
    - whether a hosted gateway is absent, optional, configured, or still needed
      for the remote-host workflow I asked for
    - any remaining values I must provide before using real remote sessions

After installation, use `host-triage` before remote work, `remote-vibe-coding`
or `safe-remote-support` to run sessions, and `remote-job-review` before
claiming completion.
```

## Expected Agent Behavior

The agent should prefer these commands after it has a checkout:

```bash
go install ./cmd/rdev
rdev doctor
rdev mcp tools
rdev bootstrap agent-plan --repo-root .

rdev skillkit export \
  --source-root . \
  --out dist/remote-dev-skillkit

rdev skillkit verify \
  --bundle dist/remote-dev-skillkit

rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan \
  --plan dist/skillkit-install/install-plan.json
```

Then it should choose the matching framework and target directory, dry-run, and
execute only after the target is clear:

```bash
rdev skillkit install \
  --bundle dist/remote-dev-skillkit \
  --framework codex \
  --target "$HOME/.codex/skills"

rdev skillkit install \
  --bundle dist/remote-dev-skillkit \
  --framework codex \
  --target "$HOME/.codex/skills" \
  --execute
```

The framework and target path above are examples. Runtime agents must replace
them with detected or confirmed values for Codex, Claude Code, Hermes,
OpenClaw, OpenCode, or a generic MCP agent.

For hosted gateway deployments, the agent may include `--gateway-url
https://api.example.com/v1` during export, replacing the placeholder with the
operator-provided gateway. For personal-machine installs, it should leave the
gateway unset and configure local MCP stdio.

## Connection Mode Selection

The agent should choose the simplest working path, in this order:

1. Local Agent install only: use `rdev mcp serve`; no gateway required.
2. Local/dev session on one machine: use the local dev gateway and loopback.
3. Shared LAN/VPN: use a LAN-reachable gateway URL and outbound host transport.
4. Existing authorized SSH path: use an SSH tunnel if it avoids new services.
5. NAT/firewall/CGNAT: prefer open-source/free tunnel or mesh tools before
   paid hosted services.
6. Hosted/public gateway: use when the operator already has one or explicitly
   approves creating one.

Once a path is selected, the user-facing output must be a universal Connection
Entry: a link, visible script, or signed package. Raw `host_command`, ticket,
root, gateway, transport, release, and checksum values are machine-readable
implementation details for the Agent and the Connection Entry Package Plan; they
must not be handed to the target-side human as a manual assembly task.

Open-source/free candidates to consider before paid relay services:

- frp: reverse proxy for exposing services behind NAT/firewall, with TCP, UDP,
  HTTP, HTTPS, and P2P support. Source: https://github.com/fatedier/frp
- Chisel: TCP/UDP tunnel over HTTP, useful when HTTP(S) egress is available.
  Source: https://github.com/jpillora/chisel
- headscale: self-hosted implementation of the Tailscale control server for
  mesh-style connectivity. Source: https://github.com/juanfont/headscale
- WireGuard: open-source VPN tunnel. Source: https://www.wireguard.com/

Before installing or enabling any tunnel/mesh component, the agent must inspect
what already exists, prefer temporary or user-scoped configuration, verify
download/source provenance, and ask before privileged, persistent, paid,
firewall, DNS, cloud, or security-policy changes.
