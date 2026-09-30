//go:build perfdiag

package gateway

// Synthetic, isolated gateway microbenchmarks only: no HTTP, host, or production
// workload claims. Run with -tags=perfdiag -bench=BenchmarkPerfHistory -benchmem
// -benchtime=100ms -run='^$'. Snapshot export growth is diagnostic ONLY,
// not a production SLO: a full export intrinsically materializes history.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const perfDiagSeed int64 = 20260930

var perfDiagTime = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

func perfDiagCounts(tb testing.TB) []int {
	tb.Helper()
	value := os.Getenv("PERF_DIAG_EVENTS")
	if value == "" {
		return []int{1000, 10000, 20000}
	}
	var counts []int
	seen := make(map[int]bool)
	for _, part := range strings.Split(value, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > 700000 {
			tb.Fatal("PERF_DIAG_EVENTS must contain integers in [1,700000]")
		}
		if n > 20000 && os.Getenv("PERF_DIAG_LARGE") != "1" {
			tb.Fatal("histories above 20000 require PERF_DIAG_LARGE=1")
		}
		if seen[n] {
			tb.Fatal("PERF_DIAG_EVENTS must not contain duplicates")
		}
		seen[n] = true
		counts = append(counts, n)
	}
	return counts
}

func perfDiagFixture(n int) *MemoryGateway {
	gw := NewMemoryGatewayWithClock(func() time.Time { return perfDiagTime })
	rng := rand.New(rand.NewSource(perfDiagSeed))
	for i := 0; i < n; i++ {
		gw.AppendAudit("synthetic", "perfdiag.event", fmt.Sprintf("fixture-%016x", rng.Uint64()), fmt.Sprintf("synthetic-%016x", rng.Uint64()))
	}
	return gw
}

func perfDiagAudit(tb testing.TB, events []model.AuditEvent, n int) {
	tb.Helper()
	if len(events) != n {
		tb.Fatalf("audit count: got %d want %d", len(events), n)
	}
	rng := rand.New(rand.NewSource(perfDiagSeed))
	for i, event := range events {
		target := fmt.Sprintf("fixture-%016x", rng.Uint64())
		message := fmt.Sprintf("synthetic-%016x", rng.Uint64())
		if event.Sequence != i+1 || !event.At.Equal(perfDiagTime) || event.Actor != "synthetic" || event.Action != "perfdiag.event" || event.TargetID != target || event.Message != message {
			tb.Fatalf("audit integrity mismatch at index %d", i)
		}
	}
}

func perfDiagSnapshot(tb testing.TB, snapshot Snapshot, n int) {
	tb.Helper()
	if snapshot.SchemaVersion != SnapshotSchemaVersion || !snapshot.GeneratedAt.Equal(perfDiagTime) {
		tb.Fatal("snapshot schema or fixed clock mismatch")
	}
	perfDiagAudit(tb, snapshot.Audit, n)
}

func perfDiagPermissions(tb testing.TB, path string) {
	tb.Helper()
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{{path, 0o600}, {filepath.Dir(path), 0o700}} {
		info, err := os.Stat(item.path)
		if err != nil {
			tb.Fatal(err)
		}
		// Unix permission bits are not Windows ACL evidence.
		if runtime.GOOS != "windows" && info.Mode().Perm() != item.mode {
			tb.Fatalf("permissions: got %o want %o", info.Mode().Perm(), item.mode)
		}
	}
}

func perfDiagRoundTrip(tb testing.TB, gw *MemoryGateway, path string, n int) {
	tb.Helper()
	before := gw.Snapshot()
	saved, err := gw.SaveSnapshot(path)
	if err != nil {
		tb.Fatal(err)
	}
	perfDiagPermissions(tb, path)
	loaded, err := gw.LoadSnapshot(path)
	if err != nil {
		tb.Fatal(err)
	}
	current := gw.Snapshot()
	want, err := json.Marshal(before)
	if err != nil {
		tb.Fatal(err)
	}
	for _, snapshot := range []Snapshot{before, saved, loaded, current} {
		perfDiagSnapshot(tb, snapshot, n)
		got, err := json.Marshal(snapshot)
		if err != nil {
			tb.Fatal(err)
		}
		if !bytes.Equal(want, got) {
			tb.Fatal("snapshot roundtrip mismatch (content suppressed)")
		}
	}
}

func TestPerfHistoryFixtureIntegrity(t *testing.T) {
	for _, n := range perfDiagCounts(t) {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			gw := perfDiagFixture(n)
			perfDiagAudit(t, gw.AuditEvents(), n)
			perfDiagRoundTrip(t, gw, filepath.Join(t.TempDir(), "state", "snapshot.json"), n)
		})
	}
}

func TestPerfHistorySnapshotGrowthDiagnostic(t *testing.T) {
	// Diagnostic ONLY, not a production SLO; full exports materialize history.
	measure := func(n int) testing.BenchmarkResult {
		gw := perfDiagFixture(n)
		perfDiagSnapshot(t, gw.Snapshot(), n)
		var snapshot Snapshot
		r := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				snapshot = gw.Snapshot()
			}
			b.StopTimer()
		})
		perfDiagSnapshot(t, snapshot, n)
		perfDiagAudit(t, gw.AuditEvents(), n)
		return r
	}
	small, large := measure(1000), measure(10000)
	if small.AllocedBytesPerOp() <= 0 {
		t.Fatal("invalid zero allocation baseline")
	}
	ratio := float64(large.AllocedBytesPerOp()) / float64(small.AllocedBytesPerOp())
	t.Logf("events_small=1000 events_large=10000 bytes_small=%d bytes_large=%d allocs_small=%d allocs_large=%d ns_small=%d ns_large=%d ratio=%.4f", small.AllocedBytesPerOp(), large.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocsPerOp(), small.NsPerOp(), large.NsPerOp(), ratio)
}

