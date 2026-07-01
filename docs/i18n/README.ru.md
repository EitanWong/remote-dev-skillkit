# Remote Dev Skillkit

Remote Dev Skillkit — это ядро безопасности для удаленной разработки с помощью
агентов. Это не скрытый инструмент удаленного управления: проект позволяет
Codex, Claude Code, Hermes, OpenClaw/OpenCode и обычным MCP-агентам делегировать
работу реальным машинам через подписанные задания, локальные политики хоста,
подтверждения, доказательства выполнения и аудит.

## Что входит

- CLI `rdev` и бинарные файлы host, gateway, MCP и verifier.
- Agent Skillkit bundles, которые можно экспортировать, проверять и устанавливать.
- Подписанные job envelopes, workspace locks, approval gates, evidence bundles и audit chains.
- Адаптеры для Codex, Claude Code, ACP/acpx, shell и PowerShell.
- Открытая лицензия Apache-2.0.

## Локальная проверка

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## Установка Skillkit

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Английский [README](../../README.md) является основным техническим источником.
