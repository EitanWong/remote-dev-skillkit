# Windows Layered Entry Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a PowerShell-first Windows layered handoff with native CMD fallback, at-most-one core process per broker attempt, dynamic in-core route switching, and a final compressed size no greater than 1,048,576 bytes.

**Architecture:** Materialization produces and measures a deterministic private ZIP. A focused Windows bootstrap reuses transport-neutral `assetdownload.Download`, verifies the signed layered manifest and core, records a private attempt transition, and starts the core in the foreground. The visible CMD broker advances through normal PowerShell, process-scoped PowerShell retry, and native CMD only before `core_started`; archive recovery remains an explicit profile of the same bootstrap. Every generated connection path is migrated away from the legacy full-helper downloader so `rdev-bootstrap` is the sole connection bootstrap project-wide.

**Tech Stack:** Go standard library, Ed25519, `archive/zip`, Windows system `curl.exe`, `internal/release`, `internal/assetdownload`, Go tests, Windows cross-compilation.

## Global Constraints

- The final `Windows-ConnectionEntry.zip`, including the executable bootstrap, must be at most 1,048,576 bytes.
- Prefer visible PowerShell; retain visible native CMD fallback.
- A single broker attempt may start the core at most once.
- Automatic path fallback is allowed only before `core_started`; archive recovery is explicit and never automatic.
- `core_started` limits process duplication only; the running core must continue to probe and dynamically switch among eligible verified connection-pool routes when latency or availability degrades.
- Every Connection Entry on every supported platform must execute through `rdev-bootstrap`; generated launchers must not download or execute the old full `rdev` helper or invoke `rdev host serve`/`rdev-host` directly.
- Reuse `internal/assetdownload.Download`; do not duplicate cache, Range retry, SHA-256, size, or atomic-promotion logic in scripts or transports.
- Add no third-party dependency.
- Require HTTPS, same-origin relative asset paths, Ed25519 release-root verification, expected version, SHA-256, signed size, private `0700`/`0600` cache semantics, UNC/reparse rejection, bounded files, and atomic publication.
- Temporary mode creates no service, scheduled task, Run key, firewall rule, hidden process, or background updater.
- Tickets, gateway values, tokens, credentials, and private controller paths must not appear in release assets, size reports, logs, or public evidence.
- Every implementation task follows RED, GREEN, refactor, focused tests, `gofmt`, `go vet`, diff/security review, and one conventional commit.

---

### Task 1: Deterministic Handoff ZIP And Hard Size Gate

**Files:**
- Create: `internal/connectionentry/windows_layered_archive.go`
- Test: `internal/connectionentry/windows_layered_archive_test.go`
- Modify: `internal/connectionentry/windows_layered.go`
- Modify: `internal/connectionentry/plan.go`
- Test: `internal/connectionentry/windows_layered_test.go`

**Interfaces:**
- Produces: `const maxWindowsLayeredHandoffBytes int64 = 1 << 20`.
- Produces: `writeWindowsLayeredArchive(path string, generatedAt time.Time, files []windowsLayeredArchiveFile) (windowsLayeredArchiveReport, error)`.
- Produces: `windowsLayeredArchiveReport{Path string, SizeBytes int64, SHA256 string}`.
- ZIP entries use basename-only safe paths, deterministic UTC timestamps, mode `0600`, and `zip.Deflate`.

- [ ] **Step 1: Write failing archive tests**

  Add tests that create the same file set in different input orders and require byte-identical ZIP output; reject duplicate, absolute, traversal, separator, drive/ADS, Windows reserved-device (including superscript digit aliases), forbidden-character, trailing-dot/space, control-character, and case/normalization-collision names; verify every entry is `0600`; and reject an incompressible archive whose final size is 1,048,577 bytes or larger. Require atomic Windows `CREATE_NEW` with a protected DACL limited to current user, SYSTEM, and Administrators before any content write, with a Windows-only execution test and cross-compile check. Add a materialization test requiring `Windows-ConnectionEntry.zip`, verified private protection, its SHA-256/size checks, and absence of ticket, gateway, token, and source absolute-path values from archive reports, all plan check details, logs, and public metadata. Private runnable launcher content may carry the signed join-manifest URL/root.

