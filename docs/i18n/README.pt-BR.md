# Remote Dev Skillkit

Remote Dev Skillkit é uma camada de segurança para agentes de IA que precisam trabalhar em máquinas reais com Mac, Windows ou Linux. Não é uma ferramenta oculta de controle remoto: Codex, Claude Code, Hermes, OpenClaw/OpenCode e agentes MCP genéricos podem executar trabalho real com jobs assinados, política local no host, aprovações, pacotes de evidência e auditoria.

## Destaques

- Um Agent Skillkit portátil para instalar em Agent Frameworks populares.
- Jobs são assinados, verificados e só então executados; nada de entregar uma shell crua ao agente.
- As skills detectam OS, shell, service manager, gateway, workspace, adapter e permissões; se algo não estiver claro, elas perguntam.
- Adapters para Codex, Claude Code, ACP/acpx, shell, PowerShell e adapters personalizados.
- Licença open source Apache-2.0.

## Instalação rápida

Se você já está no Codex, Claude Code, Hermes, OpenClaw/OpenCode ou outro agente MCP, copie isto para o agente:

```text
Bootstrap Remote Dev Skillkit for this agent runtime.

Repository: https://github.com/EitanWong/remote-dev-skillkit

First clone or update the repository in a safe user/workspace location. Then
read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow
that full prompt as the source of truth for installation. If cloning is blocked,
read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Probe my OS, shell, Git, Go, `rdev`, current agent framework, skill directory,
MCP config, network reachability, proxy/DNS state, NAT/firewall/CGNAT hints,
SSH config, installed tunnel/mesh tools, and available connection modes before
acting. If a required value is unclear, ask me one short question instead of
guessing. For a personal computer agent install, use local MCP stdio with
`rdev mcp serve`; do not require a hosted gateway URL. When remote hosts are
needed, auto-select the safest working mode: local dev gateway, LAN-reachable
gateway, hosted gateway, SSH tunnel, or relay/mesh/VPN. For remote hosts, create
an invite and present `connection_entry.entry_url` or a signed connection entry
package instead of asking humans to assemble ticket, root, gateway, or transport
flags. If tunneling is needed,
prefer open-source/free options first, such as frp, Chisel, headscale, or
WireGuard, after probing what is already installed. Dry-run before execute. Do
not hardcode private paths, secrets, or server addresses; treat
`https://api.example.com/v1` only as optional hosted-gateway placeholder
metadata.
```

Prompt completo para copiar: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
```

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
rdev demo local
rdev mcp tools
```

O [README](../../README.md) em inglês é a referência técnica oficial; se houver conflito, siga o README em inglês.
