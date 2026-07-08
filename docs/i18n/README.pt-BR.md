# Remote Dev Skillkit

Remote Dev Skillkit e um skillkit remoto, open-source e nativo para agentes de IA. Ele permite que Codex, Claude Code, Hermes, OpenClaw/OpenCode e agentes MCP trabalhem em hosts Mac, Windows e Linux reais sem receber um shell ilimitado.

O projeto junta Agent Skills, ferramentas MCP, jobs assinados, politica local do host, aprovacoes, auditoria e pacotes de evidencia. A licenca e Apache-2.0.

## Prompt de instalacao para o agente

Copie e cole isto no seu agente:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

O contrato completo fica em [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

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

## Teste local

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## Seguranca

Remote Dev Skillkit foi criado para suporte remoto explicito, visivel e com consentimento. Sessoes temporarias de terceiros devem ser visiveis, limitadas no tempo, revogaveis, auditaveis e com permissao de usuario por padrao. O projeto nao aceita persistencia oculta, bypass de UAC/sudo, desativacao de controles locais ou shell sem politica.

O [README](../../README.md) em ingles e a fonte tecnica autoritativa.
