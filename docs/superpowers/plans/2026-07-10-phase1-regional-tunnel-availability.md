# Regional Tunnel Availability Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace first-success public-tunnel startup with a typed, region-aware availability set that treats mainland China evidence as mandatory, supervises every provider process, and reports direct Windows bootstrap as degraded unless the operator explicitly accepts it.

**Architecture:** Add a focused `internal/tunnel` package for provider contracts, regional evidence, selection, process lifecycle, public probes, and readiness. Keep CLI-specific environment parsing and subprocess construction in `internal/cli/tunnel.go`, then map the typed availability result into `internal/supportsession` contracts. Reorder foreground startup so the local gateway is healthy before public providers start and tickets/handoffs are created only after provider health is known.

**Tech Stack:** Go 1.25 standard library, existing `internal/httpapi`, `internal/supportsession`, `internal/model`, table-driven tests, `httptest`, injected fake providers/handles/probers.

## Global Constraints

- Phase 1 does not implement rendezvous, descriptor quorum, fingerprint pairing, or the signed `rdev-connect` release.
- Windows is a primary platform; all production parsing and policy tests must be cross-platform.
- No hidden persistence, service installation, scheduled task, Run key, firewall/DNS/proxy/route mutation, UAC bypass, Defender bypass, or inbound public port.
- Provider commands are argv arrays executed directly; never invoke a shell wrapper.
- `StrictHostKeyChecking=no` is forbidden. SSH providers require an existing reviewed known-hosts file or SSH CA/pin policy before automatic startup.
- `cn-mainland` is evidence-required. Unknown, stale, malformed, or hard-failed regional evidence is not eligible for automatic startup.
- Global operator-side health probes do not count as mainland representative evidence.
- Direct Phase 1 handoffs have `degraded_single_entry=true`. Default `ready_to_send=false`; only `--allow-degraded-direct-handoff` may set `ready_to_send=true`, while readiness remains `degraded-single-entry`.
- Reliable provider independence is based on DNS, edge/CDN, origin, control-plane, and certificate failure domains, never provider brand alone.
- Provider credentials, target IP addresses, ticket codes, URLs, and raw descriptors must not appear in shareable evidence.
- Preserve compatibility fields such as `ready_to_send_to_human`, but derive them from the new readiness object rather than asset existence or ticket creation.
- Preserve unrelated uncommitted user changes in the original workspace. Execute this plan in an isolated `codex/` worktree.

---

### Task 1: Regional evidence and provider contracts

**Files:**
- Create: `internal/tunnel/types.go`
- Create: `internal/tunnel/region.go`
- Create: `internal/tunnel/region_test.go`

**Interfaces:**
- Produces: `Provider`, `Handle`, `Candidate`, `ProviderMetadata`, `FailureDomains`, `RegionalEvidence`, `Policy`, `Eligibility`, `EvaluateEligibility`, and immutable clone helpers used by every later task.
- Consumes: only Go standard library types.

- [ ] **Step 1: Write failing regional-evidence tests**

Create `internal/tunnel/region_test.go` with table tests covering verified, unknown, stale, future-issued, hard-failed, wrong-region, and expired evidence:

