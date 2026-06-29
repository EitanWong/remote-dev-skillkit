package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hosttrust"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

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
	if !strings.Contains(stdout.String(), "provide --ticket-code") {
		t.Fatalf("expected ticket-code guidance, got %q", stdout.String())
	}
}

func TestHostServeRegistersWithLocalGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary", "--gateway", server.URL, "--ticket-code", ticket.Code, "--name", "test-host"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status string `json:"status"`
		Host   struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "registered-pending-approval" {
		t.Fatalf("unexpected status %q", payload.Status)
	}
	if payload.Host.Name != "test-host" {
		t.Fatalf("expected host name override, got %q", payload.Host.Name)
	}
	if payload.Host.Status != "pending" {
		t.Fatalf("expected pending host, got %q", payload.Host.Status)
	}
}

func TestHostServeRegistersWithIdentityStore(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	identityPath := filepath.Join(t.TempDir(), "identity", "host.json")

	firstFingerprint := runHostServeWithIdentityStore(t, server.URL, ticket.Code, identityPath, "test-host-1")
	hosts := gw.Hosts("")
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].IdentityFingerprint != firstFingerprint {
		t.Fatalf("expected stored host fingerprint %q, got %q", firstFingerprint, hosts[0].IdentityFingerprint)
	}
	if hosts[0].IdentityKeyID != "host-test" {
		t.Fatalf("expected identity key id host-test, got %q", hosts[0].IdentityKeyID)
	}
	info, err := os.Stat(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected identity store 0600 permissions, got %#o", got)
	}

	secondTicket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint := runHostServeWithIdentityStore(t, server.URL, secondTicket.Code, identityPath, "test-host-2")
	if secondFingerprint != firstFingerprint {
		t.Fatalf("expected identity fingerprint reuse, got %s then %s", firstFingerprint, secondFingerprint)
	}
}

func TestHostServeRegistersWithJoinManifest(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary", "--manifest-url", server.URL + "/v1/tickets/" + ticket.Code + "/manifest", "--name", "manifest-host"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status string `json:"status"`
		Host   struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "registered-pending-approval" {
		t.Fatalf("unexpected status %q", payload.Status)
	}
	if payload.Host.Name != "manifest-host" {
		t.Fatalf("expected host name override, got %q", payload.Host.Name)
	}
}

func TestHostServeRegistersWithJoinManifestRoot(t *testing.T) {
	gatewayPublicKey, gatewayPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifestPublicKey, manifestPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, "gateway-jobs", gatewayPublicKey, gatewayPrivateKey).
		WithManifestSigningKey("manifest-root", manifestPublicKey, manifestPrivateKey)
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--manifest-url", server.URL + "/v1/tickets/" + ticket.Code + "/manifest",
		"--manifest-root-public-key", encodeRootPublicKey("manifest-root", manifestPublicKey),
		"--name", "manifest-root-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Host struct {
			Name string `json:"name"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Host.Name != "manifest-root-host" {
		t.Fatalf("expected host name override, got %q", payload.Host.Name)
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
	gw := gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, "gateway-jobs", gatewayPublicKey, gatewayPrivateKey).
		WithManifestSigningKey("manifest-root", manifestPublicKey, manifestPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilitiesToStrings(policy.TemporaryDefaults()), "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	_, err = fetchJoinManifest(
		context.Background(),
		server.URL+"/v1/tickets/"+ticket.Code+"/manifest",
		"",
		encodeRootPublicKey("manifest-root", wrongPublicKey),
	)
	if !errors.Is(err, model.ErrJoinManifestSignature) {
		t.Fatalf("expected manifest signature failure, got %v", err)
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

	_, err = fetchJoinManifest(context.Background(), server.URL+"/v1/tickets/"+ticket.Code+"/manifest", "sha256:0000", "")
	if err == nil {
		t.Fatal("expected trust pin mismatch")
	}
	if !strings.Contains(err.Error(), "trust pin mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostServePollsAndCompletesDevJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job, got %d", processed)
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", completed.Status)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"exit_code": 0`) {
		t.Fatalf("expected shell execution evidence, got %s", artifacts[0].Content)
	}
}

