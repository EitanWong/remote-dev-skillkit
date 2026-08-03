package gateway

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const SnapshotSchemaVersion = "rdev.gateway-snapshot.v2"

type Snapshot struct {
	SchemaVersion string                  `json:"schema_version"`
	GeneratedAt   time.Time               `json:"generated_at"`
	TrustBundle   model.SignedTrustBundle `json:"trust_bundle"`
	ControlPlane  controlplane.Snapshot   `json:"control_plane"`
	Audit         []model.AuditEvent      `json:"audit"`
	// NotifySecrets persists webhook signing secrets (sessionID -> secret)
	// in the 0600 state file so signed deliveries survive gateway restarts.
	NotifySecrets map[string]string `json:"notify_secrets,omitempty"`
}

func (g *MemoryGateway) Snapshot() Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked(g.now())
}

func (g *MemoryGateway) SaveSnapshot(path string) (Snapshot, error) {
	if strings.TrimSpace(path) == "" {
		return Snapshot{}, fmt.Errorf("snapshot path is required")
	}
	snapshot := g.Snapshot()
	content, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return Snapshot{}, err
	}
	content = append(content, '\n')
	if err := writeSnapshotFile(path, content); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (g *MemoryGateway) LoadSnapshot(path string) (Snapshot, error) {
	if strings.TrimSpace(path) == "" {
		return Snapshot{}, fmt.Errorf("snapshot path is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := g.RestoreSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (g *MemoryGateway) LoadSnapshotIfExists(path string) (Snapshot, bool, error) {
	if strings.TrimSpace(path) == "" {
		return Snapshot{}, false, fmt.Errorf("snapshot path is required")
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, false, nil
		}
		return Snapshot{}, false, err
	}
	snapshot, err := g.LoadSnapshot(path)
	return snapshot, true, err
}

func (g *MemoryGateway) RestoreSnapshot(snapshot Snapshot) error {
	if err := g.validateSnapshot(snapshot); err != nil {
		return err
	}

	sessionStore := controlplane.NewMemoryStore(g.now)
	if err := sessionStore.RestoreSnapshot(snapshot.ControlPlane); err != nil {
		return err
	}
	auditEvents := append([]model.AuditEvent(nil), snapshot.Audit...)

	g.mu.Lock()
	defer g.mu.Unlock()
	g.sessionStore = sessionStore
	g.audit = auditEvents
	g.trustBundle = snapshot.TrustBundle
	g.notifySecretsMu.Lock()
	g.notifySecrets = snapshot.NotifySecrets
	g.notifySecretsMu.Unlock()
	return nil
}

func (g *MemoryGateway) snapshotLocked(now time.Time) Snapshot {
	if g.sessionStore == nil {
		g.sessionStore = controlplane.NewMemoryStore(g.now)
	}
	auditEvents := append([]model.AuditEvent(nil), g.audit...)
	sort.Slice(auditEvents, func(i, j int) bool {
		return auditEvents[i].Sequence < auditEvents[j].Sequence
	})
	g.notifySecretsMu.RLock()
	notifySecrets := make(map[string]string, len(g.notifySecrets))
	for sessionID, secret := range g.notifySecrets {
		notifySecrets[sessionID] = secret
	}
	g.notifySecretsMu.RUnlock()
	return Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		GeneratedAt:   now.UTC(),
		TrustBundle:   g.trustBundle,
		ControlPlane:  g.sessionStore.Snapshot(),
		Audit:         auditEvents,
		NotifySecrets: notifySecrets,
	}
}

func (g *MemoryGateway) validateSnapshot(snapshot Snapshot) error {
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		return fmt.Errorf("unsupported gateway snapshot schema %q", snapshot.SchemaVersion)
	}
	root, err := snapshot.TrustBundle.ActiveTrustBundle(g.signingID, g.now())
	if err != nil {
		return fmt.Errorf("snapshot trust bundle does not include active signing key %q: %w", g.signingID, err)
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return err
	}
	if !ed25519.PublicKey(publicKey).Equal(g.publicKey) {
		return fmt.Errorf("snapshot trust bundle public key does not match loaded gateway signing key")
	}
	store := controlplane.NewMemoryStore(g.now)
	if err := store.RestoreSnapshot(snapshot.ControlPlane); err != nil {
		return err
	}
	for index, event := range snapshot.Audit {
		if event.Sequence != index+1 {
			return fmt.Errorf("snapshot audit sequence gap at index %d", index)
		}
	}
	return nil
}

func writeSnapshotFile(path string, content []byte) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".gateway-snapshot-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, abs)
}
