# Connection Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make attended temporary support-session connection resilient enough that users run one command and rdev handles helper reuse, gateway fallback, transport fallback, and progress status automatically.

**Architecture:** Add a pure `internal/connectionhealth` package for connection plans, attempts, and status derivation. Wire it into existing bootstrap/support-session/hostcmd surfaces incrementally: first contracts and status, then helper cache reuse, then gateway registration fallback and transport fallback evidence.

**Tech Stack:** Go standard library only, existing `internal/assetdownload`, `internal/bootstrapcmd`, `internal/hostcmd`, `internal/httpapi`, `internal/supportsession`, fake `http.RoundTripper` tests instead of local listeners where possible.

## Global Constraints

- Scope is attended temporary support session first.
- Do not add hidden persistence.
- Do not install, start, restart, or hide managed services automatically.
- Do not bypass UAC, sudo, TCC, Gatekeeper, Windows Defender, proxy, firewall, or enterprise policy.
- Do not expose target hosts through inbound public ports.
- Do not treat preconnect, cached helper reuse, or `rdev-bootstrap` as task authorization.
- Cached helper execution requires SHA-256 match against gateway-provided checksum.
- Gateway fallback must stay inside signed manifest candidates or explicit operator-provided gateway URLs.
- Tests should avoid real local port binding when a fake transport or pure function test can cover the behavior.

---

### Task 1: Pure Connection Health State

**Files:**
- Create: `internal/connectionhealth/health.go`
- Create: `internal/connectionhealth/health_test.go`

**Interfaces:**
- Produces: `type Candidate struct { URL string; Kind string; Priority int }`
- Produces: `type Attempt struct { Phase string; URL string; Transport string; OK bool; Error string; At time.Time }`
- Produces: `type Plan struct { SchemaVersion string; Candidates []Candidate; Attempts []Attempt; SelectedGatewayURL string; Status string; AgentNextAction string }`
- Produces: `func NewPlan(candidates []Candidate) Plan`
- Produces: `func (p Plan) WithAttempt(a Attempt) Plan`
- Produces: `func (p Plan) SelectGateway(url string) Plan`

- [ ] Write failing tests:
  - `TestPlanDeduplicatesCandidatesAndPreservesOrder`
  - `TestPlanReportsGatewaySwitchingBeforeSuccess`
  - `TestPlanReportsRecoveredAfterFailureThenSuccess`
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/connectionhealth -count=1`
  Expected: FAIL because package does not exist.
- [ ] Implement the package with immutable-style methods returning updated copies.
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/connectionhealth -count=1`
  Expected: PASS.

### Task 2: Helper Cache Defaults For Bootstrap Upgrade

**Files:**
- Modify: `internal/assetdownload/downloader.go`
- Modify: `internal/assetdownload/downloader_test.go`
- Modify: `internal/bootstrapcmd/bootstrapcmd.go`
- Modify: `internal/bootstrapcmd/bootstrapcmd_test.go`

**Interfaces:**
- Produces: `func DefaultCachePath(assetName string) (string, bool)`
- Consumes: `assetdownload.Download`
- Changes: `rdev-bootstrap upgrade` uses `DefaultCachePath(asset)` when `--cache` is omitted.

- [ ] Write failing test `TestDefaultCachePathUsesUserCache`.
- [ ] Write failing test `TestUpgradeUsesDefaultCacheWhenCacheFlagOmitted`.
- [ ] Run focused tests and confirm RED.
- [ ] Implement default cache path with OS user cache directory and safe asset filename validation.
- [ ] Wire `rdev-bootstrap upgrade` to use the default cache path only when `--cache` is empty.
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/assetdownload ./internal/bootstrapcmd -count=1`
  Expected: PASS.

### Task 3: Bootstrap Scripts Reuse Verified Helper Cache

**Files:**
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/server_test.go`

