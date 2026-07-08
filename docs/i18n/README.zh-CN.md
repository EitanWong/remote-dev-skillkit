# Remote Dev Skillkit

Remote Dev Skillkit 是一个开源的 AI 原生远程开发 Skillkit。它让 Codex、Claude Code、Hermes、OpenClaw/OpenCode 和通用 MCP Agent 可以安全地帮助真实的 Mac、Windows、Linux 主机，而不是直接拿到无限制 shell。

## 它做什么

| Agent 获得 | 人类保留 |
|---|---|
| Skills、MCP 工具、文件/桌面/任务适配器 | 可见性、审批、撤销、审计 |
| 带明确权限的签名任务 | 主机本地策略和安全边界 |
| Artifacts 和 evidence bundles | 控制运行内容和停止时间 |

## AI Agent 安装

复制下方文字发送给 Agent 即可：

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

完整安装契约见 [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)。

<details>
<summary>手动安装命令</summary>

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

## 使用

1. 让 Agent 连接机器：

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. 查看工具和本地演示：

```bash
rdev mcp tools
rdev demo local
```

3. 运行基础验证：

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## 安全

Remote Dev Skillkit 面向明确同意、可见的远程开发支持。临时第三方会话必须可见、限时、可撤销、可审计，并默认使用用户级权限。项目不接受隐藏持久化、绕过 UAC/sudo、禁用本地安全控制，或没有策略约束的无限制 shell。许可证是 Apache-2.0。

## 文档

- 英文权威 README：[../../README.md](../../README.md)
- 文档索引：[../README.md](../README.md)
