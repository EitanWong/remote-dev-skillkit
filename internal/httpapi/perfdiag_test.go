//go:build perfdiag

package httpapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

// Fixed time and synthetic data isolate persistence costs; these are not ACK or
// live-host E2E tests. Keys and leases remain cryptographically random and private.
const perfHTTPSeed int64 = 20260930

type perfHTTPClient struct {
	session, endpoint string
	lease             controlplane.Lease
}

type perfHTTPFixture struct {
	gw           *gateway.MemoryGateway
	store        gateway.FileStateStore
	handler      http.Handler
	clients      []perfHTTPClient
	initial      gateway.Snapshot
	clock        func() time.Time
	public       ed25519.PublicKey
	private      ed25519.PrivateKey
	meter        *perfMeteredStore
	initialBytes int64
}

func newPerfHTTPFixture(t *testing.T, history, clients int) *perfHTTPFixture {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("ephemeral signing key generation failed")
	}
	clock := func() time.Time { return time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC) }
	gw := gateway.NewMemoryGatewayWithSigningKey(clock, "perf-http", public, private)
	rng := mathrand.New(mathrand.NewSource(perfHTTPSeed))
	for i := 0; i < history; i++ {
		gw.AppendAudit("synthetic", "perf.history", fmt.Sprintf("synthetic-%08d", i), fmt.Sprintf("sample-%016x", rng.Uint64()))
	}
	f := &perfHTTPFixture{gw: gw, clock: clock, public: public, private: private}
	for i := 0; i < clients; i++ {
		session, err := gw.CreateSession(controlplane.SessionSpec{Reason: fmt.Sprintf("perf-client-%08d", i)})
		if err != nil {
			t.Fatal("direct session setup failed")
		}
		_, endpoint, lease, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
			Name: fmt.Sprintf("perf-client-%08d", i), IdentityFingerprint: fmt.Sprintf("perf-fingerprint-%08d", i),
			Role: "target", Platform: "linux/amd64", Transport: "long-poll",
		})
		if err != nil || lease.Secret == "" {
			t.Fatal("direct endpoint setup failed")
		}
		f.clients = append(f.clients, perfHTTPClient{session: session.ID, endpoint: endpoint.ID, lease: lease})
	}
	// Explicit scratch root prevents any fixture from falling back to /tmp.
	scratch := os.Getenv("TMPDIR")
	if !filepath.IsAbs(scratch) {
		t.Fatal("absolute profile TMPDIR is required")
	}
	dir, err := os.MkdirTemp(scratch, "perf-http-")
	if err != nil {
		t.Fatal("scratch directory creation failed")
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error("scratch directory cleanup failed")
		}
	})
	f.store, err = gateway.NewFileStateStore(filepath.Join(dir, "snapshot.json"))
	if err != nil {
		t.Fatal("file state store construction failed")
	}
	f.initial, err = f.store.SaveFrom(gw)
	if err != nil {
		t.Fatal("initial snapshot save failed")
	}
	if len(f.initial.Audit) != history+2*clients {
		t.Fatal("unexpected setup audit count")
	}
	info, err := os.Stat(f.store.Path)
	if err != nil {
		t.Fatal("initial snapshot stat failed")
	}
	f.initialBytes = info.Size()
	f.meter = &perfMeteredStore{base: f.store}
	f.handler = NewServerWithStateStore(gw, f.meter).Handler()
	return f
}

type perfHTTPRequest struct {
	History    int     `json:"history"`
	Clients    int     `json:"clients"`
	Client     int     `json:"client"`
	Repetition int     `json:"repetition"`
	LatencyMS  float64 `json:"latency_ms"`
	Status     int     `json:"status"`
}

