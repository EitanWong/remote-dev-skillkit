# Remote Dev Skillkit

Remote Dev Skillkit é um núcleo de segurança para desenvolvimento remoto guiado
por agentes. Não é uma ferramenta oculta de controle remoto: ele permite que
Codex, Claude Code, Hermes, OpenClaw/OpenCode e agentes MCP genéricos deleguem
trabalho a máquinas reais com jobs assinados, política local no host,
aprovações, evidências e auditoria.

## O que ele oferece

- CLI `rdev` e binários de host, gateway, MCP e verificação.
- Bundles Agent Skillkit exportáveis, verificáveis e instaláveis.
- Job envelopes assinados, locks de workspace, approval gates, evidence bundles
  e audit chains.
- Adaptadores para Codex, Claude Code, ACP/acpx, shell e PowerShell.
- Licença open source Apache-2.0.

## Verificação local

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## Instalar o Skillkit

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

O [README em inglês](../../README.md) é a referência técnica oficial.
