# 在 DeepSeek Harness 中使用 Remote Dev

[English](README.md)

这是一个实验性 Bundle。它复用 DeepSeek Harness 自带的
`@deepseek-ai/dsh-mcp-client`，通过 stdio 接入 `rdev`，不会复制工具定义，
也不会创建第二套控制面。

## 前置条件

- DeepSeek Harness developer preview `0.1.0-rc.8`，使用其支持的 Node 范围
  `^22.19.0 || >=24.0.0`，并且 `dsh`、pnpm 11 已在 `PATH` 中。
- 已安装 `rdev`。Bundle 会依次查找 `RDEV_BIN`、`~/.local/bin/rdev` 和
  `PATH`。

先固定到已经验证过的 Harness 版本，再安装 Bundle：

```sh
pnpm add --global @deepseek-ai/dsh@0.1.0-rc.8
dsh --version  # 必须输出 0.1.0-rc.8
```

如果 pnpm 提示 global bin 目录未配置，先执行 `pnpm setup`，重启终端后再
重复上面两条命令。不要使用不带版本的 `@deepseek-ai/dsh`：npm 的
`latest` 目前仍是 rc.7，而本 Bundle 按 rc.8 验证。

在 Remote Dev Skillkit 仓库根目录执行：

```sh
./scripts/install.sh
dsh plugin --profile web add ./integrations/deepseek-harness
```

当前 Bundle 刻意保持 private，尚未发布到 npm。新增或移除 Bundle 后，
需要重启正在运行的 Profile。

## 连接网关

要操作已托管主机，请连接 operator 管理的 HTTPS gateway。Bearer token
保存在仅 owner 可读的文件中，只传文件路径：

```sh
export RDEV_GATEWAY_URL=https://gateway.example
export RDEV_GATEWAY_OPERATOR_TOKEN_FILE=/protected/rdev-operator.token
dsh --profile web --dump-config
dsh web
```

本机开发时，先单独启动 loopback gateway，再从另一个终端启动 Harness：

```sh
rdev gateway serve --dev
RDEV_GATEWAY_URL=http://127.0.0.1:8787 dsh web
```

不设置 `RDEV_GATEWAY_URL` 时，`rdev` 使用 MCP 进程内的隔离控制面，只适合
协议检查，远程主机无法加入。

配置输出中应恰好出现一个 `id: rdev`。Bundle 设置了
`failOnStartupError`，所以 Profile 成功启动即代表 MCP 子进程已连接并同步
工具列表。工具名以 `mcp__rdev__` 开头；原始名称中的点号会触发 Harness
添加确定性的 hash 后缀。

Headless 用法：

```sh
dsh plugin --profile headless add ./integrations/deepseek-harness
dsh --profile headless "列出当前在线的远程主机，并概括状态。"
```

## 移除

```sh
dsh plugin --profile web remove rdev-deepseek-harness
```

## 常见问题

| 现象 | 处理 |
| --- | --- |
| `spawn rdev ENOENT` | 将 `RDEV_BIN` 设置为 `rdev` 的绝对路径。 |
| `read operator token file` | 检查路径和 owner-only 读取权限。 |
| Harness 更新后 Profile 启动失败 | 按 rc.8 契约重新验证；preview API 可能破坏兼容。 |
| 会话一直等待主机 | 确认配置了可达网关，再核对主机心跳是否新鲜。 |

Bundle 契约已纳入 `go test ./...` 和仓库标准 `./scripts/check.sh` 门禁。维护者
安装 `dsh` 后可运行 `integrations/deepseek-harness/live-smoke.sh`，它使用临时
loopback gateway 和 Profile，不会写入正常的 `~/.dsh`。

## 安全与兼容边界

Bundle 只启动 `rdev mcp serve`。会话策略、operator 授权、审计、中断和目标
主机控制仍由 rdev 负责。包内不写入 token 值，不开放目标主机入站端口，
MCP stdout 始终只承载协议。

当前版本锁定并验证 DeepSeek Harness `0.1.0-rc.8`。上游仍将其标记为
developer preview，并明确可能发生破坏性变更。专属 clean tool names 以及
主机、会话、任务原生卡片等 UI，等上游插件 UI 契约稳定后再接入。