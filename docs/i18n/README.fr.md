# Remote Dev Skillkit

Remote Dev Skillkit est une couche de sécurité pour les agents IA qui doivent travailler sur de vraies machines Mac, Windows ou Linux. Ce n'est pas un outil de contrôle distant caché : Codex, Claude Code, Hermes, OpenClaw/OpenCode et les agents MCP génériques peuvent exécuter du travail réel via des tâches signées, des règles locales côté hôte, des approbations, des preuves et un audit.

## Points forts

- Un Agent Skillkit portable, installable dans les frameworks d'agents courants.
- Les tâches sont signées, vérifiées, puis exécutées ; pas de shell brut remis à l'agent.
- Les skills détectent OS, shell, service manager, gateway, workspace, adapter et permissions ; si la configuration n'est pas claire, elles demandent.
- Adaptateurs pour Codex, Claude Code, ACP/acpx, shell, PowerShell et adapters personnalisés.
- Licence open source Apache-2.0.

## Installation rapide

Si vous êtes déjà dans Codex, Claude Code, Hermes, OpenClaw/OpenCode ou un autre agent MCP, copiez ceci dans votre agent :

```text
Install Remote Dev Skillkit for this agent runtime.

Repository: https://github.com/EitanWong/remote-dev-skillkit
Full install prompt: https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Clone or update the repository, read the full install prompt, and follow it as the source of truth. If cloning is blocked, open the prompt link directly. Ask one short question only when a required value is unclear.
```

Prompt complet à copier : [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

Exporter et vérifier le bundle Skillkit :

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Créer un plan d'installation vérifiable pour les frameworks d'agents :

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

L'installation directe est en dry-run par défaut. Vérifiez le rapport, puis ajoutez `--execute` :

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

La deuxième commande install est le chemin d'installation en une commande une fois le bundle vérifié.

## Démo locale

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

Le [README](../../README.md) anglais est la référence technique officielle ; en cas de divergence, suivez-le.
