# Development Gateway

`rdev gateway serve --dev` starts a local HTTP gateway backed by the same in-memory state machine used by `rdev mcp serve`.

This is a development surface only. It is not production transport, does not authenticate requests, and binds to `127.0.0.1` by default.

## Start

```bash
rdev gateway serve --dev --addr 127.0.0.1:8787 --audit-log .rdev/audit/events.jsonl
```

## Endpoints

- `GET /healthz`
- `POST /v1/tickets`
- `GET /v1/hosts`
- `POST /v1/hosts/register`
- `POST /v1/hosts/{host_id}/approve`
- `POST /v1/jobs`
- `GET /v1/jobs/{job_id}`
- `GET /v1/hosts/{host_id}/jobs/next`
- `POST /v1/jobs/{job_id}/complete`
- `GET /v1/audit`

## Example

```bash
curl -s http://127.0.0.1:8787/healthz

curl -s -X POST http://127.0.0.1:8787/v1/tickets \
  -H 'content-type: application/json' \
  -d '{"mode":"attended-temporary","ttl_seconds":600,"reason":"local dev"}'

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
  -d '{"host_id":"hst_...","adapter":"shell","intent":"local demo","policy":{"workspace_root":".","capabilities":["shell.user"]}}'

rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --max-jobs=1 \
  --approval-timeout=30s
```

When `--once=false`, `rdev host serve` waits until the registered host is approved, polls `GET /v1/hosts/{host_id}/jobs/next`, runs the development host runner, and reports completion to `POST /v1/jobs/{job_id}/complete`.

## Limitations

- In-memory state.
- No WSS host transport.
- No authentication.
- No production TLS.
- Signed job envelopes use an in-memory development Ed25519 key.
- The dev host runner performs structural and policy checks, but cryptographic host-side verification still needs gateway public-key trust distribution.
- The dev host runner does not execute arbitrary shell commands yet; it only proves the policy/envelope/job-completion loop.
