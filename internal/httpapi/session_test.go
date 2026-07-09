package httpapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func TestHTTPSessionCreateJoinEventsAndSnapshot(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()

	createRec := postJSON(t, handler, "/v1/sessions", `{
		"profile":"attended-temporary",
		"reason":"repair Windows host",
		"join_policy":"single-target",
		"authority_id":"auth-main",
		"selected_gateway_url":"https://gw.example",
		"reconnect_grace_ms":120000
	}`, "")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create session status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Session controlplane.Session       `json:"session"`
		Status  controlplane.StatusSummary `json:"status"`
	}
	decodeHTTP(t, createRec, &created)
	if created.Session.SchemaVersion != controlplane.SessionSchemaVersion || created.Session.JoinCode == "" {
		t.Fatalf("create response missing v1 session: %#v", created)
	}

	joinRec := postJSON(t, handler, "/v1/session-joins", `{
		"join_code":"`+created.Session.JoinCode+`",
		"endpoint":{
			"role":"target",
			"name":"winbox",
			"platform":"windows/amd64",
			"identity_fingerprint":"fp-winbox",
			"capabilities":["shell","fs"],
			"transport":"long-poll"
		}
	}`, "")
	if joinRec.Code != http.StatusOK {
		t.Fatalf("join status = %d body=%s", joinRec.Code, joinRec.Body.String())
	}
	var joined struct {
		Session  controlplane.Session  `json:"session"`
		Endpoint controlplane.Endpoint `json:"endpoint"`
		Lease    controlplane.Lease    `json:"lease"`
		Events   []controlplane.Event  `json:"events"`
	}
	decodeHTTP(t, joinRec, &joined)
	if joined.Session.ID != created.Session.ID || joined.Endpoint.ID == "" || joined.Lease.Secret == "" {
		t.Fatalf("join response missing session endpoint lease: %#v", joined)
	}
	if len(joined.Events) == 0 || joined.Events[0].Type != controlplane.EventTypeHello {
		t.Fatalf("join should return initial event batch: %#v", joined.Events)
	}

	appendRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/events", `{
		"type":"status",
		"from_endpoint_id":"`+joined.Endpoint.ID+`",
		"idempotency_key":"status-1",
		"payload":{"state":"online"}
	}`, joined.Lease.Secret)
	if appendRec.Code != http.StatusAccepted {
		t.Fatalf("append event status = %d body=%s", appendRec.Code, appendRec.Body.String())
	}
	var appended struct {
		Event controlplane.Event `json:"event"`
	}
	decodeHTTP(t, appendRec, &appended)
	if appended.Event.Seq == 0 || appended.Event.CreatedAt.IsZero() {
		t.Fatalf("gateway should assign event seq/time: %#v", appended.Event)
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/events?endpoint_id="+url.QueryEscape(joined.Endpoint.ID)+"&after_seq=0&limit=10", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+joined.Lease.Secret)
	eventsRec := httptest.NewRecorder()
	handler.ServeHTTP(eventsRec, eventsReq)
	if eventsRec.Code != http.StatusOK {
		t.Fatalf("events status = %d body=%s", eventsRec.Code, eventsRec.Body.String())
	}
	var replay struct {
		Events           []controlplane.Event `json:"events"`
		Lease            controlplane.Lease   `json:"lease"`
		SnapshotRequired bool                 `json:"snapshot_required"`
		LastSeq          uint64               `json:"last_seq"`
		RetryAfterMS     int                  `json:"retry_after_ms"`
	}
	decodeHTTP(t, eventsRec, &replay)
	if len(replay.Events) < 2 || replay.Lease.Secret == "" || replay.LastSeq != appended.Event.Seq {
		t.Fatalf("events response missing replay/lease/hints: %#v", replay)
	}

	snapshotReq := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+url.PathEscape(created.Session.ID), nil)
	snapshotRec := httptest.NewRecorder()
	handler.ServeHTTP(snapshotRec, snapshotReq)
	if snapshotRec.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d body=%s", snapshotRec.Code, snapshotRec.Body.String())
	}
	if strings.Contains(snapshotRec.Body.String(), joined.Lease.Secret) {
		t.Fatalf("session snapshot leaked lease secret: %s", snapshotRec.Body.String())
	}
	var snapshot struct {
		Snapshot controlplane.SessionSnapshot `json:"snapshot"`
	}
	decodeHTTP(t, snapshotRec, &snapshot)
	if snapshot.Snapshot.Session.ID != created.Session.ID || snapshot.Snapshot.Limits.EventBatch == 0 {
		t.Fatalf("snapshot missing session recovery fields: %#v", snapshot.Snapshot)
	}
}

func TestHTTPSessionLeaseSecretAuthAndStructuredErrors(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	joined := joinHTTPSession(t, handler, created.Session.JoinCode)

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/events?endpoint_id="+url.QueryEscape(joined.Endpoint.ID), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized events status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Error controlplane.ProtocolError `json:"error"`
	}
	decodeHTTP(t, rec, &payload)
	if payload.Error.SchemaVersion != controlplane.ErrorSchemaVersion || payload.Error.Code != controlplane.ErrUnauthorizedEndpoint || payload.Error.AgentNextAction == "" {
		t.Fatalf("unauthorized error should be structured rdev.error.v1: %#v", payload.Error)
	}

	first := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/events", `{
		"type":"status",
		"from_endpoint_id":"`+joined.Endpoint.ID+`",
		"idempotency_key":"same-key",
		"payload":{"state":"online"}
	}`, joined.Lease.Secret)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first append status = %d body=%s", first.Code, first.Body.String())
	}
	conflict := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/events", `{
		"type":"status",
		"from_endpoint_id":"`+joined.Endpoint.ID+`",
		"idempotency_key":"same-key",
		"payload":{"state":"offline"}
	}`, joined.Lease.Secret)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body=%s", conflict.Code, conflict.Body.String())
	}
	decodeHTTP(t, conflict, &payload)
	if payload.Error.Code != controlplane.ErrIdempotencyConflict || !payload.Error.Recoverable {
		t.Fatalf("conflict should be recoverable structured error: %#v", payload.Error)
	}
}

func TestHTTPSessionTaskResultArtifactAndTerminalBehavior(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	joined := joinHTTPSession(t, handler, created.Session.JoinCode)

	taskRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks", `{
		"adapter":"shell",
		"intent":"hostname",
		"capabilities":["shell"],
		"idempotency_key":"task-1"
	}`, "")
	if taskRec.Code != http.StatusAccepted {
		t.Fatalf("task status = %d body=%s", taskRec.Code, taskRec.Body.String())
	}
	var taskPayload struct {
		Task  controlplane.Task  `json:"task"`
		Event controlplane.Event `json:"event"`
	}
	decodeHTTP(t, taskRec, &taskPayload)
	if taskPayload.Task.TargetEndpointID != joined.Endpoint.ID || taskPayload.Event.Type != controlplane.EventTypeTask {
		t.Fatalf("task response missing routed task event: %#v", taskPayload)
	}

	artifactRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/artifacts", `{
		"id":"art_1",
		"task_id":"`+taskPayload.Task.ID+`",
		"kind":"stdout",
		"name":"stdout.txt",
		"size_bytes":5,
		"sha256":"`+strings.Repeat("a", 64)+`",
		"content_type":"text/plain",
		"upload_offset":5,
		"complete":true
	}`, joined.Lease.Secret)
	if artifactRec.Code != http.StatusAccepted {
		t.Fatalf("artifact status = %d body=%s", artifactRec.Code, artifactRec.Body.String())
	}

	resultRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/"+url.PathEscape(taskPayload.Task.ID)+"/result", `{
		"attempt_id":"`+taskPayload.Task.AttemptID+`",
		"idempotency_key":"result-1",
		"status":"succeeded",
		"summary":"ok"
	}`, joined.Lease.Secret)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("result status = %d body=%s", resultRec.Code, resultRec.Body.String())
	}
	var resultPayload struct {
		Task  controlplane.Task  `json:"task"`
		Event controlplane.Event `json:"event"`
	}
	decodeHTTP(t, resultRec, &resultPayload)
	if resultPayload.Task.Status != controlplane.TaskStatusSucceeded || resultPayload.Event.Type != controlplane.EventTypeTaskResult {
		t.Fatalf("result response missing terminal task/result event: %#v", resultPayload)
	}

	closeRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/close", `{}`, "")
	if closeRec.Code != http.StatusAccepted {
		t.Fatalf("close status = %d body=%s", closeRec.Code, closeRec.Body.String())
	}
	lateTask := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks", `{
		"adapter":"shell",
		"capabilities":["shell"],
		"idempotency_key":"late-task"
	}`, "")
	if lateTask.Code != http.StatusConflict {
		t.Fatalf("late task status = %d body=%s", lateTask.Code, lateTask.Body.String())
	}
	var errorPayload struct {
		Error controlplane.ProtocolError `json:"error"`
	}
	decodeHTTP(t, lateTask, &errorPayload)
	if errorPayload.Error.Code != controlplane.ErrTerminalSession {
		t.Fatalf("late task should return terminal_session: %#v", errorPayload.Error)
	}
}

func TestHTTPSessionEventsPersistsRenewedLeaseForGatewayRestart(t *testing.T) {
	publicKey, privateKey := httpGatewayKeyPair(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	stateStore, err := gateway.NewFileStateStore(filepath.Join(t.TempDir(), "gateway", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	handler := NewServerWithStateStore(gw, stateStore).Handler()
	created := createHTTPSession(t, handler)
	joined := joinHTTPSession(t, handler, created.Session.JoinCode)

	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/events?endpoint_id="+url.QueryEscape(joined.Endpoint.ID)+"&after_seq=0&received_seq=1&processed_seq=1", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+joined.Lease.Secret)
	eventsRec := httptest.NewRecorder()
	handler.ServeHTTP(eventsRec, eventsReq)
	if eventsRec.Code != http.StatusOK {
		t.Fatalf("events status = %d body=%s", eventsRec.Code, eventsRec.Body.String())
	}
	var replay struct {
		Lease controlplane.Lease `json:"lease"`
	}
	decodeHTTP(t, eventsRec, &replay)
	if replay.Lease.Secret == "" || replay.Lease.Secret == joined.Lease.Secret || replay.Lease.Generation <= joined.Lease.Generation {
		t.Fatalf("events should return a renewed lease: joined=%#v replay=%#v", joined.Lease, replay.Lease)
	}

	restarted := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	if _, ok, err := stateStore.LoadInto(restarted); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("expected persisted gateway state")
	}
	restartedHandler := NewServerWithStateStore(restarted, stateStore).Handler()
	resumeReq := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/events?endpoint_id="+url.QueryEscape(joined.Endpoint.ID)+"&after_seq=1", nil)
	resumeReq.Header.Set("Authorization", "Bearer "+replay.Lease.Secret)
	resumeRec := httptest.NewRecorder()
	restartedHandler.ServeHTTP(resumeRec, resumeReq)
	if resumeRec.Code != http.StatusOK {
		t.Fatalf("renewed lease should survive gateway restart, status = %d body=%s", resumeRec.Code, resumeRec.Body.String())
	}
}

func TestHTTPOldHostJobExperimentalRoutesAreNotMounted(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/v1/hosts"},
		{method: http.MethodGet, path: "/v1/hosts/hst_old"},
		{method: http.MethodPost, path: "/v1/hosts/register", body: `{"ticket_code":"OLD-1234","name":"old","os":"windows","arch":"amd64"}`},
		{method: http.MethodPost, path: "/v1/hosts/hst_old/authorize", body: `{"capabilities":["shell"]}`},
		{method: http.MethodPost, path: "/v1/hosts/hst_old/revoke", body: `{"reason":"old contract"}`},
		{method: http.MethodPost, path: "/v1/hosts/hst_old/heartbeat", body: `{}`},
		{method: http.MethodGet, path: "/v1/hosts/hst_old/jobs/next"},
		{method: http.MethodPost, path: "/v1/jobs/job_old/authorize", body: `{"authorization_id":"screen.screenshot","decision":"authorized"}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func postJSON(t *testing.T, handler http.Handler, path, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeHTTP(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func createHTTPSession(t *testing.T, handler http.Handler) struct {
	Session controlplane.Session       `json:"session"`
	Status  controlplane.StatusSummary `json:"status"`
} {
	t.Helper()
	rec := postJSON(t, handler, "/v1/sessions", `{"profile":"attended-temporary","reason":"test","join_policy":"single-target","reconnect_grace_ms":120000}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Session controlplane.Session       `json:"session"`
		Status  controlplane.StatusSummary `json:"status"`
	}
	decodeHTTP(t, rec, &payload)
	return payload
}

func joinHTTPSession(t *testing.T, handler http.Handler, joinCode string) struct {
	Session  controlplane.Session  `json:"session"`
	Endpoint controlplane.Endpoint `json:"endpoint"`
	Lease    controlplane.Lease    `json:"lease"`
	Events   []controlplane.Event  `json:"events"`
} {
	t.Helper()
	rec := postJSON(t, handler, "/v1/session-joins", `{
		"join_code":"`+joinCode+`",
		"endpoint":{"role":"target","platform":"windows/amd64","identity_fingerprint":"fp-winbox","capabilities":["shell","fs"],"transport":"long-poll"}
	}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("join session status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Session  controlplane.Session  `json:"session"`
		Endpoint controlplane.Endpoint `json:"endpoint"`
		Lease    controlplane.Lease    `json:"lease"`
		Events   []controlplane.Event  `json:"events"`
	}
	decodeHTTP(t, rec, &payload)
	return payload
}

func httpGatewayKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}
