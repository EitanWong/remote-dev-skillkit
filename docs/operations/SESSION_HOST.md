# Session Host Operation

## Discover the active contract

```bash
rdev doctor
rdev mcp tools
rdev host serve --help
```

Do not infer unsupported flags or MCP methods from older examples.

## Development gateway

```bash
rdev gateway serve --dev
```

This command binds loopback only and is suitable for local development. A remote host requires an operator-managed HTTPS gateway endpoint.

## Managed pilot gateway

An operator-managed pilot may place HTTPS termination in front of the same
loopback-only process and load a protected, hashed-principal auth file:

```bash
rdev gateway serve --dev --operator-auth-file /protected/operators.json
```

The rdev process never binds a public address. The auth file contains token
hashes, not token values; the MCP proxy reads its bearer token separately from
its protected local token file. This pilot is intentionally ephemeral: a
gateway restart invalidates active sessions and requires a new session and host
join.

## Join a host

```bash
rdev host serve --join-code CODE --gateway https://gateway.example --once
```

Useful host options from the current help surface include `--mode`, `--transport`, `--trust-pin`, `--trust-store`, `--identity-store`, and `--workspace-lock-store`.

## Browser handoff for managed Windows hosts

An operator-managed HTTPS gateway can serve a short-lived browser handoff when
it starts with `--public-base-url HTTPS_URL` and
`--windows-amd64-host-binary PATH`. Create the link through
`rdev.sessions.handoff`, then send that link to the Windows operator. The
browser exposes its native download only after a one-time claim; the page
localizes itself, gates the action to Windows, downloads
a hash-verifying double-click `Connect-Rdev.cmd` launcher, and exposes
PowerShell only as a fallback. The target connects outward via managed
long-poll. See
[`WEB_HANDOFF.md`](WEB_HANDOFF.md) for deployment, expiry, and capability
details.

## Remote MCP proxy

```bash
rdev mcp serve --gateway-url https://gateway.example --operator-token-file /protected/token-file
```

The token file contains local protected material. Keep its value out of command arguments, repository files, transcripts, and prompts.

For a managed MCP launcher, set `RDEV_GATEWAY_OPERATOR_TOKEN_FILE` to the
local protected **file path**. `rdev mcp serve` uses that path only when
`--operator-token-file` is absent; an explicit flag wins. Do not put the raw
bearer in environment variables or command arguments.

## Verification

After a host joins, query session status and events through MCP. Before reporting completion, inspect task terminal state and artifact metadata, then close only on the operator's decision.
