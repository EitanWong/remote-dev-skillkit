package gateway

import (
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func (g *MemoryGateway) CreateSession(spec controlplane.SessionSpec) (controlplane.Session, error) {
	session, err := g.controlPlane().CreateSession(spec)
	if err == nil {
		g.appendAudit("operator", "session.create", session.ID, "created session")
	}
	return session, err
}

func (g *MemoryGateway) Session(sessionID string) (controlplane.Session, error) {
	return g.controlPlane().Session(sessionID)
}

func (g *MemoryGateway) JoinSessionByCode(joinCode string, spec controlplane.EndpointSpec) (controlplane.Session, controlplane.Endpoint, controlplane.Lease, []controlplane.Event, error) {
	session, endpoint, lease, events, err := g.controlPlane().JoinByCode(joinCode, spec)
	if err == nil {
		g.appendAudit("target", "session.join", endpoint.ID, "endpoint joined session")
	}
	return session, endpoint, lease, events, err
}

func (g *MemoryGateway) JoinSession(sessionID string, spec controlplane.EndpointSpec) (controlplane.Session, controlplane.Endpoint, controlplane.Lease, error) {
	session, endpoint, lease, err := g.controlPlane().JoinSession(sessionID, spec)
	if err == nil {
		g.appendAudit("target", "session.join", endpoint.ID, "endpoint joined session")
	}
	return session, endpoint, lease, err
}

func (g *MemoryGateway) AppendSessionEvent(sessionID string, event controlplane.Event) (controlplane.Event, error) {
	return g.controlPlane().AppendEvent(sessionID, event)
}

func (g *MemoryGateway) SessionEventsAfter(sessionID string, cursor controlplane.EventCursor, limit int) ([]controlplane.Event, controlplane.Lease, controlplane.EventReplayState, error) {
	return g.controlPlane().EventsAfter(sessionID, cursor, limit)
}

func (g *MemoryGateway) PeekSessionEventsAfter(sessionID string, cursor controlplane.EventCursor, limit int) ([]controlplane.Event, controlplane.Lease, controlplane.EventReplayState, error) {
	return g.controlPlane().PeekEventsAfter(sessionID, cursor, limit)
}

func (g *MemoryGateway) SessionEventsAfterForAgent(sessionID string, afterSeq uint64, limit int) ([]controlplane.Event, controlplane.EventReplayState, error) {
	return g.controlPlane().EventsAfterForAgent(sessionID, afterSeq, limit)
}

func (g *MemoryGateway) ValidateSessionLease(sessionID, endpointID, secret string) error {
	return g.controlPlane().ValidateLease(sessionID, endpointID, secret)
}

func (g *MemoryGateway) SubmitSessionTask(sessionID string, spec controlplane.TaskSpec) (controlplane.Task, controlplane.Event, error) {
	task, event, err := g.controlPlane().SubmitTask(sessionID, spec)
	if err == nil {
		g.appendAudit("operator", "session.task.submit", task.ID, "offered task to target endpoint")
	}
	return task, event, err
}

func (g *MemoryGateway) CancelSessionTask(sessionID, taskID, reason, idempotencyKey string) (controlplane.Task, controlplane.Event, error) {
	task, event, err := g.controlPlane().CancelTask(sessionID, taskID, reason, idempotencyKey)
	if err == nil {
		g.appendAudit("operator", "session.task.cancel", task.ID, "canceled task")
	}
	return task, event, err
}

func (g *MemoryGateway) ResumeSessionTask(sessionID, taskID, checkpointID, idempotencyKey string) (controlplane.Task, controlplane.Event, error) {
	task, event, err := g.controlPlane().ResumeTask(sessionID, taskID, checkpointID, idempotencyKey)
	if err == nil {
		g.appendAudit("operator", "session.task.resume", task.ID, "resumed task from checkpoint")
	}
	return task, event, err
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
	session, event, err := g.controlPlane().CloseSession(sessionID)
	if err == nil {
		g.appendAudit("operator", "session.close", session.ID, "closed session")
	}
	return session, event, err
}

func (g *MemoryGateway) RevokeSession(sessionID string) (controlplane.Session, controlplane.Event, error) {
	session, event, err := g.controlPlane().RevokeSession(sessionID)
	if err == nil {
		g.appendAudit("operator", "session.revoke", session.ID, "revoked session and endpoint leases")
	}
	return session, event, err
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
