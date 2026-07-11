# Resilient Public Tunnel Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a verified `tunn3l.sh` foreground fallback and an official localhost.run SSH trust anchor, while making provider priority and empty-mainland diagnostics explicit.

**Architecture:** Keep provider processes behind the existing `tunnel.Provider` and `Manager` contracts. Add small focused modules for protected artifact installation and provider trust materialization, then pass a session-owned provider root through `StartRequest`. Registry evaluation retains ineligible providers for diagnostics while `Select` returns only eligible providers in explicit priority order.

**Tech Stack:** Go 1.25 standard library, existing protected-path helpers, external provider binaries only through embedded official SHA-256 manifests, existing CLI subprocess supervisor, `httptest`, race tests, cross-compilation.

## Global Constraints

- Never use SSH TOFU, `StrictHostKeyChecking=no`, `accept-new`, or runtime `ssh-keyscan`.
- Pin tunn3l release `v0.5.1` and the four reviewed compressed SHA-256 values from the design spec.
- Invoke only foreground `tunn3l http <port> --json`; never invoke daemon, login, token, reserved-subdomain, TCP, or SSH modes.
- Store all downloaded tools, provider state, and generated pins under `<work-dir>/.rdev`.
- Keep the target Windows handoff exactly the generated short readable PowerShell command.
- `cn-mainland` remains evidence-required; an Agent-side live sample never makes a provider automatically eligible there.
- Shareable diagnostics contain only allowlisted provider IDs, phases, statuses, candidate IDs, and fixed error classes.
- Write every behavior change test-first and watch the focused test fail before implementation.

---

### Task 1: Registry evaluation and explicit automatic priority

**Files:**
- Modify: `internal/tunnel/types.go`
- Modify: `internal/tunnel/registry.go`
- Modify: `internal/tunnel/registry_test.go`

**Interfaces:**
- Produces: `ProviderMetadata.AutomaticPriority int`, `ProviderTunn3l`, and `Registry.Evaluate(Policy, []RegionalEvidence) []Selection`.
- Preserves: `Registry.Select` returns only eligible providers and detached metadata/evidence copies.

- [ ] **Step 1: Write failing priority and evaluation tests**

Add tests equivalent to:

```go
func TestRegistrySelectUsesAutomaticPriorityBeforeProviderID(t *testing.T) {
	cloudflare := newRegistryFakeProvider("cloudflare-quick", true)
	cloudflare.meta.AutomaticPriority = 10
	localhost := newRegistryFakeProvider("localhost-run", true)
	localhost.meta.AutomaticPriority = 30
	tunn3l := newRegistryFakeProvider("tunn3l", true)
	tunn3l.meta.AutomaticPriority = 20
	r, _ := NewRegistry(localhost, tunn3l, cloudflare)
	got := r.Select(Policy{Region: RegionGlobal}, nil)
	want := []string{"cloudflare-quick", "tunn3l", "localhost-run"}
	for i := range want {
		if got[i].Provider.ID() != want[i] { t.Fatalf("selection=%#v", got) }
	}
}

func TestRegistryEvaluateRetainsMainlandIneligibilityReasons(t *testing.T) {
	r, _ := NewRegistry(newRegistryFakeProvider("cloudflare-quick", true), newRegistryFakeProvider("tunn3l", true))
	got := r.Evaluate(Policy{Region: RegionCNMainland, Now: time.Now()}, nil)
	if len(got) != 2 { t.Fatalf("evaluations=%#v", got) }
	for _, item := range got {
		if item.Eligibility.Eligible || item.Eligibility.Reason != "regional-evidence-missing" {
			t.Fatalf("evaluation=%#v", item)
		}
	}
}
```