func TestPerfHistorySaveGrowthBudget(t *testing.T) {
	// Fixed histories; fixture creation, validation, and reload are not timed.
	// The caller supplies -benchtime=100ms to testing.Benchmark.
	measure := func(n int) testing.BenchmarkResult {
		gw := perfDiagFixture(n)
		path := filepath.Join(t.TempDir(), "state", "snapshot.json")
		store := FileStateStore{Path: path}
		perfDiagRoundTrip(t, gw, path, n)
		var saved Snapshot
		r := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				saved, err = store.SaveFrom(gw)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
		})
		perfDiagSnapshot(t, saved, n)
		perfDiagPermissions(t, path)
		loaded, exists, err := store.LoadInto(gw)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatal("saved state missing")
		}
		for _, snapshot := range []Snapshot{loaded, gw.Snapshot()} {
			perfDiagSnapshot(t, snapshot, n)
			want, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want, got) {
				t.Fatal("saved state integrity mismatch (content suppressed)")
			}
		}
		return r
	}
	small, large := measure(1000), measure(10000)
	if small.AllocedBytesPerOp() <= 0 {
		t.Fatal("invalid zero allocation baseline")
	}
	ratio := float64(large.AllocedBytesPerOp()) / float64(small.AllocedBytesPerOp())
	t.Logf("events_small=1000 events_large=10000 bytes_small=%d bytes_large=%d allocs_small=%d allocs_large=%d ns_small=%d ns_large=%d ratio=%.4f budget=2.0000", small.AllocedBytesPerOp(), large.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocsPerOp(), small.NsPerOp(), large.NsPerOp(), ratio)
	if ratio > 2 {
		t.Fatalf("SaveFrom allocation growth ratio %.4f exceeds fixed 2x budget", ratio)
	}
}

func BenchmarkPerfHistoryPollSnapshotContention(b *testing.B) {
	// Each measured batch has fixed work and at most 20008 events. Snapshot
	// and AppendAudit contend on gw.mu; this is not a production latency SLO.
	const history, operations = 20000, 8
	gw := perfDiagFixture(history)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		gw.mu.Lock()
		gw.audit = gw.audit[:history]
		gw.mu.Unlock()
		start := make(chan struct{})
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < operations; j++ {
				gw.Snapshot()
			}
		}()
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < operations; j++ {
				gw.AppendAudit("synthetic", "perfdiag.contention", "synthetic", "synthetic")
			}
		}()
		b.StartTimer()
		close(start)
		workers.Wait()
	}
	b.StopTimer()
	events := gw.AuditEvents()
	if len(events) != history+operations {
		b.Fatalf("audit count: got %d want %d", len(events), history+operations)
	}
	perfDiagAudit(b, events[:history], history)
	for i, event := range events[history:] {
		if event.Sequence != history+i+1 || !event.At.Equal(perfDiagTime) || event.Actor != "synthetic" || event.Action != "perfdiag.contention" || event.TargetID != "synthetic" || event.Message != "synthetic" {
			b.Fatalf("audit integrity mismatch at index %d", history+i)
		}
	}
	b.ReportMetric(operations, "snapshots/op")
	b.ReportMetric(operations, "appends/op")
}

func BenchmarkPerfHistory(b *testing.B) {
	for _, operation := range []string{"Snapshot", "AuditEvents", "SaveSnapshot", "LoadSnapshot"} {
		b.Run(operation, func(b *testing.B) {
			for _, n := range perfDiagCounts(b) {
				b.Run(strconv.Itoa(n), func(b *testing.B) {
					b.StopTimer()
					gw := perfDiagFixture(n)
					path := filepath.Join(b.TempDir(), "state", "snapshot.json")
					perfDiagRoundTrip(b, gw, path, n)
					var snapshot Snapshot
					var events []model.AuditEvent
					b.ReportAllocs()
					b.ResetTimer()
					b.StartTimer()
					for i := 0; i < b.N; i++ {
						var err error
						switch operation {
						case "Snapshot":
							snapshot = gw.Snapshot()
						case "AuditEvents":
							events = gw.AuditEvents()
						case "SaveSnapshot":
							snapshot, err = gw.SaveSnapshot(path)
						case "LoadSnapshot":
							snapshot, err = gw.LoadSnapshot(path)
						}
						if err != nil {
							b.StopTimer()
							b.Fatal(err)
						}
					}
					b.StopTimer()
					if operation == "AuditEvents" {
						perfDiagAudit(b, events, n)
					} else {
						perfDiagSnapshot(b, snapshot, n)
					}
					perfDiagSnapshot(b, gw.Snapshot(), n)
					perfDiagRoundTrip(b, gw, path, n)
				})
			}
		})
	}
}
