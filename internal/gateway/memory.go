package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrInvalidState        = errors.New("invalid state")
	ErrPolicyDenied        = errors.New("policy denied")
	ErrTicketExpired       = errors.New("ticket expired")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
)

const hostHeartbeatStaleAfter = 90 * time.Second

const TicketMetadataGatewayCandidates = "gateway_url_candidates_json"

type MemoryGateway struct {
	mu                 sync.Mutex
	now                func() time.Time
	auditSink          AuditSink
	sessionStore       *controlplane.MemoryStore
	tickets            map[string]model.Ticket
	codeIndex          map[string]string
	hosts              map[string]model.Host
	hostSecrets        map[string]string // per-host auth tokens, never serialised to API
	hostRegistrationID map[string]HostRegistrationIdempotencyRecord
	hostHeartbeats     map[string]time.Time
	preconnects        map[string]model.SupportSessionPreconnect
	audit              []model.AuditEvent
	signingID          string
	publicKey          ed25519.PublicKey
	privateKey         ed25519.PrivateKey
	manifestSigningID  string
	manifestPublicKey  ed25519.PublicKey
	manifestPrivateKey ed25519.PrivateKey
	enrollmentRoot     model.TrustBundle
	enrollmentPrivate  ed25519.PrivateKey
	enrollmentRevokes  model.HostEnrollmentRevocationList
	requireEnrollment  bool
	issueEnrollment    bool
	checkEnrollmentCRL bool
	trustBundle        model.SignedTrustBundle
}

type EnrollmentCertificateRequest struct {
	TicketCode          string
	Name                string
	OS                  string
	Arch                string
	Capabilities        []string
	IdentityKeyID       string
	IdentityPublicKey   string
	IdentityFingerprint string
	ValidMinutes        int
}

type EnrollmentCertificateRenewalRequest struct {
	Certificate  model.HostEnrollmentCertificate
	ValidMinutes int
}

type HostRegistrationIdempotencyRecord struct {
	Key         string
	RequestHash string
	HostID      string
	HostSecret  string
	CreatedAt   time.Time
}

type AuditSink interface {
	Append(model.AuditEvent) error
}

func NewMemoryGateway() *MemoryGateway {
	return NewMemoryGatewayWithClock(time.Now)
}

func NewMemoryGatewayWithClock(now func() time.Time) *MemoryGateway {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("generate gateway signing key: %v", err))
	}
	return NewMemoryGatewayWithSigningKey(now, "gateway-dev", publicKey, privateKey)
}

func NewMemoryGatewayWithSigningKey(now func() time.Time, signingID string, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) *MemoryGateway {
	if signingID == "" {
		signingID = "gateway-dev"
	}
	validateSigningKey("gateway", publicKey, privateKey)
	trustBundle, err := initialSignedTrustBundle(signingID, publicKey, privateKey, now())
	if err != nil {
		panic(fmt.Sprintf("create initial trust bundle: %v", err))
	}
	return &MemoryGateway{
		now:                now,
		sessionStore:       controlplane.NewMemoryStore(now),
		tickets:            map[string]model.Ticket{},
		codeIndex:          map[string]string{},
		hosts:              map[string]model.Host{},
		hostSecrets:        map[string]string{},
		hostRegistrationID: map[string]HostRegistrationIdempotencyRecord{},
		hostHeartbeats:     map[string]time.Time{},
		preconnects:        map[string]model.SupportSessionPreconnect{},
		signingID:          signingID,
		publicKey:          append(ed25519.PublicKey(nil), publicKey...),
		privateKey:         append(ed25519.PrivateKey(nil), privateKey...),
		manifestSigningID:  signingID,
		manifestPublicKey:  append(ed25519.PublicKey(nil), publicKey...),
		manifestPrivateKey: append(ed25519.PrivateKey(nil), privateKey...),
		trustBundle:        trustBundle,
	}
}

func initialSignedTrustBundle(signingID string, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, now time.Time) (model.SignedTrustBundle, error) {
	bundle, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:     "dev-gateway",
		Sequence:     1,
		NotBefore:    now.UTC(),
		NotAfter:     now.UTC().Add(24 * time.Hour),
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

func (g *MemoryGateway) WithManifestSigningKey(signingID string, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) *MemoryGateway {
	if signingID == "" {
		signingID = "manifest-dev"
	}
	validateSigningKey("manifest", publicKey, privateKey)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.manifestSigningID = signingID
	g.manifestPublicKey = append(ed25519.PublicKey(nil), publicKey...)
	g.manifestPrivateKey = append(ed25519.PrivateKey(nil), privateKey...)
	return g
}

func (g *MemoryGateway) WithEnrollmentRoot(root model.TrustBundle) *MemoryGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.enrollmentRoot = root
	g.requireEnrollment = true
	return g
}

