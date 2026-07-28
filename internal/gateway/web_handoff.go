package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

const (
	WebHandoffSchemaVersion        = "rdev.web-handoff.v1"
	WebHandoffPlatformWindowsAMD64 = "windows-amd64"
	webHandoffIdentifierByteLength = 16
	webHandoffSecretByteLength     = 32
	webHandoffMaxArtifactTicketTTL = 30 * time.Minute
)

var (
	ErrWebHandoffNotFound       = errors.New("web handoff not found")
	ErrWebHandoffExpired        = errors.New("web handoff expired")
	ErrWebHandoffClaimed        = errors.New("web handoff already claimed")
	ErrWebHandoffInvalidProof   = errors.New("web handoff proof is invalid")
	ErrWebHandoffInvalidTicket  = errors.New("web handoff artifact ticket is invalid")
	ErrWebHandoffSessionInvalid = errors.New("web handoff session is not joinable")
)

// WebHandoffSpec creates a short-lived, single-claim browser handoff for a
// session. The returned proof is an opaque capability and is never persisted.
type WebHandoffSpec struct {
	SessionID string
	Platform  string
	ExpiresAt time.Time
}

// WebHandoff intentionally contains no raw proof or artifact ticket. Those
// values are only returned once to the caller that creates or claims a handoff.
type WebHandoff struct {
	SchemaVersion           string    `json:"schema_version"`
	ID                      string    `json:"id"`
	SessionID               string    `json:"session_id"`
	Platform                string    `json:"platform"`
	CreatedAt               time.Time `json:"created_at"`
	ExpiresAt               time.Time `json:"expires_at"`
	ClaimedAt               time.Time `json:"claimed_at,omitempty"`
	ArtifactTicketExpiresAt time.Time `json:"artifact_ticket_expires_at,omitempty"`
}

type webHandoffState struct {
	WebHandoff
	proofHash          [sha256.Size]byte
	artifactTicketHash [sha256.Size]byte
}

