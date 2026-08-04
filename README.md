# Remote Dev Skillkit

让 AI Agent 安全地在你的 Mac、Windows、Linux 主机上做真实开发工作。

**你遇到的场景**：Agent 有模型、有代码能力，但没有一台"能干活的主机"——或者你不想给 Agent 一把能碰所有东西的钥匙。Remote Dev Skillkit 是两者之间的受控通道：Agent 通过 MCP 提交**有边界、可审计、可中断**的任务，主机用**短期 join code** 加入会话、在本地策略内执行、回报事件与产物。

```text
Agent (MCP client) ──rdev mcp serve──> Control Plane ──long-poll──> Host
                                     (gateway)                (rdev host serve)
                                        │
                                        └── 每个动作：策略 + 审计 + 事件推送
```

## 它解决什么

- **给 Agent 一台受控主机**：临时任务用 join code，长期任务用 Windows 服务（浏览器 handoff 一键安装），全部策略约束。
- **不给 Agent 不受限访问**：无入站端口、无隐藏持久化、不绕过本地安全控制（UAC/TCC/Gatekeeper）。
- **Agent 能感知进展**：事件推送（webhook）让 Agent 及时知道主机上线、任务完成、产物就绪，而不是轮询。

## 快速开始（按你的角色）

### 🖥️ 我是开发者，想让 Agent 用我的电脑

```bash
go install github.com/EitanWong/remote-dev-skillkit/cmd/rdev@latest   # Go 1.25+
rdev host serve --join-code CODE --gateway https://your-gateway
```

- 一次性的临时支持：加 `--once`（打印连接状态后退出）。
- 长期托管（Windows）：让操作员发你一个浏览器 handoff 链接，页面自动生成可复制 PowerShell，粘贴 → 可见确认 → 装成服务，之后开机自启、断线重连。

### 🤖 我是 Agent（或运行 Agent 的人），想驱动远程主机

```bash
rdev mcp serve                                  # 本地控制平面
rdev mcp serve --gateway-url URL --operator-token-file PATH   # 代理到远程 gateway
```

然后把 `rdev mcp serve` 注册进你的 MCP 客户端（Claude Code / Codex / Hermes / OpenCode 均可）。工具自描述：每个工具都带 `safety` 说明和 `user_summary`/`agent_next_action` 引导，Agent 无需读文档即可正确使用。

### 🏗️ 我是网关操作者，想跑一个多主机控制面

```bash
rdev gateway serve --dev                                     # 本机试验
rdev gateway serve --operator-auth-file ops.token --state-file state.json \
    --signing-key-file key.pem --public-base-url https://gw.example \
    --windows-amd64-host-binary rdev-host.exe               # 生产形态
```

生产网关只监听 loopback，由你自己的 HTTPS 反向代理对外；operator 认证、持久化状态、签名密钥、审计全部显式配置。

## 2 分钟最小演示（全本机）

```bash
# 终端 1：起本地 gateway
rdev gateway serve --dev --addr 127.0.0.1:8788

# 终端 2：Agent 视角——建会话、拿 join code（也可用 rdev mcp tools 查看完整 MCP）
curl -X POST http://127.0.0.1:8788/v1/sessions -H 'Content-Type: application/json' \
     -d '{"reason":"first demo"}'                    # 返回 session id + join code

# 终端 3：主机视角——加入会话
rdev host serve --join-code CODE --gateway http://127.0.0.1:8788 --once

# 终端 2：看到 hello 事件，提交一个只读任务，收到结果事件
```

## 安装

最快（一条命令，自动装 Go，无需管理员）：

```bash
curl -fsSL https://raw.githubusercontent.com/EitanWong/remote-dev-skillkit/main/scripts/install.sh | bash
```

或手动（需要 Go 1.25+）：

```bash
go install github.com/EitanWong/remote-dev-skillkit/cmd/rdev@latest
```

Windows 目标主机无需手动下载：浏览器 handoff 会自动获取并校验主机二进制。

## 排障速查

| 症状 | 原因与解法 |
|---|---|
| `bind: address already in use` | 端口被占用（如 Cloudflare 等常驻服务）。换端口：`--addr 127.0.0.1:8789` |
| host join 一直失败 | gateway 不可达或 join code 过期。join code 短时有效，重新建会话再试 |
| Agent 报 `403` | operator token 未配置/不匹配。确认 `--operator-token-file` 指向受保护文件 |
| Windows 上没装成服务 | handoff 链接需在**目标 Windows 主机**的浏览器打开，且同意 UAC |
| 收不到事件推送 | webhook 需 HTTPS（本机 Hermes 可用 loopback HTTP）；见 `rdev.sessions.notify` |

## 安全边界

- 主机不暴露任何入站公网端口；所有连接由主机主动外连（long-poll）。
- 每个任务都受策略约束、限定作用域、可审计、可中断；临时会话默认不持久。
- 不绕过 UAC、sudo、TCC、Gatekeeper、Windows Defender 等本地安全控制。

## 验证与文档

- 质量门禁：`./scripts/check.sh`（gofmt、测试、vet、覆盖率门禁、surface 审计、release smoke）。
- 架构：[SESSION_CONTROL_PLANE.md](docs/architecture/SESSION_CONTROL_PLANE.md)
- 安全边界：[BOUNDARIES.md](docs/security/BOUNDARIES.md)
- 质量矩阵（live E2E 状态）：[QUALITY_MATRIX.md](docs/development/QUALITY_MATRIX.md)
- 贡献：[CONTRIBUTING.md](CONTRIBUTING.md)
