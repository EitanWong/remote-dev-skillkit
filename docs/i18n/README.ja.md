# Remote Dev Skillkit

Remote Dev Skillkit は、AI Agent のためのオープンソースで Agent ネイティブなリモート開発 Skillkit です。Codex、Claude Code、Hermes、OpenClaw/OpenCode、MCP Agent が実際の Mac、Windows、Linux ホストを扱うとき、無制限の shell を渡さずに安全な作業面を提供します。

Agent Skills、MCP ツール、署名済みジョブ、ホスト側ポリシー、承認、監査、証跡バンドルをまとめます。ライセンスは Apache-2.0 です。

## Agent に貼り付けるインストールプロンプト

次をそのまま Agent に送ってください。

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

完全な契約は [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md) にあります。

## 手動クイックスタート

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

## ローカルで試す

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## 安全性

Remote Dev Skillkit は、明示的で可視の同意ベースのリモート開発支援を前提にしています。一時的な第三者セッションは、可視、期限付き、取り消し可能、監査可能、かつ既定でユーザーレベルである必要があります。隠れた永続化、UAC/sudo バイパス、ローカル安全制御の無効化、ポリシーなしの shell は受け入れません。

技術的な正本は英語の [README](../../README.md) です。
