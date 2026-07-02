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

When I ask you to work on another machine, always create a Connection Entry.
Treat it as the universal target-side handoff for every scenario: my own durable
computer, a third-party temporary repair machine, LAN, hosted, relay, mesh, SSH,
or VPN-assisted connectivity. Do not ask humans to assemble ticket, root,
gateway, transport, release, or checksum flags. Use `rdev.invites.create`, then
materialize it with `rdev.connection_entry.plan` or `rdev connection-entry plan`.
Read `connection_entry.package_catalog` and the signed join manifest
`package_catalog` to select the target OS/architecture package candidate.
Give the target side only the selected link, visible script, or signed package;
if package assets are not published yet, use the visible fallback script. Keep
low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off repair
machines by following `connection_entry_plan.target_selection_policy`.
Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed;
prefer existing, open-source/free options such as frp, Chisel, headscale, or
WireGuard; ask before privileged, persistent, paid, firewall, DNS, cloud, or
security-policy changes. Dry-run before execute. Do not hardcode private paths,
secrets, or server addresses; example URLs are placeholders only.
```

Vollständiger Copy-Paste-Prompt: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
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
