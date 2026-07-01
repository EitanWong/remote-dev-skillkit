---
name: remote-job-review
description: Use when an agent needs to review remote job status, output, audit records, artifacts, diffs, tests, runtime-memory updates, approvals, revocation state, or residual risk before declaring Remote Dev Skillkit work complete.
---

# Remote Job Review

Use this skill before telling the user a remote job is complete.

## Review Checklist

- Evidence source is known: job id, gateway, bundle path, artifact ids, and
  audit source are explicit or discovered from the current context.
- Runtime memory used by the job is identified, and any newly discovered safe
  facts are written back only after evidence review.
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
- Keep path and configuration neutral. Evidence paths, workspace roots,
  artifact locations, verifier paths, framework directories, and gateway URLs
  must come from job metadata, current context, MCP/CLI output, manifest
  metadata, or explicit human/operator confirmation.
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
- Runtime memory updates are scoped, redacted, and backed by artifact or audit
  evidence; stale or conflicting memory is invalidated.

## Completion Format

Report with stable field names:

- `job_state`;
- `what_changed`;
- `verification_evidence`;
- `approvals_used`;
- `artifacts_reviewed`;
- `audit_refs`;
- `memory_updates`;
- `remaining_risks`;
- `revocation_or_cleanup`;
- `final_recommendation`.
