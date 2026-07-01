# Remote Dev Skillkit

Remote Dev Skillkit は、Agent によるリモート開発のための安全カーネルです。隠れたリモート操作ツールではありません。Codex、Claude Code、Hermes、OpenClaw/OpenCode、汎用 MCP Agent が、署名済みジョブ、ホスト側ポリシー、承認、証跡、監査ログを通じて実マシンへ作業を委譲できるようにします。

## 提供するもの

- `rdev` CLI と host、gateway、MCP、verifier バイナリ。
- エクスポート、検証、インストール可能な Agent Skillkit バンドル。
- 署名済み job envelope、workspace lock、approval gate、evidence bundle、audit chain。
- Codex、Claude Code、ACP/acpx、shell、PowerShell アダプター。
- Apache-2.0 オープンソースライセンス。

## ローカル検証

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## Skillkit のインストール

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

技術的な正本は英語の [README](../../README.md) です。
