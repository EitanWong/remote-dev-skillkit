---
name: remote-job-review
description: Use when an agent needs to review remote job output, audit records, artifacts, diffs, tests, and approval history before declaring a task complete.
---

# Remote Job Review

Use this skill before telling the user a remote job is complete.

## Review Checklist

- Evidence source is known: job id, gateway, bundle path, artifact ids, and
  audit source are explicit or discovered from the current context.
- Before declaring completion, probe read-only context such as `rdev doctor`,
  `rdev mcp tools`, job status, artifact listings, audit exports, workspace
  path, framework install path, and verifier availability when those details are
  not already explicit.
- If job id, gateway, workspace, evidence bundle path, artifact location,
  adapter choice, approval policy, framework install path, or expected verifier
  is unclear, ask before declaring completion.
- Do not invent gateway URLs, paths, ticket codes, root keys, release URLs,
  checksums, workspace roots, adapter choices, or approval policies from
  examples or placeholders.
- Job reached a terminal state.
- No pending approval requests remain.
- Commands and exit codes are recorded.
- Files changed are listed.
- Tests or verification commands are recorded.
- Artifacts were read or summarized.
- Dangerous actions have approval records.
- Residual risks are stated.
- Host/ticket revocation status is known or a follow-up revocation action is
  recommended.

## Completion Format

Report:

- what changed;
- verification evidence;
- approvals used;
- remaining risks;
- whether host/ticket should be revoked.