**Interfaces:**
- Consumes: gateway asset SHA-256 from `/assets/<asset>.sha256`.
- Produces bootstrap phases:
  - `using-installed-helper`
  - `using-cached-helper`
  - `downloading-helper`
  - `verifying-helper`
  - `starting-full-helper`

- [ ] Write focused tests that render `/join/<ticket>/bootstrap.ps1` and `/join/<ticket>/bootstrap.sh` without opening a listener.
- [ ] Assert scripts contain user cache paths, checksum-before-download logic, `using-cached-helper`, and no service install/start commands.
- [ ] Run focused tests and confirm RED.
- [ ] Update PowerShell bootstrap to store helper under `%LOCALAPPDATA%\RemoteDevSkillkit\cache\helpers\<asset>`.
- [ ] Update shell bootstrap to store helper under `${XDG_CACHE_HOME:-$HOME/.cache}/remote-dev-skillkit/helpers/<asset>`.
- [ ] Preserve installed `rdev` priority before cache lookup.
- [ ] Run focused tests.

### Task 4: Host Gateway Registration Fallback

**Files:**
- Modify: `internal/hostcmd/hostcmd.go`
- Modify: `internal/hostcmd/hostcmd_test.go`

**Interfaces:**
- Consumes: `serveOptions.ManifestGatewayCandidates`.
- Produces: candidate-aware host registration that tries selected gateway first, then remaining signed candidates.
- Produces: registration result metadata with selected gateway and failed attempts.

- [ ] Write failing test `TestRunServeFallsBackToSecondManifestGatewayForRegistration` using a fake `http.RoundTripper`.
- [ ] Confirm first candidate returns transient error and second candidate registers successfully.
- [ ] Implement helper `candidateGatewayURLs(primary string, candidates []model.JoinManifestGatewayCandidate) []string`.
- [ ] Implement candidate-aware registration loop around `registerHost`.
- [ ] Treat trust/policy/ticket/checksum errors as terminal; treat network/5xx/408/429 as fallback-eligible.
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/hostcmd -run TestRunServeFallsBackToSecondManifestGatewayForRegistration -count=1`
  Expected: PASS.

### Task 5: Status Contract For Progress And Recovery

**Files:**
- Modify: `internal/supportsession/plan.go`
- Modify: `internal/supportsession/plan_test.go`

**Interfaces:**
- Consumes: existing preconnect phases and new connection health summary fields.
- Produces status values:
  - `target-downloading`
  - `target-verifying`
  - `target-starting`
  - `gateway-switching`
  - `transport-degraded`
  - `reconnecting`
  - `recovered`

- [ ] Write failing tests for `BuildStatus` with preconnect phases `verifying-helper` and `starting-full-helper`.
- [ ] Write failing test for connection health summary reporting `reconnecting` as progress.
- [ ] Implement status mapping and Agent next-action copy.
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/supportsession -count=1`
  Expected: PASS.

### Task 6: Verification

**Files:**
- No planned source edits.

- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go test ./internal/connectionhealth ./internal/assetdownload ./internal/bootstrapcmd ./internal/httpapi ./internal/hostcmd ./internal/supportsession -count=1`
- [ ] Run focused manifest/bootstrap/status tests if full `httpapi` package hits local listener restrictions.
- [ ] Run: `GOCACHE=$PWD/.tmp/go-build go vet ./...`
- [ ] Run: `git diff --check`
- [ ] Build: `GOCACHE=$PWD/.tmp/go-build go build -o .tmp/bin/rdev ./cmd/rdev`
- [ ] Build: `GOCACHE=$PWD/.tmp/go-build go build -o .tmp/bin/rdev-host ./cmd/rdev-host`
- [ ] Build: `GOCACHE=$PWD/.tmp/go-build go build -o .tmp/bin/rdev-bootstrap ./cmd/rdev-bootstrap`
- [ ] Record gzip sizes for `.tmp/bin/rdev-host` and `.tmp/bin/rdev-bootstrap`.