func (g *MemoryGateway) WithEnrollmentIssuer(root model.TrustBundle, privateKey ed25519.PrivateKey) *MemoryGateway {
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		panic(fmt.Sprintf("invalid enrollment root public key: %v", err))
	}
	validateSigningKey("enrollment", publicKey, privateKey)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.enrollmentRoot = root
	g.enrollmentPrivate = append(ed25519.PrivateKey(nil), privateKey...)
	g.requireEnrollment = true
	g.issueEnrollment = true
	return g
}

func (g *MemoryGateway) WithEnrollmentRevocations(list model.HostEnrollmentRevocationList) *MemoryGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.enrollmentRevokes = list
	g.checkEnrollmentCRL = true
	return g
}

func (g *MemoryGateway) EnrollmentRoot() (model.TrustBundle, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.requireEnrollment {
		return model.TrustBundle{}, false
	}
	return g.enrollmentRoot, true
}

func (g *MemoryGateway) EnrollmentRevocations() (model.HostEnrollmentRevocationList, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.checkEnrollmentCRL {
		return model.HostEnrollmentRevocationList{}, false
	}
	list := g.enrollmentRevokes
	list.RevokedCertificates = append([]model.HostEnrollmentCertificateRevocation(nil), g.enrollmentRevokes.RevokedCertificates...)
	return list, true
}

func (g *MemoryGateway) WithAuditSink(sink AuditSink) *MemoryGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.auditSink = sink
	return g
}

func (g *MemoryGateway) CreateTicket(mode model.HostMode, ttlSeconds int, capabilities []string, reason string) (model.Ticket, error) {
	return g.CreateTicketWithMetadata(mode, ttlSeconds, capabilities, reason, nil)
}

func (g *MemoryGateway) CreateTicketWithMetadata(mode model.HostMode, ttlSeconds int, capabilities []string, reason string, metadata map[string]string) (model.Ticket, error) {
	return g.createTicketWithStatus(mode, ttlSeconds, capabilities, reason, metadata, model.TicketStatusActive)
}

func (g *MemoryGateway) CreateProbingTicketWithMetadata(mode model.HostMode, ttlSeconds int, capabilities []string, reason string, metadata map[string]string) (model.Ticket, error) {
	return g.createTicketWithStatus(mode, ttlSeconds, capabilities, reason, metadata, model.TicketStatusProbing)
}

func (g *MemoryGateway) createTicketWithStatus(mode model.HostMode, ttlSeconds int, capabilities []string, reason string, metadata map[string]string, status model.TicketStatus) (model.Ticket, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !mode.Valid() {
		return model.Ticket{}, fmt.Errorf("%w: unsupported host mode %q", ErrPolicyDenied, mode)
	}
	if ttlSeconds < 60 || ttlSeconds > 86400 {
		return model.Ticket{}, fmt.Errorf("%w: ttl must be between 60 and 86400", ErrPolicyDenied)
	}
	if len(capabilities) == 0 {
		capabilities = capabilitiesToStrings(policy.TemporaryDefaults())
	}

	ticket, err := model.NewTicket(mode, ttlSeconds, capabilities, reason, g.now())
	if err != nil {
		return model.Ticket{}, err
	}
	ticket.Status = status
	if len(metadata) > 0 {
		ticket.Metadata = map[string]string{}
		for key, value := range metadata {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key != "" && value != "" {
				ticket.Metadata[key] = value
			}
		}
	}
	g.tickets[ticket.ID] = ticket
	g.codeIndex[ticket.Code] = ticket.ID
	g.appendAuditLocked("operator", "ticket.create", ticket.ID, "created short-lived "+string(status)+" ticket")
	return ticket, nil
}

func (g *MemoryGateway) PublishTicket(ticketID string) (model.Ticket, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ticket, ok := g.tickets[ticketID]
	if !ok {
		return model.Ticket{}, fmt.Errorf("%w: ticket", ErrNotFound)
	}
	if ticket.Status != model.TicketStatusProbing {
		return model.Ticket{}, fmt.Errorf("%w: ticket must be probing", ErrInvalidState)
	}
	if !g.now().Before(ticket.ExpiresAt) {
		return model.Ticket{}, ErrTicketExpired
	}
	ticket.Status = model.TicketStatusActive
	g.tickets[ticket.ID] = ticket
	g.appendAuditLocked("operator", "ticket.publish", ticket.ID, "published probed ticket")
	return ticket, nil
}