- [ ] **Step 2: Confirm RED**

  Run `rtk test go test ./internal/connectionentry -run 'TestWriteWindowsLayeredArchive|TestWindowsLayeredHandoffArchive' -count=1`.

  Expected: FAIL because `writeWindowsLayeredArchive` and the archive output do not exist.

- [ ] **Step 3: Implement the minimal deterministic archive**

  Sort cloned entries by name, reject names unsafe under Windows basename semantics, create the temporary file with platform-private protection already installed, validate protection before writing, write a ZIP with fixed metadata, close it, hash and measure the closed file, remove it on any failure or oversize result, then atomically rename it into place. Revalidate after rename and truncate/remove any privacy-invalid publication while preserving cleanup errors. Include the six layered handoff files and an archive-recovery instruction that invokes `rdev-bootstrap` with the signed recovery profile. Add archive path, digest, size, and verified-private status to the package plan/checks without exposing private inputs.

- [ ] **Step 4: Confirm GREEN and verify**

  Run `rtk test go test ./internal/connectionentry -run 'TestWriteWindowsLayeredArchive|TestWindowsLayeredHandoffArchive|TestWindowsConnectionEntryPrefersLayered' -count=1`, `rtk proxy gofmt -w internal/connectionentry/windows_layered_archive.go internal/connectionentry/windows_layered_archive_test.go internal/connectionentry/windows_layered.go internal/connectionentry/plan.go internal/connectionentry/windows_layered_test.go`, and `rtk proxy go vet ./internal/connectionentry`.

  Expected: PASS.

- [ ] **Step 5: Review and commit**

  Run `rtk git diff --check`, inspect `rtk git diff`, verify no session secret entered the archive report, and commit `feat: enforce Windows handoff size gate`.

### Task 2: Separate Downloader Policy From HTTP Transport

**Files:**
- Modify: `internal/assetdownload/downloader.go`
- Create: `internal/assetdownload/transport.go`
- Create: `internal/assetdownload/http_transport.go`
- Test: `internal/assetdownload/downloader_test.go`
- Modify callers in: `internal/bootstrapcmd/bootstrapcmd.go`

**Interfaces:**
- Produces: `type Transport interface { Fetch(context.Context, TransportRequest) (TransportResponse, error) }`.
- Produces: `TransportRequest{URL string, Offset int64, MaxBytes int64}`.
- Produces: `TransportResponse{StatusCode int, ContentLength int64, Body io.ReadCloser}`.
- Changes: `assetdownload.Options` replaces `Client *http.Client` with required `Transport Transport`.
- Produces: `HTTPTransport{Client *http.Client}` as the standard adapter.

- [ ] **Step 1: Write failing transport-contract tests**

  Add a recording in-memory transport and require `Download` to request offset zero, resume from the exact `.part` size, restart when an offset request returns status 200, reject status outside 200/206, enforce signed response size before promotion, preserve retries/cancellation, and leave the previous verified output available until replacement succeeds. Retain all existing cache, digest, and transcript assertions.

- [ ] **Step 2: Confirm RED**

  Run `rtk test go test ./internal/assetdownload -run 'TestDownload.*Transport|TestDownload' -count=1`.

  Expected: FAIL because the transport contract is absent.

- [ ] **Step 3: Refactor without changing downloader ownership**

  Move only request/response mechanics into `HTTPTransport.Fetch`. Keep mirror order, Range offsets, retry/backoff, partial files, byte limits, SHA-256, cache validation, and promotion in `Download`. Require every caller to choose a transport explicitly. Replace remove-then-rename with a platform atomic replacement helper that never deletes a valid destination before the replacement operation succeeds.

