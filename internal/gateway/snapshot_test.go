package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func TestMemoryGatewaySnapshotRoundTripPreservesSessionState(t *testing.T) {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewMemoryGatewayWithSigningKey(clock, "snapshot-gateway", publicKey, privateKey)
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Profile:      "temporary",
		Reason:       "snapshot regression",
		Capabilities: []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined, endpoint, lease, events, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role:         controlplane.EndpointRoleTarget,
		Name:         "snapshot-target",
		Platform:     "linux/amd64",
		Capabilities: []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if joined.ID != session.ID || endpoint.ID == "" || lease.Secret == "" || len(events) == 0 {
		t.Fatalf("session join = %#v endpoint=%#v lease=%#v events=%#v", joined, endpoint, lease, events)
	}

	snapshot := gw.Snapshot()
	if snapshot.SchemaVersion != SnapshotSchemaVersion || len(snapshot.ControlPlane.Sessions) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	restored := NewMemoryGatewayWithSigningKey(clock, "snapshot-gateway", publicKey, privateKey)
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := restored.Session(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != session.ID || len(got.Endpoints) != 1 || got.Endpoints[0].ID != endpoint.ID {
		t.Fatalf("restored session = %#v", got)
	}
}

func TestFileStateStorePersistsCurrentSessionState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "gateway-state.json")
	store, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if description := store.Describe(); description != FileStateStoreProvider+":"+path {
		t.Fatalf("state store description = %#v", description)
	}
	if _, ok, err := store.LoadInto(NewMemoryGateway()); err != nil || ok {
		t.Fatalf("missing state load = ok:%t err:%v", ok, err)
	}
	if _, err := store.SaveFrom(nil); err == nil {
		t.Fatal("nil gateway save was accepted")
	}
	if _, _, err := store.LoadInto(nil); err == nil {
		t.Fatal("nil gateway load was accepted")
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC) }
	source := NewMemoryGatewayWithSigningKey(clock, "state-store-gateway", publicKey, privateKey)
	session, err := source.CreateSession(controlplane.SessionSpec{Reason: "persist current session"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.SaveFrom(source)
	if err != nil || saved.SchemaVersion != SnapshotSchemaVersion {
		t.Fatalf("SaveFrom() snapshot=%#v err=%v", saved, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions = %#v err=%v", info, err)
	}
	restored := NewMemoryGatewayWithSigningKey(clock, "state-store-gateway", publicKey, privateKey)
	loaded, ok, err := store.LoadInto(restored)
	if err != nil || !ok || loaded.SchemaVersion != SnapshotSchemaVersion {
		t.Fatalf("LoadInto() snapshot=%#v ok=%t err=%v", loaded, ok, err)
	}
	if got, err := restored.Session(session.ID); err != nil || got.ID != session.ID {
		t.Fatalf("restored session = %#v err=%v", got, err)
	}
}

func TestGatewaySigningKeyValidationGuards(t *testing.T) {
	if snapshot := NewMemoryGatewayWithClock(nil).Snapshot(); snapshot.GeneratedAt.IsZero() {
		t.Fatal("nil clock gateway did not produce a snapshot timestamp")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if gateway := NewMemoryGatewayWithSigningKey(nil, "", publicKey, privateKey); gateway.signingID != "gateway-dev" {
		t.Fatalf("default signing ID = %q", gateway.signingID)
	}
	otherPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		publicKey  ed25519.PublicKey
		privateKey ed25519.PrivateKey
	}{
		{name: "short public key", publicKey: publicKey[:1], privateKey: privateKey},
		{name: "short private key", publicKey: publicKey, privateKey: privateKey[:1]},
		{name: "mismatched key pair", publicKey: otherPublicKey, privateKey: privateKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid signing key was accepted")
				}
			}()
			_ = NewMemoryGatewayWithSigningKey(time.Now, "gateway", test.publicKey, test.privateKey)
		})
	}
}

func TestMemoryGatewaySnapshotRejectsLegacySchema(t *testing.T) {
	gw := NewMemoryGateway()
	legacy := gw.Snapshot()
	legacy.SchemaVersion = "rdev.gateway-snapshot.v1"
	if err := gw.RestoreSnapshot(legacy); err == nil || !strings.Contains(err.Error(), "unsupported gateway snapshot schema") {
		t.Fatalf("legacy snapshot error = %v", err)
	}
}

func TestMemoryGatewaySnapshotRejectsSigningKeyMismatch(t *testing.T) {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewMemoryGatewayWithSigningKey(clock, "snapshot-gateway", publicKey, privateKey)
	snapshot := gw.Snapshot()
	otherPublicKey, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	restored := NewMemoryGatewayWithSigningKey(clock, "snapshot-gateway", otherPublicKey, otherPrivateKey)
	if err := restored.RestoreSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), "public key does not match") {
		t.Fatalf("signing mismatch error = %v", err)
	}
}
