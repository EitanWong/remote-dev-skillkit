# Remote Dev Skillkit

Remote Dev Skillkit - это open-source, agent-native skillkit для удаленной разработки. Он позволяет Codex, Claude Code, Hermes, OpenClaw/OpenCode и MCP-агентам работать с реальными Mac, Windows и Linux hosts без выдачи неограниченного shell.

## Что он делает

| Агент получает | Человек сохраняет |
|---|---|
| Skills, MCP tools и адаптеры files/desktop/jobs | Видимость, approval, revocation и audit |
| Подписанные jobs с понятными capabilities | Локальную host policy и security boundaries |
| Artifacts и evidence bundles | Контроль над тем, что запускается и когда останавливается |

## Установка через агента

Скопируйте текст ниже и отправьте его агенту:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

Полный контракт находится в [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

<details>
<summary>Ручные команды</summary>

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

## Использование

1. Подключите машину:

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. Посмотрите tools и локальную demo:

```bash
rdev mcp tools
rdev demo local
```

3. Проверьте базовые evidence:

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## Безопасность

Remote Dev Skillkit создан для явной, видимой и согласованной удаленной поддержки разработки. Временные сессии для сторонних устройств должны быть видимыми, ограниченными по времени, отзывными, аудитируемыми и по умолчанию пользовательского уровня. Проект не принимает скрытую персистентность, обход UAC/sudo, отключение локальных защит или shell без политики. Лицензия Apache-2.0.

## Документация

- Авторитетный английский README: [../../README.md](../../README.md)
- Индекс документации: [../README.md](../README.md)
