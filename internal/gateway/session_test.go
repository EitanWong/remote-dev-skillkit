package gateway

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestGatewaySessionCreateJoinReplay(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	session, err := gw.CreateSession(controlplane.SessionSpec{
		Profile:            "attended-temporary",
		Reason:             "repair host",
		JoinPolicy:         "single-target",
		AuthorityID:        "auth-main",
		SelectedGatewayURL: "https://gw.example",
		ReconnectGraceMS:   120_000,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	joined, endpoint, lease, events, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "winbox",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-winbox",
		Capabilities:        []string{"shell", "fs"},
		Transport:           controlplane.TransportLongPoll,
	})
	if err != nil {
		t.Fatalf("JoinSessionByCode() error = %v", err)
	}
	if joined.ID != session.ID || endpoint.ID == "" || lease.EndpointID != endpoint.ID {
		t.Fatalf("join did not return session endpoint lease: %#v %#v %#v", joined, endpoint, lease)
	}
	if len(events) == 0 || events[0].Type != controlplane.EventTypeHello {
		t.Fatalf("join should replay hello event: %#v", events)
	}

	appended, err := gw.AppendSessionEvent(session.ID, controlplane.Event{
		Type:           controlplane.EventTypeStatus,
		FromEndpointID: endpoint.ID,
		IdempotencyKey: "status-1",
		Payload:        map[string]any{"state": "online"},
	})
	if err != nil {
		t.Fatalf("AppendSessionEvent() error = %v", err)
	}
	replay, renewed, replayState, err := gw.SessionEventsAfter(session.ID, controlplane.EventCursor{
		EndpointID:  endpoint.ID,
		LeaseSecret: lease.Secret,
		AfterSeq:    appended.Seq - 1,
		VisibleRole: controlplane.EndpointRoleTarget,
	}, 10)
	if err != nil {
		t.Fatalf("SessionEventsAfter() error = %v", err)
	}
	if len(replay) != 1 || replay[0].ID != appended.ID {
		t.Fatalf("replay = %#v, want appended event", replay)
	}
	if renewed.Secret == "" || replayState.LastSeq != appended.Seq {
		t.Fatalf("event polling should piggyback lease and replay state: %#v %#v", renewed, replayState)
	}
}

func TestGatewaySessionJoinAcceptsSupportTicketCode(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicketWithMetadata(
		model.HostModeAttendedTemporary,
		600,
		[]string{"shell.user", "fs.read"},
		"visible temporary remote support",
		map[string]string{
			TicketMetadataGatewayCandidates: `[{"url":"https://gateway.example","kind":"explicit"}]`,
		},
	)
	if err != nil {
		t.Fatalf("CreateTicketWithMetadata() error = %v", err)
	}

	joined, endpoint, lease, events, err := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "winbox",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-winbox",
		Capabilities:        []string{"shell.user", "fs.read"},
		Transport:           controlplane.TransportLongPoll,
	})
	if err != nil {
		t.Fatalf("JoinSessionByCode(ticket.Code) error = %v", err)
	}
	if joined.JoinCode != ticket.Code || joined.Profile != string(ticket.Mode) || joined.ExpiresAt != ticket.ExpiresAt {
		t.Fatalf("bridged session did not preserve ticket contract: session=%#v ticket=%#v", joined, ticket)
	}
	if endpoint.ID == "" || lease.EndpointID != endpoint.ID || len(events) == 0 {
		t.Fatalf("bridged join did not return endpoint lease and hello events: %#v %#v %#v", endpoint, lease, events)
	}
	if len(joined.GatewayCandidates) != 1 || joined.GatewayCandidates[0].URL != "https://gateway.example" {
		t.Fatalf("bridged session did not carry gateway candidates: %#v", joined.GatewayCandidates)
	}
}

