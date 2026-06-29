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
	"runtime"
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
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
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
		"--nonce-store", filepath.Join(dir, "nonces.json"),
		"--approval-store", filepath.Join(dir, "approvals.json"),
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

func TestHostServeCancelsRunningCodexJobWhenGatewayJobCanceled(t *testing.T) {
	requireGitForCLITest(t)
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
	repo := initGitRepoForCLITest(t)
	fakeCodex := buildCLITestBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	job, err := gw.CreateJob(host.ID, "codex", "cancel running codex", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"codex.run", "git.diff"},
		"prompt":               "sleep until the gateway cancels this job",
		"codex_command":        fakeCodex,
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct {
		processed int
		err       error
	}, 1)
	go func() {
		processed, err := app.pollAndRunDevJobs(ctx, hostServeOptions{
			GatewayURL:   server.URL,
			PollInterval: 50 * time.Millisecond,
			MaxJobs:      1,
		}, host.ID, "")
		done <- struct {
			processed int
			err       error
		}{processed: processed, err: err}
	}()
	waitForJobStatus(t, gw, job.ID, model.JobStatusRunning, 2*time.Second)
	if _, err := gw.CancelJob(job.ID, "operator cancel"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.processed != 1 {
			t.Fatalf("expected one processed canceled job, got %d", result.processed)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	canceled, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != model.JobStatusCanceled {
		t.Fatalf("expected job to remain canceled, got %s", canceled.Status)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected one cancellation artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"canceled": true`) {
		t.Fatalf("expected canceled evidence artifact, got %s", artifacts[0].Content)
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
	if _, err := trust.RunDevJob(context.Background(), host.ID, "", job, now); err != nil {
		t.Fatalf("expected first execution to pass: %v", err)
	}
	if _, err := trust.RunDevJob(context.Background(), host.ID, "", job, now); err == nil {
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
	if _, err := trust.RunDevJob(context.Background(), host.ID, "", job, now); err != nil {
		t.Fatalf("expected first approved execution to pass: %v", err)
	}
	if _, err := trust.RunDevJob(context.Background(), host.ID, "", job, now); !errors.Is(err, model.ErrApprovalTokenConsumed) {
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

func TestHostServeReportsWorkspaceLockedArtifact(t *testing.T) {
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
	repo := t.TempDir()
	lockStore := filepath.Join(t.TempDir(), "locks")
	if _, err := workspace.NewFileLockStore(lockStore).Acquire(workspace.LockOptions{
		RepoRoot:     repo,
		HostID:       host.ID,
		JobID:        "job_existing",
		OwnerAdapter: "codex",
		TTL:          time.Hour,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": repo,
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:         server.URL,
		PollInterval:       1,
		MaxJobs:            1,
		WorkspaceLockStore: lockStore,
	}, host.ID, "")
	if err == nil {
		t.Fatal("expected workspace lock denial")
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
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 failure artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"code": "workspace_locked"`) {
		t.Fatalf("expected workspace_locked denial artifact, got %s", artifacts[0].Content)
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
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_approvals": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": true},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_jobs": false,
    "adapter_approves_dangerous_actions": false,
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
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_approvals": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": false},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_jobs": false,
    "adapter_approves_dangerous_actions": false,
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
		"--artifacts", strings.Join([]string{rdev, host, verifier}, ","),
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
		"--key", keyPath,
		"--key-id", "release-root",
	}); err != nil {
		t.Fatalf("expected release candidate preparation to pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), `"schema": "rdev.release-candidate.v1"`) {
		t.Fatalf("expected release candidate output, got %s", stdout.String())
	}
	for _, path := range []string{
		"release-candidate.json",
		"release-bundle.json",
		"checksums.txt",
		"skillkit/manifest.json",
		"rdev-host.exe.rdev-release.json",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected release candidate file %s: %v", path, err)
		}
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
		"--artifacts", strings.Join([]string{rdev, host, verifier}, ","),
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
		OK               bool `json:"ok"`
		Report           string
		Evidence         string
		ApprovalEvidence string `json:"approval_evidence"`
		Worktree         string
		Checks           []struct {
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
		filepath.Join(payload.ApprovalEvidence, "manifest.json"),
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
	if !strings.Contains(report, `"schema_version": "rdev.evidence-bundle.v1"`) {
		t.Fatalf("expected embedded evidence manifests, got %s", report)
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

	if err := os.WriteFile(filepath.Join(payload.Evidence, "artifacts.json"), []byte("tampered"), 0o600); err != nil {
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
}

func TestAcceptanceWindowsTemporaryPlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	script := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--download-url", "https://agent.example.com/rdev-host.exe",
		"--expected-sha256", strings.Repeat("a", 64),
		"--bootstrap-script", script,
		"--manifest-url", "https://agent.example.com/j/ABCD-1234/manifest",
		"--manifest-root-public-key", "manifest-root:abc",
		"--release-manifest-url", "https://agent.example.com/rdev-host.exe.rdev-release.json",
		"--release-root-public-key", "release-root:abc",
		"--verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--verifier-sha256", strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK       bool   `json:"ok"`
		Schema   string `json:"schema"`
		Plan     string `json:"plan"`
		Launcher string `json:"launcher"`
		Commands []struct {
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
	for _, path := range []string{payload.Plan, payload.Launcher} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	output := stdout.String()
	if !strings.Contains(output, "run_foreground_temporary_host") || !strings.Contains(output, "Get-ScheduledTask") {
		t.Fatalf("expected foreground and no-persistence commands, got %s", output)
	}
	launcher := readFileForTest(t, payload.Launcher)
	if strings.Contains(launcher, "-ReleaseBundleRequiredArtifacts") {
		t.Fatalf("manifest-only launcher should not contain bundle args: %s", launcher)
	}
}

func TestAcceptanceWindowsTemporaryBundlePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	script := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--download-url", "https://agent.example.com/rdev-host.exe",
		"--expected-sha256", strings.Repeat("a", 64),
		"--bootstrap-script", script,
		"--release-bundle-url", "https://agent.example.com/release-bundle.json",
		"--release-root-public-key", "release-root:abc",
		"--verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--verifier-sha256", strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK       bool   `json:"ok"`
		Schema   string `json:"schema"`
		Plan     string `json:"plan"`
		Launcher string `json:"launcher"`
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
	launcher := readFileForTest(t, payload.Launcher)
	if !strings.Contains(launcher, "-ReleaseBundleUrl 'https://agent.example.com/release-bundle.json'") {
		t.Fatalf("expected release bundle launcher arg, got %s", launcher)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected bundle verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}
}

func TestAcceptanceVerifyWindowsTemporaryPlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	script := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--download-url", "https://agent.example.com/rdev-host.exe",
		"--expected-sha256", strings.Repeat("a", 64),
		"--bootstrap-script", script,
		"--release-manifest-url", "https://agent.example.com/rdev-host.exe.rdev-release.json",
		"--release-root-public-key", "release-root:abc",
		"--verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--verifier-sha256", strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Plan     string `json:"plan"`
		Launcher string `json:"launcher"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(payload.Launcher, []byte("Set-ExecutionPolicy Bypass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", payload.Plan,
	})
	if err == nil {
		t.Fatalf("expected tampered verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) || !strings.Contains(tamperedStdout.String(), "launcher_has_no_forbidden_side_effects") {
		t.Fatalf("expected structured tampered failure, got %s", tamperedStdout.String())
	}
}

func TestAcceptancePackageWindowsTemporary(t *testing.T) {
	root := t.TempDir()
	planOut := filepath.Join(root, "windows-temporary")
	script := filepath.Join(root, "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", planOut,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--download-url", "https://agent.example.com/rdev-host.exe",
		"--expected-sha256", strings.Repeat("a", 64),
		"--bootstrap-script", script,
		"--release-bundle-url", "https://agent.example.com/release-bundle.json",
		"--release-root-public-key", "release-root:abc",
		"--verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--verifier-sha256", strings.Repeat("b", 64),
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
	transcriptPath, releaseVerificationPath, auditPath, noPersistenceDir, approvalProbesDir := writeWindowsPackageEvidenceForCLITest(t, root, `{"ok": true, "token": "`+fakeGitHubToken+`"}`)

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-windows-temporary",
		"--plan", planPayload.Plan,
		"--out", filepath.Join(root, "windows-evidence"),
		"--transcript", transcriptPath,
		"--release-verification", releaseVerificationPath,
		"--audit", auditPath,
		"--no-persistence-dir", noPersistenceDir,
		"--approval-probes-dir", approvalProbesDir,
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
	if payload.Schema != "rdev.acceptance-package.windows-temporary.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if _, err := os.Stat(payload.Package); err != nil {
		t.Fatalf("expected package manifest: %v", err)
	}
	if payload.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github_token redaction count, got %#v", payload.RedactionRuleCounts)
	}
}

func timeNowForTest() time.Time {
	return time.Now().UTC().Add(-time.Minute)
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

func writeWindowsPackageEvidenceForCLITest(t *testing.T, root, releaseVerification string) (string, string, string, string, string) {
	t.Helper()
	transcriptPath := filepath.Join(root, "transcript.txt")
	releaseVerificationPath := filepath.Join(root, "rdev-verify.json")
	auditPath := filepath.Join(root, "audit.jsonl")
	noPersistenceDir := filepath.Join(root, "no-persistence")
	approvalProbesDir := filepath.Join(root, "approval-probes")
	if err := os.WriteFile(transcriptPath, []byte("temporary host transcript\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseVerificationPath, []byte(releaseVerification+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auditPath, []byte(`{"event":"host.registered"}`+"\n"), 0o600); err != nil {
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
	writeNamedFilesForTest(t, approvalProbesDir, []string{
		"package.install.txt",
		"elevation.request.txt",
		"service.manage.txt",
		"gui.control.txt",
		"credential.change.txt",
	})
	return transcriptPath, releaseVerificationPath, auditPath, noPersistenceDir, approvalProbesDir
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

func waitForJobStatus(t *testing.T, gw *gateway.MemoryGateway, jobID string, status model.JobStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := gw.Job(jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := gw.Job(jobID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("timed out waiting for job %s status %s, got %s", jobID, status, job.Status)
}
