# Remote Dev in DeepSeek Harness

[中文](README.zh-CN.md)

This experimental Bundle connects DeepSeek Harness to Remote Dev Skillkit
through Harness's shipped `@deepseek-ai/dsh-mcp-client`. It adds one stdio MCP
server named `rdev`; it does not copy rdev's tools or create another control
plane.

## Prerequisites

- DeepSeek Harness developer preview `0.1.0-rc.8`, using its supported Node
  range `^22.19.0 || >=24.0.0`, with `dsh` and pnpm 11 on `PATH`.
- `rdev` installed. The Bundle checks `RDEV_BIN`, then
  `~/.local/bin/rdev`, then `PATH`.

Pin the verified Harness preview before installing the Bundle:

```sh
pnpm add --global @deepseek-ai/dsh@0.1.0-rc.8
dsh --version  # must print 0.1.0-rc.8
```

If pnpm reports that its global bin directory is missing, run `pnpm setup`,
restart the shell, and repeat the two commands. Do not use an unversioned
`@deepseek-ai/dsh`: npm `latest` is still rc.7 while this Bundle is verified
against rc.8.

From the Remote Dev Skillkit checkout, install both pieces:

```sh
./scripts/install.sh
dsh plugin --profile web add ./integrations/deepseek-harness
```

The Bundle is intentionally private and is not published to npm yet. Restart a
running Profile after adding or removing it.

## Connect a gateway

For an existing managed host, point Harness at the operator-managed HTTPS
gateway. Store the bearer token in an owner-readable file; set only its path:

```sh
export RDEV_GATEWAY_URL=https://gateway.example
export RDEV_GATEWAY_OPERATOR_TOKEN_FILE=/protected/rdev-operator.token
dsh --profile web --dump-config
dsh web
```

For a loopback development journey, start the gateway separately, then launch
Harness from another terminal:

```sh
rdev gateway serve --dev
RDEV_GATEWAY_URL=http://127.0.0.1:8787 dsh web
```

With no `RDEV_GATEWAY_URL`, rdev uses an isolated in-process control plane. That
is useful for MCP contract checks, but a remote host cannot join it.

The config dump must contain exactly one `id: rdev` row. Successful Profile
startup proves the MCP process connected and synchronized its tool list because
the Bundle enables `failOnStartupError`. Harness exposes the tools under the
`mcp__rdev__` prefix; rdev's dotted raw names receive Harness's deterministic
hash suffix.

Headless use is opt-in:

```sh
dsh plugin --profile headless add ./integrations/deepseek-harness
dsh --profile headless "List the online remote hosts and summarize their state."
```

## Remove

```sh
dsh plugin --profile web remove rdev-deepseek-harness
```

## Troubleshooting

| Symptom | Action |
| --- | --- |
| `spawn rdev ENOENT` | Set `RDEV_BIN` to the absolute `rdev` path. |
| `read operator token file` | Check the path and owner-only read permission. |
| Profile startup fails after a Harness update | Re-test against the pinned rc.8 contract; preview APIs may break. |
| Session waits with no host | Confirm a reachable gateway is configured, then inspect host heartbeat freshness. |

The Bundle contract is part of `go test ./...` and the repository's normal
`./scripts/check.sh` gate. Maintainers with `dsh` installed can run
`integrations/deepseek-harness/live-smoke.sh` for an isolated loopback-gateway
and Profile boot; it never writes to the normal `~/.dsh` directory.

## Security and compatibility

The Bundle starts `rdev mcp serve` only. Session policy, operator authorization,
audit, interruption, and target-host controls stay in rdev. No token value is
embedded in the package, no inbound target port is opened, and MCP stdout stays
protocol-only.

This Bundle is pinned and tested against DeepSeek Harness `0.1.0-rc.8`, which
upstream labels a developer preview with breaking changes expected. Custom
native tool names and purpose-built host/session/task cards are intentionally
deferred until that plugin UI contract stabilizes.
