# Remote Dev Skillkit

Remote Dev Skillkit ist ein Sicherheitskern für agentengesteuerte Remote-
Entwicklung. Es ist kein verstecktes Fernsteuerungswerkzeug: Codex, Claude Code,
Hermes, OpenClaw/OpenCode und generische MCP-Agenten können damit Arbeit an
echte Maschinen delegieren, mit signierten Jobs, lokaler Host-Policy,
Freigaben, Nachweisen und Audit-Spuren.

## Was enthalten ist

- `rdev` CLI sowie Host-, Gateway-, MCP- und Verifier-Binaries.
- Exportierbare, überprüfbare und installierbare Agent-Skillkit-Bundles.
- Signierte Job-Envelopes, Workspace-Locks, Approval-Gates, Evidence-Bundles
  und Audit Chains.
- Adapter für Codex, Claude Code, ACP/acpx, Shell und PowerShell.
- Apache-2.0 Open-Source-Lizenz.

## Lokal prüfen

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## Skillkit installieren

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Das englische [README](../../README.md) ist die verbindliche technische Referenz.
