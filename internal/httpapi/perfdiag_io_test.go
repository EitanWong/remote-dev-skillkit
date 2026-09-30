//go:build perfdiag

package httpapi

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

// Test-only instrumentation delegates to the real FileStateStore. It never
// persists an alternate format or changes locking/durability. /proc/self/io
// samples include concurrent handler work, so physical IO attribution is
// process-level, not an exact per-file device write/fsync claim.
type perfPersistRecord struct {
	SnapshotBytes   int64   `json:"snapshot_bytes"`
	WcharDelta      int64   `json:"wchar_delta"`
	WriteBytesDelta int64   `json:"write_bytes_delta"`
	SaveMS          float64 `json:"save_ms"`
}

type perfMeteredStore struct {
	base    gateway.FileStateStore
	mu      sync.Mutex
	records []perfPersistRecord
}

func (s *perfMeteredStore) LoadInto(gw *gateway.MemoryGateway) (gateway.Snapshot, bool, error) {
	return s.base.LoadInto(gw)
}
func (s *perfMeteredStore) Describe() string { return s.base.Describe() }
func perfSelfIO() (int64, int64) {
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		return -1, -1
	}
	var wchar, written int64
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		switch parts[0] {
		case "wchar:":
			wchar = value
		case "write_bytes:":
			written = value
		}
	}
	return wchar, written
}
func (s *perfMeteredStore) SaveFrom(gw *gateway.MemoryGateway) (gateway.Snapshot, error) {
	beforeWchar, beforeWritten := perfSelfIO()
	start := time.Now()
	result, err := s.base.SaveFrom(gw)
	elapsed := float64(time.Since(start).Nanoseconds()) / 1e6
	afterWchar, afterWritten := perfSelfIO()
	if err != nil {
		return result, err
	}
	info, err := os.Stat(s.base.Path)
	if err != nil {
		return result, err
	}
	r := perfPersistRecord{info.Size(), -1, -1, elapsed}
	if beforeWchar >= 0 && afterWchar >= beforeWchar {
		r.WcharDelta = afterWchar - beforeWchar
	}
	if beforeWritten >= 0 && afterWritten >= beforeWritten {
		r.WriteBytesDelta = afterWritten - beforeWritten
	}
	s.mu.Lock()
	s.records = append(s.records, r)
	s.mu.Unlock()
	return result, nil
}
func (s *perfMeteredStore) emit(t *testing.T, history, clients, expected int, elapsed float64, initial, final int64) {
	t.Helper()
	s.mu.Lock()
	records := append([]perfPersistRecord(nil), s.records...)
	s.mu.Unlock()
	if len(records) != expected {
		t.Fatalf("persistence count: got %d want %d", len(records), expected)
	}
	var emitted, wchar, written int64
	available := true
	for i, r := range records {
		emitted += r.SnapshotBytes
		if r.WcharDelta < 0 || r.WriteBytesDelta < 0 {
			available = false
		} else {
			wchar += r.WcharDelta
			written += r.WriteBytesDelta
		}
		perfHTTPLog(t, "PERF_PERSIST", map[string]any{"history": history, "clients": clients, "persist_index": i, "snapshot_bytes": r.SnapshotBytes, "wchar_delta": r.WcharDelta, "write_bytes_delta": r.WriteBytesDelta, "save_ms": r.SaveMS})
	}
	var ratio any
	if final > initial {
		ratio = float64(emitted) / float64(final-initial)
	}
	perfHTTPLog(t, "PERF_IO_SUMMARY", map[string]any{"history": history, "clients": clients, "persist_calls": len(records), "persist_per_request": float64(len(records)) / float64(expected), "persist_per_second": float64(len(records)) / elapsed, "snapshot_emitted_bytes": emitted, "bytes_per_persist": float64(emitted) / float64(len(records)), "wchar_delta": wchar, "write_bytes_delta": written, "proc_io_available": available, "initial_file_bytes": initial, "final_file_bytes": final, "net_file_growth_bytes": final - initial, "emitted_to_net_file_growth_ratio": ratio, "ratio_denominator": "net snapshot file growth, not logical mutation bytes", "elapsed_seconds": elapsed, "write_bytes_per_second": float64(written) / elapsed})
}
