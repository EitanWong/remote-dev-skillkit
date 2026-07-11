package gateway

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const SnapshotSchemaVersion = "rdev.gateway-snapshot.v1"

type Snapshot struct {
	SchemaVersion string                  `json:"schema_version"`
	GeneratedAt   time.Time               `json:"generated_at"`
	TrustBundle   model.SignedTrustBundle `json:"trust_bundle"`
	Tickets       []model.Ticket          `json:"tickets"`
	Hosts         []model.Host            `json:"hosts"`
	ControlPlane  controlplane.Snapshot   `json:"control_plane"`
	Audit         []model.AuditEvent      `json:"audit"`
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

	tickets := make(map[string]model.Ticket, len(snapshot.Tickets))
	codeIndex := make(map[string]string, len(snapshot.Tickets))
	for _, ticket := range snapshot.Tickets {
		ticket.Capabilities = append([]string(nil), ticket.Capabilities...)
		ticket.Metadata = cloneStringMap(ticket.Metadata)
		tickets[ticket.ID] = ticket
		codeIndex[ticket.Code] = ticket.ID
	}
	hosts := make(map[string]model.Host, len(snapshot.Hosts))
	for _, host := range snapshot.Hosts {
		hosts[host.ID] = host
	}
	auditEvents := append([]model.AuditEvent(nil), snapshot.Audit...)
	sessionStore := controlplane.NewMemoryStore(g.now)
	if err := sessionStore.RestoreSnapshot(snapshot.ControlPlane); err != nil {
		return err
	}
	if err := reconcileTicketSessionBindings(tickets, sessionStore, g.now()); err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.tickets = tickets
	g.codeIndex = codeIndex
	g.hosts = hosts
	g.sessionStore = sessionStore
	g.audit = auditEvents
	g.trustBundle = snapshot.TrustBundle
	return nil
}

func reconcileTicketSessionBindings(tickets map[string]model.Ticket, sessionStore *controlplane.MemoryStore, now time.Time) error {
	controlSnapshot := sessionStore.Snapshot()
	sessionsByID := make(map[string]controlplane.Session, len(controlSnapshot.Sessions))
	sessionsByCode := make(map[string]controlplane.Session, len(controlSnapshot.Sessions))
	for _, session := range controlSnapshot.Sessions {
		sessionsByID[session.ID] = session
		sessionsByCode[session.JoinCode] = session
		if session.SourceTicketID == "" {
			continue
		}
		ticket, ok := tickets[session.SourceTicketID]
		if !ok || validateSnapshotTicketSession(ticket, session, now) != nil {
			return fmt.Errorf("snapshot contains mismatched ticket/session binding")
		}
	}

	for ticketID, ticket := range tickets {
		if ticket.Status == model.TicketStatusProbing && ticket.SessionID != "" {
			return fmt.Errorf("snapshot probing ticket contains a session binding")
		}
		if ticket.SessionID != "" {
			session, ok := sessionsByID[ticket.SessionID]
			if !ok || validateSnapshotTicketSession(ticket, session, now) != nil {
				return fmt.Errorf("snapshot contains invalid ticket/session binding")
			}
			continue
		}
		if ticket.Status != model.TicketStatusActive {
			continue
		}
		if _, collision := sessionsByCode[ticket.Code]; collision {
			return fmt.Errorf("snapshot ticket join code collides with an existing session")
		}
		session, err := sessionStore.CreateSessionForTicket(ticketSessionSpec(ticket), ticket.ID, ticket.Code)
		if err != nil {
			return fmt.Errorf("migrate legacy ticket session binding: %w", err)
		}
		ticket.SessionID = session.ID
		tickets[ticketID] = ticket
		sessionsByID[session.ID] = session
		sessionsByCode[session.JoinCode] = session
	}
	return nil
}

