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
	id                    string
	handle                *managerFakeHandle
	started               chan struct{}
	release               <-chan struct{}
	active                *atomic.Int32
	peak                  *atomic.Int32
	bindLifetimeToContext bool
	returnHandleOnCancel  bool
	cancelReturnDelay     time.Duration
	contextDone           chan struct{}
}

func (p *managerFakeProvider) ID() string { return p.id }
func (p *managerFakeProvider) Metadata() ProviderMetadata {
	return ProviderMetadata{ID: p.id, DefaultAutomatic: true}
}
func (p *managerFakeProvider) Start(ctx context.Context, _ StartRequest) (Handle, error) {
	if p.contextDone != nil {
		go func() {
			<-ctx.Done()
			close(p.contextDone)
		}()
	}
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
			if p.cancelReturnDelay > 0 {
				time.Sleep(p.cancelReturnDelay)
			}
			if !p.returnHandleOnCancel {
				return nil, ctx.Err()
			}
		}
	}
	if p.handle.live != nil {
		current := p.handle.live.Add(1)
		for peak := p.peak.Load(); current > peak && !p.peak.CompareAndSwap(peak, current); peak = p.peak.Load() {
		}
	}
	if p.bindLifetimeToContext {
		go func() {
			<-ctx.Done()
			select {
			case p.handle.wait <- ctx.Err():
			default:
			}
		}()
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

func TestManagerStartupTimeoutDoesNotOwnHealthyHandleLifetime(t *testing.T) {
	handle := &managerFakeHandle{
		candidate: Candidate{ProviderID: "context-bound", URL: "https://context-bound.example.test"},
		wait:      make(chan error, 1),
	}
	provider := &managerFakeProvider{id: "context-bound", handle: handle, bindLifetimeToContext: true}
	runtime, err := (Manager{
		StartTimeout: 20 * time.Millisecond,
		Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
			return ProbeEvidence{DNSOK: true, TLSOK: true, HealthOK: true}, nil
		},
	}).Start(context.Background(), []Selection{managerSelection(provider, RegionGlobal)}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Stop(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	snapshot := runtime.Snapshot()
	if len(snapshot.Candidates) != 1 || snapshot.Attempts[0].Status != AttemptHealthy {
		t.Fatalf("startup timeout canceled a healthy provider handle: %#v", snapshot)
	}
}

func TestManagerStartupTimeoutStillCancelsBlockedStart(t *testing.T) {
	handle := &managerFakeHandle{
		candidate: Candidate{ProviderID: "blocked-start", URL: "https://blocked-start.example.test"},
		wait:      make(chan error, 1),
	}
	provider := &managerFakeProvider{
		id: "blocked-start", handle: handle, release: make(chan struct{}), bindLifetimeToContext: true,
	}
	startedAt := time.Now()
	runtime, err := (Manager{StartTimeout: 20 * time.Millisecond}).Start(
		context.Background(), []Selection{managerSelection(provider, RegionGlobal)}, StartRequest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("blocked provider start ignored timeout: %s", elapsed)
	}
	attempt := runtime.Snapshot().Attempts[0]
	if attempt.Status != AttemptDegraded || attempt.ErrorClass != "timeout" {
		t.Fatalf("blocked provider timeout attempt = %#v", attempt)
	}
}

func TestManagerReapsCanceledLateHandleBeforeStartingFallback(t *testing.T) {
	stopRelease := make(chan struct{})
	firstStarted := make(chan struct{})
	firstHandle := &managerFakeHandle{
		candidate: Candidate{ProviderID: "late", URL: "https://late.example.test"},
		wait:      make(chan error, 1), stopBlock: stopRelease,
	}
	secondStarted := make(chan struct{})
	secondHandle := &managerFakeHandle{
		candidate: Candidate{ProviderID: "fallback", URL: "https://fallback.example.test"},
		wait:      make(chan error, 1),
	}
	result := make(chan *Runtime, 1)
	errResult := make(chan error, 1)
	go func() {
		runtime, err := (Manager{
			MaxActive: 1, StartTimeout: 20 * time.Millisecond,
			Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
				return ProbeEvidence{DNSOK: true, TLSOK: true, HealthOK: true}, nil
			},
		}).Start(context.Background(), []Selection{
			managerSelection(&managerFakeProvider{
				id: "late", handle: firstHandle, started: firstStarted,
				release: make(chan struct{}), returnHandleOnCancel: true,
			}, RegionGlobal),
			managerSelection(&managerFakeProvider{id: "fallback", handle: secondHandle, started: secondStarted}, RegionGlobal),
		}, StartRequest{})
		result <- runtime
		errResult <- err
	}()
	<-firstStarted
	select {
	case <-secondStarted:
		t.Fatal("fallback started before timed-out provider handle was reaped")
	case <-time.After(75 * time.Millisecond):
	}
	close(stopRelease)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("fallback did not start after timed-out provider was reaped")
	}
	runtime := <-result
	if err := <-errResult; err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Stop(context.Background()) }()
	snapshot := runtime.Snapshot()
	if firstHandle.stops.Load() != 1 || snapshot.Attempts[0].ErrorClass != "timeout" || snapshot.Attempts[1].Status != AttemptHealthy {
		t.Fatalf("late-handle cleanup snapshot = %#v stops=%d", snapshot, firstHandle.stops.Load())
	}
}

