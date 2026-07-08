# Remote Dev Skillkit

Remote Dev Skillkit 是给 AI Agent 使用真实 Mac、Windows、Linux 机器时准备的安全层。它不是隐藏远控工具，而是让 Codex、Claude Code、Hermes、OpenClaw/OpenCode 和通用 MCP Agent 通过签名任务、主机本地策略、审批门禁、证据包和审计链来执行真实开发工作。

它面向 Agent 的远控式连接模型：目标机器打开可见连接器，Agent 使用标准 Support Device Entry（`support_device_id` + 会话密码），并且只有在操作者明确要求时才断开。

## 核心亮点

- 一个可移植的 Agent Skillkit，可安装到主流 Agent Framework。
- 任务先签名、再校验、再执行，不把原始 shell 权限随手交给 Agent。
- 标准 `rdev.files.*` / `rdev.desktop.*` 覆盖文件传输/删除、截图/录屏帧、窗口、键盘鼠标、剪贴板、App 和 URL 操作。
- Skills 会先探测 OS、shell、service manager、gateway、workspace、adapter 和权限；不清楚就询问。
- 支持 Codex、Claude Code、ACP/acpx、shell、PowerShell 和自定义 adapter。
- Apache-2.0 开源许可证。

## 快速安装

如果你已经在 Codex、Claude Code、Hermes、OpenClaw/OpenCode 或其他 MCP Agent 里，把这个链接发给 Agent，并要求它先完整阅读再执行：

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

这个 Prompt 是安装流程的唯一准绳：clone 或更新仓库、探测当前 Agent Runtime、安装 Skillkit、配置 MCP，以及只有在必要值无法安全探测时才问一个简短问题。

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

如果 `rdev` 不在 `PATH` 中，请使用 `rdev bootstrap agent-plan` 或安装报告 `mcp_command` 字段给出的绝对 MCP 命令。

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
