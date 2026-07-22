# MCP Stdio Server

`rdev mcp serve` exposes the current Remote Dev Skillkit Control Plane v1
surface over newline-delimited JSON-RPC. The server implements the official MCP
`2025-11-25` lifecycle and tools methods:

- `initialize`
- `notifications/initialized`
- `tools/list`
- `tools/call`

## Current Tools

The MCP surface contains only these session-control tools:

- `rdev.sessions.create`
- `rdev.sessions.status`
- `rdev.sessions.events`
- `rdev.sessions.task`
- `rdev.sessions.interrupt`
- `rdev.sessions.artifacts`
- `rdev.sessions.close`
- `rdev.sessions.connect`

`tools/list`, `rdev mcp tools`, and the checked-in
`mcp/tools.json` are generated from the same Go contract. Clients must use
`tools/list` rather than guessing additional tool names.

`rdev.sessions.connect` may return an exact `cli_next_command` while leaving
`mcp_next_tool` empty. Support-session ticket creation and ticket-status
watching, plus Connection Entry materialization, remain CLI-only workflows.
Execute the returned argv rather than deriving an unregistered MCP tool name.
On Windows, forward the signed generated broker unchanged: it tries the current
PowerShell policy, one process-scoped `ExecutionPolicy Bypass` retry, and then
native CMD. The branches share one attempt and may start at most one core and
one connection, both through `rdev-bootstrap`.

The session tools return bounded `structuredContent` together with a textual
`content` item. Session status fields include `user_summary`,
`agent_next_action`, `recoverable`, `retry_after_ms`, `last_seq`, and
`snapshot_seq` where applicable. Large logs and binary artifacts stay behind
artifact references.

## Retired Tools

Older invite, support-session, acceptance, adapter-verification, audit,
policy, enrollment, update, file, and desktop MCP names are not part of the
current protocol surface. Calling one returns the MCP protocol error
`-32602` with an `unknown tool` message. Their operator workflows remain
available through the corresponding `rdev` CLI and Control Plane session task
adapters.

## Example

```bash
rdev mcp serve
```

```text
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rdev.sessions.create","arguments":{"reason":"visible temporary remote support"}}}
```

The server is a local MCP control surface. Real host networking and task
execution happen through the gateway and host Control Plane transports; MCP
clients submit only policy-bound session tasks.

## Gateway Proxying

Set a server-level gateway URL or pass `gateway_url` on a session tool call:

```bash
rdev mcp serve --gateway-url https://gateway.example.test/v1
```

The per-call `gateway_url` overrides the server-level value for that request.
The gateway URL must be selected through the normal trust and connection-entry
workflow; MCP does not bypass gateway, host, or local OS authorization.