func TestManagerParentCancellationCauseWinsOverLaterStartupTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	provider := &managerFakeProvider{
		id: "parent-canceled",
		handle: &managerFakeHandle{
			candidate: Candidate{ProviderID: "parent-canceled", URL: "https://parent-canceled.example.test"},
			wait:      make(chan error, 1),
		},
		started: started, release: make(chan struct{}), cancelReturnDelay: 75 * time.Millisecond,
	}
	result := make(chan *Runtime, 1)
	go func() {
		runtime, _ := (Manager{StartTimeout: 20 * time.Millisecond}).Start(
			ctx, []Selection{managerSelection(provider, RegionGlobal)}, StartRequest{},
		)
		result <- runtime
	}()
	<-started
	cancel()
	runtime := <-result
	if attempt := runtime.Snapshot().Attempts[0]; attempt.ErrorClass != "canceled" {
		t.Fatalf("parent cancellation was misclassified after timeout: %#v", attempt)
	}
}

func TestManagerReleasesProviderContextOnSpontaneousExit(t *testing.T) {
	contextDone := make(chan struct{})
	handle := &managerFakeHandle{
		candidate: Candidate{ProviderID: "spontaneous", URL: "https://spontaneous.example.test"},
		wait:      make(chan error, 1),
	}
	provider := &managerFakeProvider{id: "spontaneous", handle: handle, contextDone: contextDone}
	runtime, err := (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
		return ProbeEvidence{HealthOK: true}, nil
	}}).Start(context.Background(), []Selection{managerSelection(provider, RegionGlobal)}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Stop(context.Background()) }()
	handle.wait <- errors.New("provider exited")
	select {
	case <-contextDone:
	case <-time.After(time.Second):
		t.Fatal("provider context was not released after Wait completed")
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
	eventually(t, time.Second, func() bool {
		snapshot := runtime.Snapshot()
		return snapshot.Attempts[0].Status == AttemptExited && len(snapshot.Candidates) == 0
	})
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeExitRacingStopNeverRetainsCandidate(t *testing.T) {
	handle := &managerFakeHandle{candidate: Candidate{ProviderID: "racing", URL: "https://racing.example.test"}, wait: make(chan error, 1)}
	runtime, err := (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
		return ProbeEvidence{DNSOK: true, TLSOK: true, HealthOK: true}, nil
	}}).Start(context.Background(), []Selection{managerSelection(&managerFakeProvider{id: "racing", handle: handle}, RegionGlobal)}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	done := make(chan struct{}, 2)
	go func() { <-start; handle.wait <- errors.New("racing exit"); done <- struct{}{} }()
	go func() { <-start; _ = runtime.Stop(context.Background()); done <- struct{}{} }()
	close(start)
	<-done
	<-done
	snapshot := runtime.Snapshot()
	if len(snapshot.Candidates) != 0 || (snapshot.Attempts[0].Status != AttemptExited && snapshot.Attempts[0].Status != AttemptStopped) {
		t.Fatalf("exit/stop race retained candidate: %#v", snapshot)
	}
	if handle.stops.Load() != 1 {
		t.Fatalf("Stop called %d times, want once", handle.stops.Load())
	}
}

