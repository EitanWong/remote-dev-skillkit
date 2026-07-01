# Contributing

Thank you for helping improve Remote Dev Skillkit. This project is safety-first
infrastructure for agent-driven remote development, so contributions must
preserve consent, auditability, and host-local control.

## Development Setup

```bash
go test ./...
go vet ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

Use focused tests while developing, then run the full verification set before
opening a pull request.

## Contribution Rules

- Keep temporary support visible, foreground, revocable, and non-persistent.
- Keep managed service mode explicit, inspectable, stoppable, and uninstallable.
- Do not add hidden persistence, inbound public listeners on target hosts, UAC or
  sudo bypasses, credential scraping, policy weakening, or unrestricted shell
  access.
- New host capabilities must be signed, host-validated, auditable, revocable, and
  covered by tests.
- High-risk operations such as package installation, elevation, GUI control,
  service changes, publishing, deployment, credential changes, push, and merge
  must go through approval gates.
- Public adapter integrations must use the adapterkit conformance surfaces when
  possible.

## Pull Request Checklist

- [ ] The change is scoped to the requested behavior.
- [ ] Tests cover success and failure paths.
- [ ] `go test ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] `./scripts/check.sh` passes.
- [ ] `./scripts/ci/release-smoke.sh` passes when release, Skillkit, GitHub,
      enrollment, or adapter behavior changes.
- [ ] Documentation, Skillkit notes, and release checklists are updated when user
      behavior changes.
- [ ] No secrets, tokens, private keys, local `.rdev/` state, or target-machine
      transcripts are committed.

## External Actions

Scripts under `scripts/github/` default to dry-run or plan generation where
possible. Do not create repositories, publish releases, push tags, mutate issues,
or upload artifacts unless the maintainer has explicitly approved that external
operation.
