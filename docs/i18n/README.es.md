# Remote Dev Skillkit

Remote Dev Skillkit es un núcleo de seguridad para desarrollo remoto impulsado
por agentes. No es una herramienta oculta de control remoto: permite que Codex,
Claude Code, Hermes, OpenClaw/OpenCode y agentes MCP genéricos deleguen trabajo
en máquinas reales con tareas firmadas, política local en el host, aprobaciones,
evidencia y auditoría.

## Qué ofrece

- CLI `rdev` y binarios para host, gateway, MCP y verificación.
- Paquetes Agent Skillkit exportables, verificables e instalables.
- Envelopes de trabajo firmados, bloqueos de workspace, aprobaciones, evidencia
  y cadenas de auditoría.
- Adaptadores para Codex, Claude Code, ACP/acpx, shell y PowerShell.
- Licencia open source Apache-2.0.

## Verificación local

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## Instalar Skillkit

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

El [README en inglés](../../README.md) es la referencia técnica autoritativa.
