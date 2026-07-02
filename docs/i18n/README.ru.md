# Remote Dev Skillkit

Remote Dev Skillkit — это слой безопасности для AI Agent, которым нужно работать на реальных машинах Mac, Windows и Linux. Это не скрытый инструмент удаленного управления: Codex, Claude Code, Hermes, OpenClaw/OpenCode и обычные MCP Agent могут выполнять реальную разработку через подписанные задания, локальную политику хоста, подтверждения, evidence bundles и audit chains.

## Главное

- Портативный Agent Skillkit для популярных Agent Frameworks.
- Jobs сначала подписываются и проверяются, а только потом выполняются; агент не получает сырой shell без ограничений.
- Skills сначала определяют OS, shell, service manager, gateway, workspace, adapter и permissions; если что-то неясно, они спрашивают.
- Adapters для Codex, Claude Code, ACP/acpx, shell, PowerShell и custom adapters.
- Открытая лицензия Apache-2.0.

## Быстрая установка

Если вы уже работаете в Codex, Claude Code, Hermes, OpenClaw/OpenCode или другом MCP Agent, скопируйте это в своего агента:

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

Полный prompt для копирования: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
```

Экспортируйте и проверьте Skillkit bundle:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Создайте проверяемый план установки для Agent Frameworks:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Прямая установка по умолчанию выполняет dry-run. Проверьте отчет, затем добавьте `--execute`:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

Вторая строка install — это one-command install после проверки bundle.

## Локальная демо

```bash
go test ./...
rdev demo local
rdev mcp tools
```

Английский [README](../../README.md) является основным техническим источником; если перевод отличается, следуйте английской версии.
