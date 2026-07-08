# Remote Dev Skillkit

Remote Dev Skillkit e um skillkit remoto, open-source e nativo para agentes de IA. Ele permite que Codex, Claude Code, Hermes, OpenClaw/OpenCode e agentes MCP trabalhem em hosts Mac, Windows e Linux reais sem receber um shell ilimitado.

## O que faz

| O agente recebe | A pessoa mantem |
|---|---|
| Skills, ferramentas MCP e adaptadores de arquivos/desktop/jobs | Visibilidade, aprovacao, revogacao e auditoria |
| Jobs assinados com capacidades claras | Politica local do host e limites de seguranca |
| Artifacts e evidence bundles | Controle sobre o que roda e quando para |

## Instalar com um agente

Copie o texto abaixo e envie ao agente:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

Contrato completo: [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

<details>
<summary>Comandos manuais</summary>

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

1. Conecte uma maquina:

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. Veja ferramentas e rode a demo local:

```bash
rdev mcp tools
rdev demo local
```

3. Revise evidencia basica:

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## Seguranca

Remote Dev Skillkit foi criado para suporte remoto explicito, visivel e com consentimento. Sessoes temporarias de terceiros devem ser visiveis, limitadas no tempo, revogaveis, auditaveis e com permissao de usuario por padrao. O projeto nao aceita persistencia oculta, bypass de UAC/sudo, desativacao de controles locais ou shell sem politica. Licenca Apache-2.0.

## Documentos

- README tecnico autoritativo: [../../README.md](../../README.md)
- Indice de docs: [../README.md](../README.md)