- [ ] **Step 4: Confirm GREEN and verify**

  Run `rtk test go test ./internal/assetdownload ./internal/bootstrapcmd -count=1`, format touched Go files, and run `rtk proxy go vet ./internal/assetdownload ./internal/bootstrapcmd`.

  Expected: PASS with existing behavior and new injected-transport coverage.

- [ ] **Step 5: Review and commit**

  Run `rtk git diff --check`, inspect retry/atomic behavior and every caller, then commit `refactor: separate asset download transport`.

### Task 3: Build The Focused Windows Bootstrap Under The Final ZIP Budget

**Files:**
- Create: `internal/bootstrapcmd/windowsentry/app.go`
- Create: `internal/bootstrapcmd/windowsentry/curl_transport_windows.go`
- Create: `internal/bootstrapcmd/windowsentry/curl_transport_stub.go`
- Test: `internal/bootstrapcmd/windowsentry/app_test.go`
- Test: `internal/bootstrapcmd/windowsentry/curl_transport_windows_test.go`
- Modify: `cmd/rdev-bootstrap/main.go`
- Modify: `scripts/release/build-artifacts.sh`
- Test: `scripts/release_contract_test.go`

**Interfaces:**
- Produces: focused `rdev-bootstrap layered-run` only in the Windows release build.
- Produces: a command transport that resolves only `%SystemRoot%\System32\curl.exe`, disables redirects, permits HTTPS only, writes response headers/body to bounded private temporary files, and returns `assetdownload.TransportResponse`.
- Build flags: `-trimpath -gcflags=all=-l -ldflags "-s -w -buildid= ..."` for `rdev-bootstrap` only.

- [ ] **Step 1: Write failing bootstrap and real-size tests**

  Add tests proving the focused app rejects every subcommand except `layered-run`, rejects non-system curl paths and unsafe URLs, verifies a signed manifest before any core request, calls `assetdownload.Download` once for the selected core, and never requests optional helpers. Extend the release contract test to cross-build Windows amd64, materialize the representative final ZIP with the signed bootstrap, and require its closed-file size at or below 1,048,576 bytes.

- [ ] **Step 2: Confirm RED**

  Run `rtk test go test ./internal/bootstrapcmd/windowsentry ./scripts -run 'TestWindowsEntry|TestReleaseWindowsLayeredHandoffSize' -count=1`.

  Expected: FAIL because the focused package and real final-size gate are absent; the current measured ZIP is about 2.88 MiB.

- [ ] **Step 3: Implement focused linking and command transport**

  Move layered-only code behind the focused app, inject the command transport into `assetdownload.Download`, retain strict manifest decoding and release verification, and remove legacy upgrade/gzip helper behavior from the connection command. The transport may fetch bytes only; it may not own Range state, retry, checksum, cache, or promotion. Tune only the documented compiler flags and remove unreachable first-contact code until the actual signed representative ZIP passes.

- [ ] **Step 4: Confirm GREEN and verify**

  Run the focused tests, `rtk test go test ./internal/assetdownload ./internal/bootstrapcmd ./cmd/rdev-bootstrap ./scripts -count=1`, cross-build Windows amd64, and inspect the recorded PE and final ZIP byte counts.

  Expected: PASS and final ZIP `size_bytes <= 1048576`.

- [ ] **Step 5: Review and commit**

  Run `rtk git diff --check`, verify the size report contains no private path or session material, and commit `perf: fit Windows entry handoff under one MiB`.

### Task 4: Add Private Attempt State And At-Most-One Core Start

**Files:**
- Create: `internal/bootstrapcmd/windowsentry/attempt.go`
- Test: `internal/bootstrapcmd/windowsentry/attempt_test.go`
- Modify: `internal/bootstrapcmd/windowsentry/app.go`
- Test: `internal/bootstrapcmd/windowsentry/app_test.go`

