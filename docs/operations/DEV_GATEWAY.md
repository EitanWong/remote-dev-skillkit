# Development Gateway

`rdev gateway serve --dev` starts a local HTTP gateway backed by the same in-memory state machine used by `rdev mcp serve`.

This is a development surface only. It is not production transport, does not authenticate requests, and binds to `127.0.0.1` by default.

## Start

```bash
rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --audit-log .rdev/audit/events.jsonl \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --manifest-signing-key .rdev/keys/manifest-root-key.json
```

When `--signing-key` is set, the dev gateway creates or reuses an Ed25519 signing key file with `0600` permissions and prints its public-key fingerprint:

```text
rdev gateway signing key id=gateway-dev fingerprint=sha256:<hex>
rdev gateway manifest root id=manifest-dev public_key=manifest-dev:<base64url_ed25519_public_key>
```

Hosts can pin that key during local development:

```bash
rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --identity-store .rdev/host/identity.json \
  --nonce-store .rdev/host/nonces.json \
  --trust-store .rdev/host/trust-bundle.json \
  --trust-pin sha256:<hex>
```

Or they can consume the signed join manifest, which carries the ticket code, gateway URL, trust bundle and trust fingerprint:

```bash
rdev host serve \
  --mode temporary \
  --manifest-url http://127.0.0.1:8787/v1/tickets/<ticket_code>/manifest \
  --manifest-root-public-key manifest-dev:<base64url_ed25519_public_key> \
  --once=false
```

## Endpoints

- `GET /healthz`
- `GET /v1/trust`
- `GET /v1/trust-bundle`
- `POST /v1/trust-bundle`
- `POST /v1/tickets`
- `GET /v1/tickets/{ticket_code}/manifest`
- `GET /v1/hosts`
- `POST /v1/hosts/register`
- `POST /v1/hosts/{host_id}/approve`
- `POST /v1/hosts/{host_id}/revoke`
- `GET /v1/hosts/{host_id}/trust-bundle/update?current_sequence=<n>&current_hash=<sha256:...>`
- `POST /v1/jobs`
- `GET /v1/jobs/{job_id}`
- `GET /v1/jobs/{job_id}/artifacts`
- `GET /v1/jobs/{job_id}/evidence-bundle?out=<directory>`
- `GET /v1/hosts/{host_id}/jobs/next`
- `GET /v1/hosts/{host_id}/jobs/next?wait_seconds=<1-60>`
- `POST /v1/jobs/{job_id}/complete`
- `POST /v1/jobs/{job_id}/fail`
- `GET /v1/artifacts/{artifact_id}`
- `GET /v1/audit`

## Example

```bash
curl -s http://127.0.0.1:8787/healthz

curl -s -X POST http://127.0.0.1:8787/v1/tickets \
  -H 'content-type: application/json' \
  -d '{"mode":"attended-temporary","ttl_seconds":600,"reason":"local dev"}'

curl -s http://127.0.0.1:8787/v1/tickets/<ticket_code>/manifest
curl -s http://127.0.0.1:8787/v1/audit
```

Export and verify an audit hash chain from a JSONL audit log:

```bash
rdev audit export \
  --input .rdev/audit/events.jsonl \
  --out .rdev/audit/audit-chain.json

rdev audit verify \
  --input .rdev/audit/audit-chain.json
```

The exported file uses schema `rdev.audit-chain.v1`. Each entry stores the canonical event hash, previous chain hash, and current chain hash. Verification recomputes every event hash and the final root hash, rejecting tampered events or broken chain links.

Read or update the development signed trust bundle:

```bash
curl -s http://127.0.0.1:8787/v1/trust-bundle
curl -s -X POST http://127.0.0.1:8787/v1/trust-bundle \
  -H 'content-type: application/json' \
  -d '{"trust_bundle":{...}}'
```

Trust bundle updates must use schema `rdev.trust-bundle.v1`, verify against the current active signing key, increase the sequence, and include the current bundle hash as `previous_bundle_hash`.

Managed-host trust refresh can use the host-bound update check:

```bash
curl -s 'http://127.0.0.1:8787/v1/hosts/<host_id>/trust-bundle/update?current_sequence=1&current_hash=sha256:...'
```

