package hostcmd

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRoutePoolReconcilesNewActiveRouteAgainstInFlightProbe(t *testing.T) {
	var mu sync.Mutex
	probePhase := false
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	pool := newRoutePool([]routeCandidate{
		{URL: "https://a.example.test", Transport: "poll"},
		{URL: "https://b.example.test", Transport: "poll"},
		{URL: "https://c.example.test", Transport: "poll"},
	}, routePoolConfig{
		Probe: func(ctx context.Context, route routeCandidate) routeProbeResult {
			mu.Lock()
			phase := probePhase
			mu.Unlock()
			if phase && route.URL == "https://a.example.test" {
				startedOnce.Do(func() { close(started) })
				select {
				case <-release:
				case <-ctx.Done():
					return routeProbeResult{Err: ctx.Err()}
				}
			}
			if phase && route.URL == "https://b.example.test" {
				return routeProbeResult{Err: errors.New("b degraded")}
			}
			latency := 30 * time.Millisecond
			if route.URL == "https://a.example.test" {
				latency = 10 * time.Millisecond
			} else if route.URL == "https://b.example.test" {
				latency = 20 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	if _, err := pool.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	aSnapshot, _ := pool.currentSnapshot()
	mu.Lock()
	probePhase = true
	mu.Unlock()
	probeDone := make(chan error, 1)
	go func() {
		_, err := pool.probe(context.Background())
		probeDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("in-flight probe did not start")
	}
	if got, ok := pool.reportSnapshotFailure(context.Background(), aSnapshot); !ok || got.URL != "https://b.example.test" {
		t.Fatalf("request failure selected %#v ok=%v, want b", got, ok)
	}
	close(release)
	if err := <-probeDone; err != nil {
		t.Fatal(err)
	}
	if got, ok := pool.current(); !ok || got.URL != "https://c.example.test" {
		t.Fatalf("post-probe active route = %#v ok=%v, want healthy c", got, ok)
	}
	b := routeCandidate{URL: "https://b.example.test", Transport: "poll"}
	if pool.routeEligible(b, time.Now()) {
		t.Fatal("failed b route remained eligible")
	}
}
