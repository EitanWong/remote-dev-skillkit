package controlplane

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type EndpointSpec struct {
	EndpointID          string       `json:"endpoint_id"`
	Role                EndpointRole `json:"role"`
	Name                string       `json:"name"`
	Platform            string       `json:"platform"`
	IdentityFingerprint string       `json:"identity_fingerprint"`
	Capabilities        []string     `json:"capabilities"`
	Transport           Transport    `json:"transport"`
	LeaseTTLMS          int          `json:"lease_ttl_ms"`
	RenewAfterMS        int          `json:"renew_after_ms"`
	RetryAfterMS        int          `json:"retry_after_ms"`
	PreviousLeaseSecret string       `json:"previous_lease_secret"`
}

type EventCursor struct {
	EndpointID    string        `json:"endpoint_id"`
	LeaseSecret   string        `json:"lease_secret"`
	AfterSeq      uint64        `json:"after_seq"`
	ReceivedSeq   uint64        `json:"received_seq"`
	ProcessedSeq  uint64        `json:"processed_seq"`
	VisibleRole   EndpointRole  `json:"visible_role"`
	EndpointState EndpointState `json:"endpoint_state"`
}

type EventReplayState struct {
	SnapshotRequired bool   `json:"snapshot_required"`
	SnapshotSeq      uint64 `json:"snapshot_seq"`
	LastSeq          uint64 `json:"last_seq"`
	RetryAfterMS     int    `json:"retry_after_ms"`
	Reconnecting     bool   `json:"reconnecting"`
}

type TaskSpec struct {
	TargetEndpointID string         `json:"target_endpoint_id"`
	TargetSelector   TargetSelector `json:"target_selector"`
	Adapter          string         `json:"adapter"`
	Intent           string         `json:"intent"`
	Capabilities     []string       `json:"capabilities"`
	Payload          map[string]any `json:"payload"`
	Limits           map[string]any `json:"limits"`
	AttemptID        string         `json:"attempt_id"`
	IdempotencyKey   string         `json:"idempotency_key"`
}

type MemoryStore struct {
	mu                sync.Mutex
	clock             func() time.Time
	sessions          map[string]Session
	joinCodes         map[string]string
	events            map[string][]Event
	idempotency       map[idempotencyKey]idempotencyRecord
	taskIdempotency   map[string]taskRecord
	cancelIdempotency map[string]taskRecord
	resultIdempotency map[string]taskRecord
	leases            map[string]leaseRecord
	terminalAt        map[string]time.Time
}

type idempotencyKey struct {
	SessionID      string
	FromEndpointID string
	Key            string
}

type idempotencyRecord struct {
	Fingerprint string
	Event       Event
}

type taskRecord struct {
	TaskID  string
	EventID string
}

type leaseRecord struct {
	Current         Lease
	PreviousSecrets map[string]time.Time
}

func NewMemoryStore(clock func() time.Time) *MemoryStore {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryStore{
		clock:             clock,
		sessions:          map[string]Session{},
		joinCodes:         map[string]string{},
		events:            map[string][]Event{},
		idempotency:       map[idempotencyKey]idempotencyRecord{},
		taskIdempotency:   map[string]taskRecord{},
		cancelIdempotency: map[string]taskRecord{},
		resultIdempotency: map[string]taskRecord{},
		leases:            map[string]leaseRecord{},
		terminalAt:        map[string]time.Time{},
	}
}

func (s *MemoryStore) CreateSession(spec SessionSpec) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, err := NewSession(spec, s.now())
	if err != nil {
		return Session{}, err
	}
	if _, exists := s.joinCodes[session.JoinCode]; exists {
		return Session{}, s.err(ErrInvalidJoinCode, "join code already exists", false)
	}
	s.sessions[session.ID] = session
	s.joinCodes[session.JoinCode] = session.ID
	s.events[session.ID] = []Event{}
	return session, nil
}

