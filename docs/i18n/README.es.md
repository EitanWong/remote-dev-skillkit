# Remote Dev Skillkit

Remote Dev Skillkit es una capa de seguridad para agentes de IA que necesitan trabajar en máquinas reales con Mac, Windows o Linux. No es una herramienta oculta de control remoto: permite que Codex, Claude Code, Hermes, OpenClaw/OpenCode y agentes MCP genéricos ejecuten trabajo real con tareas firmadas, política local en el host, aprobaciones, paquetes de evidencia y auditoría.

## Puntos clave

- Un Agent Skillkit portable para instalar en frameworks de agentes populares.
- Las tareas se firman, se verifican y solo después se ejecutan; nada de entregar una shell cruda al agente.
- Las skills detectan OS, shell, service manager, gateway, workspace, adapter y permisos; si algo no está claro, preguntan.
- Adaptadores para Codex, Claude Code, ACP/acpx, shell, PowerShell y adapters personalizados.
- Licencia open source Apache-2.0.

## Instalación rápida

Si ya estás dentro de Codex, Claude Code, Hermes, OpenClaw/OpenCode u otro agente MCP, copia esto en tu agente:

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

Prompt completo para copiar: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
```

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
rdev demo local
rdev mcp tools
```

El [README](../../README.md) en inglés es la referencia técnica autoritativa; si una traducción difiere, sigue el README en inglés.
