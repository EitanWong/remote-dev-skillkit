package tunnel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type managerFakeHandle struct {
	candidate Candidate
	wait      chan error
	stops     atomic.Int32
	stopBlock <-chan struct{}
	live      *atomic.Int32
}

func (h *managerFakeHandle) Candidate() Candidate { return h.candidate }
func (h *managerFakeHandle) Wait() <-chan error   { return h.wait }
func (h *managerFakeHandle) Stop(context.Context) error {
	if h.stops.Add(1) == 1 {
		if h.stopBlock != nil {
			<-h.stopBlock
		}
		if h.live != nil {
			h.live.Add(-1)
		}
		select {
		case h.wait <- nil:
		default:
		}
	}
	return nil
}

type managerFakeProvider struct {
	id      string
	handle  *managerFakeHandle
	started chan struct{}
	release <-chan struct{}
	active  *atomic.Int32
	peak    *atomic.Int32
}

func (p *managerFakeProvider) ID() string { return p.id }
func (p *managerFakeProvider) Metadata() ProviderMetadata {
	return ProviderMetadata{ID: p.id, DefaultAutomatic: true}
}
func (p *managerFakeProvider) Start(ctx context.Context, _ StartRequest) (Handle, error) {
	if p.active != nil {
		current := p.active.Add(1)
		for peak := p.peak.Load(); current > peak && !p.peak.CompareAndSwap(peak, current); peak = p.peak.Load() {
		}
		defer p.active.Add(-1)
	}
	if p.started != nil {
		close(p.started)
	}
	if p.release != nil {
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if p.handle.live != nil {
		current := p.handle.live.Add(1)
		for peak := p.peak.Load(); current > peak && !p.peak.CompareAndSwap(peak, current); peak = p.peak.Load() {
		}
	}
	return p.handle, nil
}

func managerSelection(provider Provider, region RegionProfile) Selection {
	selection := Selection{Provider: provider, Metadata: provider.Metadata()}
	if region != "" {
		selection.Eligibility.Evidence = &RegionalEvidence{Region: region}
	}
	return selection
}

func TestManagerBoundsConcurrentStartsAndRecordsProbeResults(t *testing.T) {
	var active, live, peak atomic.Int32
	release := make(chan struct{})
	providers := make([]*managerFakeProvider, 3)
	selections := make([]Selection, 3)
	for i := range providers {
		id := fmt.Sprintf("provider-%d", i)
		providers[i] = &managerFakeProvider{
			id: id, release: release, active: &active, peak: &peak,
			handle: &managerFakeHandle{candidate: Candidate{ProviderID: id, URL: "https://" + id + ".example.test"}, wait: make(chan error, 1), live: &live},
		}
		selections[i] = managerSelection(providers[i], RegionGlobal)
	}
	go func() {
		for peak.Load() < 2 {
			time.Sleep(time.Millisecond)
		}
		close(release)
	}()

	runtime, err := (Manager{
		MaxActive: 2, StartTimeout: time.Second, ProbeTimeout: time.Second,
		Probe: func(_ context.Context, c Candidate) (ProbeEvidence, error) {
			if c.ProviderID == "provider-0" {
				return ProbeEvidence{DNSOK: false}, errors.New("nxdomain: candidate URL omitted")
			}
			return ProbeEvidence{DNSOK: true, HealthOK: true, InstanceMarker: c.ProviderID}, nil
		},
	}).Start(context.Background(), selections, StartRequest{LocalURL: "http://127.0.0.1:8787", LocalPort: "8787"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	if got := peak.Load(); got != 2 {
		t.Fatalf("peak live handles = %d, want 2", got)
	}
	if got := providers[0].handle.stops.Load(); got != 1 {
		t.Fatalf("probe-failed handle stops = %d, want 1 before fallback starts", got)
	}
	snapshot := runtime.Snapshot()
	if len(snapshot.Attempts) != 3 || snapshot.Attempts[0].Status != AttemptDegraded || snapshot.Attempts[1].Status != AttemptHealthy {
		t.Fatalf("unexpected attempts: %#v", snapshot.Attempts)
	}
	encoded, err := json.Marshal(snapshot.Attempts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "https://") || strings.Contains(string(encoded), "nxdomain:") {
		t.Fatalf("shareable attempts leaked URL or raw error: %s", encoded)
	}
}

func TestManagerRejectsCandidateProviderIDMismatch(t *testing.T) {
	handle := &managerFakeHandle{candidate: Candidate{ProviderID: "spoofed", URL: "https://public.example.test"}, wait: make(chan error, 1)}
	provider := &managerFakeProvider{id: "authoritative", handle: handle}
	runtime, err := (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
		t.Fatal("mismatched candidate must not be probed")
		return ProbeEvidence{}, nil
	}}).Start(context.Background(), []Selection{managerSelection(provider, RegionGlobal)}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	attempt := runtime.Snapshot().Attempts[0]
	if attempt.ProviderID != "authoritative" || attempt.CandidateID != "" || attempt.Status != AttemptDegraded || attempt.ErrorClass != "provider-id-mismatch" {
		t.Fatalf("unexpected mismatch attempt: %#v", attempt)
	}
	if handle.stops.Load() != 1 {
		t.Fatalf("mismatched handle stops = %d, want 1", handle.stops.Load())
	}
}

func TestManagerProbesPrintedURLAndReapsMarkerFailure(t *testing.T) {
	handle := &managerFakeHandle{candidate: Candidate{ProviderID: "printed", URL: "https://printed.example.test"}, wait: make(chan error, 1)}
	provider := &managerFakeProvider{id: "printed", handle: handle}
	runtime, err := (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
		return ProbeEvidence{DNSOK: true, TLSOK: true, HealthOK: false}, errors.New("instance marker mismatch at https://printed.example.test/healthz")
	}}).Start(context.Background(), []Selection{managerSelection(provider, RegionGlobal)}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.Snapshot().Attempts[0].Status; got != AttemptDegraded {
		t.Fatalf("printed URL status = %q, want degraded", got)
	}
	if handle.stops.Load() != 1 {
		t.Fatalf("marker-failed handle stops = %d, want 1", handle.stops.Load())
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerObservesSpontaneousHealthyExit(t *testing.T) {
	handle := &managerFakeHandle{candidate: Candidate{ProviderID: "healthy", URL: "https://healthy.example.test"}, wait: make(chan error, 1)}
	provider := &managerFakeProvider{id: "healthy", handle: handle}
	runtime, err := (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
		return ProbeEvidence{DNSOK: true, TLSOK: true, HealthOK: true}, nil
	}}).Start(context.Background(), []Selection{managerSelection(provider, RegionGlobal)}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	handle.wait <- errors.New("process exited unexpectedly")
	eventually(t, time.Second, func() bool { return runtime.Snapshot().Attempts[0].Status == AttemptExited })
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerMarksExitBeforeReadiness(t *testing.T) {
	handle := &managerFakeHandle{candidate: Candidate{ProviderID: "early", URL: "https://early.example.test"}, wait: make(chan error, 1)}
	handle.wait <- errors.New("early exit")
	provider := &managerFakeProvider{id: "early", handle: handle}
	runtime, err := (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
		t.Fatal("probe must not run after exit")
		return ProbeEvidence{}, nil
	}}).Start(context.Background(), []Selection{managerSelection(provider, RegionGlobal)}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.Snapshot().Attempts[0].Status; got != AttemptExited {
		t.Fatalf("status = %q, want exited", got)
	}
	_ = runtime.Stop(context.Background())
}

func TestRuntimeStopIsIdempotentReapsAllHandlesAndSnapshotIsDeepCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	handles := []*managerFakeHandle{
		{candidate: Candidate{ProviderID: "one", URL: "https://one.example.test", FailureDomains: FailureDomains{EdgeNetwork: "edge-one"}}, wait: make(chan error, 1)},
		{candidate: Candidate{ProviderID: "two", URL: "https://two.example.test"}, wait: make(chan error, 1)},
	}
	selections := []Selection{
		managerSelection(&managerFakeProvider{id: "one", handle: handles[0]}, RegionCNMainland),
		managerSelection(&managerFakeProvider{id: "two", handle: handles[1]}, RegionCNMainland),
	}
	runtime, err := (Manager{Region: RegionCNMainland, Probe: func(context.Context, Candidate) (ProbeEvidence, error) { return ProbeEvidence{HealthOK: true}, nil }}).Start(ctx, selections, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	first := runtime.Snapshot()
	first.Region = RegionGlobal
	first.Candidates[0].ProviderID = "mutated"
	first.Candidates[0].FailureDomains.EdgeNetwork = "mutated"
	first.Attempts[0].Probe.InstanceMarker = "mutated"
	second := runtime.Snapshot()
	if second.Region != RegionCNMainland || second.Candidates[0].ProviderID != "one" || second.Candidates[0].FailureDomains.EdgeNetwork != "edge-one" || second.Attempts[0].Probe.InstanceMarker == "mutated" {
		t.Fatalf("snapshot retained caller mutations: %#v", second)
	}
	cancel()
	eventually(t, time.Second, func() bool { return handles[0].stops.Load() == 1 && handles[1].stops.Load() == 1 })
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, handle := range handles {
		if got := handle.stops.Load(); got != 1 {
			t.Fatalf("handle stopped %d times, want once", got)
		}
	}
}

func TestRuntimeStopCleanupOutlivesFirstCallerContext(t *testing.T) {
	release := make(chan struct{})
	handle := &managerFakeHandle{candidate: Candidate{ProviderID: "one", URL: "https://one.example.test"}, wait: make(chan error, 1), stopBlock: release}
	runtime, err := (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) { return ProbeEvidence{HealthOK: true}, nil }}).Start(context.Background(), []Selection{managerSelection(&managerFakeProvider{id: "one", handle: handle}, RegionGlobal)}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	shortCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := runtime.Stop(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Stop error = %v, want deadline exceeded", err)
	}
	close(release)
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if handle.stops.Load() != 1 {
		t.Fatalf("handle stops = %d, want one shared cleanup", handle.stops.Load())
	}
}

func TestManagerRegionDefaultsGlobalDespiteSelectionEvidence(t *testing.T) {
	handle := &managerFakeHandle{candidate: Candidate{ProviderID: "one", URL: "https://one.example.test"}, wait: make(chan error, 1)}
	selection := managerSelection(&managerFakeProvider{id: "one", handle: handle}, RegionCNMainland)
	runtime, err := (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) { return ProbeEvidence{HealthOK: true}, nil }}).Start(context.Background(), []Selection{selection}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	if got := runtime.Snapshot().Region; got != RegionGlobal {
		t.Fatalf("region = %q, want global despite selection evidence", got)
	}
}

