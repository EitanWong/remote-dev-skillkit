package controlplane

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestJoinByCodeReturnsSessionEndpointLeaseAndInitialEvents(t *testing.T) {
	store, clock := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{JoinPolicy: "single-target"})

	joined, endpoint, lease, events, err := store.JoinByCode(session.JoinCode, EndpointSpec{
		Role:                EndpointRoleTarget,
		Name:                "winbox",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-winbox",
		Capabilities:        []string{"shell", "fs"},
		Transport:           TransportLongPoll,
	})
	if err != nil {
		t.Fatalf("JoinByCode() error = %v", err)
	}

	if joined.ID != session.ID || endpoint.ID == "" || lease.EndpointID != endpoint.ID {
		t.Fatalf("join did not return session, endpoint and endpoint lease: %#v %#v %#v", joined, endpoint, lease)
	}
	if endpoint.SchemaVersion != EndpointSchemaVersion || lease.SchemaVersion != LeaseSchemaVersion {
		t.Fatalf("join schemas drifted: %#v %#v", endpoint, lease)
	}
	if len(events) == 0 || events[0].Type != EventTypeHello || events[0].Seq != 1 {
		t.Fatalf("join should emit initial hello event, got %#v", events)
	}
	if joined.LastSeq != events[len(events)-1].Seq {
		t.Fatalf("session last_seq should track join event: %#v events=%#v", joined, events)
	}
	if !lease.ExpiresAt.After(clock.now()) || lease.LeaseTTLMS == 0 || lease.RenewAfterMS == 0 {
		t.Fatalf("join lease missing relative timing: %#v", lease)
	}
}

func TestJoinLeaseCarriesSelectedGatewayFromCandidates(t *testing.T) {
	store, _ := newStoreHarness()
	session, err := store.CreateSession(SessionSpec{
		JoinPolicy: "single-target",
		GatewayCandidates: []GatewayCandidate{
			{AuthorityID: "auth-main", URL: "https://gw-backup.example", Priority: 100, Kind: "hosted"},
			{AuthorityID: "auth-main", URL: "https://gw-primary.example", Priority: 10, Kind: "hosted"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, lease, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "linux/amd64",
		IdentityFingerprint: "fp-linux",
		Transport:           TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lease.SelectedGatewayURL != "https://gw-primary.example" {
		t.Fatalf("lease selected_gateway_url = %q, want selected candidate", lease.SelectedGatewayURL)
	}
}

func TestJoinSessionIsIdempotentForEndpointIdentity(t *testing.T) {
	store, clock := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{JoinPolicy: "single-target"})
	spec := EndpointSpec{
		Role:                EndpointRoleTarget,
		Name:                "winbox",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-winbox",
		Capabilities:        []string{"shell"},
		Transport:           TransportWSS,
	}

	_, first, firstLease, err := store.JoinSession(session.ID, spec)
	if err != nil {
		t.Fatalf("first JoinSession() error = %v", err)
	}
	clock.advance(10 * time.Second)
	spec.Transport = TransportLongPoll
	joined, second, secondLease, err := store.JoinSession(session.ID, spec)
	if err != nil {
		t.Fatalf("second JoinSession() error = %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("same endpoint identity should resume endpoint, got %q then %q", first.ID, second.ID)
	}
	if second.Transport != TransportLongPoll || !second.LastSeenAt.Equal(clock.now()) {
		t.Fatalf("resumed endpoint should update transport and liveness: %#v", second)
	}
	if secondLease.Generation <= firstLease.Generation || secondLease.Secret == firstLease.Secret {
		t.Fatalf("resumed endpoint should rotate lease generation/secret: first=%#v second=%#v", firstLease, secondLease)
	}
	if len(joined.Endpoints) != 1 {
		t.Fatalf("idempotent join should not create duplicate endpoints: %#v", joined.Endpoints)
	}
}

func TestSingleTargetJoinPolicyRejectsDifferentTarget(t *testing.T) {
	store, _ := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{JoinPolicy: "single-target"})
	if _, _, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-one",
	}); err != nil {
		t.Fatalf("first target join error = %v", err)
	}

	_, _, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-two",
	})
	if !errors.Is(err, ProtocolError{Code: ErrJoinPolicyRejected}) {
		t.Fatalf("second different target err = %v, want join_policy_rejected", err)
	}
}

func TestEventsAfterReturnsReplayAndPiggybackLease(t *testing.T) {
	store, clock := newStoreHarness()
	session, endpoint, lease := mustJoinedTarget(t, store)
	mustAppend(t, store, session.ID, Event{Type: EventTypeStatus, FromEndpointID: endpoint.ID, IdempotencyKey: "status-1"})

	events, renewed, replay, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:    endpoint.ID,
		LeaseSecret:   lease.Secret,
		AfterSeq:      0,
		ReceivedSeq:   1,
		ProcessedSeq:  1,
		VisibleRole:   EndpointRoleTarget,
		EndpointState: EndpointStateOnline,
	}, 10)
	if err != nil {
		t.Fatalf("EventsAfter() error = %v", err)
	}

	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("events replay = %#v, want seq 1 and 2", events)
	}
	if renewed.Secret == "" || renewed.EndpointID != endpoint.ID || !renewed.ExpiresAt.After(clock.now()) {
		t.Fatalf("event polling should piggyback renewed lease: %#v", renewed)
	}
	if replay.SnapshotRequired || replay.LastSeq != 2 || replay.RetryAfterMS == 0 {
		t.Fatalf("replay state missing hints: %#v", replay)
	}
}