**Interfaces:**
- Produces: schema `rdev.windows-layered-attempt.v1`.
- Produces stages `pre_core`, `core_started`, and `core_exited`.
- Adds required `layered-run --attempt-dir PATH --launcher powershell|powershell-bypass|cmd`.

- [ ] **Step 1: Write failing state tests**

  Require a fresh private non-UNC/non-reparse attempt directory, strict JSON decoding, legal forward-only transitions, atomic `0600` state replacement, rejection of a second `core_started`, and cancellation/exit transition after the child is reaped. Add a concurrent test whose two launchers race and whose command runner count is exactly one. Add a contract assertion that the launched core still receives `--transport auto` and that attempt state contains no selected-route field.

- [ ] **Step 2: Confirm RED**

  Run `rtk test go test ./internal/bootstrapcmd/windowsentry -run 'TestAttempt|TestLayeredRunStartsCoreOnce' -count=1`.

  Expected: FAIL because attempt state is absent.

- [ ] **Step 3: Implement the minimal state machine**

  Validate every path ancestor before opening state. Hold an exclusive attempt lock through verification and `core_started`; recheck the locked runtime; write `core_started` immediately before foreground execution; write `core_exited` only after wait returns. Return a distinct pre-core exit classification without logging URLs, local paths, or session values. Keep route selection out of this state machine and preserve `--transport auto` for the core's dynamic pool.

- [ ] **Step 4: Confirm GREEN and verify**

  Run the focused package tests with `-race` on the host platform, format touched files, and run `rtk proxy go vet ./internal/bootstrapcmd/windowsentry`.

  Expected: PASS and command runner count one.

- [ ] **Step 5: Review and commit**

  Inspect state permissions, transition ordering, cancellation, and redaction, then commit `feat: guard Windows entry core startup`.

### Task 5: Prefer PowerShell And Fall Back Through Native CMD

**Files:**
- Modify: `internal/connectionentry/windows_layered.go`
- Test: `internal/connectionentry/windows_layered_test.go`
- Modify: `internal/connectionentry/windows_layered_command_windows_test.go`
- Modify: `README.md`

**Interfaces:**
- Preferred visible entry: `Start-ConnectionEntry.ps1`.
- Visible broker/fallback: `Start-ConnectionEntry.cmd`.
- CMD modes: default broker and private `--native --attempt-dir PATH`.

- [ ] **Step 1: Replace old expectations with failing fallback tests**

  Require both launchers in the package plan, PowerShell as the preferred human entry, and broker order: current policy, process-scoped Bypass, native CMD. Require the same attempt directory on all paths, native fallback only for pre-core status, no second core after `core_started`, preserved `--transport auto`, and explicit archive output after layered exhaustion. Windows execution tests use fixture PowerShell/bootstrap commands and assert exactly one core marker for success, policy failure, runtime absence, download failure, and core-exit failure.

- [ ] **Step 2: Confirm RED**

  Run `rtk test go test ./internal/connectionentry -run 'TestWindowsConnectionEntryPrefersPowerShell|TestWindowsLayered.*Fallback' -count=1`.

  Expected: FAIL because current tests and launcher explicitly forbid PowerShell from CMD.

- [ ] **Step 3: Implement the broker and visible fallback**

  Generate both launchers. Make CMD create and secure a unique attempt directory by atomic `mkdir`, invoke PowerShell in the required order, and enter native mode only after a classified pre-core failure. Keep ACL, UNC/reparse, bootstrap digest/size, foreground execution, and cleanup checks on every path. PowerShell uses the same bootstrap and state contract. Neither launcher automatically executes the bootstrap archive profile.

- [ ] **Step 4: Confirm GREEN and verify**

  Run host tests, cross-compile the Windows test binary, run available real Windows command tests, format files, and run `rtk proxy go vet ./internal/connectionentry`.

  Expected: PASS; every fixture records at most one core start.

- [ ] **Step 5: Review and commit**

  Inspect command quoting, delayed expansion, Unicode paths, state cleanup, policy scope, secrets, and persistence strings, then commit `feat: prefer PowerShell with native CMD fallback`.

