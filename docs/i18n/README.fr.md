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

Clone or update the repository in a safe user/workspace location. Then read
`docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow it as
the source of truth. If cloning is blocked, read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Install the Skillkit and configure MCP for this agent. Probe OS, shell, Git, Go,
`rdev`, current agent framework, skill directory, MCP config, and network state
before acting. Ask one short question when a required value is unclear. For this
personal computer, prefer local MCP stdio with `rdev mcp serve`; do not require a
hosted gateway URL.

When I ask you to work on another machine, create a Connection Entry. Do not ask
humans to assemble ticket, root, gateway, or transport flags. Use
`rdev.invites.create`, then `rdev.connection_entry.plan` or
`rdev connection-entry plan`, and give the target side only a link, visible
script, or signed package. Choose `managed` for my own long-running machines and
`attended-temporary` for third-party or one-off repair machines. Auto-select
LAN, hosted, SSH, relay, mesh, or VPN paths as needed; prefer existing,
open-source/free options such as frp, Chisel, headscale, or WireGuard; ask before
privileged, persistent, paid, firewall, DNS, cloud, or security-policy changes.
Dry-run before execute. Do not hardcode private paths, secrets, or server
addresses; example URLs are placeholders only.
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
