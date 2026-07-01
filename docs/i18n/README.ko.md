# Remote Dev Skillkit

Remote Dev Skillkit은 Agent 기반 원격 개발을 위한 안전 커널입니다. 숨겨진 원격 제어 도구가 아닙니다. Codex, Claude Code, Hermes, OpenClaw/OpenCode, 일반 MCP Agent가 서명된 작업, 호스트 로컬 정책, 승인, 증거, 감사 로그를 통해 실제 머신에 작업을 위임할 수 있게 합니다.

## 제공 기능

- `rdev` CLI와 host, gateway, MCP, verifier 바이너리.
- 내보내기, 검증, 설치가 가능한 Agent Skillkit 번들.
- 서명된 job envelope, workspace lock, approval gate, evidence bundle, audit chain.
- Codex, Claude Code, ACP/acpx, shell, PowerShell 어댑터.
- Apache-2.0 오픈소스 라이선스.

## 로컬 검증

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## Skillkit 설치

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

기술 기준 문서는 영어 [README](../../README.md)입니다.
