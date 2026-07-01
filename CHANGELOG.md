# Changelog

All notable local development changes are recorded here. This repository has
not been pushed to a public GitHub remote yet; release publication still
requires explicit operator approval.

## 0.1.0-dev

Current phase: local safety kernel, open-source packaging, and public-readiness
hardening. The project is not claiming production hosted transport or real
Windows/Linux/macOS acceptance completion yet.

### Added

- Implemented the `rdev` CLI plus thin `rdev-host`, `rdev-gateway`,
  `rdev-mcp`, and `rdev-verify` entrypoints.
- Added local dev gateway, MCP stdio tools, ticket/host/job/artifact/audit
  primitives, signed job envelopes, trust bundles, enrollment certificates,
  revocation lists, host identity proofs, nonce replay protection, approval
  gates, workspace locks, evidence export, and hash-chained audit verification.
- Added shell, PowerShell, Codex, Claude Code, and ACP/acpx hostrunner adapter
  paths with bounded execution, redaction, cancellation evidence, approval
  preflight, and adapter conformance surfaces.
- Added `pkg/adapterkit` plus adapter result, lifecycle, cancellation, and
  runtime fixture verification through CLI and MCP tools.
- Added Skillkit export, verification, install-plan generation, install-plan
  verification, and dry-run-by-default direct install for Codex, Claude Code,
  Hermes, OpenClaw/OpenCode, OpenCode, and generic MCP agents.
- Added machine-readable adaptive Skillkit configuration contracts so agents
  probe OS, shell, service manager, gateway, workspace, adapters, framework
  paths, and permissions before acting, and ask when configuration is unclear.
- Added local release artifact builds, signed release bundles, release
  candidates, platform candidate automation, GitHub Release dry-run planning,
  post-release install verification planning, SPDX 2.3 SBOM generation, and
  `rdev.release-provenance.v1` provenance attestations.
- Added local GitHub project readiness auditing for docs, templates, CI,
  release scripts, project bootstrap dry-runs, public-surface hygiene, and
  multilingual quick-start coverage.
- Added public open-source project scaffolding: Apache-2.0 license,
  `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, issue templates, PR
  template, CI workflow, project structure docs, release checklist, and
  GitHub project-management docs.
- Added multilingual quick starts for 10 languages and
  `scripts/audit-i18n-quickstarts.sh` to keep global install instructions
  aligned with the English README.

### Changed

- Reworked the root README into a concise public homepage with project purpose,
  core highlights, quick install path, local demo, status, documentation map,
  and Apache-2.0 license link.
- Generalized public examples to placeholders such as `example.com` and example
  user paths; private domains, personal paths, dated public architecture labels,
  and hidden files are blocked by public-surface audits.
- Removed the untracked empty top-level `tools/` directory from the project
  shape; the tracked tool contract is `mcp/tools.json`, and automation lives in
  `scripts/`.
- Made generated release manifests and candidates use package-relative public
  paths so local output directories do not leak into release evidence.

### Verification

- `./scripts/check.sh` passes, including `go test ./...`, `go vet ./...`,
  shell syntax checks, public-surface audit, and multilingual quick-start audit.
- `scripts/github/audit-project-readiness.sh --repo example/remote-dev-skillkit`
  passes with `external_mutation=false`, no remotes, no failed checks, and
  `i18n_quickstarts_ok=true`.
- Private-pattern public-surface audit passes when local private patterns are
  injected through `RDEV_PUBLIC_SURFACE_PRIVATE_PATTERNS`.

### Remaining Gates

- Real clean Windows temporary acceptance evidence.
- Real managed Mac LaunchAgent acceptance evidence.
- Real Linux systemd reboot/reconnect acceptance evidence.
- Real Windows Service install/start/reconnect/stop/uninstall acceptance
  evidence.
- Production WSS/mTLS host transport.
- Full production enrollment authority lifecycle, operator roles, key custody,
  fleet renewal policy, and emergency drills.
- Production hosted storage/authentication and final external GitHub
  publication after explicit approval.

## 0.0.1-dev

- Created project skeleton.
- Added CLI, docs, MCP contract, and Agent Skills drafts.
- Added basic tests for CLI, MCP contracts, and temporary-mode capability defaults.
- Added in-memory gateway model and `rdev demo local` closed-loop demo.
- Added `rdev mcp serve` with MCP initialize, tools/list, and tools/call support backed by the in-memory gateway.
- Added JSONL audit store and `rdev gateway serve --dev` local HTTP development gateway.
