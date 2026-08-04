# Remote Dev Skillkit

[English](README.md)

**面向 Agent 的远程开发工具包。** 让 Claude Code、Codex、Hermes、OpenCode 或任意支持 MCP 的 Agent，在你的 Mac、Windows、Linux 主机上安全地干活 —— 有边界、可审计、可中断。

```text
Agent (MCP client) ── rdev mcp serve ──> 控制面 ── long-poll ──> 主机
                                          (rdev gateway)         (rdev host serve)
```

## 为什么

Agent 有模型、有写代码的能力，但没有一台"能干活的主机"——你也不该给它一把能碰所有东西的钥匙。Remote Dev Skillkit 就是两者之间的受控通道。

- **有边界** —— 主机用短期 join code 加入会话，每个任务受策略与能力上限约束。
- **可审计、可中断** —— 每个动作都有记录，随时可以中断或撤销。
- **托管主机** —— Windows 主机通过浏览器 handoff（复制粘贴 PowerShell）装成服务，开机自启、断线重连、控制面远程升级。
- **事件驱动** —— Agent 通过 webhook 收到推送（主机上线、任务完成、产物就绪），无需轮询。
- **零暴露** —— 主机只外连，无入站端口、无隐藏持久化、不绕过 UAC / TCC / Gatekeeper / Defender。

## 快速开始

**主机侧** —— 让 Agent 用这台机器：

```bash
go install github.com/EitanWong/remote-dev-skillkit/cmd/rdev@latest
rdev host serve --join-code CODE --gateway https://your-gateway
```

**Agent 侧** —— 通过控制面驱动远程主机：

```bash
rdev mcp serve --gateway-url URL --operator-token-file PATH
```

把 `rdev mcp serve` 注册进你的 MCP 客户端即可。工具自带安全说明与 Agent 引导，无需读文档。

**网关运维** —— 跑一个多主机控制面：

```bash
rdev gateway serve --dev                                    # 本机试验
rdev gateway serve --operator-auth-file ops.token --state-file state.json \
    --signing-key-file key.pem --public-base-url https://gw.example
```

## 安装

```bash
curl -fsSL https://raw.githubusercontent.com/EitanWong/remote-dev-skillkit/main/scripts/install.sh | bash
```

手动安装需 Go 1.25+。Windows 目标主机无需手动下载 —— 浏览器 handoff 会自动获取并校验主机二进制。

## 安全模型

- 仅出站连接，无入站公网端口。
- 任务受策略约束、限定作用域、可审计、可中断；临时会话默认不持久。
- 绝不绕过本地安全控制（UAC、sudo、TCC、Gatekeeper、Windows Defender）。

## 文档

- 架构：[SESSION_CONTROL_PLANE.md](docs/architecture/SESSION_CONTROL_PLANE.md)
- 安全边界：[BOUNDARIES.md](docs/security/BOUNDARIES.md)
- 质量矩阵（live E2E 状态）：[QUALITY_MATRIX.md](docs/development/QUALITY_MATRIX.md)
- 主机更新手册：[UPDATE_RUNBOOK.md](docs/operations/UPDATE_RUNBOOK.md)
- 质量门禁：`./scripts/check.sh`

## 许可证

MIT —— 见 [LICENSE](LICENSE)。
