# Adapter SDK

The adapter SDK starts with three stable rules:

1. every adapter result must be independently checkable as evidence;
2. every adapter must describe how it passes through the host kernel lifecycle
   before it is exposed to agents;
3. cancellation must be distinguishable from timeout and ordinary failure.

`pkg/adapterkit` provides the first public contracts for those rules.

## Runtime Lifecycle Runner

Use `adapterkit.RunLifecycle` when building or testing a runtime adapter. It
executes the public lifecycle in order:

```text
detect -> plan -> prepare -> run -> collect -> cleanup
```

The runner returns `rdev.adapter-runtime-fixture.v1`, a machine-readable record
of phase order, phase evidence, timing, cancellation/timeout state, cleanup, and
the collected result artifact.

```go
fixture, err := adapterkit.RunLifecycle(ctx, adapter, adapterkit.RuntimeRequest{
    Adapter:       "my-adapter",
    TaskID:        "task_123",
    WorkspaceRoot: "/repo",
    Intent:        "run adapter acceptance",
})
content, _ := fixture.JSON()
report := adapterkit.VerifyRuntimeFixtureJSON(content, adapterkit.RuntimeFixtureContract{
    Adapter:               "my-adapter",
    RequireSuccessful:     true,
    RequireCleanup:        true,
    RequireResultArtifact: true,
})
if err != nil || !report.OK {
    t.Fatalf("runtime adapter failed lifecycle: err=%v report=%#v", err, report)
}
```

If `run` or another post-prepare phase fails with `context.Canceled`, the
fixture records `canceled=true`, `timed_out=false`, and still runs `cleanup`
with an independent cleanup context. This lets adapter authors prove
cleanup-on-cancel behavior without relying on narration.

Agents and CI can verify a saved fixture through the CLI:

```bash
rdev adapter verify-runtime \
  --artifact adapter-runtime-fixture.json \
  --adapter claude-code \
  --require-result-artifact
```

MCP clients can call `rdev.adapter.verify_runtime` with `artifact_json` or
`artifact_id`. The verifier returns `rdev.adapter-conformance-report.v1`.

Built-in hostrunner adapters can also emit runtime fixtures in real host tasks.
Run `rdev host serve` with `--capture-runtime-fixture` to keep the normal
adapter result as the primary task artifact and append a second
`rdev.adapter-runtime-fixture.v1` artifact for shell, PowerShell, Codex, or
Claude Code tasks:

```bash
rdev host serve \
  --mode managed \
  --gateway https://agent.example.com \
  --once=false \
  --transport long-poll \
  --workspace-lock-store .rdev/workspace-locks \
  --capture-runtime-fixture
```

The runtime fixture is opt-in so existing evidence consumers keep receiving the
same top-level adapter result artifact. When enabled, the fixture records
hostrunner phases, cleanup, cancellation/timeout state, and the embedded
adapter result artifact. This is the first hostrunner-integrated runtime
fixture path for built-in adapters.

Built-in adapters currently covered by hostrunner runtime fixtures:

| Adapter | Result schema | Primary capability |
|---|---|---|
| `shell` | `rdev.shell-result.v1` | `shell.user` |
| `powershell` | `rdev.powershell-result.v1` | `powershell.user` |
| `codex` | `rdev.codex-result.v1` | `codex.run` + `git.diff` |
| `claude-code` | `rdev.claude-code-result.v1` | `claude-code.run` + `git.diff` |
| `acpx` | `rdev.acpx-result.v1` | `acpx.run` + `git.diff` |

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
- `plan` declares external consequences and required authorizations;
- `prepare` enforces workspace boundaries and workspace locks;
- `run` supports timeout and, when required, cancellation;
- `collect` emits a result artifact and result schema;
- `cleanup` is idempotent and releases locks;
- safety declarations state that the adapter does not authorize tasks, self-
  authorize dangerous actions, or install hidden persistence;
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

rdev adapter verify-result \
  --artifact claude-code-result.json \
  --adapter claude-code \
  --schema rdev.claude-code-result.v1 \
  --command-fields claude_code_command,git_status,git_diff_stat,git_diff
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

## Cancellation Artifact Conformance

Use `adapterkit.VerifyCancellationArtifactJSON` when testing a canceled task or
before accepting cancellation evidence from a new adapter.

```go
report := adapterkit.VerifyCancellationArtifactJSON(content, adapterkit.CancellationContract{
    Adapter:                 "my-adapter",
    SchemaVersion:           "rdev.my-adapter-result.v1",
    CommandFields:           []string{"my_command"},
    RequiredStringFields:    []string{"workspace_root"},
    RequireTiming:           true,
    RequireRedaction:        true,
    RejectUnredactedSecrets: true,
})
if !report.OK {
    t.Fatalf("adapter cancellation failed conformance: %#v", report)
}
```

The cancellation verifier first runs normal result-artifact conformance, then
adds cancellation-specific checks for each command evidence object:

- the command evidence object exists;
- `canceled` is present and true;
- `timed_out` is present and false;
- `exit_code` is present;
- `output_truncated` is present;
- redaction, timing, schema, adapter, required fields, and secret-pattern checks
  still pass when requested.

Top-level command adapters such as shell and PowerShell use the default command
field:

```bash
rdev adapter verify-cancellation \
  --artifact shell-result.json \
  --adapter shell \
  --schema rdev.shell-result.v1
```

Nested command adapters such as Codex pass the nested command evidence field:

```bash
rdev adapter verify-cancellation \
  --artifact codex-result.json \
  --adapter codex \
  --schema rdev.codex-result.v1 \
  --command-fields codex_command
```

Agent runtimes can call MCP tool `rdev.adapter.verify_cancellation` with
`artifact_json` or `artifact_id`. The tool returns
`rdev.adapter-conformance-report.v1` just like the result and lifecycle
verifiers.

## Current Scope

The current runtime lifecycle runner plus hostrunner opt-in capture are still
not the complete production Adapter SDK. Built-in shell, PowerShell, Codex,
Claude Code, and acpx now have hostrunner-integrated runtime fixtures, but new
adapter authors still need production wrappers for policy planning,
workspace/session preparation, adapter-specific execution, and shared runtime
cancellation fixtures. The current package provides shared runtime fixture
generation plus conformance layers for lifecycle declarations, runtime
fixtures, result evidence, and cancellation evidence.
