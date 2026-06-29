# Adapter SDK

The adapter SDK starts with two stable rules:

1. every adapter result must be independently checkable as evidence;
2. every adapter must describe how it passes through the host kernel lifecycle
   before it is exposed to agents.

`pkg/adapterkit` provides the first public contracts for those rules.

## Lifecycle Manifest Conformance

Use `adapterkit.VerifyLifecycleManifestJSON` before treating a new adapter as a
candidate for hostrunner integration.

```go
report := adapterkit.VerifyLifecycleManifestJSON(content, adapterkit.LifecycleContract{
    Adapter:                 "my-adapter",
    RequireSafety:           true,
    RequireCancellation:     true,
    RequireResultSchema:     true,
    RejectUnredactedSecrets: true,
})
if !report.OK {
    t.Fatalf("adapter lifecycle failed conformance: %#v", report)
}
```

The lifecycle manifest uses schema `rdev.adapter-lifecycle.v1` and describes the
six required phases:

```text
detect -> plan -> prepare -> run -> collect -> cleanup
```

The verifier checks that:

- every required phase is present, implemented, and has evidence declarations;
- `plan` declares external consequences and required approvals;
- `prepare` enforces workspace boundaries and workspace locks;
- `run` supports timeout and, when required, cancellation;
- `collect` emits a result artifact and result schema;
- `cleanup` is idempotent and releases locks;
- safety declarations state that the adapter does not authorize jobs, self-
  approve dangerous actions, or install hidden persistence;
- cancellation declarations include a canceled evidence field, timeout
  exclusivity, and cleanup-on-cancel behavior;
- common unredacted secret patterns are absent when requested.

Adapter authors can start from a generated manifest and then run the same check
through the CLI:

```bash
rdev adapter scaffold \
  --adapter claude-code \
  --out examples/adapters/claude-code-lifecycle.json

rdev adapter verify-lifecycle \
  --artifact examples/adapters/claude-code-lifecycle.json \
  --adapter claude-code
```

`scaffold` writes a copyable `rdev.adapter-lifecycle.v1` template, derives the
default result schema as `rdev.<adapter>-result.v1`, refuses to overwrite
existing files unless `--force` is passed, and immediately verifies the
generated manifest before returning `ok=true`.

Agent runtimes can call MCP tool `rdev.adapter.verify_lifecycle` with
`artifact_json` or `artifact_id`.

## Result Artifact Conformance

Use `adapterkit.VerifyResultArtifactJSON` in adapter tests before treating a new
adapter as compatible with the host safety kernel.

```go
report := adapterkit.VerifyResultArtifactJSON(content, adapterkit.ResultArtifactContract{
    Adapter:                 "my-adapter",
    SchemaVersion:           "rdev.my-adapter-result.v1",
    RequiredStringFields:    []string{"workspace_root"},
    RequireTiming:           true,
    RequireRedaction:        true,
    RejectUnredactedSecrets: true,
})
if !report.OK {
    t.Fatalf("adapter artifact failed conformance: %#v", report)
}
```

Agents and adapter authors can also run the same contract through the CLI:

```bash
rdev adapter verify-result \
  --artifact shell-result.json \
  --adapter shell \
  --schema rdev.shell-result.v1

rdev adapter verify-result \
  --artifact codex-result.json \
  --adapter codex \
  --schema rdev.codex-result.v1 \
  --command-fields codex_command,git_status,git_diff_stat,git_diff
```

The CLI prints `rdev.adapter-conformance-report.v1`. When `ok=false`, it prints
the structured report before returning a nonzero exit code.

Agent runtimes can call the same verifier through MCP tool
`rdev.adapter.verify_result`. Pass either `artifact_json` directly or
`artifact_id` for an artifact stored in the current gateway:

```json
{
  "adapter": "shell",
  "schema": "rdev.shell-result.v1",
  "artifact_json": "{\"schema_version\":\"rdev.shell-result.v1\",...}"
}
```

Top-level command artifacts, such as shell and PowerShell, use the default
command contract. Nested command artifacts, such as Codex, set `CommandFields`
to the command evidence objects that must be present.

The verifier checks:

- JSON validity.
- adapter and schema version.
- required string fields such as `workspace_root`.
- timing fields: `started_at`, `ended_at`, and `duration_millis`.
- redaction metadata: `redacted`, `redaction_rules`, and optional
  `redaction_counts`.
- command evidence: `exit_code`, `timed_out`, `canceled`, and
  `output_truncated`.
- cancellation and timeout are not both true for the same command.
- common unredacted secret patterns are absent when requested.

## Current Scope

Lifecycle manifest conformance is not the full runtime Adapter SDK yet. Adapter
authors still implement their detect, plan, prepare, run, collect, and cleanup
phases behind the host runner and policy engine. The current package is the
shared conformance layer for lifecycle declarations and result evidence, and the
built-in shell, PowerShell, and Codex tests use the result checks as fixtures
for future third-party adapters.
