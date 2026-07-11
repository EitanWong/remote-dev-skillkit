package gateway

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestActiveTicketCreatesBoundSessionImmediately(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "active ticket binding")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.SessionID == "" {
		t.Fatal("active ticket did not bind a session")
	}
	session, err := gw.Session(ticket.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if session.JoinCode != ticket.Code || session.SourceTicketID != ticket.ID || session.ExpiresAt != ticket.ExpiresAt {
		t.Fatal("active ticket session binding did not preserve ticket identity and expiry")
	}
}

func TestProbingTicketBindsExactlyOneSessionWhenPublished(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "publish binding", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.SessionID != "" {
		t.Fatal("probing ticket bound a session before publication")
	}
	first, err := gw.PublishTicket(ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := gw.PublishTicket(ticket.ID)
	if err != nil {
		t.Fatalf("duplicate publish was not idempotent: %v", err)
	}
	if first.SessionID == "" || second.SessionID != first.SessionID {
		t.Fatal("duplicate publish created or returned a different session binding")
	}
	if sessions := gw.Snapshot().ControlPlane.Sessions; len(sessions) != 1 || sessions[0].ID != first.SessionID {
		t.Fatalf("published ticket created %d sessions, want exactly one", len(sessions))
	}
}

func TestTicketTerminationRevokesBoundSessionEndpointAndLease(t *testing.T) {
	for _, action := range []struct {
		name string
		run  func(*MemoryGateway, string) error
	}{
		{
			name: "revoke",
			run: func(gw *MemoryGateway, ticketID string) error {
				_, err := gw.RevokeTicket(ticketID, "operator revoked")
				return err
			},
		},
		{
			name: "rollback",
			run: func(gw *MemoryGateway, ticketID string) error {
				_, _, err := gw.RollbackTicket(ticketID, "publication rollback")
				return err
			},
		},
	} {
		t.Run(action.name, func(t *testing.T) {
			gw := NewMemoryGateway()
			ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "ticket termination")
			if err != nil {
				t.Fatal(err)
			}
			_, endpoint, lease, _, err := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{
				Role:                controlplane.EndpointRoleTarget,
				Name:                "windows-target",
				Platform:            "windows/amd64",
				IdentityFingerprint: "fp-terminated-target",
				Transport:           controlplane.TransportLongPoll,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := action.run(gw, ticket.ID); err != nil {
				t.Fatal(err)
			}

			session, err := gw.Session(ticket.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if session.Status != controlplane.SessionStatusRevoked || len(session.Endpoints) != 1 || session.Endpoints[0].State != controlplane.EndpointStateRevoked {
				t.Fatal("ticket termination did not revoke the bound session and target endpoint")
			}
			leaseErr := gw.ValidateSessionLease(ticket.SessionID, endpoint.ID, lease.Secret)
			var protocolErr controlplane.ProtocolError
			if !errors.As(leaseErr, &protocolErr) || protocolErr.Recoverable {
				t.Fatal("ticket termination did not invalidate the endpoint lease permanently")
			}
			_, _, _, eventsErr := gw.SessionEventsAfter(ticket.SessionID, controlplane.EventCursor{
				EndpointID:  endpoint.ID,
				LeaseSecret: lease.Secret,
			}, 10)
			if !errors.As(eventsErr, &protocolErr) || protocolErr.Code != controlplane.ErrTerminalSession || protocolErr.Recoverable {
				t.Fatal("terminal session event polling did not reject the old lease permanently")
			}
			_, _, _, _, joinErr := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
			if !errors.As(joinErr, &protocolErr) || protocolErr.Code != controlplane.ErrInvalidJoinCode || protocolErr.Recoverable {
				t.Fatal("terminated ticket join did not return permanent invalid_join_code")
			}
		})
	}
}

func TestEndpointOnlyConnectionPreventsConditionalTicketRollback(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "endpoint connected")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-online-target",
		Transport:           controlplane.TransportLongPoll,
	}); err != nil {
		t.Fatal(err)
	}

	current, affected, rolledBack, err := gw.RollbackTicketIfNoConnectedHost(ticket.ID, "route disappeared")
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack || current.Status != model.TicketStatusActive || len(affected) != 0 {
		t.Fatal("fresh online target endpoint did not protect its ticket from rollback")
	}
}

func TestTicketOwnedDirectSessionJoinUsesTicketGate(t *testing.T) {
	for _, state := range []string{"revoked", "expired"} {
		t.Run(state, func(t *testing.T) {
			now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
			gw := NewMemoryGatewayWithClock(func() time.Time { return now })
			ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 60, nil, "direct join gate")
			if err != nil {
				t.Fatal(err)
			}
			if state == "revoked" {
				if _, err := gw.RevokeTicket(ticket.ID, "operator revoked"); err != nil {
					t.Fatal(err)
				}
			} else {
				now = now.Add(61 * time.Second)
			}
			_, _, _, err = gw.JoinSession(ticket.SessionID, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
			var protocolErr controlplane.ProtocolError
			if !errors.As(err, &protocolErr) || protocolErr.Code != controlplane.ErrInvalidJoinCode || protocolErr.Recoverable {
				t.Fatal("direct ticket-owned session join bypassed the permanent ticket gate")
			}
		})
	}
}