func validateSnapshotTicketSession(ticket model.Ticket, session controlplane.Session, now time.Time) error {
	if ticket.SessionID != session.ID || ticket.Code != session.JoinCode || session.SourceTicketID != ticket.ID {
		return fmt.Errorf("ticket/session identity mismatch")
	}
	if session.Profile != string(ticket.Mode) || session.Reason != ticket.Reason || session.JoinPolicy != "single-target" || !session.ExpiresAt.Equal(ticket.ExpiresAt) || !slices.Equal(session.Capabilities, ticket.Capabilities) {
		return fmt.Errorf("ticket/session policy mismatch")
	}
	terminal := sessionTerminalStatus(session.Status)
	switch ticket.Status {
	case model.TicketStatusActive:
		if !now.Before(ticket.ExpiresAt) && !terminal {
			return fmt.Errorf("expired active ticket retains a live session")
		}
	case model.TicketStatusRevoked:
		if !terminal {
			return fmt.Errorf("revoked ticket/session lifecycle mismatch")
		}
	case model.TicketStatusProbing:
		return fmt.Errorf("probing ticket cannot own a session")
	default:
		return fmt.Errorf("unknown ticket lifecycle")
	}
	return nil
}

func (g *MemoryGateway) snapshotLocked(now time.Time) Snapshot {
	tickets := make([]model.Ticket, 0, len(g.tickets))
	for _, ticket := range g.tickets {
		ticket.Capabilities = append([]string(nil), ticket.Capabilities...)
		ticket.Metadata = cloneStringMap(ticket.Metadata)
		tickets = append(tickets, ticket)
	}
	sort.Slice(tickets, func(i, j int) bool {
		return tickets[i].CreatedAt.Before(tickets[j].CreatedAt)
	})

	hosts := make([]model.Host, 0, len(g.hosts))
	for _, host := range g.hosts {
		host.Capabilities = append([]string(nil), host.Capabilities...)
		hosts = append(hosts, host)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].CreatedAt.Before(hosts[j].CreatedAt)
	})

	auditEvents := append([]model.AuditEvent(nil), g.audit...)
	sort.Slice(auditEvents, func(i, j int) bool {
		return auditEvents[i].Sequence < auditEvents[j].Sequence
	})
	if g.sessionStore == nil {
		g.sessionStore = controlplane.NewMemoryStore(g.now)
	}

	return Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		GeneratedAt:   now.UTC(),
		TrustBundle:   g.trustBundle,
		Tickets:       tickets,
		Hosts:         hosts,
		ControlPlane:  g.sessionStore.Snapshot(),
		Audit:         auditEvents,
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
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

	ticketIDs := map[string]struct{}{}
	ticketCodes := map[string]struct{}{}
	for _, ticket := range snapshot.Tickets {
		if ticket.ID == "" || ticket.Code == "" {
			return fmt.Errorf("snapshot contains ticket with missing id or code")
		}
		if _, exists := ticketIDs[ticket.ID]; exists {
			return fmt.Errorf("snapshot contains duplicate ticket id %q", ticket.ID)
		}
		if _, exists := ticketCodes[ticket.Code]; exists {
			return fmt.Errorf("snapshot contains duplicate ticket code %q", ticket.Code)
		}
		ticketIDs[ticket.ID] = struct{}{}
		ticketCodes[ticket.Code] = struct{}{}
	}
	hostIDs := map[string]struct{}{}
	for _, host := range snapshot.Hosts {
		if host.ID == "" {
			return fmt.Errorf("snapshot contains host with missing id")
		}
		if _, exists := ticketIDs[host.TicketID]; !exists {
			return fmt.Errorf("snapshot host %q references missing ticket %q", host.ID, host.TicketID)
		}
		if _, exists := hostIDs[host.ID]; exists {
			return fmt.Errorf("snapshot contains duplicate host id %q", host.ID)
		}
		hostIDs[host.ID] = struct{}{}
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
	defer func() {
		_ = os.Remove(tmpPath)
	}()
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
