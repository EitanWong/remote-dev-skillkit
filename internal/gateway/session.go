package gateway

import (
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func (g *MemoryGateway) CreateSession(spec controlplane.SessionSpec) (controlplane.Session, error) {
	return g.controlPlane().CreateSession(spec)
}

func (g *MemoryGateway) Session(sessionID string) (controlplane.Session, error) {
	return g.controlPlane().Session(sessionID)
}

func (g *MemoryGateway) JoinSessionByCode(joinCode string, spec controlplane.EndpointSpec) (controlplane.Session, controlplane.Endpoint, controlplane.Lease, []controlplane.Event, error) {
	joinCode = strings.TrimSpace(joinCode)
	g.mu.Lock()
	if g.sessionStore == nil {
		g.sessionStore = controlplane.NewMemoryStore(g.now)
	}
	store := g.sessionStore
	ticketID, ticketOwned := g.codeIndex[joinCode]
	if !ticketOwned {
		g.mu.Unlock()
		return store.JoinByCode(joinCode, spec)
	}
	ticket, ok := g.tickets[ticketID]
	if !ok || ticket.Status != model.TicketStatusActive || !g.now().Before(ticket.ExpiresAt) || g.validateTicketSessionBindingLocked(ticket) != nil {
		g.mu.Unlock()
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, invalidTicketJoinCodeError()
	}
	session, endpoint, lease, events, err := store.JoinByCode(joinCode, spec)
	g.mu.Unlock()
	return session, endpoint, lease, events, err
}

func invalidTicketJoinCodeError() controlplane.ProtocolError {
	return controlplane.ProtocolError{
		SchemaVersion:   controlplane.ErrorSchemaVersion,
		Code:            controlplane.ErrInvalidJoinCode,
		Message:         "join code is invalid",
		Recoverable:     false,
		UserSummary:     "The support-session entry is invalid or no longer active.",
		AgentNextAction: "create a fresh support-session entry and use its generated handoff",
	}
}

func (g *MemoryGateway) JoinSession(sessionID string, spec controlplane.EndpointSpec) (controlplane.Session, controlplane.Endpoint, controlplane.Lease, error) {
	return g.controlPlane().JoinSession(sessionID, spec)
}

func (g *MemoryGateway) AppendSessionEvent(sessionID string, event controlplane.Event) (controlplane.Event, error) {
	return g.controlPlane().AppendEvent(sessionID, event)
}

func (g *MemoryGateway) AppendSessionEventBatch(sessionID string, events []controlplane.Event) ([]controlplane.Event, error) {
	return g.controlPlane().AppendEventBatch(sessionID, events)
}

func (g *MemoryGateway) SessionEventsAfter(sessionID string, cursor controlplane.EventCursor, limit int) ([]controlplane.Event, controlplane.Lease, controlplane.EventReplayState, error) {
	return g.controlPlane().EventsAfter(sessionID, cursor, limit)
}

func (g *MemoryGateway) SessionEventsAfterForAgent(sessionID string, afterSeq uint64, limit int) ([]controlplane.Event, controlplane.EventReplayState, error) {
	return g.controlPlane().EventsAfterForAgent(sessionID, afterSeq, limit)
}

func (g *MemoryGateway) ValidateSessionLease(sessionID, endpointID, secret string) error {
	return g.controlPlane().ValidateLease(sessionID, endpointID, secret)
}

func (g *MemoryGateway) SubmitSessionTask(sessionID string, spec controlplane.TaskSpec) (controlplane.Task, controlplane.Event, error) {
	return g.controlPlane().SubmitTask(sessionID, spec)
}

func (g *MemoryGateway) CancelSessionTask(sessionID, taskID, reason, idempotencyKey string) (controlplane.Task, controlplane.Event, error) {
	return g.controlPlane().CancelTask(sessionID, taskID, reason, idempotencyKey)
}

func (g *MemoryGateway) CompleteSessionTask(sessionID, taskID string, result map[string]any) (controlplane.Task, controlplane.Event, error) {
	return g.controlPlane().CompleteTask(sessionID, taskID, result)
}

func (g *MemoryGateway) MarkSessionTaskRunning(sessionID, taskID string) (controlplane.Task, error) {
	return g.controlPlane().MarkTaskRunning(sessionID, taskID)
}

func (g *MemoryGateway) UpsertSessionArtifact(sessionID string, ref controlplane.ArtifactRef) (controlplane.ArtifactRef, controlplane.Event, error) {
	return g.controlPlane().UpsertArtifact(sessionID, ref)
}

func (g *MemoryGateway) CloseSession(sessionID string) (controlplane.Session, controlplane.Event, error) {
	return g.controlPlane().CloseSession(sessionID)
}

func (g *MemoryGateway) CompactSessionEvents(sessionID string, snapshotSeq uint64) error {
	return g.controlPlane().CompactEvents(sessionID, snapshotSeq)
}

func (g *MemoryGateway) controlPlane() *controlplane.MemoryStore {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.now == nil {
		g.now = time.Now
	}
	if g.sessionStore == nil {
		g.sessionStore = controlplane.NewMemoryStore(g.now)
	}
	return g.sessionStore
}
