# Release and Acceptance

Read this only for release candidates, Skillkit distribution, GitHub release
planning, platform candidates, or OS acceptance evidence.

## Release Readiness

- For release-surface changes, expect the project check script and release
  smoke script to pass locally and in GitHub Actions before claiming readiness.
- For installed agents or managed hosts, use `rdev update check` and
  `rdev update plan` to discover newer GitHub Releases and produce reviewable
  update steps. Treat update plans as dry-run evidence until an operator
  authorizes the upgrade.
- Before publishing artifacts or bootstrap download instructions, create and
  verify a signed release bundle using paths and root keys from the current
  release plan, not examples.
- Build artifacts through the project-provided release script resolved from the
  current project root or Skillkit manifest.
- Review the generated build manifest, SBOM, provenance, and checksums before
  preparing candidates.
- Treat failed candidate verification as release-blocking.
- Before replacing binaries or restarting managed services, verify the selected
  archive checksum and signed `release-bundle.json` with the configured release
  root, keep rollback artifacts, then run `rdev version` and `rdev doctor`.

## Skillkit Distribution

- For agent-framework distribution, run export, verify, plan-install,
  verify-install-plan, and install dry-run before telling users to install into
  Codex, Claude Code, Hermes, OpenClaw/OpenCode, or a generic MCP agent.
- Review generated install scripts before execution.
- Require an explicit execute flag before local copying.
- Keep external mutation false unless the operator explicitly authorizes a
  publishing or remote mutation action.

## GitHub Release Planning

- Use project-provided release-planning and readiness-audit scripts resolved
  from the current project root or Skillkit manifest.
- Use the current candidate path and operator-confirmed repository id.
- Do not run generated `gh release` commands, create repositories, mutate
  labels, mutate milestones, create issues, upload artifacts, or push tags
  without explicit operator authorization.

## OS Acceptance

- Managed Mac acceptance requires operator-confirmed output directory and repo
  path; review evidence and interrupt-evidence directories from the current run.
- Service-backed Mac acceptance must generate and review a plan first; it must
  not auto-run `launchctl`.
- Linux managed service work must use reviewed systemd user-unit plans with
  release-bundle gates and must prove start/status/reconnect/task/stop/uninstall
  evidence before release claims.
- Windows managed service work must use reviewed Service Control Manager plans
  with release-bundle gates and must prove create/status/start/reconnect/stop/
  uninstall evidence before release claims.
- Windows temporary support must verify the emitted acceptance plan before a
  one-command bootstrap is sent and must package real-run evidence before a
  release-ready claim.