func TestRevokedOrExpiredLegacyHostIsNotConnected(t *testing.T) {
	for _, state := range []string{"revoked", "expired"} {
		t.Run(state, func(t *testing.T) {
			now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
			gw := NewMemoryGatewayWithClock(func() time.Time { return now })
			ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 60, nil, "legacy host lifecycle")
			if err != nil {
				t.Fatal(err)
			}
			host, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "legacy-host", OS: "windows", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			host, err = gw.ActivateHost(host.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			secret, err := gw.GenerateHostSecret(host.ID)
			if err != nil {
				t.Fatal(err)
			}
			if state == "revoked" {
				if _, err := gw.RevokeTicket(ticket.ID, "operator revoked"); err != nil {
					t.Fatal(err)
				}
				revokedHost, err := gw.Host(host.ID)
				if err != nil || revokedHost.Status != model.HostStatusRevoked || gw.ValidateHostSecret(host.ID, secret) {
					t.Fatal("ticket revoke did not terminate the legacy host authorization")
				}
			} else {
				now = now.Add(61 * time.Second)
			}
			if gw.TicketHasConnectedHost(ticket.ID) {
				t.Fatal("revoked or expired ticket retained connected-host status")
			}
		})
	}
}

func TestTicketJoinGateReturnsCanonicalPermanentInvalidJoinCode(t *testing.T) {
	baselineGateway := NewMemoryGateway()
	_, _, _, _, baselineErr := baselineGateway.JoinSessionByCode("UNKNOWN-STANDALONE-CODE", controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
	var canonical controlplane.ProtocolError
	if !errors.As(baselineErr, &canonical) {
		t.Fatal("unknown standalone join code did not return a structured protocol error")
	}
	for _, state := range []string{"probing", "revoked", "expired", "corrupt"} {
		t.Run(state, func(t *testing.T) {
			now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
			gw := NewMemoryGatewayWithClock(func() time.Time { return now })
			var ticket model.Ticket
			var err error
			if state == "probing" {
				ticket, err = gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "ticket join gate", nil)
			} else {
				ticket, err = gw.CreateTicket(model.HostModeAttendedTemporary, 60, nil, "ticket join gate")
			}
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "revoked":
				if _, err := gw.RevokeTicket(ticket.ID, "operator revoked"); err != nil {
					t.Fatal(err)
				}
			case "expired":
				now = now.Add(61 * time.Second)
			case "corrupt":
				gw.mu.Lock()
				stored := gw.tickets[ticket.ID]
				stored.SessionID = ""
				gw.tickets[ticket.ID] = stored
				gw.mu.Unlock()
			}

			_, _, _, _, err = gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
			var protocolErr controlplane.ProtocolError
			if !errors.As(err, &protocolErr) || protocolErr.Code != controlplane.ErrInvalidJoinCode || protocolErr.Recoverable {
				t.Fatal("ticket gate did not return canonical permanent invalid_join_code")
			}
			if !reflect.DeepEqual(protocolErr, canonical) {
				t.Fatal("ticket gate exposed a state oracle through the invalid_join_code envelope")
			}
		})
	}
}

func TestTicketJoinGateHidesTerminalAndUnknownSessionState(t *testing.T) {
	baselineGateway := NewMemoryGateway()
	_, _, _, _, baselineErr := baselineGateway.JoinSessionByCode("UNKNOWN-STANDALONE-CODE", controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
	var canonical controlplane.ProtocolError
	if !errors.As(baselineErr, &canonical) {
		t.Fatal("unknown join code did not return a structured protocol error")
	}

	gw := NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "terminal join gate")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CloseSession(ticket.SessionID); err != nil {
		t.Fatal(err)
	}

	joinAttempts := []struct {
		name string
		run  func() error
	}{
		{
			name: "ticket code",
			run: func() error {
				_, _, _, _, err := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
				return err
			},
		},
		{
			name: "bound session id",
			run: func() error {
				_, _, _, err := gw.JoinSession(ticket.SessionID, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
				return err
			},
		},
		{
			name: "unknown session id",
			run: func() error {
				_, _, _, err := gw.JoinSession("ses_unknown", controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
				return err
			},
		},
	}
	for _, attempt := range joinAttempts {
		t.Run(attempt.name, func(t *testing.T) {
			var protocolErr controlplane.ProtocolError
			if err := attempt.run(); !errors.As(err, &protocolErr) {
				t.Fatalf("join error was not structured: %v", err)
			}
			if !reflect.DeepEqual(protocolErr, canonical) {
				t.Fatalf("join exposed session state: got %#v, want %#v", protocolErr, canonical)
			}
		})
	}
}

func TestTicketPublishCodeCollisionDoesNotOverwriteSessionNamespace(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "collision", nil)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := gw.sessionStore.CreateSessionForTicket(controlplane.SessionSpec{ExpiresAt: ticket.ExpiresAt}, "tkt_other", ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.PublishTicket(ticket.ID); err == nil {
		t.Fatal("ticket publish overwrote an existing session join code")
	}
	stored, ok := gw.TicketForCode(ticket.Code)
	if !ok || stored.Status != model.TicketStatusProbing || stored.SessionID != "" {
		t.Fatal("failed collision publish mutated the probing ticket")
	}
	preserved, err := gw.Session(existing.ID)
	if err != nil || preserved.JoinCode != ticket.Code || preserved.SourceTicketID != "tkt_other" {
		t.Fatal("failed collision publish replaced the existing session")
	}
}

func TestStandaloneSessionJoinRemainsIndependentOfTicketGate(t *testing.T) {
	gw := NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	joined, endpoint, lease, _, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
	if err != nil {
		t.Fatal(err)
	}
	if joined.ID != session.ID || endpoint.ID == "" || lease.Secret == "" {
		t.Fatal("standalone control-plane session join regressed")
	}
}

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
