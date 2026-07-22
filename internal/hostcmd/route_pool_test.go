package hostcmd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type routePoolFakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *routePoolFakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *routePoolFakeClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delta)
	c.mu.Unlock()
}

func TestRoutePoolInitializesWithFastestHealthyRouteAndBoundedProbes(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(100, 0)}
	var mu sync.Mutex
	active, maxActive := 0, 0
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	pool := newRoutePool([]routeCandidate{
		{URL: "https://slow.example.test", Transport: "poll"},
		{URL: "https://fast.example.test", Transport: "long-poll"},
		{URL: "https://down.example.test", Transport: "poll"},
	}, routePoolConfig{
		MaxConcurrent: 2,
		ProbeTimeout:  time.Second,
		Cooldown:      time.Minute,
		Now:           clock.Now,
		Probe: func(ctx context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				mu.Lock()
				active--
				mu.Unlock()
				return routeProbeResult{Err: ctx.Err()}
			}
			mu.Lock()
			active--
			mu.Unlock()
			switch route.URL {
			case "https://slow.example.test":
				return routeProbeResult{Healthy: true, Latency: 80 * time.Millisecond}
			case "https://fast.example.test":
				return routeProbeResult{Healthy: true, Latency: 20 * time.Millisecond}
			default:
				return routeProbeResult{Err: errors.New("unreachable")}
			}
		},
	})
	initialized := make(chan struct{})
	var selected routeCandidate
	var initErr error
	go func() {
		selected, initErr = pool.initialize(context.Background())
		close(initialized)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for bounded probes")
		}
	}
	select {
	case <-started:
		t.Fatal("started more than the configured probe bound")
	default:
	}
	close(release)
	select {
	case <-initialized:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for route pool initialization")
	}
	if initErr != nil {
		t.Fatal(initErr)
	}
	if selected.URL != "https://fast.example.test" || selected.Transport != "long-poll" {
		t.Fatalf("selected route = %#v, want fastest healthy route", selected)
	}
	mu.Lock()
	gotMax := maxActive
	mu.Unlock()
	if gotMax > 2 {
		t.Fatalf("maximum concurrent probes = %d, want <= 2", gotMax)
	}
}

func TestRoutePoolFailureSwitchesImmediatelyAndCoolsFailedRoute(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(200, 0)}
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "long-poll"},
	}, routePoolConfig{
		Cooldown: time.Minute,
		Now:      clock.Now,
		Probe: func(context.Context, routeCandidate) routeProbeResult {
			return routeProbeResult{Healthy: true, Latency: 10 * time.Millisecond}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	primary, ok := pool.current()
	if !ok {
		t.Fatal("pool has no current route")
	}
	if _, ok := pool.reportFailure(context.Background(), primary); !ok {
		t.Fatal("reportFailure did not select a healthy replacement")
	}
	current, _ := pool.current()
	if current.URL == primary.URL {
		t.Fatalf("failure left the primary route active: %#v", current)
	}
	if pool.routeEligible(primary, clock.Now()) {
		t.Fatal("failed route re-entered before cooldown")
	}
	clock.Advance(time.Minute)
	if !pool.routeEligible(primary, clock.Now()) {
		t.Fatal("failed route did not become eligible after cooldown")
	}
}

func TestRoutePoolLatencySwitchRequiresTwoConsecutiveTwentyPercentWins(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(300, 0)}
	var mu sync.Mutex
	probeCount := map[string]int{}
	pool := newRoutePool([]routeCandidate{
		{URL: "https://active.example.test", Transport: "poll"},
		{URL: "https://candidate.example.test", Transport: "poll"},
	}, routePoolConfig{
		ReprobeInterval: time.Millisecond,
		Now:             clock.Now,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			probeCount[route.URL]++
			count := probeCount[route.URL]
			mu.Unlock()
			if route.URL == "https://active.example.test" {
				return routeProbeResult{Healthy: true, Latency: 100 * time.Millisecond}
			}
			if count == 1 {
				return routeProbeResult{Healthy: true, Latency: 100 * time.Millisecond}
			}
			return routeProbeResult{Healthy: true, Latency: 70 * time.Millisecond}
		},
	})
	// Stable order makes the active route deterministic when the first sweep ties.
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, _ := pool.current()
	if active.URL != "https://active.example.test" {
		t.Fatalf("initial route = %#v, want active fixture", active)
	}
	clock.Advance(time.Millisecond)
	if switched, err := pool.probe(context.Background()); err != nil || switched {
		t.Fatalf("first latency probe switched=%v err=%v, want no switch", switched, err)
	}
	if got, _ := pool.current(); got.URL != active.URL {
		t.Fatalf("route switched after one fast probe: %#v", got)
	}
	clock.Advance(time.Millisecond)
	if switched, err := pool.probe(context.Background()); err != nil || !switched {
		t.Fatalf("second latency probe switched=%v err=%v, want switch", switched, err)
	}
	if got, _ := pool.current(); got.URL != "https://candidate.example.test" {
		t.Fatalf("route after hysteresis = %#v, want candidate", got)
	}
}

func TestRoutePoolInitializationHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	pool := newRoutePool([]routeCandidate{{URL: "https://blocked.example.test", Transport: "poll"}}, routePoolConfig{
		ProbeTimeout: time.Second,
		Probe: func(ctx context.Context, _ routeCandidate) routeProbeResult {
			close(started)
			<-ctx.Done()
			return routeProbeResult{Err: ctx.Err()}
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := pool.initialize(ctx)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("initialize error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initialize did not honor cancellation")
	}
}

func TestRoutePoolReentersAfterCooldownOnlyAfterSuccessfulRecoveryProbe(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(400, 0)}
	var mu sync.Mutex
	primaryHealthy := true
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://backup.example.test", Transport: "poll"},
	}, routePoolConfig{
		Cooldown: time.Second,
		Now:      clock.Now,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			healthy := primaryHealthy || route.URL != "https://primary.example.test"
			mu.Unlock()
			if !healthy {
				return routeProbeResult{Err: errors.New("primary unavailable")}
			}
			return routeProbeResult{Healthy: true, Latency: 10 * time.Millisecond}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	primary, _ := pool.current()
	mu.Lock()
	primaryHealthy = false
	mu.Unlock()
	if _, ok := pool.reportFailure(context.Background(), primary); !ok {
		t.Fatal("expected backup route after primary failure")
	}
	clock.Advance(time.Second)
	if switched, err := pool.probe(context.Background()); err != nil || switched {
		t.Fatalf("probe with failed primary switched=%v err=%v, want no switch", switched, err)
	}
	if got, _ := pool.current(); got.URL == primary.URL {
		t.Fatalf("failed primary was selected without a successful recovery probe: %#v", got)
	}
	mu.Lock()
	primaryHealthy = true
	mu.Unlock()
	clock.Advance(time.Second)
	if _, err := pool.probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	backup, _ := pool.current()
	if got, ok := pool.reportFailure(context.Background(), backup); !ok || got.URL != primary.URL {
		t.Fatalf("recovered primary was not selectable after a successful probe: %#v ok=%v", got, ok)
	}
}

