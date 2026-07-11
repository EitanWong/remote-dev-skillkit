package hostcmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHostServeSessionCompletesTask(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Reason:       "rdev-host session task smoke",
		Capabilities: []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := New(&stdout, &stderr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() {
		done <- app.runServe(ctx, serveOptions{
			Mode:          "temporary",
			GatewayURL:    server.URL,
			TicketCode:    session.JoinCode,
			Transport:     "long-poll",
			PollInterval:  time.Millisecond,
			MaxTasks:      1,
			IdentityKeyID: hostidentity.DefaultKeyID,
		})
	}()

	waitForSessionEndpoint(t, gw, session.ID)
	task, _, err := gw.SubmitSessionTask(session.ID, controlplane.TaskSpec{
		TargetSelector: controlplane.TargetSelector{Role: controlplane.EndpointRoleTarget},
		Adapter:        "shell",
		Intent:         "session shell demo",
		Capabilities:   []string{"shell.user"},
		Payload: map[string]any{
			"workspace_root": ".",
			"argv":           []any{"go", "env", "GOOS"},
			"allow_commands": []any{"go"},
		},
		IdempotencyKey: "hostcmd-session-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for session task")
	}
	snapshot, err := gw.Session(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var completed controlplane.Task
	for _, candidate := range snapshot.Tasks {
		if candidate.ID == task.ID {
			completed = candidate
			break
		}
	}
	if completed.Status != controlplane.TaskStatusSucceeded {
		t.Fatalf("expected session task succeeded, got %#v\nstdout=%s\nstderr=%s", completed, stdout.String(), stderr.String())
	}
	events, _, err := gw.SessionEventsAfterForAgent(session.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundResult := false
	for _, event := range events {
		if event.Type == controlplane.EventTypeTaskResult && event.TaskID == task.ID {
			foundResult = true
			if !strings.Contains(fmt.Sprint(event.Payload["artifact_content"]), `"exit_code": 0`) {
				t.Fatalf("expected shell evidence in task result, got %#v", event.Payload)
			}
		}
	}
	if !foundResult {
		t.Fatalf("expected task.result event, got %#v", events)
	}
}

func TestJoinSessionRetriesTransientEOFWithIdempotencyKey(t *testing.T) {
	attempts := 0
	var firstKey string
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/v1/session-joins" {
			t.Fatalf("expected session join path, got %s", req.URL.Path)
		}
		key := strings.TrimSpace(req.Header.Get("Idempotency-Key"))
		if key == "" {
			t.Fatalf("expected idempotency key on attempt %d", attempts)
		}
		if attempts == 1 {
			firstKey = key
		} else if key != firstKey {
			t.Fatalf("expected stable idempotency key, got %q then %q", firstKey, key)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"join_code":"TICKET-1"`) ||
			!strings.Contains(string(body), `"role":"target"`) {
			t.Fatalf("unexpected session join body: %s", string(body))
		}
		if attempts == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"session":{"schema_version":"rdev.session.v1","id":"ses_1","join_code":"TICKET-1","status":"waiting","created_at":"2026-07-08T00:00:00Z","updated_at":"2026-07-08T00:00:00Z"},"endpoint":{"schema_version":"rdev.endpoint.v1","id":"ep_1","session_id":"ses_1","role":"target","name":"win-temp","platform":"windows/amd64","identity_fingerprint":"fp_1","state":"online","transport":"long-poll","capabilities":["shell.user"],"lease_expires_at":"2026-07-08T00:01:00Z","last_seen_at":"2026-07-08T00:00:00Z","joined_at":"2026-07-08T00:00:00Z"},"lease":{"schema_version":"rdev.lease.v1","endpoint_id":"ep_1","secret":"lease_1","issued_at":"2026-07-08T00:00:00Z","expires_at":"2026-07-08T00:01:00Z","ttl_ms":60000,"renew_after_ms":20000,"retry_after_ms":1000},"events":[]}`)),
			Request:    req,
		}, nil
	})
	client := &http.Client{Transport: retryingRoundTripper{Base: base, MaxRetries: 2}}

	session, endpoint, lease, _, err := joinSessionByCode(context.Background(), client, "https://gateway.example.test", "TICKET-1", controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "win-temp",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp_1",
		Capabilities:        []string{"shell.user"},
		Transport:           controlplane.TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
	if session.ID != "ses_1" || endpoint.ID != "ep_1" || lease.Secret != "lease_1" {
		t.Fatalf("unexpected session join result session=%#v endpoint=%#v lease=%#v", session, endpoint, lease)
	}
}

