package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrInvalidState        = errors.New("invalid state")
	ErrPolicyDenied        = errors.New("policy denied")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
)

const (
	signedTrustBundleLifetime    = 24 * time.Hour
	signedTrustBundleRenewBefore = time.Hour
)

// MemoryGateway is the in-memory session Control Plane state with its gateway
// trust bundle and audit stream. Host access is represented exclusively by
// Control Plane sessions and endpoints.
type MemoryGateway struct {
	mu           sync.Mutex
	now          func() time.Time
	sessionStore *controlplane.MemoryStore
	audit        []model.AuditEvent
	webHandoffs  map[string]webHandoffState
	signingID    string
	publicKey    ed25519.PublicKey
	privateKey   ed25519.PrivateKey
	trustBundle  model.SignedTrustBundle
	// notifySecrets: webhook signing secrets by session ID, kept off the
	// control-plane session so serialization can never leak them. Locked
	// separately so the store event hook (store lock held) can read it.
	notifySecretsMu sync.RWMutex
	notifySecrets   map[string]string
}

func NewMemoryGateway() *MemoryGateway {
	return NewMemoryGatewayWithClock(time.Now)
}

func NewMemoryGatewayWithClock(now func() time.Time) *MemoryGateway {
	if now == nil {
		now = time.Now
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("generate gateway signing key: %v", err))
	}
	return NewMemoryGatewayWithSigningKey(now, "gateway-dev", publicKey, privateKey)
}

func NewMemoryGatewayWithSigningKey(now func() time.Time, signingID string, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) *MemoryGateway {
	if now == nil {
		now = time.Now
	}
	if signingID == "" {
		signingID = "gateway-dev"
	}
	validateSigningKey("gateway", publicKey, privateKey)
	trustBundle, err := initialSignedTrustBundle(signingID, publicKey, privateKey, now())
	if err != nil {
		panic(fmt.Sprintf("create initial trust bundle: %v", err))
	}
	g := &MemoryGateway{
		now:          now,
		sessionStore: controlplane.NewMemoryStore(now),
		webHandoffs:  map[string]webHandoffState{},
		signingID:    signingID,
		publicKey:    append(ed25519.PublicKey(nil), publicKey...),
		privateKey:   append(ed25519.PrivateKey(nil), privateKey...),
		trustBundle:  trustBundle,
	}
	g.sessionStore.SetEventHook(g.notifyEvent)
	return g
}

func initialSignedTrustBundle(signingID string, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, now time.Time) (model.SignedTrustBundle, error) {
	bundle, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:     "dev-gateway",
		Sequence:     1,
		NotBefore:    now.UTC(),
		NotAfter:     now.UTC().Add(signedTrustBundleLifetime),
		SigningKeyID: signingID,
		Keys: []model.TrustKey{
			model.NewTrustKey(signingID, publicKey, model.TrustKeyStatusActive, now.UTC()),
		},
	}, now.UTC())
	if err != nil {
		return model.SignedTrustBundle{}, err
	}
	return bundle.Sign(privateKey)
}

func validateSigningKey(label string, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) {
	if len(publicKey) != ed25519.PublicKeySize {
		panic(fmt.Sprintf("invalid %s signing public key length %d", label, len(publicKey)))
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		panic(fmt.Sprintf("invalid %s signing private key length %d", label, len(privateKey)))
	}
	derived, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok || !derived.Equal(publicKey) {
		panic(fmt.Sprintf("%s signing public key does not match private key", label))
	}
}

func (g *MemoryGateway) SignedTrustBundle() model.SignedTrustBundle {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.trustBundle
}

// RenewSignedTrustBundle refreshes the gateway-signed bundle before it expires
// and retains the previous bundle hash so managed hosts can verify the update.
func (g *MemoryGateway) RenewSignedTrustBundle() (model.SignedTrustBundle, bool, error) {
	if g == nil {
		return model.SignedTrustBundle{}, false, ErrInvalidState
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now().UTC()
	if now.Add(signedTrustBundleRenewBefore).Before(g.trustBundle.NotAfter.UTC()) {
		return g.trustBundle, false, nil
	}
	if _, err := g.trustBundle.ActiveTrustBundle(g.signingID, now); err != nil {
		return model.SignedTrustBundle{}, false, fmt.Errorf("renew trust bundle signing key: %w", err)
	}
	previousHash, err := g.trustBundle.Hash()
	if err != nil {
		return model.SignedTrustBundle{}, false, fmt.Errorf("hash previous trust bundle: %w", err)
	}
	next, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           g.trustBundle.BundleID,
		Sequence:           g.trustBundle.Sequence + 1,
		NotBefore:          now,
		NotAfter:           now.Add(signedTrustBundleLifetime),
		PreviousBundleHash: previousHash,
		SigningKeyID:       g.signingID,
		Keys: []model.TrustKey{
			model.NewTrustKey(g.signingID, g.publicKey, model.TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		return model.SignedTrustBundle{}, false, fmt.Errorf("build renewed trust bundle: %w", err)
	}
	next, err = next.Sign(g.privateKey)
	if err != nil {
		return model.SignedTrustBundle{}, false, fmt.Errorf("sign renewed trust bundle: %w", err)
	}
	g.trustBundle = next
	g.appendAuditLocked("gateway", "trust_bundle.renew", next.BundleID, fmt.Sprintf("renewed trust bundle to sequence %d", next.Sequence))
	return g.trustBundle, true, nil
}

func (g *MemoryGateway) UpdateSignedTrustBundle(next model.SignedTrustBundle) (model.SignedTrustBundle, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	root, err := g.trustBundle.ActiveTrustBundle(next.SigningKeyID, g.now())
	if err != nil {
		return model.SignedTrustBundle{}, err
	}
	if err := next.VerifyUpdate(g.trustBundle, root, g.now()); err != nil {
		return model.SignedTrustBundle{}, err
	}
	g.trustBundle = next
	g.appendAuditLocked("operator", "trust_bundle.update", next.BundleID, fmt.Sprintf("updated trust bundle to sequence %d", next.Sequence))
	return g.trustBundle, nil
}

func (g *MemoryGateway) AuditEvents() []model.AuditEvent {
	g.mu.Lock()
	defer g.mu.Unlock()

	events := append([]model.AuditEvent(nil), g.audit...)
	sort.Slice(events, func(i, j int) bool {
		return events[i].Sequence < events[j].Sequence
	})
	return events
}

func (g *MemoryGateway) appendAuditLocked(actor, action, targetID, message string) {
	event := model.AuditEvent{
		Sequence: len(g.audit) + 1,
		Actor:    actor,
		Action:   action,
		TargetID: targetID,
		Message:  message,
		At:       g.now().UTC(),
	}
	g.audit = append(g.audit, event)
}

func (g *MemoryGateway) appendAudit(actor, action, targetID, message string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.appendAuditLocked(actor, action, targetID, message)
}