func TestHostServeRejectsTrustPinMismatch(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	_, err = app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
		TrustPin:     "sha256:0000",
	}, host.ID, "")
	if err == nil {
		t.Fatal("expected trust pin mismatch")
	}
	if !strings.Contains(err.Error(), "trust pin mismatch") {
		t.Fatalf("unexpected error: %v", err)
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

	trust, err := fetchHostTrust(context.Background(), server.URL, "", "")
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

	trust, err := fetchHostTrust(context.Background(), server.URL, "", storePath)
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

	trust, err := fetchHostTrust(context.Background(), server.URL, "", storePath)
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

func TestHostTrustRejectsReplayWithNonceStore(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	legacy := gw.TrustBundle()
	trust := hostTrust{
		Legacy:     &legacy,
		NonceStore: hostNonceStore(filepath.Join(t.TempDir(), "nonce", "store.json")),
	}
	now := time.Now()
	if _, err := trust.RunDevJob(host.ID, "", job, now); err != nil {
		t.Fatalf("expected first execution to pass: %v", err)
	}
	if _, err := trust.RunDevJob(host.ID, "", job, now); err == nil {
		t.Fatal("expected replay rejection")
	}
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

func TestHostServeReportsFailedDevJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "tool", "rdev-no-such-tool"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, host.ID, "")
	if err == nil {
		t.Fatal("expected host runner failure")
	}
	if processed != 0 {
		t.Fatalf("expected 0 processed jobs, got %d", processed)
	}
	failed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.JobStatusFailed {
		t.Fatalf("expected failed job, got %s", failed.Status)
	}
	if failed.FailureReason == "" {
		t.Fatal("failure reason should be set")
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 failure artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"exit_code":`) {
		t.Fatalf("expected failure execution evidence, got %s", artifacts[0].Content)
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
	if !strings.Contains(stdout.String(), "https://agent.lunflux.com/join/") {
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
		Host struct {
			Status string `json:"status"`
		} `json:"host"`
		Job struct {
			Status string `json:"status"`
		} `json:"job"`
		Audit []struct {
			Action string `json:"action"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Host.Status != "active" {
		t.Fatalf("host should be active, got %q", payload.Host.Status)
	}
	if payload.Job.Status != "succeeded" {
		t.Fatalf("job should succeed, got %q", payload.Job.Status)
	}
	if len(payload.Audit) != 5 {
		t.Fatalf("expected 5 audit events, got %d", len(payload.Audit))
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

func TestEvidenceExportWritesBundle(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	job := model.Job{
		ID:        "job_1",
		HostID:    "hst_1",
		Adapter:   "shell",
		Intent:    "demo",
		Status:    model.JobStatusSucceeded,
		CreatedAt: now,
		Envelope: &model.JobEnvelope{
			SchemaVersion: "rdev.job.v1",
			JobID:         "job_1",
			HostID:        "hst_1",
			TicketID:      "tkt_1",
			OperatorID:    "operator",
			IssuedAt:      now,
			ExpiresAt:     now.Add(time.Hour),
			Nonce:         "nonce",
			Mode:          model.HostModeAttendedTemporary,
			Adapter:       "shell",
			Intent:        "demo",
			Capabilities:  []string{"shell.user"},
			Limits:        model.JobLimits{MaxDurationSeconds: 60, MaxOutputBytes: 1024, Network: "default-deny"},
			SigningAlg:    "ed25519",
			SigningKeyID:  "gateway-dev",
			Signature:     "signature",
		},
	}
	artifact := model.Artifact{
		ID:        "art_1",
		JobID:     "job_1",
		Kind:      "text",
		Name:      "result.json",
		Content:   `{"schema_version":"rdev.shell-result.v1"}`,
		CreatedAt: now,
	}
	jobPath := filepath.Join(dir, "job.json")
	artifactsPath := filepath.Join(dir, "artifacts.json")
	auditPath := filepath.Join(dir, "events.jsonl")
	out := filepath.Join(dir, "bundle")
	writeJSONForTest(t, jobPath, job)
	writeJSONForTest(t, artifactsPath, []model.Artifact{artifact})
	store := audit.NewJSONLStore(auditPath)
	for _, event := range []model.AuditEvent{
		{Sequence: 1, Actor: "operator", Action: "job.create", TargetID: "job_1", Message: "created", At: now},
		{Sequence: 2, Actor: "host", Action: "job.complete", TargetID: "job_1", Message: "done", At: now},
	} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"evidence", "export",
		"--job-json", jobPath,
		"--artifacts-json", artifactsPath,
		"--audit-jsonl", auditPath,
		"--out", out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("expected ok output, got %s", stdout.String())
	}
	for _, path := range []string{"manifest.json", "job.json", "envelope.json", "artifacts/art_1-result.json", "audit-chain.json", "checksums.txt"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected bundle file %s: %v", path, err)
		}
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

func timeNowForTest() time.Time {
	return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
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
			Fingerprint string `json:"fingerprint"`
			Stored      bool   `json:"stored"`
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
	if payload.Host.IdentityFingerprint != payload.Identity.Fingerprint {
		t.Fatalf("expected host identity fingerprint %q, got %q", payload.Identity.Fingerprint, payload.Host.IdentityFingerprint)
	}
	return payload.Identity.Fingerprint
}