Update the canonical-ID expectation to include `tunn3l`.

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./internal/tunnel -run 'TestRegistrySelectUsesAutomaticPriority|TestRegistryEvaluateRetains|TestCanonicalProviderIDs' -count=1
```

Expected: build/test failure because the priority field, provider ID, and `Evaluate` method do not exist.

- [ ] **Step 3: Implement immutable evaluation and sorting**

Add:

```go
type ProviderMetadata struct {
	// existing fields...
	AutomaticPriority int `json:"automatic_priority,omitempty"`
}

const ProviderTunn3l = "tunn3l"
```

Refactor the registry around:

```go
func (r Registry) Evaluate(policy Policy, evidence []RegionalEvidence) []Selection {
	items := make([]Selection, 0, len(r.providers))
	for _, provider := range r.providers {
		metadata := cloneMetadata(provider.Metadata())
		items = append(items, Selection{
			Provider: provider, Metadata: metadata,
			Eligibility: EvaluateEligibility(metadata, policy, evidence),
		})
	}
	sortSelections(items, policy, evidence)
	return items
}

func (r Registry) Select(policy Policy, evidence []RegionalEvidence) []Selection {
	evaluated := r.Evaluate(policy, evidence)
	selected := make([]Selection, 0, len(evaluated))
	for _, item := range evaluated {
		if item.Eligibility.Eligible { selected = append(selected, item) }
	}
	return selected
}
```

Sorting order is: fresh verified evidence first, `DefaultAutomatic=true` first,
lower positive `AutomaticPriority` first (`0` sorts after positive priorities),
then provider ID.

- [ ] **Step 4: Run focused and package tests**

Run:

```bash
go test ./internal/tunnel -run 'TestRegistry' -count=1
go test ./internal/tunnel -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tunnel/types.go internal/tunnel/registry.go internal/tunnel/registry_test.go
git commit -m "feat: prioritize tunnel providers explicitly"
```

---

### Task 2: Protected streaming digest verification and managed gzip installer

**Files:**
- Modify: `internal/tunnel/protected_path.go`
- Modify: `internal/tunnel/protected_file_test.go`
- Create: `internal/cli/tunnel_managed_tool.go`
- Create: `internal/cli/tunnel_managed_tool_test.go`

**Interfaces:**
- Produces: `tunnel.VerifyProtectedRegularFileSHA256(path string, maxBytes int64, expected [32]byte) error`.
- Produces: `managedToolAsset`, `managedGzipInstaller`, `tunn3lManagedAsset(goos, goarch string) (managedToolAsset, bool)`, and `Ensure(context.Context, root, asset) (string, error)`.

- [ ] **Step 1: Write failing protected-digest tests**

```go
func TestVerifyProtectedRegularFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset")
	content := []byte("reviewed artifact")
	if err := os.WriteFile(path, content, 0o600); err != nil { t.Fatal(err) }
	want := sha256.Sum256(content)
	if err := VerifyProtectedRegularFileSHA256(path, 1024, want); err != nil { t.Fatal(err) }
	wrong := sha256.Sum256([]byte("wrong"))
	if err := VerifyProtectedRegularFileSHA256(path, 1024, wrong); err == nil { t.Fatal("expected digest mismatch") }
}
```

Also cover oversize and symlink/reparse rejection using existing platform test helpers.

- [ ] **Step 2: Run the digest test and verify RED**

Run:

```bash
go test ./internal/tunnel -run TestVerifyProtectedRegularFileSHA256 -count=1
```

Expected: build failure because the function does not exist.

- [ ] **Step 3: Implement same-handle streaming verification**

Use `openProtectedPath`, `Stat`, `Lstat`, `os.SameFile`, existing permission
validation, `io.LimitReader`, and `sha256.New`. Reject limits below 1, nonregular
files, path replacement, content over the limit, and digest mismatch. Do not
read the whole binary into memory.

- [ ] **Step 4: Write failing asset-map and installer tests**

The table must assert the exact four tuples and digests from the spec. Add a TLS
`httptest` whose body is a generated gzip stream and whose expected digest is
computed in the test. Assert:

```go
path, err := installer.Ensure(ctx, protectedRoot, asset)
if err != nil { t.Fatal(err) }
if got, _ := os.ReadFile(path); !bytes.Equal(got, expanded) { t.Fatalf("got %q", got) }
if mode := mustStat(t, path).Mode().Perm(); runtime.GOOS != "windows" && mode != 0o700 {
	t.Fatalf("mode=%#o", mode)
}
```

Separate tests cover cached compressed reuse without a request, non-2xx,
HTTP redirect downgrade, more than five redirects, compressed body over 64 MiB,
expanded body over 128 MiB, digest mismatch, canceled context, and concurrent
`Ensure` calls returning verified executable content.

- [ ] **Step 5: Run installer tests and verify RED**

Run:

```bash
go test ./internal/cli -run 'TestTunn3lManagedAsset|TestManagedGzipInstaller' -count=1
```

Expected: build failure because the installer types do not exist.

- [ ] **Step 6: Implement the installer**

Define the fixed manifest:

```go
const tunn3lManagedVersion = "v0.5.1"

