# Adapter Jobs

Read this only when selecting, running, verifying, or extending adapters.

## Adapter Selection

- Treat Codex, Claude Code, ACP/acpx, shell, and PowerShell as adapters behind
  the signed-job/evidence/approval contract.
- Prefer ACP/acpx over raw PTY scraping when available.
- Use shell or PowerShell only when the host advertises the required capability
  and the signed policy allows the command.
- For PowerShell, require `powershell.user`, allow only configured commands, and
  do not ask the adapter to bypass execution policy.

## Built-In Job Expectations

- Codex jobs require `codex.run` and `git.diff`; expect
  `rdev.codex-result.v1` evidence with Git status, diff/stat, and verification
  command output.
- Claude Code jobs require `claude-code.run` and `git.diff`; expect
  `rdev.claude-code-result.v1` evidence with output, Git status, diff/stat, and
  verification command output.
- ACP/acpx jobs require `acpx.run` and `git.diff`; expect
  `rdev.acpx-result.v1` evidence with output, Git status, diff/stat, and
  verification command output.
- Prefer `go test -json` when verifying Go projects so artifacts can include
  `rdev.test-report.v1` summaries.

## Approval and Cancellation

- Package installation, elevation, GUI control, service management, push, merge,
  deploy, publish, credential changes, and execution-policy changes must return
  or consume an approval gate before execution.
- When canceling a running adapter job, expect cooperative local process
  cancellation, the gateway job to remain `canceled`, and cancellation evidence
  when available.

## Adapter Extensions

- For new adapters, start with `rdev adapter scaffold`.
- Implement runtime lifecycle through `adapterkit.RunLifecycle`.
- Verify lifecycle, runtime, result, and cancellation evidence through the MCP
  adapter verification tools, matching CLI commands, or `pkg/adapterkit`
  conformance before exposing the adapter to agents.
- For runtime fixture or release-evidence runs, prefer hosts started with
  `--capture-runtime-fixture` so built-in adapters append
  `rdev.adapter-runtime-fixture.v1` evidence.
