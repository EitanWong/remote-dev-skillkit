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
Bootstrap Remote Dev Skillkit for this agent runtime.

Repository: https://github.com/EitanWong/remote-dev-skillkit

Clone or update the repository in a safe user/workspace location. Then read
`docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow it as
the source of truth. If cloning is blocked, read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Install the Skillkit and configure MCP for this agent. Probe OS, shell, Git, Go,
`rdev`, current agent framework, skill directory, MCP config, and network state
before acting. Ask one short question when a required value is unclear. For this
personal computer, prefer local MCP stdio with `rdev mcp serve`; do not require a
hosted gateway URL.

When I ask you to work on another machine, always create a Connection Entry.
Treat it as the universal target-side handoff for every scenario: my own durable
computer, a third-party temporary repair machine, LAN, hosted, relay, mesh, SSH,
or VPN-assisted connectivity. Do not ask humans to assemble ticket, root,
gateway, transport, release, or checksum flags. Use `rdev.invites.create`, then
materialize it with `rdev.connection_entry.plan` or `rdev connection-entry plan`.
Give the target side only the selected link, visible script, or signed package;
keep low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off repair
machines by following `connection_entry_plan.target_selection_policy`.
Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed;
prefer existing, open-source/free options such as frp, Chisel, headscale, or
WireGuard; ask before privileged, persistent, paid, firewall, DNS, cloud, or
security-policy changes. Dry-run before execute. Do not hardcode private paths,
secrets, or server addresses; example URLs are placeholders only.
```

完整版复制 Prompt 见 [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md)。

```bash
go install ./cmd/rdev
rdev doctor
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
rdev demo local
rdev mcp tools
```

英文 [README](../../README.md) 是权威技术说明；如果翻译和英文冲突，请以英文为准。