func TestRoutePoolLatencyWinnerMustStayFastToAvoidFlapping(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(500, 0)}
	var mu sync.Mutex
	sweep := 0
	pool := newRoutePool([]routeCandidate{
		{URL: "https://a.example.test", Transport: "poll"},
		{URL: "https://b.example.test", Transport: "poll"},
	}, routePoolConfig{
		ReprobeInterval: time.Millisecond,
		Now:             clock.Now,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			currentSweep := sweep
			mu.Unlock()
			if route.URL == "https://a.example.test" {
				return routeProbeResult{Healthy: true, Latency: 100 * time.Millisecond}
			}
			if currentSweep == 0 {
				return routeProbeResult{Healthy: true, Latency: 100 * time.Millisecond}
			}
			if currentSweep == 1 {
				return routeProbeResult{Healthy: true, Latency: 70 * time.Millisecond}
			}
			if currentSweep == 2 {
				return routeProbeResult{Healthy: true, Latency: 90 * time.Millisecond}
			}
			return routeProbeResult{Healthy: true, Latency: 70 * time.Millisecond}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	for sweepNumber := 1; sweepNumber <= 3; sweepNumber++ {
		mu.Lock()
		sweep = sweepNumber
		mu.Unlock()
		clock.Advance(time.Millisecond)
		switched, err := pool.probe(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if sweepNumber == 2 && switched {
			t.Fatal("latency miss should reset the hysteresis streak")
		}
	}
	if got, _ := pool.current(); got.URL != "https://a.example.test" {
		t.Fatalf("route flapped after an alternating latency result: %#v", got)
	}
}

func TestRoutePoolLatencyWinnerChangeRestartsHysteresis(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(550, 0)}
	var mu sync.Mutex
	sweep := 0
	pool := newRoutePool([]routeCandidate{
		{URL: "https://active.example.test", Transport: "poll"},
		{URL: "https://b.example.test", Transport: "poll"},
		{URL: "https://c.example.test", Transport: "poll"},
	}, routePoolConfig{
		ReprobeInterval: time.Millisecond,
		Now:             clock.Now,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			currentSweep := sweep
			mu.Unlock()
			latency := 100 * time.Millisecond
			if currentSweep == 1 && route.URL == "https://b.example.test" {
				latency = 70 * time.Millisecond
			}
			if currentSweep == 1 && route.URL == "https://c.example.test" {
				latency = 75 * time.Millisecond
			}
			if currentSweep >= 2 && route.URL == "https://b.example.test" {
				latency = 75 * time.Millisecond
			}
			if currentSweep >= 2 && route.URL == "https://c.example.test" {
				latency = 70 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	for sweepNumber := 1; sweepNumber <= 2; sweepNumber++ {
		mu.Lock()
		sweep = sweepNumber
		mu.Unlock()
		clock.Advance(time.Millisecond)
		if switched, err := pool.probe(context.Background()); err != nil || switched {
			t.Fatalf("winner-changing sweep %d switched=%v err=%v", sweepNumber, switched, err)
		}
	}
	if got, _ := pool.current(); got.URL != "https://active.example.test" {
		t.Fatalf("winner change reused another route's streak: %#v", got)
	}
	mu.Lock()
	sweep = 3
	mu.Unlock()
	clock.Advance(time.Millisecond)
	if switched, err := pool.probe(context.Background()); err != nil || !switched {
		t.Fatalf("second consecutive c win switched=%v err=%v, want switch", switched, err)
	}
	if got, _ := pool.current(); got.URL != "https://c.example.test" {
		t.Fatalf("route after stable winner = %#v, want c", got)
	}
}

func TestRoutePoolIgnoresStaleSuccessAfterFailureSwitch(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(575, 0)}
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "poll"},
	}, routePoolConfig{
		Cooldown: time.Minute,
		Now:      clock.Now,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			latency := 20 * time.Millisecond
			if route.URL == "https://primary.example.test" {
				latency = 10 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	stale, ok := pool.currentSnapshot()
	if !ok {
		t.Fatal("missing active route snapshot")
	}
	if _, ok := pool.reportSnapshotFailure(context.Background(), stale); !ok {
		t.Fatal("failed active route did not switch")
	}
	pool.reportSnapshotSuccess(stale)
	if got, _ := pool.current(); got.URL != "https://secondary.example.test" {
		t.Fatalf("stale success changed active route: %#v", got)
	}
	if pool.routeEligible(stale.Candidate, clock.Now()) {
		t.Fatal("stale success cleared the failed route cooldown")
	}
}

func TestRoutePoolIgnoresProbeResultStartedBeforeActiveFailure(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(590, 0)}
	var mu sync.Mutex
	blockProbes := false
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "poll"},
	}, routePoolConfig{
		Now: clock.Now,
		Probe: func(ctx context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			blocked := blockProbes && route.URL == "https://primary.example.test"
			mu.Unlock()
			if blocked {
				startedOnce.Do(func() { close(started) })
				select {
				case <-release:
				case <-ctx.Done():
					return routeProbeResult{Err: ctx.Err()}
				}
			}
			latency := 20 * time.Millisecond
			if route.URL == "https://primary.example.test" {
				latency = 10 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	stale, ok := pool.currentSnapshot()
	if !ok {
		t.Fatal("missing primary snapshot")
	}
	mu.Lock()
	blockProbes = true
	mu.Unlock()
	probeDone := make(chan error, 1)
	go func() {
		_, err := pool.probe(context.Background())
		probeDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stale probe did not start")
	}
	if got, ok := pool.reportSnapshotFailure(context.Background(), stale); !ok || got.URL != "https://secondary.example.test" {
		t.Fatalf("active failure did not switch immediately: route=%#v ok=%v", got, ok)
	}
	close(release)
	select {
	case err := <-probeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale probe did not finish")
	}
	if got, _ := pool.current(); got.URL != "https://secondary.example.test" {
		t.Fatalf("stale probe result resurrected primary: %#v", got)
	}
	if pool.routeEligible(stale.Candidate, clock.Now()) {
		t.Fatal("stale probe result cleared primary cooldown")
	}
}

func TestRoutePoolRecoversExpiredAlternativeDuringImmediateFailover(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(595, 0)}
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "poll"},
	}, routePoolConfig{
		Cooldown: time.Second,
		Now:      clock.Now,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			latency := 20 * time.Millisecond
			if route.URL == "https://primary.example.test" {
				latency = 10 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	secondary := routeCandidate{URL: "https://secondary.example.test", Transport: "poll"}
	if current, ok := pool.reportFailure(context.Background(), secondary); !ok || current.URL != "https://primary.example.test" {
		t.Fatalf("non-active failure changed current route: %#v ok=%v", current, ok)
	}
	clock.Advance(time.Second)
	primary, _ := pool.current()
	if replacement, ok := pool.reportFailure(context.Background(), primary); !ok || replacement != secondary {
		t.Fatalf("expired alternative recovery = %#v ok=%v, want secondary", replacement, ok)
	}
}

func TestRoutePoolTrustProbeFailureWinsOverConcurrentRequestSuccess(t *testing.T) {
	var mu sync.Mutex
	failPrimary := false
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "poll"},
	}, routePoolConfig{
		Probe: func(ctx context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			fail := failPrimary && route.URL == "https://primary.example.test"
			mu.Unlock()
			if fail {
				startedOnce.Do(func() { close(started) })
				select {
				case <-release:
				case <-ctx.Done():
					return routeProbeResult{Err: ctx.Err()}
				}
				return routeProbeResult{Err: errors.New("trust verification failed")}
			}
			latency := 20 * time.Millisecond
			if route.URL == "https://primary.example.test" {
				latency = 10 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := pool.currentSnapshot()
	mu.Lock()
	failPrimary = true
	mu.Unlock()
	probeDone := make(chan error, 1)
	go func() {
		_, err := pool.probe(context.Background())
		probeDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("trust probe did not start")
	}
	if !pool.reportSnapshotSuccess(snapshot) {
		t.Fatal("current request success was unexpectedly stale")
	}
	close(release)
	if err := <-probeDone; err != nil {
		t.Fatal(err)
	}
	if got, _ := pool.current(); got.URL != "https://secondary.example.test" {
		t.Fatalf("request success masked trust probe failure: %#v", got)
	}
}

func TestRoutePoolMonitorSwitchCancelsActiveRouteRequest(t *testing.T) {
	var mu sync.Mutex
	failPrimary := false
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "poll"},
	}, routePoolConfig{
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			fail := failPrimary && route.URL == "https://primary.example.test"
			mu.Unlock()
			if fail {
				return routeProbeResult{Err: errors.New("route degraded")}
			}
			latency := 20 * time.Millisecond
			if route.URL == "https://primary.example.test" {
				latency = 10 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := pool.currentSnapshot()
	requestCtx, cancelRequest := routeRequestContext(context.Background(), snapshot, time.Second)
	defer cancelRequest()
	mu.Lock()
	failPrimary = true
	mu.Unlock()
	monitorCtx, cancelMonitor := context.WithCancel(context.Background())
	monitorDone := make(chan error, 1)
	go func() { monitorDone <- pool.monitor(monitorCtx, time.Millisecond) }()
	select {
	case <-requestCtx.Done():
	case <-time.After(time.Second):
		cancelMonitor()
		t.Fatal("route switch did not cancel the active request")
	}
	if got, _ := pool.current(); got.URL != "https://secondary.example.test" {
		cancelMonitor()
		t.Fatalf("monitor selected %#v, want secondary", got)
	}
	cancelMonitor()
	if err := <-monitorDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("monitor exit = %v, want cancellation", err)
	}
}

func TestRoutePoolCapsResourcesAndRecoversAfterAllInitialProbesFail(t *testing.T) {
	clock := &routePoolFakeClock{now: time.Unix(600, 0)}
	candidates := make([]routeCandidate, 0, 20)
	for index := 0; index < 20; index++ {
		candidates = append(candidates, routeCandidate{URL: "https://route-" + fmt.Sprint(index) + ".example.test", Transport: "poll"})
	}
	var mu sync.Mutex
	recovered := false
	pool := newRoutePool(candidates, routePoolConfig{
		MaxConcurrent: 100,
		Cooldown:      time.Second,
		Now:           clock.Now,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			healthy := recovered && route == candidates[0]
			mu.Unlock()
			if healthy {
				return routeProbeResult{Healthy: true, Latency: time.Millisecond}
			}
			return routeProbeResult{Err: errors.New("down")}
		},
	})
	if len(pool.routes) != maxGatewayRoutes || cap(pool.probeSlots) != maxRouteProbeWorkers {
		t.Fatalf("pool resources routes=%d probe_slots=%d", len(pool.routes), cap(pool.probeSlots))
	}
	if _, err := pool.initialize(context.Background()); !errors.Is(err, errNoHealthyRoutes) {
		t.Fatalf("initialization error = %v, want no healthy routes", err)
	}
	if _, ok := pool.current(); ok {
		t.Fatal("pool exposed a failed initial route")
	}
	if _, err := pool.initialize(context.Background()); !errors.Is(err, errNoHealthyRoutes) {
		t.Fatalf("repeat initialization bypassed cooldown: %v", err)
	}
	mu.Lock()
	recovered = true
	mu.Unlock()
	clock.Advance(time.Second)
	if switched, err := pool.probe(context.Background()); err != nil || !switched {
		t.Fatalf("recovery probe switched=%v err=%v", switched, err)
	}
	if got, _ := pool.current(); got != candidates[0] {
		t.Fatalf("recovered route = %#v, want first candidate", got)
	}
	var nilPool *routePool
	if err := nilPool.monitor(context.Background(), time.Millisecond); !errors.Is(err, errNoHealthyRoutes) {
		t.Fatalf("nil monitor error = %v", err)
	}
}

func TestRoutePoolCurrentLockedAndNoReplacementFailClosed(t *testing.T) {
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "poll"},
	}, routePoolConfig{
		Cooldown: time.Minute,
		Probe: func(context.Context, routeCandidate) routeProbeResult {
			return routeProbeResult{Healthy: true, Latency: time.Millisecond}
		},
	})
	pool.mu.Lock()
	if _, ok := pool.currentLocked(); ok {
		t.Fatal("uninitialized pool exposed a current route")
	}
	pool.initialized = true
	pool.active = 0
	pool.routes[0].healthy = true
	if got, ok := pool.currentLocked(); !ok || got.URL != "https://primary.example.test" {
		t.Fatalf("currentLocked() = %#v ok=%v", got, ok)
	}
	pool.mu.Unlock()
	primary, _ := pool.current()
	secondary := routeCandidate{URL: "https://secondary.example.test", Transport: "poll"}
	pool.reportFailure(context.Background(), secondary)
	if _, ok := pool.reportFailure(context.Background(), primary); ok {
		t.Fatal("all routes in cooldown produced a replacement")
	}
	if _, ok := pool.current(); ok {
		t.Fatal("pool exposed a cooled route after replacement failure")
	}
}

