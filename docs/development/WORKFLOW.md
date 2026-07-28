# Development Workflow

1. Inspect current source, contracts, and tests before editing.
2. Make the smallest change that preserves the session Control Plane contract.
3. Run focused tests while iterating.
4. Run the repository checks before committing.

```bash
go test ./...
go vet ./...
./scripts/check.sh
git diff --check
```

Keep every completed slice in its own verified commit. Do not push, publish, or alter remote state without explicit operator direction.