func (g *MemoryGateway) ValidatePowerShellBootstrapTicket(ticketCode string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	ticketID, ok := g.codeIndex[strings.TrimSpace(ticketCode)]
	if !ok {
		return fmt.Errorf("%w: ticket code", ErrNotFound)
	}
	ticket := g.tickets[ticketID]
	if ticket.Status != model.TicketStatusProbing && ticket.Status != model.TicketStatusActive {
		return fmt.Errorf("%w: ticket cannot serve bootstrap", ErrInvalidState)
	}
	if !g.now().Before(ticket.ExpiresAt) {
		return ErrTicketExpired
	}
	return nil
}

func (g *MemoryGateway) IssueEnrollmentCertificate(req EnrollmentCertificateRequest) (model.HostEnrollmentCertificate, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.issueEnrollment {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("%w: enrollment issuer not configured", ErrInvalidState)
	}
	ticketID, ok := g.codeIndex[req.TicketCode]
	if !ok {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("%w: ticket code", ErrNotFound)
	}
	ticket := g.tickets[ticketID]
	if ticket.Status != model.TicketStatusActive {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("%w: ticket is not active", ErrInvalidState)
	}
	now := g.now().UTC()
	if !now.Before(ticket.ExpiresAt) {
		return model.HostEnrollmentCertificate{}, ErrTicketExpired
	}
	if req.ValidMinutes <= 0 {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("%w: valid_minutes must be positive", ErrPolicyDenied)
	}
	capabilities := normalizeCapabilities(req.Capabilities)
	if len(capabilities) == 0 {
		capabilities = normalizeCapabilities(ticket.Capabilities)
	}
	if !capabilitiesWithin(capabilities, ticket.Capabilities) {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("%w: enrollment certificate capabilities exceed ticket capabilities", ErrPolicyDenied)
	}
	registration := model.HostRegistration{
		TicketCode:          req.TicketCode,
		Name:                req.Name,
		OS:                  req.OS,
		Arch:                req.Arch,
		Capabilities:        capabilities,
		IdentityKeyID:       req.IdentityKeyID,
		IdentityPublicKey:   req.IdentityPublicKey,
		IdentityFingerprint: req.IdentityFingerprint,
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, g.enrollmentRoot.SigningKeyID, g.enrollmentPrivate, now, time.Duration(req.ValidMinutes)*time.Minute)
	if err != nil {
		return model.HostEnrollmentCertificate{}, err
	}
	g.appendAuditLocked("operator", "enrollment.certificate.issue", ticket.ID, "issued host enrollment certificate")
	return certificate, nil
}

func (g *MemoryGateway) RenewEnrollmentCertificate(req EnrollmentCertificateRenewalRequest) (model.HostEnrollmentCertificate, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.issueEnrollment {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("%w: enrollment issuer not configured", ErrInvalidState)
	}
	if req.ValidMinutes <= 0 {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("%w: valid_minutes must be positive", ErrPolicyDenied)
	}
	now := g.now().UTC()
	if g.checkEnrollmentCRL {
		if err := model.VerifyHostEnrollmentRevocationListSignature(g.enrollmentRevokes, g.enrollmentRoot, now); err != nil {
			return model.HostEnrollmentCertificate{}, err
		}
		if err := model.VerifyHostEnrollmentCertificateNotRevoked(req.Certificate, g.enrollmentRevokes); err != nil {
			return model.HostEnrollmentCertificate{}, err
		}
	}
	renewed, err := model.RenewHostEnrollmentCertificate(req.Certificate, g.enrollmentRoot, g.enrollmentPrivate, now, time.Duration(req.ValidMinutes)*time.Minute)
	if err != nil {
		return model.HostEnrollmentCertificate{}, err
	}
	g.appendAuditLocked("operator", "enrollment.certificate.renew", req.Certificate.TicketCode, "renewed host enrollment certificate")
	return renewed, nil
}

func (g *MemoryGateway) RegisterHost(registration model.HostRegistration) (model.Host, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.registerHostLocked(registration)
}