func TestRuntimeStopReturnsWhileProviderProbeIsBlocked(t *testing.T) {
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	handle := &managerFakeHandle{candidate: Candidate{ProviderID: "blocked", URL: "https://blocked.example.test"}, wait: make(chan error, 1)}
	runtime := &Runtime{
		snapshot: AvailabilitySet{SchemaVersion: AvailabilitySchemaVersion, Region: RegionGlobal, Attempts: []Attempt{{ProviderID: "blocked", Status: AttemptStarting}}},
		done:     make(chan struct{}), cleanupDone: make(chan struct{}), updates: make(chan struct{}, 1),
		candidates: make([]Candidate, 1), hasCandidate: make([]bool, 1),
	}
	startDone := make(chan struct{})
	go func() {
		_ = (Manager{Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
			close(probeStarted)
			<-releaseProbe
			return ProbeEvidence{HealthOK: true}, nil
		}}).startOne(context.Background(), runtime, 0, managerSelection(&managerFakeProvider{id: "blocked", handle: handle}, RegionGlobal), StartRequest{})
		close(startDone)
	}()
	<-probeStarted
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Stop(stopCtx); err != nil {
		t.Fatalf("Stop blocked behind provider probe: %v", err)
	}
	close(releaseProbe)
	<-startDone
	if snapshot := runtime.Snapshot(); len(snapshot.Candidates) != 0 {
		t.Fatalf("blocked probe stop retained candidate: %#v", snapshot)
	}
}

