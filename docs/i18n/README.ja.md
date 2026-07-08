# Remote Dev Skillkit

Remote Dev Skillkit は、AI Agent が実際の Mac、Windows、Linux マシンで作業するための安全レイヤーです。隠れたリモート操作ツールではありません。Codex、Claude Code、Hermes、OpenClaw/OpenCode、汎用 MCP Agent が、署名済みジョブ、ホスト側ポリシー、承認、証跡バンドル、監査ログを通じて実作業を実行できます。

接続モデルは Agent 向けのリモート操作コネクタです。対象マシンは見える Connector を開き、Agent は標準の Support Device Entry（`support_device_id` + セッションパスワード）を使い、Operator が明示的に求めるまで切断しません。

## 主なポイント

- 主要な Agent Framework に導入できるポータブルな Agent Skillkit。
- ジョブは署名、検証、その後に実行されます。生の shell 権限をそのまま渡しません。
- 標準の `rdev.files.*` / `rdev.desktop.*` は、ファイル転送/削除、スクリーンショット/フレーム、ウィンドウ、キーボード/マウス、クリップボード、App、URL 操作を扱います。
- Skills は OS、shell、service manager、gateway、workspace、adapter、権限を検出します。不明な場合は推測せず質問します。
- Codex、Claude Code、ACP/acpx、shell、PowerShell、カスタム adapters に対応。
- Apache-2.0 オープンソースライセンス。

## クイックインストール

すでに Codex、Claude Code、Hermes、OpenClaw/OpenCode、または別の MCP Agent を使っている場合は、このリンクを Agent に渡し、実行前に全文を読むよう指示してください。

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

この Prompt がインストール手順の信頼できる基準です。リポジトリの clone / update、現在の Agent runtime の検出、Skillkit のインストール、MCP 設定、安全に検出できない必須値がある場合だけ短い質問をする流れを定義しています。

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

`rdev` が `PATH` にない場合は、`rdev bootstrap agent-plan` またはインストールレポートの `mcp_command` フィールドが示す絶対パスの MCP コマンドを使ってください。

Skillkit bundle をエクスポートして検証します：

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Agent Framework 向けの確認可能なインストール計画を作成します：

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

直接インストールはデフォルトで dry-run です。確認後に `--execute` を追加します：

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

2 つ目の install コマンドが、検証済み bundle がある場合の one-command install パスです。

## ローカルデモ

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

技術的な正本は英語の [README](../../README.md) です。翻訳と異なる場合は英語版を優先してください。
