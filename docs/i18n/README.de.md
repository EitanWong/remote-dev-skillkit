# Remote Dev Skillkit

Remote Dev Skillkit ist ein quelloffenes, agent-natives Remote-Development-Skillkit. Es hilft Codex, Claude Code, Hermes, OpenClaw/OpenCode und MCP-Agenten auf echten Mac-, Windows- und Linux-Hosts zu arbeiten, ohne ihnen eine unbegrenzte Shell zu geben.

## Was es macht

| Der Agent bekommt | Der Mensch behalt |
|---|---|
| Skills, MCP-Tools und Datei/Desktop/Job-Adapter | Sichtbarkeit, Freigabe, Widerruf und Audit |
| Signierte Jobs mit klaren Capabilities | Lokale Host-Policy und Sicherheitsgrenzen |
| Artifacts und evidence bundles | Kontrolle daruber, was lauft und wann es stoppt |

## Mit einem Agenten installieren

Kopiere den Text unten und sende ihn an den Agenten:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

Vollstandiger Vertrag: [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

<details>
<summary>Manuelle Befehle</summary>

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

</details>

## Nutzung

1. Eine Maschine verbinden:

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. Tools anzeigen und lokale Demo starten:

```bash
rdev mcp tools
rdev demo local
```

3. Basisnachweise prufen:

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## Sicherheit

Remote Dev Skillkit ist fur explizite, sichtbare und zustimmungsbasierte Remote-Entwicklung gedacht. Temporaere Sitzungen fur Drittgeraete mussen sichtbar, zeitlich begrenzt, widerrufbar, auditierbar und standardmassig auf Benutzerebene laufen. Versteckte Persistenz, UAC/sudo-Bypass, deaktivierte Sicherheitskontrollen und Shell-Zugriff ohne Policy sind nicht akzeptiert. Lizenz Apache-2.0.

## Dokumentation

- Massgebliche technische README: [../../README.md](../../README.md)
- Dokumentationsindex: [../README.md](../README.md)
