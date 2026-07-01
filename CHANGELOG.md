# Changelog

All notable local development changes are recorded here. This repository has
not been pushed to a public GitHub remote yet; release publication still
requires explicit operator approval.

## 0.1.0-dev

Current phase: local safety kernel, open-source packaging, public-readiness
hardening, and local production-like operator auth. The project is not claiming
production hosted transport or real Windows/Linux/macOS acceptance completion
yet.

### Added

- Added local operator auth through `rdev.operator-auth.v1`,
  `rdev operator-auth init`, `rdev operator-auth verify`, and
  `rdev gateway serve --operator-auth`.
- Added hashed token storage and `admin`, `operator`, `issuer`, and `auditor`
  role gates for control-plane and enrollment endpoints.
- Added operator-token support for hosted enrollment issuance, renewal, host
  near-expiry renewal, and hosted revocation refresh.

### Changed

- Removed the older enrollment-specific standalone bearer-token surface from the
  current standard; hosted enrollment authority calls now use operator auth plus
  `--operator-token-file`.
- Split this changelog into staged pre-release versions so the path from
  `0.0.1-dev` to `0.1.0-dev` is auditable.

### Verification

- `go test ./...` passes.
- `go vet ./...` passes.
- `./scripts/check.sh` passes.
- `./scripts/ci/release-smoke.sh` passes with `external_mutation=false`.
- Public-surface audits pass, including injected private-pattern checks.

### Remaining Gates

- Real clean Windows temporary acceptance evidence.
- Real managed Mac LaunchAgent acceptance evidence.
- Real Linux systemd reboot/reconnect acceptance evidence.
- Real Windows Service install/start/reconnect/stop/uninstall acceptance
  evidence.
- Production WSS/mTLS host transport.
- Full production enrollment authority lifecycle beyond the local operator-role
  foundation: key custody, fleet renewal policy, emergency drills, and hosted
  authority operations.
- Production hosted storage/authentication beyond local operator-auth preflight
  and final external GitHub publication after explicit approval.

## 0.0.9-dev

Public-readiness and open-source packaging pass.

### Added

- Reworked the root README into a concise public homepage with purpose,
  highlights, quick install, local demo, status, docs map, and Apache-2.0 link.
- Added multilingual quick starts for 10 languages plus
  `scripts/audit-i18n-quickstarts.sh`.
- Added local GitHub project readiness auditing for docs, templates, CI,
  release scripts, project bootstrap dry-runs, public-surface hygiene, and
  multilingual quick-start coverage.

### Changed

- Generalized public examples to placeholders such as `example.com` and example
  user paths.
- Removed the untracked empty top-level `tools/` directory from the project
  shape; the tracked tool contract is `mcp/tools.json`.

## 0.0.8-dev

Release and supply-chain evidence pass.

### Added

- Added local release artifact builds, signed release bundles, release
  candidates, and platform candidate automation.
- Added GitHub Release dry-run planning and post-release install verification
  planning without external mutation.
- Added SPDX 2.3 SBOM generation and `rdev.release-provenance.v1`
  provenance attestations.

### Changed

- Made generated release manifests and candidates use package-relative public
  paths so local output directories do not leak into release evidence.

## 0.0.7-dev

Skillkit and mainstream Agent framework install pass.

### Added

- Added Skillkit export, verification, install-plan generation,
  install-plan verification, and dry-run-by-default direct install.
- Added support surfaces for Codex, Claude Code, Hermes, OpenClaw/OpenCode,
  OpenCode, and generic MCP agents.
- Added machine-readable adaptive configuration contracts so agents probe OS,
  shell, service manager, gateway, workspace, adapters, framework paths, and
  permissions before acting.

## 0.0.6-dev

Adapters and host runtime pass.

### Added

- Added shell, PowerShell, Codex, Claude Code, and ACP/acpx hostrunner adapter
  paths with bounded execution, redaction, cancellation evidence, approval
  preflight, and adapter conformance surfaces.
- Added `pkg/adapterkit` plus adapter result, lifecycle, cancellation, and
  runtime fixture verification through CLI and MCP tools.
- Added managed host service planning and evidence packaging surfaces for
  macOS LaunchAgent, Linux systemd user units, Windows Service plans, and clean
  Windows temporary acceptance.

## 0.0.5-dev

Enrollment authority and trust lifecycle pass.

### Added

- Added host enrollment certificates, signed enrollment revocation lists,
  hosted issuance, hosted renewal, local renewal, revocation refresh, and
  host-side pre-registration revocation checks.
- Added trust lifecycle commands for initializing, rotating, revoking, and
  verifying signed trust bundles.
- Added host-bound trust bundle update checks for managed host refresh.

## 0.0.4-dev

Security kernel pass.

### Added

- Added signed job envelopes, host identity proofs, nonce replay protection,
  approval gates, approval token consumption, trust pins, and workspace locks.
- Added host-side denial artifacts and approval-required artifacts before
  adapter execution.
- Added evidence bundle export and hash-chained audit verification.

## 0.0.3-dev

Gateway and host loop pass.

### Added

- Added local dev gateway HTTP APIs for tickets, hosts, jobs, artifacts, audit
  events, and trust bundles.
- Added development HTTPS and mTLS listener/client preflight for local gateway
  and host flows.
- Added restart-safe development gateway state snapshots when `--state` is used
  with a persistent signing key.
- Added foreground temporary host registration, polling, job completion,
  failure reporting, and artifact upload.

## 0.0.2-dev

CLI, MCP, and local demo pass.

### Added

- Implemented the `rdev` CLI plus thin `rdev-host`, `rdev-gateway`,
  `rdev-mcp`, and `rdev-verify` entrypoints.
- Added MCP stdio initialize, tools/list, and tools/call support.
- Added policy explanation, local preview ticket creation, and local demo flow.
- Added JSONL audit storage foundation.

## 0.0.1-dev

Project foundation.

### Added

- Created project skeleton.
- Added initial CLI, docs, MCP contract, and Agent Skills drafts.
- Added architecture, roadmap, versioning, threat model, and initial task board.
- Added basic tests for CLI, MCP contracts, and temporary-mode capability
  defaults.
