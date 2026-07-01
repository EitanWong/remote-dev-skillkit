# Remote Dev Skillkit

Remote Dev Skillkit — это слой безопасности для AI Agent, которым нужно работать на реальных машинах Mac, Windows и Linux. Это не скрытый инструмент удаленного управления: Codex, Claude Code, Hermes, OpenClaw/OpenCode и обычные MCP Agent могут выполнять реальную разработку через подписанные задания, локальную политику хоста, подтверждения, evidence bundles и audit chains.

## Главное

- Портативный Agent Skillkit для популярных Agent Frameworks.
- Jobs сначала подписываются и проверяются, а только потом выполняются; агент не получает сырой shell без ограничений.
- Skills сначала определяют OS, shell, service manager, gateway, workspace, adapter и permissions; если что-то неясно, они спрашивают.
- Adapters для Codex, Claude Code, ACP/acpx, shell, PowerShell и custom adapters.
- Открытая лицензия Apache-2.0.

## Быстрая установка

```bash
go install ./cmd/rdev
rdev doctor
```

Экспортируйте и проверьте Skillkit bundle:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
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
