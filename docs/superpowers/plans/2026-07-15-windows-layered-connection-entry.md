# Windows Layered Connection Entry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Windows temporary Connection Entry download only a signed core host runtime, reuse verified cached bytes, and retain the verified self-contained archive as a fallback.

**Architecture:** A signed layered asset manifest identifies core runtime and optional components by platform, relative asset path, SHA-256, size, and capability. `rdev-bootstrap` verifies the manifest with the release root, resolves a same-origin HTTPS asset URL, and delegates resumable verified caching to `internal/assetdownload`. The bootstrapper runs the cached core runtime in the foreground; optional helpers are deferred.

**Tech Stack:** Go standard library, Ed25519, `internal/release`, `internal/assetdownload`, `internal/bootstrapcmd`, `internal/httpapi`, Go tests.

## Global Constraints

- Support Windows temporary Connection Entry first; preserve archive fallback for every platform.
- Add no third-party dependency.
- Reuse `assetdownload.Download` for SHA-256 verification, cache hit, range resume, retry, and atomic `.part` promotion.
- Verify the release-root Ed25519 signature before executing a downloaded binary.
- Allow only HTTPS, same-origin relative asset paths; reject absolute URLs, traversal, query strings, fragments, and backslashes.
- Use user-scoped `0700` cache directories and `0600` cache files. Temporary mode must not create a service, scheduled task, or background updater.
- Do not include tickets, gateway URLs, tokens, or sensitive local paths in release assets or public reports.

## File Structure

- Create `internal/release/layered_assets.go` and `_test.go` for signed manifest validation and selection.
- Modify `internal/release/candidate.go` and `_test.go` to emit `layered-assets.json` and preserve the existing archive.
- Modify `internal/httpapi/server.go` and `_test.go` to serve only configured Windows host runtime assets.
- Modify `internal/bootstrapcmd/bootstrapcmd.go` and `_test.go`, plus `cmd/rdev-bootstrap/main.go` and `_test.go`, for foreground verified cache bootstrap.
- Modify `internal/cli/cli.go`, `internal/cli/cli_test.go`, `internal/acceptance/windows_temporary_package.go`, `internal/acceptance/windows_temporary_package_test.go`, `README.md`, and `TASKS.md` to prefer the layered handoff and collect measured evidence.

---

### Task 1: Define the Signed Layered Manifest

**Files:**
- Create: `internal/release/layered_assets.go`
- Test: `internal/release/layered_assets_test.go`

**Interfaces:**

```go
const LayeredAssetManifestSchemaVersion = "rdev.layered-assets.v1"

type LayeredAssetManifest struct {
	SchemaVersion string `json:"schema_version"`
	Version string `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	SigningKeyID string `json:"signing_key_id"`
	Assets []LayeredAsset `json:"assets"`
	Signature string `json:"signature"`
}

