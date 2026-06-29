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
- `POST /v1/tickets`
- `GET /v1/tickets/{ticket_code}/manifest`
- `GET /v1/hosts`
- `POST /v1/hosts/register`
- `POST /v1/hosts/{host_id}/approve`
- `POST /v1/hosts/{host_id}/revoke`
- `POST /v1/jobs`
- `GET /v1/jobs/{job_id}`
- `GET /v1/jobs/{job_id}/artifacts`
- `GET /v1/hosts/{host_id}/jobs/next`
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

Register a foreground temporary host:

```bash
rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234
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
  --max-jobs=1 \
  --approval-timeout=30s
```

Preflight the same shell job policy before creating it:

```bash
rdev policy explain-shell \
  --policy-json '{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"],"max_duration_seconds":30,"max_output_bytes":65536}'
```

Agents can call the same policy engine through MCP tool `rdev.policy.explain_shell`.

When `--once=false`, `rdev host serve` fetches the gateway trust bundle, waits until the registered host is approved, polls `GET /v1/hosts/{host_id}/jobs/next`, verifies the signed job envelope, runs the development host runner, and reports completion to `POST /v1/jobs/{job_id}/complete`.

If the host runner rejects a job, the host reports the failure to `POST /v1/jobs/{job_id}/fail`. The gateway marks the job `failed`, stores `failure_reason`, and writes a `job.fail` audit event.

The development shell adapter executes `policy.argv` directly without shell interpolation. The first argv item must match `policy.allow_commands`, the workspace root must exist, write scopes must remain inside the workspace, and output is capped by the signed envelope limit. Completion and failure artifacts include argv, canonical workspace, exit code, stdout/stderr excerpts, timeout state, truncation state, and duration.

Read execution evidence:

```bash
curl -s http://127.0.0.1:8787/v1/jobs/<job_id>/artifacts
curl -s http://127.0.0.1:8787/v1/artifacts/<artifact_id>
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
```

The release manifest is a signed `rdev.release-artifact.v1` JSON document.

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
- No WSS host transport.
- No authentication.
- No production TLS.
- Without `--signing-key`, signed job envelopes use an in-memory development Ed25519 key.
- With `--signing-key`, the dev gateway persists one Ed25519 key file and host `--trust-pin` can reject unexpected gateway public keys.
- If `--manifest-signing-key` is omitted, the dev join manifest is signed by the same gateway key it advertises.
- If `--manifest-signing-key` is provided, the dev join manifest is signed by a separate root key; hosts should pass `--manifest-root-public-key <key_id>:<base64url_ed25519_public_key>` before trusting the embedded gateway job-signing bundle. Production still needs release-key lifecycle policy, revocation, managed trust bundle updates, and platform-native Windows code signing policy.
- The dev shell adapter is intentionally narrow: allowlisted argv only, no shell interpolation, no production redaction schema yet, and no OS-specific sandboxing beyond workspace boundary checks.