func (f *perfHTTPFixture) poll(index int) (float64, int, bool) {
	client := &f.clients[index]
	path := "/v1/sessions/" + url.PathEscape(client.session) + "/events?endpoint_id=" + url.QueryEscape(client.endpoint) + "&after_seq=0&limit=1&wait_ms=0"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+client.lease.Secret)
	rec := httptest.NewRecorder()
	started := time.Now()
	f.handler.ServeHTTP(rec, req)
	elapsed := float64(time.Since(started).Nanoseconds()) / 1e6
	if rec.Code != http.StatusOK {
		return elapsed, rec.Code, false
	}
	var body struct {
		Lease controlplane.Lease `json:"lease"`
	}
	if json.NewDecoder(rec.Body).Decode(&body) != nil || body.Lease.Secret == "" ||
		body.Lease.SessionID != client.session || body.Lease.EndpointID != client.endpoint ||
		body.Lease.Generation != client.lease.Generation+1 || body.Lease.Secret == client.lease.Secret ||
		!body.Lease.ExpiresAt.After(f.clock()) {
		return elapsed, rec.Code, false
	}
	if f.gw.ValidateSessionLease(client.session, client.endpoint, body.Lease.Secret) != nil {
		return elapsed, rec.Code, false
	}
	client.lease = body.Lease
	return elapsed, rec.Code, true
}

func (f *perfHTTPFixture) verify(t *testing.T) int64 {
	t.Helper()
	info, err := os.Stat(f.store.Path)
	if err != nil {
		t.Fatal("snapshot stat failed")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatal("snapshot is not a regular 0600 file")
	}
	restarted := gateway.NewMemoryGatewayWithSigningKey(f.clock, "perf-http", f.public, f.private)
	loaded, found, err := f.store.LoadInto(restarted)
	if err != nil || !found {
		t.Fatal("snapshot restart load failed")
	}
	live, restored := f.gw.Snapshot(), restarted.Snapshot()
	for _, snapshot := range []gateway.Snapshot{loaded, live, restored} {
		if !reflect.DeepEqual(snapshot.Audit, f.initial.Audit) {
			t.Fatal("audit history changed or was lost")
		}
		for i, event := range snapshot.Audit {
			if event.Sequence != i+1 || !event.At.Equal(f.clock()) {
				t.Fatal("audit sequence or clock mismatch")
			}
		}
		if len(snapshot.ControlPlane.Sessions) != len(f.clients) || len(snapshot.ControlPlane.Leases) != len(f.clients) ||
			!reflect.DeepEqual(snapshot.ControlPlane.Events, f.initial.ControlPlane.Events) {
			t.Fatal("session count, lease count, or event history mismatch")
		}
		endpoints := 0
		for _, session := range snapshot.ControlPlane.Sessions {
			endpoints += len(session.Endpoints)
			events := snapshot.ControlPlane.Events[session.ID]
			if len(session.Endpoints) != 1 || len(events) == 0 || session.LastSeq != uint64(len(events)) {
				t.Fatal("endpoint count or last sequence mismatch")
			}
			for i, event := range events {
				if event.Seq != uint64(i+1) || event.SessionID != session.ID {
					t.Fatal("session event sequence mismatch")
				}
			}
		}
		if endpoints != len(f.clients) {
			t.Fatal("endpoint total mismatch")
		}
	}
	for _, client := range f.clients {
		before, err1 := f.gw.Session(client.session)
		after, err2 := restarted.Session(client.session)
		if err1 != nil || err2 != nil || !reflect.DeepEqual(before, after) ||
			restarted.ValidateSessionLease(client.session, client.endpoint, client.lease.Secret) != nil {
			t.Fatal("session or renewed lease was lost on restart")
		}
	}
	return info.Size()
}

func perfHTTPInts(t *testing.T, name, fallback string, max int) []int {
	t.Helper()
	raw, set := os.LookupEnv(name)
	if !set {
		raw = fallback
	}
	var values []int
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || (max > 0 && n > max) {
			t.Fatalf("invalid %s: expected positive integers within diagnostic bounds", name)
		}
		values = append(values, n)
	}
	return values
}

func perfHTTPLog(t *testing.T, label string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal("numeric diagnostic encoding failed")
	}
	t.Logf("%s %s", label, data)
}

