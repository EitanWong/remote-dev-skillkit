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
- Treat local examples such as `https://api.example.com/v1`, `/Users/example`,
  `/home/example`, and `C:\Users\Alice` as placeholders, not deployment facts.

## Output

Return a short readiness report with:

- detected capabilities;
- missing dependencies;
- safe next action;
- actions requiring approval.
