# Project Structure

Remote Dev Skillkit keeps runtime code, public contracts, release automation,
operations docs, and agent skills in separate folders so contributors can find
the right surface quickly.

```text
.
├── cmd/                  # Thin command entrypoints: rdev, host, gateway, MCP, verifier
├── internal/             # Product implementation packages
├── pkg/adapterkit/       # Public adapter conformance helpers
├── skills/               # Agent-loadable Skillkit workflows
├── mcp/                  # Stable MCP tool contract metadata
├── docs/                 # Architecture, operations, project, and security docs
├── examples/             # Copyable adapter and integration examples
├── scripts/              # CI, release, bootstrap, and GitHub project automation
├── .github/              # CI workflow, issue templates, and PR template
├── README.md             # Main public project overview
├── CONTRIBUTING.md       # Contribution workflow and safety rules
├── CODE_OF_CONDUCT.md    # Community expectations
├── SECURITY.md           # Security model and reporting
├── LICENSE               # Apache-2.0 license
├── CHANGELOG.md          # Release notes
└── TASKS.md              # Local task board and completion gates
```

## Package Boundaries

`cmd/` contains only thin binaries. Shared behavior belongs in `internal/cli` or
domain packages.

`internal/model` owns schema objects and cryptographic verification contracts.

`internal/gateway`, `internal/httpapi`, and `internal/mcpstdio` own gateway state,
HTTP APIs, and MCP surfaces.

`internal/hostrunner`, adapter packages, `internal/policy`, `internal/workspace`,
`internal/hostidentity`, `internal/hosttrust`, `internal/hostnonce`, and
`internal/hostapproval` form the host safety kernel.

`internal/release`, `internal/skillkit`, `internal/acceptance`, and `scripts/`
own distribution, installation, release evidence, and project-management
automation.

`pkg/adapterkit` is public-facing and must avoid depending on internal packages.

## Public Repository Hygiene

- Do not commit `.rdev/`, `dist/`, build outputs, local transcripts, secrets, or
  OS metadata files.
- Keep generated release artifacts under ignored output directories unless they
  are intentionally added as small examples.
- Keep user-facing behavior documented in `README.md`, relevant `docs/operations`
  files, and `skills/`.
- Keep release/project-management behavior covered by `scripts/ci/release-smoke.sh`
  and `scripts/github/audit-project-readiness.sh`.
