# Remote Dev Skillkit

Remote Dev Skillkit은 AI Agent를 위한 오픈소스 Agent-native 원격 개발 Skillkit입니다. Codex, Claude Code, Hermes, OpenClaw/OpenCode, MCP Agent가 실제 Mac, Windows, Linux 호스트에서 작업할 때 무제한 shell을 넘기지 않고 안전한 작업 표면을 제공합니다.

이 프로젝트는 Agent Skills, MCP 도구, 서명된 작업, 호스트 로컬 정책, 승인, 감사 로그, 증거 번들을 묶습니다. 라이선스는 Apache-2.0입니다.

## Agent에 붙여넣을 설치 프롬프트

아래 내용을 그대로 Agent에 보내세요.

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

전체 계약은 [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)에 있습니다.

## 수동 빠른 시작

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

## 로컬 테스트

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## 안전 모델

Remote Dev Skillkit은 명시적이고 보이는 동의 기반 원격 개발 지원을 위해 만들어졌습니다. 임시 제3자 세션은 보이고, 시간 제한이 있으며, 취소 가능하고, 감사 가능하며, 기본적으로 사용자 권한이어야 합니다. 숨겨진 지속성, UAC/sudo 우회, 로컬 보안 제어 비활성화, 정책 없는 shell은 허용하지 않습니다.

영문 [README](../../README.md)가 권위 있는 기술 문서입니다.