The response uses schema `rdev.trust-bundle-update.v1`. If the host is current, the response status is `current` and omits a bundle. If the gateway has a newer bundle, the response status is `update_available` and includes the candidate `rdev.trust-bundle.v1`; the host still verifies sequence, `previous_bundle_hash`, signature, and validity locally before persisting it.

Register a foreground temporary host:

```bash
rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --identity-store .rdev/host/identity.json
```

Create and process one dev job:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/jobs \
  -H 'content-type: application/json' \
  -d '{"host_id":"hst_...","adapter":"shell","intent":"local demo","policy":{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"],"max_duration_seconds":30,"max_output_bytes":65536}}'

rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --transport long-poll \
  --max-jobs=1 \
  --approval-timeout=30s
```

Preflight the same shell job policy before creating it:

```bash
rdev policy explain-shell \
  --policy-json '{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"],"max_duration_seconds":30,"max_output_bytes":65536}'
```

Agents can call the same policy engine through MCP tool `rdev.policy.explain_shell`.

`rdev host serve` generates an Ed25519 host identity for the session. When `--identity-store <path>` is set, the identity is persisted to a local `0600` JSON file using schema `rdev.host-identity.v1`; the parent directory is created with `0700` permissions. The host registration includes the identity key id, public key and fingerprint. Signed job envelopes include the registered host identity fingerprint, and a host with a local identity rejects jobs bound to a different fingerprint.

`rdev host serve` keeps a nonce replay cache for signed job envelopes. By default, this cache is in-memory for the process. When `--nonce-store <path>` is set, used nonces are persisted to a local `0600` JSON file using schema `rdev.host-nonce-store.v1`; the parent directory is created with `0700` permissions. Expired nonce entries are pruned before new entries are written.

When `--once=false`, `rdev host serve` fetches the signed gateway trust bundle from `GET /v1/trust-bundle`, waits until the registered host is approved, refreshes the local trust store through `GET /v1/hosts/{host_id}/trust-bundle/update` when `--trust-store` is configured, polls `GET /v1/hosts/{host_id}/jobs/next`, verifies the signed job envelope against the active key in the trust bundle, the local host identity fingerprint, and the nonce replay cache, runs the development host runner, and reports completion to `POST /v1/jobs/{job_id}/complete`. For older dev gateways, the host falls back to legacy `GET /v1/trust`.

For a more realistic outbound host channel during local development, use HTTPS long-poll:

```bash
rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --transport long-poll \
  --long-poll-timeout=25s
```

Long-poll uses the same safety path as short polling but holds the host's outbound `GET /v1/hosts/{host_id}/jobs/next?wait_seconds=<n>` request open until a job is available or the wait timeout elapses. This is a development bridge toward WSS/mTLS transport; it is not a production transport by itself.

When `--trust-store <path>` is set, the host persists verified signed trust bundles to a local `0600` JSON file. Future updates must verify against the stored bundle's active signing key, increase sequence, and match `previous_bundle_hash`. If the gateway trust-bundle endpoint is unavailable, the host can use the stored bundle for job verification.

If the host runner rejects a job, the host reports the failure to `POST /v1/jobs/{job_id}/fail`. The gateway marks the job `failed`, stores `failure_reason`, and writes a `job.fail` audit event.

The development shell adapter executes `policy.argv` directly without shell interpolation. The first argv item must match `policy.allow_commands`, the workspace root must exist, write scopes must remain inside the workspace, and output is capped by the signed envelope limit. Before execution, the host also runs the shared implicit approval preflight. Shell jobs that request package installation, elevation, GUI control, service management, push, merge, deploy, publish, or credential changes return `rdev.approval-required.v1` unless a matching signed approval token is present. Completion and failure artifacts use schema `rdev.shell-result.v1` and include argv, canonical workspace, exit code, redacted stdout/stderr excerpts, timeout state, truncation state, duration, redaction rules, and redaction counts.

The development Codex adapter uses the same host safety path. Create an `adapter=codex` job with `codex.run` and `git.diff` capabilities:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/jobs \
  -H 'content-type: application/json' \
  -d '{
    "host_id": "hst_...",
    "adapter": "codex",
    "intent": "update README",
    "policy": {
      "workspace_root": ".",
      "capabilities": ["codex.run", "git.diff"],
      "prompt": "Update README and keep the change scoped to this repository.",
      "verification_commands": [["git", "status", "--short"]],
      "allow_verification_commands": ["git"],
      "max_duration_seconds": 1800,
      "max_output_bytes": 1048576
    }
  }'
```

