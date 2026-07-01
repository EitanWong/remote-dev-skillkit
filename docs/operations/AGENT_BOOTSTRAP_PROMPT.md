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
4. Check whether `rdev` is already installed with `rdev doctor`. If it is not
   available, build/install the CLI with the repository's documented Go workflow.
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
13. If the user wants to control a remote host, build a connection-mode plan:
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