func TestManagerExitDuringProbeCannotBecomeHealthyAndStartsFallback(t *testing.T) {
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	firstHandle := &managerFakeHandle{candidate: Candidate{ProviderID: "first", URL: "https://first.example.test"}, wait: make(chan error, 1)}
	secondStarted := make(chan struct{})
	secondHandle := &managerFakeHandle{candidate: Candidate{ProviderID: "second", URL: "https://second.example.test"}, wait: make(chan error, 1)}
	result := make(chan *Runtime, 1)
	errResult := make(chan error, 1)
	go func() {
		runtime, err := (Manager{MaxActive: 1, Probe: func(_ context.Context, candidate Candidate) (ProbeEvidence, error) {
			if candidate.ProviderID == "first" {
				close(probeStarted)
				<-releaseProbe
			}
			return ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}}).Start(context.Background(), []Selection{
			managerSelection(&managerFakeProvider{id: "first", handle: firstHandle}, RegionGlobal),
			managerSelection(&managerFakeProvider{id: "second", handle: secondHandle, started: secondStarted}, RegionGlobal),
		}, StartRequest{})
		result <- runtime
		errResult <- err
	}()
	<-probeStarted
	firstHandle.wait <- errors.New("exited during probe")
	close(releaseProbe)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("fallback provider did not start after mid-probe exit")
	}
	runtime := <-result
	if err := <-errResult; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	snapshot := runtime.Snapshot()
	if snapshot.Attempts[0].Status != AttemptExited || snapshot.Attempts[1].Status != AttemptHealthy {
		t.Fatalf("unexpected attempts after mid-probe exit: %#v", snapshot.Attempts)
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

func TestRuntimeStopSignalsAllHandlesConcurrently(t *testing.T) {
	releaseFirst := make(chan struct{})
	first := &managerFakeHandle{candidate: Candidate{ProviderID: "first", URL: "https://first.example.test"}, wait: make(chan error, 1), stopBlock: releaseFirst}
	second := &managerFakeHandle{candidate: Candidate{ProviderID: "second", URL: "https://second.example.test"}, wait: make(chan error, 1)}
	runtime, err := (Manager{MaxActive: 2, Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
		return ProbeEvidence{HealthOK: true}, nil
	}}).Start(context.Background(), []Selection{
		managerSelection(&managerFakeProvider{id: "first", handle: first}, RegionGlobal),
		managerSelection(&managerFakeProvider{id: "second", handle: second}, RegionGlobal),
	}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := runtime.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want deadline exceeded", err)
	}
	eventually(t, time.Second, func() bool { return second.stops.Load() == 1 })
	close(releaseFirst)
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerMarksSelectionsBeyondLiveCapacitySkipped(t *testing.T) {
	selections := make([]Selection, 3)
	for i := range selections {
		id := fmt.Sprintf("healthy-%d", i)
		handle := &managerFakeHandle{candidate: Candidate{ProviderID: id, URL: "https://" + id + ".example.test"}, wait: make(chan error, 1)}
		selections[i] = managerSelection(&managerFakeProvider{id: id, handle: handle}, RegionGlobal)
	}
	runtime, err := (Manager{MaxActive: 2, Probe: func(context.Context, Candidate) (ProbeEvidence, error) {
		return ProbeEvidence{HealthOK: true}, nil
	}}).Start(context.Background(), selections, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	snapshot := runtime.Snapshot()
	if snapshot.Attempts[2].Status != AttemptSkipped {
		t.Fatalf("third attempt status = %q, want skipped; attempts=%#v", snapshot.Attempts[2].Status, snapshot.Attempts)
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

func TestManagerCandidateIDsPreserveReservedEscapes(t *testing.T) {
	escapedSlash := candidateID("provider", "https://example.com/a%2Fb")
	pathSlash := candidateID("provider", "https://example.com/a/b")
	if escapedSlash == pathSlash {
		t.Fatalf("reserved escaped slash collapsed into path separator: %q", escapedSlash)
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

func TestProbeSecureDialPrefersUsableIPv4WhenResolverAlsoReturnsPrivateIPv6(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Rdev-Gateway-Instance", "instance")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	evidence, err := probeGatewayHealthWithOptions(context.Background(), Candidate{ProviderID: "provider", URL: "https://public.example.test"}, "instance", probeOptions{
		Resolver: networkProbeResolver{
			IPv4: []netip.Addr{netip.MustParseAddr("8.8.8.8")},
			IPv6: []netip.Addr{netip.MustParseAddr("fd27:712::1")},
		},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, target.Host)
		},
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, // test-only certificate for routed public hostname
	})
	if err != nil {
		t.Fatalf("usable IPv4 was rejected alongside private IPv6: evidence=%#v err=%v", evidence, err)
	}
	if !evidence.DNSOK || !evidence.TCPConnectOK || !evidence.TLSOK || !evidence.HealthOK {
		t.Fatalf("probe evidence did not record all successful stages: %#v", evidence)
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
		w.Header().Set(TicketCodeSHA256Header, TicketCodeSHA256("ticket/code"))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
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
				w.Header().Set(TicketCodeSHA256Header, TicketCodeSHA256("ticket"))
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			evidence, err := probeBootstrapAssetWithOptions(context.Background(), Candidate{ProviderID: "provider", URL: "https://public.example.test"}, "ticket", "instance", publicTestProbeOptions(t, server))
			if tt.wantOK && (err != nil || !evidence.BootstrapOK || !evidence.SmallAssetOK) {
				t.Fatalf("valid bootstrap rejected: evidence=%#v err=%v", evidence, err)
			}
			if tt.wantOK && (!evidence.TicketBoundBootstrapOK || evidence.StaticBootstrapOK) {
				t.Fatalf("ticket-bound bootstrap evidence is ambiguous: %#v", evidence)
			}
			if !tt.wantOK && err == nil {
				t.Fatal("invalid bootstrap accepted")
			}
		})
	}
}

