---
name: host-triage
description: Use when an agent needs to inspect a target host's OS, architecture, installed tools, permissions, network state, or readiness for remote development.
---

# Host Triage

Use this skill before making changes on a target host.

## Triage Checklist

- If the target OS, shell, gateway, join URL, workspace path, service manager,
  installed agent framework, or permission level is unclear, inspect the
  environment first and ask a concise follow-up before choosing commands.
- OS, version, architecture.
- User identity and admin/elevation status.
- Shell and PowerShell availability.
- Git, SSH, package manager, Codex, Claude, acpx.
- Network reachability to the configured Remote Dev Skillkit gateway or join URL.
- Workspace boundaries.
- Existing security tools or enterprise restrictions.
- Installed service managers or launch surfaces: LaunchAgent/launchd, systemd
  user units, Windows Service Control Manager, scheduled tasks, or foreground
  shell only.
- Existing `rdev` binary, Skillkit files, MCP configuration, and relevant
  environment variables.

## Adaptive Probes

- Prefer read-only probes such as `rdev doctor`, `rdev mcp tools`, `uname -a`,
  `sw_vers`, `ver`, `id`, `whoami`, `command -v`, `where`, `git rev-parse`,
  and directory existence checks.
- Do not invent a gateway URL, ticket code, root key, release URL, user home
  path, or framework install path. If it cannot be discovered safely, ask.
- Keep path and configuration neutral. Do not assume a fixed checkout path,
  user home, temp directory, workspace root, framework install directory,
  gateway URL, or release artifact location. Resolve them from read-only
  probes, active configuration, MCP/CLI output, manifest metadata, or explicit
  human/operator confirmation.
- Treat example domains, POSIX paths, Windows paths, and placeholder values as
  documentation only, not deployment facts or defaults.

## Output

Return a short readiness report with:

- detected capabilities;
- missing dependencies;
- safe next action;
- actions requiring approval.
