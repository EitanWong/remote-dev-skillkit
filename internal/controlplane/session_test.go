package controlplane

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewSessionUsesV1SchemaAndFastSnapshotFields(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(30 * time.Minute)

	session, err := NewSession(SessionSpec{
		Profile:            "attended-temporary",
		Reason:             "repair Windows host",
		Capabilities:       []string{"shell", "fs"},
		JoinPolicy:         "single-target",
		AuthorityID:        "auth-main",
		SelectedGatewayURL: "https://gw.example",
		GatewayCandidates: []GatewayCandidate{
			{AuthorityID: "auth-main", URL: "https://gw.example", Priority: 1, Kind: "hosted"},
			{AuthorityID: "auth-main", URL: "https://lan.example", Priority: 2, Kind: "lan"},
		},
		Limits: Limits{
			EventPayloadBytes:       64 * 1024,
			EventBatch:              100,
			ArtifactChunkBytes:      1024 * 1024,
			InlineTaskSummaryBytes:  32 * 1024,
			TerminalGraceMillis:     30_000,
			IdempotencyRetentionSeq: 10_000,
		},
		ReconnectGraceMS: 120_000,
		RetryAfterMS:     750,
		ExpiresAt:        expiresAt,
	}, now)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	if session.SchemaVersion != SessionSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", session.SchemaVersion, SessionSchemaVersion)
	}
	if EndpointSchemaVersion != "rdev.endpoint.v1" ||
		LeaseSchemaVersion != "rdev.lease.v1" ||
		EventSchemaVersion != "rdev.event.v1" ||
		TaskSchemaVersion != "rdev.task.v1" ||
		ArtifactRefSchemaVersion != "rdev.artifact-ref.v1" ||
		ErrorSchemaVersion != "rdev.error.v1" {
		t.Fatalf("schema constants drifted from Control Plane v1")
	}
	if session.ID == "" || session.JoinCode == "" {
		t.Fatalf("session should have generated id and join code: %#v", session)
	}
	if session.Status != SessionStatusCreated {
		t.Fatalf("status = %q, want %q", session.Status, SessionStatusCreated)
	}
	if session.LastSeq != 0 || session.SnapshotSeq != 0 {
		t.Fatalf("new session seq = (%d,%d), want zeroes", session.LastSeq, session.SnapshotSeq)
	}
	if session.AuthorityID != "auth-main" || session.SelectedGatewayURL != "https://gw.example" {
		t.Fatalf("gateway snapshot fields not preserved: %#v", session)
	}
	if session.ReconnectGraceMS != 120_000 || session.RetryAfterMS != 750 {
		t.Fatalf("retry/reconnect hints missing from snapshot: %#v", session)
	}
	if !session.ExpiresAt.Equal(expiresAt) || !session.CreatedAt.Equal(now) || !session.UpdatedAt.Equal(now) {
		t.Fatalf("time fields not gateway-authoritative: %#v", session)
	}
	if session.Limits.EventPayloadBytes != 64*1024 || session.Limits.ArtifactChunkBytes != 1024*1024 {
		t.Fatalf("limits missing from fast snapshot: %#v", session.Limits)
	}
}

