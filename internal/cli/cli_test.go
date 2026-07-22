package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd"
	"github.com/EitanWong/remote-dev-skillkit/internal/connectionrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcmd"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
	"github.com/EitanWong/remote-dev-skillkit/internal/hosttrust"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/protectedstore"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestJoinSessionByCodePermanentProtocolRejectionUsesExitCode78(t *testing.T) {
	err := cliJoinSessionResponseError(t, http.StatusNotFound, `{"error":{"schema_version":"rdev.error.v1","code":"invalid_join_code","message":"join code is invalid","recoverable":false,"retry_after_ms":0,"user_summary":"The support-session entry is invalid or no longer active.","agent_next_action":"create a fresh support-session entry"}}`)
	if got := hostcmd.ExitCode(err); got != hostcmd.PermanentJoinFailureExitCode {
		t.Fatalf("ExitCode() = %d, want %d for %v", got, hostcmd.PermanentJoinFailureExitCode, err)
	}
}

func TestJoinSessionByCodeRecoverableProtocolRejectionUsesExitCode1(t *testing.T) {
	err := cliJoinSessionResponseError(t, http.StatusServiceUnavailable, `{"error":{"schema_version":"rdev.error.v1","code":"gateway_unavailable","message":"gateway is temporarily unavailable","recoverable":true,"retry_after_ms":1000,"user_summary":"The gateway is temporarily unavailable.","agent_next_action":"retry after the suggested delay"}}`)
	if got := hostcmd.ExitCode(err); got != 1 {
		t.Fatalf("ExitCode() = %d, want 1 for %v", got, err)
	}
}

func TestFullHostRunSessionTaskRejectsCapabilityOutsideSignedManifestBeforeAdapter(t *testing.T) {
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

	app := NewApp(io.Discard, io.Discard)
	err := app.runSessionTask(context.Background(), hostServeOptions{
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
		t.Fatalf("full rdev adapter ran outside signed capability ceiling: %v", err)
	}
	payload := <-resultPayload
	if payload["status"] != string(controlplane.TaskStatusFailed) || !strings.Contains(fmt.Sprint(payload["reason"]), "signed join manifest ceiling") {
		t.Fatalf("full rdev capability denial was not reported as a failed task: %#v", payload)
	}
}

func cliJoinSessionResponseError(t *testing.T, statusCode int, body string) error {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: statusCode,
			Status:     fmt.Sprintf("%d test response", statusCode),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
	_, _, _, _, err := joinSessionByCode(context.Background(), client, "https://gateway.example.test", "WAIT-1234", controlplane.EndpointSpec{
		Role: controlplane.EndpointRoleTarget,
		Name: "windows-target",
	})
	if err == nil {
		t.Fatal("joinSessionByCode() error = nil, want protocol rejection")
	}
	return err
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

type recordingStateStore struct {
	mu             sync.Mutex
	snapshots      []gateway.Snapshot
	failSaves      int
	failAfterWrite int
}

func (s *recordingStateStore) LoadInto(gw *gateway.MemoryGateway) (gateway.Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.snapshots) == 0 {
		return gateway.Snapshot{}, false, nil
	}
	snapshot := s.snapshots[len(s.snapshots)-1]
	if err := gw.RestoreSnapshot(snapshot); err != nil {
		return gateway.Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s *recordingStateStore) SaveFrom(gw *gateway.MemoryGateway) (gateway.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := gw.Snapshot()
	if s.failSaves > 0 {
		s.failSaves--
		return gateway.Snapshot{}, errors.New("injected state save failure")
	}
	s.snapshots = append(s.snapshots, snapshot)
	if s.failAfterWrite > 0 {
		s.failAfterWrite--
		return gateway.Snapshot{}, errors.New("injected ambiguous state save failure")
	}
	return snapshot, nil
}

func (s *recordingStateStore) Describe() string { return "recording" }

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("injected stdout panic") }

type ambiguousRollbackStore struct {
	calls   int
	durable []gateway.Snapshot
}

func (s *ambiguousRollbackStore) LoadInto(gw *gateway.MemoryGateway) (gateway.Snapshot, bool, error) {
	if len(s.durable) == 0 {
		return gateway.Snapshot{}, false, nil
	}
	snapshot := s.durable[len(s.durable)-1]
	return snapshot, true, gw.RestoreSnapshot(snapshot)
}

func (s *ambiguousRollbackStore) SaveFrom(gw *gateway.MemoryGateway) (gateway.Snapshot, error) {
	s.calls++
	snapshot := gw.Snapshot()
	switch s.calls {
	case 1:
		s.durable = append(s.durable, snapshot)
		return gateway.Snapshot{}, errors.New("ambiguous active save")
	case 2:
		return gateway.Snapshot{}, errors.New("rollback save unavailable")
	default:
		s.durable = append(s.durable, snapshot)
		return snapshot, nil
	}
}

func (*ambiguousRollbackStore) Describe() string { return "ambiguous-rollback" }

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type supportSessionTestTunnelProvider struct{}

func (supportSessionTestTunnelProvider) ID() string { return "ordered-test" }

func (supportSessionTestTunnelProvider) Metadata() tunnel.ProviderMetadata {
	return tunnel.ProviderMetadata{
		ID: "ordered-test", DisplayName: "ordered test", Protocols: []string{"https"},
		Anonymous: true, Executable: "test", DocumentationURL: "https://example.test/docs", DefaultAutomatic: true,
	}
}

func (supportSessionTestTunnelProvider) Start(context.Context, tunnel.StartRequest) (tunnel.Handle, error) {
	return newSupportSessionTestTunnelHandle(tunnel.Candidate{ProviderID: "ordered-test", URL: "https://ordered.example.test"}), nil
}

type supportSessionTestTunnelHandle struct {
	candidate tunnel.Candidate
	wait      chan error
	stopOnce  sync.Once
}

func newSupportSessionTestTunnelHandle(candidate tunnel.Candidate) *supportSessionTestTunnelHandle {
	return &supportSessionTestTunnelHandle{candidate: candidate, wait: make(chan error, 1)}
}

func (h *supportSessionTestTunnelHandle) Candidate() tunnel.Candidate { return h.candidate }
func (h *supportSessionTestTunnelHandle) Wait() <-chan error          { return h.wait }
func (h *supportSessionTestTunnelHandle) Stop(context.Context) error {
	h.stopOnce.Do(func() { close(h.wait) })
	return nil
}

type supportSessionFuncTunnelProvider struct {
	id           string
	url          string
	metadata     *tunnel.ProviderMetadata
	start        func() error
	startRequest func(tunnel.StartRequest) error
	stop         func()
}

func (p supportSessionFuncTunnelProvider) ID() string { return p.id }
func (p supportSessionFuncTunnelProvider) Metadata() tunnel.ProviderMetadata {
	if p.metadata != nil {
		metadata := *p.metadata
		metadata.Protocols = append([]string(nil), p.metadata.Protocols...)
		return metadata
	}
	return tunnel.ProviderMetadata{
		ID: p.id, DisplayName: p.id, Protocols: []string{"https"}, Anonymous: true,
		Executable: "test", DocumentationURL: "https://example.test/docs", DefaultAutomatic: true,
	}
}
func (p supportSessionFuncTunnelProvider) Start(_ context.Context, request tunnel.StartRequest) (tunnel.Handle, error) {
	if p.start != nil {
		if err := p.start(); err != nil {
			return nil, err
		}
	}
	if p.startRequest != nil {
		if err := p.startRequest(request); err != nil {
			return nil, err
		}
	}
	handle := newSupportSessionTestTunnelHandle(tunnel.Candidate{ProviderID: p.id, URL: p.url})
	if p.stop == nil {
		return handle, nil
	}
	return &supportSessionStopCallbackHandle{supportSessionTestTunnelHandle: handle, stop: p.stop}, nil
}

type supportSessionStopCallbackHandle struct {
	*supportSessionTestTunnelHandle
	stop     func()
	callback sync.Once
}

func (h *supportSessionStopCallbackHandle) Stop(ctx context.Context) error {
	h.callback.Do(h.stop)
	return h.supportSessionTestTunnelHandle.Stop(ctx)
}

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "rdev") {
		t.Fatalf("expected version output to mention rdev, got %q", stdout.String())
	}
}

func TestTopLevelHelpLeadsWithSupportSessionConnect(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	got := stdout.String()
	connectIndex := strings.Index(got, "rdev support-session connect --start")
	if connectIndex < 0 {
		t.Fatalf("top-level help must expose support-session connect --start, got stdout=%q stderr=%q", got, stderr.String())
	}
	for _, lowerLevel := range []string{
		"rdev support-session start",
		"rdev support-session create",
		"rdev support-session plan",
		"rdev invite create",
		"rdev connection-entry plan",
	} {
		if index := strings.Index(got, lowerLevel); index >= 0 && index < connectIndex {
			t.Fatalf("top-level help must lead fresh Agents to connect before %q, got stdout=%q", lowerLevel, got)
		}
	}
	if !strings.Contains(got, "rdev support-session --help") {
		t.Fatalf("top-level help should route advanced support-session discovery to group help, got stdout=%q", got)
	}
}

func TestSupportSessionHelpIsAgentFriendly(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"support-session", "--help"}); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "rdev support-session connect --start") ||
		!strings.Contains(got, "Do not add --public-tunnel") ||
		!strings.Contains(got, "rdev support-session smoke-test --gateway-url <active-gateway-url> --session-id ses_...") ||
		!strings.Contains(got, "rdev support-session audit-capabilities --gateway-url <active-gateway-url> --session-id ses_...") ||
		!strings.Contains(got, "rdev support-session cleanup") {
		t.Fatalf("expected support-session help to guide fresh Agents, got stdout=%q stderr=%q", got, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := app.Run(context.Background(), []string{"support-session", "connect", "--help"}); err != nil {
		t.Fatal(err)
	}
	if got := stderr.String(); !strings.Contains(got, "Usage of support-session connect") ||
		!strings.Contains(got, "-start") {
		t.Fatalf("expected connect flag help without failure, got stdout=%q stderr=%q", stdout.String(), got)
	}
}

func TestRetryingRoundTripperRetriesIdempotentPost(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"ok":true}` {
			t.Fatalf("unexpected request body on attempt %d: %q", attempts, string(body))
		}
		if req.Header.Get("Idempotency-Key") != "idem-test" {
			t.Fatalf("expected idempotency key on attempt %d, got %q", attempts, req.Header.Get("Idempotency-Key"))
		}
		if attempts == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/sessions/sess_1/tasks", strings.NewReader(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Idempotency-Key", "idem-test")
	resp, err := (retryingRoundTripper{Base: base, MaxRetries: 2}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
}

func TestWaitForGatewayHealthRetriesUntilHealthy(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("expected /healthz probe, got %s", r.URL.Path)
		}
		attempts++
		if attempts == 1 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	if err := waitForGatewayHealth(context.Background(), server.URL, time.Second); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected health probe retry, got %d attempts", attempts)
	}
}

func TestRetryingRoundTripperDoesNotRetryPostWithoutIdempotencyKey(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return nil, io.ErrUnexpectedEOF
	})
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/sessions/sess_1/tasks", strings.NewReader(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = (retryingRoundTripper{Base: base, MaxRetries: 2}).RoundTrip(req)
	if err == nil {
		t.Fatal("expected transient POST error without retry")
	}
	if attempts != 1 {
		t.Fatalf("expected one attempt without idempotency key, got %d", attempts)
	}
}

func TestFilesUploadRetriesJobCreateWithIdempotencyKey(t *testing.T) {
	oldDefaultClient := http.DefaultClient
	oldDefaultTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultClient = oldDefaultClient
		http.DefaultTransport = oldDefaultTransport
	})

	attempts := 0
	var firstKey string
	http.DefaultClient = &http.Client{}
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/v1/sessions/sess_1/tasks" {
			t.Fatalf("expected session task endpoint, got %s", req.URL.Path)
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
		if !strings.Contains(string(body), `"adapter":"file"`) ||
			!strings.Contains(string(body), `"action":"upload"`) {
			t.Fatalf("unexpected session task body: %s", string(body))
		}
		if attempts == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"task":{"id":"task_1"}}`)),
			Request:    req,
		}, nil
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{
		"files", "upload",
		"--gateway-url", "http://gateway.test",
		"--session-id", "sess_1",
		"--path", "remote-control-upload.txt",
		"--content", "hello",
	})
	if err != nil {
		t.Fatalf("files upload should retry transient EOF, err=%v stderr=%q", err, stderr.String())
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
	if !strings.Contains(stdout.String(), `"task_1"`) {
		t.Fatalf("expected gateway response to be printed, got %q", stdout.String())
	}
}

func TestCommandGroupHelpIsAgentFriendly(t *testing.T) {
	groups := []string{
		"acceptance",
		"adapter",
		"audit",
		"bootstrap",
		"connection-entry",
		"demo",
		"deps",
		"enrollment",
		"gateway",
		"host",
		"hosted-provider",
		"invite",
		"task",
		"mcp",
		"operator-auth",
		"policy",
		"release",
		"relay-adapter",
		"skillkit",
		"ticket",
		"trust",
		"update",
		"workspace",
	}
	for _, group := range groups {
		t.Run(group, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := NewApp(&stdout, &stderr)

			if err := app.Run(context.Background(), []string{group, "--help"}); err != nil {
				t.Fatal(err)
			}
			got := stdout.String()
			if !strings.Contains(got, "Usage:") ||
				!strings.Contains(got, "Subcommands:") ||
				!strings.Contains(got, "Use `rdev "+group+" <subcommand> --help`") {
				t.Fatalf("expected command group help for %s, got stdout=%q stderr=%q", group, got, stderr.String())
			}
			if group == "task" && strings.Contains(got, "authorize") {
				t.Fatalf("task help should not advertise retired authorize subcommand, got stdout=%q stderr=%q", got, stderr.String())
			}
		})
	}
}

func TestVersionJSONAndDoctorExposeRuntimeInfo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, envName := range []string{
		"RDEV_CODEX_SKILLS_DIR",
		"RDEV_CLAUDE_CODE_SKILLS_DIR",
		"RDEV_HERMES_SKILLS_DIR",
		"RDEV_OPENCLAW_SKILLS_DIR",
		"RDEV_OPENCODE_SKILLS_DIR",
		"RDEV_GENERIC_AGENT_SKILLS_DIR",
	} {
		t.Setenv(envName, "")
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"version", "--json"}); err != nil {
		t.Fatal(err)
	}
	var versionPayload struct {
		SchemaVersion   string `json:"schema_version"`
		Name            string `json:"name"`
		Version         string `json:"version"`
		Commit          string `json:"commit"`
		CurrentExe      string `json:"current_executable"`
		SourceRootValid bool   `json:"source_root_valid"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &versionPayload); err != nil {
		t.Fatalf("invalid version JSON: %v\n%s", err, stdout.String())
	}
	if versionPayload.SchemaVersion != "rdev.runtime-info.v1" ||
		versionPayload.Name != "rdev" ||
		versionPayload.Version == "" ||
		versionPayload.Commit == "" ||
		versionPayload.CurrentExe == "" ||
		!versionPayload.SourceRootValid {
		t.Fatalf("expected runtime version info, got %#v", versionPayload)
	}

	stdout.Reset()
	if err := app.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	var doctorPayload struct {
		SchemaVersion    string         `json:"schema_version"`
		OK               bool           `json:"ok"`
		Rdev             map[string]any `json:"rdev"`
		HostCapabilities map[string]any `json:"host_capabilities"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doctorPayload); err != nil {
		t.Fatalf("invalid doctor JSON: %v\n%s", err, stdout.String())
	}
	if doctorPayload.SchemaVersion != "rdev.doctor.v1" ||
		!doctorPayload.OK ||
		doctorPayload.Rdev["schema_version"] != "rdev.runtime-info.v1" ||
		doctorPayload.HostCapabilities == nil {
		t.Fatalf("expected doctor runtime and host capability info, got %#v", doctorPayload)
	}
}

func TestDoctorFailsClosedOnUnhealthySkillkitInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, target := writeRuntimeInfoSkillFixture(t, "old safe remote support\n")
	t.Setenv("RDEV_SOURCE_ROOT", root)
	refDir := filepath.Join(root, ".remote-dev-skillkit")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version": "rdev.skillkit-install-manifest.v1",
		"bundle_dir":     filepath.Join(root, "dist", "remote-dev-skillkit"),
		"target_dir":     target,
		"framework":      "codex",
		"installed_at":   "2026-07-07T00:00:00Z",
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "install.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	ok, diagnostics, actions := runtimeInfoHealth(rdevRuntimeInfo(root))
	if ok {
		t.Fatalf("expected unhealthy Skillkit install to fail doctor health")
	}
	joinedDiagnostics := strings.Join(diagnostics, "\n")
	joinedActions := strings.Join(actions, "\n")
	if !strings.Contains(joinedDiagnostics, "stale=safe-remote-support") ||
		!strings.Contains(joinedActions, "rdev skillkit install --execute") {
		t.Fatalf("expected stale diagnostic and refresh action, diagnostics=%#v actions=%#v", diagnostics, actions)
	}

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	var doctorPayload struct {
		OK               bool     `json:"ok"`
		Diagnostics      []string `json:"diagnostics"`
		AgentNextActions []string `json:"agent_next_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doctorPayload); err != nil {
		t.Fatalf("invalid doctor JSON: %v\n%s", err, stdout.String())
	}
	if doctorPayload.OK ||
		!strings.Contains(strings.Join(doctorPayload.Diagnostics, "\n"), "stale=safe-remote-support") ||
		!strings.Contains(strings.Join(doctorPayload.AgentNextActions, "\n"), "rdev skillkit install --execute") {
		t.Fatalf("expected doctor to fail closed on stale Skillkit install, got %s", stdout.String())
	}
}

func TestDoctorFailsClosedOnLegacySkillkitInstallWithoutManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, target := writeRuntimeInfoSkillFixture(t, "legacy installed safe-remote-support\n")
	t.Setenv("RDEV_CODEX_SKILLS_DIR", target)

	ok, diagnostics, actions := runtimeInfoHealth(rdevRuntimeInfo(root))
	if ok {
		t.Fatalf("expected legacy stale Skillkit install to fail doctor health")
	}
	joinedDiagnostics := strings.Join(diagnostics, "\n")
	joinedActions := strings.Join(actions, "\n")
	if !strings.Contains(joinedDiagnostics, "legacy codex") ||
		!strings.Contains(joinedDiagnostics, "stale=safe-remote-support") ||
		!strings.Contains(joinedActions, "rdev skillkit install --execute") {
		t.Fatalf("expected legacy stale diagnostic and refresh action, diagnostics=%#v actions=%#v", diagnostics, actions)
	}
}

func TestDoctorFailsClosedOnBrokenSkillkitInstallManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, _ := writeRuntimeInfoSkillFixture(t, "source safe-remote-support\n")
	refDir := filepath.Join(root, ".remote-dev-skillkit")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "install.json"), []byte("{broken-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ok, diagnostics, actions := runtimeInfoHealth(rdevRuntimeInfo(root))
	if ok {
		t.Fatalf("expected broken Skillkit install manifest to fail doctor health")
	}
	if !strings.Contains(strings.Join(diagnostics, "\n"), "manifest is unreadable") ||
		!strings.Contains(strings.Join(actions, "\n"), "rdev skillkit install --execute") {
		t.Fatalf("expected broken manifest diagnostic and refresh action, diagnostics=%#v actions=%#v", diagnostics, actions)
	}
}

func TestDoctorFailsClosedOnUnverifiableSkillkitInstallManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, _ := writeRuntimeInfoSkillFixture(t, "source safe-remote-support\n")
	refDir := filepath.Join(root, ".remote-dev-skillkit")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version": "rdev.skillkit-install-manifest.v1",
		"framework":      "codex",
		"installed_at":   "2026-07-07T00:00:00Z",
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "install.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	ok, diagnostics, actions := runtimeInfoHealth(rdevRuntimeInfo(root))
	if ok {
		t.Fatalf("expected unverifiable Skillkit install manifest to fail doctor health")
	}
	if !strings.Contains(strings.Join(diagnostics, "\n"), "manifest is not verifiable") ||
		!strings.Contains(strings.Join(actions, "\n"), "rdev skillkit install --execute") {
		t.Fatalf("expected unverifiable manifest diagnostic and refresh action, diagnostics=%#v actions=%#v", diagnostics, actions)
	}
}

func writeRuntimeInfoSkillFixture(t *testing.T, safeRemoteInstalledContent string) (string, string) {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "skills")
	for _, skill := range []string{"safe-remote-support", "host-triage", "remote-vibe-coding", "remote-session-review"} {
		sourceDir := filepath.Join(root, "skills", skill)
		targetDir := filepath.Join(target, skill)
		if err := os.MkdirAll(sourceDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			t.Fatal(err)
		}
		source := "source " + skill + "\n"
		installed := source
		if skill == "safe-remote-support" {
			installed = safeRemoteInstalledContent
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, "SKILL.md"), []byte(installed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/rdev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "rdev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "rdev", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRuntimeInfoReferenceFixture(t, root, target, "codex")
	return root, target
}

func writeRuntimeInfoReferenceFixture(t *testing.T, root, target, framework string) []any {
	t.Helper()
	if framework == "" {
		framework = "codex"
	}
	bundle := filepath.Join(root, "dist", "remote-dev-skillkit")
	paths := map[string]string{
		filepath.Join(root, "mcp", "tools.json"):                                     "{\"tools\":[]}\n",
		filepath.Join(bundle, "frameworks", framework+".md"):                         "# " + framework + "\n",
		filepath.Join(target, ".remote-dev-skillkit", "mcp", "tools.json"):           "{\"tools\":[]}\n",
		filepath.Join(target, ".remote-dev-skillkit", "frameworks", framework+".md"): "# " + framework + "\n",
	}
	for path, content := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	references := []any{}
	for _, item := range []struct {
		name string
		rel  string
	}{
		{name: "mcp-tools", rel: "mcp/tools.json"},
		{name: "framework-doc", rel: "frameworks/" + framework + ".md"},
	} {
		path := filepath.Join(target, ".remote-dev-skillkit", filepath.FromSlash(item.rel))
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		references = append(references, map[string]any{
			"name":          item.name,
			"relative_path": item.rel,
			"sha256":        "sha256:" + hex.EncodeToString(sum[:]),
			"size_bytes":    len(content),
		})
	}
	return references
}

func runtimeInfoSkillManifestFiles(t *testing.T, target string) []any {
	t.Helper()
	files := []any{}
	for _, skill := range []string{"safe-remote-support", "host-triage", "remote-vibe-coding", "remote-session-review"} {
		path := filepath.Join(target, skill, "SKILL.md")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		files = append(files, map[string]any{
			"name":          skill,
			"relative_path": filepath.ToSlash(filepath.Join(skill, "SKILL.md")),
			"sha256":        "sha256:" + hex.EncodeToString(sum[:]),
			"size_bytes":    len(content),
		})
	}
	return files
}

func runtimeInfoReferenceManifestFiles(t *testing.T, refDir, framework string) []any {
	t.Helper()
	if framework == "" {
		framework = "codex"
	}
	files := []any{}
	for _, item := range []struct {
		name string
		rel  string
	}{
		{name: "mcp-tools", rel: "mcp/tools.json"},
		{name: "framework-doc", rel: "frameworks/" + framework + ".md"},
	} {
		path := filepath.Join(refDir, filepath.FromSlash(item.rel))
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		files = append(files, map[string]any{
			"name":          item.name,
			"relative_path": item.rel,
			"sha256":        "sha256:" + hex.EncodeToString(sum[:]),
			"size_bytes":    len(content),
		})
	}
	return files
}

func TestRuntimeInfoReportsStaleInstalledSkills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "skills")
	for _, skill := range []string{"safe-remote-support", "host-triage", "remote-vibe-coding", "remote-session-review"} {
		sourceDir := filepath.Join(root, "skills", skill)
		targetDir := filepath.Join(target, skill)
		if err := os.MkdirAll(sourceDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			t.Fatal(err)
		}
		source := "source " + skill + "\n"
		installed := source
		if skill == "safe-remote-support" {
			installed = "old safe remote support\n"
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, "SKILL.md"), []byte(installed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/rdev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "rdev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "rdev", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	referenceFiles := writeRuntimeInfoReferenceFixture(t, root, target, "codex")
	refDir := filepath.Join(root, ".remote-dev-skillkit")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version":  "rdev.skillkit-install-manifest.v1",
		"bundle_dir":      filepath.Join(root, "dist", "remote-dev-skillkit"),
		"target_dir":      target,
		"framework":       "codex",
		"installed_at":    "2026-07-07T00:00:00Z",
		"reference_files": referenceFiles,
		"mcp_tools_json":  filepath.Join(target, ".remote-dev-skillkit", "mcp", "tools.json"),
		"framework_doc":   filepath.Join(target, ".remote-dev-skillkit", "frameworks", "codex.md"),
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "install.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	payload := rdevRuntimeInfo(root)
	manifests := payload["install_manifests"].([]map[string]any)
	if len(manifests) != 1 {
		t.Fatalf("expected one install manifest, got %#v", manifests)
	}
	status := manifests[0]["skill_status"].(map[string]any)
	stale := status["stale_skills"].([]string)
	if status["schema_version"] != "rdev.skill-install-status.v1" ||
		status["ok"] != false ||
		!slices.Contains(stale, "safe-remote-support") {
		t.Fatalf("expected stale safe-remote-support status, got %#v", status)
	}
}

func TestRuntimeInfoDetectsLegacyInstalledSkillsWithoutManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "skills")
	for _, skill := range []string{"safe-remote-support", "host-triage", "remote-vibe-coding", "remote-session-review"} {
		sourceDir := filepath.Join(root, "skills", skill)
		targetDir := filepath.Join(target, skill)
		if err := os.MkdirAll(sourceDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("source "+skill+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		installed := "source " + skill + "\n"
		if skill == "safe-remote-support" {
			installed = "legacy installed safe-remote-support\n"
		}
		if err := os.WriteFile(filepath.Join(targetDir, "SKILL.md"), []byte(installed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/rdev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "rdev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "rdev", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RDEV_CODEX_SKILLS_DIR", target)

	payload := rdevRuntimeInfo(root)
	if payload["install_manifest_count"] != 0 {
		t.Fatalf("expected no manifests for legacy install, got %#v", payload["install_manifests"])
	}
	targets := payload["detected_skill_install_targets"].([]map[string]any)
	var detected map[string]any
	for _, candidate := range targets {
		if candidate["target_dir"] == target {
			detected = candidate
			break
		}
	}
	if detected == nil {
		t.Fatalf("expected detected legacy target %s, got %#v", target, targets)
	}
	status := detected["skill_status"].(map[string]any)
	stale := status["stale_skills"].([]string)
	if detected["install_manifest_found"] != false ||
		status["schema_version"] != "rdev.skill-install-status.v1" ||
		status["ok"] != false ||
		!slices.Contains(stale, "safe-remote-support") {
		t.Fatalf("expected stale legacy skill target, got %#v", detected)
	}
}

func TestSkillInstallStatusUsesManifestIntegrityWithoutSourceRoot(t *testing.T) {
	target := t.TempDir()
	skillNames := []string{"safe-remote-support", "host-triage", "remote-vibe-coding", "remote-session-review"}
	var skillFiles []any
	for _, skill := range skillNames {
		dir := filepath.Join(target, skill)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := []byte("installed " + skill + "\n")
		path := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		skillFiles = append(skillFiles, map[string]any{
			"name":          skill,
			"relative_path": filepath.ToSlash(filepath.Join(skill, "SKILL.md")),
			"sha256":        "sha256:" + hex.EncodeToString(sum[:]),
			"size_bytes":    len(content),
		})
	}
	if err := os.WriteFile(filepath.Join(target, "safe-remote-support", "SKILL.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status := skillInstallStatus("", map[string]any{
		"target_dir":     target,
		"skill_files":    skillFiles,
		"schema_version": "rdev.skillkit-install-manifest.v1",
	})
	mismatches := status["manifest_mismatch_skills"].([]string)
	if status["source_status_known"] != false ||
		status["integrity_status_known"] != true ||
		status["manifest_integrity_ok"] != false ||
		status["ok"] != false ||
		!slices.Contains(mismatches, "safe-remote-support") {
		t.Fatalf("expected manifest integrity mismatch without source root, got %#v", status)
	}
}

func TestDoctorFailsClosedOnStaleInstalledMCPToolsReference(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, target := writeRuntimeInfoSkillFixture(t, "source safe-remote-support\n")
	referenceFiles := writeRuntimeInfoReferenceFixture(t, root, target, "codex")
	if err := os.WriteFile(filepath.Join(root, "mcp", "tools.json"), []byte("{\"tools\":[{\"name\":\"new\"}]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refDir := filepath.Join(root, ".remote-dev-skillkit")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version":  "rdev.skillkit-install-manifest.v1",
		"bundle_dir":      filepath.Join(root, "dist", "remote-dev-skillkit"),
		"target_dir":      target,
		"framework":       "codex",
		"installed_at":    "2026-07-07T00:00:00Z",
		"skill_files":     runtimeInfoSkillManifestFiles(t, target),
		"reference_files": referenceFiles,
		"mcp_tools_json":  filepath.Join(target, ".remote-dev-skillkit", "mcp", "tools.json"),
		"framework_doc":   filepath.Join(target, ".remote-dev-skillkit", "frameworks", "codex.md"),
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "install.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	ok, diagnostics, actions := runtimeInfoHealth(rdevRuntimeInfo(root))
	if ok {
		t.Fatalf("expected stale installed MCP tools reference to fail doctor health")
	}
	joinedDiagnostics := strings.Join(diagnostics, "\n")
	if !strings.Contains(joinedDiagnostics, "stale_references=mcp-tools") ||
		!strings.Contains(strings.Join(actions, "\n"), "rdev skillkit install --execute") {
		t.Fatalf("expected stale MCP tools reference diagnostic and refresh action, diagnostics=%#v actions=%#v", diagnostics, actions)
	}
}

func TestSkillInstallStatusUsesReferenceManifestIntegrityWithoutSourceRoot(t *testing.T) {
	target := t.TempDir()
	skillNames := []string{"safe-remote-support", "host-triage", "remote-vibe-coding", "remote-session-review"}
	for _, skill := range skillNames {
		dir := filepath.Join(target, skill)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("installed "+skill+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refDir := filepath.Join(target, ".remote-dev-skillkit")
	if err := os.MkdirAll(filepath.Join(refDir, "mcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(refDir, "frameworks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "mcp", "tools.json"), []byte("{\"tools\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "frameworks", "codex.md"), []byte("# codex\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillFiles := runtimeInfoSkillManifestFiles(t, target)
	referenceFiles := runtimeInfoReferenceManifestFiles(t, refDir, "codex")
	if err := os.WriteFile(filepath.Join(refDir, "mcp", "tools.json"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status := skillInstallStatus("", map[string]any{
		"target_dir":      target,
		"skill_files":     skillFiles,
		"reference_files": referenceFiles,
		"schema_version":  "rdev.skillkit-install-manifest.v1",
		"framework_doc":   filepath.Join(refDir, "frameworks", "codex.md"),
	})
	mismatches := status["manifest_mismatch_reference_files"].([]string)
	if status["reference_integrity_status_known"] != true ||
		status["reference_manifest_integrity_ok"] != false ||
		status["ok"] != false ||
		!slices.Contains(mismatches, "mcp-tools") {
		t.Fatalf("expected reference manifest integrity mismatch without source root, got %#v", status)
	}
}

func TestCommonSkillTargetCandidatesExpandHomePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RDEV_CODEX_SKILLS_DIR", "~/custom-codex-skills")

	candidates := commonSkillTargetCandidates()
	want := filepath.Join(home, "custom-codex-skills")
	for _, candidate := range candidates {
		if candidate.Framework == "codex" && candidate.Path == want {
			return
		}
	}
	t.Fatalf("expected expanded codex skill target %s, got %#v", want, candidates)
}

func TestMCPToolsOutputsJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"mcp", "tools"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(payload.Tools) == 0 {
		t.Fatal("expected at least one tool")
	}
}

func TestBootstrapAgentPlanGuidesRdevRecoveryAndRemoteDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	goBin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goBinRdev := filepath.Join(goBin, "rdev")
	if err := os.WriteFile(goBinRdev, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd))

	if err := app.Run(context.Background(), []string{"bootstrap", "agent-plan", "--repo-root", repoRoot, "--framework", "codex", "--remote-requested"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Repo          string `json:"repo"`
		Framework     string `json:"framework"`
		RepoRootValid bool   `json:"repo_root_valid"`
		GoBinRdev     string `json:"go_bin_rdev"`
		LocalMCP      struct {
			Mode       string   `json:"mode"`
			Command    string   `json:"command"`
			Args       []string `json:"args"`
			GatewayURL string   `json:"gateway_url"`
		} `json:"local_mcp"`
		RecoveryOrder []struct {
			ID      string   `json:"id"`
			Status  string   `json:"status"`
			Command []string `json:"command"`
		} `json:"rdev_recovery_order"`
		RemoteDefaults struct {
			Requested          bool     `json:"requested"`
			DefaultUnknownMode string   `json:"default_unknown_owner"`
			OwnedRecurringMode string   `json:"owned_recurring_mode"`
			ThirdPartyMode     string   `json:"third_party_mode"`
			FirstQuestion      string   `json:"first_human_question"`
			AgentShouldDo      []string `json:"agent_should_continue_after_confirmation"`
			SafeDefaults       []string `json:"safe_defaults"`
		} `json:"remote_host_defaults"`
		AskOnlyWhen      []string `json:"ask_only_when"`
		DoNotAskFor      []string `json:"do_not_ask_for"`
		ForbiddenActions []string `json:"forbidden_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid bootstrap plan JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.agent-bootstrap-plan.v1" || payload.Repo != "EitanWong/remote-dev-skillkit" || payload.Framework != "codex" {
		t.Fatalf("unexpected bootstrap plan identity: %#v", payload)
	}
	if !payload.RepoRootValid {
		t.Fatalf("repo root should be valid for checkout bootstrap plan: %s", stdout.String())
	}
	if payload.GoBinRdev != goBinRdev {
		t.Fatalf("expected bootstrap plan to discover GOPATH/bin rdev fallback %s, got %q", goBinRdev, payload.GoBinRdev)
	}
	if payload.LocalMCP.Mode != "stdio" ||
		payload.LocalMCP.Command != goBinRdev ||
		!slices.Contains(payload.LocalMCP.Args, "serve") ||
		payload.LocalMCP.GatewayURL != "optional-for-local-agent-install" {
		t.Fatalf("local agent install should default to MCP stdio without hosted gateway: %#v", payload.LocalMCP)
	}
	var recoveryIDs []string
	for _, action := range payload.RecoveryOrder {
		recoveryIDs = append(recoveryIDs, action.ID)
	}
	for _, expected := range []string{"use-existing-rdev", "use-go-bin-rdev", "build-from-checkout", "run-from-checkout-with-go", "clone-then-build", "signed-release-download"} {
		if !slices.Contains(recoveryIDs, expected) {
			t.Fatalf("missing recovery action %q in %#v", expected, recoveryIDs)
		}
	}
	if !payload.RemoteDefaults.Requested ||
		payload.RemoteDefaults.DefaultUnknownMode != string(model.HostModeAttendedTemporary) ||
		payload.RemoteDefaults.ThirdPartyMode != string(model.HostModeAttendedTemporary) ||
		payload.RemoteDefaults.OwnedRecurringMode != "managed-after-explicit-persistence-authorization" ||
		!strings.Contains(payload.RemoteDefaults.FirstQuestion, "company policy") ||
		!slices.Contains(payload.RemoteDefaults.SafeDefaults, "no hidden persistence") {
		t.Fatalf("remote defaults should collapse decisions into visible temporary support: %#v", payload.RemoteDefaults)
	}
	agentSteps := strings.Join(payload.RemoteDefaults.AgentShouldDo, "\n")
	if !strings.Contains(agentSteps, "rdev.sessions.connect") ||
		!strings.Contains(agentSteps, "target_handoff_envelope.full_text") ||
		strings.Contains(agentSteps, "rdev.support_session.") ||
		strings.Contains(agentSteps, "create an invite") ||
		strings.Contains(agentSteps, "materialize a Connection Entry") {
		t.Fatalf("remote defaults must guide fresh Agents to support-session connect before low-level invite/package flows: %#v", payload.RemoteDefaults.AgentShouldDo)
	}
	joinedAsk := strings.Join(payload.AskOnlyWhen, "\n")
	joinedDontAsk := strings.Join(payload.DoNotAskFor, "\n")
	joinedForbidden := strings.Join(payload.ForbiddenActions, "\n")
	if !strings.Contains(joinedAsk, "company or owner authorization") ||
		!strings.Contains(joinedDontAsk, "target OS before starting the standard support-session connect flow") ||
		!strings.Contains(joinedDontAsk, "ticket code, manifest root, gateway URL") ||
		!strings.Contains(joinedForbidden, "ExecutionPolicy Bypass") {
		t.Fatalf("bootstrap plan should define ask boundaries and forbidden actions:\nask=%s\ndont=%s\nforbidden=%s", joinedAsk, joinedDontAsk, joinedForbidden)
	}
}

func TestAcceptanceFreshAgentSupportSession(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	out := filepath.Join(t.TempDir(), "fresh-agent")

	if err := app.Run(context.Background(), []string{
		"acceptance", "fresh-agent-support-session",
		"--out", out,
		"--gateway-url", "http://127.0.0.1:8787",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		OK     bool   `json:"ok"`
		Schema string `json:"schema"`
		Report string `json:"report"`
		Checks []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"checks"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid acceptance JSON: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance.fresh-agent-support-session.v1" {
		t.Fatalf("unexpected fresh-agent acceptance summary: %#v", payload)
	}
	if _, err := os.Stat(payload.Report); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
	var names []string
	for _, check := range payload.Checks {
		if !check.Passed {
			t.Fatalf("expected passing check: %#v", check)
		}
		names = append(names, check.Name)
	}
	for _, expected := range []string{
		"connect_without_gateway_returns_start_now_command",
		"handoff_without_gateway_prefers_connect_start",
		"handoff_with_gateway_selects_cli_create",
		"auto_activation_connects_first_attended_host",
		"connected_status_has_user_report",
		"waiting_recovery_forbids_custom_scripts",
	} {
		if !slices.Contains(names, expected) {
			t.Fatalf("missing check %q in %#v", expected, names)
		}
	}
	if !strings.Contains(payload.Note, "local contract gate only") {
		t.Fatalf("expected local-gate note, got %q", payload.Note)
	}
}

func TestSupportSessionConnectReturnsForegroundStartWithoutGateway(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{
		"support-session", "connect",
		"--target", "auto",
		"--reason", "company computer support",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion    string   `json:"schema_version"`
		SelectedPath     string   `json:"selected_path"`
		ReadyToSendHuman bool     `json:"ready_to_send_to_human"`
		StartNowCommand  []string `json:"cli_start_now_command"`
		StartCommand     []string `json:"foreground_start_command"`
		AgentNextStep    string   `json:"agent_next_step"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid connect JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-connect.v1" ||
		payload.SelectedPath != "start-foreground-gateway" ||
		payload.ReadyToSendHuman ||
		!slices.Contains(payload.StartNowCommand, "--start") ||
		slices.Contains(payload.StartNowCommand, "--gateway-url") ||
		slices.Contains(payload.StartCommand, "--gateway-url") ||
		!slices.Contains(payload.StartCommand, "start") ||
		!strings.Contains(payload.AgentNextStep, "cli_start_now_command") ||
		!strings.Contains(payload.AgentNextStep, "ready_file.path") ||
		!strings.Contains(payload.AgentNextStep, "status_file.path") {
		t.Fatalf("unexpected foreground connect payload: %#v", payload)
	}
}

func TestCommandGroupHelpListsRemoteControlSurfaces(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"files", "--help"}); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "delete") {
		t.Fatalf("files help should list delete, got stdout=%q stderr=%q", got, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := app.Run(context.Background(), []string{"desktop", "--help"}); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "clipboard") {
		t.Fatalf("desktop help should list clipboard, got stdout=%q stderr=%q", got, stderr.String())
	}
}

func TestSupportSessionSmokeTestRemoteControlCreatesStandardAdapterProbes(t *testing.T) {
	taskCounter := 0
	created := []map[string]any{}
	tasks := []map[string]any{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/sess_remote":
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshot": map[string]any{
				"id": "sess_remote",
				"endpoints": []map[string]any{{
					"id":       "ep_remote",
					"role":     "target",
					"state":    "online",
					"name":     "win-dev",
					"platform": "windows/amd64",
				}},
				"tasks": tasks,
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/sess_remote/tasks":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode task create: %v", err)
			}
			created = append(created, body)
			taskCounter++
			task := map[string]any{
				"id":                 fmt.Sprintf("task_%d", taskCounter),
				"target_endpoint_id": "ep_remote",
				"status":             "succeeded",
				"adapter":            body["adapter"],
				"intent":             body["intent"],
			}
			tasks = append(tasks, task)
			_ = json.NewEncoder(w).Encode(map[string]any{"task": task})
		default:
			t.Fatalf("unexpected smoke-test request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer remote.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"support-session", "smoke-test",
		"--gateway-url", remote.URL,
		"--session-id", "sess_remote",
		"--timeout-seconds", "5",
		"--remote-control",
	})
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid smoke-test JSON: %v\n%s", err, stdout.String())
	}
	audit := report["capability_audit"].(map[string]any)
	if report["remote_control_requested"] != true ||
		audit["remote_control_requested"] != true ||
		int(audit["remote_control_probe_count"].(float64)) != 2 {
		t.Fatalf("expected remote-control smoke metadata, got %#v", report)
	}
	adapters := []string{}
	actions := []string{}
	for _, body := range created {
		adapters = append(adapters, body["adapter"].(string))
		if payload, _ := body["payload"].(map[string]any); payload != nil {
			if action, _ := payload["action"].(string); action != "" {
				actions = append(actions, action)
			}
		}
	}
	if !slices.Contains(adapters, "file") ||
		!slices.Contains(adapters, "desktop") ||
		!slices.Contains(actions, "list") ||
		!slices.Contains(actions, "window.inspect") {
		t.Fatalf("expected standard file/desktop smoke probes, adapters=%#v actions=%#v created=%#v", adapters, actions, created)
	}
}

func TestManagedPublicTunnelRespectsExplicitGatewayURL(t *testing.T) {
	if !shouldStartManagedPublicTunnel(true, "") {
		t.Fatal("expected managed tunnel when public tunnel is needed and no explicit gateway was provided")
	}
	if shouldStartManagedPublicTunnel(true, "https://gateway.example.test") {
		t.Fatal("explicit gateway URL must not be overwritten by managed public tunnel startup")
	}
	if shouldStartManagedPublicTunnel(false, "") {
		t.Fatal("should not start managed tunnel when gateway candidates do not require it")
	}
}

func TestLocalhostRunTunnelIgnoresWelcomePageURLs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake ssh executable uses a POSIX shell script")
	}

	binDir := t.TempDir()
	sshPath := filepath.Join(binDir, "ssh")
	sshOutput := `#!/bin/sh
echo 'To manage custom domains visit https://admin.localhost.run/'
echo 'abc123.lhr.life tunneled with tls termination, https://abc123.lhr.life'
`
	if err := os.WriteFile(sshPath, []byte(sshOutput), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	knownHostsRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	knownHostsPath := filepath.Join(knownHostsRoot, "known_hosts")
	if err := os.WriteFile(knownHostsPath, []byte("localhost.run ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started, err := startLocalhostRunTunnel(context.Background(), io.Discard, "8787", knownHostsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer started.cancel()
	if started.URL != "https://abc123.lhr.life" {
		t.Fatalf("expected assigned tunnel URL, got %q", started.URL)
	}
}

func TestLocalhostRunTunnelURLFromLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "admin page", line: "manage domains at https://admin.localhost.run/", want: ""},
		{name: "assigned lhr", line: "abc123.lhr.life tunneled with tls termination, https://abc123.lhr.life", want: "https://abc123.lhr.life"},
		{name: "assigned localhost run", line: "tunnel ready: https://abc123.localhost.run", want: "https://abc123.localhost.run"},
		{name: "malicious suffix", line: "https://abc123.localhost.run.attacker.example", want: ""},
		{name: "later assigned URL", line: "docs https://admin.localhost.run then tunnel https://abc123.lhr.life", want: "https://abc123.lhr.life"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := localhostRunTunnelURLFromLine(tt.line); got != tt.want {
				t.Fatalf("localhostRunTunnelURLFromLine(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestConfiguredCloudflaredStableTunnelConfigUsesNamedURLAndRedactsToken(t *testing.T) {
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "https://rdev.example.test")
	t.Setenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN", "secret-token")

	cfg, ok, err := configuredCloudflaredStableTunnelConfig("/usr/local/bin/cloudflared", "http://127.0.0.1:8787")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected configured stable cloudflared tunnel")
	}
	if cfg.GatewayURL != "https://rdev.example.test" ||
		cfg.Mode != "token" ||
		!slices.Contains(cfg.Argv, "--token") ||
		!slices.Contains(cfg.Argv, "secret-token") ||
		!slices.Contains(cfg.Argv, "--url") ||
		!slices.Contains(cfg.Argv, "http://127.0.0.1:8787") {
		t.Fatalf("unexpected stable cloudflared config: %#v", cfg)
	}
	if strings.Contains(strings.Join(cfg.Preview, " "), "secret-token") ||
		!strings.Contains(strings.Join(cfg.Preview, " "), "<redacted>") {
		t.Fatalf("preview must redact token: %#v", cfg.Preview)
	}
}

func TestRedactCloudflaredArgvUsesStructuralAllowlist(t *testing.T) {
	argv := []string{
		`C:\Users\Alice\bin\cloudflared.exe`, "tunnel", "--protocol=HTTP2",
		`--url=https://user:password@[2001:db8::5]/?token=query-secret`, "run",
		"--token=secret-token", "--credentials-contents", `{"TunnelSecret":"secret-json"}`,
		"--token-file", `C:\Users\Alice\.cloudflared\token.txt`,
		"--credentials-file=/Users/example/.cloudflared/creds.json",
		"--config", `/Users/example/.cloudflared/config.yml`,
		"--origincert=/Users/example/.cloudflared/cert.pem",
		"--unknown=unknown-secret", "sensitive-tunnel-name",
	}
	original := slices.Clone(argv)
	got := redactCloudflaredArgv(argv)
	want := []string{
		"cloudflared.exe", "tunnel", "--protocol", "http2", "--url", "<local-url>", "run",
		"--token", "<redacted>", "--credentials-contents", "<redacted>",
		"--token-file", "<path>", "--credentials-file", "<path>", "--config", "<path>",
		"--origincert", "<path>", "<option>", "<argument>",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("redactCloudflaredArgv() = %#v, want %#v", got, want)
	}
	if !slices.Equal(argv, original) {
		t.Fatalf("redaction mutated execution argv: got %#v want %#v", argv, original)
	}
	preview := strings.Join(got, " ")
	for _, forbidden := range []string{
		"Alice", "password", "2001:db8", "query-secret", "secret-token", "secret-json",
		"token.txt", "creds.json", "config.yml", "cert.pem", "unknown-secret", "sensitive-tunnel-name",
	} {
		if strings.Contains(preview, forbidden) {
			t.Fatalf("argv preview leaked %q: %q", forbidden, preview)
		}
	}
}

func TestRedactCloudflaredArgvUsesSeparatorNeutralExecutableBase(t *testing.T) {
	for _, path := range []string{
		"/usr/local/bin/cloudflared",
		`C:\Program Files\cloudflared\cloudflared.exe`,
		`\\server\share\cloudflared.exe`,
		`C:\mixed/path\cloudflared.exe`,
	} {
		preview := redactCloudflaredArgv([]string{path})
		if len(preview) != 1 || (preview[0] != "cloudflared" && preview[0] != "cloudflared.exe") {
			t.Fatalf("portable executable preview for %q = %#v", path, preview)
		}
	}
}

func TestConfiguredStableTunnelDoesNotForwardProviderOutput(t *testing.T) {
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "secret-block")
	var stderr synchronizedBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	startedURL, stop, err := startConfiguredCloudflaredStableTunnel(ctx, &stderr, cloudflaredStableTunnelConfig{
		GatewayURL: "https://stable.example.test",
		ProviderID: "cloudflared-named",
		Argv:       []string{os.Args[0], "-test.run=TestTunnelHelperProcess"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if startedURL != "https://stable.example.test" {
		t.Fatalf("stable URL = %q", startedURL)
	}
	logged := stderr.String()
	for _, forbidden := range []string{
		"cf-secret", "ABCD-1234", "203.0.113.9", "2001:db8::9", "/Users/example/private/creds.json",
		"query-secret", "https://abc.trycloudflare.com", "cloudflare-stable",
	} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("stable provider output sentinel %q leaked: %q", forbidden, logged)
		}
	}
	expectedCandidateID := tunnel.CandidateID("cloudflared-named", startedURL)
	if !strings.Contains(logged, `"provider_id":"cloudflared-named"`) ||
		!strings.Contains(logged, `"candidate_id":"`+expectedCandidateID+`"`) {
		t.Fatalf("stable provider lifecycle identity did not match availability identity: %q", logged)
	}
}

func TestConfiguredCloudflaredStableTunnelConfigRejectsShellWrapper(t *testing.T) {
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "https://rdev.example.test")
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON", `["sh","-c","cloudflared tunnel run"]`)

	if _, ok, err := configuredCloudflaredStableTunnelConfig("/usr/local/bin/cloudflared", "http://127.0.0.1:8787"); !ok || err == nil {
		t.Fatalf("expected configured unsafe cloudflared argv to be rejected, ok=%v err=%v", ok, err)
	}
}

func TestParseCloudflaredStableStartArgvAllowsOnlyForegroundTunnelRun(t *testing.T) {
	localURL := "http://127.0.0.1:8787"
	for _, raw := range []string{
		`["cloudflared","tunnel","--protocol","http2","--url","{local_url}","run","prod"]`,
		`["cloudflared","tunnel","--protocol","auto","--url","{local_url}","run","prod"]`,
		`["cloudflared","tunnel","--protocol","quic","--url","{local_url}","run","prod"]`,
		`["cloudflared","tunnel","--protocol=http2","--url={{local_url}}","run","--token=secret"]`,
		`["cloudflared.exe","tunnel","--url","$RDEV_LOCAL_URL","run","--token-file","token.txt"]`,
	} {
		if _, err := parseCloudflaredStableStartArgv(raw, "RDEV_TEST_ARGV", localURL); err != nil {
			t.Fatalf("safe foreground tunnel argv rejected: raw=%s err=%v", raw, err)
		}
	}

	for _, raw := range []string{
		`["cloudflared","service","install"]`,
		`["cloudflared","tunnel","delete","prod"]`,
		`["cloudflared","tunnel","--url","http://127.0.0.1:9999","run","prod"]`,
		`["cloudflared","tunnel","--url","{local_url}"]`,
		`["cloudflared","tunnel","--url","{local_url}","run"]`,
		`["cloudflared","tunnel","--url","{local_url}","run","prod","extra"]`,
		`["cloudflared","tunnel","--url","{local_url}","--config","secret.yml","run","prod"]`,
		`["cloudflared","tunnel","--protocol","h3","--url","{local_url}","run","prod"]`,
		`["cloudflared","tunnel","--url=","run","prod"]`,
		`["cloudflared","tunnel","--url","{local_url}","run","--token="]`,
		`["cloudflared","tunnel","--url","{local_url}","run","--token"]`,
	} {
		if argv, err := parseCloudflaredStableStartArgv(raw, "RDEV_TEST_ARGV", localURL); err == nil {
			t.Fatalf("unsafe or non-foreground argv accepted: %#v", argv)
		}
	}
}

func TestParseCloudflaredStableStartArgvRejectsMalformedBoundaries(t *testing.T) {
	for _, raw := range []string{
		`{`,
		`[]`,
		`["cloudflared",""]`,
		"[\"cloudflared\",\"tunnel\\nrun\"]",
		`["not-cloudflared","tunnel","--url","{local_url}","run","prod"]`,
	} {
		if argv, err := parseCloudflaredStableStartArgv(raw, "RDEV_TEST_ARGV", "http://127.0.0.1:8787"); err == nil {
			t.Fatalf("malformed argv accepted: %#v", argv)
		}
	}
}

func TestExpandCloudflaredStartArgReplacesLongestMarkersFirst(t *testing.T) {
	localURL := "http://127.0.0.1:8787"
	for index := 0; index < 100; index++ {
		for _, input := range []string{"{{local_url}}", "${RDEV_LOCAL_URL}"} {
			if got := expandCloudflaredStartArg(input, localURL); got != localURL {
				t.Fatalf("expandCloudflaredStartArg(%q) = %q", input, got)
			}
		}
	}
}

func TestConfiguredCloudflaredStableTunnelConfigRequiresCanonicalHTTPSOrigin(t *testing.T) {
	for _, rawURL := range []string{
		"http://rdev.example.test",
		"https://user:password@rdev.example.test",
		"https://rdev.example.test:8443",
		"https://rdev.example.test/private",
		"https://rdev.example.test/?token=query-secret",
		"https://rdev.example.test/#fragment-secret",
		"https://*.example.test",
		"https://a..example.test",
		"https://" + strings.Repeat("a", 64) + ".example.test",
	} {
		t.Run(rawURL, func(t *testing.T) {
			t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", rawURL)
			t.Setenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN", "secret-token")
			if _, ok, err := configuredCloudflaredStableTunnelConfig("/usr/local/bin/cloudflared", "http://127.0.0.1:8787"); !ok || err == nil {
				t.Fatalf("credential-bearing or non-origin URL accepted: ok=%v err=%v", ok, err)
			}
		})
	}
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "https://RDEV.EXAMPLE.TEST/")
	t.Setenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN", "secret-token")
	cfg, ok, err := configuredCloudflaredStableTunnelConfig("/usr/local/bin/cloudflared", "http://127.0.0.1:8787")
	if err != nil || !ok || cfg.GatewayURL != "https://rdev.example.test" || cfg.ProviderID != "cloudflared-named" {
		t.Fatalf("canonical stable URL = %#v ok=%v err=%v", cfg, ok, err)
	}
}

func TestGatewayProviderIDMatchesCanonicalOrigins(t *testing.T) {
	candidates := []supportsession.GatewayURLCandidate{{
		Kind: "cloudflared-named", URL: "https://RDEV.EXAMPLE.TEST/",
	}}
	if got := gatewayProviderID(candidates, "https://rdev.example.test"); got != "cloudflared-named" {
		t.Fatalf("gatewayProviderID() = %q", got)
	}
	for _, rawURL := range []string{
		"https://different.example.test",
		"https://rdev.example.test/private",
		"https://rdev.example.test/?token=secret",
	} {
		if got := gatewayProviderID(candidates, rawURL); got != "explicit" {
			t.Fatalf("gatewayProviderID(%q) = %q", rawURL, got)
		}
	}
}

func TestConfiguredCloudflaredStableTunnelConfigUsesURLSourceProviderID(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		namedURL   string
		gatewayURL string
		providerID string
	}{
		{name: "named", namedURL: "HTTPS://NAMED.EXAMPLE.TEST/", providerID: "cloudflared-named"},
		{name: "gateway", gatewayURL: "HTTPS://GATEWAY.EXAMPLE.TEST/", providerID: "cloudflared"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", testCase.namedURL)
			t.Setenv("RDEV_CLOUDFLARED_GATEWAY_URL", testCase.gatewayURL)
			t.Setenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN", "secret-token")
			cfg, ok, err := configuredCloudflaredStableTunnelConfig("cloudflared", "http://127.0.0.1:8787")
			if err != nil || !ok || cfg.ProviderID != testCase.providerID {
				t.Fatalf("configured provider identity = %#v ok=%v err=%v", cfg, ok, err)
			}
		})
	}
}

func TestConfiguredCloudflaredStableTunnelConfigSupportsOnlyReviewedStartModes(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		envName  string
		envValue string
		mode     string
	}{
		{name: "reviewed argv", envName: "RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON", envValue: `["cloudflared","tunnel","--url","{local_url}","run","prod"]`, mode: "configured-start-argv"},
		{name: "token file", envName: "RDEV_CLOUDFLARED_TUNNEL_TOKEN_FILE", envValue: "token.txt", mode: "token-file"},
		{name: "tunnel name", envName: "RDEV_CLOUDFLARED_TUNNEL_NAME", envValue: "prod", mode: "named-tunnel"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			for _, name := range []string{
				"RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON", "RDEV_CLOUDFLARED_START_ARGV_JSON",
				"RDEV_CLOUDFLARED_TUNNEL_TOKEN_FILE", "RDEV_CLOUDFLARED_TUNNEL_TOKEN", "RDEV_CLOUDFLARED_TUNNEL_NAME",
			} {
				t.Setenv(name, "")
			}
			t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "https://rdev.example.test")
			t.Setenv(testCase.envName, testCase.envValue)
			cfg, ok, err := configuredCloudflaredStableTunnelConfig("cloudflared", "http://127.0.0.1:8787")
			if err != nil || !ok || cfg.Mode != testCase.mode {
				t.Fatalf("configured mode = %#v ok=%v err=%v", cfg, ok, err)
			}
		})
	}

	for _, name := range []string{
		"RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "RDEV_CLOUDFLARED_GATEWAY_URL",
		"RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON", "RDEV_CLOUDFLARED_START_ARGV_JSON",
		"RDEV_CLOUDFLARED_TUNNEL_TOKEN_FILE", "RDEV_CLOUDFLARED_TUNNEL_TOKEN", "RDEV_CLOUDFLARED_TUNNEL_NAME",
	} {
		t.Setenv(name, "")
	}
	if cfg, ok, err := configuredCloudflaredStableTunnelConfig("cloudflared", "http://127.0.0.1:8787"); err != nil || ok || len(cfg.Argv) != 0 {
		t.Fatalf("empty stable configuration = %#v ok=%v err=%v", cfg, ok, err)
	}
}

func TestCloudflaredStableProviderIDsFailClosed(t *testing.T) {
	for _, testCase := range []struct {
		value string
		want  string
	}{
		{value: "cloudflared-named", want: "cloudflared-named"},
		{value: "cloudflared", want: "cloudflared"},
		{value: "unsafe provider", want: "cloudflared"},
	} {
		if got := normalizedCloudflaredStableProviderID(testCase.value); got != testCase.want {
			t.Fatalf("normalized provider ID for %q = %q", testCase.value, got)
		}
	}
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "https://named.example.test")
	if got := configuredCloudflaredStableProviderID(); got != "cloudflared-named" {
		t.Fatalf("configured named provider ID = %q", got)
	}
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "")
	if got := configuredCloudflaredStableProviderID(); got != "cloudflared" {
		t.Fatalf("configured generic provider ID = %q", got)
	}
}

func TestConfiguredStableTunnelCancellationReapsProcess(t *testing.T) {
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "block")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, _, err := startConfiguredCloudflaredStableTunnelWithGrace(ctx, io.Discard, cloudflaredStableTunnelConfig{
			GatewayURL: "https://stable.example.test", ProviderID: "cloudflared",
			Argv: []string{os.Args[0], "-test.run=TestTunnelHelperProcess"},
		}, time.Second)
		result <- err
	}()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stable cancellation error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stable tunnel cancellation did not reap the provider process")
	}
}

func TestFirstStableGatewayURLRejectsCredentialBearingCloudflaredCandidate(t *testing.T) {
	candidates := []supportsession.GatewayURLCandidate{
		{Kind: "cloudflared-named", URL: "https://user:password@secret.example.test/?token=query-secret"},
		{Kind: "cloudflared", URL: "https://SAFE.EXAMPLE.TEST/"},
	}
	if got := firstStableGatewayURL(candidates); got != "https://safe.example.test" {
		t.Fatalf("firstStableGatewayURL() = %q", got)
	}
}

func TestCloudflaredStableTunnelStartRequestedRequiresURLAndStartConfig(t *testing.T) {
	if cloudflaredStableTunnelStartRequested() {
		t.Fatal("empty environment should not request stable tunnel startup")
	}
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "https://rdev.example.test")
	if cloudflaredStableTunnelStartRequested() {
		t.Fatal("stable URL without start config should be treated as externally managed")
	}
	t.Setenv("RDEV_CLOUDFLARED_TUNNEL_NAME", "rdev")
	if !cloudflaredStableTunnelStartRequested() {
		t.Fatal("stable URL with tunnel name should request managed stable tunnel startup")
	}
}

func TestSupportSessionForegroundEventIsMachineReadable(t *testing.T) {
	var out synchronizedBuffer
	statusFile := filepath.Join(t.TempDir(), "support-session-status.json")
	writeSupportSessionEvent(&out, statusFile, "connected", map[string]any{
		"schema_version": "rdev.support-session-status.v1",
		"connected":      true,
		"status":         "connected",
		"ticket_code":    "ABCD-1234",
		"gateway_url":    "https://gateway.example.test/?token=query-secret",
		"peer_ip":        "203.0.113.9",
		"peer_ipv6":      "2001:db8::9",
		"artifact_path":  "/Users/example/private/status.json",
	})

	const prefix = "rdev support session event: "
	line := strings.TrimSpace(out.String())
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("expected event prefix, got %q", line)
	}
	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Event         string `json:"event"`
		StatusClass   string `json:"status_class"`
		Connected     bool   `json:"connected"`
		ActionClass   string `json:"action_class"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &payload); err != nil {
		t.Fatalf("invalid event JSON: %v\n%s", err, line)
	}
	if payload.SchemaVersion != "rdev.support-session-foreground-log-event.v1" ||
		payload.Event != "connected" ||
		payload.StatusClass != "connected" || !payload.Connected ||
		payload.ActionClass != "report-connection-established" {
		t.Fatalf("unexpected event payload: %#v", payload)
	}
	for _, forbidden := range []string{
		"ABCD-1234", "gateway.example.test", "query-secret", "203.0.113.9", "2001:db8::9", "/Users/example/private/status.json",
	} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("stderr event leaked %q: %s", forbidden, line)
		}
	}
	statusBytes, err := os.ReadFile(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	var statusPayload struct {
		SchemaVersion string         `json:"schema_version"`
		Event         string         `json:"event"`
		Status        map[string]any `json:"status"`
		AgentRule     string         `json:"agent_rule"`
	}
	if err := json.Unmarshal(statusBytes, &statusPayload); err != nil {
		t.Fatalf("invalid status file JSON: %v\n%s", err, string(statusBytes))
	}
	if statusPayload.SchemaVersion != "rdev.support-session-foreground-event.v1" ||
		statusPayload.Event != "connected" ||
		statusPayload.Status["connected"] != true ||
		statusPayload.Status["ticket_code"] != "ABCD-1234" ||
		!strings.Contains(statusPayload.AgentRule, "connected_next_steps.user_report") {
		t.Fatalf("unexpected status file payload: %#v", statusPayload)
	}
	info, err := os.Stat(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected status file permissions 0600, got %v", info.Mode().Perm())
	}
}

func TestSupportSessionBlockedReadinessDiagnosticDoesNotEmbedHandoff(t *testing.T) {
	registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{
		id: "blocked-readiness", url: "https://blocked.example.test/?token=query-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(t.TempDir(), "private-work-dir")
	statusFile := filepath.Join(workDir, "private-status.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true, InstanceMarker: "instance-marker-secret"}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { return nil },
		FinalProbe:     func(context.Context, tunnel.Candidate, string, string) error { return nil },
	}
	err = app.supportSessionStart(context.Background(), supportSessionStartOptions{
		RepoRoot: ".", Addr: supportSessionTestAddr(t), WorkDir: workDir, StatusFile: statusFile,
		Target: "windows", Reason: "blocked diagnostic", TTLSeconds: 60, Locale: "en",
	})
	if err == nil || !strings.Contains(err.Error(), "readiness policy") {
		t.Fatalf("expected blocked readiness error, got %v", err)
	}
	statusBytes, readErr := os.ReadFile(statusFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for surface, content := range map[string][]byte{"stdout": stdout.Bytes(), "status": statusBytes} {
		for _, forbidden := range []string{
			"started", "availability_readiness", "query-secret", "blocked.example.test", "instance-marker-secret",
			"private-work-dir", "private-status.json", "ticket_code", "/join/",
		} {
			if bytes.Contains(content, []byte(forbidden)) {
				t.Fatalf("%s diagnostic leaked %q: %s", surface, forbidden, content)
			}
		}
	}
}

func TestSupportSessionForegroundWatcherWritesConnectedStatusFile(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicketWithMetadata(
		model.HostModeAttendedTemporary,
		600,
		cliPolicyCapabilitiesToStrings(policy.TemporaryDefaults()),
		"foreground status file watcher test",
		map[string]string{"auto_activate": "attended-temporary"},
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var out synchronizedBuffer
	statusFile := filepath.Join(t.TempDir(), "support-session-status.json")
	connectedReportFile := filepath.Join(t.TempDir(), "connected-report.txt")
	gatewayURL := "http://127.0.0.1:9876"
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSession(ctx, &out, statusFile, connectedReportFile, gw, ticket.Code, "en", gatewayURL)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("foreground status watcher did not stop during cleanup")
		}
	})

	waitForStatusFileEvent(t, statusFile, "waiting")
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "fresh-agent-connected-host",
		OS:           "linux",
		Arch:         "amd64",
		Capabilities: ticket.Capabilities,
	}); err != nil {
		t.Fatal(err)
	}
	statusPayload := waitForStatusFileEvent(t, statusFile, "connected")
	status := statusPayload.Status
	connectedNext := status["connected_next_steps"].(map[string]any)
	mcpNextCalls := connectedNext["mcp_next_calls"].([]any)
	mcpNextArgs := mcpNextCalls[0].(map[string]any)["arguments"].(map[string]any)
	if statusPayload.SchemaVersion != "rdev.support-session-foreground-event.v1" ||
		status["schema_version"] != "rdev.support-session-status.v1" ||
		status["connected"] != true ||
		mcpNextArgs["gateway_url"] != gatewayURL ||
		mcpNextArgs["session_id"] != "<session-id>" ||
		!strings.Contains(connectedNext["user_report"].(string), "Connection established") ||
		!strings.Contains(statusPayload.AgentRule, "connected_next_steps.user_report") {
		t.Fatalf("expected connected status event in status file, got %#v", statusPayload)
	}
	if !waitForBufferContains(&out, `"event":"connected"`, 3*time.Second) {
		t.Fatalf("expected connected stderr event, got %s", out.String())
	}
	for _, forbidden := range []string{gatewayURL, ticket.Code, "fresh-agent-connected-host", `"mcp_next_calls"`, `"gateway_url"`} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("connected stderr event leaked %q: %s", forbidden, out.String())
		}
	}
	report, err := os.ReadFile(connectedReportFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "Connection established") {
		t.Fatalf("expected connected report text, got %q", string(report))
	}
}

func TestSupportSessionStatusTimeoutKeepsDownloadGuidance(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/support-session/status" ||
			r.URL.Query().Get("ticket_code") != "WAIT-1234" {
			t.Fatalf("unexpected status request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schema_version":"rdev.support-session-status.v1",
			"ok":true,
			"ticket_code":"WAIT-1234",
			"status":"target-downloading",
			"connected":false,
			"waiting":false,
			"next_action":"Keep waiting for the download to finish; do not misdiagnose this as the target command not running.",
			"target_preconnect_summary":{
				"status":"target-downloading",
				"phase":"downloading-core",
				"agent_interpretation":"The target command reached the gateway and rdev-bootstrap is downloading the verified core runtime; this is not disconnected or user inaction."
			}
		}` + "\n"))
	}))
	defer remote.Close()

	status, err := supportSessionStatus(context.Background(), remote.Client(), supportSessionStatusOptions{
		GatewayURL: remote.URL,
		TicketCode: "WAIT-1234",
		Locale:     "en",
		Wait:       true,
		Timeout:    time.Nanosecond,
		Interval:   time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status["timed_out"] != true ||
		status["ok"] != false ||
		status["status"] != "target-downloading" ||
		!strings.Contains(status["next_action"].(string), "download") ||
		strings.Contains(status["next_action"].(string), "target command output") {
		t.Fatalf("expected timed-out download guidance to preserve preconnect context, got %#v", status)
	}
	recovery := status["connection_recovery"].(map[string]any)
	actions := strings.Join(stringsFromAny(recovery["agent_next_actions"]), "\n")
	if !strings.Contains(actions, "download") ||
		strings.Contains(actions, "command window") {
		t.Fatalf("expected recovery to focus on stalled helper download, got %#v", recovery)
	}
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		return anyStrings(typed)
	default:
		return nil
	}
}

type supportSessionStatusFileEvent struct {
	SchemaVersion string         `json:"schema_version"`
	Event         string         `json:"event"`
	Status        map[string]any `json:"status"`
	AgentRule     string         `json:"agent_rule"`
}

func waitForStatusFileEvent(t *testing.T, path, event string) supportSessionStatusFileEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			last = string(data)
			var payload supportSessionStatusFileEvent
			if err := json.Unmarshal(data, &payload); err == nil && payload.Event == event {
				return payload
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status file event %q at %s; last=%s", event, path, last)
	return supportSessionStatusFileEvent{}
}

func waitForBufferContains(buf interface{ String() string }, needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), needle) {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return strings.Contains(buf.String(), needle)
}

func TestSupportSessionPlanStandardizesOneCommandConnection(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd))
	workDir := filepath.Join(t.TempDir(), "support")

	if err := app.Run(context.Background(), []string{
		"support-session", "plan",
		"--repo-root", repoRoot,
		"--work-dir", workDir,
		"--gateway-url", "http://192.0.2.10:8787",
		"--target", "windows",
		"--reason", "company computer support",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion        string `json:"schema_version"`
		GatewayURL           string `json:"gateway_url"`
		GatewayURLCandidates []struct {
			URL         string `json:"url"`
			Kind        string `json:"kind"`
			Recommended bool   `json:"recommended"`
		} `json:"gateway_url_candidates"`
		AutoActivate struct {
			Enabled      bool     `json:"enabled"`
			Scope        string   `json:"scope"`
			Capabilities []string `json:"capabilities"`
		} `json:"auto_activate"`
		Commands map[string][]string `json:"commands"`
		Target   struct {
			Windows      string   `json:"windows"`
			HumanSurface []string `json:"human_receives_only"`
		} `json:"target_user_instructions"`
		Forbidden []string `json:"forbidden"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session plan JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-plan.v1" || !payload.AutoActivate.Enabled {
		t.Fatalf("expected standard support session plan with auto authorization: %#v", payload)
	}
	if payload.GatewayURL != "http://192.0.2.10:8787" ||
		len(payload.GatewayURLCandidates) == 0 ||
		payload.GatewayURLCandidates[0].URL != payload.GatewayURL ||
		!payload.GatewayURLCandidates[0].Recommended {
		t.Fatalf("plan should expose recommended gateway URL candidates: %#v", payload.GatewayURLCandidates)
	}
	if !strings.Contains(payload.AutoActivate.Scope, "attended-temporary") ||
		!slices.Contains(payload.AutoActivate.Capabilities, "shell.user") {
		t.Fatalf("auto authorization should be scoped and minimal: %#v", payload.AutoActivate)
	}
	startGateway := strings.Join(payload.Commands["start_gateway"], "\x00")
	if !strings.Contains(startGateway, "--rdev-bootstrap-windows-amd64") ||
		!strings.Contains(startGateway, "--manifest-signing-key") ||
		!strings.Contains(startGateway, "--state") {
		t.Fatalf("gateway plan should carry assets and durable state: %#v", payload.Commands["start_gateway"])
	}
	createInviteHTTP := strings.Join(payload.Commands["create_invite_http"], "\n")
	if !strings.Contains(createInviteHTTP, `"auto_activate":true`) ||
		!strings.Contains(createInviteHTTP, `"mode":"attended-temporary"`) {
		t.Fatalf("invite command should create auto-activated attended temporary ticket: %s", createInviteHTTP)
	}
	if strings.Contains(payload.Target.Windows, "ExecutionPolicy Bypass") ||
		!strings.Contains(payload.Target.Windows, "bootstrap.ps1") ||
		!slices.Contains(payload.Target.HumanSurface, "visible one-line script") ||
		!slices.Contains(payload.Forbidden, "unverified binary download") {
		t.Fatalf("target instructions should be one visible safe command: %#v", payload.Target)
	}
}

func TestSupportSessionHandoffSelectsCreateWhenGatewayExists(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{
		"support-session", "handoff",
		"--gateway-url", "http://192.0.2.10:8787",
		"--target", "windows",
		"--reason", "company computer support",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion    string         `json:"schema_version"`
		SelectedPath     string         `json:"selected_path"`
		MCPNextTool      string         `json:"mcp_next_tool"`
		MCPNextArguments map[string]any `json:"mcp_next_arguments"`
		CLICreateCommand []string       `json:"cli_create_command"`
		AgentNextStep    string         `json:"agent_next_step"`
		Forbidden        []string       `json:"forbidden"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session handoff JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-handoff.v1" ||
		payload.SelectedPath != "create-with-reachable-gateway" ||
		payload.MCPNextTool != "" ||
		len(payload.MCPNextArguments) != 0 ||
		!strings.Contains(strings.Join(payload.CLICreateCommand, "\x00"), "support-session\x00create") ||
		!slices.Contains(payload.CLICreateCommand, "http://192.0.2.10:8787") ||
		!slices.Contains(payload.CLICreateCommand, "windows") ||
		!strings.Contains(payload.AgentNextStep, "target_handoff_envelope.full_text") ||
		!slices.Contains(payload.Forbidden, "Agent-authored PowerShell or shell bootstrap/recovery scripts") {
		t.Fatalf("expected create handoff route, got %#v", payload)
	}
}

func TestSupportSessionHandoffSelectsForegroundStartWithoutGateway(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"support-session", "handoff", "--target", "auto"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion          string   `json:"schema_version"`
		SelectedPath           string   `json:"selected_path"`
		MCPNextTool            string   `json:"mcp_next_tool"`
		ForegroundStartCommand []string `json:"foreground_start_command"`
		AgentRule              string   `json:"agent_rule"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session handoff JSON: %v\n%s", err, stdout.String())
	}
	startCommand := strings.Join(payload.ForegroundStartCommand, "\x00")
	if payload.SchemaVersion != "rdev.support-session-handoff.v1" ||
		payload.SelectedPath != "start-foreground-gateway" ||
		payload.MCPNextTool != "" ||
		!strings.Contains(startCommand, "support-session\x00start") ||
		!strings.Contains(payload.AgentRule, "do not choose support-session plan") {
		t.Fatalf("expected foreground start handoff route, got %#v", payload)
	}
}

func TestSupportSessionHandoffAutoDetectsStableRdevCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable bits for the fallback binary fixture")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	goBin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goBinRdev := filepath.Join(goBin, "rdev")
	if err := os.WriteFile(goBinRdev, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"support-session", "handoff", "--target", "auto"}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		CliStartNowCommand []string `json:"cli_start_now_command"`
		PrepareCommand     []string `json:"prepare_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session handoff JSON: %v\n%s", err, stdout.String())
	}
	if len(payload.CliStartNowCommand) == 0 || payload.CliStartNowCommand[0] != goBinRdev {
		t.Fatalf("expected auto-detected stable rdev in cli_start_now_command, got %#v", payload.CliStartNowCommand)
	}
	if len(payload.PrepareCommand) == 0 || payload.PrepareCommand[0] != goBinRdev {
		t.Fatalf("expected auto-detected stable rdev in prepare_command, got %#v", payload.PrepareCommand)
	}
}

func TestSupportSessionHandoffUsesConfiguredGatewayWithoutExplicitURL(t *testing.T) {
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", "https://hosted.example.test/rdev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"support-session", "handoff", "--target", "auto"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion    string         `json:"schema_version"`
		SelectedPath     string         `json:"selected_path"`
		MCPNextTool      string         `json:"mcp_next_tool"`
		GatewayURL       string         `json:"gateway_url"`
		MCPNextArguments map[string]any `json:"mcp_next_arguments"`
		CLICreateCommand []string       `json:"cli_create_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session handoff JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-handoff.v1" ||
		payload.SelectedPath != "create-with-reachable-gateway" ||
		payload.MCPNextTool != "" ||
		payload.GatewayURL != "https://hosted.example.test/rdev" ||
		len(payload.MCPNextArguments) != 0 ||
		!slices.Contains(payload.CLICreateCommand, "https://hosted.example.test/rdev") {
		t.Fatalf("expected configured gateway handoff route, got %#v", payload)
	}
}

func TestSupportSessionCleanupDryRunBuildsAuthorizationGatedDeletePlan(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{
		"support-session", "cleanup",
		"--path", "rdev-audit/remote-control-upload.txt",
		"--workspace-root", "~",
		"--write-scope", "rdev-audit",
		"--reason", "cleanup test upload",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion         string         `json:"schema_version"`
		DryRun                bool           `json:"dry_run"`
		AuthorizationRequired []string       `json:"authorization_required"`
		CleanupTaskPreview    map[string]any `json:"cleanup_task_preview"`
		Safety                string         `json:"safety"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid cleanup dry-run JSON: %v\n%s", err, stdout.String())
	}
	policyPayload := payload.CleanupTaskPreview["payload"].(map[string]any)
	authorizations := policyPayload["authorizations_required"].([]any)
	writeScope := policyPayload["write_scope"].([]any)
	if payload.SchemaVersion != "rdev.support-session-cleanup-plan.v1" ||
		!payload.DryRun ||
		payload.CleanupTaskPreview["adapter"] != "file" ||
		policyPayload["action"] != "delete" ||
		policyPayload["path"] != "rdev-audit/remote-control-upload.txt" ||
		len(authorizations) != 1 || authorizations[0] != "file.delete" ||
		len(writeScope) != 1 || writeScope[0] != "rdev-audit" ||
		!strings.Contains(payload.Safety, "No deletion is performed") {
		t.Fatalf("unexpected cleanup dry-run payload: %#v", payload)
	}
}

func TestSupportSessionCleanupExecuteCreatesAuthorizationGatedDeleteTask(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/sess_1/tasks" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"task":{"id":"task_cleanup","status":"offered"}}`))
	}))
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"support-session", "cleanup",
		"--execute",
		"--gateway-url", server.URL,
		"--session-id", "sess_1",
		"--path", "rdev-audit/remote-control-upload.txt",
		"--write-scope", "rdev-audit",
	}); err != nil {
		t.Fatal(err)
	}

	policy := received["payload"].(map[string]any)
	authorizations := policy["authorizations_required"].([]any)
	if received["adapter"] != "file" ||
		policy["action"] != "delete" ||
		policy["path"] != "rdev-audit/remote-control-upload.txt" ||
		len(authorizations) != 1 || authorizations[0] != "file.delete" {
		t.Fatalf("unexpected cleanup task create payload: %#v", received)
	}
	var payload struct {
		OK           bool           `json:"ok"`
		TaskResponse map[string]any `json:"task_response"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid cleanup execute JSON: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.TaskResponse["task"] == nil {
		t.Fatalf("expected task response, got %#v", payload)
	}
}

func TestSupportSessionCleanupRejectsBroadPath(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"support-session", "cleanup",
		"--path", ".",
	})
	if err == nil || !strings.Contains(err.Error(), "specific file or directory") {
		t.Fatalf("expected broad cleanup path rejection, got err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestSupportSessionLiveE2EDefaultsToDryRunPlan(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{
		"support-session", "live-e2e",
		"--gateway-url", "https://gateway.example.test/rdev/",
		"--ticket-code", "ABCD-1234",
		"--host-id", "hst_1",
		"--session-id", "ses_1",
		"--target-endpoint-id", "ep_1",
		"--rdev-command", "rdev-test",
		"--timeout-seconds", "180",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		DryRun        bool   `json:"dry_run"`
		Execute       bool   `json:"execute"`
		GatewayURL    string `json:"gateway_url"`
		TicketCode    string `json:"ticket_code"`
		HostID        string `json:"host_id"`
		SessionID     string `json:"session_id"`
		EndpointID    string `json:"target_endpoint_id"`
		TargetOS      string `json:"target_os"`
		TimeoutSec    int    `json:"timeout_seconds"`
		Gates         []struct {
			Name             string         `json:"name"`
			Status           string         `json:"status"`
			TargetOS         string         `json:"target_os"`
			ProofCommand     []string       `json:"proof_command"`
			ProofCommands    map[string]any `json:"proof_commands"`
			MCPTool          string         `json:"mcp_tool"`
			MCPArguments     map[string]any `json:"mcp_arguments"`
			RequiredEvidence []string       `json:"required_evidence"`
		} `json:"gates"`
		Safety []string `json:"safety"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid live-e2e plan JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-live-e2e-plan.v1" ||
		!payload.DryRun ||
		payload.Execute ||
		payload.GatewayURL != "https://gateway.example.test/rdev" ||
		payload.TicketCode != "ABCD-1234" ||
		payload.HostID != "hst_1" ||
		payload.SessionID != "ses_1" ||
		payload.EndpointID != "ep_1" ||
		payload.TargetOS != "windows" ||
		payload.TimeoutSec != 180 ||
		len(payload.Gates) != 3 {
		t.Fatalf("unexpected live-e2e plan header: %#v", payload)
	}
	gates := map[string]struct {
		status           string
		proofCommand     []string
		proofCommands    map[string]any
		mcpTool          string
		mcpArguments     map[string]any
		requiredEvidence []string
	}{}
	for _, gate := range payload.Gates {
		gates[gate.Name] = struct {
			status           string
			proofCommand     []string
			proofCommands    map[string]any
			mcpTool          string
			mcpArguments     map[string]any
			requiredEvidence []string
		}{
			status:           gate.Status,
			proofCommand:     gate.ProofCommand,
			proofCommands:    gate.ProofCommands,
			mcpTool:          gate.MCPTool,
			mcpArguments:     gate.MCPArguments,
			requiredEvidence: gate.RequiredEvidence,
		}
		if gate.TargetOS != "windows" || gate.Status != "requires_real_environment" {
			t.Fatalf("gate should remain live Windows only until executed: %#v", gate)
		}
	}
	smoke := gates["windows_support_session_smoke_remote_control"]
	if !slices.Equal(smoke.proofCommand, []string{
		"rdev-test", "support-session", "smoke-test",
		"--gateway-url", "https://gateway.example.test/rdev",
		"--session-id", "ses_1",
		"--target-endpoint-id", "ep_1",
		"--ticket-code", "ABCD-1234",
		"--remote-control",
		"--timeout-seconds", "180",
	}) || smoke.mcpTool != "" || len(smoke.mcpArguments) != 0 {
		t.Fatalf("unexpected smoke gate: %#v", smoke)
	}
	transfer := gates["windows_file_transfer_byte_compare"]
	upload, _ := transfer.proofCommands["upload"].([]any)
	download, _ := transfer.proofCommands["download"].([]any)
	if len(upload) == 0 || len(download) == 0 ||
		upload[0] != "rdev-test" ||
		download[0] != "rdev-test" ||
		!slices.Contains(transfer.requiredEvidence, "byte_compare=match") ||
		!slices.Contains(transfer.requiredEvidence, "expected_sha256 equals actual_sha256") {
		t.Fatalf("unexpected file transfer gate: %#v", transfer)
	}
	interrupt := gates["windows_session_interrupt_flow"]
	if interrupt.mcpTool != "rdev.sessions.interrupt" ||
		!slices.Contains(interrupt.requiredEvidence, "rdev.sessions.events replays the interrupt after reconnect") {
		t.Fatalf("unexpected interrupt gate: %#v", interrupt)
	}
	if !slices.Contains(payload.Safety, "default dry-run creates no remote tasks") ||
		!slices.Contains(payload.Safety, "interrupt/pause/cancel must be expressed through Control Plane session events, not a separate task authorization subsystem") {
		t.Fatalf("expected dry-run and interrupt safety rules, got %#v", payload.Safety)
	}
}

func TestSupportSessionPlanDefaultGatewayDoesNotUseWildcardURL(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"support-session", "plan", "--addr", "0.0.0.0:8787"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		GatewayURL           string `json:"gateway_url"`
		GatewayURLCandidates []struct {
			URL         string `json:"url"`
			Recommended bool   `json:"recommended"`
		} `json:"gateway_url_candidates"`
		Commands map[string][]string `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session plan JSON: %v\n%s", err, stdout.String())
	}
	if payload.GatewayURL == "" ||
		strings.Contains(payload.GatewayURL, "0.0.0.0") ||
		strings.Contains(payload.GatewayURL, "[::]") {
		t.Fatalf("gateway URL should be target-usable, got %q", payload.GatewayURL)
	}
	createInvite := strings.Join(payload.Commands["create_invite_cli"], "\x00")
	watch := strings.Join(payload.Commands["watch_connection_status"], "\x00")
	if strings.Contains(createInvite, "0.0.0.0") || strings.Contains(watch, "0.0.0.0") {
		t.Fatalf("target-facing commands must not contain wildcard gateway URLs:\ncreate=%s\nwatch=%s", createInvite, watch)
	}
	if len(payload.GatewayURLCandidates) == 0 || !payload.GatewayURLCandidates[0].Recommended {
		t.Fatalf("expected a recommended gateway candidate, got %#v", payload.GatewayURLCandidates)
	}
}

func TestSupportSessionPrepareBuildsBootstrapAssetsForOneCommandTargets(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd))
	workDir := filepath.Join(t.TempDir(), "support")

	if err := app.Run(context.Background(), []string{
		"support-session", "prepare",
		"--repo-root", repoRoot,
		"--work-dir", workDir,
		"--gateway-url", "http://127.0.0.1:8787",
		"--target", "windows",
		"--build-assets",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion       string `json:"schema_version"`
		RepoRootValid       bool   `json:"repo_root_valid"`
		ConnectionReadiness struct {
			Ready                     bool `json:"ready"`
			TargetBootstrapSelfRepair bool `json:"target_bootstrap_self_repair"`
			HumanGetsOneCommand       bool `json:"human_gets_one_command"`
		} `json:"connection_readiness"`
		ConnectivityStrategy struct {
			SchemaVersion      string   `json:"schema_version"`
			SelectionOrder     []string `json:"selection_order"`
			AutomaticDowngrade []string `json:"automatic_downgrade"`
		} `json:"connectivity_strategy"`
		GatewayCandidatePreflight struct {
			SchemaVersion  string `json:"schema_version"`
			PreflightMode  string `json:"preflight_mode"`
			CandidateCount int    `json:"candidate_count"`
			AgentRule      string `json:"agent_rule"`
		} `json:"gateway_candidate_preflight"`
		AgentConnectionRunbook struct {
			SchemaVersion string   `json:"schema_version"`
			Phase         string   `json:"phase"`
			Sequence      []string `json:"sequence"`
		} `json:"agent_connection_runbook"`
		AssetReport struct {
			SchemaVersion            string `json:"schema_version"`
			AllReady                 bool   `json:"all_ready"`
			BuildAssets              bool   `json:"build_assets"`
			DownloadBudgetBytes      int64  `json:"download_budget_bytes"`
			AllGzipWithinBudget      bool   `json:"all_gzip_within_budget"`
			BootstrapTargetBytes     int64  `json:"bootstrap_target_bytes"`
			FirstConnectSizeStrategy string `json:"first_connect_size_strategy"`
			Assets                   []struct {
				ID                   string `json:"id"`
				Present              bool   `json:"present"`
				BuildStatus          string `json:"build_status"`
				SHA256               string `json:"sha256"`
				SizeBytes            int64  `json:"size_bytes"`
				GzipAssetURL         string `json:"gzip_asset_url"`
				GzipEstimatedBytes   int64  `json:"gzip_estimated_bytes"`
				GzipBudgetBytes      int64  `json:"gzip_budget_bytes"`
				GzipWithinBudget     bool   `json:"gzip_within_budget"`
				BootstrapTargetBytes int64  `json:"bootstrap_target_bytes"`
			} `json:"assets"`
		} `json:"asset_report"`
		StandardRecovery []string `json:"standard_recovery"`
		Forbidden        []string `json:"forbidden"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid prepare JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-prepare.v1" ||
		!payload.RepoRootValid ||
		!payload.ConnectionReadiness.Ready ||
		!payload.ConnectionReadiness.TargetBootstrapSelfRepair ||
		!payload.ConnectionReadiness.HumanGetsOneCommand ||
		payload.ConnectivityStrategy.SchemaVersion != "rdev.support-session-connectivity-strategy.v1" ||
		!slices.Contains(payload.ConnectivityStrategy.SelectionOrder, "native-lan-gateway") ||
		!slices.Contains(payload.ConnectivityStrategy.SelectionOrder, "existing-frp-or-chisel-relay") ||
		payload.GatewayCandidatePreflight.SchemaVersion != "rdev.support-session-gateway-candidate-preflight.v1" ||
		payload.GatewayCandidatePreflight.PreflightMode != "local-classification-no-network-scan" ||
		payload.GatewayCandidatePreflight.CandidateCount == 0 ||
		!strings.Contains(payload.GatewayCandidatePreflight.AgentRule, "target command owns ordered URL fallback") ||
		payload.AgentConnectionRunbook.SchemaVersion != "rdev.support-session-agent-runbook.v1" ||
		payload.AgentConnectionRunbook.Phase != "prepare" ||
		!slices.Contains(payload.AgentConnectionRunbook.Sequence, "send only target_handoff_envelope.full_text to the target-side human") ||
		!payload.AssetReport.AllReady ||
		!payload.AssetReport.BuildAssets ||
		payload.AssetReport.DownloadBudgetBytes <= 0 ||
		!payload.AssetReport.AllGzipWithinBudget ||
		payload.AssetReport.BootstrapTargetBytes <= 0 ||
		!strings.Contains(payload.AssetReport.FirstConnectSizeStrategy, "1 MiB") ||
		!strings.Contains(payload.AssetReport.FirstConnectSizeStrategy, "rdev-bootstrap") ||
		len(payload.AssetReport.Assets) != 6 {
		t.Fatalf("unexpected prepare payload: %#v", payload)
	}
	for _, asset := range payload.AssetReport.Assets {
		if !asset.Present ||
			asset.SHA256 == "" ||
			(asset.BuildStatus != "built" && asset.BuildStatus != "not-requested") ||
			asset.SizeBytes <= 0 ||
			asset.GzipEstimatedBytes <= 0 ||
			asset.GzipBudgetBytes <= 0 ||
			asset.BootstrapTargetBytes <= 0 ||
			!asset.GzipWithinBudget ||
			!strings.HasSuffix(asset.GzipAssetURL, ".gz") {
			t.Fatalf("expected present hashed asset, got %#v", asset)
		}
	}
	if !slices.Contains(payload.Forbidden, "ad hoc bootstrap code") ||
		!slices.Contains(payload.StandardRecovery, "do not write custom PowerShell, relay, activation polling, ticket substitution, or bootstrap glue") {
		t.Fatalf("expected standard guardrails, got recovery=%#v forbidden=%#v", payload.StandardRecovery, payload.Forbidden)
	}
}

func TestSupportSessionCreateReturnsReadyTargetCommandAndWatcher(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "https://relay.example.test/rdev")
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{
		"support-session", "create",
		"--gateway-url", server.URL,
		"--target", "windows",
		"--reason", "company computer support",
		"--locale", "zh-CN",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion        string `json:"schema_version"`
		TicketCode           string `json:"ticket_code"`
		GatewayURLCandidates []struct {
			URL         string `json:"url"`
			Kind        string `json:"kind"`
			Source      string `json:"source"`
			Recommended bool   `json:"recommended"`
		} `json:"gateway_url_candidates"`
		TargetCommand         string            `json:"target_command"`
		TargetCommands        map[string]string `json:"target_commands"`
		WatchConnectionStatus []string          `json:"watch_connection_status"`
		UserHandoff           struct {
			SchemaVersion string `json:"schema_version"`
			CopyPasteKind string `json:"copy_paste_kind"`
			CopyPaste     string `json:"copy_paste"`
			Message       string `json:"message"`
			AgentNextStep string `json:"agent_next_step"`
		} `json:"user_handoff"`
		ConnectionAttemptPolicy struct {
			SchemaVersion             string `json:"schema_version"`
			WindowsDownloadTimeoutSec int    `json:"windows_download_timeout_sec"`
			CurlConnectTimeoutSec     int    `json:"curl_connect_timeout_sec"`
			CurlMaxTimeSec            int    `json:"curl_max_time_sec"`
			RetriesPerCandidate       int    `json:"retries_per_candidate"`
		} `json:"connection_attempt_policy"`
		ConnectionContinuityPolicy struct {
			SchemaVersion               string   `json:"schema_version"`
			StableAfterLANChange        bool     `json:"stable_after_lan_change"`
			HasStableConfiguredFallback bool     `json:"has_stable_configured_fallback"`
			StableFallbackKinds         []string `json:"stable_fallback_kinds"`
			Assessment                  string   `json:"assessment"`
			AgentRule                   string   `json:"agent_rule"`
		} `json:"connection_continuity_policy"`
		TargetBootstrapRequirements struct {
			SchemaVersion  string   `json:"schema_version"`
			RequiredAssets []string `json:"required_assets"`
			StandardFix    []string `json:"standard_fix"`
			Forbidden      []string `json:"forbidden"`
		} `json:"target_bootstrap_requirements"`
		TargetBootstrapReadiness struct {
			SchemaVersion string `json:"schema_version"`
			AllReady      bool   `json:"all_ready"`
			AgentRule     string `json:"agent_rule"`
		} `json:"target_bootstrap_readiness"`
		WatchConnectionStatusConfiguredGateway struct {
			Command   []string `json:"command"`
			AgentRule string   `json:"agent_rule"`
		} `json:"watch_connection_status_configured_gateway"`
		RecommendedSurface    string `json:"recommended_surface"`
		AutoActivate          bool   `json:"auto_activate"`
		ManifestRootPublicKey string `json:"manifest_root_public_key"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid create JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-created.v1" ||
		payload.TicketCode == "" ||
		payload.RecommendedSurface != "windows" ||
		!payload.AutoActivate ||
		payload.ManifestRootPublicKey == "" {
		t.Fatalf("unexpected support-session create payload: %#v", payload)
	}
	if len(payload.GatewayURLCandidates) == 0 || !payload.GatewayURLCandidates[0].Recommended {
		t.Fatalf("expected created payload to carry gateway candidates, got %#v", payload.GatewayURLCandidates)
	}
	if len(payload.GatewayURLCandidates) < 2 ||
		payload.GatewayURLCandidates[1].URL != "https://relay.example.test/rdev" ||
		payload.GatewayURLCandidates[1].Kind != "relay" ||
		payload.GatewayURLCandidates[1].Source != "env:RDEV_RELAY_GATEWAY_URL" {
		t.Fatalf("expected configured relay fallback candidate, got %#v", payload.GatewayURLCandidates)
	}
	if !strings.Contains(payload.TargetCommand, "powershell -NoProfile -Command") ||
		!strings.Contains(payload.TargetCommand, "bootstrap.ps1") ||
		!strings.Contains(payload.TargetCommand, "-UseBasicParsing") ||
		strings.Contains(payload.TargetCommand, "gateway_url_candidates=") ||
		strings.Contains(payload.TargetCommand, "-EncodedCommand") ||
		strings.Contains(payload.TargetCommand, "foreach ($u in $urls)") ||
		strings.Contains(payload.TargetCommand, "$urls has been generated by rdev") ||
		strings.Contains(payload.TargetCommand, "ProgressPrference") ||
		strings.Contains(payload.TargetCommand, "<ticket-code>") ||
		strings.Contains(payload.TargetCommand, "ExecutionPolicy Bypass") {
		t.Fatalf("target command should be ready and safe: %s", payload.TargetCommand)
	}
	if !strings.Contains(payload.TargetCommands["macos_linux"], payload.TicketCode) ||
		!strings.Contains(payload.TargetCommands["macos_linux"], "for u in") ||
		!strings.Contains(payload.TargetCommands["macos_linux"], "--connect-timeout 2") ||
		!strings.Contains(payload.TargetCommands["macos_linux"], "--max-time 10") {
		t.Fatalf("expected cross-platform command candidates with real ticket: %#v", payload.TargetCommands)
	}
	if payload.ConnectionAttemptPolicy.SchemaVersion != "rdev.connection-attempt-policy.v1" ||
		payload.ConnectionAttemptPolicy.WindowsDownloadTimeoutSec != 10 ||
		payload.ConnectionAttemptPolicy.CurlConnectTimeoutSec != 2 ||
		payload.ConnectionAttemptPolicy.CurlMaxTimeSec != 10 ||
		payload.ConnectionAttemptPolicy.RetriesPerCandidate != 1 {
		t.Fatalf("expected bounded connection attempt policy, got %#v", payload.ConnectionAttemptPolicy)
	}
	if payload.ConnectionContinuityPolicy.SchemaVersion != "rdev.support-session-continuity-policy.v1" ||
		!payload.ConnectionContinuityPolicy.StableAfterLANChange ||
		!payload.ConnectionContinuityPolicy.HasStableConfiguredFallback ||
		!slices.Contains(payload.ConnectionContinuityPolicy.StableFallbackKinds, "relay") ||
		payload.ConnectionContinuityPolicy.Assessment != "stable-fallback-configured" ||
		!strings.Contains(payload.ConnectionContinuityPolicy.AgentRule, "opportunistic first path") {
		t.Fatalf("expected configured relay continuity policy, got %#v", payload.ConnectionContinuityPolicy)
	}
	if payload.TargetBootstrapRequirements.SchemaVersion != "rdev.support-session-target-bootstrap-requirements.v1" ||
		!slices.Contains(payload.TargetBootstrapRequirements.RequiredAssets, "rdev-bootstrap-windows-amd64.exe") ||
		!slices.Contains(payload.TargetBootstrapRequirements.StandardFix, "rdev support-session connect --start") ||
		!strings.Contains(strings.Join(payload.TargetBootstrapRequirements.Forbidden, "\n"), "signed generated Windows broker") {
		t.Fatalf("expected Windows bootstrap requirements and standard recovery, got %#v", payload.TargetBootstrapRequirements)
	}
	if payload.TargetBootstrapReadiness.SchemaVersion != "rdev.support-session-target-bootstrap-readiness.v1" ||
		payload.TargetBootstrapReadiness.AllReady ||
		!strings.Contains(payload.TargetBootstrapReadiness.AgentRule, "support-session connect --start") {
		t.Fatalf("expected create to report missing gateway bootstrap assets, got %#v", payload.TargetBootstrapReadiness)
	}
	if payload.UserHandoff.SchemaVersion != "rdev.support-session-user-handoff.v1" ||
		payload.UserHandoff.CopyPasteKind != "windows" ||
		payload.UserHandoff.CopyPaste != payload.TargetCommand ||
		!strings.Contains(payload.UserHandoff.Message, "目标电脑") ||
		!strings.Contains(strings.ToLower(payload.UserHandoff.AgentNextStep), "do not send") {
		t.Fatalf("expected ready user handoff, got %#v", payload.UserHandoff)
	}
	watch := strings.Join(payload.WatchConnectionStatus, "\x00")
	if !strings.Contains(watch, payload.TicketCode) ||
		strings.Contains(watch, "<ticket-code>") ||
		!strings.Contains(watch, "--wait") {
		t.Fatalf("watch command should be ready: %#v", payload.WatchConnectionStatus)
	}
	configuredWatch := strings.Join(payload.WatchConnectionStatusConfiguredGateway.Command, "\x00")
	if !strings.Contains(configuredWatch, payload.TicketCode) ||
		!strings.Contains(configuredWatch, "--wait") ||
		strings.Contains(configuredWatch, "--gateway-url") ||
		!strings.Contains(payload.WatchConnectionStatusConfiguredGateway.AgentRule, "RDEV_*_GATEWAY_URL") {
		t.Fatalf("configured gateway watcher should be ready and omit gateway URL: %#v", payload.WatchConnectionStatusConfiguredGateway)
	}
}

func TestSupportSessionCreateUsesConfiguredGatewayURL(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()
	backendURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	const publicGatewayURL = "https://gateway.example.test"
	oldDefaultClient := http.DefaultClient
	oldDefaultTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultClient = oldDefaultClient
		http.DefaultTransport = oldDefaultTransport
	})
	http.DefaultClient = &http.Client{}
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		forwarded := req.Clone(req.Context())
		forwardedURL := *req.URL
		forwardedURL.Scheme = backendURL.Scheme
		forwardedURL.Host = backendURL.Host
		forwarded.URL = &forwardedURL
		forwarded.Host = "gateway.example.test"
		return oldDefaultTransport.RoundTrip(forwarded)
	})
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", publicGatewayURL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{
		"support-session", "create",
		"--target", "auto",
		"--reason", "company computer support",
		"--locale", "en",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion        string `json:"schema_version"`
		GatewayURL           string `json:"gateway_url"`
		TicketCode           string `json:"ticket_code"`
		GatewayURLCandidates []struct {
			URL         string `json:"url"`
			Recommended bool   `json:"recommended"`
		} `json:"gateway_url_candidates"`
		TargetCommand string `json:"target_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid create JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-created.v1" ||
		payload.GatewayURL != publicGatewayURL ||
		payload.TicketCode == "" ||
		!strings.Contains(payload.TargetCommand, "Windows PowerShell") ||
		!strings.Contains(payload.TargetCommand, "macOS/Linux terminal") ||
		!strings.Contains(payload.TargetCommand, publicGatewayURL+"/join/"+payload.TicketCode) {
		t.Fatalf("expected configured gateway create payload, got %#v", payload)
	}
	if len(payload.GatewayURLCandidates) == 0 ||
		payload.GatewayURLCandidates[0].URL != publicGatewayURL ||
		!payload.GatewayURLCandidates[0].Recommended {
		t.Fatalf("expected configured gateway candidate, got %#v", payload.GatewayURLCandidates)
	}
}

func TestSupportSessionStartLANFailsImmediatelyWithoutTicketAndCleansServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	registry, err := tunnel.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	app.supportSessionStartDeps = &supportSessionStartDeps{Registry: registry}
	workDir := filepath.Join(t.TempDir(), "support")
	readyFile := filepath.Join(workDir, "ready", "support-session-ready.json")
	statusFile := filepath.Join(workDir, "status", "support-session-status.json")
	handoffTextFile := filepath.Join(workDir, "handoff", "target-handoff.txt")

	started := time.Now()
	err = app.Run(ctx, []string{
		"support-session", "start",
		"--addr", addr,
		"--gateway-url", "http://" + addr,
		"--work-dir", workDir,
		"--ready-file", readyFile,
		"--status-file", statusFile,
		"--handoff-text-file", handoffTextFile,
		"--target", "windows",
		"--locale", "zh-CN",
	})
	if err == nil || !strings.Contains(err.Error(), "no public gateway candidate") {
		t.Fatalf("expected immediate LAN fail-closed error, got %v", err)
	}
	if time.Since(started) > 25*time.Second {
		t.Fatalf("LAN fail-closed path returned too slowly: %v", time.Since(started))
	}

	var diagnostic struct {
		SchemaVersion string `json:"schema_version"`
		ReadyToSend   bool   `json:"ready_to_send"`
	}
	diagnosticBytes, err := os.ReadFile(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(diagnosticBytes, &diagnostic); err != nil {
		t.Fatalf("invalid diagnostic JSON: %v", err)
	}
	if diagnostic.SchemaVersion != "rdev.support-session-start-diagnostic.v2" || diagnostic.ReadyToSend {
		t.Fatalf("LAN-only foreground start must fail closed, got %#v", diagnostic)
	}
	for _, blockedPath := range []string{readyFile, handoffTextFile} {
		if _, err := os.Stat(blockedPath); !os.IsNotExist(err) {
			t.Fatalf("LAN-only foreground start must not write %s", blockedPath)
		}
	}
	stateBytes, err := os.ReadFile(filepath.Join(workDir, ".rdev", "gateway", "state.json"))
	if err == nil {
		var snapshot gateway.Snapshot
		if err := json.Unmarshal(stateBytes, &snapshot); err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Tickets) != 0 {
			t.Fatalf("LAN fail-closed path created tickets: %#v", snapshot.Tickets)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	listener, err = net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("LAN fail-closed path did not clean server: %v", err)
	}
	_ = listener.Close()
}
func TestSupportSessionStartFakePublicLifecycleOrdersGatewayBeforePublicTunnels(t *testing.T) {
	for _, name := range []string{
		"RDEV_HOSTED_GATEWAY_URL",
		"RDEV_CLOUDFLARED_GATEWAY_URL",
		"RDEV_RELAY_GATEWAY_URL",
		"RDEV_MESH_GATEWAY_URL",
		"RDEV_VPN_GATEWAY_URL",
		"RDEV_SSH_GATEWAY_URL",
	} {
		t.Setenv(name, "")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	events := make([]string, 0, 7)
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		if event == "handoff_written" {
			cancel()
		}
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	registry, err := tunnel.NewRegistry(supportSessionTestTunnelProvider{})
	if err != nil {
		t.Fatal(err)
	}
	app.supportSessionStartDeps = &supportSessionStartDeps{
		RecordEvent: record,
		Registry:    registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { return nil },
		FinalProbe: func(context.Context, tunnel.Candidate, string, string) error {
			return nil
		},
	}
	workDir := filepath.Join(t.TempDir(), "support")
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot:                   ".",
		Addr:                       addr,
		WorkDir:                    workDir,
		Target:                     "windows",
		Reason:                     "test ordered foreground startup",
		TTLSeconds:                 60,
		AutoActivate:               true,
		Locale:                     "en",
		RdevCommand:                filepath.Join(t.TempDir(), "bin", "rdev"),
		AllowDegradedDirectHandoff: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled foreground server, got %v", err)
	}

	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()
	want := []string{
		"local_gateway_started",
		"local_health_passed",
		"providers_started",
		"provider_health_passed",
		"ticket_created",
		"bootstrap_probe_passed",
		"handoff_written",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("support-session startup events = %v, want %v", got, want)
	}
}

func TestBootstrapProbeAvailabilityDoesNotMutateInput(t *testing.T) {
	original := tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates:    []tunnel.Candidate{{ProviderID: "failed", URL: "https://failed.example.test"}},
		Attempts: []tunnel.Attempt{{
			ProviderID: "failed",
			Status:     tunnel.AttemptHealthy,
			Probe:      tunnel.ProbeEvidence{HealthOK: true, InstanceMarker: "instance"},
		}},
	}
	filtered := bootstrapProbeAvailability(context.Background(), original, "instance", func(context.Context, tunnel.Candidate, string) error {
		return errors.New("rejected")
	})
	if len(filtered.Candidates) != 0 || filtered.Attempts[0].Status != tunnel.AttemptDegraded {
		t.Fatalf("unexpected filtered set: %#v", filtered)
	}
	if len(original.Candidates) != 1 || original.Attempts[0].Status != tunnel.AttemptHealthy || original.Attempts[0].ErrorClass != "" || !original.Attempts[0].Probe.HealthOK {
		t.Fatalf("bootstrap filtering mutated input: %#v", original)
	}
}

func TestSupportSessionStartMainlandFailureCleanup(t *testing.T) {
	var starts atomic.Int32
	var staticProbes atomic.Int32
	registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{
		id: "mainland-unverified", url: "https://mainland-unverified.example.test",
		start: func() error { starts.Add(1); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := supportSessionTestAddr(t)
	workDir := filepath.Join(t.TempDir(), "support")
	statusFile := filepath.Join(workDir, "status.json")
	readyFile := filepath.Join(workDir, "ready.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry, Manager: tunnel.Manager{},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { staticProbes.Add(1); return nil },
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: addr, WorkDir: workDir, StatusFile: statusFile, ReadyFile: readyFile,
		Target: "windows", Reason: "mainland evidence test", TTLSeconds: 60, Locale: "en", Region: string(tunnel.RegionCNMainland),
	})
	if err == nil || err.Error() != "no public gateway provider is eligible for the selected region" {
		t.Fatalf("expected provider-selection failure, got %v", err)
	}
	if starts.Load() != 0 {
		t.Fatalf("unverified mainland provider started %d times", starts.Load())
	}
	if staticProbes.Load() != 0 {
		t.Fatalf("empty selection ran %d static bootstrap probes", staticProbes.Load())
	}
	if _, err := os.Stat(readyFile); !os.IsNotExist(err) {
		t.Fatalf("mainland failure must not write ready file")
	}
	statusBytes, readErr := os.ReadFile(statusFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var diagnostic supportSessionStartDiagnostic
	if err := json.Unmarshal(statusBytes, &diagnostic); err != nil {
		t.Fatalf("decode provider-selection diagnostic: %v; payload=%s", err, statusBytes)
	}
	if diagnostic.Phase != "provider-selection" || diagnostic.Reason != "no_public_gateway_provider_eligible" ||
		diagnostic.NextActionClass != "review-provider-eligibility" || len(diagnostic.Attempts) != 1 ||
		diagnostic.Attempts[0].ProviderID != "mainland-unverified" || diagnostic.Attempts[0].Status != string(tunnel.AttemptSkipped) ||
		diagnostic.Attempts[0].ErrorClass != "regional-evidence-missing" {
		t.Fatalf("unexpected provider-selection diagnostic: %#v", diagnostic)
	}
	for surface, content := range map[string][]byte{"status": statusBytes, "stdout": stdout.Bytes()} {
		if bytes.Contains(content, []byte("static-bootstrap-probe")) {
			t.Fatalf("%s misreported an unexecuted static probe: %s", surface, content)
		}
	}
	listener, listenErr := net.Listen("tcp", addr)
	if listenErr != nil {
		t.Fatalf("provider-selection failure did not clean local gateway listener: %v", listenErr)
	}
	_ = listener.Close()
}

func TestSupportSessionExplicitPinProviderPreflightSkipsUnsafeConfiguration(t *testing.T) {
	for _, test := range []struct {
		name       string
		knownHosts string
		wantReason string
	}{
		{name: "missing", wantReason: "ssh-pin-missing"},
		{name: "invalid", knownHosts: "wrong.example ssh-ed25519 dGVzdA==\n", wantReason: "ssh-pin-invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("RDEV_SSH_KNOWN_HOSTS_FILE", "")
			var starts atomic.Int32
			metadata := tunnel.ProviderMetadata{
				ID: tunnel.ProviderPinggy, DisplayName: "Pinggy", Protocols: []string{"https", "ssh"}, Anonymous: true,
				Executable: "ssh", DocumentationURL: "https://pinggy.io/docs/", DefaultAutomatic: false,
				AutomaticPriority: 40, RequiresSSHPin: true,
			}
			registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{
				id: tunnel.ProviderPinggy, url: "https://unsafe-pin.example.test", metadata: &metadata,
				start: func() error { starts.Add(1); return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			policyPath := filepath.Join(root, "providers.json")
			policyBody := `{"allowed_provider_ids":["pinggy"]}`
			if test.knownHosts != "" {
				knownHostsPath := filepath.Join(root, "known_hosts")
				if err := os.WriteFile(knownHostsPath, []byte(test.knownHosts), 0o600); err != nil {
					t.Fatal(err)
				}
				policyBody = fmt.Sprintf(`{"allowed_provider_ids":["pinggy"],"ssh_known_hosts_paths":{"pinggy":%q}}`, knownHostsPath)
			}
			if err := os.WriteFile(policyPath, []byte(policyBody), 0o600); err != nil {
				t.Fatal(err)
			}
			workDir := filepath.Join(root, "support")
			statusFile := filepath.Join(workDir, "status.json")
			var stdout bytes.Buffer
			app := NewApp(&stdout, io.Discard)
			app.supportSessionStartDeps = &supportSessionStartDeps{Registry: registry}
			err = app.supportSessionStart(context.Background(), supportSessionStartOptions{
				RepoRoot: ".", Addr: supportSessionTestAddr(t), WorkDir: workDir, StatusFile: statusFile,
				Target: "windows", Reason: "pin preflight", TTLSeconds: 60, Locale: "en", ProviderPolicyPath: policyPath,
			})
			if err == nil || err.Error() != "no public gateway provider is eligible for the selected region" {
				t.Fatalf("expected provider-selection failure, got %v", err)
			}
			if starts.Load() != 0 {
				t.Fatalf("unsafe pin provider started %d times", starts.Load())
			}
			var diagnostic supportSessionStartDiagnostic
			content, readErr := os.ReadFile(statusFile)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err := json.Unmarshal(content, &diagnostic); err != nil {
				t.Fatal(err)
			}
			if len(diagnostic.Attempts) != 1 || diagnostic.Attempts[0].ProviderID != tunnel.ProviderPinggy ||
				diagnostic.Attempts[0].Status != string(tunnel.AttemptSkipped) || diagnostic.Attempts[0].ErrorClass != test.wantReason {
				t.Fatalf("unexpected pin preflight diagnostic: %#v", diagnostic)
			}
			if test.knownHosts != "" && bytes.Contains(stdout.Bytes(), []byte(test.knownHosts)) || bytes.Contains(content, []byte(root)) {
				t.Fatalf("pin preflight diagnostic leaked protected material: stdout=%s status=%s", stdout.Bytes(), content)
			}
		})
	}
}

func TestSupportSessionStartFinalBootstrapFailureCleanup(t *testing.T) {
	var stops atomic.Int32
	registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{
		id: "bootstrap-failure", url: "https://bootstrap-failure.example.test", stop: func() { stops.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := supportSessionTestAddr(t)
	workDir := filepath.Join(t.TempDir(), "support")
	statusFile := filepath.Join(workDir, "status.json")
	readyFile := filepath.Join(workDir, "ready.json")
	handoffFile := filepath.Join(workDir, "handoff.txt")
	var stdout bytes.Buffer
	app := NewApp(&stdout, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { return nil },
		FinalProbe:     func(context.Context, tunnel.Candidate, string, string) error { return errors.New("bootstrap failed") },
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: addr, WorkDir: workDir, StatusFile: statusFile, ReadyFile: readyFile,
		HandoffTextFile: handoffFile, Target: "windows", Reason: "final bootstrap failure", TTLSeconds: 60, Locale: "en",
		AllowDegradedDirectHandoff: true,
	})
	if err == nil || !strings.Contains(err.Error(), "final ticket bootstrap") {
		t.Fatalf("expected final-ticket bootstrap failure, got %v", err)
	}
	if stops.Load() != 1 {
		t.Fatalf("provider handle stopped %d times, want 1", stops.Load())
	}
	for _, path := range []string{readyFile, handoffFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("final bootstrap failure must not write %s", path)
		}
	}
}

func TestSupportSessionStartFinalTicketMetadataExcludesFailedCandidates(t *testing.T) {
	registry, err := tunnel.NewRegistry(
		supportSessionFuncTunnelProvider{id: "a-failed", url: "https://a-failed.example.test"},
		supportSessionFuncTunnelProvider{id: "b-healthy", url: "https://b-healthy.example.test"},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	app := NewApp(&stdout, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe: func(_ context.Context, candidate tunnel.Candidate, _ string) error {
			if candidate.ProviderID == "a-failed" {
				return errors.New("candidate failed static bootstrap probe")
			}
			return nil
		},
		FinalProbe: func(context.Context, tunnel.Candidate, string, string) error { return nil },
		RecordEvent: func(event string) {
			if event == "handoff_written" {
				cancel()
			}
		},
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: supportSessionTestAddr(t), WorkDir: filepath.Join(t.TempDir(), "support"),
		Target: "windows", Reason: "final ticket metadata", TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled foreground server, got %v", err)
	}
	var payload struct {
		Session struct {
			Ticket struct {
				Code     string            `json:"code"`
				Metadata map[string]string `json:"metadata"`
			} `json:"ticket"`
		} `json:"session"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	metadata := payload.Session.Ticket.Metadata[gateway.TicketMetadataGatewayCandidates]
	if strings.Contains(metadata, "a-failed") || !strings.Contains(metadata, "b-healthy") {
		t.Fatalf("final ticket must contain only statically probed candidates: code=%s metadata=%s", payload.Session.Ticket.Code, metadata)
	}
}

func TestSupportSessionStaticProbeDoesNotCreateProvisionalTicket(t *testing.T) {
	registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{id: "static-probe", url: "https://static-probe.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var probeTicketCount int
	app := NewApp(io.Discard, io.Discard)
	workDir := filepath.Join(t.TempDir(), "support")
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error {
			probeTicketCount++
			return nil
		},
		FinalProbe: func(context.Context, tunnel.Candidate, string, string) error { return nil },
		RecordEvent: func(event string) {
			if event == "handoff_written" {
				cancel()
			}
		},
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: supportSessionTestAddr(t), WorkDir: workDir,
		Target: "windows", Reason: "static probe", TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled foreground server, got %v", err)
	}
	if probeTicketCount != 1 {
		t.Fatalf("static probe calls = %d, want 1", probeTicketCount)
	}
	content, err := os.ReadFile(filepath.Join(workDir, ".rdev", "gateway", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot gateway.Snapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tickets) != 1 {
		t.Fatalf("static filtering must create exactly one final ticket, got %d", len(snapshot.Tickets))
	}
}

func TestSupportSessionStaticProbeRejectsAllWithoutCreatingTicket(t *testing.T) {
	registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{id: "rejected", url: "https://rejected.example.test/?token=query-secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var events []string
	var stdout bytes.Buffer
	workDir := filepath.Join(t.TempDir(), "support")
	app := NewApp(&stdout, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true, InstanceMarker: "instance-marker-secret"}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { return errors.New("template mismatch") },
		RecordEvent:    func(event string) { events = append(events, event) },
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: supportSessionTestAddr(t), WorkDir: workDir,
		Target: "windows", Reason: "reject all", TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
	})
	if err == nil || !strings.Contains(err.Error(), "no public gateway candidate") {
		t.Fatalf("expected fail-closed no-candidate error, got %v", err)
	}
	if slices.Contains(events, "ticket_created") {
		t.Fatalf("zero-candidate path created a ticket: events=%v", events)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"ready_to_send": false`)) {
		t.Fatalf("expected fail-closed diagnostic, got %s", stdout.String())
	}
	for _, forbidden := range []string{"query-secret", "rejected.example.test", "instance-marker-secret", `"probe"`, `"candidates"`} {
		if bytes.Contains(stdout.Bytes(), []byte(forbidden)) {
			t.Fatalf("static-probe diagnostic leaked %q: %s", forbidden, stdout.String())
		}
	}
	statePath := filepath.Join(workDir, ".rdev", "gateway", "state.json")
	if content, readErr := os.ReadFile(statePath); readErr == nil && bytes.Contains(content, []byte(`"tickets"`)) {
		var snapshot gateway.Snapshot
		if err := json.Unmarshal(content, &snapshot); err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Tickets) != 0 {
			t.Fatalf("zero-candidate state persisted tickets: %#v", snapshot.Tickets)
		}
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
}

func TestSupportSessionStartServerFailureCleansWatcherAndHandle(t *testing.T) {
	var stops atomic.Int32
	registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{id: "server-error", url: "https://server-error.example.test", stop: func() { stops.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("simulated gateway server failure")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app := NewApp(io.Discard, io.Discard)
	addr := supportSessionTestAddr(t)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe:       func(context.Context, tunnel.Candidate, string) error { return nil },
		FinalProbe:           func(context.Context, tunnel.Candidate, string, string) error { return nil },
		WaitForGatewayServer: func(context.Context, gatewayServerHandle) error { return sentinel },
	}
	started := time.Now()
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: addr, WorkDir: filepath.Join(t.TempDir(), "support"),
		Target: "windows", Reason: "server failure cleanup", TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
	})
	if !errors.Is(err, sentinel) || time.Since(started) > 25*time.Second {
		t.Fatalf("server failure should return promptly, got %v after %v", err, time.Since(started))
	}
	if stops.Load() != 1 {
		t.Fatalf("provider handle stopped %d times, want 1", stops.Load())
	}
	listener, listenErr := net.Listen("tcp", addr)
	if listenErr != nil {
		t.Fatalf("gateway server was not cleaned up: %v", listenErr)
	}
	_ = listener.Close()
}

func TestSupportSessionStartLocalHealthFailureCleanup(t *testing.T) {
	sentinel := errors.New("simulated local health failure")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := supportSessionTestAddr(t)
	app := NewApp(io.Discard, io.Discard)
	registry, err := tunnel.NewRegistry(supportSessionTestTunnelProvider{})
	if err != nil {
		t.Fatal(err)
	}
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry:           registry,
		WaitForLocalHealth: func(context.Context, gatewayServerHandle, string, time.Duration) error { return sentinel },
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: addr, GatewayURL: "http://" + addr, WorkDir: filepath.Join(t.TempDir(), "support"),
		Target: "windows", Reason: "local health failure", TTLSeconds: 60, Locale: "en",
	})
	if err == nil || err.Error() != "local gateway health check failed" || strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("expected local health failure, got %v", err)
	}
}

func TestSupportSessionStartNXDOMAINFallbackCleanup(t *testing.T) {
	var starts atomic.Int32
	var stops atomic.Int32
	var handoffWritten atomic.Bool
	cloudflareMetadata := tunnel.ProviderMetadata{
		ID: tunnel.ProviderCloudflareQuick, DisplayName: "Cloudflare Quick Tunnel", Protocols: []string{"https"},
		Anonymous: true, Executable: "cloudflared", DocumentationURL: "https://example.test/cloudflare",
		DefaultAutomatic: true, AutomaticPriority: 10,
	}
	tunn3lMetadata := tunnel.ProviderMetadata{
		ID: tunnel.ProviderTunn3l, DisplayName: "tunn3l.sh", Protocols: []string{"https", "wss"},
		Anonymous: true, Executable: "rdev-managed:tunn3l-v0.5.1", DocumentationURL: "https://example.test/tunn3l",
		DefaultAutomatic: true, AutomaticPriority: 20,
	}
	registry, err := tunnel.NewRegistry(
		supportSessionFuncTunnelProvider{id: tunnel.ProviderCloudflareQuick, url: "https://a-nxdomain.example.test", metadata: &cloudflareMetadata, start: func() error { starts.Add(1); return nil }, stop: func() { stops.Add(1) }},
		supportSessionFuncTunnelProvider{id: tunnel.ProviderTunn3l, url: "https://b-healthy.example.test", metadata: &tunn3lMetadata, start: func() error { starts.Add(1); return nil }, stop: func() { stops.Add(1) }},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	app := NewApp(&stdout, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(_ context.Context, candidate tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			if candidate.ProviderID == tunnel.ProviderCloudflareQuick {
				return tunnel.ProbeEvidence{}, &net.DNSError{Err: "no such host", Name: "a-nxdomain.example.test", IsNotFound: true}
			}
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { return nil },
		FinalProbe:     func(context.Context, tunnel.Candidate, string, string) error { return nil },
		RecordEvent: func(event string) {
			if event == "handoff_written" {
				handoffWritten.Store(true)
				cancel()
			}
		},
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: supportSessionTestAddr(t), WorkDir: filepath.Join(t.TempDir(), "support"),
		Target: "windows", Reason: "nxdomain fallback", TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
	})
	if !errors.Is(err, context.Canceled) || starts.Load() != 2 || stops.Load() != 2 || !handoffWritten.Load() {
		t.Fatalf("expected tunn3l handoff after Cloudflare NXDOMAIN, err=%v starts=%d stops=%d handoff=%v", err, starts.Load(), stops.Load(), handoffWritten.Load())
	}
	var payload struct {
		Session struct {
			TargetCommand         string `json:"target_command"`
			TargetHandoffEnvelope struct {
				FullText string `json:"full_text"`
			} `json:"target_handoff_envelope"`
		} `json:"session"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode tunn3l handoff payload: %v; stdout=%s", err, stdout.String())
	}
	command := payload.Session.TargetCommand
	if command == "" || len(command) > 512 || strings.ContainsAny(command, "\r\n") ||
		!strings.Contains(command, "powershell -NoProfile -Command") || !strings.Contains(command, "bootstrap.ps1") ||
		strings.Contains(command, "-EncodedCommand") || strings.Contains(command, "ExecutionPolicy") ||
		strings.Contains(command, "foreach") || strings.Contains(command, "while (") {
		t.Fatalf("Windows handoff command was not short and readable: %q", command)
	}
	if !strings.Contains(payload.Session.TargetHandoffEnvelope.FullText, command) {
		t.Fatalf("handoff full_text did not preserve the exact Windows command: %#v", payload.Session.TargetHandoffEnvelope)
	}
}

func TestSupportSessionTunnelStartReceivesProviderRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workDir := filepath.Join(t.TempDir(), "support")
	requestCh := make(chan tunnel.StartRequest, 1)
	registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{
		id: "provider-root", url: "https://provider-root.example.test",
		startRequest: func(request tunnel.StartRequest) error {
			requestCh <- request
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(io.Discard, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { return nil },
		FinalProbe:     func(context.Context, tunnel.Candidate, string, string) error { return nil },
		RecordEvent: func(event string) {
			if event == "handoff_written" {
				cancel()
			}
		},
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: supportSessionTestAddr(t), WorkDir: workDir,
		Target: "windows", Reason: "provider root", TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled foreground server, got %v", err)
	}
	select {
	case request := <-requestCh:
		canonicalWorkDir, canonicalErr := canonicalPathThroughExistingAncestor(workDir)
		if canonicalErr != nil {
			t.Fatal(canonicalErr)
		}
		want := filepath.Join(canonicalWorkDir, ".rdev")
		if request.ProviderRoot != want {
			t.Fatalf("ProviderRoot = %q, want %q", request.ProviderRoot, want)
		}
	default:
		t.Fatal("provider start request was not captured")
	}
}

func TestSupportSessionStartMarkerMismatchCleanup(t *testing.T) {
	registry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{id: "marker-mismatch", url: "https://marker-mismatch.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := supportSessionTestAddr(t)
	statusDir := t.TempDir()
	if err := os.Chmod(statusDir, 0o700); err != nil {
		t.Fatal(err)
	}
	statusFile := filepath.Join(statusDir, "status.json")
	workDir := filepath.Join(t.TempDir(), "support")
	app := NewApp(io.Discard, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true}, errors.New("gateway instance marker mismatch")
		}},
	}
	err = app.supportSessionStart(ctx, supportSessionStartOptions{
		RepoRoot: ".", Addr: addr, WorkDir: workDir, StatusFile: statusFile,
		Target: "windows", Reason: "marker mismatch", TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
	})
	if err == nil || !strings.Contains(err.Error(), "no public gateway candidate") {
		t.Fatalf("expected fail-closed marker mismatch error, got %v", err)
	}
	data, readErr := os.ReadFile(statusFile)
	if readErr != nil || !bytes.Contains(data, []byte("rdev.support-session-start-diagnostic.v2")) {
		t.Fatalf("expected marker mismatch diagnostic: err=%v data=%s", readErr, data)
	}
}

func waitForSupportSessionDiagnostic(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var payload struct {
				SchemaVersion string `json:"schema_version"`
			}
			if json.Unmarshal(data, &payload) == nil && payload.SchemaVersion == "rdev.support-session-start-diagnostic.v2" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for support-session diagnostic %s", path)
}

func supportSessionTestAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestTunnelRuntimePolicyValidation(t *testing.T) {
	registry, err := tunnel.NewRegistry(supportSessionTestTunnelProvider{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadTunnelRuntimeConfig("unknown", "", registry); err == nil {
		t.Fatal("unknown region should fail")
	}
	if _, err := loadTunnelRuntimeConfig("global", filepath.Join(t.TempDir(), "missing.json"), registry); err == nil {
		t.Fatal("unreadable policy should fail")
	}

	tests := []struct {
		name string
		body string
	}{
		{name: "unknown provider", body: `{"allowed_provider_ids":["unknown"]}`},
		{name: "inline credential", body: `{"allowed_provider_ids":["ordered-test"],"token":"secret"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadTunnelRuntimeConfig("global", path, registry); err == nil {
				t.Fatal("invalid provider policy should fail")
			}
		})
	}
	if runtime.GOOS != "windows" {
		path := filepath.Join(t.TempDir(), "policy.json")
		if err := os.WriteFile(path, []byte(`{"allowed_provider_ids":["ordered-test"]}`), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := loadTunnelRuntimeConfig("global", path, registry); err == nil {
			t.Fatal("execute permission on provider policy should fail")
		}
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte("{\"allowed_provider_ids\":[\"ordered-test\"]}\n{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTunnelRuntimeConfig("global", path, registry); err == nil {
		t.Fatal("trailing JSON data should fail")
	}

	overlapPath := filepath.Join(t.TempDir(), "overlap.json")
	if err := os.WriteFile(overlapPath, []byte(`{"allowed_provider_ids":["ordered-test"],"disabled_provider_ids":["ordered-test"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTunnelRuntimeConfig("global", overlapPath, registry); err == nil {
		t.Fatal("allowed/disabled provider overlap must fail closed")
	}

	allDisabledPath := filepath.Join(t.TempDir(), "all-disabled.json")
	if err := os.WriteFile(allDisabledPath, []byte(`{"disabled_provider_ids":["ordered-test"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadTunnelRuntimeConfig("global", allDisabledPath, registry)
	if err != nil {
		t.Fatal(err)
	}
	selected := registry.Select(tunnel.Policy{
		Region: config.Region, AllowedProviderIDs: config.AllowedProviderIDs,
		RestrictProviders: config.RestrictProviders, AllowNonDefault: true,
	}, config.Evidence)
	if len(selected) != 0 {
		t.Fatalf("all-disabled policy failed open: %#v", selected)
	}
	if config.ExplicitAllowlist || tunnelRuntimePolicy(config, time.Now()).AllowNonDefault {
		t.Fatalf("disabled-only policy must not enable operator-pin-only providers: %#v", config)
	}

	emptyAllowedPath := filepath.Join(t.TempDir(), "empty-allowed.json")
	if err := os.WriteFile(emptyAllowedPath, []byte(`{"allowed_provider_ids":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err = loadTunnelRuntimeConfig("global", emptyAllowedPath, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !config.RestrictProviders || !config.ExplicitAllowlist || len(config.AllowedProviderIDs) != 0 || !tunnelRuntimePolicy(config, time.Now()).AllowNonDefault {
		t.Fatalf("explicit empty allowlist must mean allow none: %#v", config)
	}

	for name, body := range map[string]string{
		"duplicate allowed":     `{"allowed_provider_ids":["ordered-test","ordered-test"]}`,
		"duplicate disabled":    `{"disabled_provider_ids":["ordered-test","ordered-test"]}`,
		"noncanonical allowed":  `{"allowed_provider_ids":[" ordered-test"]}`,
		"noncanonical disabled": `{"disabled_provider_ids":["ordered-test "]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "duplicates.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadTunnelRuntimeConfig("global", path, registry); err == nil {
				t.Fatal("duplicate provider ID must fail closed")
			}
		})
	}
}

func TestTunnelRuntimePolicyProtectsRegionalEvidenceFiles(t *testing.T) {
	registry, err := tunnel.NewRegistry(supportSessionTestTunnelProvider{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	valid := fmt.Sprintf(`{"provider_id":"ordered-test","region":"global","status":"verified","issuer":"reviewed-probe","observed_at":%q,"expires_at":%q,"samples":[{"carrier":"probe-network","region":"global","success":true}]}`,
		now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano))
	tests := []struct {
		name string
		body string
		mode os.FileMode
	}{
		{name: "trailing JSON", body: valid + `\n{}`, mode: 0o600},
		{name: "unknown field", body: strings.TrimSuffix(valid, "}") + `,"unexpected":"secret"}`, mode: 0o600},
		{name: "oversize", body: valid + strings.Repeat(" ", (1<<20)+1), mode: 0o600},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name string
			body string
			mode os.FileMode
		}{name: "writable by group or others", body: valid, mode: 0o666})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			evidencePath := filepath.Join(dir, "evidence.json")
			if err := os.WriteFile(evidencePath, []byte(tt.body), tt.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(evidencePath, tt.mode); err != nil {
				t.Fatal(err)
			}
			policyPath := filepath.Join(dir, "policy.json")
			policyBody := fmt.Sprintf(`{"regional_evidence_paths":[%q]}`, evidencePath)
			if err := os.WriteFile(policyPath, []byte(policyBody), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadTunnelRuntimeConfig("global", policyPath, registry); err == nil {
				t.Fatalf("protected evidence case %q must fail", tt.name)
			}
		})
	}
}

func TestTunnelDefaultRegistrySelectsAutomaticProvidersInPriorityOrder(t *testing.T) {
	deps, err := defaultTunnelRuntimeDeps(io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata := deps.Registry.Providers()
	if len(metadata) != 4 {
		t.Fatalf("default provider metadata count = %d, want 4: %#v", len(metadata), metadata)
	}
	byID := make(map[string]tunnel.ProviderMetadata, len(metadata))
	for _, item := range metadata {
		byID[item.ID] = item
	}
	if _, ok := byID[tunnel.ProviderTunn3l]; !ok {
		t.Fatalf("default registry omitted tunn3l: %#v", metadata)
	}
	if byID[tunnel.ProviderCloudflareQuick].AutomaticPriority != 10 {
		t.Fatalf("Cloudflare automatic priority = %d, want 10", byID[tunnel.ProviderCloudflareQuick].AutomaticPriority)
	}

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	config := tunnelRuntimeConfig{Region: tunnel.RegionGlobal, SSHKnownHostsPaths: map[string]string{}}
	evaluations := deps.Registry.Evaluate(tunnelRuntimePolicy(config, now), nil)
	evaluations, _ = preflightTunnelEvaluations(evaluations, config, runtime.GOOS, runtime.GOARCH)
	selections := eligibleTunnelSelections(evaluations)
	got := make([]string, 0, len(selections))
	for _, selection := range selections {
		got = append(got, selection.Metadata.ID)
	}
	want := []string{tunnel.ProviderCloudflareQuick, tunnel.ProviderTunn3l, tunnel.ProviderLocalhostRun}
	if !slices.Equal(got, want) {
		t.Fatalf("default global selection = %v, want %v", got, want)
	}

	knownHostsRoot, err := canonicalPathThroughExistingAncestor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	knownHostsPath := filepath.Join(knownHostsRoot, "known_hosts")
	if err := os.WriteFile(knownHostsPath, []byte("[free.pinggy.io]:443 ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	explicit := tunnelRuntimeConfig{
		Region: tunnel.RegionGlobal, AllowedProviderIDs: []string{tunnel.ProviderPinggy}, RestrictProviders: true, ExplicitAllowlist: true,
		SSHKnownHostsPaths: map[string]string{tunnel.ProviderPinggy: knownHostsPath},
	}
	evaluations = deps.Registry.Evaluate(tunnelRuntimePolicy(explicit, now), nil)
	evaluations, _ = preflightTunnelEvaluations(evaluations, explicit, runtime.GOOS, runtime.GOARCH)
	selections = eligibleTunnelSelections(evaluations)
	if len(selections) != 1 || selections[0].Metadata.ID != tunnel.ProviderPinggy {
		t.Fatalf("explicit Pinggy selection = %#v, want only Pinggy", selections)
	}
}

func TestTunnelRuntimePolicyOptsInNonDefaultProvidersOnlyWithExplicitAllowlist(t *testing.T) {
	deps, err := defaultTunnelRuntimeDeps(io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := canonicalPathThroughExistingAncestor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	knownHostsPath := filepath.Join(root, "known_hosts")
	if err := os.WriteFile(knownHostsPath, []byte("[free.pinggy.io]:443 ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writePolicy := func(t *testing.T, name, body string) string {
		t.Helper()
		if body == "" {
			return ""
		}
		path := filepath.Join(root, name+".json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tests := []struct {
		name              string
		body              string
		wantExplicit      bool
		wantPinggy        bool
		wantSelectionSize int
	}{
		{name: "no-policy", wantSelectionSize: 3},
		{name: "explicit-pinggy", body: fmt.Sprintf(`{"allowed_provider_ids":["pinggy"],"ssh_known_hosts_paths":{"pinggy":%q}}`, knownHostsPath), wantExplicit: true, wantPinggy: true, wantSelectionSize: 1},
		{name: "empty-allowlist", body: `{"allowed_provider_ids":[]}`, wantExplicit: true, wantSelectionSize: 0},
		{name: "disabled-only", body: fmt.Sprintf(`{"disabled_provider_ids":["localhost-run"],"ssh_known_hosts_paths":{"pinggy":%q}}`, knownHostsPath), wantSelectionSize: 2},
		{name: "ssh-path-only", body: fmt.Sprintf(`{"ssh_known_hosts_paths":{"pinggy":%q}}`, knownHostsPath), wantSelectionSize: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadTunnelRuntimeConfig("global", writePolicy(t, test.name, test.body), deps.Registry)
			if err != nil {
				t.Fatal(err)
			}
			policy := tunnelRuntimePolicy(config, time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC))
			if config.ExplicitAllowlist != test.wantExplicit || policy.AllowNonDefault != test.wantExplicit {
				t.Fatalf("explicit allowlist state = config:%v policy:%v, want %v", config.ExplicitAllowlist, policy.AllowNonDefault, test.wantExplicit)
			}
			evaluations := deps.Registry.Evaluate(policy, nil)
			evaluations, _ = preflightTunnelEvaluations(evaluations, config, runtime.GOOS, runtime.GOARCH)
			selections := eligibleTunnelSelections(evaluations)
			if len(selections) != test.wantSelectionSize {
				t.Fatalf("selection count = %d, want %d: %#v", len(selections), test.wantSelectionSize, selections)
			}
			foundPinggy := false
			for _, selection := range selections {
				foundPinggy = foundPinggy || selection.Metadata.ID == tunnel.ProviderPinggy
			}
			if foundPinggy != test.wantPinggy {
				t.Fatalf("Pinggy selected = %v, want %v: %#v", foundPinggy, test.wantPinggy, selections)
			}
		})
	}

	t.Run("invalid localhost override does not fall back to built-in trust", func(t *testing.T) {
		root, err := canonicalPathThroughExistingAncestor(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		knownHostsPath := filepath.Join(root, "known_hosts")
		if err := os.WriteFile(knownHostsPath, []byte("wrong.example ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		policyPath := filepath.Join(root, "providers.json")
		policyBody := fmt.Sprintf(`{"allowed_provider_ids":["localhost-run"],"ssh_known_hosts_paths":{"localhost-run":%q}}`, knownHostsPath)
		if err := os.WriteFile(policyPath, []byte(policyBody), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout bytes.Buffer
		app := NewApp(&stdout, io.Discard)
		if err := app.Run(context.Background(), []string{"tunnel", "probe", "--region", "global", "--provider-policy", policyPath, "--json"}); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Providers []tunnelProbeInspection `json:"providers"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		for _, provider := range payload.Providers {
			if provider.ID == tunnel.ProviderLocalhostRun &&
				(provider.ConfigurationReady || provider.KnownHostsConfigured || provider.Eligible || provider.EligibilityReason != "ssh-pin-invalid") {
				t.Fatalf("invalid localhost.run override fell back to built-in trust: %#v", provider)
			}
		}
		if strings.Contains(stdout.String(), knownHostsPath) || strings.Contains(stdout.String(), "wrong.example") {
			t.Fatalf("localhost.run override preflight leaked protected material: %s", stdout.String())
		}
	})
}

func TestTunnelPreflightMarksOnlyBuiltInUnsupportedManagedProviderIneligible(t *testing.T) {
	deps, err := defaultTunnelRuntimeDeps(io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	config := tunnelRuntimeConfig{Region: tunnel.RegionGlobal, SSHKnownHostsPaths: map[string]string{}}
	evaluations := deps.Registry.Evaluate(tunnelRuntimePolicy(config, time.Now().UTC()), nil)
	evaluations, configurations := preflightTunnelEvaluations(evaluations, config, "windows", "amd64")
	found := false
	for _, item := range evaluations {
		if item.Metadata.ID != tunnel.ProviderTunn3l {
			continue
		}
		found = true
		if item.Eligibility.Eligible || item.Eligibility.Reason != "tool-unsupported" || configurations[tunnel.ProviderTunn3l].ConfigurationReady {
			t.Fatalf("unsupported built-in tunn3l preflight = %#v, configuration=%#v", item, configurations[tunnel.ProviderTunn3l])
		}
	}
	if !found {
		t.Fatal("default registry omitted tunn3l")
	}

	metadata := tunnel.ProviderMetadata{
		ID: tunnel.ProviderTunn3l, DisplayName: "fake tunn3l", Protocols: []string{"https"}, Anonymous: true,
		Executable: "test", DocumentationURL: "https://example.test", DefaultAutomatic: true, AutomaticPriority: 20,
	}
	fakeRegistry, err := tunnel.NewRegistry(supportSessionFuncTunnelProvider{id: tunnel.ProviderTunn3l, metadata: &metadata})
	if err != nil {
		t.Fatal(err)
	}
	evaluations = fakeRegistry.Evaluate(tunnelRuntimePolicy(config, time.Now().UTC()), nil)
	evaluations, configurations = preflightTunnelEvaluations(evaluations, config, "windows", "amd64")
	if len(evaluations) != 1 || !evaluations[0].Eligibility.Eligible || !configurations[tunnel.ProviderTunn3l].ConfigurationReady {
		t.Fatalf("injected fake provider depended on local managed-tool support: evaluations=%#v configuration=%#v", evaluations, configurations)
	}

	mainland := tunnelRuntimeConfig{Region: tunnel.RegionCNMainland, SSHKnownHostsPaths: map[string]string{}}
	evaluations = deps.Registry.Evaluate(tunnelRuntimePolicy(mainland, time.Now().UTC()), nil)
	evaluations, _ = preflightTunnelEvaluations(evaluations, mainland, "windows", "amd64")
	for _, item := range evaluations {
		if item.Eligibility.Eligible || item.Eligibility.Reason != "regional-evidence-missing" {
			t.Fatalf("configuration preflight overwrote regional eligibility: %#v", item)
		}
	}
}

func TestTunnelProvidersReportsReadOnlyMainlandEligibility(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{
		"tunnel", "providers", "--region", "cn-mainland", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Region        string `json:"region"`
		ReadOnly      bool   `json:"read_only"`
		Providers     []struct {
			ID             string             `json:"id"`
			Executable     string             `json:"executable"`
			Eligibility    tunnel.Eligibility `json:"eligibility"`
			EvidenceStatus string             `json:"evidence_status"`
			FailureDomains map[string]bool    `json:"failure_domains"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode providers output: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if payload.SchemaVersion != "rdev.tunnel-providers.v1" || payload.Region != "cn-mainland" || !payload.ReadOnly {
		t.Fatalf("unexpected providers envelope: %#v", payload)
	}
	if len(payload.Providers) != 4 {
		t.Fatalf("expected all registered providers, got %#v", payload.Providers)
	}
	foundTunn3l := false
	for _, provider := range payload.Providers {
		foundTunn3l = foundTunn3l || provider.ID == tunnel.ProviderTunn3l
		if provider.ID == "" || provider.Executable == "" {
			t.Fatalf("provider metadata is incomplete: %#v", provider)
		}
		if provider.Eligibility.Eligible || provider.Eligibility.Reason != "regional-evidence-missing" || provider.EvidenceStatus != "missing" {
			t.Fatalf("mainland provider without evidence must be ineligible: %#v", provider)
		}
		for domain, configured := range provider.FailureDomains {
			if strings.Contains(domain, "://") || strings.Contains(domain, "192.0.2.") {
				t.Fatalf("failure-domain output leaked a raw endpoint: %#v", provider.FailureDomains)
			}
			_ = configured
		}
	}
	if !foundTunn3l {
		t.Fatalf("provider metadata omitted tunn3l: %#v", payload.Providers)
	}
}

func TestTunnelProvidersRedactsRegionalEvidenceDetails(t *testing.T) {
	now := time.Now().UTC()
	evidencePath := filepath.Join(t.TempDir(), "evidence.json")
	evidence := tunnel.RegionalEvidence{
		ProviderID: "cloudflare-quick", Region: tunnel.RegionCNMainland, Status: tunnel.EvidenceVerified,
		Issuer: "super-secret-token", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Samples: validCLIMainlandSamples(),
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, evidenceJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(t.TempDir(), "providers.json")
	policyBody := fmt.Sprintf(`{"allowed_provider_ids":["cloudflare-quick"],"regional_evidence_paths":[%q]}`, evidencePath)
	if err := os.WriteFile(policyPath, []byte(policyBody), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	app := NewApp(&stdout, io.Discard)
	if err := app.Run(context.Background(), []string{
		"tunnel", "providers", "--region", "cn-mainland", "--provider-policy", policyPath, "--json",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), evidence.Issuer) || strings.Contains(stdout.String(), "china-telecom") {
		t.Fatalf("providers output leaked evidence internals: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"evidence_status": "verified"`) || !strings.Contains(stdout.String(), `"evidence_expires_at"`) {
		t.Fatalf("providers output omitted safe evidence summary: %s", stdout.String())
	}
}

func validCLIMainlandSamples() []tunnel.NetworkSample {
	var samples []tunnel.NetworkSample
	for _, carrier := range []string{"china-telecom", "china-unicom", "china-mobile"} {
		for _, region := range []string{"north", "south"} {
			samples = append(samples, tunnel.NetworkSample{Carrier: carrier, Region: region, Success: true})
		}
	}
	return samples
}

func TestTunnelProbeChecksConfigurationWithoutStartingTunnel(t *testing.T) {
	policyDir, err := canonicalPathThroughExistingAncestor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	knownHostsPath := filepath.Join(policyDir, "known_hosts")
	knownHostsContent := localhostRunOfficialKnownHostsLine + "\n"
	if err := os.WriteFile(knownHostsPath, []byte(knownHostsContent), 0o600); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(policyDir, "providers.json")
	policyBody := fmt.Sprintf(`{"allowed_provider_ids":["localhost-run"],"ssh_known_hosts_paths":{"localhost-run":%q}}`, knownHostsPath)
	if err := os.WriteFile(policyPath, []byte(policyBody), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{
		"tunnel", "probe", "--region", "cn-mainland", "--provider-policy", policyPath, "--json",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SchemaVersion           string `json:"schema_version"`
		ReadOnly                bool   `json:"read_only"`
		PersistentTunnelStarted bool   `json:"persistent_tunnel_started"`
		Providers               []struct {
			ID                   string `json:"id"`
			ExecutableConfigured bool   `json:"executable_configured"`
			KnownHostsConfigured bool   `json:"known_hosts_configured"`
			ConfigurationReady   bool   `json:"configuration_ready"`
			Eligible             bool   `json:"eligible"`
			EligibilityReason    string `json:"eligibility_reason"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode probe output: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if payload.SchemaVersion != "rdev.tunnel-probe.v1" || !payload.ReadOnly || payload.PersistentTunnelStarted {
		t.Fatalf("probe must be explicitly read-only: %#v", payload)
	}
	if len(payload.Providers) != 4 {
		t.Fatalf("expected probe result for every provider, got %#v", payload.Providers)
	}
	var localhostRun map[string]any
	encoded, _ := json.Marshal(payload.Providers)
	var rows []map[string]any
	if err := json.Unmarshal(encoded, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row["id"] == "localhost-run" {
			localhostRun = row
		}
	}
	if localhostRun == nil || localhostRun["known_hosts_configured"] != true || localhostRun["eligible"] != false || localhostRun["eligibility_reason"] != "regional-evidence-missing" {
		t.Fatalf("unexpected localhost.run probe: %#v", localhostRun)
	}
	if strings.Contains(stdout.String(), knownHostsContent) || strings.Contains(stdout.String(), knownHostsPath) || strings.Contains(stdout.String(), policyPath) {
		t.Fatalf("probe output leaked protected policy material: %q", stdout.String())
	}
}

func TestTunnelProbeUsesProviderConfigurationPreflight(t *testing.T) {
	t.Run("built-in trust and managed asset are ready", func(t *testing.T) {
		var stdout bytes.Buffer
		app := NewApp(&stdout, io.Discard)
		if err := app.Run(context.Background(), []string{"tunnel", "probe", "--region", "global", "--json"}); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Providers []tunnelProbeInspection `json:"providers"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		byID := make(map[string]tunnelProbeInspection, len(payload.Providers))
		for _, provider := range payload.Providers {
			byID[provider.ID] = provider
		}
		localhost := byID[tunnel.ProviderLocalhostRun]
		if !localhost.KnownHostsConfigured {
			t.Fatalf("localhost.run built-in trust was not ready: %#v", localhost)
		}
		if _, err := exec.LookPath("ssh"); err == nil && !localhost.ConfigurationReady {
			t.Fatalf("localhost.run configuration should be ready with ssh installed: %#v", localhost)
		}
		tunn3l := byID[tunnel.ProviderTunn3l]
		_, supported := tunn3lManagedAsset(runtime.GOOS, runtime.GOARCH)
		if tunn3l.ConfigurationReady != supported || tunn3l.ExecutableConfigured != supported {
			t.Fatalf("tunn3l managed-asset readiness = %#v, supported=%v", tunn3l, supported)
		}
	})

	tests := []struct {
		name    string
		content string
		mode    os.FileMode
	}{
		{name: "wrong host", content: "wrong.example ssh-ed25519 dGVzdA==\n", mode: 0o600},
		{name: "malformed", content: "[free.pinggy.io]:443 ssh-ed25519 !!!\n", mode: 0o600},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name    string
			content string
			mode    os.FileMode
		}{name: "unsafe permissions", content: "[free.pinggy.io]:443 ssh-ed25519 dGVzdA==\n", mode: 0o666})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			knownHostsPath := filepath.Join(dir, "operator-known-hosts")
			if err := os.WriteFile(knownHostsPath, []byte(test.content), test.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(knownHostsPath, test.mode); err != nil {
				t.Fatal(err)
			}
			policyPath := filepath.Join(dir, "providers.json")
			policyBody := fmt.Sprintf(`{"allowed_provider_ids":["pinggy"],"ssh_known_hosts_paths":{"pinggy":%q}}`, knownHostsPath)
			if err := os.WriteFile(policyPath, []byte(policyBody), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			app := NewApp(&stdout, io.Discard)
			if err := app.Run(context.Background(), []string{"tunnel", "probe", "--region", "global", "--provider-policy", policyPath, "--json"}); err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Providers []tunnelProbeInspection `json:"providers"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			var pinggy tunnelProbeInspection
			for _, provider := range payload.Providers {
				if provider.ID == tunnel.ProviderPinggy {
					pinggy = provider
				}
			}
			if pinggy.ConfigurationReady || pinggy.KnownHostsConfigured || pinggy.Eligible || pinggy.EligibilityReason != "ssh-pin-invalid" {
				t.Fatalf("invalid operator pin failed open: %#v", pinggy)
			}
			for _, forbidden := range []string{knownHostsPath, policyPath, test.content, "wrong.example"} {
				if strings.Contains(stdout.String(), forbidden) {
					t.Fatalf("probe output leaked protected pin material %q: %s", forbidden, stdout.String())
				}
			}
		})
	}
}

func TestSupportSessionConnectPropagatesTunnelPolicyFlags(t *testing.T) {
	for _, name := range []string{"RDEV_HOSTED_GATEWAY_URL", "RDEV_RELAY_GATEWAY_URL", "RDEV_MESH_GATEWAY_URL", "RDEV_VPN_GATEWAY_URL", "RDEV_SSH_GATEWAY_URL"} {
		t.Setenv(name, "")
	}
	policyPath := filepath.Join(t.TempDir(), "providers.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, io.Discard)
	if err := app.Run(context.Background(), []string{
		"support-session", "connect",
		"--region", "cn-mainland",
		"--provider-policy", policyPath,
		"--allow-degraded-direct-handoff",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		CLIStartNowCommand     []string `json:"cli_start_now_command"`
		ForegroundStartCommand []string `json:"foreground_start_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{payload.CLIStartNowCommand, payload.ForegroundStartCommand} {
		joined := strings.Join(command, "\x00")
		if !strings.Contains(joined, "--region\x00cn-mainland") ||
			!strings.Contains(joined, "--provider-policy\x00"+policyPath) ||
			!slices.Contains(command, "--allow-degraded-direct-handoff") {
			t.Fatalf("generated start command dropped tunnel policy flags: %v", command)
		}
	}
}

func TestFindFreeAddrSkipsPortOccupiedOnLoopbackForWildcardTunnel(t *testing.T) {
	loopbackListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("IPv4 loopback is unavailable on this host: %v", err)
	}
	defer loopbackListener.Close()

	_, occupiedPort, err := net.SplitHostPort(loopbackListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	got := findFreeAddr(net.JoinHostPort("0.0.0.0", occupiedPort))
	_, gotPort, err := net.SplitHostPort(got)
	if err != nil {
		t.Fatalf("findFreeAddr returned unparsable address %q: %v", got, err)
	}
	if gotPort == occupiedPort {
		t.Fatalf("findFreeAddr chose %s, but 127.0.0.1:%s is already occupied and would send the managed public tunnel to the wrong process", got, occupiedPort)
	}

	probe, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", gotPort))
	if err != nil {
		t.Fatalf("findFreeAddr selected %s but loopback tunnel target 127.0.0.1:%s is unavailable: %v", got, gotPort, err)
	}
	_ = probe.Close()
}

func TestWriteJSONFile0600TightensExistingFilePermissions(t *testing.T) {
	readyFile := filepath.Join(t.TempDir(), "ready.json")
	if err := os.WriteFile(readyFile, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeJSONFile0600(readyFile, map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(readyFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected existing ready file permissions to tighten to 0600, got %v", info.Mode().Perm())
	}
}

func TestGatewayAssetConfigUsesDirectoryWithExplicitOverrides(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	override := filepath.Join(t.TempDir(), "custom-rdev-bootstrap.exe")
	assets := gatewayAssetConfig(gatewayServeOptions{
		RdevAssetsDir:                 dir,
		RdevBootstrapWindowsAMD64Path: override,
	})
	if assets.RdevBootstrapWindowsAMD64Path != override {
		t.Fatalf("explicit Windows bootstrap should override assets dir: %#v", assets)
	}
	if assets.RdevHostWindowsAMD64Path != filepath.Join(dir, "rdev-host-windows-amd64.exe") ||
		assets.RdevBootstrapDarwinARM64Path != filepath.Join(dir, "rdev-bootstrap-darwin-arm64") ||
		assets.RdevBootstrapDarwinAMD64Path != filepath.Join(dir, "rdev-bootstrap-darwin-amd64") ||
		assets.RdevBootstrapLinuxAMD64Path != filepath.Join(dir, "rdev-bootstrap-linux-amd64") ||
		assets.RdevBootstrapLinuxARM64Path != filepath.Join(dir, "rdev-bootstrap-linux-arm64") {
		t.Fatalf("assets dir should populate platform bootstrap paths: %#v", assets)
	}
}

func TestGatewayServeHelpListsBootstrapOnlyConnectionAssets(t *testing.T) {
	var stderr bytes.Buffer
	app := NewApp(io.Discard, &stderr)
	if err := app.Run(context.Background(), []string{"gateway", "serve", "-h"}); err == nil {
		t.Fatal("gateway serve help did not stop after printing flags")
	}
	help := stderr.String()
	for _, required := range []string{
		"-rdev-bootstrap-windows-amd64",
		"-rdev-bootstrap-windows-arm64",
		"-rdev-bootstrap-darwin-amd64",
		"-rdev-bootstrap-darwin-arm64",
		"-rdev-bootstrap-linux-amd64",
		"-rdev-bootstrap-linux-arm64",
	} {
		if !strings.Contains(help, required) {
			t.Fatalf("gateway serve help omitted bootstrap asset flag %q:\n%s", required, help)
		}
	}
	for _, forbidden := range []string{
		"-rdev-windows-amd64",
		"-rdev-darwin-amd64",
		"-rdev-darwin-arm64",
		"-rdev-linux-amd64",
		"-rdev-linux-arm64",
	} {
		if strings.Contains(help, forbidden) {
			t.Fatalf("gateway serve help still exposes legacy full-helper asset flag %q:\n%s", forbidden, help)
		}
	}
}

func TestConnectionEntryHelpExposesBootstrapCommandOnly(t *testing.T) {
	for _, subcommand := range []string{"plan", "run"} {
		var stderr bytes.Buffer
		app := NewApp(io.Discard, &stderr)
		_ = app.Run(context.Background(), []string{"connection-entry", subcommand, "-h"})
		help := stderr.String()
		if !strings.Contains(help, "-bootstrap-command") {
			t.Fatalf("connection-entry %s help omitted bootstrap command:\n%s", subcommand, help)
		}
		for _, forbidden := range []string{"-rdev-command", "host serve"} {
			if strings.Contains(help, forbidden) {
				t.Fatalf("connection-entry %s help contains legacy path %q:\n%s", subcommand, forbidden, help)
			}
		}
	}
	var inviteHelp bytes.Buffer
	app := NewApp(io.Discard, &inviteHelp)
	_ = app.Run(context.Background(), []string{"invite", "create", "-h"})
	if strings.Contains(inviteHelp.String(), "-rdev-command") {
		t.Fatalf("invite create help exposes legacy target executable selector:\n%s", inviteHelp.String())
	}
}

func TestGatewayServeConfiguresLayeredCandidateAssets(t *testing.T) {
	candidateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(candidateDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFileForCLITest(t, filepath.Join(candidateDir, "layered-assets.json"), "{}\n")
	writeFileForCLITest(t, filepath.Join(candidateDir, "assets", "rdev-host-windows-amd64.exe"), "host\n")
	override := filepath.Join(t.TempDir(), "layered-assets.json")

	assets := gatewayAssetConfig(gatewayServeOptions{RdevAssetsDir: candidateDir})
	if assets.LayeredAssetManifestPath != filepath.Join(candidateDir, "layered-assets.json") {
		t.Fatalf("candidate assets dir should configure layered manifest: %#v", assets)
	}
	if assets.RdevHostWindowsAMD64Path != filepath.Join(candidateDir, "assets", "rdev-host-windows-amd64.exe") {
		t.Fatalf("candidate assets dir should configure staged Windows core runtime: %#v", assets)
	}

	assets = gatewayAssetConfig(gatewayServeOptions{
		RdevAssetsDir:            candidateDir,
		LayeredAssetManifestPath: override,
	})
	if assets.LayeredAssetManifestPath != override {
		t.Fatalf("explicit layered manifest should override candidate assets dir: %#v", assets)
	}

	legacyCandidateDir := t.TempDir()
	writeFileForCLITest(t, filepath.Join(legacyCandidateDir, "layered-assets.json"), "{}\n")
	writeFileForCLITest(t, filepath.Join(legacyCandidateDir, "rdev-host-windows-amd64.exe"), "host\n")
	assets = gatewayAssetConfig(gatewayServeOptions{RdevAssetsDir: legacyCandidateDir})
	if assets.LayeredAssetManifestPath != filepath.Join(legacyCandidateDir, "layered-assets.json") ||
		assets.RdevHostWindowsAMD64Path != filepath.Join(legacyCandidateDir, "rdev-host-windows-amd64.exe") {
		t.Fatalf("explicit canonical candidate layout should retain the flat core and configure its manifest: %#v", assets)
	}

	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"gateway", "serve",
		"--dev",
		"--state", filepath.Join(t.TempDir(), "state.json"),
		"--rdev-assets-dir", candidateDir,
		"--layered-assets-manifest", override,
	})
	if err == nil || !strings.Contains(err.Error(), "persistent storage requires --signing-key") {
		t.Fatalf("gateway serve did not accept layered candidate asset flags: %v", err)
	}
}

func TestSupportSessionAssetConfigServesVerifiedPreSignedWindowsCore(t *testing.T) {
	fixture := newSupportSessionLayeredCandidateFixture(t)
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}

	assets, err := supportSessionAssetConfig(workDir, supportSessionLayeredCandidateOptions{
		AssetsDir:       fixture.dir,
		RootPublicKey:   fixture.rootPublicKey,
		ExpectedVersion: fixture.version,
	}, fixture.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	candidateDir, err := canonicalPathThroughExistingAncestor(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	if assets.LayeredAssetManifestPath != filepath.Join(candidateDir, "layered-assets.json") ||
		assets.RdevHostWindowsAMD64Path != filepath.Join(candidateDir, "assets", "rdev-host-windows-amd64.exe") ||
		assets.RdevBootstrapWindowsARM64Path != filepath.Join(workDir, "bin", "rdev-bootstrap-windows-arm64.exe") {
		t.Fatalf("layered support-session asset config = %#v", assets)
	}
	if _, err := os.Stat(filepath.Join(workDir, "bin", "layered-assets.json")); !os.IsNotExist(err) {
		t.Fatalf("support session unexpectedly synthesized a layered manifest: %v", err)
	}

	server := httpapi.NewServer(gateway.NewMemoryGateway())
	server.Assets = assets
	for requestPath, want := range map[string][]byte{
		"/layered-assets.json":                fixture.manifestContent,
		"/assets/rdev-host-windows-amd64.exe": fixture.coreContent,
	} {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want) {
			t.Fatalf("served layered asset %q = %d %q, want 200 and %q", requestPath, rec.Code, rec.Body.Bytes(), want)
		}
	}
}

func TestSupportSessionAssetConfigRejectsTamperedPreSignedWindowsCore(t *testing.T) {
	fixture := newSupportSessionLayeredCandidateFixture(t)
	if err := os.WriteFile(fixture.corePath, []byte("tampered runtime\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := supportSessionAssetConfig(t.TempDir(), supportSessionLayeredCandidateOptions{
		AssetsDir:       fixture.dir,
		RootPublicKey:   fixture.rootPublicKey,
		ExpectedVersion: fixture.version,
	}, fixture.now.Add(time.Minute))
	if err == nil || !strings.Contains(err.Error(), "does not match signed manifest") {
		t.Fatalf("tampered Windows core runtime error = %v", err)
	}
}

type supportSessionLayeredCandidateFixture struct {
	dir             string
	manifestPath    string
	corePath        string
	rootPublicKey   string
	version         string
	now             time.Time
	manifestContent []byte
	coreContent     []byte
}

func newSupportSessionLayeredCandidateFixture(t *testing.T) supportSessionLayeredCandidateFixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	coreContent := []byte("pre-signed Windows core runtime\n")
	corePath := filepath.Join(assetsDir, "rdev-host-windows-amd64.exe")
	if err := os.WriteFile(corePath, coreContent, 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := signing.Generate("support-session-release-root")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 16, 10, 0, 0, 0, time.UTC)
	version := "v0.2.0-support-session-test"
	sum := sha256.Sum256(coreContent)
	manifest, err := release.SignLayeredAssetManifest(release.LayeredAssetManifest{
		SchemaVersion: release.LayeredAssetManifestSchemaVersion,
		Version:       version,
		GeneratedAt:   now,
		ExpiresAt:     now.Add(time.Hour),
		Assets: []release.LayeredAsset{{
			ID:           "rdev-host-windows-amd64",
			Platform:     "windows/amd64",
			Kind:         "core-runtime",
			RelativePath: "assets/rdev-host-windows-amd64.exe",
			SHA256:       "sha256:" + hex.EncodeToString(sum[:]),
			SizeBytes:    int64(len(coreContent)),
		}},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	manifestContent, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestContent = append(manifestContent, '\n')
	manifestPath := filepath.Join(dir, "layered-assets.json")
	if err := os.WriteFile(manifestPath, manifestContent, 0o600); err != nil {
		t.Fatal(err)
	}
	return supportSessionLayeredCandidateFixture{
		dir:             dir,
		manifestPath:    manifestPath,
		corePath:        corePath,
		rootPublicKey:   encodeRootPublicKey(key.ID, key.PublicKey),
		version:         version,
		now:             now,
		manifestContent: manifestContent,
		coreContent:     coreContent,
	}
}

func TestGatewayServeConfiguresRdevHostWindowsAMD64Asset(t *testing.T) {
	coreRuntime := filepath.Join(t.TempDir(), "rdev-host-windows-amd64.exe")
	opts := gatewayServeOptions{RdevHostWindowsAMD64Path: coreRuntime}
	assets := gatewayAssetConfig(opts)
	if assets.RdevHostWindowsAMD64Path != coreRuntime {
		t.Fatalf("gateway did not configure Windows host core runtime: %#v", assets)
	}
	if !gatewayHasExplicitAssetConfig(opts) {
		t.Fatal("Windows host core runtime should count as explicit gateway asset configuration")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{
		"gateway", "serve",
		"--dev",
		"--state", filepath.Join(t.TempDir(), "state.json"),
		"--rdev-host-windows-amd64", coreRuntime,
	})
	if err == nil || !strings.Contains(err.Error(), "persistent storage requires --signing-key") {
		t.Fatalf("gateway serve did not recognize Windows host runtime flag: %v", err)
	}
}

func TestGatewayServeDevAutoBuildsRdevAssets(t *testing.T) {
	assetsDir, ready, err := prepareGatewayAutoBuildRdevAssets(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatalf("expected gateway bootstrap assets to be ready in %s", assetsDir)
	}
	assets := gatewayAssetConfig(gatewayServeOptions{RdevAssetsDir: assetsDir})
	if assets.RdevBootstrapWindowsAMD64Path == "" {
		t.Fatalf("expected auto-built Windows bootstrap asset path, got %#v", assets)
	}
	info, err := os.Stat(assets.RdevBootstrapWindowsAMD64Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() || info.Size() == 0 {
		t.Fatalf("expected non-empty Windows bootstrap asset, got %#v", info)
	}
}

func waitForHTTP(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(endpoint)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return
			}
			lastErr = fmt.Errorf("status %s", resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %v", endpoint, lastErr)
}

func TestSupportSessionStatusReportsConnectionFeedback(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "company computer support", map[string]string{
		"auto_activate": "attended-temporary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "win-dev",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: []string{"shell.user"},
	}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{
		"support-session", "status",
		"--gateway-url", server.URL,
		"--ticket-code", ticket.Code,
		"--locale", "zh-CN",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Connected     bool   `json:"connected"`
		Status        string `json:"status"`
		Feedback      string `json:"feedback"`
		NextAction    string `json:"next_action"`
		ConnectedNext struct {
			SchemaVersion string `json:"schema_version"`
			Connected     bool   `json:"connected"`
			HostID        string `json:"host_id"`
			UserReport    string `json:"user_report"`
			MCPNextCalls  []struct {
				Tool      string         `json:"tool"`
				Arguments map[string]any `json:"arguments"`
			} `json:"mcp_next_calls"`
		} `json:"connected_next_steps"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid status JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-status.v1" ||
		!payload.Connected ||
		payload.Status != "connected" ||
		!strings.Contains(payload.Feedback, "连接已经建立") ||
		!strings.Contains(payload.NextAction, "汇报连接已建立") {
		t.Fatalf("unexpected support-session status: %#v", payload)
	}
	if payload.ConnectedNext.SchemaVersion != "rdev.support-session-connected-next-steps.v1" ||
		!payload.ConnectedNext.Connected ||
		payload.ConnectedNext.HostID == "" ||
		!strings.Contains(payload.ConnectedNext.UserReport, "连接已经建立") ||
		len(payload.ConnectedNext.MCPNextCalls) != 1 ||
		payload.ConnectedNext.MCPNextCalls[0].Tool != "rdev.sessions.status" {
		t.Fatalf("unexpected connected next-step contract: %#v", payload.ConnectedNext)
	}
}

func TestSupportSessionStatusUsesConfiguredGatewayURL(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", server.URL)

	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "company computer support", map[string]string{
		"auto_activate": "attended-temporary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "win-dev",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: []string{"shell.user"},
	}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"support-session", "status",
		"--ticket-code", ticket.Code,
		"--locale", "en",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Connected     bool   `json:"connected"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid status JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-status.v1" ||
		!payload.Connected ||
		payload.Status != "connected" {
		t.Fatalf("expected configured gateway status feedback, got %#v", payload)
	}
}

func TestSupportSessionStatusWaitTimeoutIncludesRecovery(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "visible support")
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"support-session", "status",
		"--gateway-url", server.URL,
		"--ticket-code", ticket.Code,
		"--wait",
		"--timeout-seconds", "1",
		"--interval", "1ms",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		OK                 bool           `json:"ok"`
		TimedOut           bool           `json:"timed_out"`
		ConnectionRecovery map[string]any `json:"connection_recovery"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid status JSON: %v\n%s", err, stdout.String())
	}
	actions, _ := payload.ConnectionRecovery["agent_next_actions"].([]any)
	forbidden, _ := payload.ConnectionRecovery["forbidden"].([]any)
	if payload.OK ||
		!payload.TimedOut ||
		payload.ConnectionRecovery["schema_version"] != "rdev.support-session-connection-recovery.v1" ||
		payload.ConnectionRecovery["timed_out"] != true ||
		!strings.Contains(strings.Join(anyStrings(actions), "\n"), "connection-entry failure") ||
		!strings.Contains(strings.Join(anyStrings(forbidden), "\n"), "Agent-authored PowerShell") {
		t.Fatalf("expected wait timeout recovery contract, got %#v", payload)
	}
}

func TestSupportSessionRecoverRevokesStaleHostsWithoutLegacyJobs(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "visible support")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "win-stale",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ActivateHost(host.ID, []string{"shell.user"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"support-session", "recover",
		"--gateway-url", server.URL,
		"--ticket-code", ticket.Code,
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion        string           `json:"schema_version"`
		OK                   bool             `json:"ok"`
		StaleHostsSeen       int              `json:"stale_hosts_seen"`
		RetiredHostsObserved []map[string]any `json:"retired_hosts_observed"`
		TaskRecovery         string           `json:"task_recovery"`
		HumanSurfaceRule     string           `json:"human_surface_rule"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid recovery JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-recovery.v1" ||
		!payload.OK ||
		payload.StaleHostsSeen != 1 ||
		len(payload.RetiredHostsObserved) != 1 ||
		!strings.Contains(payload.TaskRecovery, "rdev.sessions.interrupt") ||
		!strings.Contains(payload.HumanSurfaceRule, "Do not ask") {
		t.Fatalf("unexpected recovery payload: %#v", payload)
	}
	recoveredHost, err := gw.Host(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredHost.Status == model.HostStatusRevoked {
		t.Fatalf("recover must not mutate retired host state through legacy HTTP routes, got %s", recoveredHost.Status)
	}
}

func TestInviteCreateUsesGatewayAndOutputsAgentPlan(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
		"--capabilities", "shell.user,codex.run,git.diff",
		"--transport", "wss",
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion         string `json:"schema_version"`
		GatewayURL            string `json:"gateway_url"`
		ManifestURL           string `json:"manifest_url"`
		ManifestRootPublicKey string `json:"manifest_root_public_key"`
		Transport             string `json:"transport"`
		TransportPlan         struct {
			SchemaVersion string `json:"schema_version"`
			Mode          string `json:"mode"`
			Candidates    []struct {
				Transport   string `json:"transport"`
				HostCommand string `json:"host_command"`
			} `json:"candidates"`
		} `json:"transport_plan"`
		ConnectionPlan struct {
			SchemaVersion       string `json:"schema_version"`
			NetworkScope        string `json:"network_scope"`
			GatewayReachability string `json:"gateway_reachability"`
			Implemented         []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"implemented"`
			AgentManaged []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"agent_managed"`
			DiscoveryPlan struct {
				SchemaVersion string   `json:"schema_version"`
				Allowed       []string `json:"allowed"`
			} `json:"discovery_plan"`
			SelectionOrder    []string `json:"selection_order"`
			EnvironmentProbes []string `json:"environment_probes"`
		} `json:"connection_plan"`
		AuthorityProfile struct {
			SchemaVersion  string `json:"schema_version"`
			Profile        string `json:"profile"`
			RemoteHostRole string `json:"remote_host_role"`
			Discovery      struct {
				Allowed bool   `json:"allowed"`
				Scope   string `json:"scope"`
			} `json:"discovery"`
			DownstreamControl struct {
				Allowed bool   `json:"allowed"`
				Scope   string `json:"scope"`
			} `json:"downstream_control"`
			RequiredCapabilities []string `json:"required_capabilities"`
			ControlPaths         []struct {
				ID string `json:"id"`
			} `json:"control_paths"`
		} `json:"authority_profile"`
		ConnectionEntry struct {
			SchemaVersion   string   `json:"schema_version"`
			HandoffName     string   `json:"handoff_name"`
			HandoffContract []string `json:"handoff_contract"`
			EntryURL        string   `json:"entry_url"`
			AutomationLevel string   `json:"automation_level"`
			PackageCatalog  struct {
				SchemaVersion string `json:"schema_version"`
				Candidates    []struct {
					ID                   string `json:"id"`
					PackageStatus        string `json:"package_status"`
					FallbackScriptStatus string `json:"fallback_script_status"`
				} `json:"candidates"`
			} `json:"package_catalog"`
			OneLineCommands map[string]string `json:"one_line_commands"`
			HumanSteps      []string          `json:"human_steps"`
		} `json:"connection_entry"`
		ConnectionEntryPlan struct {
			SchemaVersion         string   `json:"schema_version"`
			Mode                  string   `json:"mode"`
			PackagePlanSchema     string   `json:"package_plan_schema"`
			EntryModes            []string `json:"entry_modes"`
			TargetSelectionPolicy struct {
				SchemaVersion         string   `json:"schema_version"`
				DecisionOwner         string   `json:"decision_owner"`
				DefaultOwnedMode      string   `json:"default_owned_mode"`
				DefaultThirdPartyMode string   `json:"default_third_party_mode"`
				OwnedSignals          []string `json:"owned_signals"`
				ThirdPartySignals     []string `json:"third_party_signals"`
				AskWhen               []string `json:"ask_when"`
				AgentRules            []string `json:"agent_rules"`
			} `json:"target_selection_policy"`
			ModeSelection      []string `json:"mode_selection"`
			RequiredAgentFlow  []string `json:"required_agent_flow"`
			PackageFormats     []string `json:"package_formats"`
			RequiredContents   []string `json:"required_contents"`
			NetworkStrategy    []string `json:"network_strategy"`
			PrivilegeStrategy  []string `json:"privilege_strategy"`
			ImplementationGaps []string `json:"implementation_gaps"`
		} `json:"connection_entry_plan"`
		HostContextPlan struct {
			SchemaVersion         string   `json:"schema_version"`
			StorageLocation       string   `json:"storage_location"`
			ServerContextBudget   string   `json:"server_context_budget"`
			ProgressiveDisclosure []string `json:"progressive_disclosure"`
			HostLocalStores       []string `json:"host_local_stores"`
			GatewayIndexes        []string `json:"gateway_indexes"`
		} `json:"host_context_plan"`
		ProvisioningPlan struct {
			SchemaVersion            string   `json:"schema_version"`
			Mode                     string   `json:"mode"`
			DiscoveryTargets         []string `json:"discovery_targets"`
			AutoInstallAllowed       []string `json:"auto_install_allowed"`
			AuthorizationRequiredFor []string `json:"authorization_required_for"`
		} `json:"agent_provisioning_plan"`
		CollaborationPlan struct {
			SchemaVersion     string   `json:"schema_version"`
			Mode              string   `json:"mode"`
			Protocols         []string `json:"protocols"`
			DiscoveryTargets  []string `json:"discovery_targets"`
			CollaborationUses []string `json:"collaboration_uses"`
			DelegationRules   []string `json:"delegation_rules"`
		} `json:"agent_collaboration_plan"`
		LocalizationPlan struct {
			SchemaVersion      string   `json:"schema_version"`
			Mode               string   `json:"mode"`
			SupportedLanguages []string `json:"supported_languages"`
			DetectionSources   []string `json:"detection_sources"`
			LocalizedSurfaces  []string `json:"localized_surfaces"`
			FallbackOrder      []string `json:"fallback_order"`
		} `json:"localization_plan"`
		ManagedDevPlan struct {
			SchemaVersion       string   `json:"schema_version"`
			Mode                string   `json:"mode"`
			HostModes           []string `json:"host_modes"`
			ServiceSurfaces     []string `json:"service_surfaces"`
			ReliabilityControls []string `json:"reliability_controls"`
			WorkspaceControls   []string `json:"workspace_controls"`
		} `json:"managed_development_plan"`
		HostCommand        string   `json:"host_command"`
		FallbackCommands   []string `json:"fallback_commands"`
		HumanNextActions   []string `json:"human_next_actions"`
		AgentNextActions   []string `json:"agent_next_actions"`
		ConnectivityChecks []string `json:"connectivity_checks"`
		Ticket             struct {
			Code         string   `json:"code"`
			Capabilities []string `json:"capabilities"`
		} `json:"ticket"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid invite JSON: %v\n%s", err, stdout.String())
	}
	legacyEntryField := "customer" + "_bootstrap"
	legacyPlanField := "connector" + "_package_plan"
	if strings.Contains(stdout.String(), legacyEntryField) || strings.Contains(stdout.String(), legacyPlanField) {
		t.Fatalf("invite JSON should use generic connection entry fields, got %s", stdout.String())
	}
	if payload.SchemaVersion != "rdev.agent-invite.v1" {
		t.Fatalf("unexpected schema: %#v", payload)
	}
	if payload.GatewayURL != server.URL {
		t.Fatalf("expected gateway URL %q, got %#v", server.URL, payload.GatewayURL)
	}
	if payload.Transport != "wss" || payload.TransportPlan.Mode != "wss" || len(payload.TransportPlan.Candidates) != 1 {
		t.Fatalf("explicit WSS invite should keep WSS-only plan: %#v", payload.TransportPlan)
	}
	if !strings.Contains(payload.ManifestURL, "/v1/tickets/") || !strings.Contains(payload.HostCommand, "/bootstrap.sh") || !strings.Contains(payload.HostCommand, "rdev-bootstrap") || strings.Contains(payload.HostCommand, "host serve") {
		t.Fatalf("invite should include manifest URL and bootstrap-only host command: %#v", payload)
	}
	if payload.ManifestRootPublicKey == "" {
		t.Fatalf("invite should carry the signed manifest root: %#v", payload)
	}
	if len(payload.TransportPlan.Candidates) == 0 || payload.TransportPlan.Candidates[0].HostCommand != payload.HostCommand {
		t.Fatalf("transport candidates should share the bootstrap attempt command: %#v", payload.TransportPlan.Candidates)
	}
	if len(payload.HumanNextActions) == 0 || len(payload.AgentNextActions) == 0 || len(payload.ConnectivityChecks) == 0 {
		t.Fatalf("invite should split human and agent actions: %#v", payload)
	}
	if payload.ConnectionPlan.SchemaVersion != "rdev.connection-plan.v1" || len(payload.ConnectionPlan.Implemented) < 4 || len(payload.ConnectionPlan.AgentManaged) < 3 {
		t.Fatalf("invite should include implemented and agent-managed connection options: %#v", payload.ConnectionPlan)
	}
	if payload.ConnectionPlan.DiscoveryPlan.SchemaVersion != "rdev.discovery-plan.v1" || len(payload.ConnectionPlan.DiscoveryPlan.Allowed) == 0 {
		t.Fatalf("invite should include discovery plan: %#v", payload.ConnectionPlan)
	}
	if payload.AuthorityProfile.SchemaVersion != "rdev.agent-authority.v1" || payload.AuthorityProfile.Profile != "max-control" || !payload.AuthorityProfile.Discovery.Allowed || !payload.AuthorityProfile.DownstreamControl.Allowed {
		t.Fatalf("invite should include max-control authority profile: %#v", payload.AuthorityProfile)
	}
	if len(payload.AuthorityProfile.ControlPaths) < 3 || !slices.Contains(payload.AuthorityProfile.RequiredCapabilities, "downstream.control.scoped") {
		t.Fatalf("max-control profile should include downstream control paths and capability: %#v", payload.AuthorityProfile)
	}
	if payload.ConnectionEntry.SchemaVersion != "rdev.connection-entry.v1" ||
		payload.ConnectionEntry.HandoffName != "Connection Entry" ||
		payload.ConnectionEntry.EntryURL == "" ||
		len(payload.ConnectionEntry.OneLineCommands) < 2 ||
		len(payload.ConnectionEntry.HandoffContract) == 0 {
		t.Fatalf("invite should include connection entry: %#v", payload.ConnectionEntry)
	}
	if !slices.Contains(payload.ConnectionEntry.HandoffContract, "Target-side humans must not assemble ticket codes, gateway URLs, manifest roots, transports, release roots, or checksums by hand.") {
		t.Fatalf("connection entry should define the universal handoff contract: %#v", payload.ConnectionEntry.HandoffContract)
	}
	if payload.ConnectionEntry.PackageCatalog.SchemaVersion != model.ConnectionEntryPackageCatalogSchemaVersion ||
		len(payload.ConnectionEntry.PackageCatalog.Candidates) == 0 ||
		payload.ConnectionEntry.PackageCatalog.Candidates[0].PackageStatus != "planned-release-asset-required" ||
		payload.ConnectionEntry.PackageCatalog.Candidates[0].FallbackScriptStatus != "available" {
		t.Fatalf("connection entry should include package catalog candidates: %#v", payload.ConnectionEntry.PackageCatalog)
	}
	if !strings.Contains(payload.ConnectionEntry.OneLineCommands["macos_linux_sh"], "/join/") || !strings.Contains(payload.ConnectionEntry.OneLineCommands["windows_powershell"], "bootstrap.ps1") {
		t.Fatalf("connection entry should include one-link commands: %#v", payload.ConnectionEntry.OneLineCommands)
	}
	if payload.ConnectionEntryPlan.SchemaVersion != "rdev.connection-entry-plan.v1" ||
		payload.ConnectionEntryPlan.Mode != "universal-agent-selected-entry" ||
		payload.ConnectionEntryPlan.PackagePlanSchema != "rdev.connection-entry.package-plan.v1" {
		t.Fatalf("invite should include connection entry plan: %#v", payload.ConnectionEntryPlan)
	}
	if len(payload.ConnectionEntryPlan.EntryModes) < 2 ||
		payload.ConnectionEntryPlan.TargetSelectionPolicy.SchemaVersion != "rdev.target-selection-policy.v1" ||
		payload.ConnectionEntryPlan.TargetSelectionPolicy.DefaultOwnedMode != "managed" ||
		payload.ConnectionEntryPlan.TargetSelectionPolicy.DefaultThirdPartyMode != "attended-temporary" ||
		len(payload.ConnectionEntryPlan.TargetSelectionPolicy.OwnedSignals) == 0 ||
		len(payload.ConnectionEntryPlan.TargetSelectionPolicy.ThirdPartySignals) == 0 ||
		len(payload.ConnectionEntryPlan.TargetSelectionPolicy.AskWhen) == 0 ||
		len(payload.ConnectionEntryPlan.TargetSelectionPolicy.AgentRules) == 0 ||
		len(payload.ConnectionEntryPlan.ModeSelection) == 0 ||
		len(payload.ConnectionEntryPlan.RequiredAgentFlow) == 0 ||
		len(payload.ConnectionEntryPlan.PackageFormats) < 3 ||
		len(payload.ConnectionEntryPlan.RequiredContents) == 0 ||
		len(payload.ConnectionEntryPlan.NetworkStrategy) == 0 ||
		len(payload.ConnectionEntryPlan.PrivilegeStrategy) == 0 ||
		len(payload.ConnectionEntryPlan.ImplementationGaps) == 0 {
		t.Fatalf("connection entry plan should define mode, package, network, privilege, and gap details: %#v", payload.ConnectionEntryPlan)
	}
	if !slices.Contains(payload.ConnectionEntryPlan.RequiredAgentFlow, "materialize the invite with CLI-only rdev connection-entry plan before giving target-side instructions") ||
		strings.Contains(strings.Join(payload.ConnectionEntryPlan.RequiredAgentFlow, "\n"), "rdev.connection_entry.") ||
		strings.Contains(strings.Join(payload.ConnectionEntryPlan.RequiredAgentFlow, "\n"), "rdev.invites.") {
		t.Fatalf("connection entry plan should require invite materialization before target handoff: %#v", payload.ConnectionEntryPlan.RequiredAgentFlow)
	}
	if payload.HostContextPlan.SchemaVersion != "rdev.host-context-plan.v1" || payload.HostContextPlan.StorageLocation != "remote-host-first" || payload.HostContextPlan.ServerContextBudget != "index-and-on-demand-slices" {
		t.Fatalf("invite should include host context plan: %#v", payload.HostContextPlan)
	}
	if len(payload.HostContextPlan.ProgressiveDisclosure) == 0 || len(payload.HostContextPlan.HostLocalStores) == 0 || len(payload.HostContextPlan.GatewayIndexes) == 0 {
		t.Fatalf("host context plan should define progressive disclosure and indexes: %#v", payload.HostContextPlan)
	}
	if payload.ProvisioningPlan.SchemaVersion != "rdev.agent-provisioning-plan.v1" || payload.ProvisioningPlan.Mode != "adaptive-host-local" {
		t.Fatalf("invite should include agent provisioning plan: %#v", payload.ProvisioningPlan)
	}
	if len(payload.ProvisioningPlan.DiscoveryTargets) == 0 || len(payload.ProvisioningPlan.AutoInstallAllowed) == 0 || len(payload.ProvisioningPlan.AuthorizationRequiredFor) == 0 {
		t.Fatalf("provisioning plan should define discovery, auto-install, and authorization rules: %#v", payload.ProvisioningPlan)
	}
	if payload.CollaborationPlan.SchemaVersion != "rdev.agent-collaboration-plan.v1" || payload.CollaborationPlan.Mode != "host-local-peer-collaboration" {
		t.Fatalf("invite should include agent collaboration plan: %#v", payload.CollaborationPlan)
	}
	if !slices.Contains(payload.CollaborationPlan.Protocols, "a2a-agent-card") || len(payload.CollaborationPlan.DiscoveryTargets) == 0 || len(payload.CollaborationPlan.CollaborationUses) == 0 || len(payload.CollaborationPlan.DelegationRules) == 0 {
		t.Fatalf("collaboration plan should include A2A discovery and delegation rules: %#v", payload.CollaborationPlan)
	}
	if payload.LocalizationPlan.SchemaVersion != "rdev.localization-plan.v1" || payload.LocalizationPlan.Mode != "target-host-language-auto" {
		t.Fatalf("invite should include localization plan: %#v", payload.LocalizationPlan)
	}
	if !slices.Contains(payload.LocalizationPlan.SupportedLanguages, "zh-CN") || !slices.Contains(payload.LocalizationPlan.SupportedLanguages, "ar") || len(payload.LocalizationPlan.DetectionSources) == 0 || len(payload.LocalizationPlan.LocalizedSurfaces) == 0 || len(payload.LocalizationPlan.FallbackOrder) == 0 {
		t.Fatalf("localization plan should define languages, detection, surfaces, and fallback: %#v", payload.LocalizationPlan)
	}
	if payload.ManagedDevPlan.SchemaVersion != "rdev.managed-development-plan.v1" || payload.ManagedDevPlan.Mode != "owned-long-running-developer-workstation" {
		t.Fatalf("invite should include managed development plan: %#v", payload.ManagedDevPlan)
	}
	if !slices.Contains(payload.ManagedDevPlan.HostModes, "managed") || len(payload.ManagedDevPlan.ServiceSurfaces) == 0 || len(payload.ManagedDevPlan.ReliabilityControls) == 0 || len(payload.ManagedDevPlan.WorkspaceControls) == 0 {
		t.Fatalf("managed development plan should define modes, service surfaces, reliability, and workspace controls: %#v", payload.ManagedDevPlan)
	}
	privateHomePath := strings.Join([]string{"", "Users", "sample-user"}, "/")
	privateWorkspaceMarker := strings.Join([]string{"Documents", "SampleWorkspace"}, "/")
	if strings.Contains(stdout.String(), privateHomePath) || strings.Contains(stdout.String(), privateWorkspaceMarker) {
		t.Fatalf("invite leaked local private path: %s", stdout.String())
	}
}

func TestNormalizeInvitePayloadLoopbackOrigins(t *testing.T) {
	payload := gatewayInviteTicketPayload{
		JoinURL:     "http://localhost:8787/join/ABCD-1234?source=test",
		ManifestURL: "http://127.0.0.1:8787/v1/tickets/ABCD-1234/manifest",
	}
	got, err := normalizeInvitePayloadOrigins(payload, "https://public.example.test/rdev")
	if err != nil {
		t.Fatal(err)
	}
	if got.JoinURL != "https://public.example.test/rdev/join/ABCD-1234?source=test" ||
		got.ManifestURL != "https://public.example.test/rdev/v1/tickets/ABCD-1234/manifest" {
		t.Fatalf("unexpected normalized payload: %#v", got)
	}

	external := gatewayInviteTicketPayload{JoinURL: "https://other.example.test/join/ABCD-1234"}
	unchanged, err := normalizeInvitePayloadOrigins(external, "https://public.example.test")
	if err != nil || unchanged.JoinURL != external.JoinURL {
		t.Fatalf("non-loopback origin must be preserved: %#v err=%v", unchanged, err)
	}
}

func TestCreateSupportSessionPayloadUsesExplicitCapabilities(t *testing.T) {
	explicit := []string{"shell.user", "window.inspect", "screen.screenshot"}
	got := effectiveSupportSessionCapabilities(explicit)
	for _, capability := range explicit {
		if !slices.Contains(got, capability) {
			t.Fatalf("explicit capability %q was dropped: %#v", capability, got)
		}
	}
	if len(got) <= len(explicit) {
		t.Fatalf("explicit capabilities did not receive missing temporary defaults: %#v", got)
	}
	defaults := effectiveSupportSessionCapabilities(nil)
	if !slices.Contains(defaults, "shell.user") || !slices.Contains(defaults, "screen.screenshot") || !slices.Contains(defaults, "input.mouse") {
		t.Fatalf("unexpected temporary defaults: %#v", defaults)
	}
}

func TestInviteCreateDefaultsToAutoTransportPlan(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Transport             string   `json:"transport"`
		ManifestRootPublicKey string   `json:"manifest_root_public_key"`
		HostCommand           string   `json:"host_command"`
		FallbackCommands      []string `json:"fallback_commands"`
		TransportPlan         struct {
			Mode       string `json:"mode"`
			Candidates []struct {
				Transport   string `json:"transport"`
				HostCommand string `json:"host_command"`
			} `json:"candidates"`
		} `json:"transport_plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid invite JSON: %v\n%s", err, stdout.String())
	}
	if payload.Transport != "auto" || !strings.Contains(payload.HostCommand, "rdev-bootstrap") || !strings.Contains(payload.HostCommand, "/bootstrap.sh") {
		t.Fatalf("expected auto bootstrap command, got %#v", payload)
	}
	if payload.ManifestRootPublicKey == "" {
		t.Fatalf("expected invite to include manifest root, got %#v", payload)
	}
	if payload.TransportPlan.Mode != "auto" || len(payload.TransportPlan.Candidates) != 3 {
		t.Fatalf("expected three transport candidates, got %#v", payload.TransportPlan)
	}
	if payload.TransportPlan.Candidates[0].Transport != "wss" || payload.TransportPlan.Candidates[1].Transport != "long-poll" || payload.TransportPlan.Candidates[2].Transport != "poll" {
		t.Fatalf("unexpected transport fallback order: %#v", payload.TransportPlan.Candidates)
	}
	if len(payload.FallbackCommands) != 0 {
		t.Fatalf("bootstrap attempt must own transport fallback, got %#v", payload.FallbackCommands)
	}
	for _, candidate := range payload.TransportPlan.Candidates {
		if candidate.HostCommand != payload.HostCommand {
			t.Fatalf("transport candidates must share one bootstrap command: %#v", payload.TransportPlan.Candidates)
		}
	}
}

func TestInviteCreateLANScopeMarksLANReachability(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
		"--network-scope", "lan",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ConnectionPlan struct {
			NetworkScope        string   `json:"network_scope"`
			GatewayReachability string   `json:"gateway_reachability"`
			SelectionOrder      []string `json:"selection_order"`
		} `json:"connection_plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid invite JSON: %v\n%s", err, stdout.String())
	}
	if payload.ConnectionPlan.NetworkScope != "lan" {
		t.Fatalf("expected lan network scope, got %#v", payload.ConnectionPlan)
	}
	if payload.ConnectionPlan.GatewayReachability != "local-machine" {
		t.Fatalf("expected local-machine reachability for httptest localhost gateway, got %#v", payload.ConnectionPlan)
	}
	if len(payload.ConnectionPlan.SelectionOrder) == 0 || !strings.Contains(payload.ConnectionPlan.SelectionOrder[0], "lan-gateway") {
		t.Fatalf("expected LAN path first in selection order, got %#v", payload.ConnectionPlan.SelectionOrder)
	}
}

func TestConnectionEntryPlanMaterializesGenericPackagePlan(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var inviteOut bytes.Buffer
	inviteApp := NewApp(&inviteOut, &bytes.Buffer{})
	if err := inviteApp.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
		"--transport", "auto",
	}); err != nil {
		t.Fatal(err)
	}

	releaseDir := t.TempDir()
	keyPath := filepath.Join(releaseDir, "release-root.json")
	releaseRoot := signReleaseArtifactWithCLIForTest(t, releaseDir, keyPath, "rdev-bootstrap.exe", "bootstrap-binary")
	bootstrapPath := filepath.Join(releaseDir, "rdev-bootstrap.exe")
	bootstrapManifestPath := bootstrapPath + ".rdev-release.json"
	releaseKey, _, err := signing.LoadOrCreate(keyPath, "release-root")
	if err != nil {
		t.Fatal(err)
	}
	bootstrapManifest, err := release.SignArtifactForRelease(bootstrapPath, releaseKey, time.Now(), "v0.2.0", "windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := release.WriteManifest(bootstrapManifestPath, bootstrapManifest); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "entry")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"connection-entry", "plan",
		"--invite-json", inviteOut.String(),
		"--out", outDir,
		"--target-os", "windows",
		"--target-arch", "amd64",
		"--ownership", "third-party",
		"--windows-bootstrap-binary", bootstrapPath,
		"--windows-bootstrap-release-manifest", bootstrapManifestPath,
		"--layered-assets-manifest-url", "https://agent.example.com/layered-assets.json",
		"--layered-release-version", "v0.2.0",
		"--release-root-public-key", releaseRoot,
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		OK               bool `json:"ok"`
		EntryPackagePlan struct {
			SchemaVersion       string   `json:"schema_version"`
			TargetOS            string   `json:"target_os"`
			SessionMode         string   `json:"session_mode"`
			PlatformPlanKind    string   `json:"platform_plan_kind"`
			LauncherPath        string   `json:"launcher_path"`
			AgentOnlyParameters []string `json:"agent_only_parameters"`
		} `json:"entry_package_plan"`
		Plan struct {
			ConnectionEntryName    string   `json:"connection_entry_name"`
			EntryPackagePlanSchema string   `json:"entry_package_plan_schema"`
			ModeDecision           string   `json:"mode_decision"`
			HumanSurface           []string `json:"human_surface"`
			AgentMetadata          []string `json:"agent_metadata"`
			HandoffContract        []string `json:"handoff_contract"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid connection entry output: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected connection entry plan ok, got %s", stdout.String())
	}
	launcherPath := payload.EntryPackagePlan.LauncherPath
	if !filepath.IsAbs(launcherPath) {
		launcherPath = filepath.Join(outDir, filepath.FromSlash(launcherPath))
	}
	if payload.EntryPackagePlan.SchemaVersion != "rdev.connection-entry.package-plan.v1" ||
		payload.EntryPackagePlan.TargetOS != "windows" ||
		payload.EntryPackagePlan.SessionMode != string(model.HostModeAttendedTemporary) ||
		payload.EntryPackagePlan.PlatformPlanKind != "windows-layered-handoff" ||
		!fileExistsForCLITest(launcherPath) {
		t.Fatalf("expected generic entry package plan wrapping the Windows layered handoff, got %#v", payload.EntryPackagePlan)
	}
	if !slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "manifest_url") ||
		!slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "manifest_root_public_key") ||
		!slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "layered_assets_manifest_url") {
		t.Fatalf("expected raw connection parameters to be agent-only, got %#v", payload.EntryPackagePlan.AgentOnlyParameters)
	}
	if payload.Plan.ConnectionEntryName != "Connection Entry" ||
		payload.Plan.EntryPackagePlanSchema != "rdev.connection-entry.package-plan.v1" ||
		!strings.Contains(payload.Plan.ModeDecision, "attended-temporary") ||
		!slices.Contains(payload.Plan.HumanSurface, "connection_entry.entry_url") ||
		!slices.Contains(payload.Plan.AgentMetadata, "gateway URL") ||
		!slices.Contains(payload.Plan.HandoffContract, "Connection Entry materialization is CLI-only; Agents must run rdev connection-entry plan before giving target-side instructions.") ||
		strings.Contains(strings.Join(payload.Plan.HandoffContract, "\n"), "rdev.connection_entry.") {
		t.Fatalf("expected universal mode decision and split human/agent surfaces, got %#v", payload.Plan)
	}
	if strings.Contains(stdout.String(), "customer_bootstrap") ||
		strings.Contains(stdout.String(), "connector_package_plan") {
		t.Fatalf("connection entry output should not use legacy customer/connector names: %s", stdout.String())
	}
}

func TestWindowsConnectionEntryRequiresLayeredBootstrapHandoff(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var inviteOut bytes.Buffer
	if err := NewApp(&inviteOut, &bytes.Buffer{}).Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
		"--transport", "long-poll",
	}); err != nil {
		t.Fatal(err)
	}

	releaseDir := t.TempDir()
	keyPath := filepath.Join(releaseDir, "release-root.json")
	releaseRoot := signReleaseArtifactWithCLIForTest(t, releaseDir, keyPath, "rdev-bootstrap.exe", "bootstrap-binary")
	bootstrapPath := filepath.Join(releaseDir, "rdev-bootstrap.exe")
	bootstrapManifestPath := bootstrapPath + ".rdev-release.json"
	releaseKey, _, err := signing.LoadOrCreate(keyPath, "release-root")
	if err != nil {
		t.Fatal(err)
	}
	bootstrapManifest, err := release.SignArtifactForRelease(bootstrapPath, releaseKey, time.Now(), "v0.2.0", "windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := release.WriteManifest(bootstrapManifestPath, bootstrapManifest); err != nil {
		t.Fatal(err)
	}

	t.Run("layered prerequisites", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "entry")
		var stdout bytes.Buffer
		err := NewApp(&stdout, &bytes.Buffer{}).Run(context.Background(), []string{
			"connection-entry", "plan",
			"--invite-json", inviteOut.String(),
			"--out", outDir,
			"--target-os", "windows",
			"--target-arch", "amd64",
			"--ownership", "third-party",
			"--windows-bootstrap-binary", bootstrapPath,
			"--windows-bootstrap-release-manifest", bootstrapManifestPath,
			"--layered-assets-manifest-url", "https://downloads.example.com/layered-assets.json",
			"--layered-release-version", "v0.2.0",
			"--release-root-public-key", releaseRoot,
		})
		if err != nil {
			t.Fatalf("expected valid layered inputs to materialize: %v\n%s", err, stdout.String())
		}
		var payload struct {
			OK               bool `json:"ok"`
			EntryPackagePlan struct {
				PackageMode      string `json:"package_mode"`
				PlatformPlanKind string `json:"platform_plan_kind"`
				LauncherPath     string `json:"launcher_path"`
			} `json:"entry_package_plan"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatalf("invalid layered connection entry output: %v\n%s", err, stdout.String())
		}
		launcherPath := payload.EntryPackagePlan.LauncherPath
		if !filepath.IsAbs(launcherPath) {
			launcherPath = filepath.Join(outDir, filepath.FromSlash(launcherPath))
		}
		if !payload.OK ||
			!strings.Contains(payload.EntryPackagePlan.PackageMode, "layered") ||
			!strings.Contains(payload.EntryPackagePlan.PlatformPlanKind, "layered") ||
			!fileExistsForCLITest(launcherPath) {
			t.Fatalf("expected layered Windows handoff selection, got %#v\n%s", payload.EntryPackagePlan, stdout.String())
		}
		launcher, err := os.ReadFile(launcherPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(launcher), "--expected-release-version") || !strings.Contains(string(launcher), "v0.2.0") {
			t.Fatalf("layered launcher did not pin the expected release version:\n%s", launcher)
		}
	})

	t.Run("missing layered prerequisites", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "entry")
		var stdout bytes.Buffer
		err := NewApp(&stdout, &bytes.Buffer{}).Run(context.Background(), []string{
			"connection-entry", "plan",
			"--invite-json", inviteOut.String(),
			"--out", outDir,
			"--target-os", "windows",
			"--target-arch", "amd64",
			"--ownership", "third-party",
			"--release-root-public-key", releaseRoot,
		})
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			OK               bool `json:"ok"`
			EntryPackagePlan struct {
				PlatformPlanKind string `json:"platform_plan_kind"`
				LauncherPath     string `json:"launcher_path"`
			} `json:"entry_package_plan"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatalf("invalid legacy connection entry output: %v\n%s", err, stdout.String())
		}
		if payload.OK || payload.EntryPackagePlan.PlatformPlanKind != "connection-entry-runner" {
			t.Fatalf("expected missing layered inputs to retain only the failed bootstrap runner plan, got %#v\n%s", payload.EntryPackagePlan, stdout.String())
		}
		if fileExistsForCLITest(filepath.Join(outDir, "windows-temporary")) || strings.Contains(stdout.String(), "windows-temporary") {
			t.Fatalf("missing layered inputs generated a legacy Windows helper path:\n%s", stdout.String())
		}
	})
}

func TestConnectionEntryRunWritesRunnerResultEvidence(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var inviteOut bytes.Buffer
	inviteApp := NewApp(&inviteOut, &bytes.Buffer{})
	if err := inviteApp.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "runner evidence",
		"--transport", "auto",
	}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "entry")
	var planOut bytes.Buffer
	app := NewApp(&planOut, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"connection-entry", "plan",
		"--invite-json", inviteOut.String(),
		"--out", outDir,
		"--target-os", "linux",
		"--ownership", "third-party",
		"--release-root-public-key", "release-root:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"--layered-assets-manifest-url", "https://api.example.com/layered-assets.json",
		"--layered-release-version", "v2.0.0-test",
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload struct {
		RunnerPlan struct {
			ManifestPath string `json:"manifest_path"`
		} `json:"runner_plan"`
	}
	if err := json.Unmarshal(planOut.Bytes(), &planPayload); err != nil {
		t.Fatalf("invalid connection entry plan JSON: %v\n%s", err, planOut.String())
	}
	resultOut := filepath.Join(t.TempDir(), "evidence", "runner-result.json")
	helperTranscriptOut := filepath.Join(t.TempDir(), "evidence", "helper-transcript.txt")
	evidenceDir := filepath.Join(t.TempDir(), "standard-evidence")
	var runOut bytes.Buffer
	app = NewApp(&runOut, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"connection-entry", "run",
		"--runner-manifest", planPayload.RunnerPlan.ManifestPath,
		"--dry-run",
		"--probe-timeout", "1s",
		"--result-out", resultOut,
		"--helper-transcript-out", helperTranscriptOut,
		"--evidence-dir", evidenceDir,
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(resultOut)
	if err != nil {
		t.Fatalf("expected runner result evidence: %v", err)
	}
	var result connectionrunner.RunResult
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("invalid runner result evidence: %v\n%s", err, string(content))
	}
	if result.SchemaVersion != "rdev.connection-entry.runner-result.v1" ||
		result.SelectedPath != "native-direct-gateway" ||
		len(result.BootstrapArgs) == 0 ||
		result.Executed {
		t.Fatalf("unexpected runner evidence: %#v\ncli output: %s", result, runOut.String())
	}
	helperTranscript, err := os.ReadFile(helperTranscriptOut)
	if err != nil {
		t.Fatalf("expected helper transcript evidence: %v", err)
	}
	if !strings.Contains(string(helperTranscript), "selected_path native-direct-gateway") ||
		!strings.Contains(string(helperTranscript), "dry_run no_execution") ||
		!strings.Contains(runOut.String(), `"helper_transcript": "`+helperTranscriptOut+`"`) {
		t.Fatalf("unexpected helper transcript evidence:\n%s\ncli output: %s", string(helperTranscript), runOut.String())
	}
	for _, expected := range []string{"runner-result.json", "helper-transcript.txt", "gateway-status.json", "host-status.json", "connection-status.json", "audit.jsonl", "evidence-report.json"} {
		if _, err := os.Stat(filepath.Join(evidenceDir, expected)); err != nil {
			t.Fatalf("expected standard evidence file %s: %v", expected, err)
		}
	}
	if !strings.Contains(runOut.String(), `"schema_version": "rdev.connection-entry.runner-evidence.v1"`) {
		t.Fatalf("expected evidence report in output: %s", runOut.String())
	}
}

func TestConnectionEntryPlanMaterializesManagedLinuxPackagePlan(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var inviteOut bytes.Buffer
	inviteApp := NewApp(&inviteOut, &bytes.Buffer{})
	if err := inviteApp.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--mode", string(model.HostModeManaged),
		"--reason", "owned workstation",
		"--transport", "auto",
	}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "managed-entry")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"connection-entry", "plan",
		"--invite-json", inviteOut.String(),
		"--out", outDir,
		"--target-os", "linux",
		"--ownership", "owned",
		"--managed-binary", "/opt/rdev/rdev",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:" + strings.Repeat("b", 43),
		"--layered-assets-manifest-url", "https://api.example.com/layered-assets.json",
		"--layered-release-version", "v2.0.0-test",
		"--release-bundle-required-artifacts", "rdev,rdev-host,rdev-verify",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		OK               bool `json:"ok"`
		EntryPackagePlan struct {
			SchemaVersion       string   `json:"schema_version"`
			TargetOS            string   `json:"target_os"`
			SessionMode         string   `json:"session_mode"`
			PackageMode         string   `json:"package_mode"`
			PlatformPlanKind    string   `json:"platform_plan_kind"`
			LauncherPath        string   `json:"launcher_path"`
			AgentOnlyParameters []string `json:"agent_only_parameters"`
		} `json:"entry_package_plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid managed connection entry output: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected managed connection entry plan ok, got %s", stdout.String())
	}
	if payload.EntryPackagePlan.SchemaVersion != "rdev.connection-entry.package-plan.v1" ||
		payload.EntryPackagePlan.TargetOS != "linux" ||
		payload.EntryPackagePlan.SessionMode != string(model.HostModeManaged) ||
		payload.EntryPackagePlan.PackageMode != "reviewed-managed-service-connection-entry" ||
		payload.EntryPackagePlan.PlatformPlanKind != "linux-managed-service-plan" ||
		!fileExistsForCLITest(payload.EntryPackagePlan.LauncherPath) {
		t.Fatalf("expected generic entry package plan wrapping Linux managed service plan, got %#v", payload.EntryPackagePlan)
	}
	if !slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "managed_binary_path") ||
		!slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "release_bundle_path") {
		t.Fatalf("expected managed raw parameters to be agent-only, got %#v", payload.EntryPackagePlan.AgentOnlyParameters)
	}
	if !fileExistsForCLITest(filepath.Join(outDir, "managed-linux", "linux-managed-service-plan.json")) {
		t.Fatalf("expected Linux managed service plan in entry package")
	}
}

func TestInviteCreateRequiresGateway(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"invite", "create", "--reason", "repair"})
	if err == nil || !strings.Contains(err.Error(), "requires --gateway") {
		t.Fatalf("expected gateway requirement, got %v", err)
	}
}

func TestGatewayTicketsURLAcceptsAPIRoot(t *testing.T) {
	if got := gatewayTicketsURL("https://api.example.com/v1"); got != "https://api.example.com/v1/tickets" {
		t.Fatalf("unexpected API root tickets URL: %s", got)
	}
	if got := gatewayTicketsURL("https://api.example.com"); got != "https://api.example.com/v1/tickets" {
		t.Fatalf("unexpected gateway root tickets URL: %s", got)
	}
}

func TestOperatorAuthInitAndVerify(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "operators.json")
	tokenDir := filepath.Join(dir, "tokens")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{"operator-auth", "init", "--out", authPath, "--token-dir", tokenDir}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "rdev_") {
		t.Fatalf("init output should not print bearer tokens: %s", stdout.String())
	}
	for _, name := range []string{"admin", "operator", "issuer", "auditor"} {
		if _, err := os.Stat(filepath.Join(tokenDir, name+".token")); err != nil {
			t.Fatalf("expected %s token file: %v", name, err)
		}
	}

	var verifyOut bytes.Buffer
	verifyApp := NewApp(&verifyOut, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{"operator-auth", "verify", "--auth", authPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyOut.String(), `"ok": true`) {
		t.Fatalf("expected verify ok, got %s", verifyOut.String())
	}
}

func TestOperatorAuthVerifyHosted(t *testing.T) {
	publicKey, _, err := operatorauth.GenerateHostedKey()
	if err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(t.TempDir(), "hosted-auth.json")
	authFile := operatorauth.HostedFile{
		SchemaVersion: operatorauth.HostedSchemaVersion,
		Issuer:        "https://auth.example.com/",
		Audience:      "rdev-gateway",
		Keys: []operatorauth.HostedAuthKey{{
			KeyID:     "operator-key",
			PublicKey: operatorauth.EncodePublicKey(publicKey),
		}},
	}
	content, err := json.Marshal(authFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"operator-auth", "verify-hosted", "--auth", authPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), `"key_count": 1`) {
		t.Fatalf("unexpected hosted verify output: %s", stdout.String())
	}
}

func TestOperatorAuthVerifyOIDCJWKSWithToken(t *testing.T) {
	now := time.Now().UTC()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": "oidc-key",
				"use": "sig",
				"alg": "RS256",
				"n":   operatorauth.EncodeRSAJWKValue(privateKey.PublicKey.N),
				"e":   operatorauth.EncodeRSAJWKValue(big.NewInt(int64(privateKey.PublicKey.E))),
			}},
		})
	}))
	defer server.Close()
	root := t.TempDir()
	authPath := filepath.Join(root, "oidc-jwks-auth.json")
	authFile := operatorauth.OIDCJWKSFile{
		SchemaVersion:    operatorauth.OIDCJWKSSchemaVersion,
		Issuer:           "https://issuer.example.test/",
		Audience:         "rdev-gateway",
		JWKSURL:          server.URL,
		RolesClaim:       "rdev_roles",
		ClockSkewSeconds: 30,
	}
	content, err := json.Marshal(authFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := operatorauth.SignOIDCJWKSToken("oidc-key", privateKey, operatorauth.OIDCClaims{
		Issuer:    "https://issuer.example.test/",
		Subject:   "operator@example.test",
		Audiences: []string{"rdev-gateway"},
		ExpiresAt: now.Add(time.Hour).Unix(),
		Roles:     []string{operatorauth.RoleOperator},
	}, "rdev_roles")
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(root, "operator.jwt")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"operator-auth", "verify-oidc-jwks",
		"--auth", authPath,
		"--token-file", tokenPath,
		"--role", "operator",
	}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, `"ok": true`) ||
		!strings.Contains(output, `"schema_version": "rdev.oidc-jwks-operator-auth.v1"`) ||
		!strings.Contains(output, `"token_verified": true`) ||
		!strings.Contains(output, `"key_count": 1`) {
		t.Fatalf("unexpected OIDC JWKS verify output: %s", output)
	}
}

func TestOperatorAuthVerifySAMLConfig(t *testing.T) {
	now := time.Now().UTC()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rdev test idp"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	authPath := filepath.Join(root, "saml-auth.json")
	authFile := operatorauth.SAMLFile{
		SchemaVersion:        operatorauth.SAMLSchemaVersion,
		IDPIssuer:            "https://idp.example.test/saml",
		Audience:             "rdev-gateway",
		AssertionConsumerURL: "https://gateway.example.test/saml/acs",
		RoleAttribute:        "rdev_roles",
		CertificatePEM:       string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})),
	}
	content, err := json.Marshal(authFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"operator-auth", "verify-saml", "--auth", authPath}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, `"ok": true`) ||
		!strings.Contains(output, `"schema_version": "rdev.saml-operator-auth.v1"`) ||
		!strings.Contains(output, `"certificate_count": 1`) ||
		!strings.Contains(output, `"response_verified": false`) {
		t.Fatalf("unexpected SAML verify output: %s", output)
	}
}

func TestGatewayStorageVerifyRejectsUnknownProvider(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "unknown", "--path", filepath.Join(t.TempDir(), "state.json")})
	if err == nil || !strings.Contains(err.Error(), "unsupported gateway storage provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestGatewayStorageVerifyFileProvider(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	path := filepath.Join(t.TempDir(), "state.json")
	if err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "file", "--path", path}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), "file:"+path) {
		t.Fatalf("unexpected storage verify output: %s", stdout.String())
	}
}

func TestGatewayStorageVerifyPostgresRejectsInlinePassword(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "postgres", "--path", "postgres://rdev:secret@example.invalid/rdev"})
	if err == nil || !strings.Contains(err.Error(), "must not contain inline passwords") {
		t.Fatalf("expected inline password rejection, got %v", err)
	}
}

func TestGatewayStorageVerifyRedisRejectsInlineCredentials(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "redis-stream", "--path", "redis://default:secret@example.invalid:6379/0"})
	if err == nil || !strings.Contains(err.Error(), "must not contain inline credentials") {
		t.Fatalf("expected inline credential rejection, got %v", err)
	}
}

func TestHostedProviderPackageAndVerify(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", out,
		"--storage-provider", "file",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packageStdout.String(), `"schema": "rdev.hosted-provider-package.v1"`) ||
		!strings.Contains(packageStdout.String(), `"external_mutation": false`) ||
		!strings.Contains(packageStdout.String(), `"runtime_contract_schema": "rdev.hosted-provider-runtime-contract.v1"`) ||
		!strings.Contains(packageStdout.String(), `"runtime_evidence_plan_schema": "rdev.hosted-provider-runtime-evidence-plan.v1"`) {
		t.Fatalf("unexpected hosted provider package output: %s", packageStdout.String())
	}
	if _, err := os.Stat(filepath.Join(out, "runtime-evidence-plan.json")); err != nil {
		t.Fatalf("expected runtime evidence plan: %v", err)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"hosted-provider", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.hosted-provider-package-verification.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"runtime_evidence_plan_schema": "rdev.hosted-provider-runtime-evidence-plan.v1"`) {
		t.Fatalf("unexpected hosted provider verify output: %s", verifyStdout.String())
	}
}

func TestHostedProviderExternalRuntimeContractPackageAndVerify(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", out,
		"--storage-provider", "postgres",
		"--auth-provider", "oidc-jwks",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packageStdout.String(), `"schema": "rdev.hosted-provider-package.v1"`) ||
		!strings.Contains(packageStdout.String(), `"storage_provider": "postgres"`) ||
		!strings.Contains(packageStdout.String(), `"auth_provider": "oidc-jwks"`) ||
		!strings.Contains(packageStdout.String(), `"runtime_status": "durable-runtime-evidence-required"`) {
		t.Fatalf("unexpected hosted provider package output: %s", packageStdout.String())
	}
	for _, expected := range []string{"hosted-provider.json", "runtime-contract.json", "runtime-evidence-plan.json", "HOSTED_PROVIDER_RUNTIME.md"} {
		if _, err := os.Stat(filepath.Join(out, expected)); err != nil {
			t.Fatalf("expected %s: %v", expected, err)
		}
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"hosted-provider", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.hosted-provider-package-verification.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"storage_provider": "postgres"`) {
		t.Fatalf("unexpected hosted provider verify output: %s", verifyStdout.String())
	}
}

func TestHostedProviderRedisHostedJWTUsesBuiltInGatewayRuntime(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", out,
		"--storage-provider", "redis-stream",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	output := packageStdout.String()
	if !strings.Contains(output, `"storage_provider": "redis-stream"`) ||
		!strings.Contains(output, `"auth_provider": "hosted-ed25519-jwt"`) ||
		!strings.Contains(output, `"redis-stream"`) ||
		strings.Contains(output, "operator-reviewed-hosted-gateway-launcher") {
		t.Fatalf("unexpected redis hosted provider package output: %s", output)
	}
	var manifest struct {
		GatewayArgs []string `json:"gateway_args"`
	}
	content, err := os.ReadFile(filepath.Join(out, "hosted-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(manifest.GatewayArgs, " ")
	if !strings.Contains(joined, "gateway serve --storage-provider redis-stream") ||
		!strings.Contains(joined, "--hosted-operator-auth") {
		t.Fatalf("expected built-in redis gateway args, got %#v", manifest.GatewayArgs)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"hosted-provider", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"storage_provider": "redis-stream"`) {
		t.Fatalf("unexpected verify output: %s", verifyStdout.String())
	}
}

func TestHostedProviderS3CompatibleHostedJWTUsesBuiltInGatewayRuntime(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", out,
		"--storage-provider", "s3-compatible",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	output := packageStdout.String()
	if !strings.Contains(output, `"storage_provider": "s3-compatible"`) ||
		!strings.Contains(output, `"auth_provider": "hosted-ed25519-jwt"`) ||
		!strings.Contains(output, `"s3-compatible"`) ||
		strings.Contains(output, "operator-reviewed-hosted-gateway-launcher") {
		t.Fatalf("unexpected s3-compatible hosted provider package output: %s", output)
	}
	var manifest struct {
		GatewayArgs []string `json:"gateway_args"`
	}
	content, err := os.ReadFile(filepath.Join(out, "hosted-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(manifest.GatewayArgs, " ")
	if !strings.Contains(joined, "gateway serve --storage-provider s3-compatible") ||
		!strings.Contains(joined, "--hosted-operator-auth") {
		t.Fatalf("expected built-in s3-compatible gateway args, got %#v", manifest.GatewayArgs)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"hosted-provider", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"storage_provider": "s3-compatible"`) {
		t.Fatalf("unexpected verify output: %s", verifyStdout.String())
	}
}

func TestGatewayStorageVerifyS3CompatibleRejectsUnsafeLocation(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"gateway", "storage", "verify",
		"--provider", "s3-compatible",
		"--path", "s3://example-bucket/rdev?secret=inline",
	})
	if err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("expected unsafe S3-compatible location rejection, got %v output=%s", err, stdout.String())
	}
}

func TestRelayAdapterPackageAndVerify(t *testing.T) {
	out := filepath.Join(t.TempDir(), "relay")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"relay-adapter", "package",
		"--out", out,
		"--adapter", "chisel",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packageStdout.String(), `"schema": "rdev.relay-adapter-package.v1"`) ||
		!strings.Contains(packageStdout.String(), `"external_mutation": false`) ||
		!strings.Contains(packageStdout.String(), `"adapter_kind": "chisel"`) ||
		!strings.Contains(packageStdout.String(), `"acceptance_evidence_plan_schema": "rdev.relay-adapter-acceptance-evidence-plan.v1"`) {
		t.Fatalf("unexpected relay adapter package output: %s", packageStdout.String())
	}
	if _, err := os.Stat(filepath.Join(out, "acceptance-evidence-plan.json")); err != nil {
		t.Fatalf("expected acceptance evidence plan: %v", err)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"relay-adapter", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.relay-adapter-package-verification.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"adapter_kind": "chisel"`) ||
		!strings.Contains(verifyStdout.String(), `"acceptance_evidence_plan_schema": "rdev.relay-adapter-acceptance-evidence-plan.v1"`) {
		t.Fatalf("unexpected relay adapter verify output: %s", verifyStdout.String())
	}
}

func TestRelayAdapterPackageSupportsMeshSSHAndVPNKinds(t *testing.T) {
	for _, tc := range []struct {
		adapter    string
		kind       string
		helperTool string
		envVar     string
	}{
		{adapter: "ssh-tunnel", kind: "ssh-tunnel", helperTool: "ssh", envVar: "RDEV_SSH_GATEWAY_URL"},
		{adapter: "headscale-tailscale", kind: "headscale-tailscale", helperTool: "tailscale", envVar: "RDEV_MESH_GATEWAY_URL"},
		{adapter: "wireguard", kind: "wireguard", helperTool: "wg", envVar: "RDEV_VPN_GATEWAY_URL"},
	} {
		t.Run(tc.adapter, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "adapter")
			var packageStdout bytes.Buffer
			app := NewApp(&packageStdout, &bytes.Buffer{})
			if err := app.Run(context.Background(), []string{
				"relay-adapter", "package",
				"--out", out,
				"--adapter", tc.adapter,
			}); err != nil {
				t.Fatal(err)
			}
			output := packageStdout.String()
			for _, expected := range []string{
				`"schema": "rdev.relay-adapter-package.v1"`,
				`"adapter_kind": "` + tc.kind + `"`,
				`"helper_tool": "` + tc.helperTool + `"`,
				tc.envVar,
			} {
				if !strings.Contains(output, expected) {
					t.Fatalf("expected %q in output: %s", expected, output)
				}
			}

			var verifyStdout bytes.Buffer
			app = NewApp(&verifyStdout, &bytes.Buffer{})
			if err := app.Run(context.Background(), []string{"relay-adapter", "verify", "--package", out}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(verifyStdout.String(), `"ok": true`) ||
				!strings.Contains(verifyStdout.String(), `"adapter_kind": "`+tc.kind+`"`) {
				t.Fatalf("unexpected verify output: %s", verifyStdout.String())
			}
		})
	}
}

func TestAcceptanceScaffoldEvidenceForHostedProviderAndRelayPlans(t *testing.T) {
	root := t.TempDir()
	providerOut := filepath.Join(root, "hosted-provider")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", providerOut,
		"--storage-provider", "postgres",
		"--auth-provider", "oidc-jwks",
	}); err != nil {
		t.Fatal(err)
	}

	hostedScaffold := filepath.Join(root, "hosted-scaffold")
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "scaffold-evidence",
		"--hosted-provider-package", providerOut,
		"--out", hostedScaffold,
	}); err != nil {
		t.Fatal(err)
	}
	hostedOutput := stdout.String()
	for _, expected := range []string{
		`"schema": "rdev.acceptance-evidence-scaffold.v1"`,
		`"plan_schema": "rdev.hosted-provider-runtime-evidence-plan.v1"`,
		`"plan_kind": "hosted-provider-runtime"`,
		`"ready_for_packaging": false`,
		`"package-hosted-provider-runtime"`,
	} {
		if !strings.Contains(hostedOutput, expected) {
			t.Fatalf("expected %q in hosted scaffold output: %s", expected, hostedOutput)
		}
	}
	if _, err := os.Stat(filepath.Join(hostedScaffold, "gateway-startup.txt")); !os.IsNotExist(err) {
		t.Fatalf("default hosted scaffold must not create placeholders, err=%v", err)
	}
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"acceptance", "evidence-status",
		"--scaffold", hostedScaffold,
	})
	if err == nil {
		t.Fatalf("missing hosted evidence status should fail")
	}
	if !strings.Contains(stdout.String(), `"schema": "rdev.acceptance-evidence-status.v1"`) ||
		!strings.Contains(stdout.String(), `"ready_for_packaging": false`) ||
		!strings.Contains(stdout.String(), `"missing_count": 9`) {
		t.Fatalf("unexpected missing hosted status output: %s", stdout.String())
	}
	writeFile(t, filepath.Join(hostedScaffold, "gateway-startup.txt"), "gateway started\n")
	writeFile(t, filepath.Join(hostedScaffold, "storage-verification.json"), `{"ok":true}`)
	writeFile(t, filepath.Join(hostedScaffold, "auth-verification.json"), `{"ok":true}`)
	writeFile(t, filepath.Join(hostedScaffold, "backup-evidence.txt"), "backup complete\n")
	writeFile(t, filepath.Join(hostedScaffold, "restore-evidence.txt"), "restore complete\n")
	writeFile(t, filepath.Join(hostedScaffold, "retention-evidence.txt"), "retention reviewed\n")
	writeFile(t, filepath.Join(hostedScaffold, "role-mapping-evidence.json"), `{"probes":[{"authorized":true},{"authorized":false}]}`)
	writeFile(t, filepath.Join(hostedScaffold, "failure-mode-evidence.json"), `{"ok":true,"failure_mode_tested":true,"rejected":true}`)
	writeFile(t, filepath.Join(hostedScaffold, "audit.jsonl"), `{"event":"hosted_acceptance"}`)
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "evidence-status",
		"--scaffold", hostedScaffold,
	}); err != nil {
		t.Fatalf("real hosted evidence status should pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ready_for_packaging": true`) ||
		!strings.Contains(stdout.String(), `"required_ready": 9`) {
		t.Fatalf("unexpected ready hosted status output: %s", stdout.String())
	}

	relayOut := filepath.Join(root, "relay-adapter")
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"relay-adapter", "package",
		"--out", relayOut,
		"--adapter", "wireguard",
	}); err != nil {
		t.Fatal(err)
	}
	relayScaffold := filepath.Join(root, "relay-scaffold")
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "scaffold-evidence",
		"--relay-adapter-package", relayOut,
		"--out", relayScaffold,
		"--create-placeholders",
	}); err != nil {
		t.Fatal(err)
	}
	relayOutput := stdout.String()
	for _, expected := range []string{
		`"schema": "rdev.acceptance-evidence-scaffold.v1"`,
		`"plan_schema": "rdev.relay-adapter-acceptance-evidence-plan.v1"`,
		`"plan_kind": "relay-adapter"`,
		`"create_placeholders": true`,
		`"package-relay-adapter"`,
	} {
		if !strings.Contains(relayOutput, expected) {
			t.Fatalf("expected %q in relay scaffold output: %s", expected, relayOutput)
		}
	}
	content, err := os.ReadFile(filepath.Join(relayScaffold, "runner-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"placeholder": true`) {
		t.Fatalf("expected placeholder runner result, got %s", string(content))
	}
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"acceptance", "evidence-status",
		"--scaffold", relayScaffold,
	})
	if err == nil {
		t.Fatalf("placeholder relay evidence status should fail")
	}
	if !strings.Contains(stdout.String(), `"placeholder_count": 6`) ||
		!strings.Contains(stdout.String(), `"ready_for_packaging": false`) {
		t.Fatalf("unexpected placeholder relay status output: %s", stdout.String())
	}
}

func TestMCPServeProcessesInitialize(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = reader
	_, _ = writer.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}` + "\n")
	_ = writer.Close()

	if err := app.Run(context.Background(), []string{"mcp", "serve"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"protocolVersion":"2025-11-25"`) {
		t.Fatalf("expected initialize response, got %q", stdout.String())
	}
}

func TestHostInstallServiceWritesMacOSLaunchAgentPlist(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "LaunchAgents", "com.example.rdev-host.plist")
	binaryPath := filepath.Join(dir, "bin", "rdev")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", binaryPath,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--identity-store", filepath.Join(dir, "identity.json"),
		"--trust-store", filepath.Join(dir, "trust.json"),
		"--workspace-lock-store", filepath.Join(dir, "workspace-locks"),
		"--log-dir", filepath.Join(dir, "logs"),
		"--plist-out", plistPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := readFileForTest(t, plistPath)
	for _, expected := range []string{
		"<key>Label</key>",
		"<string>com.example.rdev-host</string>",
		"<string>host</string>",
		"<string>serve</string>",
		"<string>managed</string>",
		"<string>https://api.example.com/v1</string>",
		"<string>ABCD-1234</string>",
		"<string>--workspace-lock-store</string>",
		"<string>" + filepath.Join(dir, "workspace-locks") + "</string>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected plist to contain %q, got %s", expected, content)
		}
	}
	info, err := os.Stat(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected plist permissions 0600, got %#o", got)
	}
	if !strings.Contains(stdout.String(), `"launchctl bootstrap gui/$(id -u) `+plistPath+`"`) {
		t.Fatalf("expected launchctl next step in stdout, got %s", stdout.String())
	}
}

func TestHostInstallServiceDoesNotOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	if err := os.WriteFile(plistPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	})
	if err == nil {
		t.Fatal("expected existing plist to fail without --force")
	}
	if got := readFileForTest(t, plistPath); got != "existing" {
		t.Fatalf("expected existing plist to remain unchanged, got %q", got)
	}
}

func TestHostInstallServiceWritesLinuxSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "systemd", "user", "rdev-host.service")
	binaryPath := filepath.Join(dir, "bin", "rdev")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--binary", binaryPath,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--identity-store", filepath.Join(dir, "identity.json"),
		"--trust-store", filepath.Join(dir, "trust.json"),
		"--workspace-lock-store", filepath.Join(dir, "workspace-locks"),
		"--log-dir", filepath.Join(dir, "logs"),
		"--unit-out", unitPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := readFileForTest(t, unitPath)
	for _, expected := range []string{
		"[Unit]",
		"Description=Remote Dev Skillkit managed host",
		"[Service]",
		"ExecStart=" + binaryPath + " host serve --mode managed",
		"--gateway https://api.example.com/v1",
		"--ticket-code ABCD-1234",
		"--workspace-lock-store " + filepath.Join(dir, "workspace-locks"),
		"Restart=on-failure",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
		"StandardOutput=append:" + filepath.Join(dir, "logs", "rdev-host.out.log"),
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected unit to contain %q, got %s", expected, content)
		}
	}
	info, err := os.Stat(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected unit permissions 0600, got %#o", got)
	}
	for _, expected := range []string{
		`"platform": "linux"`,
		`"unit_name": "rdev-host.service"`,
		`"systemctl --user enable --now rdev-host.service"`,
		`systemctl was not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected stdout to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostInstallServicePlansWindowsService(t *testing.T) {
	binaryPath := `C:\Program Files\rdev\rdev.exe`
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "windows",
		"--label", "RemoteDevSkillkitHost",
		"--binary", binaryPath,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--identity-store", `C:\ProgramData\rdev\identity.json`,
		"--trust-store", `C:\ProgramData\rdev\trust.json`,
		"--workspace-lock-store", `C:\ProgramData\rdev\workspace-locks`,
		"--release-bundle", `C:\Program Files\rdev\release-bundle.json`,
		"--release-root-public-key", "release-root:abc123",
		"--release-require-artifacts", "rdev-host.exe,rdev-verify.exe",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"platform": "windows"`,
		`"service_name": "RemoteDevSkillkitHost"`,
		`"sc.exe"`,
		`"create"`,
		`C:\\Program Files\\rdev\\rdev.exe`,
		`"--mode"`,
		`"managed"`,
		`"--release-bundle"`,
		`C:\\Program Files\\rdev\\release-bundle.json`,
		`"start_type": "demand"`,
		`dry-run only`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected Windows service output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostInstallServiceRejectsRelativeWindowsBinaryPath(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "windows",
		"--binary", `rdev.exe`,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
	})
	if err == nil || !strings.Contains(err.Error(), "binary path must be absolute") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
}

func TestHostServiceStatusReadsMacOSLaunchAgentPlist(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	statusApp := NewApp(&stdout, &bytes.Buffer{})
	if err := statusApp.Run(context.Background(), []string{
		"host", "service-status",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"exists": true`,
		`"label": "com.example.rdev-host"`,
		`"launchctl print gui/$(id -u)/com.example.rdev-host"`,
		`launchctl was not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected status output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceStatusReadsLinuxSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "rdev-host.service")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--unit-out", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	statusApp := NewApp(&stdout, &bytes.Buffer{})
	if err := statusApp.Run(context.Background(), []string{
		"host", "service-status",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--unit", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"exists": true`,
		`"unit_name": "rdev-host.service"`,
		`"systemctl --user status rdev-host.service"`,
		`systemctl was not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected status output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceStatusPlansWindowsCommands(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "service-status",
		"--platform", "windows",
		"--label", "RemoteDevSkillkitHost",
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"platform": "windows"`,
		`"service_name": "RemoteDevSkillkitHost"`,
		`"query"`,
		`"qc"`,
		`status commands were not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected Windows status output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceControlDryRunPlansLaunchctl(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	controlApp := NewApp(&stdout, &bytes.Buffer{})
	if err := controlApp.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "macos",
		"--action", "start",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
		"--domain", "gui/501",
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"execute": false`,
		`"action": "start"`,
		`"launchctl"`,
		`"bootstrap"`,
		`"gui/501"`,
		`dry-run only`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected service-control output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceControlDryRunPlansSystemd(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "rdev-host.service")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--unit-out", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	controlApp := NewApp(&stdout, &bytes.Buffer{})
	if err := controlApp.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "linux",
		"--action", "start",
		"--label", "rdev-host.service",
		"--unit", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"execute": false`,
		`"action": "start"`,
		`"systemctl"`,
		`"daemon-reload"`,
		`"enable"`,
		`"--now"`,
		`dry-run only`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected service-control output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceControlDryRunPlansWindowsService(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "windows",
		"--action", "start",
		"--label", "RemoteDevSkillkitHost",
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"execute": false`,
		`"platform": "windows"`,
		`"action": "start"`,
		`"sc.exe"`,
		`"start"`,
		`dry-run only`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected Windows service-control output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceControlRejectsLabelMismatch(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.other.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.other.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "macos",
		"--action", "start",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
	})
	if err == nil {
		t.Fatal("expected label mismatch to fail")
	}
	if !strings.Contains(err.Error(), "refusing service-control") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostServiceControlRejectsSystemdUnitMismatch(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "other-rdev-host.service")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "other-rdev-host.service",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--unit-out", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "linux",
		"--action", "start",
		"--label", "rdev-host.service",
		"--unit", unitPath,
	})
	if err == nil {
		t.Fatal("expected unit mismatch to fail")
	}
	if !strings.Contains(err.Error(), "refusing service-control") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostUninstallServiceRemovesMacOSLaunchAgentPlist(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	uninstallApp := NewApp(&stdout, &bytes.Buffer{})
	if err := uninstallApp.Run(context.Background(), []string{
		"host", "uninstall-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Fatalf("expected plist removal, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), `"removed": true`) {
		t.Fatalf("expected removed output, got %s", stdout.String())
	}
}

func TestHostUninstallServiceRemovesLinuxSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "rdev-host.service")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--unit-out", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	uninstallApp := NewApp(&stdout, &bytes.Buffer{})
	if err := uninstallApp.Run(context.Background(), []string{
		"host", "uninstall-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--unit", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit removal, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), `"removed": true`) {
		t.Fatalf("expected removed output, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `systemctl was not executed`) {
		t.Fatalf("expected no systemctl execution note, got %s", stdout.String())
	}
}

func TestHostUninstallServicePlansWindowsServiceRemoval(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "uninstall-service",
		"--platform", "windows",
		"--label", "RemoteDevSkillkitHost",
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"platform": "windows"`,
		`"service_name": "RemoteDevSkillkitHost"`,
		`"stop"`,
		`"delete"`,
		`stop/delete commands were not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected Windows uninstall output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostUninstallServiceRejectsLabelMismatch(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.other.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.other.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{
		"host", "uninstall-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
	})
	if err == nil {
		t.Fatal("expected label mismatch to fail")
	}
	if !strings.Contains(err.Error(), "refusing to remove plist") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(plistPath); err != nil {
		t.Fatalf("plist should remain after mismatch: %v", err)
	}
}

func TestHostServeRejectsUnknownMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"host", "serve", "--mode", "hidden"})
	if err == nil {
		t.Fatal("expected unsupported mode to fail")
	}
	if !strings.Contains(err.Error(), "unsupported host mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostServeWithoutTicketExplainsPlaceholder(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "provide --gateway and --ticket-code") {
		t.Fatalf("expected ticket-code guidance, got %q", stdout.String())
	}
}

func TestHostServeMTLSGatewayRejectsMissingClientCertificate(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(httpapi.NewServer(gw).Handler())
	server.TLS = config
	server.StartTLS()
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--gateway-ca", material.CACert,
		"--ticket-code", ticket.Code,
		"--name", "missing-client-cert-host",
	})
	if err == nil {
		t.Fatalf("expected mTLS registration without client certificate to fail, got output %s", stdout.String())
	}
	if strings.Contains(err.Error(), "local dev gateways only") {
		t.Fatalf("https local gateway should pass the local dev URL gate: %v", err)
	}
	if len(gw.Hosts("")) != 0 {
		t.Fatalf("expected no registered hosts after mTLS failure, got %d", len(gw.Hosts("")))
	}
}

func TestHostServeRejectsNonLocalHTTPSGateway(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary", "--gateway", "https://api.example.com/v1", "--ticket-code", "ABCD-1234"})
	if err == nil {
		t.Fatal("expected non-local gateway registration to fail")
	}
	if !strings.Contains(err.Error(), "requires --manifest-url with --manifest-root-public-key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSignedManifestGatewayURLAllowsPrivateLANHTTPAndHTTPS(t *testing.T) {
	allowed := []string{
		"http://10.0.0.8:8787",
		"http://172.16.4.5:8787",
		"http://rdev-gateway.local:8787",
		"https://api.example.com/v1",
	}
	for _, value := range allowed {
		if !isSignedManifestGatewayURL(value, true) {
			t.Fatalf("expected signed manifest gateway URL to be allowed: %s", value)
		}
	}

	rejected := []string{
		"http://198.51.100.10:8787",
		"http://10.0.0.8",
		"ws://10.0.0.8:8787",
		"https://api.example.com/v1",
	}
	for _, value := range rejected {
		verified := true
		if strings.HasPrefix(value, "https://") {
			verified = false
		}
		if isSignedManifestGatewayURL(value, verified) {
			t.Fatalf("expected gateway URL to be rejected: %s verified=%v", value, verified)
		}
	}
}

func TestHostServeRejectsTamperedReleaseBundleBeforeRegistration(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "release-root.json")
	root := signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-host", "host-binary")
	signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-verify", "verify-binary")
	bundlePath := createReleaseBundleForHostServeTest(t, dir, keyPath, "rdev-host,rdev-verify")
	if err := os.WriteFile(filepath.Join(dir, "rdev-host"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--release-bundle", bundlePath,
		"--release-root-public-key", root,
		"--release-require-artifacts", "rdev-host,rdev-verify",
	})
	if err == nil {
		t.Fatalf("expected tampered release gate to fail, got output %s", stdout.String())
	}
	if !strings.Contains(err.Error(), "host release bundle verification failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gw.Hosts("")) != 0 {
		t.Fatalf("release gate failed after registering hosts: %#v", gw.Hosts(""))
	}
}

func TestHostServeFetchesEnrollmentRevocationsBeforeRegistration(t *testing.T) {
	dir := t.TempDir()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := model.NewTicket(model.HostModeAttendedTemporary, 600, capabilities, "revoked temporary host", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, rootPublicKey, _, certificate, issuerPrivateKey := writeHostServeEnrollmentCertificateFixture(t, dir, ticket, "revoked-host")
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: fingerprint,
			Reason:                 "host retired",
			RevokedAt:              now,
		},
	}, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var registerCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/enrollment/revocations":
			_ = json.NewEncoder(w).Encode(map[string]any{"revocations": revocations})
		case r.Method == http.MethodPost && r.URL.Path == "/should-not-register":
			registerCalled.Store(true)
			http.Error(w, "registration should not be attempted", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--identity-store", filepath.Join(dir, "identity", "host.json"),
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--fetch-enrollment-revocations",
		"--enrollment-root-public-key", rootPublicKey,
		"--name", "revoked-host",
	})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected local revocation rejection, got %v\nstdout=%s", err, stdout.String())
	}
	if registerCalled.Load() {
		t.Fatalf("registration endpoint was called after local revocation rejection")
	}
}

func TestHostServeRequiresExplicitEnrollmentRevocationFetch(t *testing.T) {
	dir := t.TempDir()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := model.NewTicket(model.HostModeAttendedTemporary, 600, capabilities, "certified temporary host", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, rootPublicKey, _, _, _ := writeHostServeEnrollmentCertificateFixture(t, dir, ticket, "cert-host")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", "http://127.0.0.1:8787",
		"--ticket-code", ticket.Code,
		"--identity-store", filepath.Join(dir, "identity", "host.json"),
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--enrollment-root-public-key", rootPublicKey,
		"--name", "cert-host",
	})
	if err == nil || !strings.Contains(err.Error(), "--fetch-enrollment-revocations") {
		t.Fatalf("expected explicit fetch flag requirement, got %v", err)
	}
}

func writeHostServeEnrollmentCertificateFixture(t *testing.T, dir string, ticket model.Ticket, name string) (string, string, model.TrustBundle, model.HostEnrollmentCertificate, ed25519.PrivateKey) {
	t.Helper()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	return writeHostServeEnrollmentCertificateFixtureWithRoot(t, dir, ticket, name, root, issuerPrivateKey, time.Hour)
}

func writeHostServeEnrollmentCertificateFixtureWithRoot(t *testing.T, dir string, ticket model.Ticket, name string, root model.TrustBundle, issuerPrivateKey ed25519.PrivateKey, ttl time.Duration) (string, string, model.TrustBundle, model.HostEnrollmentCertificate, ed25519.PrivateKey) {
	t.Helper()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	issuerPublicKey, err := root.Ed25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                name,
		OS:                  runtime.GOOS,
		Arch:                runtime.GOARCH,
		Capabilities:        ticket.Capabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, root.SigningKeyID, issuerPrivateKey, time.Now().UTC().Add(-time.Minute), ttl)
	if err != nil {
		t.Fatal(err)
	}
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	if err := writeEnrollmentCertificateFile(certificatePath, certificate, false); err != nil {
		t.Fatal(err)
	}
	return certificatePath, encodeRootPublicKey(root.SigningKeyID, issuerPublicKey), root, certificate, issuerPrivateKey
}

func TestEnrollmentFetchRevocationsWritesVerifiedList(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: "sha256:enrollment-fetch-revoked-test",
			Reason:                 "compromised",
			RevokedAt:              now,
		},
	}, "enrollment-root", privateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentRoot(model.NewTrustBundle("enrollment-root", publicKey)).
		WithEnrollmentRevocations(revocations)
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	outPath := filepath.Join(t.TempDir(), "revocations", "revocations.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "fetch-revocations",
		"--gateway", server.URL,
		"--root-public-key", encodeRootPublicKey("enrollment-root", publicKey),
		"--out", outPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("expected ok fetch output, got %s", stdout.String())
	}
	fetched, err := readEnrollmentRevocationListFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.VerifyHostEnrollmentRevocationListSignature(fetched, model.NewTrustBundle("enrollment-root", publicKey), time.Now()); err != nil {
		t.Fatalf("expected fetched revocations to verify: %v", err)
	}
	if len(fetched.RevokedCertificates) != 1 {
		t.Fatalf("expected one revoked certificate, got %d", len(fetched.RevokedCertificates))
	}
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	err = app.Run(context.Background(), []string{
		"enrollment", "fetch-revocations",
		"--gateway", server.URL,
		"--root-public-key", encodeRootPublicKey("enrollment-root", wrongPublicKey),
		"--out", filepath.Join(t.TempDir(), "wrong-root.json"),
	})
	if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected wrong root to reject fetched revocations, got %v", err)
	}
}

func TestEnrollmentFetchRevocationsSendsOperatorTokenFromFile(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList(nil, "enrollment-root", privateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	seenAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/enrollment/revocations" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"revocations": revocations})
	}))
	defer server.Close()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "operator-token.txt")
	if err := os.WriteFile(tokenPath, []byte("operator-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "revocations", "revocations.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "fetch-revocations",
		"--gateway", server.URL,
		"--root-public-key", encodeRootPublicKey("enrollment-root", publicKey),
		"--operator-token-file", tokenPath,
		"--out", outPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer operator-secret" {
		t.Fatalf("expected bearer token header, got %q", seenAuthorization)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("expected ok fetch output, got %s", stdout.String())
	}
}

func TestEnrollmentInitRevocationsWritesEmptyVerifiedList(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "keys", "enrollment-root.json")
	revocationsPath := filepath.Join(dir, "revocations", "revocations.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"enrollment", "init-revocations",
		"--out", revocationsPath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK                      bool   `json:"ok"`
		Schema                  string `json:"schema"`
		RootPublicKey           string `json:"root_public_key"`
		RevokedCertificateCount int    `json:"revoked_certificate_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid init output: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != model.HostEnrollmentRevocationListSchemaVersion || payload.RevokedCertificateCount != 0 {
		t.Fatalf("unexpected init output: %s", stdout.String())
	}
	revocations, err := readEnrollmentRevocationListFile(revocationsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(revocations.RevokedCertificates) != 0 {
		t.Fatalf("expected empty revocation list, got %d entries", len(revocations.RevokedCertificates))
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"enrollment", "verify-revocations",
		"--revocations", revocationsPath,
		"--root-public-key", payload.RootPublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"revoked_certificate_count": 0`) {
		t.Fatalf("expected empty revocation verification, got %s", verifyStdout.String())
	}
}

func TestEnrollmentLifecycleKeyCustodyWritesRecord(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "custody.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "lifecycle", "key-custody",
		"--root-public-key", encodeRootPublicKey("enrollment-root", publicKey),
		"--custodian", "release-team",
		"--provider", "kms",
		"--rotation-days", "30",
		"--out", outPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(readFileForTest(t, outPath), `"schema_version": "rdev.enrollment-key-custody.v1"`) {
		t.Fatalf("unexpected key custody output: %s", stdout.String())
	}
}

func TestEnrollmentLifecycleFleetRenewalPlanRequiresRevocations(t *testing.T) {
	certificatesPath := filepath.Join(t.TempDir(), "certificates.json")
	if err := os.WriteFile(certificatesPath, []byte(`{"certificates":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "lifecycle", "fleet-renewal-plan",
		"--certificates", certificatesPath,
		"--root-public-key", encodeRootPublicKey("enrollment-root", publicKey),
	})
	if err == nil || !strings.Contains(err.Error(), "revocations are required by policy") {
		t.Fatalf("expected missing revocations to fail, got %v", err)
	}
}

func TestEnrollmentLifecycleEmergencyDrillWritesEvidence(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "enrollment-root.json")
	revocationsPath := filepath.Join(dir, "revocations.json")
	initOut := bytes.Buffer{}
	initApp := NewApp(&initOut, &bytes.Buffer{})
	if err := initApp.Run(context.Background(), []string{
		"enrollment", "init-revocations",
		"--out", revocationsPath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
	}); err != nil {
		t.Fatal(err)
	}
	var initPayload struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(initOut.Bytes(), &initPayload); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "drill.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"enrollment", "lifecycle", "emergency-drill",
		"--name", "root-compromise-drill",
		"--scenario", "enrollment-root-compromise",
		"--operator-role", "admin",
		"--root-public-key", initPayload.RootPublicKey,
		"--revocations", revocationsPath,
		"--out", outPath,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(readFileForTest(t, outPath), `"schema_version": "rdev.enrollment-emergency-drill.v1"`) {
		t.Fatalf("unexpected emergency drill output: %s", stdout.String())
	}
	if strings.Contains(readFileForTest(t, outPath), dir) {
		t.Fatalf("drill evidence leaked local temp path: %s", readFileForTest(t, outPath))
	}
}

func TestEnrollmentIssueCertificateWritesVerifiedGatewayCertificate(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	capabilities := []string{"shell.user", "git.diff"}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "issue-certificate",
		"--gateway", server.URL,
		"--out", certificatePath,
		"--root-public-key", encodeRootPublicKey("enrollment-root", issuerPublicKey),
		"--ticket-code", ticket.Code,
		"--name", "managed-mac",
		"--os", "darwin",
		"--arch", "arm64",
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--capabilities", "shell.user",
		"--valid-minutes", "30",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK                     bool   `json:"ok"`
		Schema                 string `json:"schema"`
		CertificatePath        string `json:"certificate_path"`
		CertificateFingerprint string `json:"certificate_fingerprint"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid issue output: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != model.HostEnrollmentCertificateSchemaVersion || payload.CertificatePath != certificatePath || payload.CertificateFingerprint == "" {
		t.Fatalf("unexpected issue output: %s", stdout.String())
	}
	certificate, err := readEnrollmentCertificateFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.TicketCode != ticket.Code || certificate.HostName != "managed-mac" || certificate.Mode != model.HostModeManaged {
		t.Fatalf("unexpected issued certificate: %#v", certificate)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, now); err != nil {
		t.Fatalf("issued certificate should verify: %v", err)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"enrollment", "verify-certificate",
		"--certificate", certificatePath,
		"--root-public-key", encodeRootPublicKey("enrollment-root", issuerPublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected issued certificate verification, got %s", verifyStdout.String())
	}
}

func TestEnrollmentIssueCertificateRejectsWrongPinnedRoot(t *testing.T) {
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(model.NewTrustBundle("enrollment-root", issuerPublicKey), issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, []string{"shell.user"}, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	identityPath := filepath.Join(t.TempDir(), "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "certs", "host-enrollment.json")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "issue-certificate",
		"--gateway", server.URL,
		"--out", outPath,
		"--root-public-key", encodeRootPublicKey("enrollment-root", wrongPublicKey),
		"--ticket-code", ticket.Code,
		"--name", "managed-mac",
		"--os", "darwin",
		"--arch", "arm64",
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match pinned root-public-key") {
		t.Fatalf("expected pinned root rejection, got %v", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no certificate to be written, stat err=%v", statErr)
	}
}

func TestEnrollmentIssueCertificateSendsOperatorTokenFromFile(t *testing.T) {
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	capabilities := []string{"shell.user"}
	ticket, err := model.NewTicket(model.HostModeManaged, 600, capabilities, "managed enrollment", now)
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(t.TempDir(), "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        capabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, root.SigningKeyID, issuerPrivateKey, now, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	seenAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/enrollment/certificates" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"certificate":             certificate,
			"certificate_fingerprint": fingerprint,
			"enrollment_root":         root,
		})
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "operator-token.txt")
	if err := os.WriteFile(tokenPath, []byte(" operator-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "certs", "host-enrollment.json")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "issue-certificate",
		"--gateway", server.URL,
		"--out", outPath,
		"--root-public-key", encodeRootPublicKey(root.SigningKeyID, issuerPublicKey),
		"--ticket-code", ticket.Code,
		"--name", registration.Name,
		"--os", registration.OS,
		"--arch", registration.Arch,
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--operator-token-file", tokenPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer operator-secret" {
		t.Fatalf("expected bearer token header, got %q", seenAuthorization)
	}
	if _, err := readEnrollmentCertificateFile(outPath); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentRenewCertificateExtendsVerifiedCertificate(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "renew enrollment certificate")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "keys", "enrollment-root.json")
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	var signStdout bytes.Buffer
	signApp := NewApp(&signStdout, &bytes.Buffer{})
	err = signApp.Run(context.Background(), []string{
		"enrollment", "sign-certificate",
		"--out", certificatePath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
		"--ticket-code", ticket.Code,
		"--mode", "managed",
		"--name", "renew-host",
		"--os", runtime.GOOS,
		"--arch", runtime.GOARCH,
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--capabilities", strings.Join(capabilities, ","),
		"--valid-minutes", "30",
	})
	if err != nil {
		t.Fatal(err)
	}
	var signPayload struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(signStdout.Bytes(), &signPayload); err != nil {
		t.Fatalf("invalid sign output: %v\n%s", err, signStdout.String())
	}
	original, err := readEnrollmentCertificateFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	originalFingerprint, err := model.HostEnrollmentCertificateFingerprint(original)
	if err != nil {
		t.Fatal(err)
	}

	revocationsPath := filepath.Join(dir, "revocations", "revocations.json")
	var initStdout bytes.Buffer
	initApp := NewApp(&initStdout, &bytes.Buffer{})
	err = initApp.Run(context.Background(), []string{
		"enrollment", "init-revocations",
		"--out", revocationsPath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
	})
	if err != nil {
		t.Fatal(err)
	}

	renewedPath := filepath.Join(dir, "certs", "host-enrollment-renewed.json")
	var renewStdout bytes.Buffer
	renewApp := NewApp(&renewStdout, &bytes.Buffer{})
	err = renewApp.Run(context.Background(), []string{
		"enrollment", "renew-certificate",
		"--certificate", certificatePath,
		"--out", renewedPath,
		"--key", keyPath,
		"--revocations", revocationsPath,
		"--valid-minutes", "120",
	})
	if err != nil {
		t.Fatal(err)
	}
	var renewPayload struct {
		OK                             bool   `json:"ok"`
		Schema                         string `json:"schema"`
		PreviousCertificateFingerprint string `json:"previous_certificate_fingerprint"`
		CertificateFingerprint         string `json:"certificate_fingerprint"`
		RootPublicKey                  string `json:"root_public_key"`
	}
	if err := json.Unmarshal(renewStdout.Bytes(), &renewPayload); err != nil {
		t.Fatalf("invalid renew output: %v\n%s", err, renewStdout.String())
	}
	if !renewPayload.OK || renewPayload.Schema != model.HostEnrollmentCertificateSchemaVersion {
		t.Fatalf("unexpected renew output: %s", renewStdout.String())
	}
	if renewPayload.RootPublicKey != signPayload.RootPublicKey {
		t.Fatalf("expected same enrollment root, got sign=%q renew=%q", signPayload.RootPublicKey, renewPayload.RootPublicKey)
	}
	if renewPayload.PreviousCertificateFingerprint != originalFingerprint {
		t.Fatalf("expected previous fingerprint %q, got %q", originalFingerprint, renewPayload.PreviousCertificateFingerprint)
	}
	if renewPayload.CertificateFingerprint == originalFingerprint {
		t.Fatalf("expected renewed fingerprint to change, got %q", renewPayload.CertificateFingerprint)
	}
	renewed, err := readEnrollmentCertificateFile(renewedPath)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.TicketCode != original.TicketCode || renewed.Mode != original.Mode || renewed.HostName != original.HostName || renewed.SubjectIdentityFingerprint != original.SubjectIdentityFingerprint {
		t.Fatalf("renewal changed certificate scope: before=%#v after=%#v", original, renewed)
	}
	if renewed.OS != original.OS || renewed.Arch != original.Arch || !slices.Equal(renewed.Capabilities, original.Capabilities) {
		t.Fatalf("renewal changed platform or capabilities: before=%#v after=%#v", original, renewed)
	}
	if !renewed.NotAfter.After(original.NotAfter) {
		t.Fatalf("expected renewed certificate to extend validity: before=%s after=%s", original.NotAfter, renewed.NotAfter)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"enrollment", "verify-certificate",
		"--certificate", renewedPath,
		"--root-public-key", signPayload.RootPublicKey,
		"--revocations", revocationsPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected renewed certificate verification, got %s", verifyStdout.String())
	}
}

func TestEnrollmentRenewCertificateFromGatewayWritesVerifiedCertificate(t *testing.T) {
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	capabilities := []string{"shell.user"}
	ticket, err := model.NewTicket(model.HostModeManaged, 600, capabilities, "managed enrollment", now)
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(t.TempDir(), "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        capabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, root.SigningKeyID, issuerPrivateKey, now, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := model.RenewHostEnrollmentCertificate(certificate, root, issuerPrivateKey, now, 120*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	renewedFingerprint, err := model.HostEnrollmentCertificateFingerprint(renewed)
	if err != nil {
		t.Fatal(err)
	}
	seenAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/enrollment/certificates/renew" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"certificate":                      renewed,
			"certificate_fingerprint":          renewedFingerprint,
			"previous_certificate_fingerprint": previousFingerprint,
			"enrollment_root":                  root,
		})
	}))
	defer server.Close()
	certificatePath := filepath.Join(t.TempDir(), "certs", "host-enrollment.json")
	if err := writeEnrollmentCertificateFile(certificatePath, certificate, false); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(t.TempDir(), "operator-token.txt")
	if err := os.WriteFile(tokenPath, []byte("operator-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "certs", "host-enrollment-renewed.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "renew-certificate",
		"--certificate", certificatePath,
		"--out", outPath,
		"--gateway", server.URL,
		"--root-public-key", encodeRootPublicKey(root.SigningKeyID, issuerPublicKey),
		"--operator-token-file", tokenPath,
		"--valid-minutes", "120",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer operator-secret" {
		t.Fatalf("expected bearer token header, got %q", seenAuthorization)
	}
	var payload struct {
		OK                             bool   `json:"ok"`
		Schema                         string `json:"schema"`
		PreviousCertificateFingerprint string `json:"previous_certificate_fingerprint"`
		CertificateFingerprint         string `json:"certificate_fingerprint"`
		RootPublicKey                  string `json:"root_public_key"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid renew output: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != model.HostEnrollmentCertificateSchemaVersion || payload.RootPublicKey != encodeRootPublicKey(root.SigningKeyID, issuerPublicKey) {
		t.Fatalf("unexpected renew output: %s", stdout.String())
	}
	if payload.PreviousCertificateFingerprint != previousFingerprint || payload.CertificateFingerprint != renewedFingerprint {
		t.Fatalf("unexpected fingerprints in renew output: %s", stdout.String())
	}
	written, err := readEnrollmentCertificateFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(written, root, time.Now()); err != nil {
		t.Fatalf("written renewed certificate should verify: %v", err)
	}
}

func TestEnrollmentRenewCertificateRejectsRevokedCertificate(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "revoked renewal")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "keys", "enrollment-root.json")
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	var signStdout bytes.Buffer
	signApp := NewApp(&signStdout, &bytes.Buffer{})
	err = signApp.Run(context.Background(), []string{
		"enrollment", "sign-certificate",
		"--out", certificatePath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
		"--ticket-code", ticket.Code,
		"--mode", "managed",
		"--name", "revoked-renew-host",
		"--os", runtime.GOOS,
		"--arch", runtime.GOARCH,
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--capabilities", strings.Join(capabilities, ","),
	})
	if err != nil {
		t.Fatal(err)
	}

	revocationsPath := filepath.Join(dir, "revocations", "revocations.json")
	var revokeStdout bytes.Buffer
	revokeApp := NewApp(&revokeStdout, &bytes.Buffer{})
	err = revokeApp.Run(context.Background(), []string{
		"enrollment", "revoke-certificate",
		"--out", revocationsPath,
		"--key", keyPath,
		"--certificate", certificatePath,
		"--reason", "renewal blocked",
	})
	if err != nil {
		t.Fatal(err)
	}
	var renewStdout bytes.Buffer
	renewApp := NewApp(&renewStdout, &bytes.Buffer{})
	err = renewApp.Run(context.Background(), []string{
		"enrollment", "renew-certificate",
		"--certificate", certificatePath,
		"--out", filepath.Join(dir, "certs", "host-enrollment-renewed.json"),
		"--key", keyPath,
		"--revocations", revocationsPath,
	})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked certificate renewal failure, got %v", err)
	}
}

func TestFetchJoinManifestRejectsWrongManifestRoot(t *testing.T) {
	gatewayPublicKey, gatewayPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifestPublicKey, manifestPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, "gateway-tasks", gatewayPublicKey, gatewayPrivateKey).
		WithManifestSigningKey("manifest-root", manifestPublicKey, manifestPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilitiesToStrings(policy.TemporaryDefaults()), "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	_, err = fetchJoinManifest(
		context.Background(),
		nil,
		server.URL+"/v1/tickets/"+ticket.Code+"/manifest",
		"",
		encodeRootPublicKey("manifest-root", wrongPublicKey),
	)
	if err == nil || !strings.Contains(err.Error(), "verify gateway time proof") || !strings.Contains(err.Error(), "signature invalid") {
		t.Fatalf("expected gateway time proof signature failure, got %v", err)
	}
}

func TestFetchJoinManifestRejectsPinMismatch(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilitiesToStrings(policy.TemporaryDefaults()), "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	_, err = fetchJoinManifest(context.Background(), nil, server.URL+"/v1/tickets/"+ticket.Code+"/manifest", "sha256:0000", "")
	if err == nil {
		t.Fatal("expected trust pin mismatch")
	}
	if !strings.Contains(err.Error(), "trust pin mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostServeSessionCompletesTask(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Reason:       "session task smoke",
		Capabilities: []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() {
		done <- app.hostServe(ctx, hostServeOptions{
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
		IdempotencyKey: "cli-session-task",
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

func TestRetiredJobCreateCLIHasNoCompatibility(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"job", "create",
		"--gateway-url", "http://gateway.test",
		"--host-id", "hst_1",
		"--adapter", "shell",
		"--intent", "cli create test",
		"--policy-json", `{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}`,
	})
	if err == nil || !strings.Contains(err.Error(), `unknown command "job"`) {
		t.Fatalf("expected removed job create command error, got err=%v stdout=%s", err, stdout.String())
	}
}

func TestRetiredJobWaitCLIHasNoCompatibility(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"job", "wait",
		"--gateway-url", "http://gateway.test",
		"--job-id", "job_1",
		"--timeout-seconds", "1",
	})
	if err == nil || !strings.Contains(err.Error(), `unknown command "job"`) {
		t.Fatalf("expected removed job wait command error, got err=%v stdout=%s", err, stdout.String())
	}
}

func TestFilesReadCLICreatesFileAdapterTask(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/sess_1/tasks" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"task":{"id":"task_file","status":"offered"}}`))
	}))
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"files", "read",
		"--gateway-url", server.URL,
		"--session-id", "sess_1",
		"--path", "README.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := received["payload"].(map[string]any)
	if received["adapter"] != "file" ||
		received["intent"] != "read remote file README.md" ||
		payload["workspace_root"] != "~" ||
		payload["action"] != "read" ||
		payload["path"] != "README.md" {
		t.Fatalf("unexpected files read task payload: %#v", received)
	}
}

func TestSupportSessionSmokePoliciesDefaultToHomeWorkspace(t *testing.T) {
	policies := []map[string]any{
		fileListSmokePolicy(),
		desktopWindowInspectSmokePolicy(),
		powershellAuditPolicy("Get-Location"),
		shellAuditPolicy([]string{"shell.user"}, []string{"sh", "-c", "pwd"}, []string{"sh"}),
		shellAuditPolicyWithWriteScope([]string{"shell.user", "fs.write.scoped"}, []string{"sh", "-c", "true"}, []string{"sh"}, []string{"."}),
	}
	for _, policy := range policies {
		if policy["workspace_root"] != "~" {
			t.Fatalf("expected generated support-session policy to default workspace_root to home, got %#v", policy)
		}
	}
}

func TestFilesUploadCLISendsExpectedTransferEvidence(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/sess_1/tasks" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"task":{"id":"task_file","status":"offered"}}`))
	}))
	defer server.Close()
	localContent := []byte("hello transfer")
	sum := sha256.Sum256(localContent)
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"files", "upload",
		"--gateway-url", server.URL,
		"--session-id", "sess_1",
		"--path", "remote-control-upload.txt",
		"--content", string(localContent),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := received["payload"].(map[string]any)
	if payload["expected_bytes"] != float64(len(localContent)) ||
		payload["expected_sha256"] != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("expected transfer evidence in upload payload, got %#v", payload)
	}
}

func TestDesktopScreenshotCLICreatesDesktopAdapterTask(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/sess_1/tasks" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"task":{"id":"task_desktop","status":"offered"}}`))
	}))
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"desktop", "screenshot",
		"--gateway-url", server.URL,
		"--session-id", "sess_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := received["payload"].(map[string]any)
	if received["adapter"] != "desktop" ||
		received["intent"] != "capture remote desktop screenshot" ||
		payload["workspace_root"] != "~" ||
		payload["action"] != "screen.screenshot" ||
		payload["max_output_bytes"] != float64(65536) ||
		payload["output_path"] == nil || payload["output_path"] == "" {
		t.Fatalf("unexpected desktop screenshot task payload: %#v", received)
	}
	if _, ok := payload["authorizations_required"]; ok {
		t.Fatalf("expected screenshot task without authorization, got %#v", payload)
	}
}

func TestFilesDeleteCLICreatesAuthorizationGatedFileAdapterTask(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/sess_1/tasks" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"task":{"id":"task_delete","status":"offered"}}`))
	}))
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"files", "delete",
		"--gateway-url", server.URL,
		"--session-id", "sess_1",
		"--path", "old.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := received["payload"].(map[string]any)
	authorizations := payload["authorizations_required"].([]any)
	if received["adapter"] != "file" ||
		payload["action"] != "delete" ||
		len(authorizations) != 1 ||
		authorizations[0] != "file.delete" {
		t.Fatalf("unexpected files delete task payload: %#v", received)
	}
}

func TestDesktopClipboardCLICreatesDesktopAdapterTask(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/sess_1/tasks" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"task":{"id":"task_clipboard","status":"offered"}}`))
	}))
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"desktop", "clipboard",
		"--gateway-url", server.URL,
		"--session-id", "sess_1",
		"--action", "write",
		"--text", "hello clipboard",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := received["payload"].(map[string]any)
	if received["adapter"] != "desktop" ||
		payload["action"] != "clipboard.write" ||
		payload["text"] != "hello clipboard" {
		t.Fatalf("unexpected desktop clipboard task payload: %#v", received)
	}
	if _, ok := payload["authorizations_required"]; ok {
		t.Fatalf("expected clipboard task without authorization, got %#v", payload)
	}
}

func TestRetiredJobArtifactsCLIHasNoCompatibility(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"job", "artifacts",
		"--gateway-url", "http://gateway.test",
		"--job-id", "job_1",
	})
	if err == nil || !strings.Contains(err.Error(), `unknown command "job"`) {
		t.Fatalf("expected removed job artifacts command error, got err=%v stdout=%s", err, stdout.String())
	}
}

func TestTaskPolicyTemplateCLIOutputsSafeWindowsProcessProbe(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"task", "policy-template",
		"--capability", "process.inspect",
		"--target-os", "windows",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SchemaVersion string         `json:"schema_version"`
		Capability    string         `json:"capability"`
		Adapter       string         `json:"adapter"`
		Policy        map[string]any `json:"policy"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid policy template output: %v\n%s", err, stdout.String())
	}
	argv := payload.Policy["argv"].([]any)
	allowCommands := payload.Policy["allow_commands"].([]any)
	if payload.SchemaVersion != "rdev.task-policy-template.v1" ||
		payload.Capability != "process.inspect" ||
		payload.Adapter != "shell" ||
		len(argv) != 1 ||
		argv[0] != "tasklist" ||
		len(allowCommands) != 1 ||
		allowCommands[0] != "tasklist" {
		t.Fatalf("expected simple Windows tasklist template, got %s", stdout.String())
	}
}

func TestSupportSessionReportSummarizesTasksAndArtifacts(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "report-host",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ActivateHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Reason:       "report test",
		Capabilities: []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, endpoint, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "report-host",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-report",
		Capabilities:        []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := gw.SubmitSessionTask(session.ID, controlplane.TaskSpec{
		TargetEndpointID: endpoint.ID,
		Adapter:          "shell",
		Intent:           "basic identity probe",
		Capabilities:     []string{"shell.user"},
		Payload: map[string]any{
			"workspace_root": ".",
			"argv":           []any{"cmd", "/c", "hostname"},
			"allow_commands": []any{"cmd"},
		},
		IdempotencyKey: "report-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CompleteSessionTask(session.ID, task.ID, map[string]any{"status": "succeeded", "artifact_content": "REPORT-HOST"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err = app.Run(context.Background(), []string{
		"support-session", "report",
		"--gateway-url", server.URL,
		"--host-id", host.ID,
		"--session-id", session.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SchemaVersion      string           `json:"schema_version"`
		HumanReport        string           `json:"human_report"`
		RemoteControlEntry map[string]any   `json:"remote_control_entry"`
		LiveRemoteE2EPlan  map[string]any   `json:"live_remote_e2e_plan"`
		Tasks              []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support-session report output: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-report.v1" ||
		len(payload.Tasks) != 1 ||
		!strings.Contains(payload.HumanReport, "report-host") ||
		!strings.Contains(payload.HumanReport, "basic identity probe") ||
		payload.RemoteControlEntry["explicit_disconnect_required"] != true ||
		payload.LiveRemoteE2EPlan["schema_version"] != "rdev.support-session-live-e2e-plan.v1" ||
		payload.LiveRemoteE2EPlan["dry_run"] != true {
		t.Fatalf("expected support-session report with artifact evidence, got %s", stdout.String())
	}
	gates, _ := payload.LiveRemoteE2EPlan["gates"].([]any)
	if len(gates) != 3 {
		t.Fatalf("expected report to include live E2E gates, got %#v", payload.LiveRemoteE2EPlan)
	}
}

func TestSupportSessionReportTicketCodeSelectsSingleActiveHost(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "ticket-report-host",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ActivateHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	session, err := gw.Session(ticket.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, endpoint, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "ticket-report-host",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-ticket-report",
		Capabilities:        []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := gw.SubmitSessionTask(session.ID, controlplane.TaskSpec{
		TargetEndpointID: endpoint.ID,
		Adapter:          "shell",
		Intent:           "basic identity probe",
		Capabilities:     []string{"shell.user"},
		Payload: map[string]any{
			"workspace_root": ".",
			"argv":           []any{"cmd", "/c", "hostname"},
			"allow_commands": []any{"cmd"},
		},
		IdempotencyKey: "ticket-report-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CompleteSessionTask(session.ID, task.ID, map[string]any{"status": "succeeded", "artifact_content": "REPORT-HOST"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err = app.Run(context.Background(), []string{
		"support-session", "report",
		"--gateway-url", server.URL,
		"--ticket-code", ticket.Code,
		"--session-id", session.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SchemaVersion      string           `json:"schema_version"`
		OK                 bool             `json:"ok"`
		TicketCode         string           `json:"ticket_code"`
		HostID             string           `json:"host_id"`
		SessionID          string           `json:"session_id"`
		HumanReport        string           `json:"human_report"`
		RemoteControlEntry map[string]any   `json:"remote_control_entry"`
		Tasks              []map[string]any `json:"tasks"`
		ActiveHosts        []map[string]any `json:"active_hosts"`
		StaleHostRule      string           `json:"stale_host_rule"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support-session report output: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-report.v1" ||
		!payload.OK ||
		payload.TicketCode != ticket.Code ||
		payload.HostID != host.ID ||
		payload.SessionID != session.ID ||
		len(payload.ActiveHosts) != 1 ||
		len(payload.Tasks) != 1 ||
		!strings.Contains(payload.HumanReport, "basic identity probe") ||
		payload.RemoteControlEntry["session_passcode"] != ticket.Code ||
		payload.RemoteControlEntry["explicit_disconnect_required"] != true ||
		!strings.Contains(payload.StaleHostRule, "Do not send new session tasks") {
		t.Fatalf("expected ticket-code report to select one active host, got %s", stdout.String())
	}
}

func TestSupportSessionReportTicketCodeUsesBoundEndpointWithoutLegacyHost(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "endpoint report")
	if err != nil {
		t.Fatal(err)
	}
	_, endpoint, _, _, err := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "endpoint-report-target",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-endpoint-report",
		Capabilities:        []string{"shell.user"},
		Transport:           controlplane.TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := gw.SubmitSessionTask(ticket.SessionID, controlplane.TaskSpec{
		TargetEndpointID: endpoint.ID,
		Adapter:          "shell",
		Intent:           "endpoint identity probe",
		Capabilities:     []string{"shell.user"},
		IdempotencyKey:   "endpoint-report-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CompleteSessionTask(ticket.SessionID, task.ID, map[string]any{"status": "succeeded"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"support-session", "report", "--gateway-url", server.URL, "--ticket-code", ticket.Code,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK               bool             `json:"ok"`
		HostID           string           `json:"host_id"`
		SessionID        string           `json:"session_id"`
		TargetEndpointID string           `json:"target_endpoint_id"`
		Tasks            []map[string]any `json:"tasks"`
		Host             map[string]any   `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.HostID != "" || payload.SessionID != ticket.SessionID || payload.TargetEndpointID != endpoint.ID || len(payload.Tasks) != 1 || payload.Host["id"] != endpoint.ID {
		t.Fatalf("ticket-only report did not consume the bound endpoint IDs: %s", stdout.String())
	}
}

func TestSupportSessionReportRejectsExplicitSessionOutsideTicketBinding(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "binding mismatch")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget}); err != nil {
		t.Fatal(err)
	}
	standalone, err := gw.CreateSession(controlplane.SessionSpec{ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"support-session", "report", "--gateway-url", server.URL, "--ticket-code", ticket.Code, "--session-id", standalone.ID,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match the ticket binding") || stdout.Len() != 0 {
		t.Fatal("report did not fail closed for an explicit session outside the ticket binding")
	}
}

func TestFetchHostTrustFallsBackToLegacyTrustEndpoint(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	legacy := gw.TrustBundle()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/trust":
			_ = json.NewEncoder(w).Encode(map[string]any{"trust": legacy})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	trust, err := fetchHostTrust(context.Background(), nil, server.URL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if trust.Legacy == nil {
		t.Fatal("expected legacy trust fallback")
	}
	if trust.SignedBundle != nil {
		t.Fatal("did not expect signed trust bundle")
	}
	if trust.Legacy.SigningKeyID != legacy.SigningKeyID {
		t.Fatalf("expected legacy key %q, got %q", legacy.SigningKeyID, trust.Legacy.SigningKeyID)
	}
}

func TestFetchHostTrustPersistsSignedBundle(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	storePath := filepath.Join(t.TempDir(), "trust", "bundle.json")

	trust, err := fetchHostTrust(context.Background(), nil, server.URL, "", storePath)
	if err != nil {
		t.Fatal(err)
	}
	if trust.SignedBundle == nil {
		t.Fatal("expected signed trust bundle")
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Fatal(err)
	}
}

func TestFetchHostTrustPersistsSignedBundleToProtectedStore(t *testing.T) {
	backend := &cliMemoryKeychainBackend{items: map[string][]byte{}}
	restore := protectedstore.SetKeychainBackendForTest(backend)
	defer restore()

	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	storeRef := "keychain:remote-dev-skillkit/cli-managed-trust"

	trust, err := fetchHostTrust(context.Background(), nil, server.URL, "", storeRef)
	if err != nil {
		t.Fatal(err)
	}
	if trust.SignedBundle == nil {
		t.Fatal("expected signed trust bundle")
	}
	if len(backend.items) != 1 {
		t.Fatalf("expected one protected trust item, got %d", len(backend.items))
	}
	stored, ok, err := hosttrust.ProtectedStore{Ref: storeRef}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || stored.Sequence != gw.SignedTrustBundle().Sequence {
		t.Fatalf("expected stored protected sequence %d, got ok=%v bundle=%#v", gw.SignedTrustBundle().Sequence, ok, stored)
	}
}

func TestFetchHostTrustUsesStoredBundleWhenGatewayTrustBundleUnavailable(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	storePath := filepath.Join(t.TempDir(), "trust", "bundle.json")
	store := hosttrust.FileStore{Path: storePath}
	if err := store.Save(gw.SignedTrustBundle()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	trust, err := fetchHostTrust(context.Background(), nil, server.URL, "", storePath)
	if err != nil {
		t.Fatal(err)
	}
	if trust.SignedBundle == nil {
		t.Fatal("expected stored signed trust bundle")
	}
	if trust.SignedBundle.Sequence != gw.SignedTrustBundle().Sequence {
		t.Fatalf("expected stored sequence %d, got %d", gw.SignedTrustBundle().Sequence, trust.SignedBundle.Sequence)
	}
}

type cliMemoryKeychainBackend struct {
	items map[string][]byte
}

func (b *cliMemoryKeychainBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *cliMemoryKeychainBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

func TestGatewayServeDevReusesSigningKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-signing-key.json")

	key, created, err := signing.LoadOrCreate(path, signing.DefaultKeyID)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected initial key creation")
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, key.ID, key.PublicKey, key.PrivateKey)
	firstFingerprint, err := gw.TrustBundle().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	reused, created, err := signing.LoadOrCreate(path, signing.DefaultKeyID)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected key reuse")
	}
	gw = gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, reused.ID, reused.PublicKey, reused.PrivateKey)
	secondFingerprint, err := gw.TrustBundle().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatalf("expected stable fingerprint, got %s then %s", firstFingerprint, secondFingerprint)
	}
}

func TestTrustInitRotateRevokeAndVerify(t *testing.T) {
	dir := t.TempDir()
	rootKey := filepath.Join(dir, "trust-root.json")
	gatewayOne := filepath.Join(dir, "gateway-one.json")
	gatewayTwo := filepath.Join(dir, "gateway-two.json")
	firstPath := filepath.Join(dir, "trust-1.json")
	secondPath := filepath.Join(dir, "trust-2.json")
	thirdPath := filepath.Join(dir, "trust-3.json")

	var initStdout bytes.Buffer
	app := NewApp(&initStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "init",
		"--out", firstPath,
		"--root-key", rootKey,
		"--root-key-id", "trust-root",
		"--gateway-key", gatewayOne,
		"--gateway-key-id", "gateway-one",
		"--bundle-id", "managed-hosts",
		"--valid-hours", "24",
	}); err != nil {
		t.Fatal(err)
	}
	var initPayload struct {
		OK            bool   `json:"ok"`
		RootPublicKey string `json:"root_public_key"`
		Sequence      int    `json:"sequence"`
	}
	if err := json.Unmarshal(initStdout.Bytes(), &initPayload); err != nil {
		t.Fatalf("invalid trust init output: %v\n%s", err, initStdout.String())
	}
	if !initPayload.OK || initPayload.Sequence != 1 || initPayload.RootPublicKey == "" {
		t.Fatalf("unexpected trust init output: %s", initStdout.String())
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "verify",
		"--bundle", firstPath,
		"--root-public-key", initPayload.RootPublicKey,
	}); err != nil {
		t.Fatal(err)
	}
	assertTrustVerifyOK(t, verifyStdout.Bytes(), 1)

	var rotateStdout bytes.Buffer
	app = NewApp(&rotateStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "rotate",
		"--current", firstPath,
		"--out", secondPath,
		"--root-key", rootKey,
		"--gateway-key", gatewayTwo,
		"--gateway-key-id", "gateway-two",
		"--retire-key", "gateway-one",
		"--valid-hours", "24",
	}); err != nil {
		t.Fatal(err)
	}
	var rotatePayload struct {
		OK       bool `json:"ok"`
		Sequence int  `json:"sequence"`
	}
	if err := json.Unmarshal(rotateStdout.Bytes(), &rotatePayload); err != nil {
		t.Fatalf("invalid trust rotate output: %v\n%s", err, rotateStdout.String())
	}
	if !rotatePayload.OK || rotatePayload.Sequence != 2 {
		t.Fatalf("unexpected trust rotate output: %s", rotateStdout.String())
	}
	firstBundle := readTrustBundleForTest(t, firstPath)
	secondBundle := readTrustBundleForTest(t, secondPath)
	firstHash, err := firstBundle.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if secondBundle.Sequence != 2 || secondBundle.PreviousBundleHash != firstHash {
		t.Fatalf("unexpected rotated bundle: seq=%d previous=%q want %q", secondBundle.Sequence, secondBundle.PreviousBundleHash, firstHash)
	}
	if key, ok := secondBundle.Key("gateway-one"); !ok || key.Status != model.TrustKeyStatusRetired {
		t.Fatalf("expected gateway-one retired, got ok=%v key=%#v", ok, key)
	}
	if key, ok := secondBundle.Key("gateway-two"); !ok || key.Status != model.TrustKeyStatusActive {
		t.Fatalf("expected gateway-two active, got ok=%v key=%#v", ok, key)
	}

	var revokeStdout bytes.Buffer
	app = NewApp(&revokeStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "revoke",
		"--current", secondPath,
		"--out", thirdPath,
		"--root-key", rootKey,
		"--key-id", "gateway-two",
		"--reason", "test compromise",
		"--valid-hours", "24",
	}); err != nil {
		t.Fatal(err)
	}
	thirdBundle := readTrustBundleForTest(t, thirdPath)
	if thirdBundle.Sequence != 3 {
		t.Fatalf("expected sequence 3, got %d", thirdBundle.Sequence)
	}
	if key, ok := thirdBundle.Key("gateway-two"); !ok || key.Status != model.TrustKeyStatusRevoked || key.RevokedReason != "test compromise" || key.RevokedAt == nil {
		t.Fatalf("expected gateway-two revoked with reason, got ok=%v key=%#v", ok, key)
	}
	verifyStdout.Reset()
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "verify",
		"--bundle", thirdPath,
		"--root-public-key", initPayload.RootPublicKey,
	}); err != nil {
		t.Fatal(err)
	}
	assertTrustVerifyOK(t, verifyStdout.Bytes(), 3)
}

func TestTrustRevokeRefusesCurrentSigningKey(t *testing.T) {
	dir := t.TempDir()
	rootKey := filepath.Join(dir, "trust-root.json")
	gatewayKey := filepath.Join(dir, "gateway.json")
	bundlePath := filepath.Join(dir, "trust.json")
	outPath := filepath.Join(dir, "trust-revoked.json")

	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "init",
		"--out", bundlePath,
		"--root-key", rootKey,
		"--root-key-id", "trust-root",
		"--gateway-key", gatewayKey,
		"--gateway-key-id", "gateway",
	}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{
		"trust", "revoke",
		"--current", bundlePath,
		"--out", outPath,
		"--root-key", rootKey,
		"--key-id", "trust-root",
		"--reason", "root compromise",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot revoke current signing key") {
		t.Fatalf("expected current signing key revoke to fail, got %v", err)
	}
}

func TestTicketCreateOutputsJoinURL(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"ticket", "create", "--ttl-seconds", "600", "--reason", "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "https://agent.example.com/join/") {
		t.Fatalf("expected join URL, got %q", stdout.String())
	}
}

func TestPolicyExplainOutputsJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"policy", "explain", "--capability", "shell.user"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Allowed {
		t.Fatal("shell.user should be allowed in temporary mode")
	}
}

func TestPolicyExplainShellOutputsJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"policy", "explain-shell",
		"--policy-json", `{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Allowed {
		t.Fatalf("expected shell policy to be allowed, got %s", stdout.String())
	}
}

func TestDemoLocalOutputsClosedLoop(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"demo", "local"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Session struct {
			Status string `json:"status"`
		} `json:"session"`
		Endpoint struct {
			Role string `json:"role"`
		} `json:"endpoint"`
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Session.Status != "online" {
		t.Fatalf("session should be online, got %q", payload.Session.Status)
	}
	if payload.Endpoint.Role != "target" {
		t.Fatalf("endpoint should be target, got %q", payload.Endpoint.Role)
	}
	if payload.Task.Status != "succeeded" {
		t.Fatalf("task should succeed, got %q", payload.Task.Status)
	}
	if len(payload.Events) != 3 {
		t.Fatalf("expected 3 session events, got %d", len(payload.Events))
	}
}

func TestGatewayServeRequiresDevFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"gateway", "serve"})
	if err == nil {
		t.Fatal("expected gateway serve without --dev to fail")
	}
	if !strings.Contains(err.Error(), "requires --dev") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGatewayServeStateRequiresSigningKey(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"gateway", "serve",
		"--dev",
		"--state", filepath.Join(t.TempDir(), "state.json"),
	})
	if err == nil {
		t.Fatal("expected gateway serve --state without --signing-key to fail")
	}
	if !strings.Contains(err.Error(), "persistent storage requires --signing-key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGatewayTLSConfigRequiresCompleteKeyPair(t *testing.T) {
	_, err := gatewayTLSConfig(gatewayServeOptions{TLSCertPath: filepath.Join(t.TempDir(), "cert.pem")})
	if err == nil || !strings.Contains(err.Error(), "both --tls-cert and --tls-key") {
		t.Fatalf("expected incomplete TLS keypair error, got %v", err)
	}
	_, err = gatewayTLSConfig(gatewayServeOptions{ClientCAPath: filepath.Join(t.TempDir(), "ca.pem")})
	if err == nil || !strings.Contains(err.Error(), "--client-ca requires --tls-cert and --tls-key") {
		t.Fatalf("expected client CA TLS requirement, got %v", err)
	}
}

func TestGatewayHTTPClientRequiresCompleteClientKeyPair(t *testing.T) {
	material := writeGatewayTLSMaterial(t)
	_, err := gatewayHTTPClient(hostServeOptions{
		GatewayCACertPath:     material.CACert,
		GatewayClientCertPath: material.ClientCert,
	})
	if err == nil || !strings.Contains(err.Error(), "both --gateway-client-cert and --gateway-client-key") {
		t.Fatalf("expected incomplete gateway client keypair error, got %v", err)
	}
	_, err = gatewayHTTPClient(hostServeOptions{
		GatewayCACertPath:    material.CACert,
		GatewayClientKeyPath: material.ClientKey,
	})
	if err == nil || !strings.Contains(err.Error(), "both --gateway-client-cert and --gateway-client-key") {
		t.Fatalf("expected incomplete gateway client keypair error, got %v", err)
	}
}

func TestGatewayHTTPClientRejectsInvalidCA(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := gatewayHTTPClient(hostServeOptions{GatewayCACertPath: caPath})
	if err == nil || !strings.Contains(err.Error(), "--gateway-ca does not contain a valid PEM certificate") {
		t.Fatalf("expected invalid gateway CA error, got %v", err)
	}
}

func TestGatewayTLSConfigLoadsServerTLS(t *testing.T) {
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath: material.ServerCert,
		TLSKeyPath:  material.ServerKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if config == nil {
		t.Fatal("expected TLS config")
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 minimum, got %d", config.MinVersion)
	}
	if len(config.Certificates) != 1 {
		t.Fatalf("expected one server certificate, got %d", len(config.Certificates))
	}
	if config.ClientAuth != tls.NoClientCert {
		t.Fatalf("expected no client cert requirement, got %v", config.ClientAuth)
	}
}

func TestGatewayTLSConfigRequiresClientCertificatesWhenClientCASet(t *testing.T) {
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("expected client certificate enforcement, got %v", config.ClientAuth)
	}
	if config.ClientCAs == nil {
		t.Fatal("expected client CA pool")
	}
}

func TestGatewayDevMTLSHealthzRequiresClientCertificate(t *testing.T) {
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(httpapi.NewServer(gateway.NewMemoryGateway()).Handler())
	server.TLS = config
	server.StartTLS()
	defer server.Close()

	caPEM, err := os.ReadFile(material.CACert)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("expected test CA PEM to parse")
	}
	noClientCert := server.Client()
	noClientCert.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	resp, err := noClientCert.Get(server.URL + "/healthz")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected TLS handshake to fail without a client certificate")
	}

	clientCert, err := tls.LoadX509KeyPair(material.ClientCert, material.ClientKey)
	if err != nil {
		t.Fatal(err)
	}
	withClientCert := server.Client()
	withClientCert.Transport = &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:      roots,
		Certificates: []tls.Certificate{clientCert},
	}}
	resp, err = withClientCert.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with client certificate, got %d", resp.StatusCode)
	}
}

func TestAuditExportAndVerify(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")
	chainPath := filepath.Join(dir, "chain.json")
	store := audit.NewJSONLStore(jsonlPath)
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	for _, event := range []model.AuditEvent{
		{Sequence: 1, Actor: "operator", Action: "ticket.create", TargetID: "tkt_1", Message: "created", At: now},
		{Sequence: 2, Actor: "host", Action: "host.register", TargetID: "hst_1", Message: "registered", At: now.Add(time.Second)},
	} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	var exportStdout bytes.Buffer
	exportApp := NewApp(&exportStdout, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{"audit", "export", "--input", jsonlPath, "--out", chainPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exportStdout.String(), `"ok": true`) {
		t.Fatalf("expected export ok, got %s", exportStdout.String())
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{"audit", "verify", "--input", chainPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"event_count": 2`) {
		t.Fatalf("expected verify event count, got %s", verifyStdout.String())
	}
}

func TestAuditVerifyRejectsTamperedChain(t *testing.T) {
	dir := t.TempDir()
	chainPath := filepath.Join(dir, "chain.json")
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	chain, err := audit.ExportChain([]model.AuditEvent{
		{Sequence: 1, Actor: "operator", Action: "ticket.create", TargetID: "tkt_1", Message: "created", At: now},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	chain.Entries[0].Event.Message = "tampered"
	if err := audit.WriteChain(chainPath, chain); err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"audit", "verify", "--input", chainPath}); err == nil {
		t.Fatal("expected tampered chain verification to fail")
	}
}

func TestEvidenceCommandIsRetired(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"evidence", "export",
		"--task-json", "task.json",
	})
	if err == nil || !strings.Contains(err.Error(), `unknown command "evidence"`) {
		t.Fatalf("expected retired evidence command to be rejected, err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestSkillkitExportWritesInstallBundle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "skillkit")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--gateway-url", "https://api.example.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"schema": "rdev.skillkit-bundle.v1"`) {
		t.Fatalf("expected skillkit schema output, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"adaptive_configuration_schema": "rdev.adaptive-configuration-contract.v1"`) ||
		!strings.Contains(stdout.String(), `"adaptive_configuration_required": true`) {
		t.Fatalf("expected adaptive configuration output, got %s", stdout.String())
	}
	for _, path := range []string{
		"manifest.json",
		"INSTALL.md",
		"mcp/tools.json",
		"skills/remote-vibe-coding/SKILL.md",
		"frameworks/codex.md",
		"frameworks/claude-code.md",
		"frameworks/hermes.md",
		"frameworks/openclaw-opencode.md",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected skillkit bundle file %s: %v", path, err)
		}
	}
}

func TestSkillkitVerifyChecksInstallBundle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "skillkit")
	exportApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--gateway-url", "https://api.example.com/v1",
	}); err != nil {
		t.Fatal(err)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"skillkit", "verify",
		"--bundle", out,
	}); err != nil {
		t.Fatalf("expected verify to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) || !strings.Contains(verifyStdout.String(), `"schema": "rdev.skillkit-bundle-verification.v1"`) {
		t.Fatalf("expected skillkit verification output, got %s", verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"adaptive_configuration_verified": true`) {
		t.Fatalf("expected adaptive configuration verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(out, "skills", "host-triage", "SKILL.md"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"skillkit", "verify",
		"--bundle", out,
	})
	if err == nil {
		t.Fatalf("expected tampered bundle verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) || !strings.Contains(tamperedStdout.String(), "listed_files_sha256_match") {
		t.Fatalf("expected structured tamper failure, got %s", tamperedStdout.String())
	}
}

func TestSkillkitPlanInstallAndVerifyInstallPlan(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "skillkit")
	exportApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", bundle,
		"--gateway-url", "https://api.example.com/v1",
	}); err != nil {
		t.Fatal(err)
	}

	planDir := filepath.Join(t.TempDir(), "install-plan")
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"skillkit", "plan-install",
		"--bundle", bundle,
		"--out", planDir,
		"--frameworks", "codex,generic",
		"--rdev-command", "rdev-test",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(planStdout.String(), `"schema": "rdev.skillkit-install-plan.v1"`) || !strings.Contains(planStdout.String(), `"external_mutation": false`) {
		t.Fatalf("expected structured install plan output, got %s", planStdout.String())
	}
	if !strings.Contains(planStdout.String(), `"adaptive_configuration_schema": "rdev.adaptive-configuration-contract.v1"`) {
		t.Fatalf("expected adaptive configuration plan output, got %s", planStdout.String())
	}
	codexShellScript := readCLIFile(t, filepath.Join(planDir, "install-codex.sh"))
	if !strings.Contains(codexShellScript, "skillkit install --bundle") ||
		!strings.Contains(codexShellScript, "--execute") ||
		!strings.Contains(codexShellScript, ".remote-dev-skillkit/install.json") {
		t.Fatalf("expected generated shell install script to use standard installer and manifest:\n%s", codexShellScript)
	}
	codexPowerShellScript := readCLIFile(t, filepath.Join(planDir, "install-codex.ps1"))
	if !strings.Contains(codexPowerShellScript, "'skillkit', 'install'") ||
		!strings.Contains(codexPowerShellScript, "'--execute'") ||
		!strings.Contains(codexPowerShellScript, ".remote-dev-skillkit/install.json") {
		t.Fatalf("expected generated PowerShell install script to use standard installer and manifest:\n%s", codexPowerShellScript)
	}
	for _, path := range []string{
		"install-plan.json",
		"INSTALL_COMMANDS.md",
		"install-codex.sh",
		"install-codex.ps1",
		"install-generic-mcp-agent.sh",
		"install-generic-mcp-agent.ps1",
	} {
		if _, err := os.Stat(filepath.Join(planDir, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected install plan file %s: %v", path, err)
		}
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"skillkit", "verify-install-plan",
		"--plan", filepath.Join(planDir, "install-plan.json"),
	}); err != nil {
		t.Fatalf("expected verify-install-plan to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) || !strings.Contains(verifyStdout.String(), `"schema": "rdev.skillkit-install-plan-verification.v1"`) {
		t.Fatalf("expected install plan verification output, got %s", verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"adaptive_configuration_verified": true`) {
		t.Fatalf("expected adaptive configuration install-plan verification, got %s", verifyStdout.String())
	}
}

func TestSkillkitPlanInstallAutoDetectsStableRdevCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable bits for the fallback binary fixture")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	goBin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goBinRdev := filepath.Join(goBin, "rdev")
	if err := os.WriteFile(goBinRdev, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "skillkit")
	exportApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", bundle,
	}); err != nil {
		t.Fatal(err)
	}

	planDir := filepath.Join(t.TempDir(), "install-plan")
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"skillkit", "plan-install",
		"--bundle", bundle,
		"--out", planDir,
		"--frameworks", "codex",
	}); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(planStdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid install plan output: %v\n%s", err, planStdout.String())
	}
	steps, _ := payload["recommended_steps"].([]any)
	if !strings.Contains(strings.Join(anyStrings(steps), "\n"), goBinRdev+" mcp serve") {
		t.Fatalf("expected auto-detected absolute rdev command in plan output, got %s", planStdout.String())
	}
	codexShellScript := readCLIFile(t, filepath.Join(planDir, "install-codex.sh"))
	if !strings.Contains(codexShellScript, goBinRdev) {
		t.Fatalf("expected generated install script to use auto-detected rdev command %s:\n%s", goBinRdev, codexShellScript)
	}
}

func TestSkillkitInstallDryRunAndExecute(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "skillkit")
	exportApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", bundle,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "codex-skills")

	var dryRunStdout bytes.Buffer
	dryRunApp := NewApp(&dryRunStdout, &bytes.Buffer{})
	if err := dryRunApp.Run(context.Background(), []string{
		"skillkit", "install",
		"--bundle", bundle,
		"--framework", "codex",
		"--target", target,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dryRunStdout.String(), `"schema": "rdev.skillkit-install-report.v1"`) ||
		!strings.Contains(dryRunStdout.String(), `"execute": false`) ||
		!strings.Contains(dryRunStdout.String(), `"local_mutation": false`) ||
		!strings.Contains(dryRunStdout.String(), `"install_manifest":`) ||
		!strings.Contains(dryRunStdout.String(), `"type": "write_install_manifest"`) {
		t.Fatalf("expected dry-run install report, got %s", dryRunStdout.String())
	}
	var dryRunPayload map[string]any
	if err := json.Unmarshal(dryRunStdout.Bytes(), &dryRunPayload); err != nil {
		t.Fatalf("invalid dry-run install report: %v\n%s", err, dryRunStdout.String())
	}
	if mcpCommand, _ := dryRunPayload["mcp_command"].(string); !strings.Contains(mcpCommand, " mcp serve") {
		t.Fatalf("expected dry-run install report to include mcp_command, got %#v", dryRunPayload)
	}
	if _, err := os.Stat(filepath.Join(target, "remote-vibe-coding")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not copy skills, stat err=%v", err)
	}

	var executeStdout bytes.Buffer
	executeApp := NewApp(&executeStdout, &bytes.Buffer{})
	if err := executeApp.Run(context.Background(), []string{
		"skillkit", "install",
		"--bundle", bundle,
		"--framework", "codex",
		"--target", target,
		"--execute",
	}); err != nil {
		t.Fatalf("expected execute install to pass: %v\n%s", err, executeStdout.String())
	}
	if !strings.Contains(executeStdout.String(), `"executed": true`) ||
		!strings.Contains(executeStdout.String(), `"external_mutation": false`) ||
		!strings.Contains(executeStdout.String(), `"install_manifest":`) {
		t.Fatalf("expected executed install report, got %s", executeStdout.String())
	}
	var executePayload map[string]any
	if err := json.Unmarshal(executeStdout.Bytes(), &executePayload); err != nil {
		t.Fatalf("invalid executed install report: %v\n%s", err, executeStdout.String())
	}
	if mcpCommand, _ := executePayload["mcp_command"].(string); !strings.Contains(mcpCommand, " mcp serve") {
		t.Fatalf("expected executed install report to include mcp_command, got %#v", executePayload)
	}
	if _, err := os.Stat(filepath.Join(target, "remote-vibe-coding", "SKILL.md")); err != nil {
		t.Fatalf("expected installed skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".remote-dev-skillkit", "install.json")); err != nil {
		t.Fatalf("expected install manifest: %v", err)
	}
}

func TestAdapterVerifyResultAcceptsShellArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "shell-result.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": 0,
  "timed_out": false,
  "canceled": false,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "verify-result",
		"--artifact", artifactPath,
		"--adapter", "shell",
		"--schema", "rdev.shell-result.v1",
	}); err != nil {
		t.Fatal(err)
	}
	var report struct {
		SchemaVersion string `json:"schema_version"`
		OK            bool   `json:"ok"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid conformance output: %v\n%s", err, stdout.String())
	}
	if report.SchemaVersion != "rdev.adapter-conformance-report.v1" || !report.OK {
		t.Fatalf("unexpected conformance output: %s", stdout.String())
	}
}

func TestAdapterVerifyResultRejectsMissingCommandEvidence(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "shell-result.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "verify-result",
		"--artifact", artifactPath,
		"--adapter", "shell",
		"--schema", "rdev.shell-result.v1",
	})
	if err == nil || !strings.Contains(err.Error(), "conformance failed") {
		t.Fatalf("expected conformance failure, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"ok": false`) {
		t.Fatalf("expected structured failure report, got %s", stdout.String())
	}
}

func TestAdapterScaffoldCreatesVerifiableLifecycleManifest(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "claude-code-lifecycle.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "scaffold",
		"--adapter", "claude-code",
		"--out", artifactPath,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Schema       string `json:"schema"`
		OK           bool   `json:"ok"`
		Adapter      string `json:"adapter"`
		Manifest     string `json:"manifest"`
		ResultSchema string `json:"result_schema"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid scaffold output: %v\n%s", err, stdout.String())
	}
	if payload.Schema != "rdev.adapter-scaffold.v1" || !payload.OK || payload.Adapter != "claude-code" || payload.Manifest != artifactPath || payload.ResultSchema != "rdev.claude-code-result.v1" {
		t.Fatalf("unexpected scaffold output: %s", stdout.String())
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"adapter", "verify-lifecycle",
		"--artifact", artifactPath,
		"--adapter", "claude-code",
	}); err != nil {
		t.Fatalf("generated lifecycle manifest should verify: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected generated lifecycle manifest verification to pass, got %s", verifyStdout.String())
	}
}

func TestAdapterScaffoldRefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "adapter.json")
	if err := os.WriteFile(artifactPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "scaffold",
		"--adapter", "claude-code",
		"--out", artifactPath,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
	content, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "{}\n" {
		t.Fatalf("scaffold should not overwrite without --force, got %s", string(content))
	}
}

func TestAdapterVerifyLifecycleAcceptsManifest(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "claude-code-lifecycle.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "claude-code",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version"]},
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_authorizations": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": true},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_tasks": false,
    "adapter_authorizes_dangerous_actions": false,
    "adapter_installs_persistence": false,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {"supported": true, "evidence_field": "canceled", "timeout_exclusive": true, "cleanup_on_cancel": true}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "verify-lifecycle",
		"--artifact", artifactPath,
		"--adapter", "claude-code",
	}); err != nil {
		t.Fatal(err)
	}
	var report struct {
		SchemaVersion  string `json:"schema_version"`
		ArtifactSchema string `json:"artifact_schema"`
		OK             bool   `json:"ok"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid lifecycle conformance output: %v\n%s", err, stdout.String())
	}
	if report.SchemaVersion != "rdev.adapter-conformance-report.v1" || report.ArtifactSchema != "rdev.adapter-lifecycle.v1" || !report.OK {
		t.Fatalf("unexpected lifecycle conformance output: %s", stdout.String())
	}
}

func TestAdapterVerifyLifecycleRejectsMissingCancellation(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "claude-code-lifecycle.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "claude-code",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version"]},
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_authorizations": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": false},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_tasks": false,
    "adapter_authorizes_dangerous_actions": false,
    "adapter_installs_persistence": false,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {"supported": false, "evidence_field": "", "timeout_exclusive": true, "cleanup_on_cancel": true}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "verify-lifecycle",
		"--artifact", artifactPath,
		"--adapter", "claude-code",
	})
	if err == nil || !strings.Contains(err.Error(), "lifecycle conformance failed") {
		t.Fatalf("expected lifecycle conformance failure, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"ok": false`) || !strings.Contains(stdout.String(), "run_supports_cancellation") {
		t.Fatalf("expected structured lifecycle failure report, got %s", stdout.String())
	}
}

func TestAdapterVerifyCancellationAcceptsCanceledShellArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "shell-result.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": -1,
  "timed_out": false,
  "canceled": true,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "verify-cancellation",
		"--artifact", artifactPath,
		"--adapter", "shell",
		"--schema", "rdev.shell-result.v1",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), "cancellation_canceled_true:.") {
		t.Fatalf("expected cancellation conformance success, got %s", stdout.String())
	}
}

func TestAdapterVerifyCancellationRejectsTimeoutArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "shell-result.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": -1,
  "timed_out": true,
  "canceled": false,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "verify-cancellation",
		"--artifact", artifactPath,
		"--adapter", "shell",
		"--schema", "rdev.shell-result.v1",
	})
	if err == nil || !strings.Contains(err.Error(), "cancellation conformance failed") {
		t.Fatalf("expected cancellation conformance failure, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"ok": false`) || !strings.Contains(stdout.String(), "cancellation_not_timed_out:.") {
		t.Fatalf("expected structured cancellation failure, got %s", stdout.String())
	}
}

func TestAdapterVerifyRuntimeAcceptsRuntimeFixture(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "adapter-runtime-fixture.json")
	if err := os.WriteFile(artifactPath, []byte(runtimeFixtureJSON("fake", true)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "verify-runtime",
		"--artifact", artifactPath,
		"--adapter", "fake",
		"--require-result-artifact",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), "result_artifact_object") {
		t.Fatalf("expected runtime conformance success, got %s", stdout.String())
	}
}

func TestAdapterVerifyRuntimeRejectsMissingCleanup(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "adapter-runtime-fixture.json")
	if err := os.WriteFile(artifactPath, []byte(runtimeFixtureJSON("fake", false)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "verify-runtime",
		"--artifact", artifactPath,
		"--adapter", "fake",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime conformance failed") {
		t.Fatalf("expected runtime conformance failure, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"ok": false`) || !strings.Contains(stdout.String(), "cleanup_attempted") {
		t.Fatalf("expected structured runtime failure, got %s", stdout.String())
	}
}

func TestReleaseSignAndVerify(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "rdev-host.exe")
	if err := os.WriteFile(artifactPath, []byte("host-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "release-root.json")
	manifestPath := filepath.Join(dir, "rdev-host.release.json")
	var signStdout bytes.Buffer
	var signStderr bytes.Buffer
	signApp := NewApp(&signStdout, &signStderr)

	err := signApp.Run(context.Background(), []string{
		"release", "sign",
		"--artifact", artifactPath,
		"--key", keyPath,
		"--key-id", "release-root",
		"--out", manifestPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	var signed struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(signStdout.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	if signed.RootPublicKey == "" {
		t.Fatal("root public key should be returned")
	}
	var verifyStdout bytes.Buffer
	var verifyStderr bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &verifyStderr)
	err = verifyApp.Run(context.Background(), []string{
		"release", "verify",
		"--artifact", artifactPath,
		"--manifest", manifestPath,
		"--root-public-key", signed.RootPublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %q", verifyStdout.String())
	}
}

func runtimeFixtureJSON(adapter string, cleanup bool) string {
	cleanupAttempted := "false"
	cleanupOK := "false"
	cleanupPhase := ""
	if cleanup {
		cleanupAttempted = "true"
		cleanupOK = "true"
		cleanupPhase = `,
    {"phase": "cleanup", "ok": true, "evidence": ["cleanup"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0}`
	}
	return fmt.Sprintf(`{
  "schema_version": "rdev.adapter-runtime-fixture.v1",
  "adapter": %q,
  "task_id": "task_123",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "canceled": false,
  "timed_out": false,
  "cleanup_attempted": %s,
  "cleanup_ok": %s,
  "result_artifact_schema": "rdev.fake-result.v1",
  "result_artifact": {"schema_version": "rdev.fake-result.v1", "adapter": "fake", "workspace_root": "/tmp/repo"},
  "phases": [
    {"phase": "detect", "ok": true, "evidence": ["version"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "plan", "ok": true, "evidence": ["commands"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "prepare", "ok": true, "evidence": ["workspace"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "run", "ok": true, "evidence": ["process"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "collect", "ok": true, "evidence": ["result"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0}%s
  ]
}`, adapter, cleanupAttempted, cleanupOK, cleanupPhase)
}

func TestReleaseVerifyRejectsTamperedArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "rdev-host.exe")
	if err := os.WriteFile(artifactPath, []byte("host-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "release-root.json")
	manifestPath := filepath.Join(dir, "rdev-host.release.json")
	var signStdout bytes.Buffer
	signApp := NewApp(&signStdout, &bytes.Buffer{})
	if err := signApp.Run(context.Background(), []string{
		"release", "sign",
		"--artifact", artifactPath,
		"--key", keyPath,
		"--out", manifestPath,
	}); err != nil {
		t.Fatal(err)
	}
	var signed struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(signStdout.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	verifyApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := verifyApp.Run(context.Background(), []string{
		"release", "verify",
		"--artifact", artifactPath,
		"--manifest", manifestPath,
		"--root-public-key", signed.RootPublicKey,
	})
	if err == nil {
		t.Fatal("expected tampered artifact to fail")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReleaseCreateAndVerifyBundle(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "release-root.json")
	root := signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-host.exe", "host-binary")
	signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-verify.exe", "verify-binary")

	var createStdout bytes.Buffer
	createApp := NewApp(&createStdout, &bytes.Buffer{})
	if err := createApp.Run(context.Background(), []string{
		"release", "create-bundle",
		"--dir", dir,
		"--artifacts", "rdev-host.exe,rdev-verify.exe",
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
		"--key", keyPath,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(createStdout.String(), `"schema": "rdev.release-bundle.v1"`) {
		t.Fatalf("expected bundle output, got %s", createStdout.String())
	}
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("expected bundle file: %v", err)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"release", "verify-bundle",
		"--bundle", bundlePath,
		"--root-public-key", root,
	}); err != nil {
		t.Fatalf("expected bundle verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) || !strings.Contains(verifyStdout.String(), "signed_manifest_verifies_artifact") {
		t.Fatalf("expected structured bundle verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(dir, "rdev-host.exe"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"release", "verify-bundle",
		"--bundle", bundlePath,
		"--root-public-key", root,
	})
	if err == nil {
		t.Fatalf("expected tampered bundle verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) ||
		!strings.Contains(tamperedStdout.String(), "artifact_sha256_matches_index") ||
		!strings.Contains(tamperedStdout.String(), "signed_manifest_verifies_artifact") {
		t.Fatalf("expected structured tampered bundle failure, got %s", tamperedStdout.String())
	}
}

func TestReleaseCreateBundleRejectsOutOutsideReleaseDir(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "release-root.json")
	signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-host.exe", "host-binary")

	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"release", "create-bundle",
		"--dir", dir,
		"--artifacts", "rdev-host.exe",
		"--key", keyPath,
		"--out", filepath.Join(t.TempDir(), "release-bundle.json"),
	})
	if err == nil {
		t.Fatal("expected out path outside release dir to fail")
	}
	if !strings.Contains(err.Error(), "bundle output must be inside release directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReleasePrepareCandidateStagesBundleAndSkillkit(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rdev := writeCLIArtifactForTest(t, artifactsDir, "rdev", "cli-binary")
	host := writeCLIArtifactForTest(t, artifactsDir, "rdev-host.exe", "host-binary")
	bootstrap := writeCLIArtifactForTest(t, artifactsDir, "rdev-bootstrap.exe", "bootstrap-binary")
	verifier := writeCLIArtifactForTest(t, artifactsDir, "rdev-verify.exe", "verify-binary")
	out := filepath.Join(dir, "candidate")
	keyPath := filepath.Join(dir, "release-root.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"release", "prepare-candidate",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--version", "v0.1.0",
		"--gateway-url", "https://api.example.com/v1",
		"--artifacts", strings.Join([]string{rdev, host, bootstrap, verifier}, ","),
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
		"--key", keyPath,
		"--key-id", "release-root",
	}); err != nil {
		t.Fatalf("expected release candidate preparation to pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ok": true`) ||
		!strings.Contains(stdout.String(), `"schema": "rdev.release-candidate.v1"`) ||
		!strings.Contains(stdout.String(), `"sbom":`) ||
		!strings.Contains(stdout.String(), `"provenance":`) {
		t.Fatalf("expected release candidate output, got %s", stdout.String())
	}
	for _, path := range []string{
		"release-candidate.json",
		"release-bundle.json",
		"sbom.spdx.json",
		"provenance.json",
		"checksums.txt",
		"skillkit/manifest.json",
		"rdev-host.exe.rdev-release.json",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected release candidate file %s: %v", path, err)
		}
	}
}

func TestReleasePrepareCandidateStagesLayeredWindowsCoreForTargetPlatform(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hostBytes := "windows-host-binary"
	host := writeCLIArtifactForTest(t, artifactsDir, "rdev-host.exe", hostBytes)
	bootstrap := writeCLIArtifactForTest(t, artifactsDir, "rdev-bootstrap.exe", "bootstrap-binary")
	verifier := writeCLIArtifactForTest(t, artifactsDir, "rdev-verify.exe", "verify-binary")
	out := filepath.Join(dir, "candidate")
	var stdout bytes.Buffer
	err := NewApp(&stdout, &bytes.Buffer{}).Run(context.Background(), []string{
		"release", "prepare-candidate",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--version", "v0.2.0",
		"--artifacts", strings.Join([]string{host, bootstrap, verifier}, ","),
		"--target-platform", "windows/amd64",
		"--key", filepath.Join(dir, "release-root.json"),
	})
	if err != nil {
		t.Fatalf("expected target platform to reach candidate preparation: %v\n%s", err, stdout.String())
	}
	stagedPath := filepath.Join(out, "assets", "rdev-host-windows-amd64.exe")
	staged, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatalf("expected staged Windows core runtime: %v", err)
	}
	if string(staged) != hostBytes {
		t.Fatalf("staged Windows core runtime changed bytes: got %q", staged)
	}
	if !fileExistsForCLITest(filepath.Join(out, "layered-assets.json")) ||
		!fileExistsForCLITest(filepath.Join(out, "connection-entry-release.zip")) {
		t.Fatalf("expected layered candidate plus archive fallback in %s", out)
	}
}

func TestReleaseVerifyCandidateChecksPreparedCandidate(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rdev := writeCLIArtifactForTest(t, artifactsDir, "rdev", "cli-binary")
	host := writeCLIArtifactForTest(t, artifactsDir, "rdev-host.exe", "host-binary")
	bootstrap := writeCLIArtifactForTest(t, artifactsDir, "rdev-bootstrap.exe", "bootstrap-binary")
	verifier := writeCLIArtifactForTest(t, artifactsDir, "rdev-verify.exe", "verify-binary")
	out := filepath.Join(dir, "candidate")
	keyPath := filepath.Join(dir, "release-root.json")
	prepareApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := prepareApp.Run(context.Background(), []string{
		"release", "prepare-candidate",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--version", "v0.1.0",
		"--gateway-url", "https://api.example.com/v1",
		"--artifacts", strings.Join([]string{rdev, host, bootstrap, verifier}, ","),
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
		"--key", keyPath,
		"--key-id", "release-root",
	}); err != nil {
		t.Fatal(err)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"release", "verify-candidate",
		"--candidate", out,
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
	}); err != nil {
		t.Fatalf("expected release candidate verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"schema": "rdev.release-candidate-verification.v1"`) ||
		!strings.Contains(verifyStdout.String(), "bundle_verification") {
		t.Fatalf("expected structured candidate verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(out, "rdev-host.exe"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"release", "verify-candidate",
		"--candidate", filepath.Join(out, "release-candidate.json"),
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
	})
	if err == nil {
		t.Fatalf("expected tampered release candidate verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) ||
		!strings.Contains(tamperedStdout.String(), "file_sha256_matches") ||
		!strings.Contains(tamperedStdout.String(), "signed_manifest_verifies_artifact") {
		t.Fatalf("expected structured tamper output, got %s", tamperedStdout.String())
	}
}

func TestUpdateCheckReadsLatestGitHubRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/EitanWong/remote-dev-skillkit/releases/latest" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got == "" {
			t.Fatal("expected GitHub API version header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
  "tag_name": "v0.2.0",
  "name": "v0.2.0",
  "html_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/tag/v0.2.0",
  "draft": false,
  "prerelease": false,
  "published_at": "2026-07-02T00:00:00Z",
  "assets": [
    {
      "name": "remote-dev-skillkit-v0.2.0-darwin-arm64.tar.gz",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/remote-dev-skillkit-v0.2.0-darwin-arm64.tar.gz",
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "size": 123,
      "content_type": "application/gzip"
    },
    {
      "name": "release-bundle.json",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/release-bundle.json",
      "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "size": 456,
      "content_type": "application/json"
    }
  ]
}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"update", "check",
		"--repo", "EitanWong/remote-dev-skillkit",
		"--api-base-url", server.URL,
		"--current-version", "v0.1.0",
	}); err != nil {
		t.Fatalf("expected update check to pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"schema_version": "rdev.update-check.v1"`) ||
		!strings.Contains(stdout.String(), `"latest_version": "v0.2.0"`) ||
		!strings.Contains(stdout.String(), `"update_available": true`) {
		t.Fatalf("unexpected update check output: %s", stdout.String())
	}
}

func TestUpdatePlanSelectsPlatformArchive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
  "tag_name": "v0.2.0",
  "name": "v0.2.0",
  "html_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/tag/v0.2.0",
  "draft": false,
  "prerelease": false,
  "published_at": "2026-07-02T00:00:00Z",
  "assets": [
    {
      "name": "remote-dev-skillkit-v0.2.0-linux-amd64.tar.gz",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/remote-dev-skillkit-v0.2.0-linux-amd64.tar.gz",
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "size": 123
    },
    {
      "name": "remote-dev-skillkit-v0.2.0-windows-amd64.zip",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/remote-dev-skillkit-v0.2.0-windows-amd64.zip",
      "digest": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
      "size": 234
    },
    {
      "name": "release-bundle.json",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/release-bundle.json",
      "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "size": 456
    }
  ]
}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"update", "plan",
		"--repo", "EitanWong/remote-dev-skillkit",
		"--api-base-url", server.URL,
		"--current-version", "v0.1.0",
		"--platform", "linux/amd64",
	}); err != nil {
		t.Fatalf("expected update plan to pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"schema_version": "rdev.update-plan.v1"`) ||
		!strings.Contains(stdout.String(), `"platform": "linux/amd64"`) ||
		!strings.Contains(stdout.String(), "remote-dev-skillkit-v0.2.0-linux-amd64.tar.gz") ||
		!strings.Contains(stdout.String(), "rdev release verify-bundle") ||
		!strings.Contains(stdout.String(), `"plan_is_dry_run"`) {
		t.Fatalf("unexpected update plan output: %s", stdout.String())
	}
}

func TestDepsInstallPlanOnlyOutputsReport(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"deps", "install",
		"--tool", "chisel",
		"--scope", "user",
		"--platform", "linux/amd64",
		"--url", "https://example.com/chisel.tar.gz",
		"--expected-sha256", strings.Repeat("d", 64),
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"schema": "rdev.dependency-install-report.v1"`) ||
		!strings.Contains(stdout.String(), `"tool": "chisel"`) ||
		!strings.Contains(stdout.String(), `"execute": false`) ||
		!strings.Contains(stdout.String(), `"no_privileged_install"`) {
		t.Fatalf("unexpected deps install output: %s", stdout.String())
	}
}

func TestDepsInstallPlanOnlySupportsMeshAndVPNHelpers(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool string
		want string
	}{
		{name: "tailscale", tool: "tailscale", want: `"tool": "tailscale"`},
		{name: "wireguard alias", tool: "wireguard", want: `"tool": "wg"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			app := NewApp(&stdout, &bytes.Buffer{})
			if err := app.Run(context.Background(), []string{
				"deps", "install",
				"--tool", tc.tool,
				"--scope", "workspace",
				"--platform", "linux/amd64",
				"--url", "https://example.com/" + tc.tool + ".zip",
				"--expected-sha256", strings.Repeat("e", 64),
			}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), tc.want) ||
				!strings.Contains(stdout.String(), `"ok": true`) ||
				!strings.Contains(stdout.String(), `"execute": false`) {
				t.Fatalf("unexpected deps install output: %s", stdout.String())
			}
		})
	}
}

func writeCLIArtifactForTest(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWorkspaceLockStatusAndUnlock(t *testing.T) {
	repo := t.TempDir()
	store := filepath.Join(t.TempDir(), "locks")
	var lockStdout bytes.Buffer
	app := NewApp(&lockStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"workspace", "lock",
		"--repo", repo,
		"--store", store,
		"--host-id", "hst_cli",
		"--task-id", "task_cli",
		"--adapter", "codex",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lockStdout.String(), `"owner_adapter": "codex"`) {
		t.Fatalf("expected lock output, got %s", lockStdout.String())
	}

	var statusStdout bytes.Buffer
	statusApp := NewApp(&statusStdout, &bytes.Buffer{})
	if err := statusApp.Run(context.Background(), []string{
		"workspace", "status",
		"--repo", repo,
		"--store", store,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusStdout.String(), `"exists": true`) {
		t.Fatalf("expected existing status, got %s", statusStdout.String())
	}

	var unlockStdout bytes.Buffer
	unlockApp := NewApp(&unlockStdout, &bytes.Buffer{})
	if err := unlockApp.Run(context.Background(), []string{
		"workspace", "unlock",
		"--repo", repo,
		"--store", store,
		"--task-id", "task_cli",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unlockStdout.String(), `"removed": true`) {
		t.Fatalf("expected removal output, got %s", unlockStdout.String())
	}
}

func TestWorkspacePrepareWorktreeCreatesGitWorktree(t *testing.T) {
	requireGitForCLITest(t)
	repo := initGitRepoForCLITest(t)
	store := filepath.Join(t.TempDir(), "locks")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"workspace", "prepare-worktree",
		"--repo", repo,
		"--store", store,
		"--host-id", "hst_cli",
		"--task-id", "task_cli",
		"--adapter", "codex",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Worktree struct {
			SchemaVersion string `json:"schema_version"`
			WorktreePath  string `json:"worktree_path"`
			Branch        string `json:"branch"`
			Lock          struct {
				TaskID       string `json:"task_id"`
				OwnerAdapter string `json:"owner_adapter"`
			} `json:"lock"`
		} `json:"worktree"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if payload.Worktree.SchemaVersion != "rdev.git-worktree-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Worktree.SchemaVersion)
	}
	if payload.Worktree.Branch != "rdev/task_task_cli" {
		t.Fatalf("unexpected branch %q", payload.Worktree.Branch)
	}
	if payload.Worktree.Lock.TaskID != "task_cli" || payload.Worktree.Lock.OwnerAdapter != "codex" {
		t.Fatalf("unexpected lock %#v", payload.Worktree.Lock)
	}
	if _, err := os.Stat(filepath.Join(payload.Worktree.WorktreePath, "README.md")); err != nil {
		t.Fatalf("expected checked out worktree: %v", err)
	}
}

func TestAcceptanceManagedMacGeneratesEvidence(t *testing.T) {
	requireGitForCLITest(t)
	fakeCodex := buildCLITestBinary(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# rdev acceptance fixture\n\nChanged by managed Mac acceptance.\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake codex acceptance run")
}
`)
	out := filepath.Join(t.TempDir(), "managed-mac-acceptance")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "managed-mac",
		"--out", out,
		"--codex-command", fakeCodex,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK              bool `json:"ok"`
		Report          string
		Evidence        string
		SideEffectProbe string `json:"side_effect_probe"`
		Worktree        string
		Checks          []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected acceptance ok, got %s", stdout.String())
	}
	for _, path := range []string{
		payload.Report,
		filepath.Join(payload.Evidence, "manifest.json"),
		filepath.Join(payload.SideEffectProbe, "manifest.json"),
		filepath.Join(payload.Worktree, "README.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	readme := readFileForTest(t, filepath.Join(payload.Worktree, "README.md"))
	if !strings.Contains(readme, "Changed by managed Mac acceptance") {
		t.Fatalf("expected fake codex change in worktree, got %s", readme)
	}
	if len(payload.Checks) == 0 {
		t.Fatal("expected checks")
	}
	for _, check := range payload.Checks {
		if !check.Passed {
			t.Fatalf("expected check %s to pass: %s", check.Name, stdout.String())
		}
	}
	report := readFileForTest(t, payload.Report)
	if !strings.Contains(report, `"schema_version": "rdev.acceptance.managed-mac.v1"`) {
		t.Fatalf("expected managed Mac acceptance report, got %s", report)
	}
	if !strings.Contains(report, `"schema_version": "rdev.session-evidence.v1"`) {
		t.Fatalf("expected embedded session evidence manifests, got %s", report)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify",
		"--report", payload.Report,
	}); err != nil {
		t.Fatalf("expected acceptance verification to pass: %v\n%s", err, verifyStdout.String())
	}
	var verifyPayload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(verifyStdout.Bytes(), &verifyPayload); err != nil {
		t.Fatalf("invalid verification json: %v\n%s", err, verifyStdout.String())
	}
	if !verifyPayload.OK {
		t.Fatalf("expected verification ok, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(payload.Evidence, "coding-result.json"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"acceptance", "verify",
		"--report", payload.Report,
	})
	if err == nil {
		t.Fatalf("expected tampered acceptance verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) {
		t.Fatalf("expected structured failed verification, got %s", tamperedStdout.String())
	}
}

func TestAcceptanceManagedMacServicePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "managed-mac-service")
	repo := t.TempDir()
	binaryPath := filepath.Join(t.TempDir(), "rdev")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "managed-mac-service",
		"--out", out,
		"--binary", binaryPath,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--repo", repo,
		"--label", "com.example.rdev-acceptance",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK       bool   `json:"ok"`
		Schema   string `json:"schema"`
		Plan     string `json:"plan"`
		Plist    string `json:"plist"`
		Commands []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected service acceptance plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.managed-mac-service-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	for _, path := range []string{payload.Plan, payload.Plist} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	commands := stdout.String()
	if !strings.Contains(commands, "rdev host service-control") || !strings.Contains(commands, "rdev acceptance verify") {
		t.Fatalf("expected service-control and verification commands, got %s", commands)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-managed-mac-service",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected managed mac service plan verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.acceptance-verification.managed-mac-service-plan.v1"`) {
		t.Fatalf("expected managed mac service verification schema, got %s", verifyStdout.String())
	}
}

func TestAcceptancePackageManagedMacService(t *testing.T) {
	requireGitForCLITest(t)
	fakeCodex := buildCLITestBinary(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# rdev acceptance fixture\n\nChanged by managed Mac service package.\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake codex service package run")
}
`)
	root := t.TempDir()
	planOut := filepath.Join(root, "managed-mac-service")
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"acceptance", "managed-mac-service",
		"--out", planOut,
		"--binary", filepath.Join(root, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--repo", t.TempDir(),
		"--label", "com.example.rdev-acceptance",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(planStdout.Bytes(), &planPayload); err != nil {
		t.Fatalf("invalid plan json: %v\n%s", err, planStdout.String())
	}
	managedOut := filepath.Join(root, "managed-mac-run")
	var managedStdout bytes.Buffer
	managedApp := NewApp(&managedStdout, &bytes.Buffer{})
	if err := managedApp.Run(context.Background(), []string{
		"acceptance", "managed-mac",
		"--out", managedOut,
		"--codex-command", fakeCodex,
	}); err != nil {
		t.Fatal(err)
	}
	var managedPayload struct {
		Report string `json:"report"`
	}
	if err := json.Unmarshal(managedStdout.Bytes(), &managedPayload); err != nil {
		t.Fatalf("invalid managed mac json: %v\n%s", err, managedStdout.String())
	}
	fakeGitHubToken := "ghp_" + "abcdefghijklmnopqrstuvwx"
	evidence := writeManagedMacServicePackageEvidenceForCLITest(t, root, `{"ok": true, "token": "`+fakeGitHubToken+`"}`)

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-managed-mac-service",
		"--plan", planPayload.Plan,
		"--out", filepath.Join(root, "managed-mac-service-evidence"),
		"--review-transcript", evidence.reviewTranscriptPath,
		"--start-transcript", evidence.startTranscriptPath,
		"--inspect-transcript", evidence.inspectTranscriptPath,
		"--logs", evidence.logsPath,
		"--release-gate", evidence.releaseGatePath,
		"--audit", evidence.auditPath,
		"--reconnect", evidence.reconnectPath,
		"--managed-report", managedPayload.Report,
		"--stop-transcript", evidence.stopTranscriptPath,
		"--uninstall-transcript", evidence.uninstallTranscriptPath,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK      bool   `json:"ok"`
		Schema  string `json:"schema"`
		Package string `json:"package"`
		Files   []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
		RedactionRuleCounts map[string]int `json:"redaction_rule_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected package ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance-package.managed-mac-service.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if _, err := os.Stat(payload.Package); err != nil {
		t.Fatalf("expected package manifest: %v", err)
	}
	if payload.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github_token redaction count, got %#v", payload.RedactionRuleCounts)
	}
	output := stdout.String()
	for _, expected := range []string{"launch-agent-plist", "managed-mac-report", "managed-mac-evidence", "checksums.txt"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected packaged output containing %q, got %s", expected, output)
		}
	}
}

func TestAcceptanceWindowsTemporaryPlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	archivePath := writeLayeredHandoffArchiveForCLITest(t)
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--handoff-archive", archivePath,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK             bool   `json:"ok"`
		Schema         string `json:"schema"`
		Plan           string `json:"plan"`
		HandoffArchive string `json:"handoff_archive"`
		Commands       []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"commands"`
		NoPersistenceChecks []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"no_persistence_checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected windows temporary plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.windows-temporary-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	for _, path := range []string{filepath.Join(out, payload.Plan), filepath.Join(out, payload.HandoffArchive)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	output := stdout.String()
	if !strings.Contains(output, "run_foreground_temporary_host") || !strings.Contains(output, "Get-ScheduledTask") {
		t.Fatalf("expected foreground and no-persistence commands, got %s", output)
	}
	if strings.Contains(strings.ToLower(stdout.String()), "rdev-host.exe") || !strings.Contains(stdout.String(), "rdev-bootstrap layered-run") {
		t.Fatalf("acceptance output must remain bootstrap-only: %s", stdout.String())
	}
}

func TestAcceptanceWindowsTemporaryBundlePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	archivePath := writeLayeredHandoffArchiveForCLITest(t)
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--handoff-archive", archivePath,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK             bool   `json:"ok"`
		Schema         string `json:"schema"`
		Plan           string `json:"plan"`
		HandoffArchive string `json:"handoff_archive"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected windows temporary bundle plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.windows-temporary-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if payload.HandoffArchive != "Windows-ConnectionEntry.zip" {
		t.Fatalf("unexpected handoff archive %q", payload.HandoffArchive)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", filepath.Join(out, payload.Plan),
	}); err != nil {
		t.Fatalf("expected bundle verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}
}

func TestAcceptanceWindowsManagedServicePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-managed-service")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-managed-service",
		"--out", out,
		"--binary", `C:\Program Files\rdev\rdev.exe`,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--label", "RemoteDevSkillkitHost",
		"--workspace-lock-store", `C:\ProgramData\rdev\workspace-locks`,
		"--release-bundle", `C:\Program Files\rdev\release-bundle.json`,
		"--release-root-public-key", "release-root:abc123",
		"--release-require-artifacts", "rdev.exe,rdev-host.exe,rdev-verify.exe",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK          bool   `json:"ok"`
		Schema      string `json:"schema"`
		Plan        string `json:"plan"`
		ServiceName string `json:"service_name"`
		StartType   string `json:"start_type"`
		Commands    []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected windows managed service plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.windows-managed-service-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if payload.ServiceName != "RemoteDevSkillkitHost" || payload.StartType != "demand" {
		t.Fatalf("unexpected service identity: %#v", payload)
	}
	if _, err := os.Stat(payload.Plan); err != nil {
		t.Fatalf("expected generated plan %s: %v", payload.Plan, err)
	}
	output := stdout.String()
	for _, expected := range []string{
		"sc.exe create RemoteDevSkillkitHost",
		"sc.exe delete RemoteDevSkillkitHost",
		"verify-windows-managed-service",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected output containing %q, got %s", expected, output)
		}
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-managed-service",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}
}

func TestAcceptanceVerifyWindowsManagedServicePlanRejectsTampering(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-managed-service")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-managed-service",
		"--out", out,
		"--binary", `C:\Program Files\rdev\rdev.exe`,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--release-bundle", `C:\Program Files\rdev\release-bundle.json`,
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	var planDoc map[string]any
	if err := json.Unmarshal([]byte(readFileForTest(t, payload.Plan)), &planDoc); err != nil {
		t.Fatal(err)
	}
	uninstall, ok := planDoc["uninstall"].(map[string]any)
	if !ok {
		t.Fatalf("expected uninstall object in plan")
	}
	uninstall["commands"] = []any{[]any{"Set-ExecutionPolicy", "Bypass"}}
	tampered, err := json.MarshalIndent(planDoc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload.Plan, append(tampered, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-managed-service",
		"--plan", payload.Plan,
	})
	if err == nil {
		t.Fatalf("expected tampered verification to fail: %s", verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": false`) || !strings.Contains(verifyStdout.String(), "sc_delete_present") {
		t.Fatalf("expected structured tampered failure, got %s", verifyStdout.String())
	}
}

func TestAcceptanceLinuxManagedServicePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "linux-managed-service",
		"--out", out,
		"--binary", "/opt/rdev/rdev",
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--label", "rdev-host.service",
		"--workspace-lock-store", "/var/lib/rdev/workspace-locks",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
		"--release-require-artifacts", "rdev,rdev-host,rdev-verify",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK       bool   `json:"ok"`
		Schema   string `json:"schema"`
		Plan     string `json:"plan"`
		Unit     string `json:"unit"`
		UnitName string `json:"unit_name"`
		Commands []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected linux managed service plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.linux-managed-service-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if payload.UnitName != "rdev-host.service" {
		t.Fatalf("unexpected unit name %#v", payload)
	}
	for _, path := range []string{payload.Plan, payload.Unit} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	output := stdout.String()
	for _, expected := range []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable --now rdev-host.service",
		"systemctl --user disable --now rdev-host.service",
		"verify-linux-managed-service",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected output containing %q, got %s", expected, output)
		}
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-linux-managed-service",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}
}

func TestAcceptanceVerifyLinuxManagedServicePlanRejectsTampering(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "linux-managed-service",
		"--out", out,
		"--binary", "/opt/rdev/rdev",
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	var planDoc map[string]any
	if err := json.Unmarshal([]byte(readFileForTest(t, payload.Plan)), &planDoc); err != nil {
		t.Fatal(err)
	}
	start, ok := planDoc["start"].(map[string]any)
	if !ok {
		t.Fatalf("expected start object in plan")
	}
	start["commands"] = []any{[]any{"sudo", "systemctl", "enable", "--now", "remote-dev-skillkit-host.service"}}
	tampered, err := json.MarshalIndent(planDoc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload.Plan, append(tampered, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-linux-managed-service",
		"--plan", payload.Plan,
	})
	if err == nil {
		t.Fatalf("expected tampered verification to fail: %s", verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": false`) || !strings.Contains(verifyStdout.String(), "systemctl_daemon_reload_present") {
		t.Fatalf("expected structured tampered failure, got %s", verifyStdout.String())
	}
}

func TestAcceptanceVerifyWindowsTemporaryPlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	archivePath := writeLayeredHandoffArchiveForCLITest(t)
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--handoff-archive", archivePath,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Plan           string `json:"plan"`
		HandoffArchive string `json:"handoff_archive"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", filepath.Join(out, payload.Plan),
	}); err != nil {
		t.Fatalf("expected verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(out, payload.HandoffArchive), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", filepath.Join(out, payload.Plan),
	})
	if err == nil {
		t.Fatalf("expected tampered verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) || !strings.Contains(tamperedStdout.String(), "handoff_archive") {
		t.Fatalf("expected structured tampered failure, got %s", tamperedStdout.String())
	}
}

func TestAcceptancePackageWindowsTemporary(t *testing.T) {
	root := t.TempDir()
	planOut := filepath.Join(root, "windows-temporary")
	archivePath := writeLayeredHandoffArchiveForCLITest(t)
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", planOut,
		"--handoff-archive", archivePath,
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(planStdout.Bytes(), &planPayload); err != nil {
		t.Fatalf("invalid plan json: %v\n%s", err, planStdout.String())
	}
	planPayload.Plan = filepath.Join(planOut, planPayload.Plan)
	fakeGitHubToken := "ghp_" + "abcdefghijklmnopqrstuvwx"
	transcriptPath, releaseVerificationPath, auditPath, noPersistenceDir, denialProbesDir := writeWindowsPackageEvidenceForCLITest(t, root, `{"ok": true, "token": "`+fakeGitHubToken+`"}`)
	coldLayeredRunPath := writeLayeredRunReportForCLITest(t, root, "cold-layered-run.json", false)
	warmLayeredRunPath := writeLayeredRunReportForCLITest(t, root, "warm-layered-run.json", true)
	layeredEntryEvidencePath := writeLayeredEntryEvidenceForCLITest(t, root, planPayload.Plan)
	packageArgs := func(out, coldLayeredRun, warmLayeredRun string) []string {
		args := []string{
			"acceptance", "package-windows-temporary",
			"--plan", planPayload.Plan,
			"--out", out,
			"--transcript", transcriptPath,
			"--release-verification", releaseVerificationPath,
			"--audit", auditPath,
			"--no-persistence-dir", noPersistenceDir,
			"--denial-probes-dir", denialProbesDir,
			"--layered-entry-evidence", layeredEntryEvidencePath,
		}
		if coldLayeredRun != "" {
			args = append(args, "--cold-layered-run", coldLayeredRun)
		}
		if warmLayeredRun != "" {
			args = append(args, "--warm-layered-run", warmLayeredRun)
		}
		return args
	}

	t.Run("packages cold and warm layered run evidence", func(t *testing.T) {
		out := filepath.Join(root, "windows-evidence")
		var stdout bytes.Buffer
		app := NewApp(&stdout, &bytes.Buffer{})
		if err := app.Run(context.Background(), packageArgs(out, coldLayeredRunPath, warmLayeredRunPath)); err != nil {
			t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
		}
		var payload struct {
			OK      bool   `json:"ok"`
			Schema  string `json:"schema"`
			Package string `json:"package"`
			Files   []struct {
				Path string `json:"path"`
				Kind string `json:"kind"`
			} `json:"files"`
			RedactionRuleCounts map[string]int `json:"redaction_rule_counts"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
		}
		if !payload.OK {
			t.Fatalf("expected package ok, got %s", stdout.String())
		}
		if payload.Schema != "rdev.acceptance-package.windows-temporary.v1" {
			t.Fatalf("unexpected schema %q", payload.Schema)
		}
		if filepath.IsAbs(payload.Package) {
			t.Fatalf("public package output leaked an absolute path: %q", payload.Package)
		}
		if _, err := os.Stat(filepath.Join(out, payload.Package)); err != nil {
			t.Fatalf("expected package manifest: %v", err)
		}
		if payload.RedactionRuleCounts["github_token"] != 1 {
			t.Fatalf("expected github_token redaction count, got %#v", payload.RedactionRuleCounts)
		}
		filesByKind := make(map[string]string, len(payload.Files))
		for _, file := range payload.Files {
			filesByKind[file.Kind] = file.Path
		}
		for kind, wantPath := range map[string]string{
			"cold-layered-run": "evidence/cold-layered-run.json",
			"warm-layered-run": "evidence/warm-layered-run.json",
		} {
			if got := filesByKind[kind]; got != wantPath {
				t.Fatalf("packaged %s path = %q, want %q; files = %#v", kind, got, wantPath, payload.Files)
			}
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(wantPath))); err != nil {
				t.Fatalf("expected packaged %s evidence: %v", kind, err)
			}
		}
	})

	for _, tc := range []struct {
		name            string
		missingEvidence string
		coldLayeredRun  string
		warmLayeredRun  string
	}{
		{name: "requires cold layered run evidence", missingEvidence: "cold", warmLayeredRun: warmLayeredRunPath},
		{name: "requires warm layered run evidence", missingEvidence: "warm", coldLayeredRun: coldLayeredRunPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := NewApp(&stdout, &stderr)
			err := app.Run(context.Background(), packageArgs(
				filepath.Join(root, "windows-evidence-missing-"+tc.missingEvidence),
				tc.coldLayeredRun,
				tc.warmLayeredRun,
			))
			if err == nil {
				t.Fatalf("expected package command to reject missing %s layered run evidence", tc.missingEvidence)
			}
			failure := strings.ToLower(err.Error() + "\n" + stdout.String() + "\n" + stderr.String())
			if !strings.Contains(failure, tc.missingEvidence) {
				t.Fatalf("expected missing %s evidence failure, got %s", tc.missingEvidence, failure)
			}
		})
	}
}

func TestAcceptancePackageLinuxManagedService(t *testing.T) {
	root := t.TempDir()
	planOut := filepath.Join(root, "linux-managed-service")
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"acceptance", "linux-managed-service",
		"--out", planOut,
		"--binary", "/opt/rdev/rdev",
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(planStdout.Bytes(), &planPayload); err != nil {
		t.Fatalf("invalid plan json: %v\n%s", err, planStdout.String())
	}
	fakeGitHubToken := "ghp_" + "abcdefghijklmnopqrstuvwx"
	evidence := writeLinuxPackageEvidenceForCLITest(t, root, `{"ok": true, "token": "`+fakeGitHubToken+`"}`)

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-linux-managed-service",
		"--plan", planPayload.Plan,
		"--out", filepath.Join(root, "linux-evidence"),
		"--start-transcript", evidence.startTranscriptPath,
		"--status-transcript", evidence.statusTranscriptPath,
		"--logs", evidence.logsPath,
		"--release-gate", evidence.releaseGatePath,
		"--audit", evidence.auditPath,
		"--reconnect", evidence.reconnectPath,
		"--session-evidence-dir", evidence.sessionEvidenceDir,
		"--stop-transcript", evidence.stopTranscriptPath,
		"--uninstall-transcript", evidence.uninstallTranscriptPath,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK      bool   `json:"ok"`
		Schema  string `json:"schema"`
		Package string `json:"package"`
		Files   []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
		RedactionRuleCounts map[string]int `json:"redaction_rule_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected package ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance-package.linux-managed-service.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if _, err := os.Stat(payload.Package); err != nil {
		t.Fatalf("expected package manifest: %v", err)
	}
	if payload.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github_token redaction count, got %#v", payload.RedactionRuleCounts)
	}
	output := stdout.String()
	for _, expected := range []string{"start-transcript", "release-gate", "session-evidence", "checksums.txt"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected packaged output containing %q, got %s", expected, output)
		}
	}
}

func TestAcceptancePackageRelayAdapter(t *testing.T) {
	root := t.TempDir()
	relayOut := filepath.Join(root, "relay")
	var relayStdout bytes.Buffer
	app := NewApp(&relayStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"relay-adapter", "package",
		"--out", relayOut,
		"--adapter", "chisel",
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForCLITest(t, root)
	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-relay-adapter",
		"--relay-package", relayOut,
		"--out", filepath.Join(root, "relay-evidence"),
		"--evidence-dir", evidence.dir,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK            bool     `json:"ok"`
		Schema        string   `json:"schema"`
		Package       string   `json:"package"`
		SelectedPath  string   `json:"selected_path"`
		AcceptedPaths []string `json:"accepted_paths"`
		Files         []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
		RedactionRuleCounts map[string]int `json:"redaction_rule_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance-package.relay-adapter.v1" {
		t.Fatalf("unexpected package output: %s", stdout.String())
	}
	if payload.SelectedPath != "existing-wireguard-vpn" ||
		!slices.Contains(payload.AcceptedPaths, "existing-ssh-tunnel") {
		t.Fatalf("unexpected connectivity path output: %#v", payload)
	}
	if payload.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github token redaction, got %#v", payload.RedactionRuleCounts)
	}
	var packagedPaths []string
	for _, file := range payload.Files {
		packagedPaths = append(packagedPaths, file.Path)
	}
	if !slices.Contains(packagedPaths, "evidence/audit.jsonl") {
		t.Fatalf("expected evidence-dir package files, got %#v", packagedPaths)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "verify-relay-adapter-package",
		"--package", payload.Package,
	}); err != nil {
		t.Fatalf("expected verify command to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.acceptance-verification.relay-adapter-package.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("unexpected verify output: %s", verifyStdout.String())
	}
}

func TestAcceptancePackageHostedProviderRuntime(t *testing.T) {
	root := t.TempDir()
	providerOut := filepath.Join(root, "hosted-provider")
	var providerStdout bytes.Buffer
	app := NewApp(&providerStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", providerOut,
		"--storage-provider", "file",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeHostedProviderRuntimeEvidenceForCLITest(t, root)
	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-hosted-provider-runtime",
		"--hosted-provider-package", providerOut,
		"--out", filepath.Join(root, "hosted-runtime-evidence"),
		"--evidence-dir", evidence.dir,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK           bool              `json:"ok"`
		Schema       string            `json:"schema"`
		Package      string            `json:"package"`
		RuntimeClaim string            `json:"runtime_claim"`
		Redactions   map[string]int    `json:"redaction_rule_counts"`
		Files        []json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance-package.hosted-provider-runtime.v1" ||
		payload.RuntimeClaim != "single-node-hosted-smoke" {
		t.Fatalf("unexpected hosted runtime package output: %s", stdout.String())
	}
	if payload.Redactions["github_token"] != 1 {
		t.Fatalf("expected github token redaction, got %#v", payload.Redactions)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "verify-hosted-provider-runtime-package",
		"--package", payload.Package,
	}); err != nil {
		t.Fatalf("expected verify command to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.acceptance-verification.hosted-provider-runtime-package.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("unexpected hosted runtime verify output: %s", verifyStdout.String())
	}
}

func TestAcceptancePackagePostReleaseDownload(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadEvidenceForCLITest(t, root)
	scaffoldDir := filepath.Join(root, "post-release-download-scaffold")
	var scaffoldStdout bytes.Buffer
	app := NewApp(&scaffoldStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "scaffold-post-release-download",
		"--post-release-install-dir", filepath.Dir(fixture.plan),
		"--out", scaffoldDir,
		"--create-placeholders",
	}); err != nil {
		t.Fatalf("expected scaffold command to pass: %v\n%s", err, scaffoldStdout.String())
	}
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-transcript.txt"), filepath.Join(scaffoldDir, "platform-download-evidence", "linux-amd64-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-candidate-verify.json"), filepath.Join(scaffoldDir, "platform-download-evidence", "linux-amd64-candidate-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-bundle-verify.json"), filepath.Join(scaffoldDir, "platform-download-evidence", "linux-amd64-bundle-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-transcript.txt"), filepath.Join(scaffoldDir, "platform-download-evidence", "windows-amd64-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-candidate-verify.json"), filepath.Join(scaffoldDir, "platform-download-evidence", "windows-amd64-candidate-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-bundle-verify.json"), filepath.Join(scaffoldDir, "platform-download-evidence", "windows-amd64-bundle-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.skillkitDir, "skillkit-transcript.txt"), filepath.Join(scaffoldDir, "skillkit-download-evidence", "skillkit-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.skillkitDir, "skillkit-verify.json"), filepath.Join(scaffoldDir, "skillkit-download-evidence", "skillkit-verify.json"))

	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-post-release-download",
		"--scaffold", scaffoldDir,
		"--out", filepath.Join(root, "post-release-download-evidence"),
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK              bool              `json:"ok"`
		Schema          string            `json:"schema"`
		Package         string            `json:"package"`
		Repo            string            `json:"repo"`
		Tag             string            `json:"tag"`
		PlatformTargets []string          `json:"platform_targets"`
		Skillkit        bool              `json:"skillkit_included"`
		Redactions      map[string]int    `json:"redaction_rule_counts"`
		Files           []json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance-package.post-release-download.v1" ||
		payload.Repo != "EitanWong/remote-dev-skillkit" || payload.Tag != "v0.1.18-dev" ||
		len(payload.PlatformTargets) != 2 || !payload.Skillkit {
		t.Fatalf("unexpected post-release download package output: %s", stdout.String())
	}
	if payload.Redactions["github_token"] != 2 {
		t.Fatalf("expected github token redaction, got %#v", payload.Redactions)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "verify-post-release-download-package",
		"--package", payload.Package,
	}); err != nil {
		t.Fatalf("expected verify command to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.acceptance-verification.post-release-download-package.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("unexpected post-release download verify output: %s", verifyStdout.String())
	}
}

func TestAcceptanceReleaseEvidenceIndex(t *testing.T) {
	root := t.TempDir()
	hostedPackage := writeHostedRuntimePackageForCLIIndexTest(t, root)
	relayPackage := writeRelayPackageForCLIIndexTest(t, root)
	postReleasePackage := writePostReleasePackageForCLIIndexTest(t, root)

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "release-evidence-index",
		"--out", filepath.Join(root, "release-evidence-index"),
		"--hosted-provider-runtime-package", hostedPackage,
		"--relay-adapter-package", relayPackage,
		"--post-release-download-package", postReleasePackage,
	}); err != nil {
		t.Fatalf("expected release evidence index to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK                    bool   `json:"ok"`
		Schema                string `json:"schema"`
		Index                 string `json:"index"`
		HostedProviderRuntime struct {
			OK           bool   `json:"ok"`
			RuntimeClaim string `json:"runtime_claim"`
		} `json:"hosted_provider_runtime"`
		RelayAdapters []struct {
			OK           bool   `json:"ok"`
			SelectedPath string `json:"selected_path"`
		} `json:"relay_adapters"`
		PostReleaseDownload struct {
			OK              bool     `json:"ok"`
			PlatformTargets []string `json:"platform_targets"`
		} `json:"post_release_download"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid release evidence index json: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance-release-evidence-index.v1" ||
		!payload.HostedProviderRuntime.OK || payload.HostedProviderRuntime.RuntimeClaim == "" ||
		len(payload.RelayAdapters) != 1 || !payload.RelayAdapters[0].OK ||
		payload.RelayAdapters[0].SelectedPath != "existing-wireguard-vpn" ||
		!payload.PostReleaseDownload.OK || len(payload.PostReleaseDownload.PlatformTargets) != 2 {
		t.Fatalf("unexpected release evidence index: %s", stdout.String())
	}
	if _, err := os.Stat(payload.Index); err != nil {
		t.Fatalf("expected index file: %v", err)
	}
}

func TestAcceptanceScaffoldPostReleaseDownload(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadEvidenceForCLITest(t, root)
	out := filepath.Join(root, "post-release-scaffold")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "scaffold-post-release-download",
		"--post-release-install-dir", filepath.Dir(fixture.plan),
		"--out", out,
		"--create-placeholders",
	}); err != nil {
		t.Fatalf("expected scaffold command to pass: %v\n%s", err, stdout.String())
	}
	for _, expected := range []string{
		`"schema": "rdev.post-release-download-evidence-scaffold.v1"`,
		`"ready_for_packaging": false`,
		`"skillkit_included": true`,
		`"platform_evidence_dir"`,
		`"package-post-release-download"`,
		`"--scaffold"`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected %q in scaffold output: %s", expected, stdout.String())
		}
	}
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"acceptance", "post-release-evidence-status",
		"--scaffold", out,
	})
	if err == nil {
		t.Fatal("placeholder post-release evidence status should fail")
	}
	if !strings.Contains(stdout.String(), `"schema": "rdev.post-release-download-evidence-status.v1"`) ||
		!strings.Contains(stdout.String(), `"placeholder_count": 8`) ||
		!strings.Contains(stdout.String(), `"ready_for_packaging": false`) {
		t.Fatalf("unexpected placeholder status: %s", stdout.String())
	}

	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-transcript.txt"), filepath.Join(out, "platform-download-evidence", "linux-amd64-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-candidate-verify.json"), filepath.Join(out, "platform-download-evidence", "linux-amd64-candidate-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-bundle-verify.json"), filepath.Join(out, "platform-download-evidence", "linux-amd64-bundle-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-transcript.txt"), filepath.Join(out, "platform-download-evidence", "windows-amd64-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-candidate-verify.json"), filepath.Join(out, "platform-download-evidence", "windows-amd64-candidate-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-bundle-verify.json"), filepath.Join(out, "platform-download-evidence", "windows-amd64-bundle-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.skillkitDir, "skillkit-transcript.txt"), filepath.Join(out, "skillkit-download-evidence", "skillkit-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.skillkitDir, "skillkit-verify.json"), filepath.Join(out, "skillkit-download-evidence", "skillkit-verify.json"))

	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "post-release-evidence-status",
		"--scaffold", out,
	}); err != nil {
		t.Fatalf("real post-release evidence status should pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ready_for_packaging": true`) ||
		!strings.Contains(stdout.String(), `"required_ready": 8`) {
		t.Fatalf("unexpected ready status: %s", stdout.String())
	}
}

func writeHostedRuntimePackageForCLIIndexTest(t *testing.T, root string) string {
	t.Helper()
	providerOut := filepath.Join(root, "hosted-provider")
	var providerStdout bytes.Buffer
	app := NewApp(&providerStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", providerOut,
		"--storage-provider", "file",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeHostedProviderRuntimeEvidenceForCLITest(t, root)
	out := filepath.Join(root, "hosted-runtime-evidence")
	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-hosted-provider-runtime",
		"--hosted-provider-package", providerOut,
		"--out", out,
		"--evidence-dir", evidence.dir,
	}); err != nil {
		t.Fatalf("expected hosted package command to pass: %v\n%s", err, stdout.String())
	}
	return out
}

func writeRelayPackageForCLIIndexTest(t *testing.T, root string) string {
	t.Helper()
	relayOut := filepath.Join(root, "relay")
	var relayStdout bytes.Buffer
	app := NewApp(&relayStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"relay-adapter", "package",
		"--out", relayOut,
		"--adapter", "wireguard",
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForCLITest(t, root)
	out := filepath.Join(root, "relay-evidence")
	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-relay-adapter",
		"--relay-package", relayOut,
		"--out", out,
		"--evidence-dir", evidence.dir,
	}); err != nil {
		t.Fatalf("expected relay package command to pass: %v\n%s", err, stdout.String())
	}
	return out
}

func writePostReleasePackageForCLIIndexTest(t *testing.T, root string) string {
	t.Helper()
	fixture := writePostReleaseDownloadEvidenceForCLITest(t, root)
	scaffoldDir := filepath.Join(root, "post-release-download-scaffold-for-index")
	var scaffoldStdout bytes.Buffer
	app := NewApp(&scaffoldStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "scaffold-post-release-download",
		"--post-release-install-dir", filepath.Dir(fixture.plan),
		"--out", scaffoldDir,
		"--create-placeholders",
	}); err != nil {
		t.Fatalf("expected scaffold command to pass: %v\n%s", err, scaffoldStdout.String())
	}
	for _, name := range []string{
		"linux-amd64-transcript.txt",
		"linux-amd64-candidate-verify.json",
		"linux-amd64-bundle-verify.json",
		"windows-amd64-transcript.txt",
		"windows-amd64-candidate-verify.json",
		"windows-amd64-bundle-verify.json",
	} {
		copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, name), filepath.Join(scaffoldDir, "platform-download-evidence", name))
	}
	copyFileForCLITest(t, filepath.Join(fixture.skillkitDir, "skillkit-transcript.txt"), filepath.Join(scaffoldDir, "skillkit-download-evidence", "skillkit-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.skillkitDir, "skillkit-verify.json"), filepath.Join(scaffoldDir, "skillkit-download-evidence", "skillkit-verify.json"))
	out := filepath.Join(root, "post-release-download-evidence")
	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-post-release-download",
		"--scaffold", scaffoldDir,
		"--out", out,
	}); err != nil {
		t.Fatalf("expected post-release package command to pass: %v\n%s", err, stdout.String())
	}
	return out
}

func timeNowForTest() time.Time {
	return time.Now().UTC().Add(-time.Minute)
}

type relayAdapterEvidenceForCLITest struct {
	dir              string
	runnerResult     string
	helperTranscript string
	gatewayStatus    string
	hostStatus       string
	connectionStatus string
	audit            string
	evidenceReport   string
}

func writeRelayAdapterEvidenceForCLITest(t *testing.T, root string) relayAdapterEvidenceForCLITest {
	t.Helper()
	evidenceRoot := filepath.Join(root, "relay-package-fixture")
	runnerResult := filepath.Join(evidenceRoot, "runner-result.json")
	helperTranscript := filepath.Join(evidenceRoot, "helper-transcript.txt")
	gatewayStatus := filepath.Join(evidenceRoot, "gateway-status.json")
	hostStatus := filepath.Join(evidenceRoot, "host-status.json")
	connectionStatus := filepath.Join(evidenceRoot, "connection-status.json")
	audit := filepath.Join(evidenceRoot, "audit.jsonl")
	evidenceReport := filepath.Join(evidenceRoot, "evidence-report.json")
	writeFileForCLITest(t, runnerResult, `{"schema_version":"rdev.connection-entry.runner-result.v1","selected_path":"existing-wireguard-vpn","helper_started":true}`+"\n")
	writeFileForCLITest(t, helperTranscript, "started reviewed relay helper\ntoken ghp_abcdefghijklmnopqrstuvwx\n")
	writeFileForCLITest(t, gatewayStatus, `{"ok":true,"status":"healthy"}`+"\n")
	writeFileForCLITest(t, hostStatus, `{"ok":true,"host_status":"active"}`+"\n")
	writeFileForCLITest(t, connectionStatus, `{"ok":true,"connected":true}`+"\n")
	writeFileForCLITest(t, audit, `{"event":"helper_start"}`+"\n"+`{"event":"host_registered"}`+"\n"+`{"event":"cleanup"}`+"\n")
	writeFileForCLITest(t, evidenceReport, `{"schema_version":"rdev.connection-entry.runner-evidence.v1","connected":true}`+"\n")
	return relayAdapterEvidenceForCLITest{
		dir:              evidenceRoot,
		runnerResult:     runnerResult,
		helperTranscript: helperTranscript,
		gatewayStatus:    gatewayStatus,
		hostStatus:       hostStatus,
		connectionStatus: connectionStatus,
		audit:            audit,
		evidenceReport:   evidenceReport,
	}
}

type hostedProviderRuntimeEvidenceForCLITest struct {
	dir                 string
	gatewayStartup      string
	storageVerification string
	authVerification    string
	backupEvidence      string
	restoreEvidence     string
	retentionEvidence   string
	roleMappingEvidence string
	failureModeEvidence string
	audit               string
}

func writeHostedProviderRuntimeEvidenceForCLITest(t *testing.T, root string) hostedProviderRuntimeEvidenceForCLITest {
	t.Helper()
	evidenceRoot := filepath.Join(root, "hosted-runtime-fixture")
	gatewayStartup := filepath.Join(evidenceRoot, "gateway-startup.txt")
	storageVerification := filepath.Join(evidenceRoot, "storage-verification.json")
	authVerification := filepath.Join(evidenceRoot, "auth-verification.json")
	backupEvidence := filepath.Join(evidenceRoot, "backup-evidence.txt")
	restoreEvidence := filepath.Join(evidenceRoot, "restore-evidence.txt")
	retentionEvidence := filepath.Join(evidenceRoot, "retention-evidence.txt")
	roleMappingEvidence := filepath.Join(evidenceRoot, "role-mapping-evidence.json")
	failureModeEvidence := filepath.Join(evidenceRoot, "failure-mode-evidence.json")
	audit := filepath.Join(evidenceRoot, "audit.jsonl")
	writeFileForCLITest(t, gatewayStartup, "gateway started with hosted provider package\ntoken ghp_abcdefghijklmnopqrstuvwx\n")
	writeFileForCLITest(t, storageVerification, `{"ok":true,"provider":"file"}`+"\n")
	writeFileForCLITest(t, authVerification, `{"ok":true,"provider":"hosted-ed25519-jwt"}`+"\n")
	writeFileForCLITest(t, backupEvidence, "snapshot copied to reviewed backup location\n")
	writeFileForCLITest(t, restoreEvidence, "restored snapshot and verified audit chain\n")
	writeFileForCLITest(t, retentionEvidence, "retention policy reviewed for release smoke\n")
	writeFileForCLITest(t, roleMappingEvidence, `{"probes":[{"role":"operator","authorized":true},{"role":"viewer","authorized":false}]}`+"\n")
	writeFileForCLITest(t, failureModeEvidence, `{"ok":true,"failure_mode_tested":true,"rejected":true,"mode":"invalid auth rejected"}`+"\n")
	writeFileForCLITest(t, audit, `{"event":"gateway_start"}`+"\n"+`{"event":"storage_verify"}`+"\n"+`{"event":"auth_verify"}`+"\n"+`{"event":"role_probe"}`+"\n"+`{"event":"failure_probe"}`+"\n"+`{"event":"cleanup"}`+"\n")
	return hostedProviderRuntimeEvidenceForCLITest{
		dir:                 evidenceRoot,
		gatewayStartup:      gatewayStartup,
		storageVerification: storageVerification,
		authVerification:    authVerification,
		backupEvidence:      backupEvidence,
		restoreEvidence:     restoreEvidence,
		retentionEvidence:   retentionEvidence,
		roleMappingEvidence: roleMappingEvidence,
		failureModeEvidence: failureModeEvidence,
		audit:               audit,
	}
}

type postReleaseDownloadEvidenceForCLITest struct {
	plan             string
	planVerification string
	evidenceDir      string
	skillkitDir      string
}

func writePostReleaseDownloadEvidenceForCLITest(t *testing.T, root string) postReleaseDownloadEvidenceForCLITest {
	t.Helper()
	dir := filepath.Join(root, "post-release-download")
	evidenceDir := filepath.Join(dir, "platform-evidence")
	skillkitDir := filepath.Join(dir, "skillkit-evidence")
	for _, path := range []string{evidenceDir, skillkitDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	targets := []string{"linux/amd64", "windows/amd64"}
	platforms := make([]map[string]string, 0, len(targets))
	write := func(path, content string) string {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	writeJSON := func(path string, value any) string {
		t.Helper()
		content, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return write(path, string(content)+"\n")
	}
	for _, target := range targets {
		platforms = append(platforms, map[string]string{"target": target})
		slug := strings.ReplaceAll(target, "/", "-")
		write(filepath.Join(evidenceDir, slug+"-transcript.txt"), "downloaded "+target+"\nghp_abcdefghijklmnopqrstuvwx\n")
		writeJSON(filepath.Join(evidenceDir, slug+"-candidate-verify.json"), map[string]any{"ok": true})
		writeJSON(filepath.Join(evidenceDir, slug+"-bundle-verify.json"), map[string]any{"ok": true})
	}
	write(filepath.Join(skillkitDir, "skillkit-transcript.txt"), "downloaded skillkit\n")
	writeJSON(filepath.Join(skillkitDir, "skillkit-verify.json"), map[string]any{"ok": true})
	plan := writeJSON(filepath.Join(dir, "post-release-install-plan.json"), map[string]any{
		"schema_version": "rdev.post-release-install-plan.v1",
		"repo":           "EitanWong/remote-dev-skillkit",
		"tag":            "v0.1.18-dev",
		"platforms":      platforms,
		"skillkit":       map[string]any{"archive": map[string]any{"name": "remote-dev-skillkit.tar.gz"}},
	})
	planVerification := writeJSON(filepath.Join(dir, "post-release-install-verification.json"), map[string]any{
		"schema_version": "rdev.post-release-install-verification.v1",
		"ok":             true,
	})
	return postReleaseDownloadEvidenceForCLITest{
		plan:             plan,
		planVerification: planVerification,
		evidenceDir:      evidenceDir,
		skillkitDir:      skillkitDir,
	}
}

type linuxPackageEvidenceForCLITest struct {
	startTranscriptPath     string
	statusTranscriptPath    string
	logsPath                string
	releaseGatePath         string
	auditPath               string
	reconnectPath           string
	sessionEvidenceDir      string
	stopTranscriptPath      string
	uninstallTranscriptPath string
}

type managedMacServicePackageEvidenceForCLITest struct {
	reviewTranscriptPath    string
	startTranscriptPath     string
	inspectTranscriptPath   string
	logsPath                string
	releaseGatePath         string
	auditPath               string
	reconnectPath           string
	stopTranscriptPath      string
	uninstallTranscriptPath string
}

func writeManagedMacServicePackageEvidenceForCLITest(t *testing.T, root, releaseGate string) managedMacServicePackageEvidenceForCLITest {
	t.Helper()
	evidenceRoot := filepath.Join(root, "managed-mac-service-package-fixture")
	reviewTranscriptPath := filepath.Join(evidenceRoot, "review.txt")
	startTranscriptPath := filepath.Join(evidenceRoot, "start.txt")
	inspectTranscriptPath := filepath.Join(evidenceRoot, "inspect.txt")
	logsPath := filepath.Join(evidenceRoot, "logs.txt")
	releaseGatePath := filepath.Join(evidenceRoot, "release-gate.json")
	auditPath := filepath.Join(evidenceRoot, "audit.jsonl")
	reconnectPath := filepath.Join(evidenceRoot, "reconnect.txt")
	stopTranscriptPath := filepath.Join(evidenceRoot, "stop.txt")
	uninstallTranscriptPath := filepath.Join(evidenceRoot, "uninstall.txt")
	writeFileForCLITest(t, reviewTranscriptPath, "plutil -lint com.example.rdev-acceptance.plist\nOK\n")
	writeFileForCLITest(t, startTranscriptPath, "rdev host service-control --platform macos --action start --execute\n")
	writeFileForCLITest(t, inspectTranscriptPath, "rdev host service-control --platform macos --action inspect --execute\nstate = running\n")
	writeFileForCLITest(t, logsPath, "managed host log release gate passed\n")
	writeFileForCLITest(t, releaseGatePath, releaseGate+"\n")
	writeFileForCLITest(t, auditPath, `{"event":"host.registered"}`+"\n"+`{"event":"task.completed"}`+"\n")
	writeFileForCLITest(t, reconnectPath, "logout/login complete; host hst_123 reconnected\n")
	writeFileForCLITest(t, stopTranscriptPath, "rdev host service-control --platform macos --action stop --execute\n")
	writeFileForCLITest(t, uninstallTranscriptPath, "rdev host uninstall-service --platform macos --removed true\n")
	return managedMacServicePackageEvidenceForCLITest{
		reviewTranscriptPath:    reviewTranscriptPath,
		startTranscriptPath:     startTranscriptPath,
		inspectTranscriptPath:   inspectTranscriptPath,
		logsPath:                logsPath,
		releaseGatePath:         releaseGatePath,
		auditPath:               auditPath,
		reconnectPath:           reconnectPath,
		stopTranscriptPath:      stopTranscriptPath,
		uninstallTranscriptPath: uninstallTranscriptPath,
	}
}

func writeLinuxPackageEvidenceForCLITest(t *testing.T, root, releaseGate string) linuxPackageEvidenceForCLITest {
	t.Helper()
	evidenceRoot := filepath.Join(root, "linux-package-fixture")
	startTranscriptPath := filepath.Join(evidenceRoot, "start.txt")
	statusTranscriptPath := filepath.Join(evidenceRoot, "status.txt")
	logsPath := filepath.Join(evidenceRoot, "logs.txt")
	releaseGatePath := filepath.Join(evidenceRoot, "release-gate.json")
	auditPath := filepath.Join(evidenceRoot, "audit.jsonl")
	reconnectPath := filepath.Join(evidenceRoot, "reconnect.txt")
	stopTranscriptPath := filepath.Join(evidenceRoot, "stop.txt")
	uninstallTranscriptPath := filepath.Join(evidenceRoot, "uninstall.txt")
	sessionEvidenceDir := filepath.Join(evidenceRoot, "session-evidence")
	writeFileForCLITest(t, startTranscriptPath, "systemctl --user daemon-reload\nsystemctl --user enable --now remote-dev-skillkit-host.service\n")
	writeFileForCLITest(t, statusTranscriptPath, "systemctl --user status remote-dev-skillkit-host.service\nactive (running)\n")
	writeFileForCLITest(t, logsPath, "journalctl --user -u remote-dev-skillkit-host.service\nrelease gate passed\n")
	writeFileForCLITest(t, releaseGatePath, releaseGate+"\n")
	writeFileForCLITest(t, auditPath, `{"event":"session.joined"}`+"\n"+`{"event":"task.completed"}`+"\n")
	writeFileForCLITest(t, reconnectPath, "rebooted host reconnected as hst_123\n")
	writeFileForCLITest(t, stopTranscriptPath, "systemctl --user disable --now remote-dev-skillkit-host.service\n")
	writeFileForCLITest(t, uninstallTranscriptPath, "rdev host uninstall-service --platform linux --removed true\n")
	writeFileForCLITest(t, filepath.Join(sessionEvidenceDir, "manifest.json"), `{"schema_version":"rdev.session-evidence.v1"}`+"\n")
	writeFileForCLITest(t, filepath.Join(sessionEvidenceDir, "artifacts", "host-denial.json"), `{"schema_version":"rdev.host-denial.v1"}`+"\n")
	return linuxPackageEvidenceForCLITest{
		startTranscriptPath:     startTranscriptPath,
		statusTranscriptPath:    statusTranscriptPath,
		logsPath:                logsPath,
		releaseGatePath:         releaseGatePath,
		auditPath:               auditPath,
		reconnectPath:           reconnectPath,
		sessionEvidenceDir:      sessionEvidenceDir,
		stopTranscriptPath:      stopTranscriptPath,
		uninstallTranscriptPath: uninstallTranscriptPath,
	}
}

func writeFileForCLITest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyFileForCLITest(t *testing.T, source, dest string) {
	t.Helper()
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeFileForCLITest(t, dest, string(content))
}

func writeLayeredHandoffArchiveForCLITest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Windows-ConnectionEntry.zip")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	files := map[string]string{
		"Start-ConnectionEntry.ps1":            "& $PSScriptRoot\\rdev-bootstrap.exe layered-run\n",
		"Start-ConnectionEntry.cmd":            "@echo off\r\n%~dp0rdev-bootstrap.exe layered-run\r\n",
		"rdev-bootstrap.exe":                   "bootstrap fixture",
		"rdev-bootstrap.exe.rdev-release.json": "{}",
		"rdev-bootstrap.exe.sha256":            strings.Repeat("a", 64),
		"windows-layered-verification.json":    "{}",
		"ARCHIVE-RECOVERY.txt":                 "rdev-bootstrap.exe layered-run\n",
	}
	for _, name := range []string{
		"Start-ConnectionEntry.ps1",
		"Start-ConnectionEntry.cmd",
		"rdev-bootstrap.exe",
		"rdev-bootstrap.exe.rdev-release.json",
		"rdev-bootstrap.exe.sha256",
		"windows-layered-verification.json",
		"ARCHIVE-RECOVERY.txt",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func signReleaseArtifactWithCLIForTest(t *testing.T, dir, keyPath, name, content string) string {
	t.Helper()
	artifactPath := filepath.Join(dir, name)
	if err := os.WriteFile(artifactPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"release", "sign",
		"--artifact", artifactPath,
		"--key", keyPath,
		"--key-id", "release-root",
	}); err != nil {
		t.Fatal(err)
	}
	var signed struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &signed); err != nil {
		t.Fatalf("invalid sign output: %v\n%s", err, stdout.String())
	}
	if signed.RootPublicKey == "" {
		t.Fatalf("expected root public key in sign output: %s", stdout.String())
	}
	return signed.RootPublicKey
}

func createReleaseBundleForHostServeTest(t *testing.T, dir, keyPath, artifacts string) string {
	t.Helper()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"release", "create-bundle",
		"--dir", dir,
		"--artifacts", artifacts,
		"--require-artifacts", artifacts,
		"--key", keyPath,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Bundle string `json:"bundle"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid bundle output: %v\n%s", err, stdout.String())
	}
	if payload.Bundle == "" {
		t.Fatalf("expected bundle path in output: %s", stdout.String())
	}
	return payload.Bundle
}

func writeJSONForTest(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTrustBundleForTest(t *testing.T, path string) model.SignedTrustBundle {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle model.SignedTrustBundle
	if err := json.Unmarshal(content, &bundle); err != nil {
		t.Fatalf("invalid trust bundle: %v\n%s", err, string(content))
	}
	return bundle
}

func assertTrustVerifyOK(t *testing.T, content []byte, wantSequence int) {
	t.Helper()
	var payload struct {
		OK       bool `json:"ok"`
		Sequence int  `json:"sequence"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("invalid trust verify output: %v\n%s", err, string(content))
	}
	if !payload.OK || payload.Sequence != wantSequence {
		t.Fatalf("unexpected trust verify output: %s", string(content))
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func fileExistsForCLITest(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeWindowsPackageEvidenceForCLITest(t *testing.T, root, releaseVerification string) (string, string, string, string, string) {
	t.Helper()
	transcriptPath := filepath.Join(root, "transcript.txt")
	releaseVerificationPath := filepath.Join(root, "rdev-verify.json")
	auditPath := filepath.Join(root, "audit.jsonl")
	noPersistenceDir := filepath.Join(root, "no-persistence")
	denialProbesDir := filepath.Join(root, "denial-probes")
	if err := os.WriteFile(transcriptPath, []byte("temporary host transcript\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseVerificationPath, []byte(releaseVerification+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auditPath, []byte(`{"event":"session.joined"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeNamedFilesForTest(t, noPersistenceDir, []string{
		"services.txt",
		"scheduled_tasks.txt",
		"hkcu_run_key.txt",
		"hklm_run_key.txt",
		"startup_folders.txt",
		"firewall_rules.txt",
	})
	writeNamedFilesForTest(t, denialProbesDir, []string{
		"package.install.txt",
		"elevation.request.txt",
		"service.manage.txt",
		"gui.control.txt",
		"credential.change.txt",
	})
	return transcriptPath, releaseVerificationPath, auditPath, noPersistenceDir, denialProbesDir
}

func writeLayeredRunReportForCLITest(t *testing.T, root, name string, fromCache bool) string {
	t.Helper()
	report := bootstrapcmd.LayeredRunReport{
		SchemaVersion: bootstrapcmd.LayeredRunReportSchemaVersion,
		AssetID:       "rdev-host-windows-amd64",
		FromCache:     fromCache,
		Resumed:       false,
		Bytes:         4096,
		Stages: []bootstrapcmd.LayeredRunStage{
			{Name: "manifest-fetch", DurationMS: 12},
			{Name: "signature-verification", DurationMS: 3},
			{Name: "runtime-download", DurationMS: 24},
			{Name: "runtime-launch-preparation", DurationMS: 2},
		},
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, append(content, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeLayeredEntryEvidenceForCLITest(t *testing.T, root, planPath string) string {
	t.Helper()
	content, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		HandoffZIPSizeBytes int64  `json:"handoff_archive_size_bytes"`
		HandoffZIPSHA256    string `json:"handoff_archive_sha256"`
	}
	if err := json.Unmarshal(content, &plan); err != nil {
		t.Fatal(err)
	}
	evidence := map[string]any{
		"schema_version":            "rdev.acceptance.windows-layered-entry-evidence.v1",
		"windows_release":           "11",
		"architecture":              "amd64",
		"handoff_zip_size_bytes":    plan.HandoffZIPSizeBytes,
		"handoff_zip_sha256":        plan.HandoffZIPSHA256,
		"selected_launcher":         "powershell",
		"fallback_attempts":         []string{"powershell"},
		"core_start_count":          1,
		"network_bytes":             4096,
		"registration_duration_ms":  750,
		"cache_hit":                 false,
		"range_interrupted":         true,
		"range_resumed":             true,
		"range_bytes":               2048,
		"private_acl":               true,
		"unc_rejected":              true,
		"reparse_rejected":          true,
		"defender_lock_verified":    true,
		"active_route_failed":       true,
		"route_reselected":          true,
		"registration_count":        1,
		"session_identity_stable":   true,
		"event_cursor_stable":       true,
		"archive_recovery_executed": false,
		"bootstrap_only":            true,
		"persistence_residue":       []string{},
		"cleanup_complete":          true,
	}
	evidenceContent, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "layered-entry-evidence.json")
	if err := os.WriteFile(path, append(evidenceContent, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeNamedFilesForTest(t *testing.T, dir string, names []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+" ok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runHostServeWithIdentityStore(t *testing.T, gatewayURL, ticketCode, identityPath, name string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", gatewayURL,
		"--ticket-code", ticketCode,
		"--identity-store", identityPath,
		"--identity-key-id", "host-test",
		"--name", name,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Host struct {
			IdentityFingerprint string `json:"identity_fingerprint"`
		} `json:"host"`
		Identity struct {
			Fingerprint       string `json:"fingerprint"`
			Stored            bool   `json:"stored"`
			ProofSchema       string `json:"proof_schema"`
			RegistrationProof bool   `json:"registration_proof"`
		} `json:"identity"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Identity.Fingerprint == "" {
		t.Fatalf("expected identity fingerprint in output, got %s", stdout.String())
	}
	if !payload.Identity.Stored {
		t.Fatalf("expected identity to be stored, got %s", stdout.String())
	}
	if !payload.Identity.RegistrationProof || payload.Identity.ProofSchema != model.HostRegistrationProofSchemaVersion {
		t.Fatalf("expected registration proof schema %q, got %s", model.HostRegistrationProofSchemaVersion, stdout.String())
	}
	if payload.Host.IdentityFingerprint != payload.Identity.Fingerprint {
		t.Fatalf("expected host identity fingerprint %q, got %q", payload.Identity.Fingerprint, payload.Host.IdentityFingerprint)
	}
	return payload.Identity.Fingerprint
}

func requireGitForCLITest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func initGitRepoForCLITest(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitForCLITest(t, repo, "init")
	runGitForCLITest(t, repo, "config", "user.email", "rdev-test@example.com")
	runGitForCLITest(t, repo, "config", "user.name", "Rdev Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForCLITest(t, repo, "add", "README.md")
	runGitForCLITest(t, repo, "commit", "-m", "initial")
	return repo
}

func runGitForCLITest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func buildCLITestBinary(t *testing.T, source string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is required")
	}
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	binaryName := "rdev-cli-test"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(dir, binaryName)
	cmd := exec.Command("go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(output))
	}
	return binaryPath
}

type gatewayTLSMaterial struct {
	CACert     string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

func writeGatewayTLSMaterial(t *testing.T) gatewayTLSMaterial {
	t.Helper()
	dir := t.TempDir()
	caCert, caKey := createTestCertificateAuthority(t)
	serverCert, serverKey := createSignedTestCertificate(t, caCert, caKey, "rdev-gateway-test-server", []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")}, nil)
	clientCert, clientKey := createSignedTestCertificate(t, caCert, caKey, "rdev-gateway-test-client", nil, nil, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	material := gatewayTLSMaterial{
		CACert:     filepath.Join(dir, "ca.pem"),
		ServerCert: filepath.Join(dir, "server-cert.pem"),
		ServerKey:  filepath.Join(dir, "server-key.pem"),
		ClientCert: filepath.Join(dir, "client-cert.pem"),
		ClientKey:  filepath.Join(dir, "client-key.pem"),
	}
	writePEMFile(t, material.CACert, "CERTIFICATE", caCert.Raw)
	writePEMFile(t, material.ServerCert, "CERTIFICATE", serverCert.Raw)
	writePEMFile(t, material.ServerKey, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverKey))
	writePEMFile(t, material.ClientCert, "CERTIFICATE", clientCert.Raw)
	writePEMFile(t, material.ClientKey, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(clientKey))
	return material
}

func createTestCertificateAuthority(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rdev gateway test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func createSignedTestCertificate(t *testing.T, caCert *x509.Certificate, caKey *rsa.PrivateKey, commonName string, dnsNames []string, ipAddresses []net.IP, extKeyUsage []x509.ExtKeyUsage) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(extKeyUsage) == 0 {
		extKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  extKeyUsage,
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func writePEMFile(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	var content bytes.Buffer
	if err := pem.Encode(&content, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readCLIFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func issueHostSecretForCLITest(t *testing.T, gw *gateway.MemoryGateway, hostID string) string {
	t.Helper()
	secret, err := gw.GenerateHostSecret(hostID)
	if err != nil {
		t.Fatal(err)
	}
	return secret
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

func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