func TestGatewaySessionIdempotencySnapshotAndLeaseGrace(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	session, err := gw.CreateSession(controlplane.SessionSpec{ReconnectGraceMS: 120_000})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	_, endpoint, lease, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-winbox",
		Capabilities:        []string{"shell"},
	})
	if err != nil {
		t.Fatalf("JoinSession() error = %v", err)
	}

	event := controlplane.Event{
		Type:           controlplane.EventTypeStatus,
		FromEndpointID: endpoint.ID,
		IdempotencyKey: "status-same",
		Payload:        map[string]any{"state": "online"},
	}
	first, err := gw.AppendSessionEvent(session.ID, event)
	if err != nil {
		t.Fatalf("AppendSessionEvent() error = %v", err)
	}
	second, err := gw.AppendSessionEvent(session.ID, event)
	if err != nil {
		t.Fatalf("second AppendSessionEvent() error = %v", err)
	}
	if first.Seq != second.Seq {
		t.Fatalf("idempotent append allocated second seq: %#v %#v", first, second)
	}
	event.Payload = map[string]any{"state": "offline"}
	if _, err := gw.AppendSessionEvent(session.ID, event); !errors.Is(err, controlplane.ProtocolError{Code: controlplane.ErrIdempotencyConflict}) {
		t.Fatalf("idempotency conflict err = %v", err)
	}

	if err := gw.CompactSessionEvents(session.ID, first.Seq+1); err != nil {
		t.Fatalf("CompactSessionEvents() error = %v", err)
	}
	_, _, replay, err := gw.SessionEventsAfter(session.ID, controlplane.EventCursor{
		EndpointID:  endpoint.ID,
		LeaseSecret: lease.Secret,
		AfterSeq:    0,
		VisibleRole: controlplane.EndpointRoleTarget,
	}, 10)
	if !errors.Is(err, controlplane.ProtocolError{Code: controlplane.ErrSnapshotRequired}) || !replay.SnapshotRequired {
		t.Fatalf("stale replay err=%v state=%#v, want snapshot_required", err, replay)
	}

	now = now.Add(70 * time.Second)
	_, renewed, replay, err := gw.SessionEventsAfter(session.ID, controlplane.EventCursor{
		EndpointID:  endpoint.ID,
		LeaseSecret: lease.Secret,
		AfterSeq:    first.Seq + 1,
		VisibleRole: controlplane.EndpointRoleTarget,
	}, 10)
	if err != nil {
		t.Fatalf("expired lease inside reconnect grace should renew: %v", err)
	}
	if renewed.Secret == lease.Secret || !replay.Reconnecting {
		t.Fatalf("lease grace renewal missing rotation/reconnecting hint: %#v %#v", renewed, replay)
	}
}

func TestGatewaySessionTaskResultArtifactAndTerminalGrace(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	session, err := gw.CreateSession(controlplane.SessionSpec{
		ReconnectGraceMS: 120_000,
		Limits:           controlplane.Limits{TerminalGraceMillis: 30_000},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	_, endpoint, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-winbox",
		Capabilities:        []string{"shell", "fs"},
	})
	if err != nil {
		t.Fatalf("JoinSession() error = %v", err)
	}

	task, event, err := gw.SubmitSessionTask(session.ID, controlplane.TaskSpec{
		Adapter:        "shell",
		Intent:         "hostname",
		Capabilities:   []string{"shell"},
		IdempotencyKey: "task-1",
	})
	if err != nil {
		t.Fatalf("SubmitSessionTask() error = %v", err)
	}
	if task.TargetEndpointID != endpoint.ID || event.Type != controlplane.EventTypeTask {
		t.Fatalf("task was not routed through task event: %#v %#v", task, event)
	}
	task, err = gw.MarkSessionTaskRunning(session.ID, task.ID)
	if err != nil {
		t.Fatalf("MarkSessionTaskRunning() error = %v", err)
	}

	artifact, artifactEvent, err := gw.UpsertSessionArtifact(session.ID, controlplane.ArtifactRef{
		ID:           "art_1",
		TaskID:       task.ID,
		Kind:         "stdout",
		Name:         "stdout.txt",
		SizeBytes:    5,
		SHA256:       strings.Repeat("a", 64),
		ContentType:  "text/plain",
		UploadOffset: 5,
		Complete:     true,
	})
	if err != nil {
		t.Fatalf("UpsertSessionArtifact() error = %v", err)
	}
	if !artifact.Complete || artifactEvent.Type != controlplane.EventTypeArtifact {
		t.Fatalf("artifact upsert did not emit artifact ref event: %#v %#v", artifact, artifactEvent)
	}

	if _, _, err := gw.CloseSession(session.ID); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if _, _, err := gw.SubmitSessionTask(session.ID, controlplane.TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "late-task"}); !errors.Is(err, controlplane.ProtocolError{Code: controlplane.ErrTerminalSession}) {
		t.Fatalf("new task after close err = %v, want terminal_session", err)
	}

	completed, resultEvent, err := gw.CompleteSessionTask(session.ID, task.ID, map[string]any{
		"attempt_id":      task.AttemptID,
		"idempotency_key": "result-1",
		"status":          "succeeded",
	})
	if err != nil {
		t.Fatalf("CompleteSessionTask() during grace error = %v", err)
	}
	if completed.Status != controlplane.TaskStatusSucceeded || resultEvent.Type != controlplane.EventTypeTaskResult {
		t.Fatalf("final result was not accepted during terminal grace: %#v %#v", completed, resultEvent)
	}
}