func TestGatewaySwitchEventUpdatesSelectedGatewayAndRenewedLease(t *testing.T) {
	store, _ := newStoreHarness()
	session, target, lease := mustJoinedTarget(t, store)

	mustAppend(t, store, session.ID, Event{
		Type:           EventTypeGateway,
		FromEndpointID: "gateway",
		IdempotencyKey: "gateway-switch-1",
		Payload: map[string]any{
			"action":   "switch",
			"next_url": "https://gw-next.example/rdev/",
		},
	})

	updated, err := store.Session(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SelectedGatewayURL != "https://gw-next.example/rdev" {
		t.Fatalf("selected gateway not updated by gateway event: %#v", updated)
	}

	_, renewed, _, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:   target.ID,
		LeaseSecret:  lease.Secret,
		AfterSeq:     0,
		ReceivedSeq:  updated.LastSeq,
		ProcessedSeq: updated.LastSeq,
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.SelectedGatewayURL != "https://gw-next.example/rdev" {
		t.Fatalf("renewed lease selected gateway = %q", renewed.SelectedGatewayURL)
	}
}

func TestEventsAfterAppliesEndpointVisibility(t *testing.T) {
	store, _ := newStoreHarness()
	session, target, lease := mustJoinedTarget(t, store)
	_, agent, agentLease, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleAgent,
		Platform:            "darwin/arm64",
		IdentityFingerprint: "agent-fp",
		Transport:           TransportLocal,
	})
	if err != nil {
		t.Fatalf("JoinSession(agent) error = %v", err)
	}

	hidden := mustAppend(t, store, session.ID, Event{
		Type:           EventTypeStatus,
		FromEndpointID: agent.ID,
		ToEndpointID:   "another-target",
		IdempotencyKey: "hidden",
	})
	visible := mustAppend(t, store, session.ID, Event{
		Type:           EventTypeTask,
		FromEndpointID: agent.ID,
		ToEndpointID:   target.ID,
		IdempotencyKey: "visible",
	})

	targetEvents, _, _, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:  target.ID,
		LeaseSecret: lease.Secret,
		AfterSeq:    hidden.Seq - 1,
		VisibleRole: EndpointRoleTarget,
	}, 10)
	if err != nil {
		t.Fatalf("target EventsAfter() error = %v", err)
	}
	if len(targetEvents) != 1 || targetEvents[0].ID != visible.ID {
		t.Fatalf("target should only see addressed/broadcast events, got %#v", targetEvents)
	}

	agentEvents, _, _, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:  agent.ID,
		LeaseSecret: agentLease.Secret,
		AfterSeq:    hidden.Seq - 1,
		VisibleRole: EndpointRoleAgent,
	}, 10)
	if err != nil {
		t.Fatalf("agent EventsAfter() error = %v", err)
	}
	if len(agentEvents) != 2 {
		t.Fatalf("agent should see all events, got %#v", agentEvents)
	}
}

func TestVisibilityFilteredSeqGapsDoNotBlockEndpointCursor(t *testing.T) {
	store, _ := newStoreHarness()
	session, target, lease := mustJoinedTarget(t, store)
	mustAppend(t, store, session.ID, Event{Type: EventTypeStatus, ToEndpointID: "other", IdempotencyKey: "hidden-gap"})
	visible := mustAppend(t, store, session.ID, Event{Type: EventTypeStatus, ToEndpointID: target.ID, IdempotencyKey: "visible-gap"})

	events, _, _, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:   target.ID,
		LeaseSecret:  lease.Secret,
		AfterSeq:     0,
		ReceivedSeq:  visible.Seq,
		ProcessedSeq: visible.Seq,
		VisibleRole:  EndpointRoleTarget,
	}, 10)
	if err != nil {
		t.Fatalf("EventsAfter() error = %v", err)
	}
	if events[len(events)-1].Seq != visible.Seq {
		t.Fatalf("visible event with seq gap was not returned: %#v", events)
	}

	snapshot, err := store.Session(session.ID)
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	got := endpointByID(snapshot.Endpoints, target.ID)
	if got.ProcessedSeq != visible.Seq {
		t.Fatalf("hidden events should not block cursor advancement: %#v", got)
	}
}

func TestEventsAfterOldSequenceRequiresSnapshot(t *testing.T) {
	store, _ := newStoreHarness()
	session, target, lease := mustJoinedTarget(t, store)
	if err := store.CompactEvents(session.ID, 5); err != nil {
		t.Fatalf("CompactEvents() error = %v", err)
	}

	_, _, replay, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:  target.ID,
		LeaseSecret: lease.Secret,
		AfterSeq:    1,
		VisibleRole: EndpointRoleTarget,
	}, 10)
	if !errors.Is(err, ProtocolError{Code: ErrSnapshotRequired}) || !replay.SnapshotRequired || replay.SnapshotSeq != 5 {
		t.Fatalf("EventsAfter old seq err=%v replay=%#v, want snapshot_required", err, replay)
	}
}

func TestAppendEventIsIdempotentForEndpointAndKey(t *testing.T) {
	store, _ := newStoreHarness()
	session, endpoint, _ := mustJoinedTarget(t, store)
	event := Event{
		Type:           EventTypeStatus,
		FromEndpointID: endpoint.ID,
		IdempotencyKey: "same-status",
		Payload:        map[string]any{"state": "online"},
	}

	first := mustAppend(t, store, session.ID, event)
	second := mustAppend(t, store, session.ID, event)
	if first.ID != second.ID || first.Seq != second.Seq {
		t.Fatalf("idempotent append allocated a new event: first=%#v second=%#v", first, second)
	}
}

func TestIdempotencySurvivesEventBodyCompaction(t *testing.T) {
	store, _ := newStoreHarness()
	session, endpoint, _ := mustJoinedTarget(t, store)
	event := Event{
		Type:           EventTypeStatus,
		FromEndpointID: endpoint.ID,
		IdempotencyKey: "before-compact",
		Payload:        map[string]any{"state": "online"},
	}
	first := mustAppend(t, store, session.ID, event)
	if err := store.CompactEvents(session.ID, first.Seq+1); err != nil {
		t.Fatalf("CompactEvents() error = %v", err)
	}

	second := mustAppend(t, store, session.ID, event)
	if second.ID != first.ID || second.Seq != first.Seq {
		t.Fatalf("compaction should not lose idempotency records: first=%#v second=%#v", first, second)
	}
}