func (g *MemoryGateway) RegisterHostWithIdempotencyKey(idempotencyKey, requestHash string, registration model.HostRegistration) (model.Host, string, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		host, err := g.RegisterHost(registration)
		if err != nil {
			return model.Host{}, "", err
		}
		secret, err := g.GenerateHostSecret(host.ID)
		if err != nil {
			return model.Host{}, "", err
		}
		return host, secret, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.hostRegistrationID == nil {
		g.hostRegistrationID = map[string]HostRegistrationIdempotencyRecord{}
	}
	if record, ok := g.hostRegistrationID[idempotencyKey]; ok {
		if record.RequestHash != "" && requestHash != "" && record.RequestHash != requestHash {
			return model.Host{}, "", fmt.Errorf("%w: idempotency key %q was used for a different host registration request", ErrIdempotencyConflict, idempotencyKey)
		}
		if host, exists := g.hosts[record.HostID]; exists && record.HostSecret != "" {
			g.hostSecrets[record.HostID] = record.HostSecret
			return host, record.HostSecret, nil
		}
		delete(g.hostRegistrationID, idempotencyKey)
	}

	host, err := g.registerHostLocked(registration)
	if err != nil {
		return model.Host{}, "", err
	}
	secret, err := g.generateHostSecretLocked(host.ID)
	if err != nil {
		return model.Host{}, "", err
	}
	g.hostRegistrationID[idempotencyKey] = HostRegistrationIdempotencyRecord{
		Key:         idempotencyKey,
		RequestHash: requestHash,
		HostID:      host.ID,
		HostSecret:  secret,
		CreatedAt:   g.now().UTC(),
	}
	return host, secret, nil
}

func (g *MemoryGateway) registerHostLocked(registration model.HostRegistration) (model.Host, error) {
	ticketID, ok := g.codeIndex[registration.TicketCode]
	if !ok {
		return model.Host{}, fmt.Errorf("%w: ticket code", ErrNotFound)
	}
	ticket := g.tickets[ticketID]
	if ticket.Status != model.TicketStatusActive {
		return model.Host{}, fmt.Errorf("%w: ticket is not active", ErrInvalidState)
	}
	if !g.now().Before(ticket.ExpiresAt) {
		return model.Host{}, ErrTicketExpired
	}
	if len(registration.Capabilities) == 0 {
		registration.Capabilities = ticket.Capabilities
	}
	if g.requireEnrollment {
		if err := model.VerifyHostEnrollmentCertificate(registration, ticket, g.enrollmentRoot, g.now()); err != nil {
			return model.Host{}, err
		}
		if g.checkEnrollmentCRL {
			if err := model.VerifyHostEnrollmentRevocationListSignature(g.enrollmentRevokes, g.enrollmentRoot, g.now()); err != nil {
				return model.Host{}, err
			}
			if err := model.VerifyHostEnrollmentCertificateNotRevoked(*registration.EnrollmentCertificate, g.enrollmentRevokes); err != nil {
				return model.Host{}, err
			}
		}
	}

	host, err := model.NewHost(ticket, registration, g.now())
	if err != nil {
		return model.Host{}, err
	}
	if ticket.Metadata["auto_activate"] == "attended-temporary" &&
		ticket.Mode == model.HostModeAttendedTemporary &&
		!g.ticketHasActiveRecentHostLocked(ticket.ID, 90*time.Second) {
		now := g.now().UTC()
		g.supersedeMatchingHostsLocked(ticket.ID, registration, now)
		host.Status = model.HostStatusActive
		host.ActivatedAt = &now
		host.LastSeenAt = now
		g.hosts[host.ID] = host
		g.appendAuditLocked("host", "host.register", host.ID, "registered host with attended-temporary auto activation")
		g.appendAuditLocked("operator", "host.auto_activate", host.ID, "auto-activated attended-temporary host from standard connection entry")
		return host, nil
	}
	g.hosts[host.ID] = host
	g.appendAuditLocked("host", "host.register", host.ID, "registered pending host")
	return host, nil
}

func (g *MemoryGateway) ticketHasHostLocked(ticketID string) bool {
	for _, host := range g.hosts {
		if host.TicketID == ticketID {
			return true
		}
	}
	return false
}

// ticketHasActiveRecentHostLocked returns true when there is at least one host
// associated with ticketID whose status is Active AND whose LastSeenAt is within
// the given staleness window.
//
// This is used by the auto-activate gate so that a bootstrap re-registration can
// receive automatic activation once the previous host process has gone silent
// (i.e., it exited, crashed, or lost its network path). The staleness window
// should match the gateway's heartbeat timeout — hosts that stop sending
// heartbeats will have a stale LastSeenAt, signalling that re-activation is safe.
func (g *MemoryGateway) ticketHasActiveRecentHostLocked(ticketID string, window time.Duration) bool {
	cutoff := g.now().Add(-window)
	for _, host := range g.hosts {
		if host.TicketID == ticketID &&
			host.Status == model.HostStatusActive &&
			host.LastSeenAt.After(cutoff) {
			return true
		}
	}
	return false
}

