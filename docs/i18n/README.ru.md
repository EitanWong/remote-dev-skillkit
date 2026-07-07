# Remote Dev Skillkit

Remote Dev Skillkit — это слой безопасности для AI Agent, которым нужно работать на реальных машинах Mac, Windows и Linux. Это не скрытый инструмент удаленного управления: Codex, Claude Code, Hermes, OpenClaw/OpenCode и обычные MCP Agent могут выполнять реальную разработку через подписанные задания, локальную политику хоста, подтверждения, evidence bundles и audit chains.

## Главное

- Портативный Agent Skillkit для популярных Agent Frameworks.
- Jobs сначала подписываются и проверяются, а только потом выполняются; агент не получает сырой shell без ограничений.
- Skills сначала определяют OS, shell, service manager, gateway, workspace, adapter и permissions; если что-то неясно, они спрашивают.
- Adapters для Codex, Claude Code, ACP/acpx, shell, PowerShell и custom adapters.
- Открытая лицензия Apache-2.0.

## Быстрая установка

Если вы уже работаете в Codex, Claude Code, Hermes, OpenClaw/OpenCode или другом MCP Agent, отправьте агенту эту ссылку и попросите прочитать весь prompt перед выполнением:

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

Этот prompt является источником истины для clone или update репозитория, определения текущего Agent runtime, установки Skillkit, настройки MCP и одного короткого вопроса, если обязательное значение нельзя безопасно определить.

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

Если `rdev` нет в `PATH`, используйте абсолютную MCP-команду из `rdev bootstrap agent-plan` или поля `mcp_command` в отчете установки.

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
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

Английский [README](../../README.md) является основным техническим источником; если перевод отличается, следуйте английской версии.
