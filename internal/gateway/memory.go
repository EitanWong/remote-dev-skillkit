package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrInvalidState  = errors.New("invalid state")
	ErrPolicyDenied  = errors.New("policy denied")
	ErrTicketExpired = errors.New("ticket expired")
)

type MemoryGateway struct {
	mu                 sync.Mutex
	now                func() time.Time
	auditSink          AuditSink
	tickets            map[string]model.Ticket
	codeIndex          map[string]string
	hosts              map[string]model.Host
	jobs               map[string]model.Job
	artifacts          map[string][]model.Artifact
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
		tickets:            map[string]model.Ticket{},
		codeIndex:          map[string]string{},
		hosts:              map[string]model.Host{},
		jobs:               map[string]model.Job{},
		artifacts:          map[string][]model.Artifact{},
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
	g.tickets[ticket.ID] = ticket
	g.codeIndex[ticket.Code] = ticket.ID
	g.appendAuditLocked("operator", "ticket.create", ticket.ID, "created short-lived ticket")
	return ticket, nil
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

func (g *MemoryGateway) RegisterHost(registration model.HostRegistration) (model.Host, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

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
	g.hosts[host.ID] = host
	g.appendAuditLocked("host", "host.register", host.ID, "registered pending host")
	return host, nil
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

func (g *MemoryGateway) ApproveHost(hostID string, capabilities []string) (model.Host, error) {
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
	host.ApprovedAt = &now
	host.LastSeenAt = now
	g.hosts[host.ID] = host
	g.appendAuditLocked("operator", "host.approve", host.ID, "approved host")
	return host, nil
}

func (g *MemoryGateway) RevokeHost(hostID, reason string) (model.Host, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Host{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status == model.HostStatusRevoked {
		return model.Host{}, fmt.Errorf("%w: host already revoked", ErrInvalidState)
	}
	now := g.now().UTC()
	host.Status = model.HostStatusRevoked
	host.LastSeenAt = now
	g.hosts[host.ID] = host
	g.appendAuditLocked("operator", "host.revoke", host.ID, reasonOrDefault(reason, "revoked host"))
	g.cancelJobsForHostLocked(host.ID, now, "canceled because host was revoked")
	return host, nil
}

func (g *MemoryGateway) Hosts(status string) []model.Host {
	g.mu.Lock()
	defer g.mu.Unlock()

	hosts := make([]model.Host, 0, len(g.hosts))
	for _, host := range g.hosts {
		if status == "" || string(host.Status) == status {
			hosts = append(hosts, host)
		}
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].CreatedAt.Before(hosts[j].CreatedAt)
	})
	return hosts
}

func (g *MemoryGateway) Host(hostID string) (model.Host, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Host{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	return host, nil
}

func (g *MemoryGateway) CreateJob(hostID, adapter, intent string, jobPolicy map[string]any) (model.Job, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Job{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusActive {
		return model.Job{}, fmt.Errorf("%w: host must be active", ErrInvalidState)
	}
	ticket, ok := g.tickets[host.TicketID]
	if !ok {
		return model.Job{}, fmt.Errorf("%w: ticket", ErrNotFound)
	}
	if ticket.Status != model.TicketStatusActive {
		return model.Job{}, fmt.Errorf("%w: ticket is not active", ErrInvalidState)
	}
	if !g.now().Before(ticket.ExpiresAt) {
		return model.Job{}, ErrTicketExpired
	}
	if adapter == "" || intent == "" {
		return model.Job{}, fmt.Errorf("%w: adapter and intent are required", ErrPolicyDenied)
	}
	job, err := model.NewJob(hostID, adapter, intent, jobPolicy, g.now())
	if err != nil {
		return model.Job{}, err
	}
	envelope, err := model.NewJobEnvelope(job, host, ticket, jobEnvelopeSpec(jobPolicy, host, g.signingID), g.now())
	if err != nil {
		return model.Job{}, err
	}
	envelope, err = envelope.Sign(g.privateKey)
	if err != nil {
		return model.Job{}, err
	}
	job.Envelope = &envelope
	g.jobs[job.ID] = job
	g.appendAuditLocked("operator", "job.create", job.ID, "created policy-bound job")
	return job, nil
}

func (g *MemoryGateway) VerifyJobEnvelope(envelope model.JobEnvelope, hostID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	return envelope.VerifyForHost(g.publicKey, hostID, g.now())
}

func (g *MemoryGateway) TrustBundle() model.TrustBundle {
	g.mu.Lock()
	defer g.mu.Unlock()

	return model.NewTrustBundle(g.signingID, g.publicKey)
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
	manifest, err := model.NewJoinManifest(ticket, model.JoinManifestSpec{
		GatewayURL:   gatewayURL,
		JoinURL:      joinURL,
		Trust:        model.NewTrustBundle(g.signingID, g.publicKey),
		SigningKeyID: g.manifestSigningID,
	}, g.now())
	if err != nil {
		return model.JoinManifest{}, err
	}
	return manifest.Sign(g.manifestPrivateKey)
}

func (g *MemoryGateway) CompleteJob(jobID, artifactContent string) (model.Job, model.Artifact, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	job, ok := g.jobs[jobID]
	if !ok {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job", ErrNotFound)
	}
	if job.Status != model.JobStatusQueued && job.Status != model.JobStatusRunning {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job must be queued or running", ErrInvalidState)
	}
	now := g.now().UTC()
	if job.StartedAt == nil {
		job.StartedAt = &now
	}
	job.Status = model.JobStatusSucceeded
	job.EndedAt = &now
	artifact, err := model.NewArtifact(job.ID, "text", "demo-result.txt", artifactContent, now)
	if err != nil {
		return model.Job{}, model.Artifact{}, err
	}
	g.jobs[job.ID] = job
	g.artifacts[job.ID] = append(g.artifacts[job.ID], artifact)
	g.appendAuditLocked("host", "job.complete", job.ID, "completed job and produced artifact")
	return job, artifact, nil
}

func (g *MemoryGateway) CompleteJobForHost(hostID, jobID, artifactContent string) (model.Job, model.Artifact, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusActive {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: host must be active", ErrInvalidState)
	}
	job, ok := g.jobs[jobID]
	if !ok {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job", ErrNotFound)
	}
	if job.HostID != hostID {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job is not assigned to host", ErrPolicyDenied)
	}
	if job.Status != model.JobStatusQueued && job.Status != model.JobStatusRunning {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job must be queued or running", ErrInvalidState)
	}
	now := g.now().UTC()
	if job.StartedAt == nil {
		job.StartedAt = &now
	}
	job.Status = model.JobStatusSucceeded
	job.EndedAt = &now
	artifact, err := model.NewArtifact(job.ID, "text", "demo-result.txt", artifactContent, now)
	if err != nil {
		return model.Job{}, model.Artifact{}, err
	}
	g.jobs[job.ID] = job
	g.artifacts[job.ID] = append(g.artifacts[job.ID], artifact)
	g.appendAuditLocked("host", "job.complete", job.ID, "completed job and produced artifact")
	return job, artifact, nil
}

func (g *MemoryGateway) FailJobForHost(hostID, jobID, reason string) (model.Job, error) {
	job, _, err := g.FailJobForHostWithArtifact(hostID, jobID, reason, "")
	return job, err
}

func (g *MemoryGateway) FailJobForHostWithArtifact(hostID, jobID, reason, artifactContent string) (model.Job, *model.Artifact, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Job{}, nil, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusActive {
		return model.Job{}, nil, fmt.Errorf("%w: host must be active", ErrInvalidState)
	}
	job, ok := g.jobs[jobID]
	if !ok {
		return model.Job{}, nil, fmt.Errorf("%w: job", ErrNotFound)
	}
	if job.HostID != hostID {
		return model.Job{}, nil, fmt.Errorf("%w: job is not assigned to host", ErrPolicyDenied)
	}
	if job.Status != model.JobStatusQueued && job.Status != model.JobStatusRunning {
		return model.Job{}, nil, fmt.Errorf("%w: job must be queued or running", ErrInvalidState)
	}
	now := g.now().UTC()
	if job.StartedAt == nil {
		job.StartedAt = &now
	}
	job.Status = model.JobStatusFailed
	job.FailureReason = reasonOrDefault(reason, "host reported job failure")
	job.EndedAt = &now
	var artifact *model.Artifact
	if artifactContent != "" {
		created, err := model.NewArtifact(job.ID, "text", "failure-result.txt", artifactContent, now)
		if err != nil {
			return model.Job{}, nil, err
		}
		artifact = &created
	}
	g.jobs[job.ID] = job
	if artifact != nil {
		g.artifacts[job.ID] = append(g.artifacts[job.ID], *artifact)
	}
	g.appendAuditLocked("host", "job.fail", job.ID, job.FailureReason)
	return job, artifact, nil
}

func (g *MemoryGateway) AppendCanceledJobArtifactForHost(hostID, jobID, artifactContent string) (model.Job, model.Artifact, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusActive {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: host must be active", ErrInvalidState)
	}
	job, ok := g.jobs[jobID]
	if !ok {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job", ErrNotFound)
	}
	if job.HostID != hostID {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job is not assigned to host", ErrPolicyDenied)
	}
	if job.Status != model.JobStatusCanceled {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job must be canceled", ErrInvalidState)
	}
	if artifactContent == "" {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: artifact_content is required", ErrPolicyDenied)
	}
	now := g.now().UTC()
	artifact, err := model.NewArtifact(job.ID, "text", "canceled-result.txt", artifactContent, now)
	if err != nil {
		return model.Job{}, model.Artifact{}, err
	}
	g.artifacts[job.ID] = append(g.artifacts[job.ID], artifact)
	g.appendAuditLocked("host", "job.artifact", job.ID, "host appended artifact for canceled job")
	return job, artifact, nil
}

func (g *MemoryGateway) AppendJobArtifactForHost(hostID, jobID, artifactContent string) (model.Job, model.Artifact, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusActive {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: host must be active", ErrInvalidState)
	}
	job, ok := g.jobs[jobID]
	if !ok {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job", ErrNotFound)
	}
	if job.HostID != hostID {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job is not assigned to host", ErrPolicyDenied)
	}
	if job.Status == model.JobStatusQueued {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: job must have been claimed before artifact append", ErrInvalidState)
	}
	if artifactContent == "" {
		return model.Job{}, model.Artifact{}, fmt.Errorf("%w: artifact_content is required", ErrPolicyDenied)
	}
	now := g.now().UTC()
	artifact, err := model.NewArtifact(job.ID, "text", appendedArtifactName(artifactContent), artifactContent, now)
	if err != nil {
		return model.Job{}, model.Artifact{}, err
	}
	g.artifacts[job.ID] = append(g.artifacts[job.ID], artifact)
	g.appendAuditLocked("host", "job.artifact", job.ID, "host appended artifact")
	return job, artifact, nil
}

func (g *MemoryGateway) NextJobForHost(hostID string) (model.Job, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	host, ok := g.hosts[hostID]
	if !ok {
		return model.Job{}, false, fmt.Errorf("%w: host", ErrNotFound)
	}
	if host.Status != model.HostStatusActive {
		return model.Job{}, false, fmt.Errorf("%w: host must be active", ErrInvalidState)
	}
	jobs := make([]model.Job, 0, len(g.jobs))
	for _, job := range g.jobs {
		if job.HostID == hostID && job.Status == model.JobStatusQueued {
			jobs = append(jobs, job)
		}
	}
	if len(jobs) == 0 {
		return model.Job{}, false, nil
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	job := jobs[0]
	now := g.now().UTC()
	job.Status = model.JobStatusRunning
	job.StartedAt = &now
	g.jobs[job.ID] = job
	g.appendAuditLocked("host", "job.claim", job.ID, "host claimed queued job")
	return job, true, nil
}

func appendedArtifactName(content string) string {
	if strings.Contains(content, `"schema_version": "rdev.adapter-runtime-fixture.v1"`) || strings.Contains(content, `"schema_version":"rdev.adapter-runtime-fixture.v1"`) {
		return "adapter-runtime-fixture.json"
	}
	return "host-artifact.txt"
}

func (g *MemoryGateway) Job(jobID string) (model.Job, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	job, ok := g.jobs[jobID]
	if !ok {
		return model.Job{}, fmt.Errorf("%w: job", ErrNotFound)
	}
	return job, nil
}

func (g *MemoryGateway) CancelJob(jobID, reason string) (model.Job, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	job, ok := g.jobs[jobID]
	if !ok {
		return model.Job{}, fmt.Errorf("%w: job", ErrNotFound)
	}
	if job.Status == model.JobStatusSucceeded || job.Status == model.JobStatusFailed || job.Status == model.JobStatusCanceled {
		return model.Job{}, fmt.Errorf("%w: job already terminal", ErrInvalidState)
	}
	now := g.now().UTC()
	job.Status = model.JobStatusCanceled
	job.EndedAt = &now
	g.jobs[job.ID] = job
	g.appendAuditLocked("operator", "job.cancel", job.ID, reasonOrDefault(reason, "canceled job"))
	return job, nil
}

func (g *MemoryGateway) cancelJobsForHostLocked(hostID string, now time.Time, reason string) {
	for _, job := range g.jobs {
		if job.HostID != hostID {
			continue
		}
		if job.Status != model.JobStatusQueued && job.Status != model.JobStatusRunning {
			continue
		}
		job.Status = model.JobStatusCanceled
		job.EndedAt = &now
		g.jobs[job.ID] = job
		g.appendAuditLocked("operator", "job.cancel", job.ID, reasonOrDefault(reason, "canceled job"))
	}
}

func (g *MemoryGateway) ApproveJob(jobID, approvalID, decision, reason string) (model.Job, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	job, ok := g.jobs[jobID]
	if !ok {
		return model.Job{}, fmt.Errorf("%w: job", ErrNotFound)
	}
	if decision != "approved" && decision != "denied" {
		return model.Job{}, fmt.Errorf("%w: decision must be approved or denied", ErrPolicyDenied)
	}
	if decision == "approved" {
		if strings.TrimSpace(approvalID) == "" {
			return model.Job{}, fmt.Errorf("%w: approval id is required", ErrPolicyDenied)
		}
		if job.Envelope == nil {
			return model.Job{}, fmt.Errorf("%w: job envelope is required", ErrInvalidState)
		}
		envelope := *job.Envelope
		token, err := model.NewApprovalToken(model.ApprovalTokenSpec{
			JobID:        envelope.JobID,
			HostID:       envelope.HostID,
			ApprovalID:   approvalID,
			Operation:    approvalID,
			OperatorID:   envelope.OperatorID,
			Source:       "operator",
			ExpiresAt:    envelope.ExpiresAt,
			SigningKeyID: envelope.SigningKeyID,
		}, g.now())
		if err != nil {
			return model.Job{}, err
		}
		token, err = token.Sign(g.privateKey)
		if err != nil {
			return model.Job{}, err
		}
		envelope.ApprovalTokens = appendApprovalToken(envelope.ApprovalTokens, token)
		envelope.Signature = ""
		signed, err := envelope.Sign(g.privateKey)
		if err != nil {
			return model.Job{}, err
		}
		job.Envelope = &signed
		g.jobs[job.ID] = job
	}
	g.appendAuditLocked("operator", "job.approve", job.ID, fmt.Sprintf("%s approval %s: %s", decision, approvalID, reason))
	return job, nil
}

func (g *MemoryGateway) Artifacts(jobID string) []model.Artifact {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]model.Artifact(nil), g.artifacts[jobID]...)
}

func (g *MemoryGateway) Artifact(artifactID string) (model.Artifact, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, artifacts := range g.artifacts {
		for _, artifact := range artifacts {
			if artifact.ID == artifactID {
				return artifact, nil
			}
		}
	}
	return model.Artifact{}, fmt.Errorf("%w: artifact", ErrNotFound)
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

func jobEnvelopeSpec(jobPolicy map[string]any, host model.Host, signingID string) model.JobEnvelopeSpec {
	return model.JobEnvelopeSpec{
		OperatorID:              stringValue(jobPolicy, "operator_id", "operator"),
		HostIdentityFingerprint: host.IdentityFingerprint,
		Workspace:               workspaceValue(jobPolicy),
		Capabilities:            stringSliceValue(jobPolicy, "capabilities", host.Capabilities),
		Limits:                  limitsValue(jobPolicy),
		ApprovalsRequired:       stringSliceValue(jobPolicy, "approvals_required", nil),
		Payload:                 jobPolicy,
		TTLSeconds:              intValue(jobPolicy, "ttl_seconds", model.DefaultJobTTLSeconds),
		SigningKeyID:            signingID,
	}
}

func workspaceValue(values map[string]any) model.JobWorkspace {
	root := stringValue(values, "workspace_root", "")
	if root == "" {
		root = stringValue(values, "cwd", "")
	}
	return model.JobWorkspace{
		Root:       root,
		WriteScope: stringSliceValue(values, "write_scope", nil),
		Branch:     stringValue(values, "branch", ""),
	}
}

func limitsValue(values map[string]any) model.JobLimits {
	return model.JobLimits{
		MaxDurationSeconds: intValue(values, "max_duration_seconds", model.DefaultJobTTLSeconds),
		MaxOutputBytes:     intValue(values, "max_output_bytes", model.DefaultMaxOutputBytes),
		Network:            stringValue(values, "network", "default-deny"),
	}
}

func stringValue(values map[string]any, key, fallback string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

func intValue(values map[string]any, key string, fallback int) int {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func stringSliceValue(values map[string]any, key string, fallback []string) []string {
	value, ok := values[key]
	if !ok || value == nil {
		return append([]string(nil), fallback...)
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return append([]string(nil), fallback...)
	}
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

func appendApprovalToken(values []model.ApprovalToken, next model.ApprovalToken) []model.ApprovalToken {
	result := append([]model.ApprovalToken(nil), values...)
	for index, value := range result {
		if value.ApprovalID == next.ApprovalID && value.Operation == next.Operation {
			result[index] = next
			return result
		}
	}
	return append(result, next)
}