func TestSubmitTaskAppendsTaskEvent(t *testing.T) {
	store, _ := newStoreHarness()
	session, target, _ := mustJoinedTarget(t, store)

	task, event, err := store.SubmitTask(session.ID, TaskSpec{
		Adapter:        "shell",
		Intent:         "run hostname",
		Capabilities:   []string{"shell"},
		IdempotencyKey: "task-1",
	})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if task.SchemaVersion != TaskSchemaVersion || task.TargetEndpointID != target.ID || task.Status != TaskStatusOffered {
		t.Fatalf("task not routed/offered correctly: %#v", task)
	}
	if event.Type != EventTypeTask || event.TaskID != task.ID || event.ToEndpointID != target.ID {
		t.Fatalf("SubmitTask should append addressed task event: %#v", event)
	}
}

func TestRepeatedTaskEventDoesNotCreateSecondAttempt(t *testing.T) {
	store, _ := newStoreHarness()
	session, _, _ := mustJoinedTarget(t, store)
	spec := TaskSpec{Adapter: "shell", Intent: "hostname", Capabilities: []string{"shell"}, IdempotencyKey: "same-task"}

	firstTask, firstEvent, err := store.SubmitTask(session.ID, spec)
	if err != nil {
		t.Fatalf("first SubmitTask() error = %v", err)
	}
	secondTask, secondEvent, err := store.SubmitTask(session.ID, spec)
	if err != nil {
		t.Fatalf("second SubmitTask() error = %v", err)
	}

	if firstTask.ID != secondTask.ID || firstTask.AttemptID != secondTask.AttemptID || firstEvent.Seq != secondEvent.Seq {
		t.Fatalf("repeated task idempotency created a second attempt: %#v %#v %#v %#v", firstTask, secondTask, firstEvent, secondEvent)
	}
}

func TestCancelTaskUsesTaskEventAndIsIdempotent(t *testing.T) {
	store, _ := newStoreHarness()
	session, _, _ := mustJoinedTarget(t, store)
	task, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "cancel-base"})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}

	firstTask, firstEvent, err := store.CancelTask(session.ID, task.ID, "user stopped", "cancel-key")
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	secondTask, secondEvent, err := store.CancelTask(session.ID, task.ID, "user stopped", "cancel-key")
	if err != nil {
		t.Fatalf("second CancelTask() error = %v", err)
	}

	if firstTask.Status != TaskStatusCanceled || firstEvent.Type != EventTypeTask || firstEvent.Payload["action"] != "cancel" {
		t.Fatalf("cancel should use task event with cancel payload: %#v %#v", firstTask, firstEvent)
	}
	if firstEvent.Seq != secondEvent.Seq || firstTask.ID != secondTask.ID {
		t.Fatalf("cancel should be idempotent: %#v %#v", firstEvent, secondEvent)
	}
}

func TestCompleteTaskAppendsResultEvent(t *testing.T) {
	store, _ := newStoreHarness()
	session, _, _ := mustJoinedTarget(t, store)
	task, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "complete-base"})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	task, err = store.MarkTaskRunning(session.ID, task.ID)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}

	completed, event, err := store.CompleteTask(session.ID, task.ID, map[string]any{
		"attempt_id":      task.AttemptID,
		"idempotency_key": "result-1",
		"status":          "succeeded",
		"summary":         "ok",
	})
	if err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if completed.Status != TaskStatusSucceeded || event.Type != EventTypeTaskResult || event.TaskID != task.ID {
		t.Fatalf("task result not recorded: %#v %#v", completed, event)
	}
}

func TestCompleteTaskIsIdempotentForAttemptAndKey(t *testing.T) {
	store, _ := newStoreHarness()
	session, _, _ := mustJoinedTarget(t, store)
	task, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "result-base"})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	task, err = store.MarkTaskRunning(session.ID, task.ID)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}
	result := map[string]any{"attempt_id": task.AttemptID, "idempotency_key": "same-result", "status": "succeeded"}

	firstTask, firstEvent, err := store.CompleteTask(session.ID, task.ID, result)
	if err != nil {
		t.Fatalf("first CompleteTask() error = %v", err)
	}
	secondTask, secondEvent, err := store.CompleteTask(session.ID, task.ID, result)
	if err != nil {
		t.Fatalf("second CompleteTask() error = %v", err)
	}
	if firstTask.ID != secondTask.ID || firstEvent.Seq != secondEvent.Seq {
		t.Fatalf("task result should be idempotent: %#v %#v %#v %#v", firstTask, secondTask, firstEvent, secondEvent)
	}
}

func TestUpsertArtifactResumesOffsetAndVerifiesHash(t *testing.T) {
	store, _ := newStoreHarness()
	session, _, _ := mustJoinedTarget(t, store)
	task, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "artifact-base"})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}

	ref, event, err := store.UpsertArtifact(session.ID, ArtifactRef{
		ID:           "art_1",
		TaskID:       task.ID,
		Kind:         "stdout",
		Name:         "stdout.txt",
		SizeBytes:    11,
		SHA256:       strings.Repeat("a", 64),
		ContentType:  "text/plain",
		UploadOffset: 5,
	})
	if err != nil {
		t.Fatalf("UpsertArtifact(partial) error = %v", err)
	}
	if ref.SchemaVersion != ArtifactRefSchemaVersion || event.Type != EventTypeArtifact || event.Payload["complete"] == true {
		t.Fatalf("partial artifact not recorded correctly: %#v %#v", ref, event)
	}

	ref, _, err = store.UpsertArtifact(session.ID, ArtifactRef{
		ID:           "art_1",
		TaskID:       task.ID,
		Kind:         "stdout",
		Name:         "stdout.txt",
		SizeBytes:    11,
		SHA256:       strings.Repeat("a", 64),
		ContentType:  "text/plain",
		UploadOffset: 11,
		Complete:     true,
	})
	if err != nil {
		t.Fatalf("UpsertArtifact(complete) error = %v", err)
	}
	if !ref.Complete || ref.UploadOffset != 11 {
		t.Fatalf("artifact resume/complete not recorded: %#v", ref)
	}

	_, _, err = store.UpsertArtifact(session.ID, ArtifactRef{ID: "art_1", TaskID: task.ID, UploadOffset: 10, SizeBytes: 11, SHA256: strings.Repeat("a", 64)})
	if !errors.Is(err, ProtocolError{Code: ErrArtifactOffsetMismatch}) {
		t.Fatalf("offset mismatch err = %v, want artifact_offset_mismatch", err)
	}
}

