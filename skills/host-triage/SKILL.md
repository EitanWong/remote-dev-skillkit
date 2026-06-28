---
name: host-triage
description: Use when an agent needs to inspect a target host's OS, architecture, installed tools, permissions, network state, or readiness for remote development.
---

# Host Triage

Use this skill before making changes on a target host.

## Triage Checklist

- OS, version, architecture.
- User identity and admin/elevation status.
- Shell and PowerShell availability.
- Git, SSH, package manager, Codex, Claude, acpx.
- Network reachability to `agent.lunflux.com:443`.
- Workspace boundaries.
- Existing security tools or enterprise restrictions.

## Output

Return a short readiness report with:

- detected capabilities;
- missing dependencies;
- safe next action;
- actions requiring approval.