func TestProbeBootstrapAssetRejectsMissingWrongAndReplayedTicketHash(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		headerHash string
		cache      string
		wantOK     bool
	}{
		{name: "missing hash", cache: "no-store"},
		{name: "wrong hash", headerHash: TicketCodeSHA256("ticket-b"), cache: "no-store"},
		{name: "replayed other ticket", headerHash: TicketCodeSHA256("ticket-b"), cache: "no-store"},
		{name: "cacheable response", headerHash: TicketCodeSHA256("ticket-a"), cache: "public, max-age=300"},
		{name: "correct current ticket", headerHash: TicketCodeSHA256("ticket-a"), cache: "no-store", wantOK: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Rdev-Gateway-Instance", "instance")
				if r.URL.Path == "/healthz" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set(TicketCodeSHA256Header, testCase.headerHash)
				w.Header().Set("Cache-Control", testCase.cache)
				w.Header().Set("X-Content-Type-Options", "nosniff")
				_, _ = w.Write([]byte("$ErrorActionPreference = 'Stop'\nWrite-Host '[rdev]'"))
			}))
			defer server.Close()
			_, err := probeBootstrapAssetWithOptions(context.Background(), Candidate{ProviderID: "provider", URL: "https://public.example.test"}, "ticket-a", "instance", publicTestProbeOptions(t, server))
			if testCase.wantOK && err != nil {
				t.Fatalf("current ticket response rejected: %v", err)
			}
			if !testCase.wantOK && err == nil {
				t.Fatal("unbound or cacheable ticket response accepted")
			}
		})
	}
}

func TestProbeBootstrapTemplateRequiresStaticMarkerAndExactInstance(t *testing.T) {
	tests := []struct {
		name, instance, contentType, body string
		wantOK                            bool
	}{
		{name: "valid", instance: "instance", contentType: "text/plain; charset=utf-8", body: BootstrapProbePowerShell, wantOK: true},
		{name: "wrong instance", instance: "other", contentType: "text/plain", body: "$ErrorActionPreference = 'Stop'\n# rdev-bootstrap-probe-v1\n"},
		{name: "missing marker", instance: "instance", contentType: "text/plain", body: "$ErrorActionPreference = 'Stop'\n"},
		{name: "ticket bootstrap", instance: "instance", contentType: "text/plain", body: "$ErrorActionPreference = 'Stop'\n# rdev-bootstrap-probe-v1\n$TicketCode = 'ABCD-EFGH'\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Rdev-Gateway-Instance", tt.instance)
				w.Header().Set("Content-Type", tt.contentType)
				if r.URL.Path != "/v1/support-session/bootstrap-probe.ps1" {
					t.Fatalf("probe path = %q", r.URL.Path)
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			evidence, err := probeBootstrapTemplateWithOptions(context.Background(), Candidate{ProviderID: "provider", URL: "https://public.example.test"}, "instance", publicTestProbeOptions(t, server))
			if tt.wantOK && (err != nil || !evidence.BootstrapOK || !evidence.SmallAssetOK) {
				t.Fatalf("valid template rejected: evidence=%#v err=%v", evidence, err)
			}
			if tt.wantOK && (!evidence.StaticBootstrapOK || evidence.TicketBoundBootstrapOK) {
				t.Fatalf("static bootstrap evidence is ambiguous: %#v", evidence)
			}
			if !tt.wantOK && err == nil {
				t.Fatal("invalid template accepted")
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

type networkProbeResolver struct {
	IPv4 []netip.Addr
	IPv6 []netip.Addr
}

func (r networkProbeResolver) LookupNetIP(_ context.Context, network, _ string) ([]netip.Addr, error) {
	switch network {
	case "ip4":
		return append([]netip.Addr(nil), r.IPv4...), nil
	case "ip6":
		return append([]netip.Addr(nil), r.IPv6...), nil
	default:
		return append(append([]netip.Addr(nil), r.IPv6...), r.IPv4...), nil
	}
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
