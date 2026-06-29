# Adapter SDK

The adapter SDK starts with one stable rule: every adapter result must be
independently checkable as evidence. `pkg/adapterkit` provides the first public
contract for that rule.

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

This is not the full lifecycle SDK yet. Adapter authors still implement their
own detect, plan, prepare, run, collect, and cleanup phases behind the host
runner and policy engine. The current package is the shared conformance layer
for result evidence, and the built-in shell, PowerShell, and Codex tests use it
as fixtures for future third-party adapters.
