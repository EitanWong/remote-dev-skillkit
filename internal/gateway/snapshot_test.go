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
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestMemoryGatewaySnapshotRoundTripPreservesSignedHostState(t *testing.T) {
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	host := activeHost(t, gw)
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Profile:      "attended",
		Reason:       "snapshot session",
		Capabilities: []string{"shell.user"},
		JoinPolicy:   "open",
		ExpiresAt:    now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, endpoint, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "snapshot-target",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-snapshot",
		Capabilities:        []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := gw.SubmitSessionTask(session.ID, controlplane.TaskSpec{
		Adapter:          "shell",
		Intent:           "demo",
		TargetEndpointID: endpoint.ID,
		Capabilities:     []string{"shell.user"},
		Payload: map[string]any{
			"workspace_root": ".",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.UpsertSessionArtifact(session.ID, controlplane.ArtifactRef{
		ID:           "artifact-1",
		TaskID:       task.ID,
		Kind:         "result",
		Name:         "result.json",
		SizeBytes:    42,
		SHA256:       "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		ContentType:  "application/json",
		UploadOffset: 42,
		Complete:     true,
	}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "gateway", "state.json")
	snapshot, err := gw.SaveSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		t.Fatalf("unexpected snapshot schema %q", snapshot.SchemaVersion)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 snapshot, got %o", got)
	}

	loadedGateway := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	loaded, err := loadedGateway.LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Tickets) != 1 || len(loaded.Hosts) != 1 {
		t.Fatalf("unexpected loaded snapshot counts: %#v", loaded)
	}
	loadedHost, err := loadedGateway.Host(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedHost.Status != model.HostStatusActive {
		t.Fatalf("expected active host, got %s", loadedHost.Status)
	}
	loadedSession, err := loadedGateway.Session(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSession.Tasks) != 1 || loadedSession.Tasks[0].ID != task.ID {
		t.Fatalf("expected restored session task %s, got %#v", task.ID, loadedSession.Tasks)
	}
	if len(loadedSession.Artifacts) != 1 || loadedSession.Artifacts[0].ID != "artifact-1" {
		t.Fatalf("expected restored session artifact, got %#v", loadedSession.Artifacts)
	}
}

func TestMemoryGatewaySnapshotRejectsSigningKeyMismatch(t *testing.T) {
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	if _, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if _, err := gw.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}

	wrongPublicKey, wrongPrivateKey := gatewaySnapshotKeyPair(t)
	wrongGateway := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", wrongPublicKey, wrongPrivateKey)
	_, err := wrongGateway.LoadSnapshot(path)
	if err == nil || !strings.Contains(err.Error(), "does not match loaded gateway signing key") {
		t.Fatalf("expected signing key mismatch, got %v", err)
	}
}

func gatewaySnapshotKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}
