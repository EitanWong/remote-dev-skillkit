package hostcmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
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
			Body:       io.NopCloser(strings.NewReader(`{"session":{"schema_version":"rdev.session.v1","id":"ses_1","join_code":"TICKET-1","status":"waiting","created_at":"2026-07-08T00:00:00Z","updated_at":"2026-07-08T00:00:00Z"},"endpoint":{"schema_version":"rdev.endpoint.v1","id":"ep_1","session_id":"ses_1","role":"target","name":"win-temp","platform":"windows/amd64","identity_fingerprint":"fp_1","state":"online","transport":"long-poll","capabilities":["shell.user"],"lease_expires_at":"2026-07-08T00:01:00Z","last_seen_at":"2026-07-08T00:00:00Z","joined_at":"2026-07-08T00:00:00Z"},"lease":{"schema_version":"rdev.lease.v1","session_id":"ses_1","endpoint_id":"ep_1","generation":1,"secret":"lease_1","issued_at":"2026-07-08T00:00:00Z","expires_at":"2026-07-08T00:01:00Z","ttl_ms":60000,"renew_after_ms":20000,"retry_after_ms":1000},"events":[]}`)),
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
		{name: "server error is never permanent", statusCode: http.StatusInternalServerError, status: "500 Internal Server Error", body: completePermanent, want: 1},
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

func TestSignedManifestCapabilityCeilingConstrainsInventoryAndTasks(t *testing.T) {
	got := ConstrainCapabilities(
		[]string{"shell.user", "desktop.admin", "shell.user"},
		[]string{"shell.user", "fs.read.scoped"},
		true,
	)
	if strings.Join(got, ",") != "shell.user" {
		t.Fatalf("constrained capabilities = %#v, want shell.user", got)
	}
	if CapabilitiesAllowed([]string{"shell.user"}, []string{"shell.user", "fs.read.scoped"}, true) != true {
		t.Fatal("task inside signed capability ceiling was rejected")
	}
	if CapabilitiesAllowed([]string{"desktop.admin"}, []string{"shell.user", "fs.read.scoped"}, true) {
		t.Fatal("task outside signed capability ceiling was accepted")
	}
	if got := ConstrainCapabilities([]string{"shell.user"}, nil, true); len(got) != 0 {
		t.Fatalf("signed empty capability ceiling must deny all capabilities: %#v", got)
	}
	if !CapabilitiesAllowed([]string{"desktop.admin"}, nil, false) {
		t.Fatal("local direct mode unexpectedly enforced a missing signed ceiling")
	}
}

func TestRegistrationCapabilitiesAdvertisesWindowsDesktopSupportOnlyWhenManifestGrantsIt(t *testing.T) {
	detected := RegistrationCapabilities(hostcap.Inventory{
		OS:                    "windows",
		TemporaryCapabilities: []string{"shell.user", "fs.read"},
	})
	got := ConstrainCapabilities(detected, []string{"shell.user", "window.inspect", "screen.screenshot"}, true)
	if strings.Join(got, ",") != "shell.user,window.inspect,screen.screenshot" {
		t.Fatalf("registered capabilities = %#v", got)
	}
	withoutDesktopGrant := ConstrainCapabilities(detected, []string{"shell.user"}, true)
	if strings.Join(withoutDesktopGrant, ",") != "shell.user" {
		t.Fatalf("manifest ceiling did not restrict desktop capabilities: %#v", withoutDesktopGrant)
	}
}

func TestRegistrationCapabilitiesOmitsDesktopSupportOnNonWindows(t *testing.T) {
	for _, osName := range []string{"darwin", "linux"} {
		t.Run(osName, func(t *testing.T) {
			got := RegistrationCapabilities(hostcap.Inventory{
				OS: osName,
				TemporaryCapabilities: []string{
					"shell.user",
					"gui.view",
					"screen.screenshot",
					"window.inspect",
				},
			})
			for _, capability := range got {
				if strings.HasPrefix(capability, "gui.") || strings.HasPrefix(capability, "screen.") || strings.HasPrefix(capability, "window.") || strings.HasPrefix(capability, "input.") || strings.HasPrefix(capability, "app.") || strings.HasPrefix(capability, "clipboard.") || capability == "url.open" {
					t.Fatalf("non-Windows %s advertised unsupported desktop capability %q: %#v", osName, capability, got)
				}
			}
		})
	}
}

