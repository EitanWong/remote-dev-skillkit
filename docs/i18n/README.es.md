# Remote Dev Skillkit

Remote Dev Skillkit es un skillkit remoto, abierto y nativo para agentes de IA. Permite que Codex, Claude Code, Hermes, OpenClaw/OpenCode y agentes MCP trabajen en hosts Mac, Windows y Linux reales sin recibir un shell ilimitado.

El proyecto combina Agent Skills, herramientas MCP, trabajos firmados, politica local del host, aprobaciones, auditoria y paquetes de evidencia. La licencia es Apache-2.0.

## Prompt de instalacion para tu agente

Copia y pega esto en tu agente:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

El contrato completo esta en [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

## Inicio rapido manual

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

## Prueba local

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## Seguridad

Remote Dev Skillkit esta disenado para soporte remoto explicito, visible y con consentimiento. Las sesiones temporales de terceros deben ser visibles, limitadas en el tiempo, revocables, auditables y de nivel usuario por defecto. No se aceptan funciones de persistencia oculta, bypass de UAC/sudo, desactivacion de controles de seguridad, ni shell sin politica.

El [README](../../README.md) en ingles es la fuente tecnica autoritativa.
