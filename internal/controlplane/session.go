package controlplane

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"
)

const (
	SessionSchemaVersion     = "rdev.session.v1"
	EndpointSchemaVersion    = "rdev.endpoint.v1"
	LeaseSchemaVersion       = "rdev.lease.v1"
	EventSchemaVersion       = "rdev.event.v1"
	TaskSchemaVersion        = "rdev.task.v1"
	ArtifactRefSchemaVersion = "rdev.artifact-ref.v1"
	ErrorSchemaVersion       = "rdev.error.v1"
)

type SessionStatus string

const (
	SessionStatusCreated      SessionStatus = "created"
	SessionStatusJoining      SessionStatus = "joining"
	SessionStatusOnline       SessionStatus = "online"
	SessionStatusBusy         SessionStatus = "busy"
	SessionStatusWaiting      SessionStatus = "waiting"
	SessionStatusDegraded     SessionStatus = "degraded"
	SessionStatusReconnecting SessionStatus = "reconnecting"
	SessionStatusRecovered    SessionStatus = "recovered"
	SessionStatusClosed       SessionStatus = "closed"
	SessionStatusFailed       SessionStatus = "failed"
	SessionStatusRevoked      SessionStatus = "revoked"
)

type EndpointRole string

const (
	EndpointRoleTarget    EndpointRole = "target"
	EndpointRoleAgent     EndpointRole = "agent"
	EndpointRoleOperator  EndpointRole = "operator"
	EndpointRoleGateway   EndpointRole = "gateway"
	EndpointRoleWorker    EndpointRole = "worker"
	EndpointRoleWorkspace EndpointRole = "workspace"
	EndpointRoleAdapter   EndpointRole = "adapter"
)

type EndpointState string

const (
	EndpointStateJoining      EndpointState = "joining"
	EndpointStateOnline       EndpointState = "online"
	EndpointStateBusy         EndpointState = "busy"
	EndpointStateDegraded     EndpointState = "degraded"
	EndpointStateReconnecting EndpointState = "reconnecting"
	EndpointStateOffline      EndpointState = "offline"
	EndpointStateClosed       EndpointState = "closed"
	EndpointStateRevoked      EndpointState = "revoked"
)

type Transport string

const (
	TransportWSS      Transport = "wss"
	TransportSSE      Transport = "sse"
	TransportLongPoll Transport = "long-poll"
	TransportPoll     Transport = "poll"
	TransportMesh     Transport = "mesh"
	TransportProvider Transport = "provider"
	TransportLocal    Transport = "local"
)

type EventType string

const (
	EventTypeHello      EventType = "hello"
	EventTypeHelper     EventType = "helper"
	EventTypeGateway    EventType = "gateway"
	EventTypeTransport  EventType = "transport"
	EventTypeStatus     EventType = "status"
	EventTypeTask       EventType = "task"
	EventTypeTaskResult EventType = "task.result"
	EventTypeArtifact   EventType = "artifact"
	EventTypeInterrupt  EventType = "interrupt"
	EventTypeClose      EventType = "close"
)

type TaskStatus string