func TestRunSessionTaskRejectsCapabilityOutsideSignedManifestBeforeAdapter(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	resultPayload := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		resultPayload <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task":{},"event":{}}`)
	}))
	defer server.Close()

	app := New(io.Discard, io.Discard)
	err := app.runSessionTask(context.Background(), serveOptions{
		GatewayURL:           server.URL,
		CapabilityCeiling:    []string{"fs.read.scoped"},
		CapabilityCeilingSet: true,
	}, server.Client(), "ses_test", "end_test", "fp-test", "lease-test", controlplane.Task{
		ID:               "task_test",
		AttemptID:        "attempt_test",
		TargetEndpointID: "end_test",
		Adapter:          "shell",
		Capabilities:     []string{"shell.user"},
		Payload: map[string]any{
			"workspace_root": filepath.Dir(marker),
			"argv":           []any{"sh", "-c", "touch " + marker},
			"allow_commands": []any{"sh"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("task adapter ran outside signed capability ceiling: %v", err)
	}
	payload := <-resultPayload
	if payload["status"] != string(controlplane.TaskStatusFailed) || !strings.Contains(fmt.Sprint(payload["reason"]), "signed join manifest ceiling") {
		t.Fatalf("capability denial was not reported as a failed task: %#v", payload)
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
		if key != "result_1" {
			t.Fatalf("expected payload idempotency key, got %q", key)
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

func TestRunSessionTasksSelectsHealthyRouteAfterConcurrentInitialProbe(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust := model.NewTrustBundle("test", publicKey)
	trustBody, err := json.Marshal(map[string]any{"trust": trust})
	if err != nil {
		t.Fatal(err)
	}
	var deadEvents, healthyEvents int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "dead.example.test" {
			if strings.HasSuffix(req.URL.Path, "/events") {
				deadEvents++
				return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("No tunnel found")), Request: req}, nil
			}
			if req.URL.Path == "/v1/trust" {
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(trustBody)), Request: req}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found")), Request: req}, nil
		}
		if req.URL.Host != "healthy.example.test" {
			return nil, fmt.Errorf("unexpected candidate host %q", req.URL.Host)
		}
		if req.URL.Path == "/v1/trust-bundle" {
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("legacy gateway")), Request: req}, nil
		}
		if req.URL.Path == "/v1/trust" {
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(trustBody)), Request: req}, nil
		}
		if strings.HasSuffix(req.URL.Path, "/events") {
			healthyEvents++
			body := fmt.Sprintf(`{"events":[],"lease":{"session_id":"ses_test","endpoint_id":"end_test","generation":%d,"secret":"lease-test-%d"},"last_seq":0}`, healthyEvents, healthyEvents)
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found")), Request: req}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	app := New(io.Discard, io.Discard)
	processed, err := app.runSessionTasks(ctx, serveOptions{
		GatewayURL:                "https://dead.example.test",
		ManifestGatewayCandidates: []model.JoinManifestGatewayCandidate{{URL: "https://healthy.example.test"}},
		PollInterval:              time.Millisecond,
		MaxTasks:                  1,
	}, client, "ses_test", "end_test", "fp-test", "lease-test", controlplane.Lease{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runSessionTasks error = %v, want context deadline after continued polling", err)
	}
	if processed != 0 || healthyEvents == 0 {
		t.Fatalf("expected healthy route selection and continued polling, processed=%d dead_events=%d healthy_events=%d", processed, deadEvents, healthyEvents)
	}
}

func TestRunSessionTasksSwitchesRouteWithoutReregistering(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trustBody, err := json.Marshal(map[string]any{"trust": model.NewTrustBundle("route-test", publicKey)})
	if err != nil {
		t.Fatal(err)
	}
	registrationCalls := 0
	primaryEventCalls := 0
	secondaryEventCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}
		if req.URL.Path == "/v1/session-joins" {
			registrationCalls++
			return response(http.StatusOK, `{"session":{"schema_version":"rdev.session.v1","id":"ses_route","join_code":"TEST-CODE","status":"active"},"endpoint":{"schema_version":"rdev.endpoint.v1","id":"ep_route","session_id":"ses_route","role":"target","state":"online"},"lease":{"schema_version":"rdev.lease.v1","session_id":"ses_route","endpoint_id":"ep_route","generation":1,"secret":"lease_one"},"events":[]}`)
		}
		if req.URL.Path == "/v1/trust-bundle" {
			return response(http.StatusNotFound, `{"error":"legacy fixture"}`)
		}
		if req.URL.Path == "/v1/trust" {
			return response(http.StatusOK, string(trustBody))
		}
		if strings.HasSuffix(req.URL.Path, "/events") {
			if req.URL.Query().Get("endpoint_id") != "ep_route" {
				t.Fatalf("event endpoint_id = %q, want original endpoint", req.URL.Query().Get("endpoint_id"))
			}
			switch req.URL.Host {
			case "primary.example.test":
				primaryEventCalls++
				if primaryEventCalls == 1 {
					if req.URL.Query().Get("after_seq") != "0" || req.Header.Get("Authorization") != "Bearer lease_one" {
						t.Fatalf("initial event request lost cursor or lease binding: query=%v auth=%q", req.URL.Query(), req.Header.Get("Authorization"))
					}
					return response(http.StatusOK, `{"events":[],"lease":{"session_id":"ses_route","endpoint_id":"ep_route","generation":2,"secret":"lease_two","retry_after_ms":1},"last_seq":7}`)
				}
				return nil, io.ErrUnexpectedEOF
			case "secondary.example.test":
				secondaryEventCalls++
				if req.URL.Path != "/v1/sessions/ses_route/events" || req.URL.Query().Get("after_seq") != "7" || req.Header.Get("Authorization") != "Bearer lease_two" {
					t.Fatalf("route switch changed session continuity: path=%q query=%v auth=%q", req.URL.Path, req.URL.Query(), req.Header.Get("Authorization"))
				}
				return response(http.StatusOK, `{"events":[{"seq":8,"type":"task","task_id":"task_route","payload":{"action":"offer"}}],"lease":{"session_id":"ses_route","endpoint_id":"ep_route","generation":3,"secret":"lease_three"},"last_seq":8}`)
			default:
				return nil, fmt.Errorf("unexpected event route %q", req.URL.Host)
			}
		}
		if req.Method == http.MethodGet && req.URL.Path == "/v1/sessions/ses_route" {
			if req.URL.Host != "secondary.example.test" {
				t.Fatalf("task fetch route = %q, want secondary", req.URL.Host)
			}
			return response(http.StatusOK, `{"snapshot":{"tasks":[{"schema_version":"rdev.task.v1","id":"task_route","session_id":"ses_route","target_endpoint_id":"ep_route","attempt_id":"attempt_route","adapter":"shell","intent":"fixture","capabilities":["shell.user"],"status":"offered"}]}}`)
		}
		if req.Method == http.MethodPost && req.URL.Path == "/v1/sessions/ses_route/tasks/task_route/result" {
			if req.URL.Host != "secondary.example.test" || req.Header.Get("Authorization") != "Bearer lease_three" || req.Header.Get("Idempotency-Key") == "" {
				t.Fatalf("task result lost route, lease, or idempotency binding: host=%q auth=%q key=%q", req.URL.Host, req.Header.Get("Authorization"), req.Header.Get("Idempotency-Key"))
			}
			return response(http.StatusOK, `{"task":{"id":"task_route"},"event":{"seq":9}}`)
		}
		return nil, fmt.Errorf("unexpected request %s %s", req.Method, req.URL.String())
	})}

	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "long-poll"},
	}, routePoolConfig{
		ReprobeInterval: time.Hour,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			latency := 20 * time.Millisecond
			if route.URL == "https://primary.example.test" {
				latency = 10 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	selected, err := pool.initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session, endpoint, lease, _, err := joinSessionByCode(context.Background(), client, selected.URL, "TEST-CODE", controlplane.EndpointSpec{
		Role:      controlplane.EndpointRoleTarget,
		Transport: controlplane.TransportPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	app := New(io.Discard, io.Discard)
	processed, err := app.runSessionTasks(context.Background(), serveOptions{
		GatewayURL:           selected.URL,
		Transport:            selected.Transport,
		PollInterval:         time.Millisecond,
		MaxTasks:             1,
		CapabilityCeilingSet: true,
	}, client, session.ID, endpoint.ID, "fixture-fingerprint", lease.Secret, lease, newGatewayCandidateSetWithPool(pool))
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || registrationCalls != 1 || primaryEventCalls != 2 || secondaryEventCalls != 1 {
		t.Fatalf("continuity counts processed=%d registrations=%d primary_events=%d secondary_events=%d", processed, registrationCalls, primaryEventCalls, secondaryEventCalls)
	}
}

func TestRunSessionTasksDoesNotSkipEventsBeyondTransportLimit(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trustBody, err := json.Marshal(map[string]any{"trust": model.NewTrustBundle("cursor-test", publicKey)})
	if err != nil {
		t.Fatal(err)
	}
	eventCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := func(status int, payload any) (*http.Response, error) {
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
		}
		if req.URL.Path == "/v1/trust-bundle" {
			return response(http.StatusNotFound, map[string]any{"error": "legacy"})
		}
		if req.URL.Path == "/v1/trust" {
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(trustBody)), Request: req}, nil
		}
		if strings.HasSuffix(req.URL.Path, "/events") {
			eventCalls++
			if req.URL.Query().Get("limit") != "16" || req.URL.Query().Get("wait_ms") != "25000" {
				t.Fatalf("long-poll adapter query = %v, want limit=16 wait_ms=25000", req.URL.Query())
			}
			if eventCalls == 1 {
				if got := req.URL.Query().Get("after_seq"); got != "0" {
					t.Fatalf("first after_seq = %q, want 0", got)
				}
				events := make([]controlplane.Event, 0, 16)
				for seq := uint64(1); seq <= 16; seq++ {
					events = append(events, controlplane.Event{Seq: seq, Type: controlplane.EventType("status")})
				}
				return response(http.StatusOK, map[string]any{
					"events":   events,
					"lease":    controlplane.Lease{SessionID: "ses_cursor", EndpointID: "ep_cursor", Generation: 1, Secret: "lease_one"},
					"last_seq": 17,
				})
			}
			if got := req.URL.Query().Get("after_seq"); got != "16" {
				t.Fatalf("second after_seq = %q, want committed limited-batch cursor 16", got)
			}
			return response(http.StatusOK, map[string]any{
				"events":   []controlplane.Event{{Seq: 17, Type: controlplane.EventTypeTask, TaskID: "task_cursor", Payload: map[string]any{"action": "offer"}}},
				"lease":    controlplane.Lease{SessionID: "ses_cursor", EndpointID: "ep_cursor", Generation: 2, Secret: "lease_two"},
				"last_seq": 17,
			})
		}
		if req.Method == http.MethodGet && req.URL.Path == "/v1/sessions/ses_cursor" {
			return response(http.StatusOK, map[string]any{"snapshot": controlplane.SessionSnapshot{Tasks: []controlplane.Task{{ID: "task_cursor", SessionID: "ses_cursor", TargetEndpointID: "ep_cursor", AttemptID: "attempt_cursor", Capabilities: []string{"shell.user"}}}}})
		}
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/tasks/task_cursor/result") {
			return response(http.StatusOK, map[string]any{"task": controlplane.Task{ID: "task_cursor"}, "event": controlplane.Event{Seq: 18}})
		}
		return nil, fmt.Errorf("unexpected request %s %s", req.Method, req.URL.String())
	})}
	pool := newRoutePool([]routeCandidate{{URL: "https://cursor.example.test", Transport: "long-poll"}}, routePoolConfig{
		ReprobeInterval: time.Hour,
		Probe: func(context.Context, routeCandidate) routeProbeResult {
			return routeProbeResult{Healthy: true, Latency: time.Millisecond}
		},
	})
	selected, err := pool.initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease := controlplane.Lease{SessionID: "ses_cursor", EndpointID: "ep_cursor", Secret: "lease_zero"}
	processed, err := New(io.Discard, io.Discard).runSessionTasks(context.Background(), serveOptions{
		GatewayURL:           selected.URL,
		Transport:            selected.Transport,
		PollInterval:         time.Millisecond,
		MaxTasks:             1,
		CapabilityCeilingSet: true,
	}, client, "ses_cursor", "ep_cursor", "fixture-fingerprint", lease.Secret, lease, newGatewayCandidateSetWithPool(pool))
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || eventCalls != 2 {
		t.Fatalf("processed=%d event_calls=%d, want 1 and 2", processed, eventCalls)
	}
}

