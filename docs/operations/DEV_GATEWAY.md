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
- `GET /v1/audit`

## Example

```bash
curl -s http://127.0.0.1:8787/healthz

curl -s -X POST http://127.0.0.1:8787/v1/tickets \
  -H 'content-type: application/json' \
  -d '{"mode":"attended-temporary","ttl_seconds":600,"reason":"local dev"}'

curl -s http://127.0.0.1:8787/v1/audit
```

## Limitations

- In-memory state.
- No host transport.
- No authentication.
- No production TLS.
- No signed jobs yet.