By default the adapter runs:

```bash
codex exec -C <workspace_root> --sandbox workspace-write --json <prompt>
```

For deterministic local tests, signed job payloads may override `codex_command` and `codex_args`. The result artifact uses schema `rdev.codex-result.v1` and includes Codex command output, Git status, Git diff/stat, optional verification command results, output truncation flags, duration, redaction rules, and redaction counts. Verification commands must be allowlisted through `allow_verification_commands`. When a verification command is `go test -json ...`, the command result also includes a `rdev.test-report.v1` summary with package/test pass, fail, skip counts and parsed test cases.

Codex jobs also run the same implicit approval preflight before workspace lock acquisition and before adapter execution. If `intent`, `prompt`, `codex_args`, `verification_commands`, `external_actions`, `dangerous_actions`, `approval_actions`, or `requested_approvals` request high-risk external consequences, the host returns `rdev.approval-required.v1` unless a matching approval token is present. Current operations include `git.push`, `git.merge`, `deploy.run`, `publish.run`, `credential.change`, `service.manage`, `package.install`, `elevation.request`, and `gui.control`.

## Workspace Locks And Git Worktrees

Managed coding jobs should not mutate the primary checkout directly. The development CLI now includes the workspace foundation that future Codex, Claude Code, ACP, and Git adapters will share.

Acquire a one-writer lock for a repository:

```bash
rdev workspace lock \
  --repo . \
  --host-id hst_... \
  --job-id job_... \
  --adapter codex
```

The lock file uses schema `rdev.workspace-lock.v1`, is stored under `<repo>/.rdev/workspace-locks` by default, and is written with `0600` permissions. A second writer for the same canonical repo root is rejected until the lock expires or is released by the owning job.

Inspect or release the lock:

```bash
rdev workspace status --repo .
rdev workspace unlock --repo . --job-id job_...
```

Prepare a Git worktree for a coding adapter:

```bash
rdev workspace prepare-worktree \
  --repo . \
  --host-id hst_... \
  --job-id job_... \
  --adapter codex \
  --base-ref HEAD
```

The response uses schema `rdev.git-worktree-plan.v1`, records the lock, branch, worktree path, and command evidence for `git rev-parse` and `git worktree add`. If `git worktree add` fails, the CLI releases the lock so another job is not blocked by a failed preparation step.

To enforce the same lock during host job execution, start the host with a lock store:

```bash
rdev host serve \
  --mode managed \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --workspace-lock-store .rdev/workspace-locks
```

When `--workspace-lock-store` is set, the hostrunner acquires `rdev.workspace-lock.v1` after signature, nonce, identity, capability, and approval checks, and before adapter execution. The lock is released after success or adapter denial. If another active job holds the canonical repo root, the job fails with structured denial code `workspace_locked`.

The host redacts common secret patterns before artifact upload:

- `sk-...` API keys
- GitHub `ghp_...` and `github_pat_...` tokens
- `Authorization: Bearer ...`
- `password=...`, `token=...`, `api_key=...`, and JSON equivalents
- AWS access key IDs
- PEM private key blocks

Read execution evidence:

```bash
curl -s http://127.0.0.1:8787/v1/jobs/<job_id>/artifacts
curl -s http://127.0.0.1:8787/v1/artifacts/<artifact_id>
```

Export a complete job evidence bundle from gateway state:

```bash
rdev evidence export \
  --gateway http://127.0.0.1:8787 \
  --job-id <job_id> \
  --out .rdev/evidence/<job_id>
```

The CLI fetches the job, artifacts, and audit events from the gateway API and writes the same `rdev.evidence-bundle.v1` directory shape as file-based export: `manifest.json`, `checksums.txt`, `job.json`, `envelope.json`, `policy-decision.json`, `artifacts.json`, artifact files, `audit-slice.jsonl`, and `audit-chain.json`.

