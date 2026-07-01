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

First clone or update the repository in a safe user/workspace location. Then
read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow
that full prompt as the source of truth for installation. If cloning is blocked,
read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Probe my OS, shell, Git, Go, `rdev`, current agent framework, skill directory,
MCP config, network reachability, proxy/DNS state, NAT/firewall/CGNAT hints,
SSH config, installed tunnel/mesh tools, and available connection modes before
acting. If a required value is unclear, ask me one short question instead of
guessing. For a personal computer agent install, use local MCP stdio with
`rdev mcp serve`; do not require a hosted gateway URL. When remote hosts are
needed, auto-select the safest working mode: local dev gateway, LAN-reachable
gateway, hosted gateway, SSH tunnel, or relay/mesh/VPN. If tunneling is needed,
prefer open-source/free options first, such as frp, Chisel, headscale, or
WireGuard, after probing what is already installed. Dry-run before execute. Do
not hardcode private paths, secrets, or server addresses; treat
`https://api.example.com/v1` only as optional hosted-gateway placeholder
metadata.
```

전체 복사-붙여넣기 Prompt: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
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