func TestRunSessionTasksRequeuesInitialEventAcrossTaskFetchFailover(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trustBody, err := json.Marshal(map[string]any{"trust": model.NewTrustBundle("initial-event-test", publicKey)})
	if err != nil {
		t.Fatal(err)
	}
	var taskFetchHosts []string
	resultCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}
		if req.URL.Path == "/v1/trust-bundle" {
			return response(http.StatusNotFound, `{"error":"legacy"}`)
		}
		if req.URL.Path == "/v1/trust" {
			return response(http.StatusOK, string(trustBody))
		}
		if req.Method == http.MethodGet && req.URL.Path == "/v1/sessions/ses_initial" {
			taskFetchHosts = append(taskFetchHosts, req.URL.Host)
			if req.URL.Host == "primary.example.test" {
				return nil, io.ErrUnexpectedEOF
			}
			return response(http.StatusOK, `{"snapshot":{"tasks":[{"id":"task_initial","session_id":"ses_initial","target_endpoint_id":"ep_initial","attempt_id":"attempt_initial","capabilities":["shell.user"]}]}}`)
		}
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/tasks/task_initial/result") {
			resultCalls++
			if req.URL.Host != "secondary.example.test" {
				t.Fatalf("result host = %q, want secondary", req.URL.Host)
			}
			return response(http.StatusOK, `{"task":{"id":"task_initial"},"event":{"seq":2}}`)
		}
		if strings.HasSuffix(req.URL.Path, "/events") {
			t.Fatal("initial event was dropped and fetched again from the gateway")
		}
		return nil, fmt.Errorf("unexpected request %s %s", req.Method, req.URL.String())
	})}
	pool := newRoutePool([]routeCandidate{
		{URL: "https://primary.example.test", Transport: "poll"},
		{URL: "https://secondary.example.test", Transport: "poll"},
	}, routePoolConfig{
		ReprobeInterval: time.Hour,
		Probe: func(_ context.Context, route routeCandidate) routeProbeResult {
			latency := 20 * time.Millisecond
			if route.URL == "https://primary.example.test" {
				latency = 10 * time.Millisecond
			}
			return routeProbeResult{Healthy: true, Latency: latency}
		},
	})
	selected, err := pool.initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease := controlplane.Lease{SessionID: "ses_initial", EndpointID: "ep_initial", Generation: 1, Secret: newIdempotencyKey("fixture-lease")}
	processed, err := New(io.Discard, io.Discard).runSessionTasksWithEvents(context.Background(), serveOptions{
		GatewayURL:           selected.URL,
		Transport:            selected.Transport,
		PollInterval:         time.Millisecond,
		MaxTasks:             1,
		CapabilityCeilingSet: true,
	}, client, "ses_initial", "ep_initial", "fixture-fingerprint", lease.Secret, lease, newGatewayCandidateSetWithPool(pool), []controlplane.Event{{Seq: 1, Type: controlplane.EventTypeTask, TaskID: "task_initial", Payload: map[string]any{"action": "offer"}}})
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || strings.Join(taskFetchHosts, ",") != "primary.example.test,secondary.example.test" || resultCalls != 1 {
		t.Fatalf("processed=%d task_fetch_hosts=%v result_calls=%d", processed, taskFetchHosts, resultCalls)
	}
}

