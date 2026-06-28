# MCP Stdio Server

`rdev mcp serve` exposes the current Remote Dev Skillkit tool contract over a newline-delimited JSON-RPC stdio loop.

Implemented methods:

- `initialize`
- `notifications/initialized`
- `tools/list`
- `tools/call`

The server currently uses an in-memory gateway. It is suitable for local integration tests and early agent wiring, not persistent production use.

## Example

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rdev.tickets.create","arguments":{"mode":"attended-temporary","ttl_seconds":600,"reason":"local test"}}}' \
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
