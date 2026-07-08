# Remote Dev Skillkit

Remote Dev Skillkit - это open-source, agent-native skillkit для удаленной разработки. Он позволяет Codex, Claude Code, Hermes, OpenClaw/OpenCode и MCP-агентам работать с реальными Mac, Windows и Linux hosts без выдачи неограниченного shell.

Проект объединяет Agent Skills, MCP tools, подписанные задания, локальную политику хоста, approval gates, audit logs и evidence bundles. Лицензия - Apache-2.0.

## Prompt для установки через агента

Скопируйте и отправьте это своему агенту:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

Полный контракт находится в [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

## Ручной быстрый старт

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

## Локальная проверка

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## Безопасность

Remote Dev Skillkit создан для явной, видимой и согласованной удаленной поддержки разработки. Временные сессии для сторонних устройств должны быть видимыми, ограниченными по времени, отзывными, аудитируемыми и по умолчанию пользовательского уровня. Проект не принимает скрытую персистентность, обход UAC/sudo, отключение локальных защит или shell без политики.

Английский [README](../../README.md) является авторитетным техническим источником.