type managedToolAsset struct {
	Name, URL, CompressedSHA256 string
}

var tunn3lManagedAssets = map[string]managedToolAsset{
	"darwin/arm64": {"tunn3l-darwin-arm64.gz", "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-darwin-arm64.gz", "360669bd64595709cdc111e9bf430040c4608ad823582d035f955464fa1f45e4"},
	"darwin/amd64": {"tunn3l-darwin-x64.gz", "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-darwin-x64.gz", "35d559e55cbd40afcaf3acbe806020b7cafd9d8559d3fb6db2c3d16844c10bd6"},
	"linux/arm64": {"tunn3l-linux-arm64.gz", "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-linux-arm64.gz", "9df47cad6d1e09313e5b01f76c69e4cde0c901ee424fa7943269cd101db3b1e1"},
	"linux/amd64": {"tunn3l-linux-x64.gz", "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-linux-x64.gz", "902bc626033efb7bddde141542a145d95f55d256bd310e439bc71290a0ad6d58"},
}
```

Implementation rules:

- create only session-owned `0700` directories;
- download with an injected `*http.Client`, fixed HTTPS initial URL, HTTPS-only
  redirects, allowed hosts `github.com` and `release-assets.githubusercontent.com`,
  five-redirect maximum, and context-bound request;
- write unique temp files in the destination directory, hash while copying,
  `Sync`, close, validate, and atomic rename;
- verify cached gzip on every reuse;
- decompress verified gzip through `io.LimitReader`, write a unique executable,
  hash expanded bytes, `Sync`, close, chmod `0700` on Unix, and atomically
  publish it as `tunn3l-<expanded-sha256>`; a concurrent winner is accepted only
  after same-handle verification against the expanded digest derived from the
  pinned gzip stream;
- remove every temporary file on error.

- [ ] **Step 7: Run focused tests, race tests, and vet**

```bash
go test ./internal/tunnel ./internal/cli -run 'TestVerifyProtectedRegularFileSHA256|TestTunn3lManagedAsset|TestManagedGzipInstaller' -count=1
go test -race ./internal/cli -run TestManagedGzipInstaller -count=10
go vet ./internal/tunnel ./internal/cli
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tunnel/protected_path.go internal/tunnel/protected_file_test.go internal/cli/tunnel_managed_tool.go internal/cli/tunnel_managed_tool_test.go
git commit -m "feat: install verified tunnel tools"
```

---

### Task 3: Managed tunn3l foreground provider

**Files:**
- Modify: `internal/tunnel/types.go`
- Modify: `internal/cli/tunnel.go`
- Create: `internal/cli/tunnel_tunn3l_test.go`
- Modify: `internal/cli/tunnel_test.go`

**Interfaces:**
- Extends: `StartRequest.ProviderRoot string`.
- Produces: `newTunn3lProvider(io.Writer, managedGzipInstaller) tunnel.Provider`, `startTunn3lTunnel`, and a canonical `tunn3l.sh` candidate parser.

- [ ] **Step 1: Write failing parser, argv, environment, and lifecycle tests**

Add table tests accepting only rootless HTTPS strict subdomains of `tunn3l.sh`
and rejecting the apex, ports, paths, query/fragment, userinfo, wildcard-like
labels, and `tunn3l.sh.attacker.example`.

Use a fake executable/helper process to assert the provider invokes exactly:

```text
<verified-path> http 8787 --json
```

and sets a session-scoped home plus fixed relay while removing
`TUNN3L_TOKEN`, `TUNN3L_SUBDOMAIN`, `NODE_TLS_REJECT_UNAUTHORIZED`,
`NODE_EXTRA_CA_CERTS`, and caller-provided `TUNN3L_RELAY`.

Add a process test proving the provider stays alive after candidate assignment
and is canceled/reaped by `Handle.Stop`.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/cli -run 'TestTunn3l|TestCanonicalProviderURL.*Tunn3l' -count=1
```