func TestPayloadAndBatchLimitsReturnStructuredErrors(t *testing.T) {
	store, _ := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{Limits: Limits{EventPayloadBytes: 24, EventBatch: 1}})

	_, err := store.AppendEvent(session.ID, Event{
		Type:           EventTypeStatus,
		FromEndpointID: "end_1",
		IdempotencyKey: "too-large",
		Payload:        map[string]any{"message": strings.Repeat("x", 100)},
	})
	if !errors.Is(err, ProtocolError{Code: ErrPayloadTooLarge}) {
		t.Fatalf("large payload err = %v, want payload_too_large", err)
	}

	_, err = store.AppendEventBatch(session.ID, []Event{
		{Type: EventTypeStatus, IdempotencyKey: "a"},
		{Type: EventTypeStatus, IdempotencyKey: "b"},
	})
	if !errors.Is(err, ProtocolError{Code: ErrTooManyEvents}) {
		t.Fatalf("large batch err = %v, want too_many_events", err)
	}
}

func TestAppendEventBatchAssignsContiguousServerSequences(t *testing.T) {
	store, _ := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{})

	events, err := store.AppendEventBatch(session.ID, []Event{
		{Type: EventTypeStatus, FromEndpointID: "end_1", IdempotencyKey: "batch-1"},
		{Type: EventTypeStatus, FromEndpointID: "end_1", IdempotencyKey: "batch-2"},
		{Type: EventTypeStatus, FromEndpointID: "end_1", IdempotencyKey: "batch-3"},
	})
	if err != nil {
		t.Fatalf("AppendEventBatch() error = %v", err)
	}
	if events[0].Seq+1 != events[1].Seq || events[1].Seq+1 != events[2].Seq {
		t.Fatalf("batch seq values should be contiguous: %#v", events)
	}
	for _, event := range events {
		if event.ID == "" || event.CreatedAt.IsZero() {
			t.Fatalf("gateway should assign event id and created_at: %#v", event)
		}
	}
}

func TestIdempotencyKeyWithDifferentPayloadReturnsConflict(t *testing.T) {
	store, _ := newStoreHarness()
	session, endpoint, _ := mustJoinedTarget(t, store)
	mustAppend(t, store, session.ID, Event{
		Type:           EventTypeStatus,
		FromEndpointID: endpoint.ID,
		IdempotencyKey: "conflict",
		Payload:        map[string]any{"state": "online"},
	})

	_, err := store.AppendEvent(session.ID, Event{
		Type:           EventTypeStatus,
		FromEndpointID: endpoint.ID,
		IdempotencyKey: "conflict",
		Payload:        map[string]any{"state": "offline"},
	})
	if !errors.Is(err, ProtocolError{Code: ErrIdempotencyConflict}) {
		t.Fatalf("idempotency conflict err = %v, want idempotency_conflict", err)
	}
}

func TestConflictingGatewayEventDoesNotChangeSelectedGateway(t *testing.T) {
	store, _ := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{SelectedGatewayURL: "https://initial.example"})
	first := Event{
		Type:           EventTypeGateway,
		FromEndpointID: "gateway.lifecycle",
		IdempotencyKey: "gateway-conflict",
		Payload:        map[string]any{"selected_url": "https://primary.example"},
	}
	if _, err := store.AppendEvent(session.ID, first); err != nil {
		t.Fatal(err)
	}
	conflict := first
	conflict.Payload = map[string]any{"selected_url": "https://attacker.example"}
	if _, err := store.AppendEvent(session.ID, conflict); !errors.Is(err, ProtocolError{Code: ErrIdempotencyConflict}) {
		t.Fatalf("gateway event conflict error = %v, want idempotency conflict", err)
	}
	current, err := store.Session(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.SelectedGatewayURL != "https://primary.example" {
		t.Fatalf("conflicting gateway event changed selected URL to %q", current.SelectedGatewayURL)
	}
}

func TestAppendEventAssignsServerOwnedUniqueEventIDs(t *testing.T) {
	store, _ := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{})
	first, err := store.AppendEvent(session.ID, Event{
		ID:             "evt_client_supplied",
		Type:           EventTypeStatus,
		FromEndpointID: "end_test",
		IdempotencyKey: "event-id-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AppendEvent(session.ID, Event{
		ID:             "evt_client_supplied",
		Type:           EventTypeStatus,
		FromEndpointID: "end_test",
		IdempotencyKey: "event-id-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "evt_client_supplied" || second.ID == "evt_client_supplied" || first.ID == second.ID {
		t.Fatalf("event IDs were not server-owned and unique: %q %q", first.ID, second.ID)
	}
}

func TestTaskRoutingUsesDefaultTargetAndCapabilityMatch(t *testing.T) {
	store, _ := newStoreHarness()
	session, target, _ := mustJoinedTarget(t, store)

	task, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "fs", Capabilities: []string{"fs"}, IdempotencyKey: "route-ok"})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if task.TargetEndpointID != target.ID {
		t.Fatalf("default route target = %q, want %q", task.TargetEndpointID, target.ID)
	}

	_, _, err = store.SubmitTask(session.ID, TaskSpec{Adapter: "desktop", Capabilities: []string{"desktop"}, IdempotencyKey: "route-miss"})
	if !errors.Is(err, ProtocolError{Code: ErrCapabilityUnavailable}) {
		t.Fatalf("capability miss err = %v, want capability_unavailable", err)
	}
}