// CreateWebHandoff returns a public handoff record and a high-entropy proof.
// The caller must place the proof only in a browser URL fragment so it is not
// sent to the gateway in an HTTP URL, referrer, or query string.
func (g *MemoryGateway) CreateWebHandoff(spec WebHandoffSpec) (WebHandoff, string, error) {
	spec.SessionID = strings.TrimSpace(spec.SessionID)
	spec.Platform = strings.TrimSpace(spec.Platform)
	if spec.SessionID == "" || spec.Platform != WebHandoffPlatformWindowsAMD64 || spec.ExpiresAt.IsZero() {
		return WebHandoff{}, "", fmt.Errorf("invalid web handoff specification")
	}

	now := g.webHandoffNow()
	if !spec.ExpiresAt.After(now) {
		return WebHandoff{}, "", ErrWebHandoffExpired
	}
	session, err := g.Session(spec.SessionID)
	if err != nil {
		return WebHandoff{}, "", err
	}
	if !webHandoffSessionJoinable(session, now) {
		return WebHandoff{}, "", ErrWebHandoffSessionInvalid
	}

	proof, err := newWebHandoffSecret()
	if err != nil {
		return WebHandoff{}, "", err
	}
	proofHash := sha256.Sum256([]byte(proof))
	state := webHandoffState{
		WebHandoff: WebHandoff{
			SchemaVersion: WebHandoffSchemaVersion,
			SessionID:     spec.SessionID,
			Platform:      spec.Platform,
			CreatedAt:     now,
			ExpiresAt:     spec.ExpiresAt.UTC(),
		},
		proofHash: proofHash,
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.webHandoffs == nil {
		g.webHandoffs = map[string]webHandoffState{}
	}
	for {
		id, err := newWebHandoffIdentifier()
		if err != nil {
			return WebHandoff{}, "", err
		}
		if _, exists := g.webHandoffs[id]; exists {
			continue
		}
		state.ID = id
		g.webHandoffs[id] = state
		g.appendAuditLocked("operator", "web_handoff.create", id, "created a short-lived browser host handoff")
		return state.WebHandoff, proof, nil
	}
}

// ClaimWebHandoff consumes a browser proof and returns a fresh, header-only
// artifact ticket. The ticket lifetime never extends beyond the handoff TTL.
func (g *MemoryGateway) ClaimWebHandoff(id, proof string, artifactTicketTTL time.Duration) (WebHandoff, string, error) {
	id = strings.TrimSpace(id)
	proof = strings.TrimSpace(proof)
	if id == "" || proof == "" || artifactTicketTTL <= 0 {
		return WebHandoff{}, "", ErrWebHandoffInvalidProof
	}
	if artifactTicketTTL > webHandoffMaxArtifactTicketTTL {
		artifactTicketTTL = webHandoffMaxArtifactTicketTTL
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	state, exists := g.webHandoffs[id]
	if !exists {
		return WebHandoff{}, "", ErrWebHandoffNotFound
	}
	now := g.webHandoffNowLocked()
	if !state.ExpiresAt.After(now) {
		return WebHandoff{}, "", ErrWebHandoffExpired
	}
	if !state.ClaimedAt.IsZero() {
		return WebHandoff{}, "", ErrWebHandoffClaimed
	}
	proofHash := sha256.Sum256([]byte(proof))
	if subtle.ConstantTimeCompare(state.proofHash[:], proofHash[:]) != 1 {
		return WebHandoff{}, "", ErrWebHandoffInvalidProof
	}

	ticket, err := newWebHandoffSecret()
	if err != nil {
		return WebHandoff{}, "", err
	}
	ticketExpiresAt := now.Add(artifactTicketTTL)
	if ticketExpiresAt.After(state.ExpiresAt) {
		ticketExpiresAt = state.ExpiresAt
	}
	state.ClaimedAt = now
	state.ArtifactTicketExpiresAt = ticketExpiresAt
	state.artifactTicketHash = sha256.Sum256([]byte(ticket))
	g.webHandoffs[id] = state
	g.appendAuditLocked("handoff", "web_handoff.claim", id, "claimed browser host handoff and issued artifact ticket")
	return state.WebHandoff, ticket, nil
}

// ValidateWebHandoffArtifactTicket accepts only a current ticket delivered in a
// request header after the one-time browser claim.
func (g *MemoryGateway) ValidateWebHandoffArtifactTicket(id, ticket string) (WebHandoff, error) {
	id = strings.TrimSpace(id)
	ticket = strings.TrimSpace(ticket)
	if id == "" || ticket == "" {
		return WebHandoff{}, ErrWebHandoffInvalidTicket
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	state, exists := g.webHandoffs[id]
	if !exists {
		return WebHandoff{}, ErrWebHandoffNotFound
	}
	now := g.webHandoffNowLocked()
	if !state.ExpiresAt.After(now) || state.ArtifactTicketExpiresAt.IsZero() || !state.ArtifactTicketExpiresAt.After(now) {
		return WebHandoff{}, ErrWebHandoffExpired
	}
	ticketHash := sha256.Sum256([]byte(ticket))
	if subtle.ConstantTimeCompare(state.artifactTicketHash[:], ticketHash[:]) != 1 {
		return WebHandoff{}, ErrWebHandoffInvalidTicket
	}
	return state.WebHandoff, nil
}

func webHandoffSessionJoinable(session controlplane.Session, now time.Time) bool {
	if session.Status == controlplane.SessionStatusClosed || session.Status == controlplane.SessionStatusRevoked || session.Status == controlplane.SessionStatusFailed {
		return false
	}
	if !session.ExpiresAt.IsZero() && !session.ExpiresAt.After(now) {
		return false
	}
	return session.JoinPolicy != "single-target" || len(session.Endpoints) == 0
}

func (g *MemoryGateway) webHandoffNow() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.webHandoffNowLocked()
}

// Now returns the gateway clock for HTTP handlers that create or validate
// short-lived gateway capabilities.
func (g *MemoryGateway) Now() time.Time {
	return g.webHandoffNow()
}

func (g *MemoryGateway) webHandoffNowLocked() time.Time {
	if g.now == nil {
		g.now = time.Now
	}
	return g.now().UTC()
}

func newWebHandoffIdentifier() (string, error) {
	value := make([]byte, webHandoffIdentifierByteLength)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate web handoff identifier: %w", err)
	}
	return "hnd_" + base64.RawURLEncoding.EncodeToString(value), nil
}

func newWebHandoffSecret() (string, error) {
	value := make([]byte, webHandoffSecretByteLength)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate web handoff secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
