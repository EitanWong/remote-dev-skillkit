# MCP Stdio Server

`rdev mcp serve` exposes the current Remote Dev Skillkit tool contract over a newline-delimited JSON-RPC stdio loop.

Implemented methods:

- `initialize`
- `notifications/initialized`
- `tools/list`
- `tools/call`

The server currently uses an in-memory gateway. It is suitable for local integration tests and early agent wiring, not persistent production use.

Useful read-only tools include:

- `rdev.policy.explain`
- `rdev.policy.explain_shell`
- `rdev.adapter.verify_result`

`rdev.adapter.verify_result` returns `rdev.adapter-conformance-report.v1` in
`structuredContent`. It accepts either `artifact_json` or `artifact_id`, plus
the expected adapter and result schema.

## Example

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rdev.tickets.create","arguments":{"mode":"attended-temporary","ttl_seconds":600,"reason":"local test"}}}' \
  '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"rdev.adapter.verify_result","arguments":{"adapter":"shell","schema":"rdev.shell-result.v1","artifact_json":"{\"schema_version\":\"rdev.shell-result.v1\",\"adapter\":\"shell\",\"workspace_root\":\"/tmp/repo\",\"exit_code\":0,\"timed_out\":false,\"canceled\":false,\"output_truncated\":false,\"started_at\":\"2026-06-30T00:00:00Z\",\"ended_at\":\"2026-06-30T00:00:01Z\",\"duration_millis\":1000,\"redacted\":false,\"redaction_rules\":[\"openai_api_key\"]}"}}}' \
  | rdev mcp serve
```

Tool calls return:

- `content`: text content for basic MCP clients.
- `structuredContent`: machine-readable result data.

## Current Limitations

- In-memory only.
- No persistent host sessions yet.
- Job envelopes are signed with an in-memory development Ed25519 key; production key storage is not implemented yet.
- No real network transport yet.
