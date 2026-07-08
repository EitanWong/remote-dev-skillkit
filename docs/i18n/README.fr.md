# Remote Dev Skillkit

Remote Dev Skillkit est un skillkit open-source, natif pour les agents IA. Il permet a Codex, Claude Code, Hermes, OpenClaw/OpenCode et aux agents MCP de travailler sur de vrais hôtes Mac, Windows et Linux sans recevoir un shell illimite.

Le projet regroupe Agent Skills, outils MCP, jobs signes, politique locale de l'hôte, validations humaines, audit et preuves exportables. La licence est Apache-2.0.

## Prompt d'installation pour l'agent

Copiez ce texte dans votre agent :

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

Le contrat complet se trouve dans [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

## Demarrage manuel

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

## Essai local

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## Securite

Remote Dev Skillkit vise le support distant explicite, visible et consenti. Les sessions temporaires tierces doivent etre visibles, limitees dans le temps, revocables, auditables et en mode utilisateur par defaut. Le projet refuse la persistance cachee, les bypass UAC/sudo, la desactivation des controles locaux et le shell sans politique.

Le [README](../../README.md) anglais reste la reference technique.
