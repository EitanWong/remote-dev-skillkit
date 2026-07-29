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

## Long-lived managed gateway

An operator-managed gateway may place HTTPS termination in front of the same
loopback-only process. Long-lived control is explicit: persist the state and
the signing key together, and require protected operator auth:

```bash
rdev gateway serve \
  --operator-auth-file /protected/operators.json \
  --state-file /protected/rdev-gateway/state.json \
  --signing-key-file /protected/rdev-gateway/signing-key.json
```

The rdev process never binds a public address. The auth file contains token
hashes, not token values; the MCP proxy reads its bearer token separately from
its protected local token file. `--state-file` and `--signing-key-file` are a
required pair; persistent mode also requires `--operator-auth-file`. Both
runtime files must be ordinary `0600` files. The gateway creates the signing
key locally on first start and refuses an existing state file with wider
permissions. Keep the pair across restart to restore session, endpoint lease,
task, event, and audit state. Browser handoff proofs remain intentionally
short-lived and are not restored after a gateway restart.

The host keeps an outbound authenticated long-poll. A permitted operator task
is appended by the gateway and wakes that poll; no inbound host port or
unrestricted resident shell is created. Task authority remains bound to the
session capability, workspace policy, adapter, and local host policy.

For an immediate control stop, call `rdev.sessions.close` with
`action: "revoke"`. It invalidates endpoint leases immediately and records the
lifecycle action in the durable audit log. Normal `close` remains the default.

## Join a host

```bash
rdev host serve --join-code CODE --gateway https://gateway.example --once
```

Useful host options from the current help surface include `--mode`, `--transport`, `--trust-pin`, `--trust-store`, `--identity-store`, and `--workspace-lock-store`.

For an explicit long-lived managed connector, keep the operator-visible process
running for the scoped session instead of using the safe one-shot default:

```bash
rdev host serve \
  --mode managed \
  --gateway https://gateway.example \
  --join-code CODE \
  --once=false \
  --transport long-poll \
  --max-tasks 0
```

This does not grant new authority: every task is still checked against the
session capability ceiling, workspace policy, adapter policy, and local host
policy. Keep the connector visible and stop it when the session is closed or
revoked.

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

After a host joins, query session status and events through MCP. Before reporting completion, inspect task terminal state and artifact metadata, then close or revoke only on the operator's decision. Operators and auditors can inspect durable lifecycle records through `GET /v1/audit`.