func (g *MemoryGateway) supersedeMatchingHostsLocked(ticketID string, registration model.HostRegistration, now time.Time) {
	for _, host := range g.hosts {
		if host.TicketID != ticketID || host.Status == model.HostStatusRevoked {
			continue
		}
		if !sameHostRegistration(host, registration) {
			continue
		}
		host.Status = model.HostStatusRevoked
		host.LastSeenAt = now
		g.hosts[host.ID] = host
		g.appendAuditLocked("operator", "host.supersede", host.ID, "superseded by newer matching attended-temporary host registration")
	}
}

func sameHostRegistration(host model.Host, registration model.HostRegistration) bool {
	if strings.TrimSpace(host.IdentityFingerprint) != "" &&
		strings.TrimSpace(registration.IdentityFingerprint) != "" {
		return host.IdentityFingerprint == registration.IdentityFingerprint
	}
	return strings.EqualFold(strings.TrimSpace(host.Name), strings.TrimSpace(registration.Name)) &&
		strings.EqualFold(strings.TrimSpace(host.OS), strings.TrimSpace(registration.OS)) &&
		strings.EqualFold(strings.TrimSpace(host.Arch), strings.TrimSpace(registration.Arch))
}

func (g *MemoryGateway) RevokeTicket(ticketID, reason string) (model.Ticket, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ticket, ok := g.tickets[ticketID]
	if !ok {
		return model.Ticket{}, fmt.Errorf("%w: ticket", ErrNotFound)
	}
	if ticket.Status == model.TicketStatusRevoked {
		return model.Ticket{}, fmt.Errorf("%w: ticket already revoked", ErrInvalidState)
	}
	ticket.Status = model.TicketStatusRevoked
	g.tickets[ticket.ID] = ticket
	g.appendAuditLocked("operator", "ticket.revoke", ticket.ID, reasonOrDefault(reason, "revoked ticket"))
	return ticket, nil
}

// RollbackTicket invalidates a ticket transaction and every host authorization
// derived from it. It is intentionally idempotent so callers can install a
// rollback guard immediately after ticket creation and invoke it on every
// publication error without racing a concurrent host registration.
func (g *MemoryGateway) RollbackTicket(ticketID, reason string) (model.Ticket, []model.Host, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ticket, ok := g.tickets[ticketID]
	if !ok {
		return model.Ticket{}, nil, fmt.Errorf("%w: ticket", ErrNotFound)
	}
	if ticket.Status != model.TicketStatusRevoked {
		ticket.Status = model.TicketStatusRevoked
		g.tickets[ticket.ID] = ticket
		g.appendAuditLocked("operator", "ticket.rollback", ticket.ID, reasonOrDefault(reason, "rolled back unpublished ticket"))
	}

	affected := make([]model.Host, 0)
	for hostID, host := range g.hosts {
		if host.TicketID != ticketID {
			continue
		}
		if host.Status != model.HostStatusRevoked {
			host.Status = model.HostStatusRevoked
			host.LastSeenAt = g.now().UTC()
			g.hosts[hostID] = host
			g.appendAuditLocked("operator", "host.rollback", host.ID, "revoked host created by unpublished ticket")
		}
		delete(g.hostSecrets, hostID)
		delete(g.hostHeartbeats, hostID)
		affected = append(affected, host)
	}
	for key, record := range g.hostRegistrationID {
		if host, exists := g.hosts[record.HostID]; exists && host.TicketID == ticketID {
			delete(g.hostRegistrationID, key)
		}
	}
	for key, event := range g.preconnects {
		if event.TicketCode == ticket.Code {
			delete(g.preconnects, key)
		}
	}
	sort.Slice(affected, func(i, j int) bool { return affected[i].ID < affected[j].ID })
	return ticket, affected, nil
}

func (g *MemoryGateway) ActivateHost(hostID string, capabilities []string) (model.Host, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Host{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusPending {
		return model.Host{}, fmt.Errorf("%w: host must be pending", ErrInvalidState)
	}
	if len(capabilities) > 0 {
		host.Capabilities = capabilities
	}
	now := g.now().UTC()
	host.Status = model.HostStatusActive
	host.ActivatedAt = &now
	host.LastSeenAt = now
	g.hosts[host.ID] = host
	g.appendAuditLocked("operator", "host.activate", host.ID, "activated host")
	return host, nil
}

func (g *MemoryGateway) Hosts(status string) []model.Host {
	g.mu.Lock()
	defer g.mu.Unlock()

	hosts := make([]model.Host, 0, len(g.hosts))
	for _, host := range g.hosts {
		host = g.hostWithLivenessLocked(host)
		if status == "" || string(host.Status) == status {
			hosts = append(hosts, host)
		}
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].CreatedAt.Before(hosts[j].CreatedAt)
	})
	return hosts
}

