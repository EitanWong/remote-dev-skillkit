# Remote Dev Skillkit

Remote Dev Skillkit es un skillkit remoto, open-source y nativo para agentes de IA. Permite que Codex, Claude Code, Hermes, OpenClaw/OpenCode y agentes MCP trabajen en hosts Mac, Windows y Linux reales sin recibir un shell ilimitado.

## Que hace

| El agente obtiene | La persona conserva |
|---|---|
| Skills, herramientas MCP y adaptadores de archivos/escritorio/tasks | Visibilidad, aprobacion, revocacion y auditoria |
| Tasks firmados con capacidades claras | Politica local del host y limites de seguridad |
| Artifacts y session evidence | Control sobre que se ejecuta y cuando se detiene |

## Instalar con un agente

Copia el texto siguiente y envialo al agente:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

Contrato completo: [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

<details>
<summary>Comandos manuales</summary>

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

## Uso

1. Conecta una maquina:

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. Lista herramientas y prueba la demo local:

```bash
rdev mcp tools
rdev demo local
```

3. Revisa evidencia basica:

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## Seguridad

Remote Dev Skillkit esta disenado para soporte remoto explicito, visible y con consentimiento. Las sesiones temporales de terceros deben ser visibles, limitadas en el tiempo, revocables, auditables y de nivel usuario por defecto. No se acepta persistencia oculta, bypass de UAC/sudo, desactivar controles locales ni shell sin politica. Licencia Apache-2.0.

## Documentacion

- README tecnico autoritativo: [../../README.md](../../README.md)
- Indice de docs: [../README.md](../README.md)