func TestNewSessionDefaultsSelectedGatewayFromCandidates(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 30, 0, 0, time.UTC)

	session, err := NewSession(SessionSpec{
		Reason: "gateway HA",
		GatewayCandidates: []GatewayCandidate{
			{AuthorityID: "auth", URL: "https://gw-slow.example", Priority: 20, Kind: "hosted"},
			{AuthorityID: "auth", URL: "https://gw-fast.example", Priority: 5, Kind: "hosted"},
			{AuthorityID: "auth", URL: "https://gw-lan.example", Priority: 0, Kind: "lan"},
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if session.SelectedGatewayURL != "https://gw-lan.example" {
		t.Fatalf("selected_gateway_url = %q, want first priority candidate", session.SelectedGatewayURL)
	}

	explicit, err := NewSession(SessionSpec{
		SelectedGatewayURL: "https://operator-choice.example",
		GatewayCandidates: []GatewayCandidate{
			{AuthorityID: "auth", URL: "https://gw-lan.example", Priority: 0, Kind: "lan"},
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if explicit.SelectedGatewayURL != "https://operator-choice.example" {
		t.Fatalf("explicit selected_gateway_url was overwritten: %#v", explicit)
	}
}

func TestWithEventAdvancesLastSeqAndDerivesHelperProgress(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	session := mustSession(t, now)

	event := Event{
		SchemaVersion:  EventSchemaVersion,
		ID:             "evt_1",
		SessionID:      session.ID,
		Seq:            7,
		Type:           EventTypeHelper,
		FromEndpointID: "endpoint-bootstrap",
		Payload:        map[string]any{"phase": "verifying"},
		CreatedAt:      now.Add(time.Second),
	}

	updated := session.WithEvent(event, now.Add(2*time.Second))
	status := updated.DeriveStatus()

	if session.LastSeq != 0 {
		t.Fatalf("WithEvent mutated original session last_seq = %d", session.LastSeq)
	}
	if updated.LastSeq != 7 || updated.SnapshotSeq != 0 {
		t.Fatalf("WithEvent seq = (%d,%d), want (7,0)", updated.LastSeq, updated.SnapshotSeq)
	}
	if status.Status != StatusHelperVerifying {
		t.Fatalf("status = %q, want %q", status.Status, StatusHelperVerifying)
	}
	if status.LastSeq != 7 || status.LatestEvent.Type != EventTypeHelper {
		t.Fatalf("status did not carry latest event summary: %#v", status)
	}
	if status.AgentNextAction == "" || status.UserSummary == "" {
		t.Fatalf("status should be Agent-native and user-explainable: %#v", status)
	}
}

func TestDeriveStatusReportsGatewaySwitchingAsRecoverable(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	session := mustSession(t, now).WithEvent(Event{
		SchemaVersion: EventSchemaVersion,
		ID:            "evt_gateway",
		SessionID:     "ignored-by-status",
		Seq:           3,
		Type:          EventTypeGateway,
		Payload: map[string]any{
			"action":         "failed",
			"next_url":       "https://gw-backup.example",
			"retry_after_ms": float64(1250),
		},
		CreatedAt: now,
	}, now)

	status := session.DeriveStatus()
	if status.Status != StatusGatewaySwitching {
		t.Fatalf("status = %q, want %q", status.Status, StatusGatewaySwitching)
	}
	if !status.Recoverable || status.RetryAfterMS != 1250 {
		t.Fatalf("gateway switching should be recoverable with retry hint: %#v", status)
	}
	if !strings.Contains(status.AgentNextAction, "wait") {
		t.Fatalf("agent next action should explain waiting/retry: %q", status.AgentNextAction)
	}
}

func TestDeriveStatusReportsTransportDegradedAsOnline(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	session := mustSession(t, now).WithEndpoint(Endpoint{
		SchemaVersion:       EndpointSchemaVersion,
		ID:                  "end_target",
		SessionID:           "ses_1",
		Role:                EndpointRoleTarget,
		Name:                "winbox",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp",
		State:               EndpointStateOnline,
		Transport:           TransportLongPoll,
		LastSeenAt:          now,
	}, now).WithEvent(Event{
		SchemaVersion: EventSchemaVersion,
		ID:            "evt_transport",
		Seq:           8,
		Type:          EventTypeTransport,
		Payload: map[string]any{
			"action":    "wss-failed",
			"selected":  "long-poll",
			"degraded":  true,
			"transport": "long-poll",
		},
		CreatedAt: now,
	}, now)

	status := session.DeriveStatus()
	if status.Status != StatusTransportDegraded {
		t.Fatalf("status = %q, want %q", status.Status, StatusTransportDegraded)
	}
	if !status.Online || !status.Recoverable {
		t.Fatalf("transport degraded should still be online/recoverable: %#v", status)
	}
	if status.Transport != TransportLongPoll {
		t.Fatalf("transport = %q, want %q", status.Transport, TransportLongPoll)
	}
}

func TestEventIsImmutableAndEndpointCursorsTrackProgress(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	session := mustSession(t, now)
	endpoint := Endpoint{
		SchemaVersion:       EndpointSchemaVersion,
		ID:                  "end_target",
		SessionID:           session.ID,
		Role:                EndpointRoleTarget,
		Name:                "target",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp",
		State:               EndpointStateOnline,
		Transport:           TransportWSS,
		ReceivedSeq:         4,
		ProcessedSeq:        3,
		LastSeenAt:          now,
	}
	withEndpoint := session.WithEndpoint(endpoint, now)
	withEvent := withEndpoint.WithEvent(Event{
		SchemaVersion: EventSchemaVersion,
		ID:            "evt_task",
		SessionID:     session.ID,
		Seq:           5,
		Type:          EventTypeTask,
		CreatedAt:     now,
	}, now)

	if len(session.Endpoints) != 0 {
		t.Fatalf("WithEndpoint mutated original session")
	}
	if withEvent.Endpoints[0].ReceivedSeq != 4 || withEvent.Endpoints[0].ProcessedSeq != 3 {
		t.Fatalf("endpoint cursors should not mutate when event is appended: %#v", withEvent.Endpoints[0])
	}

	advanced := withEvent.WithEndpoint(Endpoint{
		SchemaVersion:       EndpointSchemaVersion,
		ID:                  "end_target",
		SessionID:           session.ID,
		Role:                EndpointRoleTarget,
		Name:                "target",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp",
		State:               EndpointStateOnline,
		Transport:           TransportWSS,
		ReceivedSeq:         5,
		ProcessedSeq:        5,
		LastSeenAt:          now.Add(time.Second),
	}, now.Add(time.Second))
	if advanced.Endpoints[0].ProcessedSeq != 5 {
		t.Fatalf("endpoint cursor update was not tracked: %#v", advanced.Endpoints[0])
	}
	if withEvent.Endpoints[0].ProcessedSeq != 3 {
		t.Fatalf("WithEndpoint should be immutable; previous cursor = %d", withEvent.Endpoints[0].ProcessedSeq)
	}
}

func TestLeaseUsesRelativeDurationsForWrongTargetClocks(t *testing.T) {
	serverNow := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	wrongTargetNow := serverNow.Add(-365 * 24 * time.Hour)

	lease := NewLease(LeaseSpec{
		ID:                 "lease_1",
		SessionID:          "ses_1",
		EndpointID:         "end_1",
		Generation:         2,
		Secret:             strings.Repeat("x", 16),
		Transport:          TransportLongPoll,
		SelectedGatewayURL: "https://gw.example",
		LeaseTTLMS:         60_000,
		RenewAfterMS:       20_000,
		RetryAfterMS:       500,
	}, serverNow)

	if lease.ServerTime != serverNow || lease.ExpiresAt != serverNow.Add(60*time.Second) {
		t.Fatalf("lease should carry gateway-authoritative display times: %#v", lease)
	}
	if lease.TimeUntilRenewal(wrongTargetNow) != 20*time.Second {
		t.Fatalf("lease renewal should use relative renew_after_ms, not target wall clock")
	}
	if lease.TimeUntilExpiry(wrongTargetNow) != 60*time.Second {
		t.Fatalf("lease expiry should use relative lease_ttl_ms, not target wall clock")
	}
}

func TestLeaseSecretIsEndpointOnlyAndStatusRedactsIt(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	secret := "lease-secret-must-not-leak"
	lease := NewLease(LeaseSpec{
		ID:         "lease_1",
		SessionID:  "ses_1",
		EndpointID: "end_1",
		Secret:     secret,
	}, now)
	session := mustSession(t, now).WithEndpoint(Endpoint{
		SchemaVersion: EndpointSchemaVersion,
		ID:            lease.EndpointID,
		SessionID:     lease.SessionID,
		Role:          EndpointRoleTarget,
		State:         EndpointStateOnline,
		Transport:     TransportPoll,
		LastSeenAt:    now,
	}, now)

	statusBytes, err := json.Marshal(session.DeriveStatus())
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if strings.Contains(string(statusBytes), secret) {
		t.Fatalf("Agent-facing status leaked lease secret: %s", statusBytes)
	}
	if lease.Secret != secret {
		t.Fatalf("endpoint lease should still carry secret for endpoint auth")
	}
}

func TestStructuredErrorCarriesAgentAndUserRecoveryFields(t *testing.T) {
	err := ProtocolError{
		SchemaVersion:   ErrorSchemaVersion,
		Code:            ErrStaleReplica,
		Message:         "candidate is behind processed cursor",
		Recoverable:     true,
		RetryAfterMS:    1000,
		UserSummary:     "The connection is retrying through another gateway.",
		AgentNextAction: "try the next candidate with the same authority_id",
		Details:         map[string]any{"candidate_last_seq": float64(4), "processed_seq": float64(7)},
	}

	var target error = err
	if !errors.Is(target, err) {
		t.Fatalf("ProtocolError should behave as an error")
	}
	if err.Error() == "" || err.UserSummary == "" || err.AgentNextAction == "" {
		t.Fatalf("structured error missing human/agent recovery fields: %#v", err)
	}
	if err.SchemaVersion != "rdev.error.v1" || !err.Recoverable || err.RetryAfterMS != 1000 {
		t.Fatalf("structured error fields drifted: %#v", err)
	}
}

func TestSessionSnapshotIncludesLimitsAndReconnectGrace(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	session, err := NewSession(SessionSpec{
		Profile:          "managed",
		Reason:           "persistent owned host",
		JoinPolicy:       "multi-target",
		ReconnectGraceMS: 300_000,
		SnapshotSeq:      42,
		Limits: Limits{
			EventPayloadBytes:      16 * 1024,
			EventBatch:             25,
			ArtifactChunkBytes:     512 * 1024,
			InlineTaskSummaryBytes: 8 * 1024,
		},
	}, now)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	snapshot := session.Snapshot()
	if snapshot.Session.SchemaVersion != SessionSchemaVersion {
		t.Fatalf("snapshot missing session schema: %#v", snapshot.Session)
	}
	if snapshot.Session.SnapshotSeq != 42 || snapshot.Session.ReconnectGraceMS != 300_000 {
		t.Fatalf("snapshot missing compaction/reconnect recovery fields: %#v", snapshot.Session)
	}
	if snapshot.Limits.EventBatch != 25 || snapshot.Limits.InlineTaskSummaryBytes != 8*1024 {
		t.Fatalf("snapshot missing flow-control limits: %#v", snapshot.Limits)
	}
	if snapshot.Status.Status == "" || snapshot.Status.AgentNextAction == "" {
		t.Fatalf("snapshot missing derived status: %#v", snapshot.Status)
	}
}

func TestTaskStateTransitionsRejectInvalidMoves(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	task := Task{
		SchemaVersion:  TaskSchemaVersion,
		ID:             "task_1",
		SessionID:      "ses_1",
		Adapter:        "shell",
		Intent:         "run command",
		AttemptID:      "attempt_1",
		IdempotencyKey: "task-key",
		Status:         TaskStatusQueued,
		CreatedAt:      now,
	}

	offered, err := task.Transition(TaskStatusOffered, now.Add(time.Second))
	if err != nil {
		t.Fatalf("queued -> offered should be valid: %v", err)
	}
	running, err := offered.Transition(TaskStatusRunning, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("offered -> running should be valid: %v", err)
	}
	succeeded, err := running.Transition(TaskStatusSucceeded, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("running -> succeeded should be valid: %v", err)
	}
	if !succeeded.Terminal() || succeeded.EndedAt == nil {
		t.Fatalf("succeeded task should be terminal with ended_at: %#v", succeeded)
	}
	if _, err := task.Transition(TaskStatusSucceeded, now); err == nil {
		t.Fatalf("queued -> succeeded should be rejected")
	}
	if _, err := succeeded.Transition(TaskStatusRunning, now); err == nil {
		t.Fatalf("terminal task should reject new attempts/transitions")
	}
}

func mustSession(t *testing.T, now time.Time) Session {
	t.Helper()

	session, err := NewSession(SessionSpec{
		Profile:            "attended-temporary",
		Reason:             "test",
		JoinPolicy:         "single-target",
		AuthorityID:        "auth-main",
		SelectedGatewayURL: "https://gw.example",
		Limits: Limits{
			EventPayloadBytes:      64 * 1024,
			EventBatch:             100,
			ArtifactChunkBytes:     1024 * 1024,
			InlineTaskSummaryBytes: 32 * 1024,
		},
		ReconnectGraceMS: 120_000,
		RetryAfterMS:     500,
	}, now)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	return session
}
