---
name: remote-session-review
description: Use when an agent needs to review session task status, events, artifacts, diffs, tests, interrupts, or residual risk before declaring Remote Dev Skillkit work complete.
---

# Remote Session Review

## Review checklist

- Session identifier, task identifier, gateway source, and endpoint are known.
- Task reached a terminal state.
- Relevant session events were inspected.
- Artifacts and their metadata were reviewed.
- Changed files, commands, exit codes, and requested tests are recorded.
- No pending interrupt remains.
- Residual risks and follow-up work are explicit.
- Session closure state matches the operator decision.

## Adaptive configuration

Before acting, probe the current gateway, network reachability, tunnel and mesh
availability, workspace, and host environment. If a required value is unclear,
ask a concise question; never invent a configuration value.

## Completion format

Report: `task_state`, `what_changed`, `verification_evidence`, `artifacts_reviewed`, `remaining_risks`, and `next_action`.
