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

## Join a host

```bash
rdev host serve --join-code CODE --gateway https://gateway.example --once
```

Useful host options from the current help surface include `--mode`, `--transport`, `--trust-pin`, `--trust-store`, `--identity-store`, and `--workspace-lock-store`.

## Remote MCP proxy

```bash
rdev mcp serve --gateway-url https://gateway.example --operator-token-file /protected/token-file
```

The token file contains local protected material. Keep its value out of command arguments, repository files, transcripts, and prompts.

## Verification

After a host joins, query session status and events through MCP. Before reporting completion, inspect task terminal state and artifact metadata, then close only on the operator's decision.
