# Remote Dev Skillkit

Remote Dev Skillkit est un noyau de sécurité pour le développement distant
piloté par des agents. Ce n'est pas un outil de contrôle à distance caché : il
permet à Codex, Claude Code, Hermes, OpenClaw/OpenCode et aux agents MCP
génériques de déléguer du travail à de vraies machines avec des tâches signées,
des règles locales côté hôte, des approbations, des preuves et un journal
d'audit.

## Ce que le projet fournit

- La CLI `rdev` et les binaires host, gateway, MCP et vérificateur.
- Des bundles Agent Skillkit exportables, vérifiables et installables.
- Des enveloppes de tâches signées, des verrous de workspace, des approbations,
  des bundles de preuves et des chaînes d'audit.
- Des adaptateurs Codex, Claude Code, ACP/acpx, shell et PowerShell.
- Une licence open source Apache-2.0.

## Vérification locale

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## Installer le Skillkit

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Le [README anglais](../../README.md) reste la référence technique faisant foi.
