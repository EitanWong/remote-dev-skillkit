# Remote Dev Skillkit

Remote Dev Skillkit é uma camada de segurança para agentes de IA que precisam trabalhar em máquinas reais com Mac, Windows ou Linux. Não é uma ferramenta oculta de controle remoto: Codex, Claude Code, Hermes, OpenClaw/OpenCode e agentes MCP genéricos podem executar trabalho real com jobs assinados, política local no host, aprovações, pacotes de evidência e auditoria.

## Destaques

- Um Agent Skillkit portátil para instalar em Agent Frameworks populares.
- Jobs são assinados, verificados e só então executados; nada de entregar uma shell crua ao agente.
- As skills detectam OS, shell, service manager, gateway, workspace, adapter e permissões; se algo não estiver claro, elas perguntam.
- Adapters para Codex, Claude Code, ACP/acpx, shell, PowerShell e adapters personalizados.
- Licença open source Apache-2.0.

## Instalação rápida

Se você já está no Codex, Claude Code, Hermes, OpenClaw/OpenCode ou outro agente MCP, envie este link ao agente e peça que leia o prompt completo antes de agir:

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

Esse prompt é a fonte de verdade para clonar ou atualizar o repositório, detectar o runtime do agente, instalar o Skillkit, configurar MCP e fazer uma única pergunta curta quando um valor obrigatório não puder ser descoberto com segurança.

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

Se `rdev` não estiver no `PATH`, use o comando MCP absoluto mostrado por `rdev bootstrap agent-plan` ou pelo campo `mcp_command` do relatório de instalação.

Exporte e verifique o bundle Skillkit:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
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
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

O [README](../../README.md) em inglês é a referência técnica oficial; se houver conflito, siga o README em inglês.
