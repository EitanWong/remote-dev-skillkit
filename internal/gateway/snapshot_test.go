package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
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
