package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func TestFileStateStoreRoundTripSessionSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewMemoryGatewayWithSigningKey(clock, "store-test", publicKey, privateKey)
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "state-store round trip"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStateStore(t.TempDir() + "/gateway.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFrom(gw); err != nil {
		t.Fatal(err)
	}

	restored := NewMemoryGatewayWithSigningKey(clock, "store-test", publicKey, privateKey)
	loaded, found, err := store.LoadInto(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !found || loaded.SchemaVersion != SnapshotSchemaVersion {
		t.Fatalf("loaded snapshot = %#v, found=%t", loaded, found)
	}
	if got, err := restored.Session(session.ID); err != nil || got.ID != session.ID {
		t.Fatalf("restored session = %#v, err=%v", got, err)
	}
}

func TestStateStoreConfigurationGuards(t *testing.T) {
	if _, err := NewFileStateStore(""); err == nil {
		t.Fatal("empty file path was accepted")
	}
}

func TestFileStateStoreRejectsInsecureExistingFile(t *testing.T) {
	path := t.TempDir() + "/gateway.json"
	store, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewMemoryGateway()
	if _, err := store.SaveFrom(gw); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadInto(gw); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("group-readable gateway state was accepted: %v", err)
	}
}