const (
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusOffered   TaskStatus = "offered"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusPaused    TaskStatus = "paused"
	TaskStatusSucceeded TaskStatus = "succeeded"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

type DerivedStatus string

const (
	StatusJoining           DerivedStatus = "joining"
	StatusHelperDownloading DerivedStatus = "helper-downloading"
	StatusHelperVerifying   DerivedStatus = "helper-verifying"
	StatusGatewaySwitching  DerivedStatus = "gateway-switching"
	StatusOnline            DerivedStatus = "online"
	StatusBusy              DerivedStatus = "busy"
	StatusTransportDegraded DerivedStatus = "transport-degraded"
	StatusReconnecting      DerivedStatus = "reconnecting"
	StatusRecovered         DerivedStatus = "recovered"
	StatusWaiting           DerivedStatus = "waiting"
	StatusFailed            DerivedStatus = "failed"
	StatusClosed            DerivedStatus = "closed"
)

type ErrorCode string

const (
	ErrInvalidJoinCode        ErrorCode = "invalid_join_code"
	ErrJoinPolicyRejected     ErrorCode = "join_policy_rejected"
	ErrUnauthorizedEndpoint   ErrorCode = "unauthorized_endpoint"
	ErrLeaseExpired           ErrorCode = "lease_expired"
	ErrStaleReplica           ErrorCode = "stale_replica"
	ErrSnapshotRequired       ErrorCode = "snapshot_required"
	ErrPayloadTooLarge        ErrorCode = "payload_too_large"
	ErrTooManyEvents          ErrorCode = "too_many_events"
	ErrArtifactOffsetMismatch ErrorCode = "artifact_offset_mismatch"
	ErrChecksumMismatch       ErrorCode = "checksum_mismatch"
	ErrTaskNotFound           ErrorCode = "task_not_found"
	ErrTaskAlreadyTerminal    ErrorCode = "task_already_terminal"
	ErrSessionClosed          ErrorCode = "session_closed"
	ErrAuthorityMismatch      ErrorCode = "authority_mismatch"
	ErrIdempotencyConflict    ErrorCode = "idempotency_conflict"
	ErrCapabilityUnavailable  ErrorCode = "capability_unavailable"
	ErrEndpointNotFound       ErrorCode = "endpoint_not_found"
	ErrStaleCursor            ErrorCode = "stale_cursor"
	ErrTerminalSession        ErrorCode = "terminal_session"
)

type GatewayCandidate struct {
	AuthorityID string `json:"authority_id"`
	URL         string `json:"url"`
	Priority    int    `json:"priority"`
	Kind        string `json:"kind"`
}

type Limits struct {
	EventPayloadBytes       int `json:"event_payload_bytes"`
	EventBatch              int `json:"event_batch"`
	ArtifactChunkBytes      int `json:"artifact_chunk_bytes"`
	InlineTaskSummaryBytes  int `json:"inline_task_result_summary_bytes"`
	TerminalGraceMillis     int `json:"terminal_grace_ms,omitempty"`
	IdempotencyRetentionSeq int `json:"idempotency_retention_seq,omitempty"`
}

type SessionSpec struct {
	Profile            string             `json:"profile"`
	Reason             string             `json:"reason"`
	Capabilities       []string           `json:"capabilities"`
	JoinPolicy         string             `json:"join_policy"`
	GatewayCandidates  []GatewayCandidate `json:"gateway_candidates"`
	SelectedGatewayURL string             `json:"selected_gateway_url"`
	AuthorityID        string             `json:"authority_id"`
	Limits             Limits             `json:"limits"`
	ReconnectGraceMS   int                `json:"reconnect_grace_ms"`
	RetryAfterMS       int                `json:"retry_after_ms"`
	SnapshotSeq        uint64             `json:"snapshot_seq"`
	ExpiresAt          time.Time          `json:"expires_at"`
}

type Session struct {
	SchemaVersion      string             `json:"schema_version"`
	ID                 string             `json:"id"`
	JoinCode           string             `json:"join_code"`
	SourceTicketID     string             `json:"source_ticket_id,omitempty"`
	Profile            string             `json:"profile"`
	Status             SessionStatus      `json:"status"`
	Reason             string             `json:"reason"`
	Capabilities       []string           `json:"capabilities"`
	JoinPolicy         string             `json:"join_policy"`
	GatewayCandidates  []GatewayCandidate `json:"gateway_candidates"`
	SelectedGatewayURL string             `json:"selected_gateway_url"`
	Endpoints          []Endpoint         `json:"endpoints"`
	Tasks              []Task             `json:"tasks,omitempty"`
	Artifacts          []ArtifactRef      `json:"artifacts,omitempty"`
	LastSeq            uint64             `json:"last_seq"`
	SnapshotSeq        uint64             `json:"snapshot_seq"`
	AuthorityID        string             `json:"authority_id"`
	Limits             Limits             `json:"limits"`
	ReconnectGraceMS   int                `json:"reconnect_grace_ms"`
	RetryAfterMS       int                `json:"retry_after_ms"`
	LatestEvent        Event              `json:"latest_event,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	ExpiresAt          time.Time          `json:"expires_at"`
}

type Endpoint struct {
	SchemaVersion       string        `json:"schema_version"`
	ID                  string        `json:"id"`
	SessionID           string        `json:"session_id"`
	Role                EndpointRole  `json:"role"`
	Name                string        `json:"name"`
	Platform            string        `json:"platform"`
	IdentityFingerprint string        `json:"identity_fingerprint"`
	Capabilities        []string      `json:"capabilities"`
	State               EndpointState `json:"state"`
	Transport           Transport     `json:"transport"`
	ReceivedSeq         uint64        `json:"received_seq"`
	ProcessedSeq        uint64        `json:"processed_seq"`
	LastSeenAt          time.Time     `json:"last_seen_at"`
}

type LeaseSpec struct {
	ID                 string
	SessionID          string
	EndpointID         string
	Generation         int
	Secret             string
	Transport          Transport
	SelectedGatewayURL string
	LeaseTTLMS         int
	RenewAfterMS       int
	RetryAfterMS       int
}

type Lease struct {
	SchemaVersion      string    `json:"schema_version"`
	ID                 string    `json:"id"`
	SessionID          string    `json:"session_id"`
	EndpointID         string    `json:"endpoint_id"`
	Generation         int       `json:"generation"`
	Secret             string    `json:"secret"`
	Transport          Transport `json:"transport"`
	SelectedGatewayURL string    `json:"selected_gateway_url"`
	RenewAfter         time.Time `json:"renew_after"`
	ExpiresAt          time.Time `json:"expires_at"`
	ServerTime         time.Time `json:"server_time"`
	LeaseTTLMS         int       `json:"lease_ttl_ms"`
	RenewAfterMS       int       `json:"renew_after_ms"`
	RetryAfterMS       int       `json:"retry_after_ms"`
}

type Event struct {
	SchemaVersion  string         `json:"schema_version"`
	ID             string         `json:"id"`
	SessionID      string         `json:"session_id"`
	Seq            uint64         `json:"seq"`
	Type           EventType      `json:"type"`
	FromEndpointID string         `json:"from_endpoint_id"`
	ToEndpointID   string         `json:"to_endpoint_id"`
	TaskID         string         `json:"task_id"`
	IdempotencyKey string         `json:"idempotency_key"`
	Payload        map[string]any `json:"payload,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type TargetSelector struct {
	Role         EndpointRole `json:"role,omitempty"`
	Platform     string       `json:"platform,omitempty"`
	Capabilities []string     `json:"capabilities,omitempty"`
}

type Task struct {
	SchemaVersion    string         `json:"schema_version"`
	ID               string         `json:"id"`
	SessionID        string         `json:"session_id"`
	TargetEndpointID string         `json:"target_endpoint_id"`
	TargetSelector   TargetSelector `json:"target_selector"`
	Adapter          string         `json:"adapter"`
	Intent           string         `json:"intent"`
	Capabilities     []string       `json:"capabilities"`
	Payload          map[string]any `json:"payload,omitempty"`
	Limits           map[string]any `json:"limits,omitempty"`
	AttemptID        string         `json:"attempt_id"`
	IdempotencyKey   string         `json:"idempotency_key"`
	Status           TaskStatus     `json:"status"`
	CreatedAt        time.Time      `json:"created_at"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	EndedAt          *time.Time     `json:"ended_at,omitempty"`
}

type ArtifactRef struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	SessionID     string `json:"session_id"`
	TaskID        string `json:"task_id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	ContentType   string `json:"content_type"`
	UploadOffset  int64  `json:"upload_offset"`
	Complete      bool   `json:"complete"`
}

type StatusSummary struct {
	Status             DerivedStatus `json:"status"`
	UserSummary        string        `json:"user_summary"`
	AgentNextAction    string        `json:"agent_next_action"`
	SelectedGatewayURL string        `json:"selected_gateway_url"`
	Transport          Transport     `json:"transport"`
	LastSeq            uint64        `json:"last_seq"`
	SnapshotSeq        uint64        `json:"snapshot_seq"`
	LatestEvent        Event         `json:"latest_event,omitempty"`
	Recoverable        bool          `json:"recoverable"`
	RetryAfterMS       int           `json:"retry_after_ms"`
	Online             bool          `json:"online"`
}

type SessionSnapshot struct {
	Session            Session            `json:"session"`
	Endpoints          []Endpoint         `json:"endpoints"`
	Tasks              []Task             `json:"tasks"`
	Artifacts          []ArtifactRef      `json:"artifacts"`
	Status             StatusSummary      `json:"status"`
	GatewayCandidates  []GatewayCandidate `json:"gateway_candidates"`
	SelectedGatewayURL string             `json:"selected_gateway_url"`
	Limits             Limits             `json:"limits"`
	ReconnectGraceMS   int                `json:"reconnect_grace_ms"`
	RetryAfterMS       int                `json:"retry_after_ms"`
}

type ProtocolError struct {
	SchemaVersion   string         `json:"schema_version"`
	Code            ErrorCode      `json:"code"`
	Message         string         `json:"message"`
	Recoverable     bool           `json:"recoverable"`
	RetryAfterMS    int            `json:"retry_after_ms"`
	UserSummary     string         `json:"user_summary"`
	AgentNextAction string         `json:"agent_next_action"`
	Details         map[string]any `json:"details,omitempty"`
}

func InvalidJoinCodeError() ProtocolError {
	return ProtocolError{
		SchemaVersion:   ErrorSchemaVersion,
		Code:            ErrInvalidJoinCode,
		Message:         "join code is invalid",
		Recoverable:     false,
		UserSummary:     "The support-session entry is invalid or no longer active.",
		AgentNextAction: "create a fresh support-session entry and use its generated handoff",
	}
}

func NewSession(spec SessionSpec, now time.Time) (Session, error) {
	id, err := newID("ses")
	if err != nil {
		return Session{}, err
	}
	joinCode, err := newJoinCode()
	if err != nil {
		return Session{}, err
	}
	profile := strings.TrimSpace(spec.Profile)
	if profile == "" {
		profile = "attended-temporary"
	}
	joinPolicy := strings.TrimSpace(spec.JoinPolicy)
	if joinPolicy == "" {
		joinPolicy = "single-target"
	}
	gatewayCandidates := cloneGatewayCandidates(spec.GatewayCandidates)
	selectedGatewayURL := selectedGatewayURL(spec.SelectedGatewayURL, gatewayCandidates)
	limits := spec.Limits.withDefaults()

	return Session{
		SchemaVersion:      SessionSchemaVersion,
		ID:                 id,
		JoinCode:           joinCode,
		Profile:            profile,
		Status:             SessionStatusCreated,
		Reason:             spec.Reason,
		Capabilities:       append([]string(nil), spec.Capabilities...),
		JoinPolicy:         joinPolicy,
		GatewayCandidates:  gatewayCandidates,
		SelectedGatewayURL: selectedGatewayURL,
		Endpoints:          []Endpoint{},
		Tasks:              []Task{},
		Artifacts:          []ArtifactRef{},
		SnapshotSeq:        spec.SnapshotSeq,
		AuthorityID:        spec.AuthorityID,
		Limits:             limits,
		ReconnectGraceMS:   spec.ReconnectGraceMS,
		RetryAfterMS:       spec.RetryAfterMS,
		CreatedAt:          now.UTC(),
		UpdatedAt:          now.UTC(),
		ExpiresAt:          spec.ExpiresAt.UTC(),
	}, nil
}

func (s Session) WithEndpoint(endpoint Endpoint, now time.Time) Session {
	next := s.clone()
	if endpoint.SchemaVersion == "" {
		endpoint.SchemaVersion = EndpointSchemaVersion
	}
	if endpoint.SessionID == "" {
		endpoint.SessionID = s.ID
	}
	endpoint.Capabilities = append([]string(nil), endpoint.Capabilities...)

	replaced := false
	for i, existing := range next.Endpoints {
		if existing.ID == endpoint.ID && endpoint.ID != "" {
			next.Endpoints[i] = endpoint
			replaced = true
			break
		}
	}
	if !replaced {
		next.Endpoints = append(next.Endpoints, endpoint)
	}
	next.UpdatedAt = now.UTC()
	return next
}

func (s Session) WithEvent(event Event, now time.Time) Session {
	next := s.clone()
	if event.SchemaVersion == "" {
		event.SchemaVersion = EventSchemaVersion
	}
	if event.SessionID == "" {
		event.SessionID = s.ID
	}
	event.Payload = cloneMap(event.Payload)
	next.LatestEvent = event
	if event.Seq > next.LastSeq {
		next.LastSeq = event.Seq
	}
	next.UpdatedAt = now.UTC()
	return next
}

func (s Session) DeriveStatus() StatusSummary {
	status := StatusSummary{
		Status:             StatusWaiting,
		UserSummary:        "Waiting for the target to connect.",
		AgentNextAction:    "wait for the next session event",
		SelectedGatewayURL: s.SelectedGatewayURL,
		LastSeq:            s.LastSeq,
		SnapshotSeq:        s.SnapshotSeq,
		LatestEvent:        s.LatestEvent,
		RetryAfterMS:       s.RetryAfterMS,
	}

	if transport, ok := firstEndpointTransport(s.Endpoints); ok {
		status.Transport = transport
	}
	if endpointOnline(s.Endpoints) {
		status.Status = StatusOnline
		status.Online = true
		status.UserSummary = "The target is online."
		status.AgentNextAction = "send tasks or continue watching session events"
	}
	if endpointBusy(s.Endpoints) {
		status.Status = StatusBusy
		status.Online = true
		status.UserSummary = "The target is busy running a task."
		status.AgentNextAction = "wait for the task result event"
	}

	switch s.Status {
	case SessionStatusJoining:
		status.Status = StatusJoining
		status.UserSummary = "The target is joining the session."
	case SessionStatusReconnecting:
		status.Status = StatusReconnecting
		status.Recoverable = true
		status.UserSummary = "The target is reconnecting."
		status.AgentNextAction = "wait for the endpoint to resume before asking the user to rerun the command"
	case SessionStatusRecovered:
		status.Status = StatusRecovered
		status.Online = true
		status.Recoverable = true
		status.UserSummary = "The target recovered its connection."
	case SessionStatusFailed:
		status.Status = StatusFailed
		status.UserSummary = "The session failed."
		status.AgentNextAction = "surface the structured error and recovery action"
	case SessionStatusClosed, SessionStatusRevoked:
		status.Status = StatusClosed
		status.UserSummary = "The session is closed."
		status.AgentNextAction = "do not send new tasks"
	}

	switch s.LatestEvent.Type {
	case EventTypeHelper:
		phase := stringPayload(s.LatestEvent.Payload, "phase")
		switch phase {
		case "downloading":
			status.Status = StatusHelperDownloading
			status.UserSummary = "The helper is downloading."
			status.AgentNextAction = "wait for helper verification or ready event"
		case "verifying":
			status.Status = StatusHelperVerifying
			status.UserSummary = "The helper is verifying its checksum."
			status.AgentNextAction = "wait for helper ready event"
		case "ready":
			status.Status = StatusOnline
			status.Online = true
			status.UserSummary = "The helper is ready."
			status.AgentNextAction = "continue with session tasks"
		}
	case EventTypeGateway:
		action := stringPayload(s.LatestEvent.Payload, "action")
		if action == "failed" || action == "trying" {
			status.Status = StatusGatewaySwitching
			status.Recoverable = true
			status.UserSummary = "The connection is switching gateways."
			status.AgentNextAction = "wait while rdev tries the next gateway candidate"
			status.RetryAfterMS = intPayload(s.LatestEvent.Payload, "retry_after_ms", status.RetryAfterMS)
		}
	case EventTypeTransport:
		if boolPayload(s.LatestEvent.Payload, "degraded") || stringPayload(s.LatestEvent.Payload, "action") == "wss-failed" {
			status.Status = StatusTransportDegraded
			status.Online = true
			status.Recoverable = true
			status.UserSummary = "The target is online through a fallback transport."
			status.AgentNextAction = "continue normally; rdev will keep the fallback transport alive"
			if transport := Transport(stringPayload(s.LatestEvent.Payload, "transport")); transport != "" {
				status.Transport = transport
			}
			if selected := Transport(stringPayload(s.LatestEvent.Payload, "selected")); status.Transport == "" && selected != "" {
				status.Transport = selected
			}
		}
	case EventTypeTask:
		status.Status = StatusBusy
		status.Online = true
		status.UserSummary = "A task is active."
		status.AgentNextAction = "wait for task.result"
	case EventTypeClose:
		status.Status = StatusClosed
		status.UserSummary = "The session is closed."
		status.AgentNextAction = "do not send new tasks"
	}

	return status
}

func NewLease(spec LeaseSpec, now time.Time) Lease {
	ttlMS := spec.LeaseTTLMS
	if ttlMS == 0 {
		ttlMS = 60_000
	}
	renewMS := spec.RenewAfterMS
	if renewMS == 0 {
		renewMS = ttlMS / 3
	}
	retryMS := spec.RetryAfterMS
	if retryMS == 0 {
		retryMS = 1_000
	}
	id := spec.ID
	if id == "" {
		id, _ = newID("lease")
	}
	return Lease{
		SchemaVersion:      LeaseSchemaVersion,
		ID:                 id,
		SessionID:          spec.SessionID,
		EndpointID:         spec.EndpointID,
		Generation:         spec.Generation,
		Secret:             spec.Secret,
		Transport:          spec.Transport,
		SelectedGatewayURL: spec.SelectedGatewayURL,
		RenewAfter:         now.Add(time.Duration(renewMS) * time.Millisecond).UTC(),
		ExpiresAt:          now.Add(time.Duration(ttlMS) * time.Millisecond).UTC(),
		ServerTime:         now.UTC(),
		LeaseTTLMS:         ttlMS,
		RenewAfterMS:       renewMS,
		RetryAfterMS:       retryMS,
	}
}

func (l Lease) TimeUntilRenewal(_ time.Time) time.Duration {
	return time.Duration(l.RenewAfterMS) * time.Millisecond
}

func (l Lease) TimeUntilExpiry(_ time.Time) time.Duration {
	return time.Duration(l.LeaseTTLMS) * time.Millisecond
}

func (s Session) Snapshot() SessionSnapshot {
	session := s.clone()
	return SessionSnapshot{
		Session:            session,
		Endpoints:          cloneEndpoints(s.Endpoints),
		Tasks:              cloneTasks(s.Tasks),
		Artifacts:          append([]ArtifactRef(nil), s.Artifacts...),
		Status:             s.DeriveStatus(),
		GatewayCandidates:  cloneGatewayCandidates(s.GatewayCandidates),
		SelectedGatewayURL: s.SelectedGatewayURL,
		Limits:             s.Limits,
		ReconnectGraceMS:   s.ReconnectGraceMS,
		RetryAfterMS:       s.RetryAfterMS,
	}
}

func (t Task) Terminal() bool {
	switch t.Status {
	case TaskStatusSucceeded, TaskStatusFailed, TaskStatusCanceled:
		return true
	default:
		return false
	}
}

func (t Task) Transition(next TaskStatus, now time.Time) (Task, error) {
	if t.Terminal() {
		return Task{}, ProtocolError{
			SchemaVersion:   ErrorSchemaVersion,
			Code:            ErrTaskAlreadyTerminal,
			Message:         "task is already terminal",
			UserSummary:     "The task has already finished.",
			AgentNextAction: "do not reuse a terminal task; create a new task if needed",
		}
	}
	if !validTaskTransition(t.Status, next) {
		return Task{}, ProtocolError{
			SchemaVersion:   ErrorSchemaVersion,
			Code:            ErrTaskAlreadyTerminal,
			Message:         fmt.Sprintf("invalid task transition %s -> %s", t.Status, next),
			UserSummary:     "The requested task state change is invalid.",
			AgentNextAction: "refresh the task snapshot before sending another state change",
		}
	}

	updated := t
	updated.Status = next
	if next == TaskStatusRunning && updated.StartedAt == nil {
		startedAt := now.UTC()
		updated.StartedAt = &startedAt
	}
	if updated.Terminal() {
		endedAt := now.UTC()
		updated.EndedAt = &endedAt
	}
	return updated, nil
}

func (e ProtocolError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func (e ProtocolError) Is(target error) bool {
	other, ok := target.(ProtocolError)
	if !ok {
		return false
	}
	return e.Code == other.Code
}

func (s Session) clone() Session {
	next := s
	next.Capabilities = append([]string(nil), s.Capabilities...)
	next.GatewayCandidates = cloneGatewayCandidates(s.GatewayCandidates)
	next.Endpoints = cloneEndpoints(s.Endpoints)
	next.Tasks = cloneTasks(s.Tasks)
	next.Artifacts = append([]ArtifactRef(nil), s.Artifacts...)
	next.LatestEvent.Payload = cloneMap(s.LatestEvent.Payload)
	return next
}

func (l Limits) withDefaults() Limits {
	if l.EventPayloadBytes == 0 {
		l.EventPayloadBytes = 64 * 1024
	}
	if l.EventBatch == 0 {
		l.EventBatch = 100
	}
	if l.ArtifactChunkBytes == 0 {
		l.ArtifactChunkBytes = 1024 * 1024
	}
	if l.InlineTaskSummaryBytes == 0 {
		l.InlineTaskSummaryBytes = 32 * 1024
	}
	return l
}

func validTaskTransition(from, to TaskStatus) bool {
	switch from {
	case TaskStatusQueued:
		return to == TaskStatusOffered || to == TaskStatusCanceled
	case TaskStatusOffered:
		return to == TaskStatusRunning || to == TaskStatusCanceled
	case TaskStatusRunning:
		return to == TaskStatusPaused || to == TaskStatusSucceeded || to == TaskStatusFailed || to == TaskStatusCanceled
	case TaskStatusPaused:
		return to == TaskStatusRunning || to == TaskStatusSucceeded || to == TaskStatusFailed || to == TaskStatusCanceled
	default:
		return false
	}
}

func endpointOnline(endpoints []Endpoint) bool {
	for _, endpoint := range endpoints {
		switch endpoint.State {
		case EndpointStateOnline, EndpointStateBusy, EndpointStateDegraded:
			return true
		}
	}
	return false
}

func endpointBusy(endpoints []Endpoint) bool {
	for _, endpoint := range endpoints {
		if endpoint.State == EndpointStateBusy {
			return true
		}
	}
	return false
}

func firstEndpointTransport(endpoints []Endpoint) (Transport, bool) {
	for _, endpoint := range endpoints {
		if endpoint.Transport != "" {
			return endpoint.Transport, true
		}
	}
	return "", false
}

func cloneGatewayCandidates(candidates []GatewayCandidate) []GatewayCandidate {
	return append([]GatewayCandidate(nil), candidates...)
}

func selectedGatewayURL(explicit string, candidates []GatewayCandidate) string {
	explicit = strings.TrimRight(strings.TrimSpace(explicit), "/")
	if explicit != "" {
		return explicit
	}
	selected := ""
	selectedPriority := 0
	for _, candidate := range candidates {
		url := strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if url == "" {
			continue
		}
		if selected == "" || candidate.Priority < selectedPriority {
			selected = url
			selectedPriority = candidate.Priority
		}
	}
	return selected
}

func cloneEndpoints(endpoints []Endpoint) []Endpoint {
	cloned := append([]Endpoint(nil), endpoints...)
	for i := range cloned {
		cloned[i].Capabilities = append([]string(nil), cloned[i].Capabilities...)
	}
	return cloned
}

func cloneTasks(tasks []Task) []Task {
	cloned := append([]Task(nil), tasks...)
	for i := range cloned {
		cloned[i].Capabilities = append([]string(nil), cloned[i].Capabilities...)
		cloned[i].Payload = cloneMap(cloned[i].Payload)
		cloned[i].Limits = cloneMap(cloned[i].Limits)
	}
	return cloned
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func stringPayload(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func boolPayload(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

func intPayload(payload map[string]any, key string, fallback int) int {
	value, ok := payload[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return fallback
	}
}

func newJoinCode() (string, error) {
	var raw [5]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
	if len(encoded) > 8 {
		encoded = encoded[:8]
	}
	return encoded[:4] + "-" + encoded[4:], nil
}

func newID(prefix string) (string, error) {
	var raw [10]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]))
	return prefix + "_" + encoded, nil
}
