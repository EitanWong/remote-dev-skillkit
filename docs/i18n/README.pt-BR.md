# Remote Dev Skillkit

Remote Dev Skillkit é uma camada de segurança para agentes de IA que precisam trabalhar em máquinas reais com Mac, Windows ou Linux. Não é uma ferramenta oculta de controle remoto: Codex, Claude Code, Hermes, OpenClaw/OpenCode e agentes MCP genéricos podem executar trabalho real com jobs assinados, política local no host, aprovações, pacotes de evidência e auditoria.

## Destaques

- Um Agent Skillkit portátil para instalar em Agent Frameworks populares.
- Jobs são assinados, verificados e só então executados; nada de entregar uma shell crua ao agente.
- As skills detectam OS, shell, service manager, gateway, workspace, adapter e permissões; se algo não estiver claro, elas perguntam.
- Adapters para Codex, Claude Code, ACP/acpx, shell, PowerShell e adapters personalizados.
- Licença open source Apache-2.0.

## Instalação rápida

```bash
go install ./cmd/rdev
rdev doctor
```

Exporte e verifique o bundle Skillkit:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Gere um plano de instalação revisável para Agent Frameworks:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

A instalação direta é dry-run por padrão. Revise o relatório e depois adicione `--execute`:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

A segunda linha de install é o caminho de instalação em um comando quando o bundle já foi verificado.

## Demo local

```bash
go test ./...
rdev demo local
rdev mcp tools
```

O [README](../../README.md) em inglês é a referência técnica oficial; se houver conflito, siga o README em inglês.
