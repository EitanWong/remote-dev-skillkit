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

First clone or update the repository in a safe user/workspace location. Then
read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow
that full prompt as the source of truth for installation. If cloning is blocked,
read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Probe my OS, shell, Git, Go, `rdev`, current agent framework, skill directory,
MCP config, network reachability, proxy/DNS state, NAT/firewall/CGNAT hints,
SSH config, installed tunnel/mesh tools, and available connection modes before
acting. If a required value is unclear, ask me one short question instead of
guessing. For a personal computer agent install, use local MCP stdio with
`rdev mcp serve`; do not require a hosted gateway URL. When remote hosts are
needed, auto-select the safest working mode: local dev gateway, LAN-reachable
gateway, hosted gateway, SSH tunnel, or relay/mesh/VPN. If tunneling is needed,
prefer open-source/free options first, such as frp, Chisel, headscale, or
WireGuard, after probing what is already installed. Dry-run before execute. Do
not hardcode private paths, secrets, or server addresses; treat
`https://api.example.com/v1` only as optional hosted-gateway placeholder
metadata.
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