Expected: build/test failure because the provider and ID are absent.

- [ ] **Step 3: Implement provider process options**

Extend the subprocess path with a small immutable options value:

```go
type providerProcessOptions struct {
	WorkingDirectory string
	Env              []string
}
```

Existing providers pass zero options. `startProviderProcess` assigns `cmd.Dir`
and `cmd.Env` only from this value. No shell wrapper is introduced.

- [ ] **Step 4: Implement tunn3l provider**

The provider:

1. validates `LocalPort` and `ProviderRoot`;
2. resolves the current platform asset;
3. calls the managed installer under `<root>/tools/tunn3l/v0.5.1`;
4. creates `<root>/provider-state/tunn3l/home` as `0700`;
5. starts the verified binary with `http <port> --json` and the sanitized env;
6. returns a candidate with provider ID `tunn3l` and parsed HTTPS URL;
7. uses the existing process lifecycle and bounded output sink.

Set metadata to `DefaultAutomatic=true`, `AutomaticPriority=20`, executable
`rdev-managed:tunn3l-v0.5.1`, and failure-domain fields matching the reviewed
public service architecture. Give the provider a 90-second internal startup
budget and increase the manager startup ceiling to 120 seconds so a bounded
first download is not canceled prematurely.

- [ ] **Step 5: Run focused, package, race, and cross-build checks**

```bash
go test ./internal/cli -run 'TestTunn3l|TestTunnelProvider' -count=1
go test -race ./internal/cli -run 'TestTunn3l|TestTunnelProvider' -count=10
GOOS=windows GOARCH=amd64 go test -c ./internal/cli -o /tmp/rdev-cli-windows.test.exe
```

Expected: PASS. Windows compilation must succeed even though the runtime asset
resolver reports unsupported on Windows.

- [ ] **Step 6: Commit**

```bash
git add internal/tunnel/types.go internal/cli/tunnel.go internal/cli/tunnel_tunn3l_test.go internal/cli/tunnel_test.go
git commit -m "feat: add managed tunn3l provider"
```

---

### Task 4: Official localhost.run trust anchor

**Files:**
- Create: `internal/cli/tunnel_trust.go`
- Create: `internal/cli/tunnel_trust_test.go`
- Modify: `internal/cli/tunnel.go`
- Modify: `internal/cli/tunnel_test.go`

**Interfaces:**
- Produces: `providerTrustAnchor`, `localhostRunTrustAnchor`, and `materializeProviderKnownHosts(root string, anchor providerTrustAnchor) (string, error)`.
- Changes: localhost.run uses an operator path when configured, otherwise the embedded reviewed anchor.

- [ ] **Step 1: Write failing provenance, fingerprint, and materialization tests**

The production `localhostRunTrustAnchor` must contain the exact reviewed
official line below as its single source of truth:

```text
localhost.run ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC3lJnhW1oCXuAYV9IBdcJA+Vx7AHL5S/ZQvV2fhceOAPgO2kNQZla6xvUwoE4iw8lYu3zoE1KtieCU9yInWOVI6W/wFaT/ETH1tn55T2FVsK/zaxPiHZVJGLPPdEEid0vS2p1JDfc9onZ0pNSHLl1QusIOeMUyZ2bUMMLLgw46KOT9S3s/LmxgoJ3PocVUn5rVXz/Dng7Y8jYNe4IFrZOAUsi7hNBa+OYja6ceefpDvNDEJ1BdhbYfGolBdNA7f+FNl0kfaWru4Cblr843wBe2ckO/sNqgeAMXO/qH+SSgQxUXF2AgAw+TGp3yCIyYoOPvOgvcPsQziJLmDbUuQpnH
```

The test reads that production anchor (it must not duplicate the long key),
decodes the key blob, computes SHA-256, encodes raw base64 without padding, and
assert `SHA256:FV8IMJ4IYjYUTnd6on7PqbRjaZf4c1EhhEBgeUdE94I`. Assert the source commit and
immutable GitHub URL are nonempty and fixed. Materialization must create a
private exact-host file that passes `validateKnownHostsFile`.

Add tests for idempotent reuse, tampered existing snapshot fail-closed behavior,
and operator-file override.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/cli -run 'TestLocalhostRunTrustAnchor|TestMaterializeProviderKnownHosts' -count=1
```

Expected: build failure because trust-anchor types do not exist.

- [ ] **Step 3: Implement trust materialization**

Define:

```go
type providerTrustAnchor struct {
	ProviderID, Host, KeyLine, Fingerprint, SourceCommit, SourceURL, ReviewedAt string
	Port int
}
```

Validate every constant before writing: canonical provider ID, exact host/port,
key-line host, supported key type, base64 key, computed fingerprint, immutable
source commit in URL, and canonical review date. Write to
`<root>/provider-trust/<provider>/known_hosts` via unique temp + sync + atomic
rename, mode `0600`, then pass it through the existing protected known-hosts
validator. Existing unexpected content is an integrity failure, not silently
overwritten.

- [ ] **Step 4: Wire localhost.run provider fallback**

In `cliTunnelProvider.Start`, when provider ID is `localhost-run` and neither
`request.KnownHostsFile` nor the configured operator path is present, materialize
the built-in anchor from `request.ProviderRoot`. Keep all existing SSH argv
hardening. Set metadata `DefaultAutomatic=true`, `AutomaticPriority=30`, and
`RequiresSSHPin=false` because the release supplies the pin.

Pinggy remains `DefaultAutomatic=false`, `AutomaticPriority=40`, and
`RequiresSSHPin=true`.

- [ ] **Step 5: Run focused, platform, and package tests**

```bash
go test ./internal/cli -run 'TestLocalhostRunTrustAnchor|TestMaterializeProviderKnownHosts|TestStartSSHTunnel|TestValidateKnownHostsFile' -count=1
GOOS=windows GOARCH=amd64 go test -c ./internal/cli -o /tmp/rdev-cli-windows.test.exe
go test ./internal/cli -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/tunnel_trust.go internal/cli/tunnel_trust_test.go internal/cli/tunnel.go internal/cli/tunnel_test.go
git commit -m "feat: pin localhost run host identity"
```

---

### Task 5: Support-session selection diagnostics and provider integration

**Files:**
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `internal/cli/support_session_diagnostic.go`
- Modify: `internal/cli/support_session_diagnostic_test.go`
- Modify: `internal/cli/tunnel.go`

**Interfaces:**
- Produces: `availabilityFromEligibilityEvaluations([]tunnel.Selection, tunnel.RegionProfile) tunnel.AvailabilitySet`.
- Changes: support-session passes `ProviderRoot`, starts only selected providers, and reports empty selection before static probing.

- [ ] **Step 1: Write failing integration tests**

Add/adjust tests for:

- default provider metadata count is four and includes tunn3l;
- default global selection order is Cloudflare, tunn3l, localhost.run;
- Pinggy is absent unless an explicit allowlist enables it;
- an explicitly allowed pin-required provider with no valid reviewed pin is
  retained as skipped with `ssh-pin-missing` or `ssh-pin-invalid` and is never
  started;
- `TestSupportSessionStartMainlandFailureCleanup` decodes the status payload and
  requires phase `provider-selection`, reason
  `no_public_gateway_provider_eligible`, skipped attempts with
  `regional-evidence-missing`, zero provider starts, and no occurrence of
  `static-bootstrap-probe`;
- a fake Cloudflare DNS failure followed by a healthy tunn3l candidate reaches
  handoff generation;
- `StartRequest.ProviderRoot` equals `<work-dir>/.rdev`.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/cli -run 'TestTunnelProvidersReports|TestSupportSessionStartMainlandFailureCleanup|TestSupportSessionStartNXDOMAINFallbackCleanup|TestSupportSession.*ProviderRoot' -count=1
```