func TestExplicitTaskTargetMustBeOnlineTargetEndpoint(t *testing.T) {
	store, _ := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{
		JoinPolicy:   "open",
		Capabilities: []string{"shell"},
	})
	_, agentEndpoint, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleAgent,
		Platform:            "agent/test",
		IdentityFingerprint: "fp-agent-endpoint",
		Capabilities:        []string{"shell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, targetEndpoint, targetLease, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-offline-target",
		Capabilities:        []string{"shell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:    targetEndpoint.ID,
		LeaseSecret:   targetLease.Secret,
		EndpointState: EndpointStateOffline,
	}, 10); err != nil {
		t.Fatal(err)
	}

	for _, endpointID := range []string{agentEndpoint.ID, targetEndpoint.ID} {
		_, _, err := store.SubmitTask(session.ID, TaskSpec{
			TargetEndpointID: endpointID,
			Adapter:          "shell",
			Capabilities:     []string{"shell"},
			IdempotencyKey:   "invalid-explicit-target-" + endpointID,
		})
		if !errors.Is(err, ProtocolError{Code: ErrEndpointNotFound}) {
			t.Fatalf("explicit non-target/offline endpoint %q was accepted: %v", endpointID, err)
		}
	}
}

func TestTargetEndpointCapabilitiesAreLimitedBySessionCeiling(t *testing.T) {
	store, _ := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{
		JoinPolicy:   "single-target",
		Capabilities: []string{"shell", "fs"},
	})
	_, endpoint, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-capability-ceiling",
		Capabilities:        []string{"shell", "desktop.admin"},
		Transport:           TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(endpoint.Capabilities, ",") != "shell" {
		t.Fatalf("endpoint capabilities escaped session ceiling: %#v", endpoint.Capabilities)
	}
	if _, _, err := store.SubmitTask(session.ID, TaskSpec{
		Adapter:        "desktop",
		Capabilities:   []string{"desktop.admin"},
		IdempotencyKey: "outside-ceiling",
	}); !errors.Is(err, ProtocolError{Code: ErrCapabilityUnavailable}) {
		t.Fatalf("task outside session ceiling was accepted: %v", err)
	}
	if _, _, err := store.SubmitTask(session.ID, TaskSpec{
		Adapter:        "shell",
		Capabilities:   []string{"shell"},
		IdempotencyKey: "inside-ceiling",
	}); err != nil {
		t.Fatalf("task inside session ceiling was rejected: %v", err)
	}

	_, resumed, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-capability-ceiling",
		Transport:           TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(resumed.Capabilities, ",") != "shell" {
		t.Fatalf("reconnect without inventory erased the authorized capabilities: %#v", resumed.Capabilities)
	}
}

func TestExpiredSessionCannotRenewLeaseJoinOrReceiveTasks(t *testing.T) {
	store, clock := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{
		JoinPolicy:   "single-target",
		Capabilities: []string{"shell"},
		ExpiresAt:    clock.now().Add(time.Minute),
	})
	_, endpoint, lease, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-expiring-target",
		Capabilities:        []string{"shell"},
		Transport:           TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := store.SubmitTask(session.ID, TaskSpec{
		Adapter:        "shell",
		Capabilities:   []string{"shell"},
		IdempotencyKey: "before-expiry",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Minute)

	assertExpired := func(name string, err error) {
		t.Helper()
		var protocolErr ProtocolError
		if !errors.As(err, &protocolErr) || protocolErr.Code != ErrTerminalSession || protocolErr.Recoverable {
			t.Fatalf("%s error = %#v, want permanent terminal_session", name, err)
		}
	}

	_, _, _, err = store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-expiring-target",
		Capabilities:        []string{"shell"},
	})
	assertExpired("join", err)
	assertExpired("validate lease", store.ValidateLease(session.ID, endpoint.ID, lease.Secret))
	_, renewed, _, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:  endpoint.ID,
		LeaseSecret: lease.Secret,
	}, 10)
	assertExpired("events", err)
	if renewed.Secret != "" {
		t.Fatal("expired session renewed its endpoint lease")
	}
	_, _, err = store.SubmitTask(session.ID, TaskSpec{
		Adapter:        "shell",
		Capabilities:   []string{"shell"},
		IdempotencyKey: "after-expiry",
	})
	assertExpired("submit task", err)
	_, _, err = store.CancelTask(session.ID, task.ID, "expired session", "after-expiry")
	assertExpired("cancel task", err)
	unchanged, err := store.Session(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.Tasks) != 1 || unchanged.Tasks[0].Status != TaskStatusCanceled {
		t.Fatalf("session expiry did not terminalize its outstanding task: %#v", unchanged.Tasks)
	}
	_, err = store.AppendEvent(session.ID, Event{
		Type:           EventTypeStatus,
		FromEndpointID: endpoint.ID,
		IdempotencyKey: "after-expiry",
	})
	assertExpired("append event", err)
	_, err = store.MarkTaskRunning(session.ID, task.ID)
	assertExpired("mark task running", err)
	_, _, err = store.CompleteTask(session.ID, task.ID, map[string]any{
		"attempt_id":      task.AttemptID,
		"idempotency_key": "after-expiry",
		"status":          "succeeded",
	})
	assertExpired("complete task", err)
	_, _, err = store.UpsertArtifact(session.ID, ArtifactRef{
		ID:           "art_after_expiry",
		TaskID:       task.ID,
		Kind:         "stdout",
		Name:         "stdout.txt",
		SizeBytes:    1,
		SHA256:       strings.Repeat("a", 64),
		UploadOffset: 1,
		Complete:     true,
	})
	assertExpired("upsert artifact", err)

	snapshot := store.Snapshot()
	if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Status != SessionStatusClosed {
		t.Fatalf("expired session was not persisted as terminal: %#v", snapshot.Sessions)
	}
	if len(snapshot.Sessions[0].Endpoints) != 1 || snapshot.Sessions[0].Endpoints[0].State != EndpointStateRevoked {
		t.Fatalf("expired endpoint was not revoked: %#v", snapshot.Sessions[0].Endpoints)
	}
	if len(snapshot.Leases) != 0 {
		t.Fatalf("expired session retained renewable lease state: %#v", snapshot.Leases)
	}
	events := snapshot.Events[session.ID]
	if len(events) == 0 || events[len(events)-1].Type != EventTypeClose || stringMapValue(events[len(events)-1].Payload, "reason") != "session expired" {
		t.Fatalf("expired session did not record a lifecycle close event: %#v", events)
	}
}

