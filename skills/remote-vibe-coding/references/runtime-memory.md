# Runtime Memory

Read this only when loading, updating, pruning, exporting, or reasoning from
Skill runtime memory.

## Contents

- [Purpose](#purpose)
- [Storage Rules](#storage-rules)
- [Record Shape](#record-shape)
- [Read Before Acting](#read-before-acting)
- [Write After Discovering](#write-after-discovering)
- [Refresh and Invalidate](#refresh-and-invalidate)
- [Evidence and Privacy](#evidence-and-privacy)

## Purpose

Skill runtime memory stores reusable facts discovered during real sessions so
the Agent does not repeatedly ask for or probe the same environment details. It
is dynamic runtime state, not part of the public Skill source.

Use it for:

- detected OS, shell, service manager, package manager, locale, proxy, and
  network constraints;
- resolved gateway, join, manifest, workspace, release, verifier, framework,
  and adapter paths;
- host capabilities, approval policy, enrolled host identity hints, and
  managed-service status;
- project structure summaries, safe probe results, tool versions, and
  dependency availability;
- operator preferences that affect future safe choices.

## Storage Rules

- Store memory on the target host or in the operator-configured local state
  store, never in the open-source repository.
- Resolve the memory root from the active Skillkit manifest, host context plan,
  configured policy, environment probes, MCP/CLI output, or explicit
  human/operator confirmation.
- Keep memory scoped by host identity, workspace, gateway, and project when
  those scopes are known. Do not reuse one customer's memory for another
  customer.
- Default to user/workspace-scoped storage. Elevation, service-wide memory,
  shared fleet memory, hosted sync, or external storage requires approval and
  an audit record.
- Do not store secrets, private keys, bearer tokens, operator tokens, raw
  credentials, unredacted transcripts, private hostnames, customer personal
  data, or full local filesystem inventories.

## Record Shape

Use records compatible with `rdev.skill-runtime-memory.v1`:

```json
{
  "schema": "rdev.skill-runtime-memory.v1",
  "scope": {
    "host_id_hash": "sha256:...",
    "workspace_id_hash": "sha256:...",
    "gateway_id_hash": "sha256:..."
  },
  "facts": [
    {
      "key": "workspace.root",
      "value_ref": "redacted-or-host-local-reference",
      "source": "probe|manifest|mcp|operator|job-artifact",
      "confidence": "confirmed|observed|inferred",
      "sensitivity": "public|internal|sensitive",
      "observed_at": "RFC3339",
      "expires_at": "RFC3339-or-empty",
      "evidence_ref": "artifact-or-audit-id"
    }
  ]
}
```

Store real sensitive values as host-local references when possible. If a raw
path is necessary, scope it narrowly and redact it from public evidence.

## Read Before Acting

Before probing or asking the human, check runtime memory for:

- gateway URL and join/manifest sources;
- workspace root, repo root, branch/worktree policy, and lock store;
- adapter availability and preferred adapter;
- framework install paths and user/workspace tool locations;
- release root, verifier, bundle, and checksum locations;
- proxy, TLS interception, LAN, relay, mesh, SSH, or VPN requirements;
- prior approvals, denials, residual risks, and revocation status.

Memory is a hint, not authority. Re-verify stale, sensitive, or high-impact
facts before use.

## Write After Discovering

Update runtime memory after:

- successful host triage or environment probing;
- invite creation and host approval;
- workspace discovery or worktree preparation;
- adapter/tool/runtime detection;
- dependency or Skill provisioning;
- release/verification input discovery;
- job completion, denial, approval-required pause, cancellation, or revocation.

Write only the smallest durable fact that will help the next run. Prefer
structured facts and artifact references over raw logs.

## Refresh and Invalidate

- Refresh facts when tool versions, Git state, network state, service status,
  trust roots, enrollment certificates, revocations, approvals, or release
  artifacts may have changed.
- Treat inferred facts as short-lived. Treat confirmed facts as reusable only
  within their scope.
- Invalidate memory after host revocation, ticket revocation, workspace
  deletion, repo move, gateway rotation, root key rotation, credential rotation,
  customer handoff completion, or operator request.
- If memory conflicts with live probes, trust live probes and record the
  correction.

## Evidence and Privacy

- Every memory write should be explainable by a probe, manifest, MCP result,
  job artifact, audit record, or operator confirmation.
- Public reports should cite memory keys and evidence ids, not sensitive raw
  values.
- Memory export must redact sensitive values and preserve enough metadata to
  explain what was used, when it was observed, and why it was trusted.