### Task 6: Add Dynamic In-Core Route Pool

**Files:**
- Create: `internal/hostcmd/route_pool.go`
- Test: `internal/hostcmd/route_pool_test.go`
- Modify: `internal/hostcmd/hostcmd.go`
- Test: `internal/hostcmd/hostcmd_test.go`

**Interfaces:**
- Produces: a bounded route pool over signed gateway candidates and maintained event transports.
- Preserves: one registered session ID, endpoint ID, lease, event cursor, and core process across route changes.
- Selection policy: immediate active-route failure switch; latency-only switch requires a healthy candidate at least 20 percent faster for two consecutive probes; failed routes cool down before re-entry.

- [ ] **Step 1: Write failing pool and continuity tests**

  Add deterministic fake-clock/probe tests for bounded concurrent initial probes, fastest healthy initial selection, immediate failover, two-probe latency hysteresis, cooldown, recovery, cancellation, and no flapping. Add a session-loop test that injects an active-route failure and requires a new gateway/transport with unchanged session ID, endpoint ID, lease identity, event cursor, and registration-call count of one.

- [ ] **Step 2: Confirm RED**

  Run `rtk test go test ./internal/hostcmd -run 'TestRoutePool|TestRunSessionTasksSwitchesRouteWithoutReregistering' -count=1`.

  Expected: FAIL because current `gatewayCandidateSet` probes sequentially and stores no latency, health history, cooldown, or transport adapter.

- [ ] **Step 3: Implement the bounded dynamic pool**

  Model safe route candidates from the signed join manifest and supported long-poll/short-poll adapters. Probe with a fixed small worker bound and per-probe timeout, keep health/latency state under one mutex, preserve the event cursor when switching, and feed failures/successes back after trust, event, task, and result requests. Exclude unsupported transports rather than adding an ad hoc protocol implementation. Keep all registration outside the route-switch loop.

- [ ] **Step 4: Confirm GREEN and verify**

  Run `rtk test go test ./internal/hostcmd -run 'TestRoutePool|TestRunSessionTasks|TestRunSessionTask' -count=1`, `rtk test go test -race ./internal/hostcmd`, format touched files, and run `rtk proxy go vet ./internal/hostcmd`.

  Expected: PASS with one registration and dynamic route changes inside one core.

- [ ] **Step 5: Review and commit**

  Inspect concurrency bounds, cursor continuity, route trust, hysteresis, cancellation, and redacted events, then commit `feat: add dynamic host route pool`.

### Task 7: Remove Legacy Full-Helper Connection Paths

**Files:**
- Modify: `internal/httpapi/server.go`
- Test: `internal/httpapi/server_test.go`
- Modify: `internal/release/candidate.go`
- Test: `internal/release/candidate_test.go`
- Modify: `internal/connectionrunner/runner.go`
- Test: `internal/connectionrunner/runner_test.go`
- Modify: `internal/agentinvite/invite.go`
- Test: `internal/agentinvite/invite_test.go`
- Modify: `internal/bootstrapcmd/bootstrapcmd.go`
- Test: `internal/bootstrapcmd/bootstrapcmd_test.go`
- Modify: `scripts/release/build-artifacts.sh`
- Test: `scripts/release_contract_test.go`

**Interfaces:**
- Project-wide invariant: generated connection launchers invoke only `rdev-bootstrap` for bootstrap and foreground host startup.
- Release builds emit `rdev-bootstrap` for every supported `GOOS/GOARCH` target.
- Archive recovery is a signed bootstrap profile, not a separate full-helper script.

- [ ] **Step 1: Write failing project-wide bootstrap-route tests**

  Render Windows PowerShell, Windows CMD, macOS/Linux shell, candidate release, runner, and join-page launchers. Require `rdev-bootstrap` and reject direct `rdev-<os>-<arch>` helper downloads, `Get-Command rdev`, `command -v rdev`, direct `rdev host serve`, direct `rdev-host`, and `rdev-bootstrap upgrade`. Add a release-contract case requiring bootstrap artifacts for every requested supported target. Keep fixture matches scoped to generated connection surfaces so documentation and historical plans do not create false positives.