type LayeredAsset struct {
	ID string `json:"id"`
	Platform string `json:"platform"`
	Kind string `json:"kind"`
	RelativePath string `json:"relative_path"`
	SHA256 string `json:"sha256"`
	SizeBytes int64 `json:"size_bytes"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func SignLayeredAssetManifest(LayeredAssetManifest, signing.Key) (LayeredAssetManifest, error)
func VerifyLayeredAssetManifest(LayeredAssetManifest, model.TrustBundle, time.Time) error
func SelectLayeredAsset(LayeredAssetManifest, platform, kind string, capabilities []string) (LayeredAsset, error)
```

- [ ] Write `TestLayeredAssetManifestSignsVerifiesAndSelectsWindowsCore`, using a generated Ed25519 key and `rdev-host-windows-amd64` with kind `core-runtime`. Add table cases rejecting zero size, non-`sha256:` digest, duplicate ID, unknown kind, empty version, absolute path, `..`, backslash, query/fragment, and invalid signature.
- [ ] Run `rtk test go test ./internal/release -run TestLayeredAssetManifest -count=1`; expect failure because the API is absent.
- [ ] Implement canonical unsigned JSON: copy assets, sort by ID, sort capabilities, set `Signature` empty, and sign with `ed25519.Sign`. Permit only `core-runtime` and `optional-helper`; require a nonempty version and a unique matching core runtime.
- [ ] Implement this exact path predicate and call it during both signing and verification:

```go
func validRelativeAssetPath(value string) bool {
	u, err := url.Parse(value)
	if err != nil || value == "" || u.IsAbs() || u.RawQuery != "" || u.Fragment != "" { return false }
	if strings.Contains(value, `\\`) || strings.HasPrefix(value, "/") { return false }
	clean := path.Clean(value)
	return clean == value && clean != "." && !strings.HasPrefix(clean, "../")
}
```

- [ ] Run `rtk test go test ./internal/release -run 'TestLayeredAssetManifest|Test.*Bundle' -count=1`; expect PASS.
- [ ] Run `rtk proxy gofmt -w internal/release/layered_assets.go internal/release/layered_assets_test.go && rtk proxy go vet ./internal/release && rtk test go test ./internal/release`, then commit `feat: add signed layered asset manifest`.

### Task 2: Emit and Serve a Windows Core Runtime

**Files:**
- Modify: `internal/release/candidate.go`
- Test: `internal/release/candidate_test.go`
- Modify: `internal/httpapi/server.go`
- Test: `internal/httpapi/server_test.go`
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go`

**Interfaces:**

```go
const layeredAssetManifestPath = "layered-assets.json"

func WriteLayeredAssetManifest(path string, candidate Candidate, key signing.Key, now time.Time) (CandidateFile, error)
```

- [ ] Write `TestPrepareCandidateWritesSignedWindowsLayeredAssets`: build a candidate containing `rdev-host-windows-amd64.exe`, verify `layered-assets.json` with `Candidate.RootPublicKey`, and assert selection returns `assets/rdev-host-windows-amd64.exe` as `core-runtime`. Write HTTP tests for `GET /assets/rdev-host-windows-amd64.exe`, its `.sha256` sibling, unknown names, and `../` traversal.
- [ ] Run `rtk test go test ./internal/release ./internal/httpapi -run 'TestPrepareCandidateWritesSignedWindowsLayeredAssets|Test.*HostWindows.*Asset' -count=1`; expect failure.
- [ ] Add `LayeredAssetManifestPath string` to `Candidate`. After candidate artifacts are staged and before checksums are collected, write signed `layered-assets.json`; add it to `Candidate.Files` and checksums. Classify only exact basename `rdev-host-windows-amd64.exe` as the Windows core runtime. Leave all other artifacts in the existing archive and out of this manifest.
- [ ] Add `RdevHostWindowsAMD64Path string` to `httpapi.Assets` and `gatewayServeOptions`; parse `--rdev-host-windows-amd64` beside existing rdev asset flags. Extend `assetPath` with that exact flat name and use existing path validation and `.sha256` behavior. Do not add wildcard or nested asset paths.
- [ ] Run `rtk test go test ./internal/release ./internal/httpapi ./internal/cli -run 'TestPrepareCandidate|Test.*HostWindows.*Asset|TestGatewayServe' -count=1`; expect PASS.
- [ ] Run `rtk proxy gofmt -w internal/release/candidate.go internal/release/candidate_test.go internal/httpapi/server.go internal/httpapi/server_test.go internal/cli/cli.go internal/cli/cli_test.go && rtk proxy go vet ./internal/release ./internal/httpapi ./internal/cli && rtk test go test ./internal/release ./internal/httpapi ./internal/cli`, then commit `feat: publish layered Windows host assets`.

### Task 3: Add Foreground Verified Bootstrap Cache

**Files:**
- Modify: `internal/bootstrapcmd/bootstrapcmd.go`
- Test: `internal/bootstrapcmd/bootstrapcmd_test.go`
- Modify: `cmd/rdev-bootstrap/main.go`
- Test: `cmd/rdev-bootstrap/main_test.go`

**Interfaces:**

```go
type LayeredRunOptions struct {
	ManifestURL string
	Root model.TrustBundle
	Platform string
	CacheDir string
	Mode string
	Args []string
	Client *http.Client
	Now time.Time
}

type LayeredRunReport struct {
	SchemaVersion string `json:"schema_version"`
	AssetID string `json:"asset_id"`
	FromCache bool `json:"from_cache"`
	Resumed bool `json:"resumed"`
	Bytes int64 `json:"bytes"`
	Stages []LayeredRunStage `json:"stages"`
}

type LayeredRunStage struct {
	Name string `json:"name"`
	DurationMS int64 `json:"duration_ms"`
}

func RunLayeredTemporary(context.Context, LayeredRunOptions) (LayeredRunReport, string, error)
```

- [ ] Write `TestRunLayeredTemporaryUsesVerifiedDigestCache` with `httptest.NewTLSServer`: first run downloads a signed core file, second run has `FromCache=true` and does not re-fetch bytes. Add test coverage for interrupted transfer resuming with `Range`, bad signature preventing download/execution, HTTP manifest rejection, and cross-origin redirect rejection.
- [ ] Run `rtk test go test ./internal/bootstrapcmd ./cmd/rdev-bootstrap -run TestRunLayeredTemporary -count=1`; expect failure.
- [ ] Fetch only an HTTPS manifest with redirect checks that preserve origin. Decode it, call `release.VerifyLayeredAssetManifest`, select `windows/amd64` `core-runtime`, and resolve its verified relative path against the manifest URL.
- [ ] Call the existing downloader exactly once per selected core asset:

```go
result, err := assetdownload.Download(ctx, assetdownload.Options{
	Mirrors: []assetdownload.Mirror{{URL: assetURL}},
	ExpectedSHA256: asset.SHA256,
	OutputPath: filepath.Join(cacheDir, "runtime", strings.TrimPrefix(asset.SHA256, "sha256:"), filepath.Base(asset.RelativePath)),
	CachePath: filepath.Join(cacheDir, "content", strings.TrimPrefix(asset.SHA256, "sha256:")),
})
```

- [ ] Measure manifest fetch, manifest verification, runtime download/cache resolution, and runtime launch preparation with `time.Since`, and emit them as `LayeredRunStage` values. Return only asset ID, byte count, cache/resume booleans, and these non-sensitive measurements. Return the private verified executable path separately. Do not execute a child process in `internal/bootstrapcmd`.
- [ ] Add `rdev-bootstrap layered-run --manifest-url --root-public-key --platform --cache-dir --mode temporary -- <rdev-host args>`. Permit only `--mode temporary`; after `RunLayeredTemporary` succeeds, execute the returned path in the foreground with `exec.CommandContext`. Do not create a service, scheduler, registry key, or self-replacement.
- [ ] Run `rtk test go test ./internal/bootstrapcmd ./cmd/rdev-bootstrap -count=1 && rtk proxy go vet ./internal/bootstrapcmd ./cmd/rdev-bootstrap`; expect PASS. Commit `feat: bootstrap verified layered host runtime`.

### Task 4: Prefer the Layered Windows Handoff, Keep Archive Fallback

**Files:**
- Modify: `internal/release/candidate.go`
- Test: `internal/release/candidate_test.go`
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go`
- Modify: `README.md`

- [ ] Write `TestWindowsConnectionEntryPrefersLayeredBootstrapAndRetainsArchiveFallback`. With a valid layered manifest and `rdev-bootstrap.exe`, assert the generated handoff has `rdev-bootstrap.exe layered-run`, `layered-assets.json`, a pinned root key, `--platform windows/amd64`, and a user-scoped cache path. Without layered assets, assert it uses `Start-ConnectionEntry.ps1`.
- [ ] Run `rtk test go test ./internal/release ./internal/cli -run TestWindowsConnectionEntryPrefersLayeredBootstrapAndRetainsArchiveFallback -count=1`; expect failure.
- [ ] Modify the Windows launcher/template generator to prefer the layered command only if both signed layered manifest and bootstrap executable are present. Session ticket and gateway values must still come from the existing signed join manifest, not candidate assets. Preserve the existing archive command as the explicit fallback on missing layered assets or bootstrap failure.
- [ ] Add a README section for cold/warm behavior, temporary-mode no-background-update policy, cache policy, and report fields `from_cache`, `resumed`, `bytes`, and stage durations. Do not claim performance numbers.
- [ ] Run `rtk test go test ./internal/release ./internal/cli -count=1 && rtk test go test ./... && rtk proxy go vet ./...`; expect PASS. Commit `feat: prefer layered Windows connection entry`.

### Task 5: Require Cold and Warm Bootstrap Evidence in Windows Packages

**Files:**
- Modify: `internal/acceptance/windows_temporary_package.go`
- Test: `internal/acceptance/windows_temporary_package_test.go`
- Modify: `internal/acceptance/windows_temporary_verify.go`
- Test: `internal/acceptance/windows_temporary_verify_test.go`
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go`
- Modify: `README.md`
- Modify: `TASKS.md`

- [ ] Write tests requiring `cold-layered-run.json` (`from_cache=false`) and `warm-layered-run.json` (`from_cache=true`) with a signature-verification stage and nonnegative durations. Assert package verification fails for missing reports, reversed cache semantics, missing signature phase, or report text containing ticket/gateway/token patterns.
- [ ] Run `rtk test go test ./internal/acceptance -run 'Test.*Windows.*Layered' -count=1`; expect failure.
- [ ] Add those reports to Windows temporary required evidence. Validate report schema, phase names, cache semantics, durations, and redaction before copying the files and adding their SHA-256 checksums. Fixture reports must remain non-production and cannot satisfy the formal release gate alone.
- [ ] Add a `TASKS.md` unchecked item for real Windows cold/warm measurements: bytes downloaded, time to host registration, reconnect time, cache hit rate, helper isolation, and rollback result. Update README acceptance instructions with the two report names.
- [ ] Run `rtk proxy gofmt -w internal/acceptance/windows_temporary_package.go internal/acceptance/windows_temporary_package_test.go internal/acceptance/windows_temporary_verify.go internal/acceptance/windows_temporary_verify_test.go internal/cli/cli.go internal/cli/cli_test.go && rtk test go test ./internal/acceptance ./internal/cli && rtk test go test ./... && rtk proxy go vet ./...`; expect PASS. Commit `test: require layered bootstrap acceptance evidence`.

## Deferred Follow-Up

Do not add binary delta patches, automatic background updates, optional helper download, managed-mode activation/rollback, or macOS/Linux layered path in this plan. Write separate plans for those after this Windows temporary MVP has real acceptance evidence.
