# Remote Dev Skillkit

Remote Dev Skillkit ist eine Sicherheitsschicht für KI-Agenten, die auf echten Mac-, Windows- oder Linux-Maschinen arbeiten müssen. Es ist kein verstecktes Fernsteuerungswerkzeug: Codex, Claude Code, Hermes, OpenClaw/OpenCode und generische MCP-Agenten können echte Entwicklungsarbeit über signierte Jobs, lokale Host-Policy, Freigaben, Evidence Bundles und Audit Chains ausführen.

## Highlights

- Ein portables Agent Skillkit für gängige Agent Frameworks.
- Jobs werden signiert, geprüft und erst dann ausgeführt; keine rohe Shell als Freifahrtschein.
- Skills erkennen OS, shell, service manager, gateway, workspace, adapter und Berechtigungen; wenn etwas unklar ist, fragen sie.
- Adapter für Codex, Claude Code, ACP/acpx, shell, PowerShell und eigene adapters.
- Open-Source-Lizenz Apache-2.0.

## Schnellinstallation

Wenn du bereits in Codex, Claude Code, Hermes, OpenClaw/OpenCode oder einem anderen MCP-Agenten bist, kopiere dies in deinen Agenten:

```text
Bootstrap Remote Dev Skillkit for this agent runtime.

Repository: https://github.com/EitanWong/remote-dev-skillkit

Clone or update the repository in a safe user/workspace location. Then read
`docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow it as
the source of truth. If cloning is blocked, read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Install the Skillkit and configure MCP for this agent. Probe OS, shell, Git, Go,
`rdev`, current agent framework, skill directory, MCP config, and network state
before acting. Ask one short question when a required value is unclear. For this
personal computer, prefer local MCP stdio with `rdev mcp serve`; do not require a
hosted gateway URL.

If `rdev` is missing, do not stop. Recover it from an existing local rdev binary, build this checkout with `go install ./cmd/rdev`, use `go run ./cmd/rdev bootstrap agent-plan --repo-root .` as a temporary planner, or clone then build in a safe user/workspace location. After `rdev` works, run `rdev bootstrap agent-plan --repo-root .` and follow the JSON plan.

When I ask you to work on another machine, always create a Connection Entry.
Treat it as the universal target-side handoff for every scenario: my own durable
computer, a third-party temporary repair machine, LAN, hosted, relay, mesh, SSH,
or VPN-assisted connectivity. Do not ask humans to assemble ticket, root,
gateway, transport, release, or checksum flags. First call
`rdev.support_session.handoff` through MCP or run `rdev support-session handoff`;
follow `selected_path`, `mcp_next_tool`, `mcp_next_arguments`, or
`foreground_start_command` instead of guessing whether to prepare, create, start,
or plan. If local `rdev`, gateway state, or target helper assets are unclear,
call `rdev.support_session.prepare` through MCP or run
`rdev support-session prepare --build-assets` from the checkout. Follow its `connection_readiness`, `asset_report`, `missing_inputs`,
and `standard_recovery` fields instead of writing custom bootstrap, approval, or
relay glue. Use the recommended `gateway_url_candidates` entry for target-side
commands; never send a remote target `0.0.0.0`, and treat loopback as
same-machine only. If `RDEV_HOSTED_GATEWAY_URL`, `RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`, `RDEV_VPN_GATEWAY_URL`, or `RDEV_SSH_GATEWAY_URL` is configured, `rdev` appends it to `gateway_url_candidates` after direct/LAN candidates and before loopback so the target command can fail over without custom tunnel scripts. `handoff` and `create` can use the first configured `RDEV_*_GATEWAY_URL` when no explicit `gateway_url` was supplied. If a reachable gateway is already running, call
`rdev.support_session.create` through MCP or `rdev support-session create`
through CLI to get the ready target command, join URL, real ticket code,
manifest root, scoped auto-approval state, and status watch command in one
payload. Read `target_bootstrap_requirements` and, for CLI-created sessions, `target_bootstrap_readiness`; if readiness is false for a platform command, recover with `rdev support-session start` or `rdev support-session prepare --build-assets` instead of asking the target-side human to install `rdev` manually. Prefer `user_handoff.message` plus `user_handoff.copy_paste` for
the human-facing response. When `user_handoff.target` is `auto`, follow
`user_handoff.auto_target_rule`: send the join URL first and use returned
platform commands only when a terminal command is needed. The target command already tries ordered Connection
Entry URLs on the target machine with bounded timeout/retry behavior, so do not
write custom fallback scripts. If no gateway is
running yet, run `rdev support-session start` in a visible foreground terminal;
it prepares verified helper assets when possible, starts the local gateway,
selects a target-usable gateway URL candidate, and prints the same ready session
payload before listening. Do not manually combine `rdev gateway serve` plus `rdev invite create` for ordinary support sessions; use `rdev support-session start` so verified helper assets are served automatically. Use
`rdev.support_session.plan` or `rdev support-session plan` only
for review/debug planning. After giving the target-machine command, watch
`rdev.support_session.status` with `wait=true` or `rdev support-session status --wait`; CLI status can omit `--gateway-url` when a configured `RDEV_*_GATEWAY_URL` exists. Created session payloads include `watch_connection_status_configured_gateway`; use that returned command when configured gateway metadata is present. When
`connected=true`, proactively tell me the connection has been established, follow
`connected_next_steps.user_report`, inspect `rdev.hosts.capabilities`, and only
then create the smallest scoped job for my task. If waiting times out or the target does not appear, read
`connection_recovery` and follow its standard tools instead of writing custom
recovery scripts. Use
`rdev.invites.create`, then materialize it with `rdev.connection_entry.plan` or
`rdev connection-entry plan` only when lower-level invite materialization is
needed.
Read `connection_entry.package_catalog` and the signed join manifest
`package_catalog` to select the target OS/architecture package candidate.
Give the target side only the selected link, visible runner/launcher,
visible script, or signed package. Prefer the materialized Connection Entry
runner when `runner_plan` is available; dry-run it before execute when network
reliability is uncertain. If release package assets are not published yet, use
the visible fallback script. Keep low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off repair
machines by following `connection_entry_plan.target_selection_policy`.
Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed. Use
existing open-source/free options through configured helper gateway overrides such
as `RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`,
`RDEV_VPN_GATEWAY_URL`, or `RDEV_SSH_GATEWAY_URL` when unambiguous; prefer frp,
Chisel, headscale-compatible mesh, or WireGuard before paid relays. If a
user-space helper is missing and an explicit URL/SHA-256 is approved, use
`rdev deps install` or reviewed `RDEV_*_INSTALL_ACTION_JSON` metadata for
user/workspace-scoped helper repair before helper startup; ask before
privileged, persistent, paid, firewall, DNS, cloud, credential, route, or
security-policy changes. For company or third-party machines, ask for authorization first, then default to visible attended-temporary mode and let Connection Entry probes detect Windows/macOS/Linux. Dry-run before execute. Do not hardcode private paths,
secrets, or server addresses; example URLs are placeholders only.
```

Vollständiger Copy-Paste-Prompt: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

Skillkit-Bundle exportieren und prüfen:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Einen prüfbaren Installationsplan für Agent Frameworks erstellen:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Die direkte Installation ist standardmäßig ein dry-run. Erst prüfen, dann `--execute` hinzufügen:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

Die zweite install-Zeile ist der Ein-Befehl-Installationspfad, sobald das Bundle verifiziert ist.

## Lokale Demo

```bash
go test ./...
rdev demo local
rdev mcp tools
```

Das englische [README](../../README.md) ist die verbindliche technische Referenz; bei Abweichungen gilt Englisch.
