# Skillkit Install

Remote Dev Skillkit is meant to be installable by many agent runtimes without requiring Hermes-specific assumptions.

The portable install surface is a generated bundle:

If you want your current AI agent to perform the setup, start with the
[Agent Bootstrap Prompt](AGENT_BOOTSTRAP_PROMPT.md). It gives Codex, Claude
Code, Hermes, OpenClaw/OpenCode, or a generic MCP-capable agent a copy-paste
workflow for probing the environment, building `rdev`, exporting/verifying the
bundle, installing the matching skills, and preparing MCP configuration.

```bash
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

For personal-computer Agent installs, no hosted gateway URL is required. Install
the skills and configure local MCP stdio with the install report's
`mcp_command` value. It is usually `rdev mcp serve`, but it may be an absolute
`rdev` path when Go installed the binary outside `PATH`. Add
`--gateway-url https://api.example.com/v1` only when you already have a hosted
gateway and want that URL recorded as bundle metadata.

The bundle uses schema `rdev.skillkit-evidence.v1` and contains:

- `manifest.json` with checksums and skill metadata;
- machine-readable `adaptive_configuration` metadata with schema
  `rdev.adaptive-configuration-contract.v1`;
- `skills/` with the agent-loadable workflows;
- `mcp/tools.json` with the stable tool contract metadata;
- `INSTALL.md` with generic install steps;
- `frameworks/` with notes for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.

Verification emits schema `rdev.skillkit-bundle-verification.v1` and checks the
manifest schema, required skills, required framework notes, safe relative paths,
listed file SHA-256/size, the adaptive configuration contract, required skill
probe/ask guidance, and absence of unlisted bundle files. Do not install a
bundle into any agent runtime until verification returns `ok=true` and
`adaptive_configuration_verified=true`.

## Adaptive Configuration Contract

Agents using this Skillkit must discover their environment before acting. They
should inspect the installed `rdev` binary, MCP tools, target OS, shell,
available service manager, gateway configuration, network reachability,
proxy/DNS state, NAT/firewall/CGNAT hints, SSH config, installed tunnel/mesh
tools, workspace path, installed agent adapters, and current permissions. If a
gateway URL, ticket code, root key, release URL, checksum, framework install
path, workspace root, adapter choice, tunnel/mesh authorization, or interrupt policy
cannot be discovered safely, the agent must ask a short follow-up question
instead of inventing a value.

For local Agent installs on a personal computer, gateway configuration can be
absent. The Agent should configure local MCP stdio through the install report's
`mcp_command` value and
defer gateway selection until a real remote-host workflow needs one. When remote
hosts are needed, the Agent should choose among local dev gateway, LAN-reachable
gateway, hosted gateway, SSH tunnel, or relay/mesh/VPN based on the probed
environment and operator policy. If direct reachability is blocked by NAT,
firewall, CGNAT, proxy, or DNS constraints, prefer already-configured or
open-source/free tunnel and mesh options before paid hosted relay services.
Candidate tools include frp, Chisel, headscale, and WireGuard; probe existing
installation and source provenance before suggesting any install.

For Agent installs on a VPS/cloud server or any machine with a public DNS/IP,
the Agent should recommend a stable gateway configuration after the first
working connection instead of reusing a temporary Quick Tunnel URL. Use
`RDEV_HOSTED_GATEWAY_URL=https://your-domain-or-public-gateway` for a directly
reachable hosted gateway. Use
`RDEV_CLOUDFLARED_NAMED_TUNNEL_URL=https://rdev.example.com` plus a reviewed
Cloudflare Named Tunnel token, token file, tunnel name, or start argv when a
reusable Cloudflare address is preferred. If no stable URL exists yet, the
default support-session path still uses `rdev support-session connect --start`
so the target can connect immediately through the managed public-tunnel
fallback.

Examples such as `https://api.example.com/v1`, `/Users/example`,
`/home/example`, and `C:\Users\Alice` are placeholders. Runtime agents must
replace them with operator-provided or detected values.

`rdev skillkit plan-install` adds a second, reviewable layer for mainstream
agent runtimes. It writes `rdev.skillkit-install-plan.v1`, `INSTALL_COMMANDS.md`,
and per-framework shell/PowerShell scripts. The plan also includes
`adaptive_configuration` so automation can see the required probes and
ask-if-unclear fields without scraping prose. The command does not modify Codex,
Claude Code, Hermes, OpenClaw, OpenCode, generic MCP agents, or user home
configuration by itself. Generated scripts verify the bundle before installing,
print the adaptive configuration contract, then call the standard
`rdev skillkit install --execute` path so copied skills, reference files, and
`.remote-dev-skillkit/install.json` are produced by one installer. Existing
skill directories are not overwritten unless `RDEV_SKILLKIT_FORCE=1` is set, and
MCP configuration remains an explicit review step with `rdev mcp serve`.