func TestRestoreSnapshotRejectsInvalidLeaseBindings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Snapshot, Session, Endpoint)
	}{
		{
			name: "lease session mismatch",
			mutate: func(snapshot *Snapshot, _ Session, endpoint Endpoint) {
				record := snapshot.Leases[endpoint.ID]
				record.Current.SessionID = "ses_other"
				snapshot.Leases[endpoint.ID] = record
			},
		},
		{
			name: "lease endpoint key mismatch",
			mutate: func(snapshot *Snapshot, _ Session, endpoint Endpoint) {
				record := snapshot.Leases[endpoint.ID]
				delete(snapshot.Leases, endpoint.ID)
				snapshot.Leases["end_other"] = record
			},
		},
		{
			name: "terminal session retains lease",
			mutate: func(snapshot *Snapshot, session Session, _ Endpoint) {
				for index := range snapshot.Sessions {
					if snapshot.Sessions[index].ID == session.ID {
						snapshot.Sessions[index].Status = SessionStatusRevoked
					}
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, clock := newStoreHarness()
			session := mustStoreSession(t, store, SessionSpec{
				JoinPolicy:   "single-target",
				Capabilities: []string{"shell"},
			})
			_, endpoint, _, err := store.JoinSession(session.ID, EndpointSpec{
				Role:                EndpointRoleTarget,
				Platform:            "windows/amd64",
				IdentityFingerprint: "fp-snapshot-lease",
				Capabilities:        []string{"shell"},
			})
			if err != nil {
				t.Fatal(err)
			}
			snapshot := store.Snapshot()
			tc.mutate(&snapshot, session, endpoint)
			restored := NewMemoryStore(clock.now)
			if err := restored.RestoreSnapshot(snapshot); err == nil {
				t.Fatal("invalid snapshot lease binding was accepted")
			}
		})
	}
}

func TestClosedSessionSnapshotRoundTripDropsEndpointLeases(t *testing.T) {
	store, clock := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{JoinPolicy: "single-target"})
	_, endpoint, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-closed-snapshot",
		Capabilities:        []string{"shell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	if len(snapshot.Leases) != 0 || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Endpoints[0].State != EndpointStateRevoked {
		t.Fatalf("closed snapshot retained live endpoint authorization: %#v %#v", snapshot.Sessions, snapshot.Leases)
	}
	restored := NewMemoryStore(clock.now)
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := restored.ValidateLease(session.ID, endpoint.ID, "stale-secret"); !errors.Is(err, ProtocolError{Code: ErrTerminalSession}) {
		t.Fatalf("restored closed session accepted an endpoint lease: %v", err)
	}
}

