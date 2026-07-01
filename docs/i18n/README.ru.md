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

First clone or update the repository in a safe user/workspace location. Then
read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow
that full prompt as the source of truth for installation. If cloning is blocked,
read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Probe my OS, shell, Git, Go, `rdev`, current agent framework, skill directory,
MCP config, and available connection modes before acting. If a required value is
unclear, ask me one short question instead of guessing. For a personal computer
agent install, use local MCP stdio with `rdev mcp serve`; do not require a
hosted gateway URL. When remote hosts are needed, auto-select the safest
available mode: local dev gateway, LAN-reachable gateway, hosted gateway,
relay/mesh/VPN, or SSH tunnel. Dry-run before execute. Do not hardcode private
paths, secrets, or server addresses; treat `https://api.example.com/v1` only as
optional hosted-gateway placeholder metadata.
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
