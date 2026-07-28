# Contributing

Remote Dev Skillkit is a session-native remote-development toolkit. Keep changes small, auditable, and compatible with the current Control Plane contract.

## Before editing

1. Read the relevant source, contract, tests, and current CLI help.
2. Keep host actions policy-bound and visible.
3. Do not add persistence, public inbound host listeners, security-control bypasses, or unrestricted shell access.
4. Do not place credentials, tokens, private keys, or host transcripts in the repository.

## Development loop

```bash
go test ./...
go vet ./...
./scripts/check.sh
```

Use focused tests while iterating, then run the complete checks before opening a pull request. Keep commits narrow and explain the observable session contract change.

## Pull request checklist

- [ ] The change has a bounded purpose.
- [ ] Current session behavior has targeted coverage.
- [ ] `go test ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] `./scripts/check.sh` passes.
- [ ] Documentation reflects the active CLI and MCP surface.
- [ ] No protected material or local runtime state is included.