```go
package tunnel

import (
	"testing"
	"time"
)

func validMainlandSamples() []NetworkSample {
	return []NetworkSample{
		{Carrier: "china-telecom", Region: "east-cn", Success: true},
		{Carrier: "china-telecom", Region: "west-cn", Success: true},
		{Carrier: "china-unicom", Region: "north-cn", Success: true},
		{Carrier: "china-unicom", Region: "south-cn", Success: true},
		{Carrier: "china-mobile", Region: "central-cn", Success: true},
		{Carrier: "china-mobile", Region: "coastal-cn", Success: true},
	}
}

func TestEvaluateEligibilityRequiresFreshMainlandEvidence(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	meta := ProviderMetadata{ID: "provider-a", DefaultAutomatic: true}
	tests := []struct {
		name     string
		evidence []RegionalEvidence
		want     bool
		reason   string
	}{
		{name: "missing", want: false, reason: "regional-evidence-missing"},
		{name: "unknown", evidence: []RegionalEvidence{{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceUnknown, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()}}, want: false, reason: "regional-evidence-not-verified"},
		{name: "expired", evidence: []RegionalEvidence{{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Second), Samples: validMainlandSamples()}}, want: false, reason: "regional-evidence-expired"},
		{name: "hard failed", evidence: []RegionalEvidence{{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceBlocked, Issuer: "probe", ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()}}, want: false, reason: "regional-evidence-blocked"},
		{name: "verified", evidence: []RegionalEvidence{{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(6 * 24 * time.Hour), Samples: validMainlandSamples()}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateEligibility(meta, Policy{Region: RegionCNMainland, Now: now}, tt.evidence)
			if got.Eligible != tt.want || got.Reason != tt.reason {
				t.Fatalf("EvaluateEligibility() = %#v, want eligible=%v reason=%q", got, tt.want, tt.reason)
			}
		})
	}
}

func TestRegionalEvidenceValidateRejectsTargetIPAndLongTTL(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	base := RegionalEvidence{
		ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified,
		Issuer: "project-probe", ObservedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour),
		Samples: validMainlandSamples(),
	}
	if err := base.Validate(); err != nil { t.Fatalf("valid evidence: %v", err) }
	tooLong := base
	tooLong.ExpiresAt = now.Add(7*24*time.Hour + time.Second)
	if err := tooLong.Validate(); err == nil { t.Fatal("expected TTL rejection") }
	withIP := base
	withIP.Samples = []NetworkSample{{Carrier: "china-telecom", Region: "203.0.113.4", Success: true}}
	if err := withIP.Validate(); err == nil { t.Fatal("expected IP-like region rejection") }
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `go test ./internal/tunnel -run 'TestEvaluateEligibility|TestRegionalEvidence' -count=1`

Expected: build failure because the `internal/tunnel` types and functions do not exist.

- [ ] **Step 3: Implement the immutable contracts and eligibility rules**

Create `internal/tunnel/types.go` with these public shapes:

```go
package tunnel

import (
	"context"
	"time"
)

const AvailabilitySchemaVersion = "rdev.tunnel-availability.v1"
const ReadinessSchemaVersion = "rdev.connection-readiness.v2"

type RegionProfile string
const (
	RegionGlobal RegionProfile = "global"
	RegionCNMainland RegionProfile = "cn-mainland"
)

type EvidenceStatus string
const (
	EvidenceUnknown EvidenceStatus = "unknown"
	EvidenceCandidate EvidenceStatus = "candidate"
	EvidenceVerified EvidenceStatus = "verified"
	EvidenceDegraded EvidenceStatus = "degraded"
	EvidenceBlocked EvidenceStatus = "blocked"
)

type FailureDomains struct {
	AuthoritativeDNS string `json:"authoritative_dns,omitempty"`
	EdgeNetwork string `json:"edge_network,omitempty"`
	OriginNetwork string `json:"origin_network,omitempty"`
	ControlPlane string `json:"control_plane,omitempty"`
	CertificateDependency string `json:"certificate_dependency,omitempty"`
}

type ProviderMetadata struct {
	ID string `json:"id"`
	DisplayName string `json:"display_name"`
	Protocols []string `json:"protocols"`
	Anonymous bool `json:"anonymous"`
	CredentialRequirement string `json:"credential_requirement,omitempty"`
	Executable string `json:"executable"`
	DocumentationURL string `json:"documentation_url"`
	TermsURL string `json:"terms_url,omitempty"`
	DefaultAutomatic bool `json:"default_automatic"`
	RequiresSSHPin bool `json:"requires_ssh_pin"`
	FailureDomains FailureDomains `json:"failure_domains"`
}

type StartRequest struct { LocalURL, LocalPort, KnownHostsFile string }

type Candidate struct {
	ProviderID string `json:"provider_id"`
	URL string `json:"url"`
	FailureDomains FailureDomains `json:"failure_domains"`
}

type Handle interface {
	Candidate() Candidate
	Wait() <-chan error
	Stop(context.Context) error
}

type Provider interface {
	ID() string
	Metadata() ProviderMetadata
	Start(context.Context, StartRequest) (Handle, error)
}

type Policy struct {
	Region RegionProfile
	Now time.Time
	AllowedProviderIDs []string
	AllowNonDefault bool
	AllowUnverifiedGlobal bool
}

type Eligibility struct { Eligible bool; Reason string; Evidence *RegionalEvidence }
```

Create `internal/tunnel/region.go` implementing:

```go
const MaxRegionalEvidenceTTL = 7 * 24 * time.Hour

type NetworkSample struct {
	Carrier string `json:"carrier"`
	Region string `json:"region"`
	ResolverType string `json:"resolver_type,omitempty"`
	Success bool `json:"success"`
	LatencyMS int64 `json:"latency_ms,omitempty"`
}

