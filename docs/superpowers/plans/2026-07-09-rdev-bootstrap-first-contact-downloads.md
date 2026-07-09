# Rdev Bootstrap First-Contact Downloads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the target-side first-contact path robust on weak networks by giving `rdev-bootstrap` a reusable verified downloader with mirrors, cache reuse, resume support, and progress evidence before adding Cloudflare IP optimization.

**Architecture:** Add a small `internal/assetdownload` package that owns mirror selection, cache checks, Range resume, retry/backoff, SHA-256 verification, and transcript events. Wire `rdev-bootstrap upgrade` through this package while keeping authorization unchanged: rdev-bootstrap can download and verify a full helper, but it cannot claim tasks or execute privileged capabilities before the full helper runs.

**Tech Stack:** Go standard library only, existing `internal/bootstrapcmd`, existing support-session preconnect status, signed manifest/checksum inputs, `net/http` Range requests.

## Global Constraints

- Do not trust target host wall clock for gateway-issued validity decisions.
- Do not add hidden persistence, DNS mutation, hosts-file mutation, firewall changes, or proxy changes.
- Do not make Cloudflare IP optimization default until it is explicit, bounded, audited, and isolated to helper asset downloads.
- Keep rdev-bootstrap unable to claim tasks; full runner must execute shell/fs/process/desktop/coding capabilities.
- Use TDD: every behavior change gets a failing test first.
- Prefer standard-library Go.
- Treat Windows as a primary platform.

---

### Task 1: Verified Asset Downloader Foundation

**Files:**
- Create: `internal/assetdownload/downloader.go`
- Create: `internal/assetdownload/downloader_test.go`

**Interfaces:**
- Produces: `type Mirror struct { URL string; Kind string; Weight int }`
- Produces: `type Options struct { Mirrors []Mirror; OutputPath string; CachePath string; ExpectedSHA256 string; Client *http.Client; MaxAttempts int }`
- Produces: `type Result struct { OutputPath string; SourceURL string; FromCache bool; Resumed bool; Bytes int64; SHA256 string; Transcript []Event }`
- Produces: `func Download(ctx context.Context, opts Options) (Result, error)`

- [ ] Write failing tests:
  - `TestDownloadReusesVerifiedCache`
  - `TestDownloadResumesPartialFileWithRange`
  - `TestDownloadFallsBackToSecondMirrorAfterEOF`
  - `TestDownloadRejectsChecksumMismatch`
- [ ] Run: `go test ./internal/assetdownload -count=1`
  Expected: FAIL because the package does not exist.
- [ ] Implement minimal downloader:
  - verify `CachePath` first when present;
  - resume `OutputPath + ".part"` with `Range: bytes=<size>-`;
  - try mirrors in order with retry on EOF/5xx/408/429;
  - atomically rename verified `.part` to `OutputPath`;
  - return transcript events.
- [ ] Run: `go test ./internal/assetdownload -count=1`
  Expected: PASS.

### Task 2: Wire rdev-bootstrap Upgrade Through Downloader

**Files:**
- Modify: `internal/bootstrapcmd/bootstrapcmd.go`
- Modify: `internal/bootstrapcmd/bootstrapcmd_test.go`

**Interfaces:**
- Consumes: `assetdownload.Download`
- Produces CLI flags:
  - `--mirror URL` repeatable;
  - `--cache PATH`;
  - existing `--gateway-url`, `--ticket-code`, `--asset`, `--out`, `--no-exec` remain compatible.

- [ ] Write failing test `TestUpgradeUsesMirrorFallbackAndCache`.
- [ ] Run: `go test ./internal/bootstrapcmd -run TestUpgradeUsesMirrorFallbackAndCache -count=1`
  Expected: FAIL because flags/download wiring are absent.
- [ ] Implement mirror list construction:
  - default mirror: `<gateway-url>/assets/<asset>`;
  - extra `--mirror` values are tried before or after default based on flag order;
  - checksum URL remains `<gateway-url>/assets/<asset>.sha256` for now.
- [ ] Use `assetdownload.Download` and print JSON transcript on `--no-exec`.
- [ ] Run: `go test ./internal/bootstrapcmd -count=1`
  Expected: PASS.

### Task 3: Support-Session Manifest Contract For Mirrors

**Files:**
- Modify: `internal/model/connection_entry_package_catalog.go`
- Modify: `internal/model/join_manifest.go`
- Modify: `internal/supportsession/plan.go`
- Modify: `internal/httpapi/server.go`
- Test: existing package tests plus new focused tests where contracts are generated.

**Interfaces:**
- Produces signed manifest fields for helper asset mirrors without changing existing fields.
- Mirrors are descriptive download candidates only; they do not authorize code execution.

- [ ] Write failing test proving generated manifest/catalog exposes helper asset mirror URLs and SHA-256 expectations.
- [ ] Run focused tests.
- [ ] Add additive JSON fields for mirrors and expected hashes.
- [ ] Run focused tests.

### Task 4: Cloudflare Download Optimizer Plan Gate

**Files:**
- Create: `internal/cdnopt/plan.go`
- Create: `internal/cdnopt/plan_test.go`
- Modify: `internal/supportsession/plan.go`

**Interfaces:**
- Produces dry-run optimizer plan only; no IP testing yet.
- Explicitly records safety boundaries: no DNS/hosts/proxy/firewall mutation, low concurrency, asset downloads only.

- [ ] Write failing tests for optimizer plan defaults and forbidden side effects.
- [ ] Implement dry-run plan data.
- [ ] Surface plan in support-session report.

### Task 5: Verification

**Files:**
- No planned source edits.

- [ ] Run `go test ./internal/assetdownload ./internal/bootstrapcmd ./internal/supportsession ./internal/httpapi -count=1`.
- [ ] Run `go test ./... -count=1`.
- [ ] Run `go vet ./...`.
- [ ] Run `git diff --check`.
- [ ] Build `.tmp/bin/rdev`, `.tmp/bin/rdev-host`, `.tmp/bin/rdev-bootstrap`.
- [ ] Record current compressed `rdev-bootstrap` and `rdev-host` sizes.
