# Remote Dev Skillkit

Remote Dev Skillkit es una capa de seguridad para agentes de IA que necesitan trabajar en máquinas reales con Mac, Windows o Linux. No es una herramienta oculta de control remoto: permite que Codex, Claude Code, Hermes, OpenClaw/OpenCode y agentes MCP genéricos ejecuten trabajo real con tareas firmadas, política local en el host, aprobaciones, paquetes de evidencia y auditoría.

## Puntos clave

- Un Agent Skillkit portable para instalar en frameworks de agentes populares.
- Las tareas se firman, se verifican y solo después se ejecutan; nada de entregar una shell cruda al agente.
- Las skills detectan OS, shell, service manager, gateway, workspace, adapter y permisos; si algo no está claro, preguntan.
- Adaptadores para Codex, Claude Code, ACP/acpx, shell, PowerShell y adapters personalizados.
- Licencia open source Apache-2.0.

## Instalación rápida

```bash
go install ./cmd/rdev
rdev doctor
```

Exporta y verifica el bundle Skillkit:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
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