func (g *MemoryGateway) HostsForTicketCode(ticketCode, status string) []model.Host {
	g.mu.Lock()
	defer g.mu.Unlock()

	ticketID := ""
	if strings.TrimSpace(ticketCode) != "" {
		ticketID = g.codeIndex[strings.TrimSpace(ticketCode)]
	}
	hosts := make([]model.Host, 0, len(g.hosts))
	for _, host := range g.hosts {
		if ticketID != "" && host.TicketID != ticketID {
			continue
		}
		host = g.hostWithLivenessLocked(host)
		if status == "" || string(host.Status) == status {
			hosts = append(hosts, host)
		}
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].CreatedAt.Before(hosts[j].CreatedAt)
	})
	return hosts
}

func (g *MemoryGateway) RecordSupportSessionPreconnect(event model.SupportSessionPreconnect) (model.SupportSessionPreconnect, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ticketCode := strings.TrimSpace(event.TicketCode)
	if ticketCode == "" {
		return model.SupportSessionPreconnect{}, fmt.Errorf("%w: ticket code is required", ErrPolicyDenied)
	}
	ticketID, ok := g.codeIndex[ticketCode]
	if !ok {
		return model.SupportSessionPreconnect{}, fmt.Errorf("%w: ticket code", ErrNotFound)
	}
	ticket := g.tickets[ticketID]
	if ticket.Status != model.TicketStatusActive {
		return model.SupportSessionPreconnect{}, fmt.Errorf("%w: ticket is not active", ErrInvalidState)
	}
	now := g.now().UTC()
	if !now.Before(ticket.ExpiresAt) {
		return model.SupportSessionPreconnect{}, ErrTicketExpired
	}
	event.TicketCode = ticketCode
	event.Phase = sanitizePreconnectField(event.Phase, "started", 80)
	event.OS = sanitizePreconnectField(event.OS, "", 40)
	event.Arch = sanitizePreconnectField(event.Arch, "", 40)
	event.Asset = sanitizePreconnectField(event.Asset, "", 160)
	event.Source = sanitizePreconnectField(event.Source, "rdev-bootstrap-preconnect", 80)
	event.Message = sanitizePreconnectField(event.Message, "", 240)
	key := strings.Join([]string{event.TicketCode, event.Phase, event.OS, event.Arch, event.Asset, event.Source}, "\x00")
	if existing, ok := g.preconnects[key]; ok {
		existing.LastSeenAt = now
		existing.SeenCount++
		if event.Message != "" {
			existing.Message = event.Message
		}
		g.preconnects[key] = existing
		g.appendAuditLocked("host", "support_session.preconnect", ticketID, "updated target preconnect status: "+event.Phase)
		return existing, nil
	}
	secret, err := generateSecret()
	if err != nil {
		return model.SupportSessionPreconnect{}, err
	}
	event.ID = "pre_" + secret[:16]
	event.CreatedAt = now
	event.LastSeenAt = now
	event.SeenCount = 1
	g.preconnects[key] = event
	g.appendAuditLocked("host", "support_session.preconnect", ticketID, "recorded target preconnect status: "+event.Phase)
	return event, nil
}