`rdev skillkit verify-install-plan` emits
`rdev.skillkit-install-plan-verification.v1` and checks the plan schema, bundle
verification, adaptive configuration contract, listed script SHA-256/size,
required scripts, no forbidden external mutation, bundle-verification calls,
standard installer use, script/install-command adaptive guidance, and absence
of unlisted files in the install-plan directory. Treat `ok=false` as
installation-blocking.

`rdev skillkit install` is the direct installer path. It defaults to dry-run and
emits `rdev.skillkit-install-report.v1`; no files are copied until `--execute`
is supplied. The dry-run report includes the planned `install_manifest` path and
a `write_install_manifest` action so Agents can verify that runtime freshness
diagnostics will have a manifest after execution. The installer verifies the
bundle first, resolves the framework target directory, refuses filesystem-root
targets, refuses existing skill directory conflicts unless `--force` is
supplied, copies only the verified skill folders plus
`.remote-dev-skillkit/mcp/tools.json` and framework notes, writes
`.remote-dev-skillkit/install.json` for future `rdev doctor` stale-skill
detection. The manifest also stores per-skill `SKILL.md` SHA-256 values so
`rdev doctor` can detect tampered or partially overwritten skill installs even
when the original source checkout is unavailable. The installer does not write
MCP runtime configuration. Generic MCP agents must use `--target` explicitly.

## Update Checks

Installed agents and managed hosts can check GitHub for newer releases without
changing local files:

```bash
rdev update check --repo EitanWong/remote-dev-skillkit
rdev update plan --repo EitanWong/remote-dev-skillkit
```

`rdev update check` emits `rdev.update-check.v1`. `rdev update plan` emits
`rdev.update-plan.v1`, selects the matching platform archive, and prints
download/checksum/release-bundle verification steps. Treat the plan as dry-run
evidence. Do not replace binaries, update services, or mutate agent framework
configuration until the operator authorizes the upgrade and the selected archive
and signed `release-bundle.json` verify against the configured release root.

## Generic Agent Runtime

1. Install or build the `rdev` binary.
2. Verify the exported or downloaded bundle with `rdev skillkit verify --bundle <bundle-dir>`.
3. Generate and verify an install plan with `rdev skillkit plan-install` and `rdev skillkit verify-install-plan`.
4. Run `rdev skillkit install --framework <name> --target <dir>` first as a dry-run, then re-run with `--execute`, or run only the reviewed script for the target runtime. Generic MCP agents must set an explicit target.
5. Configure MCP stdio with the install report's `mcp_command` value for local Agent installs, or configure MCP HTTP/API against a deployed gateway when one exists.
6. Keep these skills installed together:
   - `safe-remote-support`;
   - `host-triage`;
   - `remote-vibe-coding`;
   - `remote-session-review`.
7. Require the agent to export or read `rdev.session-evidence.v1` evidence before claiming remote work is complete.

## Framework Notes

- Codex: install the skill folders into the Codex skill path and configure the MCP command from the install report's `mcp_command`.
- Claude Code: install the skill files as project or user instructions and configure MCP stdio/HTTP through the runtime's MCP surface.
- Hermes: install the skills into the Hermes agent profile and use local MCP stdio or a deployed rdev gateway, depending on the detected environment.
- OpenClaw/OpenCode: install the same skill folders and MCP tool contract; no Hermes-only dependency is required.
- Generic MCP agents: use `mcp/tools.json` as the stable schema reference and call the rdev MCP server.

## Safety Contract

An agent runtime is compatible only if it preserves the safety contract:

- agents request typed session tasks, not raw host ownership;
- temporary hosts use attended foreground mode;
- high-risk actions require session interrupts or explicit authorization;
- host-side validation remains mandatory;
- evidence and audit are reviewed before completion;
- temporary sessions are revoked when finished.

## Current Limits

The generated bundle and install plan are packaging surfaces for the current local/dev implementation. Production hosted install still needs:

- authenticated MCP HTTP;
- production gateway storage;
- signed multi-platform release artifacts;
- OS-native managed service install/uninstall;
- production WSS/mTLS transport.
