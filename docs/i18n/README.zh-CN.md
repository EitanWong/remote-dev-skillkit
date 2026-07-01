# Remote Dev Skillkit

Remote Dev Skillkit 是一个面向 Agent 开发者的远程开发安全内核。它不是隐藏远控工具，而是让 Codex、Claude Code、Hermes、OpenClaw/OpenCode 和通用 MCP Agent 以可审计、可撤销、受策略约束的方式，把任务交给真实机器执行。

## 核心能力

- `rdev` CLI、`rdev-host`、`rdev-gateway`、`rdev-mcp` 和 `rdev-verify`。
- Agent Skillkit：可导出、验证并安装到主流 Agent Framework。
- 签名 job envelope、host 本地策略校验、workspace lock、approval gate、evidence bundle、audit chain。
- Codex、Claude Code、ACP/acpx、shell、PowerShell 适配器。
- Apache-2.0 开源许可证。

## 快速验证

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## 安装 Skillkit

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

详细技术说明以英文 [README](../../README.md) 为准。
