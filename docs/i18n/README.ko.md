# Remote Dev Skillkit

Remote Dev Skillkit은 AI Agent를 위한 오픈소스 Agent-native 원격 개발 Skillkit입니다. Codex, Claude Code, Hermes, OpenClaw/OpenCode, MCP Agent가 실제 Mac, Windows, Linux 호스트에서 작업할 때 무제한 shell을 넘기지 않고 안전한 작업 표면을 제공합니다.

## 하는 일

| Agent가 얻는 것 | 사람이 유지하는 것 |
|---|---|
| Skills, MCP tools, file/desktop/job adapters | 가시성, 승인, 철회, 감사 |
| 명확한 capability가 있는 서명된 jobs | 호스트 로컬 policy와 보안 경계 |
| Artifacts와 evidence bundles | 무엇을 실행하고 언제 멈출지에 대한 통제 |

## Agent로 설치

아래 텍스트를 Agent에 보내세요.

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

전체 계약은 [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)에 있습니다.

<details>
<summary>수동 명령</summary>

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

</details>

## 사용

1. 머신을 연결합니다.

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. 도구와 로컬 데모를 확인합니다.

```bash
rdev mcp tools
rdev demo local
```

3. 기본 증거를 확인합니다.

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## 안전

Remote Dev Skillkit은 명시적이고 보이는 동의 기반 원격 개발 지원을 위해 만들어졌습니다. 임시 제3자 세션은 보이고, 시간 제한이 있으며, 취소 가능하고, 감사 가능하며, 기본적으로 사용자 권한이어야 합니다. 숨겨진 지속성, UAC/sudo 우회, 로컬 보안 제어 비활성화, 정책 없는 shell은 허용하지 않습니다. 라이선스는 Apache-2.0입니다.

## 문서

- 권위 있는 영문 README: [../../README.md](../../README.md)
- 문서 색인: [../README.md](../README.md)