func TestJoinSessionResponseErrorExitCodeRequiresCompletePermanentProtocolEnvelope(t *testing.T) {
	completePermanent := `{"error":{"schema_version":"rdev.error.v1","code":"invalid_join_code","message":"join code is invalid","recoverable":false,"retry_after_ms":0,"user_summary":"The support-session entry is invalid or no longer active.","agent_next_action":"create a fresh support-session entry"}}`
	completeRecoverable := `{"error":{"schema_version":"rdev.error.v1","code":"gateway_unavailable","message":"gateway is unavailable","recoverable":true,"retry_after_ms":500,"user_summary":"The gateway is temporarily unavailable.","agent_next_action":"retry after the requested delay"}}`

	tests := []struct {
		name       string
		statusCode int
		status     string
		body       string
		cause      error
		want       int
	}{
		{name: "complete permanent protocol error", statusCode: http.StatusNotFound, status: "404 Not Found", body: completePermanent, want: PermanentJoinFailureExitCode},
		{name: "recoverable protocol error", statusCode: http.StatusServiceUnavailable, status: "503 Service Unavailable", body: completeRecoverable, want: 1},
		{name: "missing recoverable field", statusCode: http.StatusNotFound, status: "404 Not Found", body: `{"error":{"schema_version":"rdev.error.v1","code":"invalid_join_code","message":"join code is invalid","retry_after_ms":0,"user_summary":"invalid entry","agent_next_action":"create a fresh entry"}}`, want: 1},
		{name: "missing required summary", statusCode: http.StatusNotFound, status: "404 Not Found", body: `{"error":{"schema_version":"rdev.error.v1","code":"invalid_join_code","message":"join code is invalid","recoverable":false,"retry_after_ms":0,"agent_next_action":"create a fresh entry"}}`, want: 1},
		{name: "wrong schema", statusCode: http.StatusNotFound, status: "404 Not Found", body: `{"error":{"schema_version":"other.error.v1","code":"invalid_join_code","message":"join code is invalid","recoverable":false,"retry_after_ms":0,"user_summary":"invalid entry","agent_next_action":"create a fresh entry"}}`, want: 1},
		{name: "malformed json", statusCode: http.StatusNotFound, status: "404 Not Found", body: `{"error":`, want: 1},
		{name: "legacy string error", statusCode: http.StatusNotFound, status: "404 Not Found", body: `{"error":"join code is invalid"}`, want: 1},
		{name: "protocol body with success status", statusCode: http.StatusOK, status: "200 OK", body: completePermanent, want: 1},
		{name: "network error", cause: io.ErrUnexpectedEOF, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewJoinSessionResponseError(tc.statusCode, tc.status, []byte(tc.body), tc.cause)
			if got := ExitCode(err); got != tc.want {
				t.Fatalf("ExitCode() = %d, want %d for %v", got, tc.want, err)
			}
		})
	}
}

