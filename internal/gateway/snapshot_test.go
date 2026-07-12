package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
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

func TestMemoryGatewaySnapshotMigratesLegacyActiveTicketSessionBinding(t *testing.T) {
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "legacy active ticket")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := gw.Snapshot()
	snapshot.Tickets[0].SessionID = ""
	snapshot.ControlPlane.Sessions = nil
	snapshot.ControlPlane.Events = map[string][]controlplane.Event{}
	snapshot.ControlPlane.Leases = nil
	snapshot.ControlPlane.TerminalAt = nil

	restored := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	migrated, ok := restored.TicketForCode(ticket.Code)
	if !ok || migrated.SessionID == "" {
		t.Fatal("legacy active ticket did not receive a migrated session binding")
	}
	session, err := restored.Session(migrated.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if session.JoinCode != migrated.Code || session.SourceTicketID != migrated.ID {
		t.Fatal("migrated session did not preserve the exact ticket binding")
	}
}

func TestMemoryGatewaySnapshotRoundTripAcceptsExpiredActiveTicketTerminalSession(t *testing.T) {
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 60, []string{"shell"}, "expired ticket snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-expired-ticket-snapshot",
		Capabilities:        []string{"shell"},
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Second)
	snapshot := gw.Snapshot()
	if len(snapshot.ControlPlane.Sessions) != 1 || snapshot.ControlPlane.Sessions[0].Status != controlplane.SessionStatusClosed || len(snapshot.ControlPlane.Leases) != 0 {
		t.Fatalf("expired ticket snapshot retained live session authorization: %#v", snapshot.ControlPlane)
	}

	restored := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	restoredTicket, ok := restored.TicketForCode(ticket.Code)
	if !ok || restoredTicket.Status != model.TicketStatusActive {
		t.Fatalf("expired ticket identity did not round-trip: %#v", restoredTicket)
	}
	restoredSession, err := restored.Session(ticket.SessionID)
	if err != nil || restoredSession.Status != controlplane.SessionStatusClosed {
		t.Fatalf("expired ticket session did not remain terminal: %#v %v", restoredSession, err)
	}
}

func TestMemoryGatewaySnapshotRoundTripAcceptsExplicitlyClosedActiveTicketSession(t *testing.T) {
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell"}, "closed ticket session snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CloseSession(ticket.SessionID); err != nil {
		t.Fatal(err)
	}
	snapshot := gw.Snapshot()
	restored := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	restoredTicket, ok := restored.TicketForCode(ticket.Code)
	if !ok || restoredTicket.Status != model.TicketStatusActive {
		t.Fatalf("explicitly closed ticket identity did not round-trip: %#v", restoredTicket)
	}
	_, _, _, _, err = restored.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
	var protocolErr controlplane.ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != controlplane.ErrInvalidJoinCode || protocolErr.Recoverable {
		t.Fatalf("explicitly closed ticket session became joinable after restore: %v", err)
	}
}

func TestMemoryGatewaySnapshotRejectsCorruptTicketSessionBindings(t *testing.T) {
	for _, corruption := range []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{
			name: "missing bound session",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tickets[0].SessionID = "ses_missing"
			},
		},
		{
			name: "standalone session owns ticket code",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tickets[0].SessionID = ""
				snapshot.ControlPlane.Sessions[0].SourceTicketID = ""
			},
		},
		{
			name: "revoked ticket owns live session",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tickets[0].Status = model.TicketStatusRevoked
			},
		},
		{
			name: "ticket session capability mismatch",
			mutate: func(snapshot *Snapshot) {
				snapshot.ControlPlane.Sessions[0].Capabilities = append(snapshot.ControlPlane.Sessions[0].Capabilities, "desktop.admin")
			},
		},
		{
			name: "ticket session profile mismatch",
			mutate: func(snapshot *Snapshot) {
				snapshot.ControlPlane.Sessions[0].Profile = "managed"
			},
		},
		{
			name: "ticket session expiry mismatch",
			mutate: func(snapshot *Snapshot) {
				snapshot.ControlPlane.Sessions[0].ExpiresAt = snapshot.ControlPlane.Sessions[0].ExpiresAt.Add(time.Minute)
			},
		},
	} {
		t.Run(corruption.name, func(t *testing.T) {
			publicKey, privateKey := gatewaySnapshotKeyPair(t)
			now := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
			gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
			if _, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "corrupt binding"); err != nil {
				t.Fatal(err)
			}
			snapshot := gw.Snapshot()
			corruption.mutate(&snapshot)
			restored := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
			if err := restored.RestoreSnapshot(snapshot); err == nil {
				t.Fatal("corrupt ticket/session binding was accepted")
			}
		})
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