Expected: failure because provider count/order, provider root, and diagnostic
phase are not implemented.

- [ ] **Step 3: Integrate the registry and provider root**

Construct the default registry with Cloudflare, tunn3l, localhost.run, and
Pinggy. Build one `tunnel.Policy` value, call `Registry.Evaluate`, derive
configuration-adjusted evaluations through a CLI preflight, derive `selections`
by filtering eligible entries, and pass:

```go
tunnel.StartRequest{
	LocalURL: localListenURL,
	LocalPort: localPort,
	ProviderRoot: filepath.Join(workDir, ".rdev"),
}
```

For global startup set `AllowNonDefault` only when an explicit restrictive
provider policy is present. This keeps operator-pin-only providers out of the
automatic pool. The configuration preflight resolves the same provider-specific
known-hosts path used by `Start`, validates exact host/port and permissions, and
changes otherwise eligible pin-required entries to fixed reasons
`ssh-pin-missing` or `ssh-pin-invalid` without exposing the path.

- [ ] **Step 4: Implement empty-selection diagnostics**

Map each evaluation to a redacted skipped attempt:

```go
Attempt{
	ProviderID: item.Metadata.ID,
	Status: AttemptSkipped,
	ErrorClass: safeEligibilityReason(item.Eligibility.Reason),
}
```

When `len(selections)==0`, write:

```go
newSupportSessionStartDiagnostic(
	"provider-selection",
	"no_public_gateway_provider_eligible",
	"review-provider-eligibility",
	availability,
)
```

and return `errors.New("no public gateway provider is eligible for the selected region")` before creating a manager runtime or running bootstrap probes. Add the new fixed phase/reason/action to diagnostic allowlists; unknown values still fail closed.

- [ ] **Step 5: Fix read-only probe consistency**

Make `rdev tunnel probe` and support-session selection use the same provider
preflight used by Start. A
malformed, wrong-host, or unsafe operator known-hosts file must report
`configuration_ready=false`; the built-in localhost anchor and supported
tunn3l managed asset report ready without exposing paths or key material.

- [ ] **Step 6: Run focused and full CLI tests**

```bash
go test ./internal/cli -run 'TestTunnelProviders|TestTunnelProbe|TestSupportSessionStartMainland|TestSupportSessionStartNXDOMAIN' -count=1
go test ./internal/cli -count=1
go vet ./internal/cli
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go internal/cli/support_session_diagnostic.go internal/cli/support_session_diagnostic_test.go internal/cli/tunnel.go
git commit -m "fix: report tunnel eligibility before probing"
```

---

### Task 6: Live readiness, Windows target connection, documentation, and final review

