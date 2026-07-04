# Remote Dev Skillkit

Remote Dev Skillkit 是给 AI Agent 使用真实 Mac、Windows、Linux 机器时准备的安全层。它不是隐藏远控工具，而是让 Codex、Claude Code、Hermes、OpenClaw/OpenCode 和通用 MCP Agent 通过签名任务、主机本地策略、审批门禁、证据包和审计链来执行真实开发工作。

## 核心亮点

- 一个可移植的 Agent Skillkit，可安装到主流 Agent Framework。
- 任务先签名、再校验、再执行，不把原始 shell 权限随手交给 Agent。
- Skills 会先探测 OS、shell、service manager、gateway、workspace、adapter 和权限；不清楚就询问。
- 支持 Codex、Claude Code、ACP/acpx、shell、PowerShell 和自定义 adapter。
- Apache-2.0 开源许可证。

## 快速安装

如果你已经在 Codex、Claude Code、Hermes、OpenClaw/OpenCode 或其他 MCP Agent 里，可以直接复制这段给你的 Agent：

```text
Install Remote Dev Skillkit for this agent runtime.

Repository: https://github.com/EitanWong/remote-dev-skillkit
Full install prompt: https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Clone or update the repository, read the full install prompt, and follow it as the source of truth. If cloning is blocked, open the prompt link directly. Ask one short question only when a required value is unclear.
```

完整版复制 Prompt 见 [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md)。

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

导出并验证 Skillkit bundle：

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

为主流 Agent Framework 生成可审查安装计划：

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

直接安装默认是 dry-run。确认目标目录无误后，再加 `--execute`：

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

第二条 install 命令就是已验证 bundle 存在后的“一条命令安装”路径。

## 本地试跑

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

英文 [README](../../README.md) 是权威技术说明；如果翻译和英文冲突，请以英文为准。
