# Remote Dev Skillkit

Remote Dev Skillkit は、AI Agent のためのオープンソースで Agent ネイティブなリモート開発 Skillkit です。Codex、Claude Code、Hermes、OpenClaw/OpenCode、MCP Agent が実際の Mac、Windows、Linux ホストを扱うとき、無制限の shell を渡さずに安全な作業面を提供します。

## できること

| Agent が得るもの | 人が保つもの |
|---|---|
| Skills、MCP tools、file/desktop/task adapters | 可視性、承認、取り消し、監査 |
| 明確な capability を持つ署名済み tasks | ホストローカル policy と安全境界 |
| Artifacts と session evidence | 何を実行し、いつ止めるかの制御 |

## Agent でインストール

下の文字列を Agent に送ってください。

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

完全な契約は [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md) にあります。

<details>
<summary>手動コマンド</summary>

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

## 使い方

1. マシンを接続します。

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. ツールとローカルデモを確認します。

```bash
rdev mcp tools
rdev demo local
```

3. 基本的な証跡を確認します。

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## 安全性

Remote Dev Skillkit は、明示的で可視の同意ベースのリモート開発支援を前提にしています。一時的な第三者セッションは、可視、期限付き、取り消し可能、監査可能、かつ既定でユーザーレベルである必要があります。隠れた永続化、UAC/sudo バイパス、ローカル安全制御の無効化、ポリシーなしの shell は受け入れません。ライセンスは Apache-2.0 です。

## Docs

- 技術的な正本: [../../README.md](../../README.md)
- ドキュメント索引: [../README.md](../README.md)
