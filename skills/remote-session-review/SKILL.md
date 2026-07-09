---
name: remote-session-review
description: Use when an agent needs to review remote session task status, output, audit records, artifacts, diffs, tests, runtime-memory updates, interrupts, revocation state, or residual risk before declaring Remote Dev Skillkit work complete.
---

# Remote Session Review

Use this skill before telling the user a remote session task is complete.

## Review Checklist

- Evidence source is known: session id, task id, gateway, bundle path, artifact
  ids, and audit source are explicit or discovered from the current context.
- Runtime memory used by the task is identified, and any newly discovered safe
  facts are written back only after evidence review.
- Before declaring completion, probe read-only context such as `rdev doctor`,
  `rdev mcp tools`, session status, task status, artifact listings, audit
  exports, workspace path, framework install path, network reachability,
  tunnel/mesh selection, and verifier availability when those details are not
  already explicit.
- If session id, task id, gateway, workspace, evidence bundle path, artifact
  location, adapter choice, tunnel/mesh authorization, interrupt policy,
  framework install
  path, or expected verifier is unclear, ask before declaring completion.
- Do not invent gateway URLs, paths, ticket codes, root keys, release URLs,
  checksums, workspace roots, adapter choices, or interrupt policies from
  examples or placeholders.
- Keep path and configuration neutral. Evidence paths, workspace roots,
  artifact locations, verifier paths, framework directories, and gateway URLs
  must come from task metadata, current context, MCP/CLI output, manifest
  metadata, or explicit human/operator confirmation.
- Treat relay, tunnel, mesh, VPN, SSH, proxy, and DNS facts as evidence-backed
  connectivity context, not assumed defaults.
- Task reached a terminal state.
- No pending interrupt requests remain.
- Commands and exit codes are recorded.
- Files changed are listed.
- Tests or verification commands are recorded.
- Artifacts were read or summarized.
- Dangerous actions have interrupt or authorization records.
- Residual risks are stated.
- Host/ticket revocation status is known or a follow-up revocation action is
  recommended.
- Runtime memory updates are scoped, redacted, and backed by artifact or audit
  evidence; stale or conflicting memory is invalidated.

## Completion Format

Report with stable field names:

- `task_state`;
- `what_changed`;
- `verification_evidence`;
- `interrupts_or_authorizations`;
- `artifacts_reviewed`;
- `audit_refs`;
- `memory_updates`;
- `remaining_risks`;
- `revocation_or_cleanup`;
- `final_recommendation`.
