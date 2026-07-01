package gateway

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestFileStateStoreRoundTrip(t *testing.T) {
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "store round trip")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStateStore(filepath.Join(t.TempDir(), "gateway", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.SaveFrom(gw)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tickets) != 1 || snapshot.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected saved snapshot: %#v", snapshot.Tickets)
	}

	restarted := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	loaded, ok, err := store.LoadInto(restarted)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected state store load")
	}
	if len(loaded.Tickets) != 1 || loaded.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected loaded snapshot: %#v", loaded.Tickets)
	}
}

func TestFileStateStoreRequiresPath(t *testing.T) {
	if _, err := NewFileStateStore(""); err == nil {
		t.Fatal("expected empty path to fail")
	}
}
