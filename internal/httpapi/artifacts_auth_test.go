package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

// Artifact writes are endpoint-scoped or operator-scoped:
//   - endpoint_id + Bearer lease  → endpoint writes artifacts for tasks it owns
//   - no endpoint_id              → operator path (agent-side MCP proxy)
// See issue #19.

func artifactBody(taskID string) string {
	return `{
		"id":"art_1",
		"task_id":"` + taskID + `",
		"kind":"stdout",
		"name":"stdout.txt",
		"size_bytes":5,
		"sha256":"` + strings.Repeat("a", 64) + `",
		"content_type":"text/plain",
		"upload_offset":5,
		"complete":true
	}`
}

func TestHTTPArtifactWriteRequiresEndpointLease(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	joined := joinHTTPSession(t, handler, created.Session.JoinCode)
	base := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/artifacts"

	cases := []struct {
		name   string
		path   string
		bearer string
		want   int
		code   controlplane.ErrorCode
	}{
		{"endpoint but no bearer", base + "?endpoint_id=" + url.QueryEscape(joined.Endpoint.ID), "", http.StatusUnauthorized, controlplane.ErrUnauthorizedEndpoint},
		{"unknown endpoint with bearer", base + "?endpoint_id=ep_unknown", joined.Lease.Secret, http.StatusNotFound, controlplane.ErrEndpointNotFound},
		{"wrong bearer", base + "?endpoint_id=" + url.QueryEscape(joined.Endpoint.ID), "not-the-lease", http.StatusUnauthorized, controlplane.ErrUnauthorizedEndpoint},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(t, handler, tc.path, artifactBody(""), tc.bearer)
			if rec.Code != tc.want {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
			var payload struct {
				Error controlplane.ProtocolError `json:"error"`
			}
			decodeHTTP(t, rec, &payload)
			if payload.Error.Code != tc.code {
				t.Fatalf("error code = %q, want %q", payload.Error.Code, tc.code)
			}
		})
	}
}

func TestHTTPArtifactWriteOperatorPathRequiresOperatorRole(t *testing.T) {
	handler := NewServerWithOperatorAuth(gateway.NewMemoryGateway(), "", httpTestOperatorAuth(t)).Handler()
	created := createHTTPSessionWithBearer(t, handler, "operator-secret")
	base := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/artifacts"

	// No endpoint_id and no operator token → forbidden.
	rec := postJSON(t, handler, base, artifactBody(""), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
	// Auditor role is not enough.
	rec = postJSON(t, handler, base, artifactBody(""), "auditor-secret")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("auditor status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
	// Operator token succeeds (this is the agent-side MCP proxy path).
	rec = postJSON(t, handler, base, artifactBody(""), "operator-secret")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("operator status = %d body=%s, want 202", rec.Code, rec.Body.String())
	}
}

func TestHTTPArtifactWriteRejectsUnknownTask(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	joined := joinHTTPSession(t, handler, created.Session.JoinCode)
	base := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/artifacts?endpoint_id=" + url.QueryEscape(joined.Endpoint.ID)

	rec := postJSON(t, handler, base, artifactBody("task_nope"), joined.Lease.Secret)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
	var payload struct {
		Error controlplane.ProtocolError `json:"error"`
	}
	decodeHTTP(t, rec, &payload)
	if payload.Error.Code != controlplane.ErrTaskNotFound {
		t.Fatalf("error code = %q, want task not found", payload.Error.Code)
	}
}

func TestHTTPArtifactWriteRejectsTaskOwnedByOtherEndpoint(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createMultiTargetHTTPSession(t, handler)
	endpointA := joinHTTPSession(t, handler, created.Session.JoinCode)
	// Identity-based join dedupes by fingerprint, so use a distinct one.
	endpointB := joinHTTPSessionWithFingerprint(t, handler, created.Session.JoinCode, "fp-winbox-2")

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
		Task controlplane.Task `json:"task"`
	}
	decodeHTTP(t, taskRec, &taskPayload)
	taskOwner := taskPayload.Task.TargetEndpointID
	var otherEndpoint struct {
		ID    string
		Lease controlplane.Lease
	}
	if taskOwner == endpointA.Endpoint.ID {
		otherEndpoint.ID = endpointB.Endpoint.ID
		otherEndpoint.Lease = endpointB.Lease
	} else {
		otherEndpoint.ID = endpointA.Endpoint.ID
		otherEndpoint.Lease = endpointA.Lease
	}
	if taskOwner != endpointA.Endpoint.ID && taskOwner != endpointB.Endpoint.ID {
		t.Fatalf("task routed to unexpected endpoint %q", taskOwner)
	}

	// The non-owning endpoint must be rejected even with a valid lease.
	base := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/artifacts?endpoint_id=" + url.QueryEscape(otherEndpoint.ID)
	rec := postJSON(t, handler, base, artifactBody(taskPayload.Task.ID), otherEndpoint.Lease.Secret)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}

	// The owning endpoint succeeds.
	ownerBase := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/artifacts?endpoint_id=" + url.QueryEscape(taskOwner)
	ownerLease := endpointA.Lease.Secret
	if taskOwner == endpointB.Endpoint.ID {
		ownerLease = endpointB.Lease.Secret
	}
	ownerRec := postJSON(t, handler, ownerBase, artifactBody(taskPayload.Task.ID), ownerLease)
	if ownerRec.Code != http.StatusAccepted {
		t.Fatalf("owner status = %d body=%s, want 202", ownerRec.Code, ownerRec.Body.String())
	}
}

func TestHTTPArtifactWriteAllowsSessionScopedArtifact(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	joined := joinHTTPSession(t, handler, created.Session.JoinCode)
	base := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/artifacts?endpoint_id=" + url.QueryEscape(joined.Endpoint.ID)

	rec := postJSON(t, handler, base, artifactBody(""), joined.Lease.Secret)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", rec.Code, rec.Body.String())
	}
}

func joinHTTPSessionWithFingerprint(t *testing.T, handler http.Handler, joinCode, fingerprint string) struct {
	Session  controlplane.Session  `json:"session"`
	Endpoint controlplane.Endpoint `json:"endpoint"`
	Lease    controlplane.Lease    `json:"lease"`
	Events   []controlplane.Event  `json:"events"`
} {
	t.Helper()
	rec := postJSON(t, handler, "/v1/session-joins", `{
		"join_code":"`+joinCode+`",
		"endpoint":{"role":"target","platform":"windows/amd64","identity_fingerprint":"`+fingerprint+`","capabilities":["shell","fs"],"transport":"long-poll"}
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

func createHTTPSessionWithBearer(t *testing.T, handler http.Handler, bearer string) struct {
	Session controlplane.Session       `json:"session"`
	Status  controlplane.StatusSummary `json:"status"`
} {
	t.Helper()
	rec := postJSON(t, handler, "/v1/sessions", `{"profile":"attended-temporary","reason":"test","join_policy":"single-target","reconnect_grace_ms":120000}`, bearer)
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

func createMultiTargetHTTPSession(t *testing.T, handler http.Handler) struct {
	Session controlplane.Session       `json:"session"`
	Status  controlplane.StatusSummary `json:"status"`
} {
	t.Helper()
	rec := postJSON(t, handler, "/v1/sessions", `{"profile":"attended-temporary","reason":"test","join_policy":"multi-target","reconnect_grace_ms":120000}`, "")
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