The dev gateway also exposes server-side export for local diagnostics:

```bash
curl -s "http://127.0.0.1:8787/v1/jobs/<job_id>/evidence-bundle?out=$(pwd)/.rdev/evidence/<job_id>"
```

Revoke a host and cancel its pending/running jobs:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/hosts/<host_id>/revoke \
  -H 'content-type: application/json' \
  -d '{"reason":"support session complete"}'
```

## Release Artifact Verification

Development release artifacts can be signed and verified before bootstrap:

```bash
rdev release sign \
  --artifact ./rdev-host.exe \
  --key .rdev/keys/release-root.json \
  --out ./rdev-host.exe.rdev-release.json

rdev release verify \
  --artifact ./rdev-host.exe \
  --manifest ./rdev-host.exe.rdev-release.json \
  --root-public-key release-root:<base64url_ed25519_public_key>

rdev release create-bundle \
  --dir ./dist \
  --artifacts rdev-host.exe,rdev-verify.exe \
  --require-artifacts rdev-host.exe,rdev-verify.exe \
  --key .rdev/keys/release-root.json

rdev release verify-bundle \
  --bundle ./dist/release-bundle.json \
  --root-public-key release-root:<base64url_ed25519_public_key>

rdev-verify \
  --bundle ./dist/release-bundle.json \
  --root-public-key release-root:<base64url_ed25519_public_key> \
  --require-artifacts rdev-host.exe,rdev-verify.exe
```

The release manifest is a signed `rdev.release-artifact.v1` JSON document.
The release bundle is a signed `rdev.release-bundle.v1` index that verifies the
bundle signature, every listed artifact manifest, artifact and manifest
SHA-256/size, and required artifact presence before publishing. The standalone
verifier supports the same bundle check for target-host or bootstrap contexts
after the verifier binary itself has been hash-pinned.

For Windows bootstrap, publish a tiny verifier binary alongside the host binary:

```powershell
.\windows-temporary.ps1 `
  -GatewayUrl https://agent.lunflux.com `
  -TicketCode ABCD-1234 `
  -DownloadUrl https://example/rdev-host.exe `
  -ExpectedSha256 <host_sha256> `
  -ReleaseManifestUrl https://example/rdev-host.exe.rdev-release.json `
  -ReleaseRootPublicKey release-root:<base64url_ed25519_public_key> `
  -VerifierDownloadUrl https://example/rdev-verify.exe `
  -VerifierExpectedSha256 <verifier_sha256>
```

The script hash-pins `rdev-verify.exe` before using it to verify the signed host release manifest.

## Limitations

- In-memory state.
- No production WSS/mTLS host transport. Development HTTPS long-poll is available for local outbound-channel testing.
- No authentication.
- No production TLS.
- Without `--signing-key`, signed job envelopes use an in-memory development Ed25519 key.
- With `--signing-key`, the dev gateway persists one Ed25519 key file and host `--trust-pin` can reject unexpected gateway public keys.
- If `--manifest-signing-key` is omitted, the dev join manifest is signed by the same gateway key it advertises.
- If `--manifest-signing-key` is provided, the dev join manifest is signed by a separate root key; hosts should pass `--manifest-root-public-key <key_id>:<base64url_ed25519_public_key>` before trusting the embedded gateway job-signing bundle. Production still needs release-key lifecycle policy, revocation, managed trust bundle updates, and platform-native Windows code signing policy.
- `GET /v1/trust-bundle` and `POST /v1/trust-bundle` are development endpoints for exercising signed trust bundle rotation. They are not authenticated in dev mode and are not durable across process restarts.
- `--trust-store` is a file-backed development store. Production managed hosts should move this to OS-protected storage where available.
- `--identity-store` is a file-backed development host identity store. Production managed hosts should move identity keys to OS-protected storage and add signed registration proofs.
- `--nonce-store` is a file-backed development nonce replay cache. Production managed hosts should use durable local storage with pruning and crash-safe writes.
- The dev shell adapter is intentionally narrow: allowlisted argv only, no shell interpolation, host-side artifact redaction for common secret patterns, and no OS-specific sandboxing beyond workspace boundary checks.