func TestJoinSessionByCodeReturnsPermanentFailureForProtocolRejection(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"schema_version":"rdev.error.v1","code":"invalid_join_code","message":"join code is invalid","recoverable":false,"retry_after_ms":0,"user_summary":"The support-session entry is invalid or no longer active.","agent_next_action":"create a fresh support-session entry"}}`,
			)),
			Request: req,
		}, nil
	})}

	_, _, _, _, err := joinSessionByCode(context.Background(), client, "https://gateway.example.test", "TEST-CODE", controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "test-target",
		Platform:            "windows/amd64",
		IdentityFingerprint: "test-fingerprint",
		Transport:           controlplane.TransportLongPoll,
	})
	if err == nil {
		t.Fatal("expected join rejection")
	}
	if got := ExitCode(err); got != PermanentJoinFailureExitCode {
		t.Fatalf("ExitCode() = %d, want %d for %v", got, PermanentJoinFailureExitCode, err)
	}
}

func TestCompleteSessionTaskRetriesTransientEOFWithIdempotencyKey(t *testing.T) {
	attempts := 0
	var firstKey string
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/v1/sessions/ses_1/tasks/task_1/result" {
			t.Fatalf("expected task result path, got %s", req.URL.Path)
		}
		key := strings.TrimSpace(req.Header.Get("Idempotency-Key"))
		if key == "" {
			t.Fatalf("expected idempotency key on attempt %d", attempts)
		}
		if attempts == 1 {
			firstKey = key
		} else if key != firstKey {
			t.Fatalf("expected stable idempotency key, got %q then %q", firstKey, key)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"artifact_content":"task result"`) ||
			!strings.Contains(string(body), `"status":"succeeded"`) {
			t.Fatalf("unexpected task result body: %s", string(body))
		}
		if attempts == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"task":{"schema_version":"rdev.task.v1","id":"task_1","session_id":"ses_1","target_endpoint_id":"ep_1","adapter":"shell","intent":"demo","capabilities":["shell.user"],"attempt_id":"attempt_1","idempotency_key":"task_1","status":"succeeded","created_at":"2026-07-08T00:00:00Z","ended_at":"2026-07-08T00:00:01Z"},"event":{"schema_version":"rdev.event.v1","id":"evt_1","seq":2,"session_id":"ses_1","type":"task.result","task_id":"task_1","idempotency_key":"result_1","created_at":"2026-07-08T00:00:01Z"}}`)),
			Request:    req,
		}, nil
	})
	client := &http.Client{Transport: retryingRoundTripper{Base: base, MaxRetries: 2}}

	task, event, err := completeSessionTask(context.Background(), client, "https://gateway.example.test", "ses_1", "task_1", "lease_1", map[string]any{
		"status":           "succeeded",
		"attempt_id":       "attempt_1",
		"idempotency_key":  "result_1",
		"artifact_content": "task result",
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
	if task.ID != "task_1" || task.Status != controlplane.TaskStatusSucceeded || event.Type != controlplane.EventTypeTaskResult {
		t.Fatalf("unexpected task result task=%#v event=%#v", task, event)
	}
}

func TestFetchJoinManifestUsesGatewayDateForClockSkew(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuedAt := time.Now().UTC().Add(-2 * time.Hour)
	ticket, err := model.NewTicket(model.HostModeAttendedTemporary, 60, []string{"shell.user"}, "repair", issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := model.NewJoinManifest(ticket, model.JoinManifestSpec{
		GatewayURL:   "https://gateway.example.test",
		Trust:        model.NewTrustBundle("manifest-root", publicKey),
		SigningKeyID: "manifest-root",
	}, issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = manifest.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := model.NewGatewayTimeProof(model.GatewayTimeProofPurposeJoinManifest, manifest, "manifest-root", privateKey, issuedAt.Add(30*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"manifest": manifest, "gateway_time_proof": proof})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    req,
		}
		resp.Header.Set("Date", issuedAt.Add(30*time.Second).Format(http.TimeFormat))
		return resp, nil
	})}

	got, err := fetchJoinManifest(context.Background(), client, "https://gateway.example.test/v1/tickets/"+ticket.Code+"/manifest", "", "manifest-root:"+manifest.Trust.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.TicketCode != ticket.Code {
		t.Fatalf("unexpected manifest ticket code %q", got.TicketCode)
	}
}

func waitForSessionEndpoint(t *testing.T, gw *gateway.MemoryGateway, sessionID string) controlplane.Endpoint {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		session, err := gw.Session(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		for _, endpoint := range session.Endpoints {
			if endpoint.Role == controlplane.EndpointRoleTarget && endpoint.State == controlplane.EndpointStateOnline {
				return endpoint
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session endpoint %s", sessionID)
	return controlplane.Endpoint{}
}
