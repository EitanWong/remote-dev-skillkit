package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestMemoryGatewaySnapshotRoundTripPreservesSignedState(t *testing.T) {
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CompleteJobForHost(host.ID, job.ID, `{"schema_version":"rdev.shell-result.v1"}`); err != nil {
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
	if len(loaded.Tickets) != 1 || len(loaded.Hosts) != 1 || len(loaded.Jobs) != 1 {
		t.Fatalf("unexpected loaded snapshot counts: %#v", loaded)
	}
	loadedJob, err := loadedGateway.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedJob.Status != model.JobStatusSucceeded {
		t.Fatalf("expected succeeded job, got %s", loadedJob.Status)
	}
	if loadedJob.Envelope == nil {
		t.Fatal("expected loaded job envelope")
	}
	if err := loadedJob.Envelope.VerifyForHost(publicKey, host.ID, now); err != nil {
		t.Fatalf("expected loaded envelope to verify: %v", err)
	}
	if artifacts := loadedGateway.Artifacts(job.ID); len(artifacts) != 1 {
		t.Fatalf("expected one artifact after load, got %d", len(artifacts))
	}
	if events := loadedGateway.AuditEvents(); len(events) == 0 || events[len(events)-1].Action != "job.complete" {
		t.Fatalf("expected persisted job.complete audit event, got %#v", events)
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
