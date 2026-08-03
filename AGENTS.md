# Agent Instructions

## Project Goal

Build a general-purpose, open-source agent-native remote development skillkit. The end state is a reliable toolkit that lets Hermes, Codex, Claude Code, OpenCode, and similar agents safely delegate coding and repair tasks to enrolled Mac, Windows, and Linux hosts.

## Safety Boundaries

- Do not implement hidden persistence.
- Do not bypass UAC, sudo, TCC, Gatekeeper, Windows Defender, or other local security controls.
- Do not create unattended access for third-party machines.
- Do not expose target hosts through inbound public ports.
- Do not add tools that provide unrestricted shell access without policy enforcement.
- Do not store secrets in source files, logs, prompts, test fixtures, or audit artifacts.

## Engineering Rules

- Keep the Phase 1 MVP small and verifiable.
- Prefer standard-library Go until an external dependency clearly pays for itself.
- Treat Windows as a primary platform, not an afterthought.
- Every networked or privileged action must have policy and audit design before implementation.
- Tests should cover command parsing, contracts, policy decisions, and safety invariants.

## Current Surface

- `rdev doctor`, `rdev mcp tools`, `rdev mcp serve`
- `rdev host serve --join-code CODE --gateway URL` (attended-temporary) and managed Windows service bootstrap via browser handoff
- `rdev gateway serve --dev` for loopback development; production gateway is operator-managed (bearer/OIDC/SAML auth, persistent state, signing key, audit)
- Session MCP tools: `create`, `handoff`, `status`, `events`, `task` (submit/resume), `interrupt`, `artifacts`, `close`
- Host directory MCP tools: `rdev.hosts.list`, `rdev.hosts.rename`
- Typed engineering tasks: adapter profiles (shell, powershell, codex, claude-code, acpx, coder, devpod, rustdesk, desktop, file), git-worktree isolation, bounded limits, network policy, verification commands
- Agent skills in `skills/`, MCP contracts in `internal/contracts` and `mcp/tools.json`

## Quality Bar

Quality is a release gate, not a goal. Every change must keep the full gate green:

- CI (`scripts/check.sh`): gofmt, `go test ./...`, `go vet ./...`, per-package coverage floors (`scripts/check-coverage.sh`), shell syntax, public-surface and skill audits, release smoke.
- Security-critical packages (auth, trust, policy, audit, contracts, update, protected store, HTTP API, host identity) are coverage-gated; regressions fail CI.
- Corner-case coverage is tracked in `docs/development/QUALITY_MATRIX.md`; new surfaces must extend it before merge.
- Live E2E on real hosts (managed install, reboot, sleep/wake, lock screen, tunnel rotation) is required before reporting managed-host features complete.
- Every networked or privileged action ships with policy and audit design before implementation.

## MVP Contract

Keep the Phase 1 MVP small and verifiable: sessions that are policy-bound, scoped, auditable, interruptible, and non-persistent by default. Do not add hidden persistence, inbound public listeners, unrestricted shell access, or bypasses of local OS security controls.
