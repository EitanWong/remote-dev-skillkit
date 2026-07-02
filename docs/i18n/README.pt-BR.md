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

Clone or update the repository in a safe user/workspace location. Then read
`docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow it as
the source of truth. If cloning is blocked, read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Install the Skillkit and configure MCP for this agent. Probe OS, shell, Git, Go,
`rdev`, current agent framework, skill directory, MCP config, and network state
before acting. Ask one short question when a required value is unclear. For this
personal computer, prefer local MCP stdio with `rdev mcp serve`; do not require a
hosted gateway URL.

When I ask you to work on another machine, always create a Connection Entry.
Treat it as the universal target-side handoff for every scenario: my own durable
computer, a third-party temporary repair machine, LAN, hosted, relay, mesh, SSH,
or VPN-assisted connectivity. Do not ask humans to assemble ticket, root,
gateway, transport, release, or checksum flags. Use `rdev.invites.create`, then
materialize it with `rdev.connection_entry.plan` or `rdev connection-entry plan`.
Give the target side only the selected link, visible script, or signed package;
keep low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off repair
machines. Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed;
prefer existing, open-source/free options such as frp, Chisel, headscale, or
WireGuard; ask before privileged, persistent, paid, firewall, DNS, cloud, or
security-policy changes. Dry-run before execute. Do not hardcode private paths,
secrets, or server addresses; example URLs are placeholders only.
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