func TestRoutePoolSharesGatewayProbeAcrossMaintainedAdapters(t *testing.T) {
	probeCalls := 0
	pool := newRoutePool([]routeCandidate{
		{URL: "https://gateway.example.test", Transport: "long-poll"},
		{URL: "https://gateway.example.test", Transport: "poll"},
	}, routePoolConfig{
		ShareGatewayHealth: true,
		Probe: func(context.Context, routeCandidate) routeProbeResult {
			probeCalls++
			return routeProbeResult{Healthy: true, Latency: 10 * time.Millisecond}
		},
	})
	selected, err := pool.initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if probeCalls != 1 || selected.Transport != "long-poll" {
		t.Fatalf("probe_calls=%d selected=%#v, want one gateway probe and deterministic long-poll preference", probeCalls, selected)
	}
	if replacement, ok := pool.reportFailure(context.Background(), selected); !ok || replacement.Transport != "poll" {
		t.Fatalf("transport fallback = %#v ok=%v, want poll adapter", replacement, ok)
	}
}

func TestRouteCandidatesFromManifestRejectsUnsafeURLsAndUnsupportedTransports(t *testing.T) {
	routes := routeCandidatesFromManifest("https://primary.example.test/base", []model.JoinManifestGatewayCandidate{
		{URL: "https://ok.example.test/relay"},
		{URL: "https://ok.example.test/relay?x=1"},
		{URL: "https://ok.example.test/../escape"},
		{URL: "http://public.example.test:8080"},
		{URL: "http://192.168.1.20:8787"},
	}, "auto")
	if len(routes) != 4 {
		t.Fatalf("safe route expansion = %#v, want primary + one candidate across two adapters", routes)
	}
	if got := routeCandidatesFromManifest("https://primary.example.test", nil, "wss"); got != nil {
		t.Fatalf("unsupported wss transport produced routes: %#v", got)
	}
}