func TestPerfHTTPActiveHistoryMatrix(t *testing.T) {
	histories := perfHTTPInts(t, "PERF_DIAG_EVENTS", "1000,10000,20000", 700000)
	clients := perfHTTPInts(t, "PERF_DIAG_CLIENTS", "20,40", 80)
	pollValues := perfHTTPInts(t, "PERF_DIAG_POLLS", "2", 20)
	if len(pollValues) != 1 {
		t.Fatal("PERF_DIAG_POLLS must be a single integer")
	}
	polls := pollValues[0]
	for _, history := range histories {
		if history > 20000 && os.Getenv("PERF_DIAG_LARGE") != "1" {
			t.Fatal("histories above 20000 require PERF_DIAG_LARGE=1")
		}
	}
	for _, history := range histories {
		for _, clients := range clients {
			t.Run(fmt.Sprintf("history_%d/clients_%d", history, clients), func(t *testing.T) {
				f := newPerfHTTPFixture(t, history, clients)
				requests := make([]perfHTTPRequest, clients*polls)
				valid := make([]bool, len(requests))
				var wg sync.WaitGroup
				start := make(chan struct{})
				for client := 0; client < clients; client++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						for repetition := 0; repetition < polls; repetition++ {
							index := client*polls + repetition
							latency, status, ok := f.poll(client)
							requests[index] = perfHTTPRequest{history, clients, client, repetition, latency, status}
							valid[index] = ok
						}
					}()
				}
				var before, after runtime.MemStats
				goroutinesBefore := runtime.NumGoroutine()
				runtime.ReadMemStats(&before)
				started := time.Now()
				close(start)
				wg.Wait()
				elapsed := time.Since(started).Seconds()
				runtime.ReadMemStats(&after)
				goroutinesAfter := runtime.NumGoroutine()
				latencies := make([]float64, len(requests))
				successes := 0
				for i, request := range requests {
					perfHTTPLog(t, "PERF_REQUEST", request)
					latencies[i] = request.LatencyMS
					if valid[i] {
						successes++
					}
				}
				sort.Float64s(latencies)
				percentile := func(p float64) float64 { return latencies[int(math.Ceil(p*float64(len(latencies))))-1] }
				info, err := os.Stat(f.store.Path)
				if err != nil {
					t.Fatal("measured snapshot stat failed")
				}
				f.meter.emit(t, history, clients, len(requests), elapsed, f.initialBytes, info.Size())
				perfHTTPLog(t, "PERF_SUMMARY", map[string]any{
					"history": history, "clients": clients, "polls": polls, "audit_count": len(f.initial.Audit),
					"requests": len(requests), "successes": successes, "failures": len(requests) - successes,
					"elapsed_seconds": elapsed, "rate_requests_per_second": float64(len(requests)) / elapsed,
					"p50_ms": percentile(.50), "p95_ms": percentile(.95), "p99_ms": percentile(.99), "max_ms": latencies[len(latencies)-1],
					"heap_before_bytes": before.HeapAlloc, "heap_after_bytes": after.HeapAlloc, "heap_delta_bytes": int64(after.HeapAlloc) - int64(before.HeapAlloc),
					"totalalloc_delta_bytes": after.TotalAlloc - before.TotalAlloc, "numgc_delta": after.NumGC - before.NumGC, "pause_delta_ns": after.PauseTotalNs - before.PauseTotalNs,
					"goroutines_before": goroutinesBefore, "goroutines_after": goroutinesAfter, "goroutines_delta": goroutinesAfter - goroutinesBefore,
					"snapshot_bytes": info.Size(),
				})
				if successes != len(requests) {
					t.Fatalf("poll status or renewed lease validation failed: %d requests", len(requests)-successes)
				}
				f.verify(t)
				if os.Getenv("PERF_DIAG_ENFORCE_SLO") == "1" && (percentile(.95) > 200 || percentile(.99) > 500) {
					t.Fatalf("frozen handler latency budget exceeded: p95_ms=%.3f limit=200 p99_ms=%.3f limit=500", percentile(.95), percentile(.99))
				}
			})
		}
	}
}

func TestPerfHTTPPollWriteAmplificationGrowth(t *testing.T) {
	// Explicit diagnostic budget: archival history must not scale hot-path poll
	// rewrites. This intentionally stays RED while full snapshots are rewritten.
	var sizes [2]int64
	for i, history := range []int{1000, 10000} {
		f := newPerfHTTPFixture(t, history, 1)
		_, status, valid := f.poll(0)
		if status != http.StatusOK || !valid {
			t.Fatalf("growth fixture poll failed: history=%d status=%d", history, status)
		}
		sizes[i] = f.verify(t)
	}
	if sizes[1] > 2*sizes[0] {
		t.Fatalf("diagnostic archive/hot-path budget exceeded: small_bytes=%d large_bytes=%d budget_multiplier=2", sizes[0], sizes[1])
	}
}
