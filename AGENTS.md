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

## Current MVP Surface

- `rdev doctor`
- `rdev mcp tools`
- `rdev host serve --mode temporary`
- Agent Skills in `skills/`
- MCP contracts in `internal/contracts` and `mcp/tools.json`
