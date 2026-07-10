package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
}

func (h *managerFakeHandle) Candidate() Candidate       { return h.candidate }
func (h *managerFakeHandle) Wait() <-chan error         { return h.wait }
func (h *managerFakeHandle) Stop(context.Context) error { h.stops.Add(1); return nil }

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
	var active, peak atomic.Int32
	release := make(chan struct{})
	providers := make([]*managerFakeProvider, 3)
	selections := make([]Selection, 3)
	for i := range providers {
		id := fmt.Sprintf("provider-%d", i)
		providers[i] = &managerFakeProvider{
			id: id, release: release, active: &active, peak: &peak,
			handle: &managerFakeHandle{candidate: Candidate{ProviderID: id, URL: "https://" + id + ".example.test"}, wait: make(chan error, 1)},
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
		t.Fatalf("peak concurrent starts = %d, want 2", got)
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

func TestManagerProbesPrintedURLAndObservesSpontaneousExit(t *testing.T) {
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
	first := candidateID("provider", " HTTPS://Example.COM:443/path/?query=secret#fragment ")
	second := candidateID("provider", "https://example.com/path")
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
			_, err := ProbeGatewayHealth(context.Background(), publicTestClient(t, server), Candidate{ProviderID: "provider", URL: "https://public.example.test"}, tt.marker)
			if err == nil {
				t.Fatal("expected probe rejection")
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
	if _, err := ProbeBootstrapAsset(context.Background(), publicTestClient(t, server), candidate, "ticket/code", "instance"); err == nil {
		t.Fatal("expected HTML interstitial rejection")
	}
	if requestedPath != "/join/ticket%2Fcode/bootstrap.ps1" {
		t.Fatalf("bootstrap path = %q", requestedPath)
	}
}

func publicTestClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.InsecureSkipVerify = true // test transport routes a public hostname to httptest TLS
	dialer := &net.Dialer{Timeout: time.Second}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, target.Host)
	}
	return &http.Client{Transport: transport}
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
