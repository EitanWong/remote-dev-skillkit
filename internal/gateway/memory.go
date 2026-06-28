package gateway

import (
	"errors"
	"fmt"
	"sort"
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
	mu        sync.Mutex
	now       func() time.Time
	auditSink AuditSink
	tickets   map[string]model.Ticket
	codeIndex map[string]string
	hosts     map[string]model.Host
	jobs      map[string]model.Job
	artifacts map[string][]model.Artifact
	audit     []model.AuditEvent
}

type AuditSink interface {
	Append(model.AuditEvent) error
}

func NewMemoryGateway() *MemoryGateway {
	return NewMemoryGatewayWithClock(time.Now)
}

func NewMemoryGatewayWithClock(now func() time.Time) *MemoryGateway {
	return &MemoryGateway{
		now:       now,
		tickets:   map[string]model.Ticket{},
		codeIndex: map[string]string{},
		hosts:     map[string]model.Host{},
		jobs:      map[string]model.Job{},
		artifacts: map[string][]model.Artifact{},
	}
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
	host.Status = model.HostStatusRevoked
	host.LastSeenAt = g.now().UTC()
	g.hosts[host.ID] = host
	g.appendAuditLocked("operator", "host.revoke", host.ID, reasonOrDefault(reason, "revoked host"))
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
	if adapter == "" || intent == "" {
		return model.Job{}, fmt.Errorf("%w: adapter and intent are required", ErrPolicyDenied)
	}
	job, err := model.NewJob(hostID, adapter, intent, jobPolicy, g.now())
	if err != nil {
		return model.Job{}, err
	}
	g.jobs[job.ID] = job
	g.appendAuditLocked("operator", "job.create", job.ID, "created policy-bound job")
	return job, nil
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
