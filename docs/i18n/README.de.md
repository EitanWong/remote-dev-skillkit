# Remote Dev Skillkit

Remote Dev Skillkit ist eine Sicherheitsschicht für KI-Agenten, die auf echten Mac-, Windows- oder Linux-Maschinen arbeiten müssen. Es ist kein verstecktes Fernsteuerungswerkzeug: Codex, Claude Code, Hermes, OpenClaw/OpenCode und generische MCP-Agenten können echte Entwicklungsarbeit über signierte Jobs, lokale Host-Policy, Freigaben, Evidence Bundles und Audit Chains ausführen.

## Highlights

- Ein portables Agent Skillkit für gängige Agent Frameworks.
- Jobs werden signiert, geprüft und erst dann ausgeführt; keine rohe Shell als Freifahrtschein.
- Skills erkennen OS, shell, service manager, gateway, workspace, adapter und Berechtigungen; wenn etwas unklar ist, fragen sie.
- Adapter für Codex, Claude Code, ACP/acpx, shell, PowerShell und eigene adapters.
- Open-Source-Lizenz Apache-2.0.

## Schnellinstallation

```bash
go install ./cmd/rdev
rdev doctor
```

Skillkit-Bundle exportieren und prüfen:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
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
