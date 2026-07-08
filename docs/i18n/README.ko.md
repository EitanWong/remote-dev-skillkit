# Remote Dev Skillkit

Remote Dev Skillkit은 AI Agent가 실제 Mac, Windows, Linux 머신에서 작업할 때 쓰는 안전 레이어입니다. 숨겨진 원격 제어 도구가 아닙니다. Codex, Claude Code, Hermes, OpenClaw/OpenCode, 일반 MCP Agent가 서명된 작업, 호스트 로컬 정책, 승인, 증거 번들, 감사 체인을 통해 실제 개발 작업을 실행하게 합니다.

연결 모델은 Agent용 원격 제어 커넥터입니다. 대상 머신은 보이는 connector를 열고, Agent는 표준 Support Device Entry(`support_device_id` + 세션 비밀번호)를 사용하며, operator가 명시적으로 요청하기 전에는 연결을 끊지 않습니다.

## 핵심 포인트

- 주요 Agent Framework에 설치할 수 있는 휴대용 Agent Skillkit.
- 작업은 서명되고 검증된 뒤 실행됩니다. Agent에게 원시 shell 권한을 통째로 주지 않습니다.
- 표준 `rdev.files.*` / `rdev.desktop.*` 표면은 파일 전송/삭제, 스크린샷/프레임, 창, 키보드/마우스, 클립보드, 앱, URL 작업을 다룹니다.
- Skills는 OS, shell, service manager, gateway, workspace, adapter, 권한을 먼저 탐지합니다. 불명확하면 추측하지 않고 질문합니다.
- Codex, Claude Code, ACP/acpx, shell, PowerShell, custom adapters 지원.
- Apache-2.0 오픈소스 라이선스.

## 빠른 설치

이미 Codex, Claude Code, Hermes, OpenClaw/OpenCode 또는 다른 MCP Agent 안에 있다면 이 링크를 Agent에게 보내고, 실행 전에 전체 prompt를 읽으라고 지시하세요.

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

이 Prompt가 설치 절차의 기준입니다. 저장소 clone/update, 현재 Agent runtime 탐지, Skillkit 설치, MCP 설정, 안전하게 확인할 수 없는 필수 값이 있을 때만 짧게 질문하는 흐름을 정의합니다.

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

`rdev`가 `PATH`에 없으면 `rdev bootstrap agent-plan` 또는 설치 보고서의 `mcp_command` 필드가 알려주는 절대 MCP 명령을 사용하세요.

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
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

기술 기준 문서는 영어 [README](../../README.md)입니다. 번역과 다르면 영어 README를 따르세요.