func TestManagerCandidateIDsNormalizeEquivalentURLs(t *testing.T) {
	first := candidateID("provider", " HTTPS://Example.COM.:443/a/../path/%7Euser/?query=secret#fragment ")
	second := candidateID("provider", "https://example.com/path/~user")
	if first != second {
		t.Fatalf("equivalent URLs produced different candidate IDs: %q != %q", first, second)
	}
	if len(first) != 16 {
		t.Fatalf("candidate ID length = %d, want 16 hex characters", len(first))
	}
}

func TestProbeGatewayHealthRejectsUnsafeOrInvalidResponses(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		marker  string
	}{
		{name: "redirect", marker: "instance", handler: func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", http.StatusFound) }},
		{name: "missing marker", marker: "instance", handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }},
		{name: "marker mismatch", marker: "expected", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Rdev-Gateway-Instance", "wrong")
			w.WriteHeader(http.StatusOK)
		}},
		{name: "oversized body", marker: "instance", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Rdev-Gateway-Instance", "instance")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, 256<<10+1))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tt.handler)
			defer server.Close()
			evidence, err := probeGatewayHealthWithOptions(context.Background(), Candidate{ProviderID: "provider", URL: "https://public.example.test"}, tt.marker, publicTestProbeOptions(t, server))
			if err == nil {
				t.Fatal("expected probe rejection")
			}
			if tt.name == "marker mismatch" && (!evidence.DNSOK || !evidence.TCPConnectOK || !evidence.TLSOK || evidence.HealthOK) {
				t.Fatalf("completed stages were not preserved: %#v", evidence)
			}
		})
	}
	for _, rawURL := range []string{"http://public.example.com", "https://localhost", "https://service.localhost", "https://127.0.0.1", "https://10.0.0.1", "https://169.254.1.1"} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := ProbeGatewayHealth(context.Background(), nil, Candidate{ProviderID: "provider", URL: rawURL}, "instance"); err == nil {
				t.Fatal("expected unsafe candidate rejection")
			}
		})
	}
}

