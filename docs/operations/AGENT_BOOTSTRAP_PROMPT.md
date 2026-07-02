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
    flags. If local `rdev`, gateway state, or target helper assets are unclear,
    first call `rdev.support_session.prepare` through MCP or run
    `rdev support-session prepare --build-assets` from the checkout. Follow its
    `connection_readiness`, `asset_report`, `missing_inputs`, and
    `standard_recovery` fields instead of writing custom setup code. Use its
    recommended `gateway_url_candidates` entry for target-side commands instead
    of asking me to choose a gateway URL; never hand a remote target `0.0.0.0`
    or loopback unless the target is the same machine. If a reachable gateway is
    already running, first call
    `rdev.support_session.create` through MCP, or
    `rdev support-session create` through the CLI, to create the session and
    obtain the ready target command, join URL, manifest root, real ticket code,
    and status watcher in one payload. If no suitable gateway is running yet,
    run `rdev support-session start` in a visible foreground terminal. It
    prepares verified Windows/macOS/Linux helper assets when a checkout and Go
    are available, starts the local gateway, and prints
    `rdev.support-session-started.v1` with the embedded ready target command,
    join URL, real ticket code, manifest root, status watcher, asset report,
    recommended gateway URL candidates, and connection readiness before
    listening. Use `rdev.support_session.plan` or `rdev support-session plan`
    only for review/debug planning or when the operator asks for the underlying
    gateway argv.
    After giving the target command to the human, call
    `rdev.support_session.status` through MCP or
    `rdev support-session status --wait` through CLI. When the status returns
    `connected=true`, proactively tell me that the connection has been
    established before creating any jobs.
    Do not write ad hoc PowerShell, shell relay, nohup, approval, or bootstrap
    code when the plan can provide it. For lower-level package materialization
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
