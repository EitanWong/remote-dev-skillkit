# Remote Dev Skillkit

Remote Dev Skillkit은 AI Agent가 실제 Mac, Windows, Linux 머신에서 작업할 때 쓰는 안전 레이어입니다. 숨겨진 원격 제어 도구가 아닙니다. Codex, Claude Code, Hermes, OpenClaw/OpenCode, 일반 MCP Agent가 서명된 작업, 호스트 로컬 정책, 승인, 증거 번들, 감사 체인을 통해 실제 개발 작업을 실행하게 합니다.

## 핵심 포인트

- 주요 Agent Framework에 설치할 수 있는 휴대용 Agent Skillkit.
- 작업은 서명되고 검증된 뒤 실행됩니다. Agent에게 원시 shell 권한을 통째로 주지 않습니다.
- Skills는 OS, shell, service manager, gateway, workspace, adapter, 권한을 먼저 탐지합니다. 불명확하면 추측하지 않고 질문합니다.
- Codex, Claude Code, ACP/acpx, shell, PowerShell, custom adapters 지원.
- Apache-2.0 오픈소스 라이선스.

## 빠른 설치

이미 Codex, Claude Code, Hermes, OpenClaw/OpenCode 또는 다른 MCP Agent 안에 있다면 이 내용을 Agent에 붙여 넣으세요.

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
`rdev.support_session.plan` through MCP or `rdev support-session plan` through
CLI to get the standard gateway startup, verified helper assets, invite commands,
localized target command, and scoped auto-approval plan. Use
`rdev.invites.create`, then materialize it with `rdev.connection_entry.plan` or
`rdev connection-entry plan`.
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

전체 복사-붙여넣기 Prompt: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

Skillkit bundle을 내보내고 검증합니다:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Agent Framework용 검토 가능한 설치 계획을 생성합니다:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

직접 설치는 기본적으로 dry-run입니다. 보고서를 확인한 뒤 `--execute`를 추가하세요:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

두 번째 install 명령이 검증된 bundle이 있을 때의 one-command install 경로입니다.

## 로컬 데모

```bash
go test ./...
rdev demo local
rdev mcp tools
```

기술 기준 문서는 영어 [README](../../README.md)입니다. 번역과 다르면 영어 README를 따르세요.