func TestRunSessionTasksRejectsNetworkEventsWithoutRenewedLease(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trustBody, err := json.Marshal(map[string]any{"trust": model.NewTrustBundle("missing-lease-test", publicKey)})
	if err != nil {
		t.Fatal(err)
	}
	taskFetches := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		status := http.StatusOK
		switch {
		case req.URL.Path == "/v1/trust-bundle":
			status, body = http.StatusNotFound, `{"error":"legacy"}`
		case req.URL.Path == "/v1/trust":
			body = string(trustBody)
		case strings.HasSuffix(req.URL.Path, "/events"):
			body = `{"events":[{"seq":1,"type":"task","task_id":"must_not_run","payload":{"action":"offer"}}],"last_seq":1}`
		case req.URL.Path == "/v1/sessions/ses_missing_lease":
			taskFetches++
			body = `{"snapshot":{}}`
		default:
			return nil, fmt.Errorf("unexpected request %s", req.URL.String())
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	pool := newRoutePool([]routeCandidate{{URL: "https://missing-lease.example.test", Transport: "poll"}}, routePoolConfig{
		Probe: func(context.Context, routeCandidate) routeProbeResult {
			return routeProbeResult{Healthy: true, Latency: time.Millisecond}
		},
	})
	selected, err := pool.initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease := controlplane.Lease{SessionID: "ses_missing_lease", EndpointID: "ep_missing_lease", Generation: 1, Secret: newIdempotencyKey("fixture-lease")}
	_, err = New(io.Discard, io.Discard).runSessionTasks(context.Background(), serveOptions{
		GatewayURL:   selected.URL,
		Transport:    selected.Transport,
		MaxTasks:     1,
		PollInterval: time.Millisecond,
	}, client, lease.SessionID, lease.EndpointID, "fixture-fingerprint", lease.Secret, lease, newGatewayCandidateSetWithPool(pool))
	if err == nil {
		t.Fatal("events response without renewed lease was accepted")
	}
	if taskFetches != 0 {
		t.Fatalf("task fetched %d times from an unbound events response", taskFetches)
	}
}

func TestJoinManifestContainsGatewayRequiresExactVerifiedCandidate(t *testing.T) {
	manifest := model.JoinManifest{
		GatewayURL: "https://primary.example.test/base",
		GatewayCandidates: []model.JoinManifestGatewayCandidate{
			{URL: "https://secondary.example.test/relay"},
		},
	}
	for _, test := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "primary", value: "https://primary.example.test/base/", want: true},
		{name: "signed candidate", value: "https://secondary.example.test/relay", want: true},
		{name: "unlisted host", value: "https://other.example.test/base", want: false},
		{name: "query injection", value: "https://secondary.example.test/relay?x=1", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := joinManifestContainsGateway(manifest, test.value); got != test.want {
				t.Fatalf("joinManifestContainsGateway(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestValidateGatewayCandidateSetRejectsUnrootedPublicCandidate(t *testing.T) {
	routes := newGatewayCandidateSet("http://127.0.0.1:8787", []model.JoinManifestGatewayCandidate{
		{URL: "https://public.example.test"},
	}, "poll")
	if err := validateGatewayCandidateSet(routes, false); err == nil {
		t.Fatal("unrooted public manifest candidate was accepted")
	}
	if err := validateGatewayCandidateSet(routes, true); err != nil {
		t.Fatalf("root-verified candidate set was rejected: %v", err)
	}
}

func TestRunServeRejectsPublicHTTPManifestURLBeforeFetch(t *testing.T) {
	err := New(io.Discard, io.Discard).runServe(context.Background(), serveOptions{
		Mode:        "temporary",
		ManifestURL: "http://192.0.2.10/v1/tickets/fixture/manifest",
		Transport:   "poll",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("public HTTP manifest error = %v", err)
	}
}

func TestDoGatewayRequestRejectsRedirect(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls++
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	req, err := http.NewRequest(http.MethodPost, redirect.URL, strings.NewReader(`{"join_code":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := doGatewayRequest(redirect.Client(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || targetCalls != 0 {
		t.Fatalf("redirect status=%d target_calls=%d, want rejected redirect", resp.StatusCode, targetCalls)
	}
}

func TestJoinSessionNetworkErrorRedactsGatewayURL(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("dial https://private.example.test/hidden: connection failed")
	})}
	_, _, _, _, err := joinSessionByCode(context.Background(), client, "https://private.example.test/hidden", "TEST-CODE", controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
	if err == nil {
		t.Fatal("expected registration network failure")
	}
	if message := err.Error(); strings.Contains(message, "private.example.test") || strings.Contains(message, "/hidden") || strings.Contains(message, "TEST-CODE") {
		t.Fatalf("registration error leaked private connection data: %q", message)
	}
}

func TestDecodeBoundedGatewayJSONRejectsOversize(t *testing.T) {
	var payload map[string]any
	if err := decodeBoundedGatewayJSON(strings.NewReader(`{"ok":true}`), 64, &payload); err != nil {
		t.Fatal(err)
	}
	if err := decodeBoundedGatewayJSON(strings.NewReader(`{"value":"`+strings.Repeat("x", 80)+`"}`), 64, &payload); err == nil {
		t.Fatal("oversized gateway JSON was accepted")
	}
}

func TestValidateLeaseBindingRejectsCrossSessionAndStaleGeneration(t *testing.T) {
	current := controlplane.Lease{SessionID: "ses", EndpointID: "ep", Generation: 2, Secret: "old"}
	if err := validateLeaseBinding(current, controlplane.Lease{SessionID: "ses", EndpointID: "ep", Generation: 3, Secret: "next"}, "ses", "ep"); err != nil {
		t.Fatal(err)
	}
	for _, next := range []controlplane.Lease{
		{SessionID: "other", EndpointID: "ep", Generation: 3, Secret: "next"},
		{SessionID: "ses", EndpointID: "other", Generation: 3, Secret: "next"},
		{SessionID: "ses", EndpointID: "ep", Generation: 2, Secret: "next"},
	} {
		if err := validateLeaseBinding(current, next, "ses", "ep"); err == nil {
			t.Fatalf("invalid lease was accepted: %#v", next)
		}
	}
}

func TestFetchHostTrustDoesNotDowngradeInvalidSignedBundle(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	legacyBody, err := json.Marshal(map[string]any{"trust": model.NewTrustBundle("legacy", publicKey)})
	if err != nil {
		t.Fatal(err)
	}
	legacyCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"trust_bundle":{}}`
		if req.URL.Path == "/v1/trust" {
			legacyCalls++
			body = string(legacyBody)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	if _, err := fetchHostTrust(context.Background(), client, "https://gateway.example.test", "", ""); err == nil {
		t.Fatal("invalid signed trust response downgraded to legacy trust")
	}
	if legacyCalls != 0 {
		t.Fatalf("legacy trust endpoint called %d times after signed verification failure", legacyCalls)
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
