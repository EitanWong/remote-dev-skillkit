//go:build perfdiag

package gateway

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

// Event-only negative control: audit remains exactly the create/join pair;
// one active endpoint/session is held constant while cold event history grows.
func perfEventFixture(tb testing.TB, n int) (*MemoryGateway, string, int) {
	tb.Helper()
	gw := NewMemoryGatewayWithClock(func() time.Time { return perfDiagTime })
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "synthetic event-only perfdiag"})
	if err != nil {
		tb.Fatal("event fixture session setup failed")
	}
	_, endpoint, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget, Name: "synthetic", Platform: "linux/amd64"})
	if err != nil {
		tb.Fatal("event fixture endpoint setup failed")
	}
	initial := gw.Snapshot()
	base := len(initial.ControlPlane.Events[session.ID])
	for i := 0; i < n; i++ {
		_, err := gw.AppendSessionEvent(session.ID, controlplane.Event{Type: controlplane.EventTypeStatus, FromEndpointID: endpoint.ID, IdempotencyKey: fmt.Sprintf("synthetic-%d", i), Payload: map[string]any{"state": "synthetic", "sample": fmt.Sprintf("seed-20260930-%08d", i)}})
		if err != nil {
			tb.Fatal("event fixture append failed")
		}
	}
	if len(gw.Snapshot().Audit) != 2 {
		tb.Fatal("event-only control unexpectedly grew audit")
	}
	return gw, session.ID, n + base
}

func perfEventVerify(tb testing.TB, snapshot Snapshot, session string, count int) {
	tb.Helper()
	if len(snapshot.Audit) != 2 || len(snapshot.ControlPlane.Sessions) != 1 || len(snapshot.ControlPlane.Events[session]) != count {
		tb.Fatal("event-only control lost audit/session/events")
	}
	for i, e := range snapshot.ControlPlane.Events[session] {
		if e.Seq != uint64(i+1) || e.SessionID != session {
			tb.Fatal("event-only sequence mismatch")
		}
	}
}

func BenchmarkPerfEventOnlySave(b *testing.B) {
	for _, n := range perfDiagCounts(b) {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			b.StopTimer()
			gw, session, count := perfEventFixture(b, n)
			store := FileStateStore{Path: filepath.Join(b.TempDir(), "state", "snapshot.json")}
			var saved Snapshot
			b.ReportAllocs()
			b.ResetTimer()
			b.StartTimer()
			for i := 0; i < b.N; i++ {
				var err error
				saved, err = store.SaveFrom(gw)
				if err != nil {
					b.Fatal("event-only snapshot save failed")
				}
			}
			b.StopTimer()
			perfEventVerify(b, saved, session, count)
			loaded, ok, err := store.LoadInto(gw)
			if err != nil || !ok {
				b.Fatal("event-only restore failed")
			}
			perfEventVerify(b, loaded, session, count)
			perfDiagPermissions(b, store.Path)
		})
	}
}
