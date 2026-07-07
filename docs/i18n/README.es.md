# Remote Dev Skillkit

Remote Dev Skillkit es una capa de seguridad para agentes de IA que necesitan trabajar en máquinas reales con Mac, Windows o Linux. No es una herramienta oculta de control remoto: permite que Codex, Claude Code, Hermes, OpenClaw/OpenCode y agentes MCP genéricos ejecuten trabajo real con tareas firmadas, política local en el host, aprobaciones, paquetes de evidencia y auditoría.

Su modelo de conexión es de control remoto para agentes: la máquina destino abre un conector visible, el agente usa el Support Device Entry estándar (`support_device_id` + contraseña de sesión) y no desconecta hasta que el operador lo pide explícitamente.

## Puntos clave

- Un Agent Skillkit portable para instalar en frameworks de agentes populares.
- Las tareas se firman, se verifican y solo después se ejecutan; nada de entregar una shell cruda al agente.
- Las skills detectan OS, shell, service manager, gateway, workspace, adapter y permisos; si algo no está claro, preguntan.
- Adaptadores para Codex, Claude Code, ACP/acpx, shell, PowerShell y adapters personalizados.
- Licencia open source Apache-2.0.

## Instalación rápida

Si ya estás dentro de Codex, Claude Code, Hermes, OpenClaw/OpenCode u otro agente MCP, envíale este enlace y pídele que lea el prompt completo antes de actuar:

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

Ese prompt es la fuente de verdad para clonar o actualizar el repositorio, detectar el runtime del agente, instalar el Skillkit, configurar MCP y hacer una sola pregunta corta cuando un valor requerido no pueda descubrirse de forma segura.

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

Si `rdev` no está en `PATH`, usa el comando MCP absoluto mostrado por `rdev bootstrap agent-plan` o por el campo `mcp_command` del informe de instalación.

Exporta y verifica el bundle Skillkit:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Genera un plan de instalación revisable para frameworks de agentes:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

La instalación directa hace dry-run por defecto. Revisa el resultado y luego añade `--execute`:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

La segunda línea de install es el camino de instalación en un comando cuando el bundle ya fue verificado.

## Demo local

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

El [README](../../README.md) en inglés es la referencia técnica autoritativa; si una traducción difiere, sigue el README en inglés.
