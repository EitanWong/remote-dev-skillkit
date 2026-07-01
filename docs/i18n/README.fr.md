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
Bootstrap Remote Dev Skillkit for this agent runtime.

Repository: https://github.com/EitanWong/remote-dev-skillkit

First clone or update the repository in a safe user/workspace location. Then
read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow
that full prompt as the source of truth for installation. If cloning is blocked,
read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Probe my OS, shell, Git, Go, `rdev`, current agent framework, skill directory,
MCP config, and available connection modes before acting. If a required value is
unclear, ask me one short question instead of guessing. For a personal computer
agent install, use local MCP stdio with `rdev mcp serve`; do not require a
hosted gateway URL. When remote hosts are needed, auto-select the safest
available mode: local dev gateway, LAN-reachable gateway, hosted gateway,
relay/mesh/VPN, or SSH tunnel. Dry-run before execute. Do not hardcode private
paths, secrets, or server addresses; treat `https://api.example.com/v1` only as
optional hosted-gateway placeholder metadata.
```

Prompt complet à copier : [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
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
rdev demo local
rdev mcp tools
```

Le [README](../../README.md) anglais est la référence technique officielle ; en cas de divergence, suivez-le.