func (g *MemoryGateway) SupportSessionPreconnects(ticketCode string) []model.SupportSessionPreconnect {
	g.mu.Lock()
	defer g.mu.Unlock()

	ticketCode = strings.TrimSpace(ticketCode)
	values := make([]model.SupportSessionPreconnect, 0)
	for _, event := range g.preconnects {
		if ticketCode != "" && event.TicketCode != ticketCode {
			continue
		}
		values = append(values, event)
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func sanitizePreconnectField(value, fallback string, maxLen int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if maxLen > 0 {
		runes := []rune(value)
		if len(runes) > maxLen {
			value = string(runes[:maxLen])
		}
	}
	return value
}

func (g *MemoryGateway) Host(hostID string) (model.Host, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Host{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	return g.hostWithLivenessLocked(host), nil
}

// TicketForCode looks up a ticket by its join code. Returns (ticket, true) when
// found regardless of ticket status, or (zero, false) when the code is unknown.
func (g *MemoryGateway) TicketForCode(ticketCode string) (model.Ticket, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ticketCode = strings.TrimSpace(ticketCode)
	if ticketCode == "" {
		return model.Ticket{}, false
	}
	ticketID, ok := g.codeIndex[ticketCode]
	if !ok {
		return model.Ticket{}, false
	}
	ticket, ok := g.tickets[ticketID]
	return ticket, ok
}

func (g *MemoryGateway) Ticket(ticketID string) (model.Ticket, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ticket, ok := g.tickets[strings.TrimSpace(ticketID)]
	return ticket, ok
}

// GenerateHostSecret creates a random 32-byte hex secret for a host and stores
// it internally. The secret is returned once at registration and must be
// presented by the host process on subsequent host-side requests that still
// require host-local authentication.
func (g *MemoryGateway) GenerateHostSecret(hostID string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.generateHostSecretLocked(hostID)
}

func (g *MemoryGateway) generateHostSecretLocked(hostID string) (string, error) {
	host, ok := g.hosts[hostID]
	if !ok {
		return "", fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status == model.HostStatusRevoked {
		return "", fmt.Errorf("%w: host is revoked", ErrInvalidState)
	}
	secret, err := generateSecret()
	if err != nil {
		return "", err
	}
	g.hostSecrets[hostID] = secret
	return secret, nil
}

// ValidateHostSecret returns true when secret is non-empty and matches the
// stored secret for hostID.
func (g *MemoryGateway) ValidateHostSecret(hostID, secret string) bool {
	if secret == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	stored, ok := g.hostSecrets[hostID]
	return ok && stored == secret
}

// HeartbeatHost records the current time as the host's last heartbeat.
// Returns ErrNotFound when hostID is unknown and ErrPolicyDenied when the
// host is not active.
func (g *MemoryGateway) HeartbeatHost(hostID, secret string) error {
	if !g.ValidateHostSecret(hostID, secret) {
		return fmt.Errorf("%w: invalid host secret", ErrPolicyDenied)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	host, ok := g.hosts[hostID]
	if !ok {
		return fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusActive {
		return fmt.Errorf("%w: host must be active to send heartbeat", ErrInvalidState)
	}
	now := g.now().UTC()
	host.LastSeenAt = now
	g.hosts[hostID] = host
	g.hostHeartbeats[hostID] = now
	return nil
}

// HostIsLive returns true when the host has sent a heartbeat within the last
// maxAge duration.  Used by Hosts() to report stale hosts accurately.
func (g *MemoryGateway) HostIsLive(hostID string, maxAge time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.hostHeartbeats[hostID]
	if !ok {
		host, hostOK := g.hosts[hostID]
		if !hostOK || host.Status != model.HostStatusActive {
			return false
		}
		t = host.LastSeenAt
	}
	return g.now().UTC().Sub(t) <= maxAge
}

func (g *MemoryGateway) hostWithLivenessLocked(host model.Host) model.Host {
	if g.hostIsStaleLocked(host) {
		host.Status = model.HostStatusStale
	}
	return host
}

func (g *MemoryGateway) hostIsStaleLocked(host model.Host) bool {
	if host.Status != model.HostStatusActive {
		return false
	}
	lastSeen := host.LastSeenAt
	if heartbeat, ok := g.hostHeartbeats[host.ID]; ok && heartbeat.After(lastSeen) {
		lastSeen = heartbeat
	}
	if lastSeen.IsZero() {
		return true
	}
	return g.now().UTC().Sub(lastSeen) > hostHeartbeatStaleAfter
}

func (g *MemoryGateway) TrustBundle() model.TrustBundle {
	g.mu.Lock()
	defer g.mu.Unlock()

	return model.NewTrustBundle(g.signingID, g.publicKey)
}

func (g *MemoryGateway) ManifestRoot() model.TrustBundle {
	g.mu.Lock()
	defer g.mu.Unlock()

	return model.NewTrustBundle(g.manifestSigningID, g.manifestPublicKey)
}

func (g *MemoryGateway) SignedTrustBundle() model.SignedTrustBundle {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.trustBundle
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

func (g *MemoryGateway) TrustBundleUpdateForHost(hostID string, currentSequence int, currentHash string) (model.TrustBundleUpdate, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.TrustBundleUpdate{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusActive {
		return model.TrustBundleUpdate{}, fmt.Errorf("%w: host must be active", ErrInvalidState)
	}
	if currentSequence < 0 {
		return model.TrustBundleUpdate{}, fmt.Errorf("%w: current sequence must be non-negative", ErrInvalidState)
	}
	hash, err := g.trustBundle.Hash()
	if err != nil {
		return model.TrustBundleUpdate{}, err
	}
	if currentSequence > g.trustBundle.Sequence {
		return model.TrustBundleUpdate{}, fmt.Errorf("%w: host trust sequence is newer than gateway", ErrInvalidState)
	}
	if currentSequence == g.trustBundle.Sequence {
		if currentHash != "" && currentHash != hash {
			return model.TrustBundleUpdate{}, fmt.Errorf("%w: current trust hash mismatch", ErrInvalidState)
		}
		return model.NewCurrentTrustBundleUpdate(hostID, g.trustBundle, hash), nil
	}
	return model.NewAvailableTrustBundleUpdate(hostID, g.trustBundle, hash), nil
}

func (g *MemoryGateway) JoinManifest(ticketCode, gatewayURL, joinURL string) (model.JoinManifest, error) {
	return g.JoinManifestWithGatewayCandidates(ticketCode, gatewayURL, joinURL, nil)
}

func (g *MemoryGateway) JoinManifestWithGatewayCandidates(ticketCode, gatewayURL, joinURL string, candidates []model.JoinManifestGatewayCandidate) (model.JoinManifest, error) {
	return g.JoinManifestWithPackageCatalog(ticketCode, gatewayURL, joinURL, candidates, model.ConnectionEntryPackageCatalog{})
}

func (g *MemoryGateway) JoinManifestWithPackageCatalog(ticketCode, gatewayURL, joinURL string, candidates []model.JoinManifestGatewayCandidate, catalog model.ConnectionEntryPackageCatalog) (model.JoinManifest, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ticketID, ok := g.codeIndex[ticketCode]
	if !ok {
		return model.JoinManifest{}, fmt.Errorf("%w: ticket code", ErrNotFound)
	}
	ticket := g.tickets[ticketID]
	if ticket.Status != model.TicketStatusActive {
		return model.JoinManifest{}, fmt.Errorf("%w: ticket is not active", ErrInvalidState)
	}
	if !g.now().Before(ticket.ExpiresAt) {
		return model.JoinManifest{}, ErrTicketExpired
	}
	if len(candidates) == 0 {
		candidates = gatewayCandidatesFromTicketMetadata(ticket.Metadata)
	}
	manifest, err := model.NewJoinManifest(ticket, model.JoinManifestSpec{
		GatewayURL:        gatewayURL,
		GatewayCandidates: candidates,
		JoinURL:           joinURL,
		Trust:             model.NewTrustBundle(g.signingID, g.publicKey),
		PackageCatalog:    catalog,
		SigningKeyID:      g.manifestSigningID,
	}, g.now())
	if err != nil {
		return model.JoinManifest{}, err
	}
	return manifest.Sign(g.manifestPrivateKey)
}

func (g *MemoryGateway) JoinManifestTimeProof(manifest model.JoinManifest) (model.GatewayTimeProof, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return model.NewGatewayTimeProof(model.GatewayTimeProofPurposeJoinManifest, manifest, g.manifestSigningID, g.manifestPrivateKey, g.now(), 5*time.Minute)
}

func gatewayCandidatesFromTicketMetadata(metadata map[string]string) []model.JoinManifestGatewayCandidate {
	if len(metadata) == 0 {
		return nil
	}
	raw := strings.TrimSpace(metadata[TicketMetadataGatewayCandidates])
	if raw == "" {
		return nil
	}
	var candidates []model.JoinManifestGatewayCandidate
	if err := json.Unmarshal([]byte(raw), &candidates); err != nil {
		return nil
	}
	out := make([]model.JoinManifestGatewayCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.URL = strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if candidate.URL == "" {
			continue
		}
		out = append(out, candidate)
	}
	return out
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
	if g.auditSink != nil {
		_ = g.auditSink.Append(event)
	}
}

func capabilitiesToStrings(caps []policy.Capability) []string {
	values := make([]string, 0, len(caps))
	for _, cap := range caps {
		values = append(values, string(cap))
	}
	return values
}

func reasonOrDefault(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	return reason
}

// generateSecret returns a cryptographically-random 32-byte hex string.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func normalizeCapabilities(capabilities []string) []string {
	if len(capabilities) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(capabilities))
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		normalized = append(normalized, capability)
	}
	sort.Strings(normalized)
	return normalized
}

func capabilitiesWithin(requested, allowed []string) bool {
	allowedSet := map[string]struct{}{}
	for _, capability := range normalizeCapabilities(allowed) {
		allowedSet[capability] = struct{}{}
	}
	for _, capability := range normalizeCapabilities(requested) {
		if _, ok := allowedSet[capability]; !ok {
			return false
		}
	}
	return true
}