**Files:**
- Modify: `internal/cli/tunnel_live_test.go`
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-07-10-bootstrap-rendezvous-multi-tunnel-design.md`
- Modify: `skills/safe-remote-support/SKILL.md`
- Modify additional existing tunnel-operation docs only where they describe the old default order.

**Interfaces:**
- Produces: opt-in `TestLiveTunn3lReadiness` and updated operator guidance.

- [ ] **Step 1: Add the opt-in live test**

Keep `RDEV_LIVE_TUNNEL_TEST=1` as the explicit guard. Start an `httptest` origin
with `/healthz` and an instance marker, create the real managed tunn3l provider
under `t.TempDir`, and retry `ProbeGatewayHealth` for at most 60 seconds. Log
only attempt count and fixed probe stage. Always stop/reap the provider.

- [ ] **Step 2: Run the live readiness test**

```bash
RDEV_LIVE_TUNNEL_TEST=1 go test -v ./internal/cli -run TestLiveTunn3lReadiness -count=1
```

Expected: PASS with the current public service; if it fails, record the fixed
stage and do not send a handoff.

- [ ] **Step 3: Run the real foreground support session**

Discover flags with:

```bash
go run ./cmd/rdev support-session connect --help
```

Then run the standard foreground `connect --start` with the existing real-Windows
work directory and `--region global`. Do not background it. Verify G1-G5, then
send exactly `target_handoff_envelope.full_text` to the target user.

- [ ] **Step 4: Complete target connection and smoke tests**

Wait no more than three minutes for `connected=true`. Use the active public
gateway URL explicitly for status/report calls. After connection run:

```bash
rdev support-session report --gateway-url <ACTIVE_GATEWAY_URL> --ticket-code <TICKET>
rdev support-session smoke-test --gateway-url <ACTIVE_GATEWAY_URL> --host-id <RECOMMENDED_JOB_HOST_ID>
rdev support-session smoke-test --gateway-url <ACTIVE_GATEWAY_URL> --host-id <RECOMMENDED_JOB_HOST_ID> --remote-control
```

The low-risk remote-control probe may only list files and inspect windows. Do not
disconnect automatically.

- [ ] **Step 5: Update docs and skill guidance**

Document the actual order: stable gateway, Cloudflare, managed tunn3l,
localhost.run official pin, then explicit operator-pin providers. State that
anonymous providers are candidates, not guaranteed mainland services, and that
`cn-mainland` requires fresh carrier evidence. Preserve the one-command and
foreground-process rules.

- [ ] **Step 6: Run final verification**

```bash
go test ./... -count=1
go vet ./...
go test -race ./internal/tunnel -count=1
go test -race ./internal/cli -run 'TestTunn3l|TestTunnelProvider|TestSupportSessionStartNXDOMAINFallbackCleanup' -count=10
GOOS=windows GOARCH=amd64 go test -c ./internal/tunnel -o /tmp/rdev-tunnel-windows.test.exe
GOOS=windows GOARCH=amd64 go test -c ./internal/cli -o /tmp/rdev-cli-windows.test.exe
GOOS=windows GOARCH=amd64 go build -o /tmp/rdev-windows-amd64.exe ./cmd/rdev
GOOS=linux GOARCH=amd64 go build -o /tmp/rdev-linux-amd64 ./cmd/rdev
git diff --check
```

Expected: all commands exit 0. Review `git diff` for secrets, raw URLs, key
material outside the reviewed trust-anchor constant, unsafe argv, or unrelated
workspace changes.

- [ ] **Step 7: Request independent code and security review**

Review must cover managed artifact provenance, redirect policy, cache races,
provider environment sanitization, trust-anchor rotation, candidate parsing,
diagnostic redaction, process cleanup, and the real Windows evidence.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/tunnel_live_test.go README.md docs/superpowers/specs/2026-07-10-bootstrap-rendezvous-multi-tunnel-design.md skills/safe-remote-support/SKILL.md
git commit -m "docs: document resilient tunnel fallback"
```
