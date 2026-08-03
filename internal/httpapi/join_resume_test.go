package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

const engineeringTaskBody = `{
	"adapter":"shell",
	"capabilities":["shell.user"],
	"idempotency_key":"join-resume-task",
	"engineering_task":{
		"schema_version":"rdev.engineering-task.v1",
		"goal":"Exercise resume.",
		"workspace":{"root":"/tmp/resume-repo","base_sha":"0123456789abcdef0123456789abcdef01234567","isolation":"git-worktree","dirty_policy":"preserve","read_scope":["."],"write_scope":["internal"]},
		"plan":["Inspect."],
		"acceptance":["Tests pass."],
		"verification":{"commands":[["go","test","./internal/contracts"]],"allow_commands":["go"]},
		"limits":{"max_duration_seconds":600,"max_output_bytes":65536,"max_attempts":2},
		"network_policy":"default-deny",
		"required_capabilities":["shell.user"],
		"idempotency_key":"join-resume-task"
	}
}`

// joinWithCapabilities joins a session with the given endpoint capabilities
// (as a JSON array literal) under a fixed per-call fingerprint.
func joinWithCapabilities(t *testing.T, handler http.Handler, joinCode, fingerprint, capabilities string) struct {
	Session  controlplane.Session  `json:"session"`
	Endpoint controlplane.Endpoint `json:"endpoint"`
	Lease    controlplane.Lease    `json:"lease"`
} {
	t.Helper()
	rec := postJSON(t, handler, "/v1/session-joins", `{
		"join_code":"`+joinCode+`",
		"endpoint":{"role":"target","platform":"linux/amd64","identity_fingerprint":"`+fingerprint+`","capabilities":`+capabilities+`,"transport":"long-poll"}
	}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("join session status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Session  controlplane.Session  `json:"session"`
		Endpoint controlplane.Endpoint `json:"endpoint"`
		Lease    controlplane.Lease    `json:"lease"`
	}
	decodeHTTP(t, rec, &payload)
	return payload
}

func TestHTTPJoinSessionBySessionID(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)

	rec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/join", `{
		"role":"target",
		"platform":"linux/amd64",
		"identity_fingerprint":"fp-join-session",
		"capabilities":["shell","fs"],
		"transport":"long-poll"
	}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("join by session id status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Session  controlplane.Session  `json:"session"`
		Endpoint controlplane.Endpoint `json:"endpoint"`
		Lease    controlplane.Lease    `json:"lease"`
	}
	decodeHTTP(t, rec, &payload)
	if payload.Endpoint.ID == "" || payload.Lease.Secret == "" {
		t.Fatalf("join must return endpoint and lease: %#v", payload)
	}
	if payload.Session.ID != created.Session.ID {
		t.Fatalf("joined wrong session: %q", payload.Session.ID)
	}
}

func TestHTTPJoinSessionRejectsMalformedBody(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)

	rec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/join", `{not json`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed join status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPJoinSessionRejectsUnknownSession(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()

	rec := postJSON(t, handler, "/v1/sessions/ses_unknown/join", `{
		"role":"target",
		"platform":"linux/amd64",
		"identity_fingerprint":"fp-unknown",
		"capabilities":["shell"],
		"transport":"long-poll"
	}`, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session join status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPResumeSessionTaskHappyPath(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	joined := joinWithCapabilities(t, handler, created.Session.JoinCode, "fp-resume", `["shell.user"]`)

	taskRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks", engineeringTaskBody, "")
	if taskRec.Code != http.StatusAccepted {
		t.Fatalf("task submit status = %d body=%s", taskRec.Code, taskRec.Body.String())
	}
	var taskPayload struct {
		Task controlplane.Task `json:"task"`
	}
	decodeHTTP(t, taskRec, &taskPayload)

	resultRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/"+url.PathEscape(taskPayload.Task.ID)+"/result?endpoint_id="+url.QueryEscape(joined.Endpoint.ID), `{
		"attempt_id":"`+taskPayload.Task.AttemptID+`",
		"idempotency_key":"result-1",
		"status":"succeeded",
		"summary":"ok"
	}`, joined.Lease.Secret)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("result status = %d body=%s", resultRec.Code, resultRec.Body.String())
	}

	resumeRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/"+url.PathEscape(taskPayload.Task.ID)+"/resume", `{
		"checkpoint_id":"cp-1",
		"idempotency_key":"resume-1"
	}`, "")
	if resumeRec.Code != http.StatusAccepted {
		t.Fatalf("resume status = %d body=%s", resumeRec.Code, resumeRec.Body.String())
	}
	var resumePayload struct {
		Task controlplane.Task `json:"task"`
	}
	decodeHTTP(t, resumeRec, &resumePayload)
	if resumePayload.Task.ID == taskPayload.Task.ID {
		t.Fatal("resume must create a fresh task")
	}
	if resumePayload.Task.Payload["engineering_resume_checkpoint_id"] != "cp-1" {
		t.Fatalf("resumed task missing checkpoint marker: %#v", resumePayload.Task.Payload)
	}
	if resumePayload.Task.Payload["engineering_resume_task_id"] != taskPayload.Task.ID {
		t.Fatalf("resumed task missing source task marker: %#v", resumePayload.Task.Payload)
	}
}

func TestHTTPResumeSessionTaskRejectsMissingCheckpoint(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	joined := joinWithCapabilities(t, handler, created.Session.JoinCode, "fp-resume-2", `["shell.user"]`)

	taskRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks", engineeringTaskBody, "")
	if taskRec.Code != http.StatusAccepted {
		t.Fatalf("task submit status = %d body=%s", taskRec.Code, taskRec.Body.String())
	}
	var taskPayload struct {
		Task controlplane.Task `json:"task"`
	}
	decodeHTTP(t, taskRec, &taskPayload)
	resultRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/"+url.PathEscape(taskPayload.Task.ID)+"/result?endpoint_id="+url.QueryEscape(joined.Endpoint.ID), `{
		"attempt_id":"`+taskPayload.Task.AttemptID+`",
		"idempotency_key":"result-1",
		"status":"succeeded",
		"summary":"ok"
	}`, joined.Lease.Secret)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("result status = %d body=%s", resultRec.Code, resultRec.Body.String())
	}

	resumeRec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/"+url.PathEscape(taskPayload.Task.ID)+"/resume", `{
		"idempotency_key":"resume-1"
	}`, "")
	if resumeRec.Code != http.StatusBadRequest {
		t.Fatalf("missing checkpoint status = %d body=%s, want 400", resumeRec.Code, resumeRec.Body.String())
	}
}

func TestHTTPResumeSessionTaskRejectsUnknownTask(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)

	rec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/task_unknown/resume", `{
		"checkpoint_id":"cp-1",
		"idempotency_key":"resume-1"
	}`, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task resume status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func TestHTTPResumeSessionTaskRequiresOperatorRole(t *testing.T) {
	handler := NewServerWithOperatorAuth(gateway.NewMemoryGateway(), "", httpTestOperatorAuth(t)).Handler()
	created := createHTTPSessionWithBearer(t, handler, "operator-secret")

	rec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/task_any/resume", `{
		"checkpoint_id":"cp-1",
		"idempotency_key":"resume-1"
	}`, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("resume without operator status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
	rec = postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/task_any/resume", `{
		"checkpoint_id":"cp-1",
		"idempotency_key":"resume-1"
	}`, "auditor-secret")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("resume with auditor status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
}

func TestHTTPResumeSessionTaskRejectsMalformedBody(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)

	rec := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(created.Session.ID)+"/tasks/task_any/resume", `{not json`, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid JSON") {
		t.Fatalf("malformed resume status = %d body=%s", rec.Code, rec.Body.String())
	}
}
