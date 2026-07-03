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

If `rdev` is missing, do not stop. Recover it from an existing local rdev binary, build this checkout with `go install ./cmd/rdev`, use `go run ./cmd/rdev bootstrap agent-plan --repo-root .` as a temporary planner, or clone then build in a safe user/workspace location. After `rdev` works, run `rdev bootstrap agent-plan --repo-root .` and follow the JSON plan.

When I ask you to work on another machine, always create a Connection Entry.
Treat it as the universal target-side handoff for every scenario: my own durable
computer, a third-party temporary repair machine, LAN, hosted, relay, mesh, SSH,
or VPN-assisted connectivity. Do not ask humans to assemble ticket, root,
gateway, transport, release, or checksum flags. First call
`rdev.support_session.connect` through MCP or run `rdev support-session connect`.
If it returns `ready_to_send_to_human=true`, send only the returned
`user_handoff.message` plus `user_handoff.copy_paste`. If it returns
`ready_to_send_to_human=false`, run the returned `cli_start_now_command` visible foreground
`rdev support-session connect --start` command, read `ready_file.path` when stdout is hard to parse and `status_file.path` when terminal output is unavailable, then send only the started payload's top-level `user_handoff.message` plus `user_handoff.copy_paste`. If local `rdev`, gateway state, or target helper assets are unclear,
call `rdev.support_session.prepare` through MCP or run
`rdev support-session prepare --build-assets` from the checkout. Follow its `connection_readiness`, `asset_report`, `missing_inputs`,
`agent_connection_runbook`, `gateway_candidate_preflight`, `connectivity_helper_preflight`, and `standard_recovery` fields instead of writing custom bootstrap, approval, helper tunnel, or
relay glue. Read `fresh_agent_connect_contract` when present: it is the shortest standard contract for fresh Agents to recover missing `rdev`, send only the human handoff, wait through standard status surfaces, and avoid custom PowerShell, shell, tunnel, approval, or polling scripts. Read `agent_connection_runbook` first. Also read `agent_connection_runbook.fresh_agent_failure_prevention` before writing setup code: it records known bad fresh-Agent failure patterns such as manual `gateway serve` plus `invite create`, missing helper assets that make Windows say `rdev is required`, background gateway glue, custom approval polling, and Agent-written PowerShell/shell bootstraps. If you are about to write one of those workarounds, stop and use `cli_start_now_command`, `ready_file.path`, `status_file.path`, `connection_supervision`, or `rdev.support_session.prepare` instead. Use its `standard_entry_tool` for ordinary "connect this computer" requests, and obey `low_level_entry_rule`: do not start with `rdev.invites.create`, `rdev invite create`, `rdev.connection_entry.plan`, or `rdev connection-entry plan` unless the operator explicitly requests reviewed package materialization, approved managed owned-host planning, or a returned high-level recovery payload names that path. Then read `gateway_candidate_preflight`, `connectivity_helper_preflight`, and `connection_entry_runner_recommendation` before asking network questions or writing probes. Use the recommended `gateway_url_candidates` entry for simple target-side commands; for durable or restrictive-network connectivity, use the runner recommendation's inline invite JSON plus standard `rdev.connection_entry.plan` / `rdev connection-entry run --dry-run` path. Never send a remote target `0.0.0.0`, and treat loopback as
same-machine only. If `RDEV_HOSTED_GATEWAY_URL`, `RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`, `RDEV_VPN_GATEWAY_URL`, or `RDEV_SSH_GATEWAY_URL` is configured, `rdev` appends it to `gateway_url_candidates` after direct/LAN candidates and before loopback so the target command can fail over without custom tunnel scripts. `connect`, `handoff`, and `create` can use the first configured `RDEV_*_GATEWAY_URL` when no explicit `gateway_url` was supplied. If a lower-level explicit gateway workflow is needed, call
`rdev.support_session.create` through MCP or `rdev support-session create`
through CLI to get the ready target command, join URL, real ticket code,
manifest root, scoped auto-approval state, and status watch command in one
payload. Read `target_bootstrap_requirements` and, for CLI-created sessions, `target_bootstrap_readiness`; if readiness is false for a platform command, recover with `rdev support-session connect --start` or `rdev support-session prepare --build-assets` instead of asking the target-side human to install `rdev` manually. Prefer `user_handoff.message` plus `user_handoff.copy_paste` for
the human-facing response. When `user_handoff.target` is `auto`, follow
`user_handoff.auto_target_rule`: send the join URL first and use returned
platform commands only when a terminal command is needed. The target command already tries ordered Connection
Entry URLs on the target machine with bounded timeout/retry behavior, so do not
write custom fallback scripts. After registration, `rdev host serve --transport auto` reuses signed join-manifest gateway candidates and can switch to another reachable candidate if the current gateway fails before jobs are processed. Read `connection_continuity_policy`; when `stable_after_lan_change=false`, treat LAN as an opportunistic first path and prefer a configured hosted/relay/mesh/VPN/SSH gateway for durable work. If no gateway is
running yet, run `rdev support-session connect --start` in a visible foreground terminal;
it prepares verified helper assets when possible, starts the local gateway,
selects a target-usable gateway URL candidate, prints the same ready session
payload with `agent_connection_runbook` and `gateway_candidate_preflight` before listening, and writes that payload to `ready_file.path` as `support-session-ready.json` by default. It also exposes `foreground_feedback`; while the foreground command stays open, stderr lines prefixed with `rdev support session event: ` report `event=connected` so the Agent can immediately tell the user the connection is established. If stderr is unavailable, read `status_file.path` for the latest machine-readable foreground event. Do not manually combine `rdev gateway serve` plus `rdev invite create` for ordinary support sessions; use `rdev support-session connect --start` so verified helper assets are served automatically. Use
`rdev.support_session.plan` or `rdev support-session plan` only
for review/debug planning. After giving the target-machine command, watch
`rdev.support_session.status` with `wait=true` or `rdev support-session status --wait`; CLI status can omit `--gateway-url` when a configured `RDEV_*_GATEWAY_URL` exists. Created session payloads include `watch_connection_status_configured_gateway`; use that returned command when configured gateway metadata is present. When
`connected=true`, proactively tell me the connection has been established, follow
`connected_next_steps.user_report`, inspect `rdev.hosts.capabilities`, and only
then create the smallest scoped job for my task. If waiting times out or the target does not appear, read
`connection_recovery` and follow its standard tools instead of writing custom
recovery scripts. Use
`rdev.invites.create`, then materialize it with `rdev.connection_entry.plan` or
`rdev connection-entry plan` only when lower-level invite materialization is
needed.
Read `connection_entry.package_catalog` and the signed join manifest
`package_catalog` to select the target OS/architecture package candidate.
Give the target side only the selected link, visible runner/launcher,
visible script, or signed package. Prefer the materialized Connection Entry
runner when `runner_plan` is available; dry-run it before execute when network
reliability is uncertain. If release package assets are not published yet, use
the visible fallback script. Keep low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off repair
machines by following `connection_entry_plan.target_selection_policy`.
Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed. Use
existing open-source/free options through configured helper gateway overrides such
as `RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`,
`RDEV_VPN_GATEWAY_URL`, or `RDEV_SSH_GATEWAY_URL` when unambiguous; prefer frp,
Chisel, headscale-compatible mesh, or WireGuard before paid relays. If a
user-space helper is missing and an explicit URL/SHA-256 is approved, use
`rdev deps install` or reviewed `RDEV_*_INSTALL_ACTION_JSON` metadata for
user/workspace-scoped helper repair before helper startup; ask before
privileged, persistent, paid, firewall, DNS, cloud, credential, route, or
security-policy changes. For company or third-party machines, ask for authorization first, then default to visible attended-temporary mode and let Connection Entry probes detect Windows/macOS/Linux. Dry-run before execute. Do not hardcode private paths,
secrets, or server addresses; example URLs are placeholders only.
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
