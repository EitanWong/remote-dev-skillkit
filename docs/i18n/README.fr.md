# Remote Dev Skillkit

Remote Dev Skillkit est un skillkit open-source, natif pour les agents IA. Il permet a Codex, Claude Code, Hermes, OpenClaw/OpenCode et aux agents MCP de travailler sur de vrais hotes Mac, Windows et Linux sans recevoir un shell illimite.

## Ce qu'il fait

| L'agent obtient | L'humain garde |
|---|---|
| Skills, outils MCP et adaptateurs fichiers/bureau/jobs | Visibilite, validation, revocation et audit |
| Jobs signes avec capacites explicites | Politique locale de l'hote et limites de securite |
| Artifacts et evidence bundles | Controle de ce qui s'execute et de l'arret |

## Installer avec un agent

Copiez le texte ci-dessous et envoyez-le a l'agent :

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

Contrat complet : [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

<details>
<summary>Commandes manuelles</summary>

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

## Utilisation

1. Connecter une machine :

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. Voir les outils et lancer la demo locale :

```bash
rdev mcp tools
rdev demo local
```

3. Verifier les preuves de base :

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## Securite

Remote Dev Skillkit vise le support distant explicite, visible et consenti. Les sessions temporaires tierces doivent etre visibles, limitees dans le temps, revocables, auditables et en mode utilisateur par defaut. Le projet refuse la persistance cachee, les bypass UAC/sudo, la desactivation des controles locaux et le shell sans politique. Licence Apache-2.0.

## Documentation

- README technique de reference : [../../README.md](../../README.md)
- Index des docs : [../README.md](../README.md)