func TestLifecycleEventConflictDoesNotPartiallyTerminateSession(t *testing.T) {
	for _, action := range []struct {
		name      string
		keyPrefix string
		run       func(*MemoryStore, string) error
	}{
		{
			name:      "close",
			keyPrefix: "close:",
			run: func(store *MemoryStore, sessionID string) error {
				_, _, err := store.CloseSession(sessionID)
				return err
			},
		},
		{
			name:      "revoke",
			keyPrefix: "revoke:",
			run: func(store *MemoryStore, sessionID string) error {
				_, _, err := store.RevokeSession(sessionID)
				return err
			},
		},
	} {
		t.Run(action.name, func(t *testing.T) {
			store, _ := newStoreHarness()
			session := mustStoreSession(t, store, SessionSpec{JoinPolicy: "single-target"})
			_, endpoint, lease, err := store.JoinSession(session.ID, EndpointSpec{
				Role:                EndpointRoleTarget,
				Platform:            "windows/amd64",
				IdentityFingerprint: "fp-lifecycle-conflict",
				Capabilities:        []string{"shell"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.AppendEvent(session.ID, Event{
				Type:           EventTypeStatus,
				FromEndpointID: "gateway.lifecycle",
				IdempotencyKey: action.keyPrefix + session.ID,
				Payload:        map[string]any{"poisoned": true},
			}); err != nil {
				t.Fatal(err)
			}

			err = action.run(store, session.ID)
			if !errors.Is(err, ProtocolError{Code: ErrIdempotencyConflict}) {
				t.Fatalf("lifecycle action error = %v, want idempotency conflict", err)
			}
			current, err := store.Session(session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if sessionTerminal(current.Status) || len(current.Endpoints) != 1 || current.Endpoints[0].State != EndpointStateOnline {
				t.Fatalf("failed lifecycle action partially terminalized session: %#v", current)
			}
			if err := store.ValidateLease(session.ID, endpoint.ID, lease.Secret); err != nil {
				t.Fatalf("failed lifecycle action invalidated endpoint lease: %v", err)
			}
			if _, exists := store.Snapshot().TerminalAt[session.ID]; exists {
				t.Fatal("failed lifecycle action wrote terminal timestamp")
			}
		})
	}
}

func TestTaskEventConflictDoesNotPartiallyMutateSessionState(t *testing.T) {
	t.Run("submit", func(t *testing.T) {
		store, _ := newStoreHarness()
		session, _, _ := mustJoinedTarget(t, store)
		if _, err := store.AppendEvent(session.ID, Event{
			Type:           EventTypeStatus,
			FromEndpointID: "agent",
			IdempotencyKey: "task:conflicting-submit",
			Payload:        map[string]any{"poisoned": true},
		}); err != nil {
			t.Fatal(err)
		}
		_, _, err := store.SubmitTask(session.ID, TaskSpec{
			Adapter:        "shell",
			Capabilities:   []string{"shell"},
			IdempotencyKey: "conflicting-submit",
		})
		if !errors.Is(err, ProtocolError{Code: ErrIdempotencyConflict}) {
			t.Fatalf("submit error = %v, want idempotency conflict", err)
		}
		current, err := store.Session(session.ID)
		if err != nil || len(current.Tasks) != 0 {
			t.Fatalf("failed submit partially appended task: %#v %v", current.Tasks, err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		store, _ := newStoreHarness()
		session, _, _ := mustJoinedTarget(t, store)
		task, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "cancel-base-atomic"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AppendEvent(session.ID, Event{
			Type:           EventTypeStatus,
			FromEndpointID: "agent",
			IdempotencyKey: "cancel:" + task.ID + ":conflicting-cancel",
			Payload:        map[string]any{"poisoned": true},
		}); err != nil {
			t.Fatal(err)
		}
		_, _, err = store.CancelTask(session.ID, task.ID, "operator canceled", "conflicting-cancel")
		if !errors.Is(err, ProtocolError{Code: ErrIdempotencyConflict}) {
			t.Fatalf("cancel error = %v, want idempotency conflict", err)
		}
		current, err := store.Session(session.ID)
		if err != nil || len(current.Tasks) != 1 || current.Tasks[0].Status != TaskStatusOffered {
			t.Fatalf("failed cancel partially mutated task: %#v %v", current.Tasks, err)
		}
	})

	t.Run("complete", func(t *testing.T) {
		store, _ := newStoreHarness()
		session, endpoint, _ := mustJoinedTarget(t, store)
		task, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "complete-base-atomic"})
		if err != nil {
			t.Fatal(err)
		}
		resultKey := "conflicting-result"
		if _, err := store.AppendEvent(session.ID, Event{
			Type:           EventTypeStatus,
			FromEndpointID: endpoint.ID,
			IdempotencyKey: "result:" + task.ID + ":" + task.AttemptID + ":" + resultKey,
			Payload:        map[string]any{"poisoned": true},
		}); err != nil {
			t.Fatal(err)
		}
		_, _, err = store.CompleteTask(session.ID, task.ID, map[string]any{
			"attempt_id":      task.AttemptID,
			"idempotency_key": resultKey,
			"status":          "succeeded",
		})
		if !errors.Is(err, ProtocolError{Code: ErrIdempotencyConflict}) {
			t.Fatalf("complete error = %v, want idempotency conflict", err)
		}
		current, err := store.Session(session.ID)
		if err != nil || len(current.Tasks) != 1 || current.Tasks[0].Status != TaskStatusOffered {
			t.Fatalf("failed completion partially mutated task: %#v %v", current.Tasks, err)
		}
	})

	t.Run("artifact", func(t *testing.T) {
		store, _ := newStoreHarness()
		session, _, _ := mustJoinedTarget(t, store)
		if _, err := store.AppendEvent(session.ID, Event{
			Type:           EventTypeStatus,
			FromEndpointID: "artifact",
			IdempotencyKey: "artifact:art_atomic:1:true",
			Payload:        map[string]any{"poisoned": true},
		}); err != nil {
			t.Fatal(err)
		}
		_, _, err := store.UpsertArtifact(session.ID, ArtifactRef{
			ID:           "art_atomic",
			Kind:         "stdout",
			Name:         "stdout.txt",
			SizeBytes:    1,
			SHA256:       strings.Repeat("a", 64),
			UploadOffset: 1,
			Complete:     true,
		})
		if !errors.Is(err, ProtocolError{Code: ErrIdempotencyConflict}) {
			t.Fatalf("artifact error = %v, want idempotency conflict", err)
		}
		current, err := store.Session(session.ID)
		if err != nil || len(current.Artifacts) != 0 {
			t.Fatalf("failed artifact upsert partially mutated session: %#v %v", current.Artifacts, err)
		}
	})
}

func TestRestoreSnapshotConstrainsLegacyTargetCapabilities(t *testing.T) {
	store, clock := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{
		JoinPolicy:   "single-target",
		Capabilities: []string{"shell"},
	})
	_, endpoint, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-legacy-capabilities",
		Capabilities:        []string{"shell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	for sessionIndex := range snapshot.Sessions {
		for endpointIndex := range snapshot.Sessions[sessionIndex].Endpoints {
			if snapshot.Sessions[sessionIndex].Endpoints[endpointIndex].ID == endpoint.ID {
				snapshot.Sessions[sessionIndex].Endpoints[endpointIndex].Capabilities = []string{"shell", "desktop.admin"}
			}
		}
	}

	restored := NewMemoryStore(clock.now)
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	restoredSession, err := restored.Session(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredSession.Endpoints) != 1 || strings.Join(restoredSession.Endpoints[0].Capabilities, ",") != "shell" {
		t.Fatalf("legacy target capabilities escaped restored ceiling: %#v", restoredSession.Endpoints)
	}
	if _, _, err := restored.SubmitTask(session.ID, TaskSpec{
		Adapter:        "desktop",
		Capabilities:   []string{"desktop.admin"},
		IdempotencyKey: "restored-outside-ceiling",
	}); !errors.Is(err, ProtocolError{Code: ErrCapabilityUnavailable}) {
		t.Fatalf("restored legacy endpoint accepted task outside ceiling: %v", err)
	}
}

func TestRestoreSnapshotRejectsLegacyTaskOutsideCapabilityCeiling(t *testing.T) {
	store, clock := newStoreHarness()
	session := mustStoreSession(t, store, SessionSpec{
		JoinPolicy:   "single-target",
		Capabilities: []string{"shell"},
	})
	_, _, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-legacy-task-capabilities",
		Capabilities:        []string{"shell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := store.SubmitTask(session.ID, TaskSpec{
		Adapter:        "shell",
		Capabilities:   []string{"shell"},
		IdempotencyKey: "legacy-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	for sessionIndex := range snapshot.Sessions {
		for taskIndex := range snapshot.Sessions[sessionIndex].Tasks {
			if snapshot.Sessions[sessionIndex].Tasks[taskIndex].ID == task.ID {
				snapshot.Sessions[sessionIndex].Tasks[taskIndex].Capabilities = []string{"desktop.admin"}
			}
		}
	}

	restored := NewMemoryStore(clock.now)
	if err := restored.RestoreSnapshot(snapshot); err == nil {
		t.Fatal("snapshot with live task outside restored capability ceiling was accepted")
	}
}

func TestTerminalSessionRejectsNewTasksButAcceptsFinalResultDuringGrace(t *testing.T) {
	store, clock := newStoreHarness()
	session, _, _ := mustJoinedTarget(t, store)
	task, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "terminal-base"})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	task, err = store.MarkTaskRunning(session.ID, task.ID)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}
	if _, _, err := store.CloseSession(session.ID); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if _, _, err := store.SubmitTask(session.ID, TaskSpec{Adapter: "shell", Capabilities: []string{"shell"}, IdempotencyKey: "after-close"}); !errors.Is(err, ProtocolError{Code: ErrTerminalSession}) {
		t.Fatalf("new task after close err = %v, want terminal_session", err)
	}

	if _, _, err := store.CompleteTask(session.ID, task.ID, map[string]any{
		"attempt_id":      task.AttemptID,
		"idempotency_key": "final-result",
		"status":          "succeeded",
	}); err != nil {
		t.Fatalf("final result during terminal grace should be accepted: %v", err)
	}
	clock.advance(time.Minute)
	_, _, err = store.CompleteTask(session.ID, task.ID, map[string]any{
		"attempt_id":      task.AttemptID,
		"idempotency_key": "late-result",
		"status":          "succeeded",
	})
	if !errors.Is(err, ProtocolError{Code: ErrTerminalSession}) {
		t.Fatalf("late final result err = %v, want terminal_session", err)
	}
}