- [ ] **Step 2: Confirm RED**

  Run `rtk test go test ./internal/httpapi ./internal/release ./internal/connectionrunner ./internal/agentinvite ./internal/bootstrapcmd ./scripts -run 'Test.*BootstrapOnly|TestRelease.*BootstrapTargets' -count=1`.

  Expected: FAIL because shell and PowerShell join scripts still download the full helper, generated runners directly invoke it, `upgrade` remains exposed, and release builds restrict bootstrap to Windows amd64.

- [ ] **Step 3: Route every generated connection through bootstrap**

  Replace direct helper discovery/download/launch with platform bootstrap package acquisition and signed manifest arguments. Generalize bootstrap platform selection for the supported release targets while retaining the Windows-only final-ZIP budget. Remove the legacy `upgrade` connection command. Make archive recovery select a signed recovery profile through bootstrap. Do not introduce a second downloader; all core and recovery asset downloads call `assetdownload.Download`.

- [ ] **Step 4: Confirm GREEN and verify**

  Run the focused packages, `rtk test go test ./... -count=1`, cross-build all release targets, format touched Go files, and run `rtk proxy go vet ./...`.

  Expected: PASS; generated connection content contains no legacy full-helper route.

- [ ] **Step 5: Review and commit**

  Inspect every generated launcher, release artifact list, secret boundary, and compatibility message, then commit `refactor: route connections through rdev-bootstrap`.

### Task 8: Extend Entry Acceptance Contracts

**Files:**
- Modify: `internal/acceptance/windows_temporary.go`
- Modify: `internal/acceptance/windows_temporary_package.go`
- Modify: `internal/acceptance/windows_temporary_verify.go`
- Test: corresponding `*_test.go` files
- Modify: `TASKS.md`
- Modify: `README.md`

**Interfaces:**
- Adds non-sensitive entry evidence for final ZIP size, selected launcher path, fallback attempts, core-start count, network bytes, registration duration, cache hit, and cleanup result.

- [ ] **Step 1: Write failing acceptance tests**

  Reject missing/oversized ZIP evidence, unordered fallback attempts, core-start count other than one, negative durations/bytes, warm cache miss, cold cache hit, missing active-route failure/reselection evidence, duplicate registration during route change, automatic archive execution, any legacy full-helper route, persistence residue, and reports containing ticket, gateway, token, credential, or private-path markers.

- [ ] **Step 2: Confirm RED**

  Run `rtk test go test ./internal/acceptance -run 'Test.*Windows.*LayeredEntry' -count=1`.

  Expected: FAIL because the expanded evidence schema is absent.

- [ ] **Step 3: Implement validation and documentation**

  Strictly decode the reports, enforce the measured invariants before copying evidence, checksum every copied file, and add explicit unchecked real-run items for Windows 10 and 11 amd64, physical Range resume, Defender/file-lock behavior, and process/persistence cleanup. Do not claim a real result without real evidence files.

- [ ] **Step 4: Run full verification**

  Run focused acceptance tests, `rtk test go test ./... -count=1`, `rtk proxy go vet ./...`, `rtk test go test -coverprofile=.tmp/coverage.out ./...`, and `rtk proxy go tool cover -func=.tmp/coverage.out`.

  Expected: all automated tests pass; real Windows items remain explicitly incomplete until their evidence is collected.

- [ ] **Step 5: Review and commit**

  Run `rtk git diff --check`, inspect the full task diff and redaction rules, then commit `test: require layered Windows entry evidence`.

## Follow-Up Plans

After this plan passes the actual 1 MiB delivery gate, write separate plans for:

1. signed post-registration optional components and restricted helper dispatch;
2. real Windows 10/11 acceptance execution and release-evidence packaging.