type RegionalEvidence struct {
	ProviderID string `json:"provider_id"`
	Region RegionProfile `json:"region"`
	Status EvidenceStatus `json:"status"`
	Issuer string `json:"issuer"`
	ObservedAt time.Time `json:"observed_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Samples []NetworkSample `json:"samples"`
}

func (e RegionalEvidence) Validate() error
func EvaluateEligibility(meta ProviderMetadata, policy Policy, evidence []RegionalEvidence) Eligibility
```

Validation must reject empty IDs/issuer, unsupported regions/statuses, zero or reversed timestamps, TTL over seven days, IP literals in `NetworkSample.Region`, and samples without carrier/region. `verified` mainland evidence must contain successful samples from China Telecom, China Unicom, and China Mobile in at least two distinct coarse regions per carrier. `EvaluateEligibility` must clone the chosen evidence before returning its pointer. Under `cn-mainland`, only unexpired `verified` evidence for the same provider and region is eligible. Under `global`, default providers are eligible unless explicitly denied; `AllowNonDefault` controls non-default providers.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `go test ./internal/tunnel -run 'TestEvaluateEligibility|TestRegionalEvidence' -count=1`

Expected: PASS.

- [ ] **Step 5: Add clone/immutability regression tests and refactor**

Add a test that mutates input slices and returned evidence after evaluation and confirms stored metadata/evidence copies do not change. Implement `cloneMetadata`, `cloneEvidence`, and `cloneStrings`; keep all exported return values detached from inputs.

Run: `go test ./internal/tunnel -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tunnel/types.go internal/tunnel/region.go internal/tunnel/region_test.go
git commit -m "feat: add regional tunnel evidence contracts"
```

---

### Task 2: Provider registry and safe provider adapters

**Files:**
- Create: `internal/tunnel/registry.go`
- Create: `internal/tunnel/registry_test.go`
- Create: `internal/cli/tunnel.go`
- Create: `internal/cli/tunnel_test.go`
- Modify: `internal/cli/cli.go:3783-4144`
- Modify: `internal/cli/cli_test.go:1240-1325`

**Interfaces:**
- Consumes: Task 1 `tunnel.Provider`, `ProviderMetadata`, `Policy`, `RegionalEvidence`, and `EvaluateEligibility`.
- Produces: `tunnel.Registry`, ordered `Selection`, CLI provider constructors, pure URL parsers, and direct argv builders.

- [ ] **Step 1: Write failing registry tests**

Test duplicate provider IDs, deterministic ordering, allowlist filtering, mainland evidence gating, and returned-slice immutability:

```go
func TestRegistrySelectMainlandUsesFreshEvidenceOnly(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	r, err := NewRegistry(fakeProvider{id: "cloudflare", automatic: true}, fakeProvider{id: "cpolar", automatic: false})
	if err != nil { t.Fatal(err) }
	got := r.Select(Policy{Region: RegionCNMainland, Now: now, AllowNonDefault: true}, []RegionalEvidence{
		{ProviderID: "cpolar", Region: RegionCNMainland, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()},
	})
	if len(got) != 1 || got[0].Provider.ID() != "cpolar" { t.Fatalf("unexpected selection: %#v", got) }
}
```

- [ ] **Step 2: Run registry tests and verify RED**

Run: `go test ./internal/tunnel -run TestRegistry -count=1`

Expected: build failure because `Registry` and `Selection` do not exist.

- [ ] **Step 3: Implement registry selection**

Create:

```go
type Selection struct { Provider Provider; Metadata ProviderMetadata; Eligibility Eligibility }
type Registry struct { providers []Provider }

func NewRegistry(providers ...Provider) (Registry, error)
func (r Registry) Providers() []ProviderMetadata
func (r Registry) Select(policy Policy, evidence []RegionalEvidence) []Selection
```

Reject nil providers, empty IDs, duplicate IDs, and `provider.ID() != metadata.ID`. Sort selected providers deterministically by verified evidence first, default-automatic second, then provider ID; never use provider brand as a proxy for failure-domain independence.

- [ ] **Step 4: Write failing CLI parser and argv tests before moving code**

In `internal/cli/tunnel_test.go`, cover:

```go
func TestSSHProviderArgsRequireKnownHosts(t *testing.T) {
	spec := sshTunnelSpec{Destination: "nokey@localhost.run", Port: 22, RemoteForward: "80:localhost:8787"}
	if _, err := sshTunnelArgs("ssh", spec, ""); err == nil { t.Fatal("expected missing pin error") }
	args, err := sshTunnelArgs("ssh", spec, "/tmp/known_hosts")
	if err != nil { t.Fatal(err) }
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "StrictHostKeyChecking=no") || !strings.Contains(joined, "StrictHostKeyChecking=yes") { t.Fatalf("unsafe args: %v", args) }
}

func TestProviderURLParsersRejectMisleadingURLs(t *testing.T) {
	tests := []struct{name, provider, line, want string}{
		{"cloudflare valid", "cloudflare-quick", "ready https://abc.trycloudflare.com", "https://abc.trycloudflare.com"},
		{"cloudflare suffix", "cloudflare-quick", "https://abc.trycloudflare.com.attacker.test", ""},
		{"localhost admin", "localhost-run", "https://admin.localhost.run", ""},
		{"localhost valid", "localhost-run", "https://abc.lhr.life", "https://abc.lhr.life"},
		{"userinfo", "localhost-run", "https://user@abc.lhr.life", ""},
	}
	for _, tt := range tests { t.Run(tt.name, func(t *testing.T) { if got := providerURLFromLine(tt.provider, tt.line); got != tt.want { t.Fatalf("got %q want %q", got, tt.want) } }) }
}
```

For Pinggy, accept only HTTPS hosts ending in `.pinggy.link` or `.pinggy-free.link`, matching the official examples at <https://pinggy.io/docs/> and <https://pinggy.io/docs/http_tunnels/>. Reject the bare suffix, userinfo, non-default ports, attacker suffixes, and every other hostname.

- [ ] **Step 5: Run parser/argv tests and verify RED**

Run: `go test ./internal/cli -run 'TestSSHProviderArgs|TestProviderURLParsers' -count=1`

Expected: build failure because the new helpers do not exist.

- [ ] **Step 6: Move provider-specific code into `internal/cli/tunnel.go` and make it safe**

Move `startCloudflaredQuickTunnel`, `startCloudflaredWithProtocol`, `startLocalhostRunTunnel`, and URL parsing out of `cli.go` without behavior changes except:

```go
type sshTunnelSpec struct {
	Destination string
	Port int
	RemoteForward string
}

func sshTunnelArgs(sshPath string, spec sshTunnelSpec, knownHostsFile string) ([]string, error) {
	if strings.TrimSpace(knownHostsFile) == "" { return nil, fmt.Errorf("reviewed known-hosts file is required") }
	args := []string{sshPath}
	if spec.Port != 22 { args = append(args, "-p", strconv.Itoa(spec.Port)) }
	return append(args,
		"-T", "-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + knownHostsFile,
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-R", spec.RemoteForward,
		spec.Destination,
	), nil
}
```

Construct localhost.run with `sshTunnelSpec{Destination: "nokey@localhost.run", Port: 22, RemoteForward: "80:localhost:" + localPort}` and Pinggy with `sshTunnelSpec{Destination: "free.pinggy.io", Port: 443, RemoteForward: "0:localhost:" + localPort}`. Validate the local port with `strconv.Atoi` and range 1..65535; reject CR/LF/NUL in paths, destinations, and forward specifications. Delete all production use of `StrictHostKeyChecking=no`. Add CLI-local provider wrappers implementing `tunnel.Provider`; SSH wrappers return an eligibility/start error until their reviewed known-hosts file is configured.

- [ ] **Step 7: Run focused package tests and verify GREEN**

Run: `go test ./internal/tunnel ./internal/cli -run 'TestRegistry|TestSSHProviderArgs|TestProviderURLParsers|TestLocalhostRunTunnelURLFromLine' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tunnel/registry.go internal/tunnel/registry_test.go internal/cli/tunnel.go internal/cli/tunnel_test.go internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat: add safe public tunnel provider registry"
```

---

### Task 3: Availability manager, probes, and process cleanup

**Files:**
- Create: `internal/tunnel/manager.go`
- Create: `internal/tunnel/probe.go`
- Create: `internal/tunnel/manager_test.go`

**Interfaces:**
- Consumes: Task 2 ordered `[]Selection` and Task 1 provider/handle contracts.
- Produces: `Manager.Start`, `Runtime.Snapshot`, `Runtime.Stop`, `AvailabilitySet`, `Attempt`, `ProbeEvidence`, and injected `ProbeFunc`.

- [ ] **Step 1: Write failing lifecycle tests using fake providers**

Create fakes whose handles expose buffered `Wait` channels and count `Stop` calls. Cover:

- two providers start concurrently, never more than `MaxActive=2`;
- first provider returns NXDOMAIN probe error and second becomes healthy;
- provider prints a URL but `/healthz` marker validation fails;
- provider exits before readiness;
- context cancellation and explicit `Stop` reap every started handle exactly once;
- candidate URLs and errors are not copied into shareable attempt details.

Use this API in tests:

```go
runtime, err := (Manager{
	MaxActive: 2,
	StartTimeout: time.Second,
	ProbeTimeout: time.Second,
	Probe: func(ctx context.Context, c Candidate) (ProbeEvidence, error) { return evidence[c.ProviderID], probeErr[c.ProviderID] },
}).Start(ctx, selections, StartRequest{LocalURL: "http://127.0.0.1:8787", LocalPort: "8787"})
```

- [ ] **Step 2: Run manager tests and verify RED**

Run: `go test ./internal/tunnel -run 'TestManager|TestRuntimeStop' -count=1`

Expected: build failure because manager/runtime types do not exist.

- [ ] **Step 3: Implement bounded startup and immutable snapshots**

Define:

```go
type AttemptStatus string
const (AttemptStarting AttemptStatus = "starting"; AttemptHealthy AttemptStatus = "healthy"; AttemptDegraded AttemptStatus = "degraded"; AttemptExited AttemptStatus = "exited"; AttemptStopped AttemptStatus = "stopped")

type ProbeEvidence struct { DNSOK, TCPConnectOK, TLSOK, HealthOK, BootstrapOK, SmallAssetOK bool; Latency time.Duration; InstanceMarker string }
type Attempt struct { ProviderID string `json:"provider_id"`; CandidateID string `json:"candidate_id,omitempty"`; Status AttemptStatus `json:"status"`; ErrorClass string `json:"error_class,omitempty"`; Probe ProbeEvidence `json:"probe"` }
type AvailabilitySet struct { SchemaVersion string `json:"schema_version"`; Region RegionProfile `json:"region"`; Candidates []Candidate `json:"candidates"`; Attempts []Attempt `json:"attempts"` }
type ProbeFunc func(context.Context, Candidate) (ProbeEvidence, error)
type Manager struct { MaxActive int; StartTimeout, ProbeTimeout time.Duration; Probe ProbeFunc }
type Runtime struct { /* private mutex, handles, immutable snapshot, stopOnce */ }
func (m Manager) Start(context.Context, []Selection, StartRequest) (*Runtime, error)
func (r *Runtime) Snapshot() AvailabilitySet
func (r *Runtime) Stop(context.Context) error
```

Use worker goroutines bounded by `MaxActive`, default 2. A provider is healthy only after `Probe` succeeds. `Stop` calls all handles, joins errors with `errors.Join`, and is idempotent. Candidate IDs are SHA-256 hashes of `providerID + "\x00" + normalizedURL`, truncated for logs; raw URLs remain only in protected in-memory candidates, not `Attempt` JSON.

- [ ] **Step 4: Implement the default public probe**

`probe.go` must use an injected or private `http.Client` with no ambient proxy override, bounded timeouts, redirect rejection, and body limits. Probe `/healthz`, require HTTP 200 and exact non-secret header `X-Rdev-Gateway-Instance`, then fetch a caller-provided small bootstrap/checksum path only after the ticket exists. In Task 3 expose two functions so startup can use health-only and Task 5 can use ticket-specific final probing:

```go
func ProbeGatewayHealth(ctx context.Context, client *http.Client, candidate Candidate, expectedInstance string) (ProbeEvidence, error)
func ProbeBootstrapAsset(ctx context.Context, client *http.Client, candidate Candidate, ticketCode, expectedInstance string) (ProbeEvidence, error)
```

Reject redirects, content over 256 KiB, missing marker, wrong marker, non-HTTPS public candidate URLs, loopback/private public candidates, and response bodies that look like an interstitial HTML page when a PowerShell/bootstrap content type is expected.

- [ ] **Step 5: Run manager tests and verify GREEN**

Run: `go test ./internal/tunnel -count=1`

Expected: PASS.

- [ ] **Step 6: Run the race detector for the new manager**

Run: `go test -race ./internal/tunnel -count=1`

Expected: PASS with no race reports.

- [ ] **Step 7: Commit**

```bash
git add internal/tunnel/manager.go internal/tunnel/probe.go internal/tunnel/manager_test.go
git commit -m "feat: supervise public tunnel availability"
```

---

### Task 4: Health marker and direct-readiness contract

**Files:**
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/server_test.go`
- Create: `internal/supportsession/availability.go`
- Create: `internal/supportsession/availability_test.go`
- Modify: `internal/supportsession/plan.go:280-370,431-510,1122-1160,2126-2320`
- Modify: `internal/supportsession/plan_test.go`

**Interfaces:**
- Consumes: Task 3 `tunnel.AvailabilitySet`.
- Produces: health instance marker, `supportsession.AvailabilityReadiness`, `DirectAvailability`, and compatibility mapping into created/connect/started payloads.

- [ ] **Step 1: Write a failing HTTP health-marker test**

Construct a gateway server and assert `/healthz` returns a stable per-server `X-Rdev-Gateway-Instance` value containing no ticket or key material. A second server must use a different marker.

Run: `go test ./internal/httpapi -run TestHealthzIncludesGatewayInstanceMarker -count=1`

Expected: FAIL because the header is absent.

- [ ] **Step 2: Implement the marker**

Add an unexported random 128-bit instance ID to `httpapi.Server`, generated in constructors using `crypto/rand` and hex encoding. `/healthz` sets `X-Rdev-Gateway-Instance` before writing 200. Add `func (s *Server) GatewayInstance() string` so the local supervisor can require the exact marker through a tunnel. Constructor failure is not possible: if `rand.Read` fails, derive a process-unique fallback from time plus an atomic counter and do not expose the error contents.

Run: `go test ./internal/httpapi -run TestHealthzIncludesGatewayInstanceMarker -count=1`

Expected: PASS.

- [ ] **Step 3: Write failing readiness tests**

Create tests that assert:

```go
func TestDirectAvailabilityRequiresExplicitOverride(t *testing.T) {
	set := tunnel.AvailabilitySet{SchemaVersion: tunnel.AvailabilitySchemaVersion, Region: tunnel.RegionCNMainland, Candidates: []tunnel.Candidate{{ProviderID: "a", URL: "https://a.example"}, {ProviderID: "b", URL: "https://b.example"}}}
	got := DirectAvailability(set, false)
	if got.ReadyToSend || !got.DegradedSingleEntry || got.State != "degraded-single-entry" { t.Fatalf("unexpected readiness: %#v", got) }
	overridden := DirectAvailability(set, true)
	if !overridden.ReadyToSend || overridden.ReadyToActivate || overridden.ReadyToExecute || !overridden.DegradedSingleEntry { t.Fatalf("unexpected override: %#v", overridden) }
}
```

Also test no candidates, LAN-only candidates, one candidate, and compatibility alias mapping.

- [ ] **Step 4: Run readiness tests and verify RED**

Run: `go test ./internal/supportsession -run 'TestDirectAvailability|TestBuildConnect.*Readiness' -count=1`

Expected: build failure because readiness types/functions do not exist.

- [ ] **Step 5: Implement readiness mapping**

Create:

```go
type AvailabilityReadiness struct {
	SchemaVersion string `json:"schema_version"`
	State string `json:"state"`
	Region tunnel.RegionProfile `json:"region"`
	ReadyToSend bool `json:"ready_to_send"`
	ReadyToActivate bool `json:"ready_to_activate"`
	ReadyToExecute bool `json:"ready_to_execute"`
	DegradedSingleEntry bool `json:"degraded_single_entry"`
	DegradedReason string `json:"degraded_reason,omitempty"`
	StandardNextAction string `json:"standard_next_action,omitempty"`
	AvailabilitySet tunnel.AvailabilitySet `json:"availability_set"`
}

func DirectAvailability(set tunnel.AvailabilitySet, allowOverride bool) AvailabilityReadiness
```

Add `AvailabilityReadiness` to `CreatedOptions` and `StartedOptions`. `BuildCreated`, `BuildStarted`, `BuildConnectFromCreated`, and `freshAgentConnectContract` must emit the three readiness states. Keep `ready_to_send_to_human` and `ready_to_send_human` aliases equal to `ReadyToSend`. Do not remove existing fields in Phase 1.

- [ ] **Step 6: Run support-session tests and verify GREEN**

Run: `go test ./internal/supportsession -count=1`

Expected: PASS after updating existing script-first expectations. In Phase 1, explicit stable URLs and managed ephemeral URLs are both non-sendable without the explicit degraded override because neither has the signed connector/rendezvous path yet. A zero-value readiness input fails closed.

- [ ] **Step 7: Commit**

```bash
git add internal/httpapi/server.go internal/httpapi/server_test.go internal/supportsession/availability.go internal/supportsession/availability_test.go internal/supportsession/plan.go internal/supportsession/plan_test.go
git commit -m "feat: report honest direct tunnel readiness"
```

---

### Task 5: Reorder foreground startup and integrate availability

**Files:**
- Modify: `internal/cli/cli.go:3648-4456`
- Modify: `internal/cli/tunnel.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `internal/supportsession/plan.go`

**Interfaces:**
- Consumes: Tasks 2-4 registry, providers, manager, probes, and readiness.
- Produces: foreground support-session orchestration with local-first startup, typed candidates, final bootstrap probe, and complete cleanup.

- [ ] **Step 1: Write a failing orchestration test with injected tunnel dependencies**

Extract a small dependency struct rather than using global function replacement:

```go
type tunnelRuntimeDeps struct {
	Registry tunnel.Registry
	Evidence []tunnel.RegionalEvidence
	Manager tunnel.Manager
	FinalProbe func(context.Context, tunnel.Candidate, string, string) error
}
```

Add an `App` private field or package-level constructor used only by production `NewApp`; tests create an app with fake providers. The test records events and asserts this order:

```text
local_gateway_started
local_health_passed
providers_started
provider_health_passed
ticket_created
bootstrap_probe_passed
handoff_written
```

Also assert provider failure stops every handle and shuts down the local server, and no ready/handoff file is written when override is false.

- [ ] **Step 2: Run the orchestration test and verify RED**

Run: `go test ./internal/cli -run TestSupportSessionStartOrdersGatewayBeforePublicTunnels -count=1`

Expected: FAIL because current code starts public tunnels before the gateway and creates the ticket before local/public health completes.

- [ ] **Step 3: Add CLI options and direct policy plumbing**

Extend both `supportSessionConnectOptions` and `supportSessionStartOptions` with:

```go
Region string
ProviderPolicyPath string
AllowDegradedDirectHandoff bool
```

Add flags:

```text
--region global|cn-mainland
--provider-policy <JSON path>
--allow-degraded-direct-handoff
```

Reject unknown regions, unreadable policy files, unknown provider IDs, inline credentials, and policy files with permissions broader than `0600` on POSIX. The policy JSON contains only provider IDs, regional evidence paths, SSH known-hosts paths, and enable/disable decisions; secrets remain external references.

- [ ] **Step 4: Reorder `supportSessionStart`**

Implement this exact sequence:

1. validate options, choose free address, resolve explicit stable candidates;
2. prepare and verify helper assets;
3. load/create signing keys, state store, audit store, gateway, HTTP server, and health instance marker;
4. start local gateway and verify local `/healthz`;
5. if an explicit stable gateway was supplied, probe it and build a typed candidate; otherwise select eligible providers and start the availability manager;
6. compute `DirectAvailability`; without explicit override, keep managed direct handoff non-sendable;
7. create the ticket with the healthy typed candidates and save gateway state;
8. probe `/join/<ticket>/bootstrap.ps1` through every candidate kept in the handoff; demote failures and recompute readiness;
9. build created/started payloads from final candidates/readiness;
10. write ready/handoff files only when `ReadyToSend`; always write a protected diagnostic status payload when not ready;
11. on every return path, stop availability handles and shut down the local gateway; on normal foreground exit, do the same after watchers stop.

Do not store localhost.run or Pinggy URLs in `RDEV_CLOUDFLARED_GATEWAY_URL`. Remove that assignment for generic managed tunnels. Convert `tunnel.Candidate` to `supportsession.GatewayURLCandidate` with provider-specific `Kind`, `Scope="public"`, and `Recommended` only for the first healthy candidate.

- [ ] **Step 5: Run orchestration tests and verify GREEN**

Run: `go test ./internal/cli -run 'TestSupportSessionStartOrdersGatewayBeforePublicTunnels|TestManagedPublicTunnel|TestLocalhostRun|TestConfiguredCloudflared' -count=1`

Expected: PASS.

- [ ] **Step 6: Add failure-path tests**

Cover local health failure, all providers ineligible for `cn-mainland`, first provider NXDOMAIN/second healthy, URL printed but marker mismatch, final ticket bootstrap failure, context cancellation, and explicit degraded override. Assert no LAN URL is presented as remote-ready.

Run: `go test ./internal/cli -run 'TestSupportSessionStart.*(Failure|Mainland|Fallback|Override|Cleanup)' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go internal/cli/tunnel.go internal/supportsession/plan.go
git commit -m "feat: start support tunnels as an availability set"
```

---

### Task 6: CLI inspection commands, acceptance contracts, and verification

**Files:**
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `internal/mcpstdio/server.go`
- Modify: `internal/mcpstdio/server_test.go`
- Modify: `internal/acceptance/fresh_agent_support_session.go`
- Modify: `internal/acceptance/fresh_agent_support_session_test.go`
- Modify: `README.md` only if it already documents support-session connection commands; otherwise update the existing closest support-session document rather than creating a new top-level file.

**Interfaces:**
- Consumes: all Phase 1 packages and readiness fields.
- Produces: `rdev tunnel providers`, `rdev tunnel probe`, MCP flag propagation, acceptance checks, and user-facing degraded/recovery guidance.

- [ ] **Step 1: Write failing CLI command tests**

Test:

```text
rdev tunnel providers --region cn-mainland --json
rdev tunnel probe --region cn-mainland --provider-policy <file> --json
```

`providers` is read-only and lists metadata, evidence status/expiry, eligibility, and redacted failure domains. `probe` checks executables/configuration and may perform bounded network health probes, but never starts a persistent tunnel, changes configuration, accepts terms, registers accounts, or prints credentials.

- [ ] **Step 2: Run CLI tests and verify RED**

Run: `go test ./internal/cli -run 'TestTunnelProviders|TestTunnelProbe' -count=1`

Expected: FAIL with unknown tunnel subcommand.

- [ ] **Step 3: Implement CLI commands and MCP propagation**

Add top-level `tunnel` routing with `providers` and `probe`. Ensure `rdev.sessions.connect` schema accepts and forwards:

```json
{
  "region": "cn-mainland",
  "provider_policy": "/protected/path/policy.json",
  "allow_degraded_direct_handoff": false
}
```

The MCP output includes `availability_set`, `regional_evidence`, `ready_to_send`, `ready_to_activate`, `ready_to_execute`, `degraded_single_entry`, and compatibility aliases.

- [ ] **Step 4: Update acceptance checks**

Change the fresh-agent acceptance fixture so both an explicit stable test gateway and a managed direct tunnel are degraded/non-sendable without override; add a separate explicit-override case that is sendable but still `degraded-single-entry`. Add checks that:

- `cn-mainland` never treats missing evidence as verified;
- direct mode cannot claim pre-bootstrap failover;
- no output contains a known-hosts content, token, target IP, or raw provider URL in shareable attempts;
- cleanup and readiness transitions are represented independently.

- [ ] **Step 5: Run package-level tests**

Run:

```bash
go test ./internal/tunnel ./internal/supportsession ./internal/httpapi ./internal/cli ./internal/mcpstdio ./internal/acceptance -count=1
```

Expected: PASS.

- [ ] **Step 6: Run full verification**

Run:

```bash
go test ./... -count=1
go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/rdev
git diff --check
```

Expected: all commands exit 0. Run `go test -race ./internal/tunnel ./internal/supportsession ./internal/httpapi -count=1`; it must pass. Run the broader CLI race suite separately and record any pre-existing `bytes.Buffer` race without attributing it to this phase; any new race blocks completion.

- [ ] **Step 7: Perform real mainland Windows acceptance**

On the user-authorized Windows host, use `--region cn-mainland`. Before representative evidence is installed, verify providers are ineligible rather than guessed. With an explicitly approved evidence fixture and degraded override, verify DNS/TLS/bootstrap behavior, smoke-test registration, explicit stop cleanup, and absence of services/tasks/Run keys/firewall rules. Do not claim three-carrier mainland readiness from this single host.

- [ ] **Step 8: Final review and commit**

Run a Go code review and a security review focused on subprocess argv, host-key verification, URL parsing, evidence trust, redaction, process cleanup, and readiness claims. Fix all Critical/High findings and rerun the affected tests.

```bash
git add internal/cli internal/mcpstdio internal/acceptance README.md
git commit -m "feat: expose regional tunnel readiness"
```