func TestProbeSecureDialRejectsResolvedPrivateAddressesAndTrailingDotLiteral(t *testing.T) {
	for _, tt := range []struct {
		name string
		url  string
		ips  []netip.Addr
	}{
		{name: "hostname resolves loopback", url: "https://public.example.test", ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{name: "hostname includes private result", url: "https://public.example.test", ips: []netip.Addr{netip.MustParseAddr("203.0.113.1"), netip.MustParseAddr("10.0.0.1")}},
		{name: "trailing dot literal", url: "https://127.0.0.1."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dialed := atomic.Bool{}
			_, err := probeGatewayHealthWithOptions(context.Background(), Candidate{ProviderID: "provider", URL: tt.url}, "instance", probeOptions{
				Resolver: staticProbeResolver{IPs: tt.ips},
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					dialed.Store(true)
					return nil, errors.New("unexpected dial")
				},
			})
			if err == nil || dialed.Load() {
				t.Fatalf("err = %v, dialed = %v; want rejection before dial", err, dialed.Load())
			}
		})
	}
}

func TestProbeClientDoesNotTrustCallerTransportOrJar(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("caller transport must not be used")
		return nil, nil
	}), Jar: panicCookieJar{t: t}}
	probeClient := newProbeHTTPClient(client, probeOptions{})
	if probeClient.Jar != nil || probeClient.Transport == client.Transport {
		t.Fatalf("probe client retained caller-controlled state: %#v", probeClient)
	}
}