func (s *MemoryStore) Session(sessionID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return Session{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	return session.clone(), nil
}

func (s *MemoryStore) JoinByCode(joinCode string, spec EndpointSpec) (Session, Endpoint, Lease, []Event, error) {
	s.mu.Lock()
	sessionID, ok := s.joinCodes[joinCode]
	s.mu.Unlock()
	if !ok {
		return Session{}, Endpoint{}, Lease{}, nil, s.err(ErrInvalidJoinCode, "join code is invalid", false)
	}

	session, endpoint, lease, err := s.JoinSession(sessionID, spec)
	if err != nil {
		return Session{}, Endpoint{}, Lease{}, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.visibleEventsLocked(sessionID, endpoint, 0, 0)
	session = s.sessions[sessionID].clone()
	return session, endpoint, lease, events, nil
}

func (s *MemoryStore) JoinSession(sessionID string, spec EndpointSpec) (Session, Endpoint, Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return Session{}, Endpoint{}, Lease{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	if sessionTerminal(session.Status) {
		return Session{}, Endpoint{}, Lease{}, s.err(ErrTerminalSession, "session is terminal", false)
	}

	endpoint, created, err := s.joinEndpointLocked(session, spec)
	if err != nil {
		return Session{}, Endpoint{}, Lease{}, err
	}
	session = s.sessions[sessionID]
	lease := s.issueLeaseLocked(session, endpoint, spec.LeaseTTLMS, spec.RenewAfterMS, spec.RetryAfterMS)
	if created {
		hello := Event{
			Type:           EventTypeHello,
			FromEndpointID: "gateway",
			ToEndpointID:   endpoint.ID,
			IdempotencyKey: "join-hello:" + endpoint.ID,
			Payload: map[string]any{
				"role":     string(endpoint.Role),
				"endpoint": endpoint.ID,
			},
		}
		if _, err := s.appendEventLocked(sessionID, hello, true); err != nil {
			return Session{}, Endpoint{}, Lease{}, err
		}
		session = s.sessions[sessionID]
	}
	return session.clone(), endpoint, lease, nil
}

func (s *MemoryStore) AppendEvent(sessionID string, event Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEventLocked(sessionID, event, true)
}

func (s *MemoryStore) AppendEventBatch(sessionID string, batch []Event) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	if len(batch) > session.Limits.EventBatch {
		return nil, s.err(ErrTooManyEvents, "event batch is too large", true)
	}

	appended := make([]Event, 0, len(batch))
	for _, event := range batch {
		next, err := s.appendEventLocked(sessionID, event, true)
		if err != nil {
			return nil, err
		}
		appended = append(appended, next)
	}
	return appended, nil
}

func (s *MemoryStore) EventsAfter(sessionID string, cursor EventCursor, limit int) ([]Event, Lease, EventReplayState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, Lease{}, EventReplayState{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	replay := EventReplayState{
		SnapshotSeq:  session.SnapshotSeq,
		LastSeq:      session.LastSeq,
		RetryAfterMS: session.RetryAfterMS,
	}
	if cursor.AfterSeq < session.SnapshotSeq {
		replay.SnapshotRequired = true
		return nil, Lease{}, replay, s.err(ErrSnapshotRequired, "event cursor is older than compacted snapshot", true)
	}

	endpointIndex := s.endpointIndexLocked(session, cursor.EndpointID)
	if endpointIndex < 0 {
		return nil, Lease{}, replay, s.err(ErrEndpointNotFound, "endpoint not found", true)
	}
	endpoint := session.Endpoints[endpointIndex]
	reconnecting, err := s.authorizeLeaseLocked(session, endpoint.ID, cursor.LeaseSecret)
	if err != nil {
		return nil, Lease{}, replay, err
	}
	replay.Reconnecting = reconnecting

	if cursor.ReceivedSeq > endpoint.ReceivedSeq {
		endpoint.ReceivedSeq = cursor.ReceivedSeq
	}
	if cursor.ProcessedSeq > endpoint.ProcessedSeq {
		endpoint.ProcessedSeq = cursor.ProcessedSeq
	}
	if cursor.EndpointState != "" {
		endpoint.State = cursor.EndpointState
	}
	if reconnecting {
		endpoint.State = EndpointStateOnline
		session.Status = SessionStatusRecovered
	}
	endpoint.LastSeenAt = s.now()
	session = session.WithEndpoint(endpoint, s.now())
	s.sessions[sessionID] = session

	lease := s.issueLeaseLocked(session, endpoint, 0, 0, 0)
	if limit <= 0 || limit > session.Limits.EventBatch {
		limit = session.Limits.EventBatch
	}
	events := s.visibleEventsLocked(sessionID, endpoint, cursor.AfterSeq, limit)
	replay.LastSeq = s.sessions[sessionID].LastSeq
	return events, lease, replay, nil
}

func (s *MemoryStore) EventsAfterForAgent(sessionID string, afterSeq uint64, limit int) ([]Event, EventReplayState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, EventReplayState{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	replay := EventReplayState{
		SnapshotSeq:  session.SnapshotSeq,
		LastSeq:      session.LastSeq,
		RetryAfterMS: session.RetryAfterMS,
	}
	if afterSeq < session.SnapshotSeq {
		replay.SnapshotRequired = true
		return nil, replay, s.err(ErrSnapshotRequired, "event cursor is older than compacted snapshot", true)
	}
	if limit <= 0 || limit > session.Limits.EventBatch {
		limit = session.Limits.EventBatch
	}
	agent := Endpoint{Role: EndpointRoleAgent}
	return s.visibleEventsLocked(sessionID, agent, afterSeq, limit), replay, nil
}

func (s *MemoryStore) ValidateLease(sessionID, endpointID, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return s.err(ErrInvalidJoinCode, "session not found", false)
	}
	if s.endpointIndexLocked(session, endpointID) < 0 {
		return s.err(ErrEndpointNotFound, "endpoint not found", true)
	}
	_, err := s.authorizeLeaseLocked(session, endpointID, secret)
	return err
}

func (s *MemoryStore) SubmitTask(sessionID string, spec TaskSpec) (Task, Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return Task{}, Event{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	if sessionTerminal(session.Status) {
		return Task{}, Event{}, s.err(ErrTerminalSession, "session is terminal", false)
	}
	idempotencyKey := "task:" + spec.IdempotencyKey
	if record, ok := s.taskIdempotency[sessionID+":"+idempotencyKey]; ok {
		return s.taskAndEventLocked(sessionID, record)
	}

	target, err := s.routeTaskLocked(session, spec)
	if err != nil {
		return Task{}, Event{}, err
	}
	taskID, err := newID("task")
	if err != nil {
		return Task{}, Event{}, err
	}
	attemptID := spec.AttemptID
	if attemptID == "" {
		attemptID, err = newID("attempt")
		if err != nil {
			return Task{}, Event{}, err
		}
	}
	task := Task{
		SchemaVersion:    TaskSchemaVersion,
		ID:               taskID,
		SessionID:        sessionID,
		TargetEndpointID: target.ID,
		TargetSelector:   spec.TargetSelector,
		Adapter:          spec.Adapter,
		Intent:           spec.Intent,
		Capabilities:     append([]string(nil), spec.Capabilities...),
		Payload:          cloneMap(spec.Payload),
		Limits:           cloneMap(spec.Limits),
		AttemptID:        attemptID,
		IdempotencyKey:   spec.IdempotencyKey,
		Status:           TaskStatusOffered,
		CreatedAt:        s.now(),
	}
	session.Tasks = append(session.Tasks, task)
	s.sessions[sessionID] = session

	event, err := s.appendEventLocked(sessionID, Event{
		Type:           EventTypeTask,
		FromEndpointID: "agent",
		ToEndpointID:   target.ID,
		TaskID:         task.ID,
		IdempotencyKey: idempotencyKey,
		Payload: map[string]any{
			"action":     "offer",
			"adapter":    task.Adapter,
			"attempt_id": task.AttemptID,
		},
	}, true)
	if err != nil {
		return Task{}, Event{}, err
	}
	s.taskIdempotency[sessionID+":"+idempotencyKey] = taskRecord{TaskID: task.ID, EventID: event.ID}
	return task, event, nil
}

func (s *MemoryStore) CancelTask(sessionID, taskID, reason, idempotencyKey string) (Task, Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionID + ":cancel:" + taskID + ":" + idempotencyKey
	if record, ok := s.cancelIdempotency[key]; ok {
		return s.taskAndEventLocked(sessionID, record)
	}
	task, index, session, err := s.findTaskLocked(sessionID, taskID)
	if err != nil {
		return Task{}, Event{}, err
	}
	if task.Terminal() {
		return Task{}, Event{}, s.err(ErrTaskAlreadyTerminal, "task is already terminal", false)
	}
	canceled, err := task.Transition(TaskStatusCanceled, s.now())
	if err != nil {
		return Task{}, Event{}, err
	}
	session.Tasks[index] = canceled
	s.sessions[sessionID] = session
	event, err := s.appendEventLocked(sessionID, Event{
		Type:           EventTypeTask,
		FromEndpointID: "agent",
		ToEndpointID:   canceled.TargetEndpointID,
		TaskID:         canceled.ID,
		IdempotencyKey: "cancel:" + taskID + ":" + idempotencyKey,
		Payload:        map[string]any{"action": "cancel", "reason": reason},
	}, true)
	if err != nil {
		return Task{}, Event{}, err
	}
	s.cancelIdempotency[key] = taskRecord{TaskID: canceled.ID, EventID: event.ID}
	return canceled, event, nil
}

func (s *MemoryStore) CompleteTask(sessionID, taskID string, result map[string]any) (Task, Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, index, session, err := s.findTaskLocked(sessionID, taskID)
	if err != nil {
		return Task{}, Event{}, err
	}
	attemptID := stringMapValue(result, "attempt_id")
	if attemptID == "" {
		attemptID = task.AttemptID
	}
	idempotencyKey := stringMapValue(result, "idempotency_key")
	resultKey := sessionID + ":result:" + taskID + ":" + attemptID + ":" + idempotencyKey
	if record, ok := s.resultIdempotency[resultKey]; ok {
		return s.taskAndEventLocked(sessionID, record)
	}
	if sessionTerminal(session.Status) && !s.inTerminalGrace(session) {
		return Task{}, Event{}, s.err(ErrTerminalSession, "terminal session result grace elapsed", false)
	}
	if task.Terminal() {
		return Task{}, Event{}, s.err(ErrTaskAlreadyTerminal, "task is already terminal", false)
	}
	if attemptID != task.AttemptID {
		return Task{}, Event{}, s.err(ErrTaskNotFound, "task attempt does not match", false)
	}
	if task.Status == TaskStatusOffered {
		task, err = task.Transition(TaskStatusRunning, s.now())
		if err != nil {
			return Task{}, Event{}, err
		}
	}

	nextStatus := TaskStatusSucceeded
	switch stringMapValue(result, "status") {
	case string(TaskStatusFailed):
		nextStatus = TaskStatusFailed
	case string(TaskStatusCanceled):
		nextStatus = TaskStatusCanceled
	}
	completed, err := task.Transition(nextStatus, s.now())
	if err != nil {
		return Task{}, Event{}, err
	}
	session.Tasks[index] = completed
	s.sessions[sessionID] = session
	event, err := s.appendEventLocked(sessionID, Event{
		Type:           EventTypeTaskResult,
		FromEndpointID: completed.TargetEndpointID,
		TaskID:         completed.ID,
		IdempotencyKey: "result:" + taskID + ":" + attemptID + ":" + idempotencyKey,
		Payload:        cloneMap(result),
	}, true)
	if err != nil {
		return Task{}, Event{}, err
	}
	s.resultIdempotency[resultKey] = taskRecord{TaskID: completed.ID, EventID: event.ID}
	return completed, event, nil
}

func (s *MemoryStore) UpsertArtifact(sessionID string, ref ArtifactRef) (ArtifactRef, Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return ArtifactRef{}, Event{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	if ref.ID == "" {
		id, err := newID("art")
		if err != nil {
			return ArtifactRef{}, Event{}, err
		}
		ref.ID = id
	}
	if ref.Complete && len(ref.SHA256) != 64 {
		return ArtifactRef{}, Event{}, s.err(ErrChecksumMismatch, "complete artifact requires sha256", true)
	}
	ref.SchemaVersion = ArtifactRefSchemaVersion
	ref.SessionID = sessionID

	replaced := false
	for i, existing := range session.Artifacts {
		if existing.ID != ref.ID {
			continue
		}
		if ref.UploadOffset < existing.UploadOffset {
			return ArtifactRef{}, Event{}, s.err(ErrArtifactOffsetMismatch, "artifact upload offset moved backwards", true)
		}
		session.Artifacts[i] = ref
		replaced = true
		break
	}
	if !replaced {
		session.Artifacts = append(session.Artifacts, ref)
	}
	s.sessions[sessionID] = session
	event, err := s.appendEventLocked(sessionID, Event{
		Type:           EventTypeArtifact,
		FromEndpointID: "artifact",
		TaskID:         ref.TaskID,
		IdempotencyKey: fmt.Sprintf("artifact:%s:%d:%t", ref.ID, ref.UploadOffset, ref.Complete),
		Payload: map[string]any{
			"id":            ref.ID,
			"task_id":       ref.TaskID,
			"upload_offset": ref.UploadOffset,
			"complete":      ref.Complete,
			"sha256":        ref.SHA256,
		},
	}, true)
	if err != nil {
		return ArtifactRef{}, Event{}, err
	}
	return ref, event, nil
}

func (s *MemoryStore) MarkTaskRunning(sessionID, taskID string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, index, session, err := s.findTaskLocked(sessionID, taskID)
	if err != nil {
		return Task{}, err
	}
	running, err := task.Transition(TaskStatusRunning, s.now())
	if err != nil {
		return Task{}, err
	}
	session.Tasks[index] = running
	s.sessions[sessionID] = session
	return running, nil
}

func (s *MemoryStore) CloseSession(sessionID string) (Session, Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return Session{}, Event{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	session.Status = SessionStatusClosed
	session.UpdatedAt = s.now()
	s.sessions[sessionID] = session
	s.terminalAt[sessionID] = s.now()
	event, err := s.appendEventLocked(sessionID, Event{
		Type:           EventTypeClose,
		FromEndpointID: "gateway",
		IdempotencyKey: "close:" + sessionID,
	}, true)
	if err != nil {
		return Session{}, Event{}, err
	}
	return s.sessions[sessionID].clone(), event, nil
}

func (s *MemoryStore) CompactEvents(sessionID string, snapshotSeq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return s.err(ErrInvalidJoinCode, "session not found", false)
	}
	session.SnapshotSeq = snapshotSeq
	retained := s.events[sessionID][:0]
	for _, event := range s.events[sessionID] {
		if event.Seq >= snapshotSeq {
			retained = append(retained, event)
		}
	}
	s.events[sessionID] = retained
	s.sessions[sessionID] = session
	return nil
}

func (s *MemoryStore) joinEndpointLocked(session Session, spec EndpointSpec) (Endpoint, bool, error) {
	role := spec.Role
	if role == "" {
		role = EndpointRoleTarget
	}
	transport := spec.Transport
	if transport == "" {
		transport = TransportPoll
	}
	state := EndpointStateOnline
	endpointKey := endpointIdentityKey(session.ID, role, spec.IdentityFingerprint, spec.Platform)

	for _, endpoint := range session.Endpoints {
		if endpointIdentityKey(session.ID, endpoint.Role, endpoint.IdentityFingerprint, endpoint.Platform) == endpointKey && spec.IdentityFingerprint != "" {
			endpoint.Name = nonEmpty(spec.Name, endpoint.Name)
			endpoint.Capabilities = append([]string(nil), spec.Capabilities...)
			if len(endpoint.Capabilities) == 0 {
				endpoint.Capabilities = append([]string(nil), endpoint.Capabilities...)
			}
			endpoint.Transport = transport
			endpoint.State = state
			endpoint.LastSeenAt = s.now()
			session.Status = SessionStatusOnline
			session = session.WithEndpoint(endpoint, s.now())
			s.sessions[session.ID] = session
			return endpoint, false, nil
		}
	}

	if session.JoinPolicy == "single-target" && role == EndpointRoleTarget {
		for _, endpoint := range session.Endpoints {
			if endpoint.Role == EndpointRoleTarget {
				return Endpoint{}, false, s.err(ErrJoinPolicyRejected, "single-target session already has a different target", false)
			}
		}
	}

	endpointID := spec.EndpointID
	if endpointID == "" {
		var err error
		endpointID, err = newID("end")
		if err != nil {
			return Endpoint{}, false, err
		}
	}
	endpoint := Endpoint{
		SchemaVersion:       EndpointSchemaVersion,
		ID:                  endpointID,
		SessionID:           session.ID,
		Role:                role,
		Name:                spec.Name,
		Platform:            spec.Platform,
		IdentityFingerprint: spec.IdentityFingerprint,
		Capabilities:        append([]string(nil), spec.Capabilities...),
		State:               state,
		Transport:           transport,
		LastSeenAt:          s.now(),
	}
	session.Status = SessionStatusOnline
	session = session.WithEndpoint(endpoint, s.now())
	s.sessions[session.ID] = session
	return endpoint, true, nil
}

func (s *MemoryStore) appendEventLocked(sessionID string, event Event, enforceLimits bool) (Event, error) {
	session, ok := s.sessions[sessionID]
	if !ok {
		return Event{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	if sessionTerminal(session.Status) && event.Type != EventTypeTaskResult && event.Type != EventTypeArtifact && event.Type != EventTypeClose {
		return Event{}, s.err(ErrTerminalSession, "session is terminal", false)
	}
	if enforceLimits {
		payloadSize, err := jsonPayloadSize(event.Payload)
		if err != nil {
			return Event{}, err
		}
		if payloadSize > session.Limits.EventPayloadBytes {
			return Event{}, s.err(ErrPayloadTooLarge, "event payload is too large", true)
		}
	}

	event.SchemaVersion = EventSchemaVersion
	event.SessionID = sessionID
	event.Payload = cloneMap(event.Payload)
	if event.Type == EventTypeGateway {
		if selected := selectedGatewayURLFromEvent(event); selected != "" {
			session.SelectedGatewayURL = selected
			s.sessions[sessionID] = session
		}
	}
	if event.IdempotencyKey != "" {
		key := idempotencyKey{SessionID: sessionID, FromEndpointID: event.FromEndpointID, Key: event.IdempotencyKey}
		fingerprint, err := eventFingerprint(event)
		if err != nil {
			return Event{}, err
		}
		if record, ok := s.idempotency[key]; ok {
			if record.Fingerprint != fingerprint {
				return Event{}, s.err(ErrIdempotencyConflict, "idempotency key was reused with a different payload", true)
			}
			return record.Event, nil
		}
		defer func() {
			s.idempotency[key] = idempotencyRecord{Fingerprint: fingerprint, Event: event}
		}()
	}
	if event.ID == "" {
		id, err := newID("evt")
		if err != nil {
			return Event{}, err
		}
		event.ID = id
	}
	event.Seq = session.LastSeq + 1
	event.CreatedAt = s.now()
	s.events[sessionID] = append(s.events[sessionID], event)
	session = session.WithEvent(event, s.now())
	s.sessions[sessionID] = session
	return event, nil
}

func selectedGatewayURLFromEvent(event Event) string {
	for _, key := range []string{"selected_url", "next_url", "gateway_url"} {
		value := strings.TrimRight(strings.TrimSpace(stringPayload(event.Payload, key)), "/")
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *MemoryStore) issueLeaseLocked(session Session, endpoint Endpoint, ttlMS, renewAfterMS, retryAfterMS int) Lease {
	record := s.leases[endpoint.ID]
	if record.PreviousSecrets == nil {
		record.PreviousSecrets = map[string]time.Time{}
	}
	if record.Current.Secret != "" {
		record.PreviousSecrets[record.Current.Secret] = record.Current.ExpiresAt.Add(time.Duration(session.ReconnectGraceMS) * time.Millisecond)
	}
	secret, _ := newID("lease_secret")
	lease := NewLease(LeaseSpec{
		ID:                 "",
		SessionID:          session.ID,
		EndpointID:         endpoint.ID,
		Generation:         record.Current.Generation + 1,
		Secret:             secret,
		Transport:          endpoint.Transport,
		SelectedGatewayURL: session.SelectedGatewayURL,
		LeaseTTLMS:         ttlMS,
		RenewAfterMS:       renewAfterMS,
		RetryAfterMS:       retryAfterMSOrSession(retryAfterMS, session.RetryAfterMS),
	}, s.now())
	record.Current = lease
	s.leases[endpoint.ID] = record
	return lease
}

func (s *MemoryStore) authorizeLeaseLocked(session Session, endpointID, secret string) (bool, error) {
	record, ok := s.leases[endpointID]
	if !ok || secret == "" {
		return false, s.err(ErrUnauthorizedEndpoint, "endpoint lease is missing", true)
	}
	now := s.now()
	if record.Current.Secret == secret {
		if !now.After(record.Current.ExpiresAt) {
			return false, nil
		}
		if !now.After(record.Current.ExpiresAt.Add(time.Duration(session.ReconnectGraceMS) * time.Millisecond)) {
			return true, nil
		}
		return false, s.err(ErrLeaseExpired, "lease expired outside reconnect grace", true)
	}
	if graceUntil, ok := record.PreviousSecrets[secret]; ok && !now.After(graceUntil) {
		return true, nil
	}
	return false, s.err(ErrUnauthorizedEndpoint, "lease secret does not match endpoint", true)
}

func (s *MemoryStore) visibleEventsLocked(sessionID string, endpoint Endpoint, afterSeq uint64, limit int) []Event {
	events := []Event{}
	for _, event := range s.events[sessionID] {
		if event.Seq <= afterSeq || !eventVisibleToEndpoint(event, endpoint) {
			continue
		}
		events = append(events, cloneEvent(event))
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	return events
}

func (s *MemoryStore) routeTaskLocked(session Session, spec TaskSpec) (Endpoint, error) {
	if spec.TargetEndpointID != "" {
		for _, endpoint := range session.Endpoints {
			if endpoint.ID == spec.TargetEndpointID {
				if !endpointHasCapabilities(endpoint, spec.Capabilities) {
					return Endpoint{}, s.err(ErrCapabilityUnavailable, "endpoint lacks required capabilities", true)
				}
				return endpoint, nil
			}
		}
		return Endpoint{}, s.err(ErrEndpointNotFound, "target endpoint not found", true)
	}
	selector := spec.TargetSelector
	for _, endpoint := range session.Endpoints {
		if selector.Role != "" && endpoint.Role != selector.Role {
			continue
		}
		if selector.Platform != "" && endpoint.Platform != selector.Platform {
			continue
		}
		required := spec.Capabilities
		if len(required) == 0 {
			required = selector.Capabilities
		}
		if endpoint.Role == EndpointRoleTarget && endpointOnlineState(endpoint.State) && endpointHasCapabilities(endpoint, required) {
			return endpoint, nil
		}
	}
	return Endpoint{}, s.err(ErrCapabilityUnavailable, "no online endpoint matches task capabilities", true)
}

func (s *MemoryStore) findTaskLocked(sessionID, taskID string) (Task, int, Session, error) {
	session, ok := s.sessions[sessionID]
	if !ok {
		return Task{}, -1, Session{}, s.err(ErrInvalidJoinCode, "session not found", false)
	}
	for i, task := range session.Tasks {
		if task.ID == taskID {
			return task, i, session, nil
		}
	}
	return Task{}, -1, Session{}, s.err(ErrTaskNotFound, "task not found", true)
}

func (s *MemoryStore) taskAndEventLocked(sessionID string, record taskRecord) (Task, Event, error) {
	task, _, _, err := s.findTaskLocked(sessionID, record.TaskID)
	if err != nil {
		return Task{}, Event{}, err
	}
	for _, event := range s.events[sessionID] {
		if event.ID == record.EventID {
			return task, event, nil
		}
	}
	return Task{}, Event{}, s.err(ErrSnapshotRequired, "idempotent event body was compacted", true)
}

func (s *MemoryStore) endpointIndexLocked(session Session, endpointID string) int {
	for i, endpoint := range session.Endpoints {
		if endpoint.ID == endpointID {
			return i
		}
	}
	return -1
}

func (s *MemoryStore) inTerminalGrace(session Session) bool {
	closedAt, ok := s.terminalAt[session.ID]
	if !ok {
		return false
	}
	grace := time.Duration(session.Limits.TerminalGraceMillis) * time.Millisecond
	if grace == 0 {
		grace = 30 * time.Second
	}
	return !s.now().After(closedAt.Add(grace))
}

func (s *MemoryStore) err(code ErrorCode, message string, recoverable bool) ProtocolError {
	return ProtocolError{
		SchemaVersion:   ErrorSchemaVersion,
		Code:            code,
		Message:         message,
		Recoverable:     recoverable,
		RetryAfterMS:    500,
		UserSummary:     message,
		AgentNextAction: defaultAgentNextAction(code),
	}
}

func (s *MemoryStore) now() time.Time {
	return s.clock().UTC()
}

func eventVisibleToEndpoint(event Event, endpoint Endpoint) bool {
	if endpoint.Role == EndpointRoleAgent || endpoint.Role == EndpointRoleOperator {
		return true
	}
	return event.ToEndpointID == "" || event.ToEndpointID == endpoint.ID
}

func endpointOnlineState(state EndpointState) bool {
	return state == EndpointStateOnline || state == EndpointStateBusy || state == EndpointStateDegraded
}

func endpointHasCapabilities(endpoint Endpoint, required []string) bool {
	if len(required) == 0 {
		return true
	}
	have := map[string]bool{}
	for _, capability := range endpoint.Capabilities {
		have[capability] = true
	}
	for _, capability := range required {
		if !have[capability] {
			return false
		}
	}
	return true
}

func endpointIdentityKey(sessionID string, role EndpointRole, fingerprint, platform string) string {
	return sessionID + "|" + string(role) + "|" + fingerprint + "|" + platform
}

func sessionTerminal(status SessionStatus) bool {
	return status == SessionStatusClosed || status == SessionStatusFailed || status == SessionStatusRevoked
}

func cloneEvent(event Event) Event {
	event.Payload = cloneMap(event.Payload)
	return event
}

func eventFingerprint(event Event) (string, error) {
	fingerprint := struct {
		Type           EventType      `json:"type"`
		FromEndpointID string         `json:"from_endpoint_id"`
		ToEndpointID   string         `json:"to_endpoint_id"`
		TaskID         string         `json:"task_id"`
		Payload        map[string]any `json:"payload,omitempty"`
	}{
		Type:           event.Type,
		FromEndpointID: event.FromEndpointID,
		ToEndpointID:   event.ToEndpointID,
		TaskID:         event.TaskID,
		Payload:        event.Payload,
	}
	content, err := json.Marshal(fingerprint)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func jsonPayloadSize(payload map[string]any) (int, error) {
	if payload == nil {
		return 0, nil
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	return len(content), nil
}

func stringMapValue(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	if typed, ok := value.(string); ok {
		return typed
	}
	return fmt.Sprint(value)
}

func retryAfterMSOrSession(value, sessionRetry int) int {
	if value != 0 {
		return value
	}
	if sessionRetry != 0 {
		return sessionRetry
	}
	return 1_000
}

func nonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func defaultAgentNextAction(code ErrorCode) string {
	switch code {
	case ErrSnapshotRequired:
		return "fetch the session snapshot and resume from snapshot_seq"
	case ErrIdempotencyConflict:
		return "reuse the original payload for this idempotency key or choose a new key"
	case ErrCapabilityUnavailable:
		return "wait for a matching endpoint or choose a different adapter"
	case ErrUnauthorizedEndpoint, ErrLeaseExpired:
		return "resume or renew the endpoint lease"
	case ErrTerminalSession, ErrSessionClosed:
		return "do not send new work to this session"
	default:
		return "inspect the structured error and retry only if recoverable"
	}
}
