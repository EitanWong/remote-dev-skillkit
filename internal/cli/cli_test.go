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
	"os/exec"
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
		"--nonce-store", filepath.Join(dir, "nonces.json"),
		"--approval-store", filepath.Join(dir, "approvals.json"),
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

func TestHostServeLongPollWaitsAndCompletesDevJob(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)

	go func() {
		processed, err := app.pollAndRunDevJobs(ctx, hostServeOptions{
			GatewayURL:      server.URL,
			Transport:       "long-poll",
			LongPollTimeout: time.Second,
			MaxJobs:         1,
		}, host.ID, "")
		if err != nil {
			done <- err
			return
		}
		if processed != 1 {
			done <- errors.New("expected one processed long-poll job")
			return
		}
		done <- nil
	}()

	time.Sleep(50 * time.Millisecond)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", completed.Status)
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

func TestRefreshHostTrustUpdatePersistsGatewayUpdate(t *testing.T) {
	now := time.Now().Add(-time.Minute).UTC()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "managed-mac",
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
	storePath := filepath.Join(t.TempDir(), "trust", "bundle.json")
	store := hosttrust.FileStore{Path: storePath}
	first := gw.SignedTrustBundle()
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	firstHash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	next, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           first.BundleID,
		Sequence:           2,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: firstHash,
		SigningKeyID:       "gateway-dev",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway-dev", publicKey, model.TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err = next.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.UpdateSignedTrustBundle(next); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	current := hostTrust{SignedBundle: &first}

	updated, err := refreshHostTrustUpdate(context.Background(), server.URL, host.ID, storePath, current)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SignedBundle == nil || updated.SignedBundle.Sequence != 2 {
		t.Fatalf("expected in-memory trust to update to sequence 2, got %#v", updated.SignedBundle)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded.Sequence != 2 {
		t.Fatalf("expected persisted sequence 2, ok=%v bundle=%#v", ok, loaded)
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

func TestHostTrustRejectsConsumedApprovalWithApprovalStore(t *testing.T) {
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
		"workspace_root":     ".",
		"capabilities":       []string{"shell.user"},
		"argv":               []string{"go", "env", "GOOS"},
		"allow_commands":     []string{"go"},
		"approvals_required": []string{"git.push"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.ApproveJob(job.ID, "git.push", "approved", "test approval")
	if err != nil {
		t.Fatal(err)
	}
	legacy := gw.TrustBundle()
	trust := hostTrust{
		Legacy:        &legacy,
		ApprovalStore: hostApprovalStore(filepath.Join(t.TempDir(), "approval", "store.json")),
	}
	now := time.Now()
	if _, err := trust.RunDevJob(host.ID, "", job, now); err != nil {
		t.Fatalf("expected first approved execution to pass: %v", err)
	}
	if _, err := trust.RunDevJob(host.ID, "", job, now); !errors.Is(err, model.ErrApprovalTokenConsumed) {
		t.Fatalf("expected consumed approval token rejection, got %v", err)
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

func TestHostServeReportsHostDenialArtifact(t *testing.T) {
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
		"capabilities": []string{"shell.user"},
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
		t.Fatal("expected host runner denial")
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
	if !strings.Contains(failed.FailureReason, "Workspace root is required") {
		t.Fatalf("expected denial summary as failure reason, got %q", failed.FailureReason)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 failure artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"schema_version": "rdev.host-denial.v1"`) {
		t.Fatalf("expected denial artifact, got %s", artifacts[0].Content)
	}
	if !strings.Contains(artifacts[0].Content, `"code": "workspace_required"`) {
		t.Fatalf("expected workspace_required denial artifact, got %s", artifacts[0].Content)
	}
}

func TestHostServeReportsApprovalRequiredArtifact(t *testing.T) {
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
		"workspace_root":     ".",
		"capabilities":       []string{"shell.user"},
		"argv":               []string{"go", "env", "GOOS"},
		"allow_commands":     []string{"go"},
		"approvals_required": []string{"git.push"},
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
		t.Fatal("expected approval requirement")
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
	if !strings.Contains(failed.FailureReason, "requires approval") {
		t.Fatalf("expected approval summary as failure reason, got %q", failed.FailureReason)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 failure artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"schema_version": "rdev.approval-required.v1"`) {
		t.Fatalf("expected approval-required artifact, got %s", artifacts[0].Content)
	}
	if !strings.Contains(artifacts[0].Content, `"git.push"`) {
		t.Fatalf("expected git.push approval requirement, got %s", artifacts[0].Content)
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

func TestEvidenceExportFromGatewayJobIDWritesBundle(t *testing.T) {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CompleteJobForHost(host.ID, job.ID, `{"schema_version":"rdev.shell-result.v1","exit_code":0}`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	out := filepath.Join(t.TempDir(), "bundle")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err = app.Run(context.Background(), []string{
		"evidence", "export",
		"--gateway", server.URL,
		"--job-id", job.ID,
		"--out", out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"source": "gateway"`) {
		t.Fatalf("expected gateway source output, got %s", stdout.String())
	}
	for _, path := range []string{"manifest.json", "job.json", "envelope.json", "artifacts.json", "audit-slice.jsonl", "audit-chain.json", "checksums.txt"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected gateway evidence bundle file %s: %v", path, err)
		}
	}
	artifactFiles, err := os.ReadDir(filepath.Join(out, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(artifactFiles) != 1 {
		t.Fatalf("expected one artifact file, got %d", len(artifactFiles))
	}
	artifactContent := readFileForTest(t, filepath.Join(out, "artifacts", artifactFiles[0].Name()))
	if !strings.Contains(artifactContent, "rdev.shell-result.v1") {
		t.Fatalf("expected shell result artifact content, got %s", artifactContent)
	}
	if content := readFileForTest(t, filepath.Join(out, "audit-slice.jsonl")); !strings.Contains(content, "job.complete") {
		t.Fatalf("expected job.complete audit event, got %s", content)
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
		"--job-id", "job_cli",
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
		"--job-id", "job_cli",
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
		"--job-id", "job_cli",
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
				JobID        string `json:"job_id"`
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
	if payload.Worktree.Branch != "rdev/job_job_cli" {
		t.Fatalf("unexpected branch %q", payload.Worktree.Branch)
	}
	if payload.Worktree.Lock.JobID != "job_cli" || payload.Worktree.Lock.OwnerAdapter != "codex" {
		t.Fatalf("unexpected lock %#v", payload.Worktree.Lock)
	}
	if _, err := os.Stat(filepath.Join(payload.Worktree.WorktreePath, "README.md")); err != nil {
		t.Fatalf("expected checked out worktree: %v", err)
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

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
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
