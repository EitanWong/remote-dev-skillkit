# Remote Dev Skillkit

Remote Dev Skillkit ist ein quelloffenes, agent-natives Remote-Development-Skillkit. Es hilft Codex, Claude Code, Hermes, OpenClaw/OpenCode und MCP-Agenten auf echten Mac-, Windows- und Linux-Hosts zu arbeiten, ohne ihnen eine unbegrenzte Shell zu geben.

Das Projekt kombiniert Agent Skills, MCP-Tools, signierte Jobs, lokale Host-Policy, Freigaben, Audit-Logs und Evidence-Bundles. Die Lizenz ist Apache-2.0.

## Installationsprompt fuer den Agenten

Kopiere diesen Text in deinen Agenten:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

Der vollstaendige Vertrag steht im [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

## Manueller Schnellstart

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

## Lokal testen

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## Sicherheit

Remote Dev Skillkit ist fuer explizite, sichtbare und zustimmungsbasierte Remote-Entwicklung gedacht. Temporaere Sitzungen fuer Drittgeraete muessen sichtbar, zeitlich begrenzt, widerrufbar, auditierbar und standardmaessig auf Benutzerebene laufen. Versteckte Persistenz, UAC/sudo-Bypass, deaktivierte Sicherheitskontrollen und Shell-Zugriff ohne Policy sind nicht akzeptiert.

Das englische [README](../../README.md) ist die massgebliche technische Quelle.