func TestProbeBootstrapAssetRequiresTicketAndRejectsInterstitial(t *testing.T) {
	var requestedPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.EscapedPath()
		w.Header().Set("X-Rdev-Gateway-Instance", "instance")
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><html><title>Access denied</title></html>"))
	}))
	defer server.Close()
	candidate := Candidate{ProviderID: "provider", URL: "https://public.example.test"}
	if _, err := ProbeBootstrapAsset(context.Background(), server.Client(), candidate, "", "instance"); err == nil {
		t.Fatal("expected empty ticket rejection")
	}
	if _, err := probeBootstrapAssetWithOptions(context.Background(), candidate, "ticket/code", "instance", publicTestProbeOptions(t, server)); err == nil {
		t.Fatal("expected HTML interstitial rejection")
	}
	if requestedPath != "/join/ticket%2Fcode/bootstrap.ps1" {
		t.Fatalf("bootstrap path = %q", requestedPath)
	}
}

func TestProbeBootstrapAssetRequiresPowerShellContentAndMarker(t *testing.T) {
	tests := []struct {
		name, contentType, body string
		wantOK                  bool
	}{
		{name: "valid", contentType: "text/plain; charset=utf-8", body: "$ErrorActionPreference = 'Stop'\nWrite-Host '[rdev]'", wantOK: true},
		{name: "empty", contentType: "text/plain", body: ""},
		{name: "json", contentType: "application/json", body: `{"ok":true}`},
		{name: "missing script marker", contentType: "text/plain", body: "Write-Host 'not the bootstrap'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Rdev-Gateway-Instance", "instance")
				w.Header().Set("Content-Type", tt.contentType)
				if r.URL.Path == "/healthz" {
					w.WriteHeader(http.StatusOK)
					return
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			evidence, err := probeBootstrapAssetWithOptions(context.Background(), Candidate{ProviderID: "provider", URL: "https://public.example.test"}, "ticket", "instance", publicTestProbeOptions(t, server))
			if tt.wantOK && (err != nil || !evidence.BootstrapOK || !evidence.SmallAssetOK) {
				t.Fatalf("valid bootstrap rejected: evidence=%#v err=%v", evidence, err)
			}
			if !tt.wantOK && err == nil {
				t.Fatal("invalid bootstrap accepted")
			}
		})
	}
}

func publicTestProbeOptions(t *testing.T, server *httptest.Server) probeOptions {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &net.Dialer{Timeout: time.Second}
	return probeOptions{
		Resolver: staticProbeResolver{IPs: []netip.Addr{netip.MustParseAddr("8.8.8.8")}},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, target.Host)
		},
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, // test-only certificate for routed public hostname
	}
}

type staticProbeResolver struct{ IPs []netip.Addr }

func (r staticProbeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), r.IPs...), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type panicCookieJar struct{ t *testing.T }

func (j panicCookieJar) SetCookies(*url.URL, []*http.Cookie) {
	j.t.Fatal("caller jar must not be used")
}
func (j panicCookieJar) Cookies(*url.URL) []*http.Cookie {
	j.t.Fatal("caller jar must not be used")
	return nil
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
