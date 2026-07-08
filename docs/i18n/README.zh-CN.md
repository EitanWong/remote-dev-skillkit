# Remote Dev Skillkit

Remote Dev Skillkit 是一个开源的 AI 原生远程开发 Skillkit。它让 Codex、Claude Code、Hermes、OpenClaw/OpenCode 和通用 MCP Agent 可以安全地帮助真实的 Mac、Windows、Linux 主机，而不是直接拿到无限制 shell。

项目用 Agent Skills、MCP 工具、签名任务、主机本地策略、审批门禁、审计日志和证据包，把远程修复流程变成可见、可撤销、可验证的工作流。许可证是 Apache-2.0。

## AI Agent 安装

复制下方文字发送给 Agent 即可：

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

完整安装契约见 [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)。

## 手动快速开始

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

## 本地试跑

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## 安全边界

Remote Dev Skillkit 面向明确同意、可见的远程开发支持。临时第三方会话必须可见、限时、可撤销、可审计，并默认使用用户级权限。项目不接受隐藏持久化、绕过 UAC/sudo、禁用本地安全控制，或没有策略约束的无限制 shell。

英文 [README](../../README.md) 是权威技术说明；如果翻译和英文冲突，请以英文为准。