func TestExpiredLeaseEntersReconnectGraceBeforeFailure(t *testing.T) {
	store, clock := newStoreHarness()
	session, target, lease := mustJoinedTarget(t, store)
	clock.advance(70 * time.Second)

	_, renewed, replay, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:  target.ID,
		LeaseSecret: lease.Secret,
		AfterSeq:    0,
		VisibleRole: EndpointRoleTarget,
	}, 10)
	if err != nil {
		t.Fatalf("expired lease inside reconnect grace should renew on polling: %v", err)
	}
	if renewed.Secret == lease.Secret {
		t.Fatalf("reconnect grace renewal should rotate lease secret: %#v", renewed)
	}
	if !replay.Reconnecting {
		t.Fatalf("replay state should explain reconnect grace: %#v", replay)
	}

	snapshot, err := store.Session(session.ID)
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if endpointByID(snapshot.Endpoints, target.ID).State != EndpointStateOnline {
		t.Fatalf("successful grace poll should bring endpoint online: %#v", snapshot.Endpoints)
	}
}

func TestPreviousLeaseSecretCanOnlyResumeSameEndpointDuringGrace(t *testing.T) {
	store, clock := newStoreHarness()
	session, target, lease := mustJoinedTarget(t, store)
	_, other, _, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleAgent,
		Platform:            "darwin/arm64",
		IdentityFingerprint: "agent-fp",
	})
	if err != nil {
		t.Fatalf("JoinSession(agent) error = %v", err)
	}
	clock.advance(70 * time.Second)

	if _, _, _, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:  other.ID,
		LeaseSecret: lease.Secret,
		AfterSeq:    0,
		VisibleRole: EndpointRoleAgent,
	}, 10); !errors.Is(err, ProtocolError{Code: ErrUnauthorizedEndpoint}) {
		t.Fatalf("expired target lease used by other endpoint err = %v, want unauthorized_endpoint", err)
	}

	if _, _, _, err := store.EventsAfter(session.ID, EventCursor{
		EndpointID:  target.ID,
		LeaseSecret: lease.Secret,
		AfterSeq:    0,
		VisibleRole: EndpointRoleTarget,
	}, 10); err != nil {
		t.Fatalf("same endpoint should be allowed to poll during reconnect grace: %v", err)
	}
}

type storeClock struct {
	current time.Time
}

func newStoreHarness() (*MemoryStore, *storeClock) {
	clock := &storeClock{current: time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)}
	return NewMemoryStore(clock.now), clock
}

func (c *storeClock) now() time.Time {
	return c.current
}

func (c *storeClock) advance(delta time.Duration) {
	c.current = c.current.Add(delta)
}

func mustStoreSession(t *testing.T, store *MemoryStore, spec SessionSpec) Session {
	t.Helper()
	if spec.Profile == "" {
		spec.Profile = "attended-temporary"
	}
	if spec.AuthorityID == "" {
		spec.AuthorityID = "auth-main"
	}
	if spec.SelectedGatewayURL == "" {
		spec.SelectedGatewayURL = "https://gw.example"
	}
	if spec.ReconnectGraceMS == 0 {
		spec.ReconnectGraceMS = 120_000
	}
	if spec.RetryAfterMS == 0 {
		spec.RetryAfterMS = 500
	}
	if spec.Limits.TerminalGraceMillis == 0 {
		spec.Limits.TerminalGraceMillis = 30_000
	}

	session, err := store.CreateSession(spec)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	return session
}

func mustJoinedTarget(t *testing.T, store *MemoryStore) (Session, Endpoint, Lease) {
	t.Helper()
	session := mustStoreSession(t, store, SessionSpec{JoinPolicy: "single-target"})
	joined, endpoint, lease, err := store.JoinSession(session.ID, EndpointSpec{
		Role:                EndpointRoleTarget,
		Name:                "winbox",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-winbox",
		Capabilities:        []string{"shell", "fs"},
		Transport:           TransportLongPoll,
	})
	if err != nil {
		t.Fatalf("JoinSession() error = %v", err)
	}
	return joined, endpoint, lease
}

func mustAppend(t *testing.T, store *MemoryStore, sessionID string, event Event) Event {
	t.Helper()
	appended, err := store.AppendEvent(sessionID, event)
	if err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	return appended
}

func endpointByID(endpoints []Endpoint, endpointID string) Endpoint {
	for _, endpoint := range endpoints {
		if endpoint.ID == endpointID {
			return endpoint
		}
	}
	return Endpoint{}
}
