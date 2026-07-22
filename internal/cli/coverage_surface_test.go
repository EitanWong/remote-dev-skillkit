package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/acceptance"
	"github.com/EitanWong/remote-dev-skillkit/internal/connectionentry"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestCLISurfaceHelpers(t *testing.T) {
	if samePath("", "x") || samePath("x", "y") {
		t.Fatal("samePath returned true for empty or distinct paths")
	}
	tmp := t.TempDir()
	left := filepath.Join(tmp, "left")
	if err := os.WriteFile(left, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !samePath(left, left) || !pathExists(left) || pathExists(filepath.Join(tmp, "missing")) {
		t.Fatal("samePath/pathExists did not handle existing paths")
	}
	if expandHomePath("~/remote") == "~/remote" || expandHomePath(" ") == "" {
		t.Fatal("expandHomePath did not normalize home paths")
	}
	if rootPublicKeyString(model.TrustBundle{}) != "" || rootPublicKeyString(model.TrustBundle{SigningKeyID: "root", PublicKey: "key"}) != "root:key" {
		t.Fatal("root public key formatting changed")
	}
	if supportSessionContinuityAction(true, false) == "" || supportSessionContinuityAction(false, true) == "" || supportSessionContinuityAction(false, false) == "" {
		t.Fatal("continuity actions should cover all gateway states")
	}
	continuity := supportSessionConnectionContinuity("https://edge.example")
	if continuity["stable_gateway"] != true || continuity["ephemeral_quick_tunnel"] != false {
		t.Fatalf("continuity = %#v", continuity)
	}
	if !taskStatusTerminal(string(controlplane.TaskStatusSucceeded)) || !taskStatusTerminal(string(controlplane.TaskStatusFailed)) || !taskStatusTerminal(string(controlplane.TaskStatusCanceled)) || taskStatusTerminal("running") {
		t.Fatal("task terminal status classification changed")
	}
	if platformOS("windows/amd64") != "windows" || platformArch("windows/amd64") != "amd64" || platformArch("windows") != "" {
		t.Fatal("platform parsing changed")
	}

	snapshot := map[string]any{"endpoints": []any{
		map[string]any{"id": "end_target", "role": "target", "state": "online", "platform": "darwin/arm64", "name": "mac"},
		map[string]any{"id": "end_other", "role": "operator", "state": "online"},
	}}
	if targetEndpointFromSnapshot(snapshot, "end_target")["id"] != "end_target" || targetEndpointFromSnapshot(snapshot, "missing")["id"] != "end_target" {
		t.Fatal("target endpoint selection changed")
	}
	host := hostMapFromEndpoint(snapshot["endpoints"].([]any)[0].(map[string]any))
	if host["os"] != "darwin" || host["arch"] != "arm64" || host["name"] != "mac" {
		t.Fatalf("host map = %#v", host)
	}
	if stringFromNestedMap(map[string]any{"status": map[string]any{"value": "ok"}}, "status", "value") != "ok" || nestedMapOrSelf(map[string]any{"value": "ok"}, "missing")["value"] != "ok" {
		t.Fatal("nested map helpers changed")
	}
	if len(mapSliceFromAny([]map[string]any{{"id": "typed"}})) != 1 || len(mapSliceFromAny([]any{map[string]any{"id": "raw"}, "ignored"})) != 1 || mapSliceFromAny("wrong") != nil {
		t.Fatal("map slice conversion changed")
	}
	if responseErrorMessage(map[string]any{"error": "specific"}, "fallback") != "specific" || responseErrorMessage(nil, "fallback") != "fallback" {
		t.Fatal("response error fallback changed")
	}
	if summarizeFirstArtifact(map[string]any{"artifacts": []any{map[string]any{"content": "  artifact  "}}}) != "artifact" || summarizeFirstArtifact(map[string]any{}) != "" {
		t.Fatal("artifact summary changed")
	}
	long := oneLine(strings.Repeat("x", 300))
	if len(long) != 243 || strings.Contains(oneLine("a\n b"), "\n") {
		t.Fatal("oneLine did not normalize and cap text")
	}
	var card bytes.Buffer
	writeHostRemoteControlCard(&card, map[string]any{"support_device_id": "device", "session_passcode": "pass"})
	if !strings.Contains(card.String(), "Device ID: device") || !strings.Contains(card.String(), "Session Password: pass") {
		t.Fatalf("remote control card = %q", card.String())
	}
}

func TestCLIEntrypointHelp(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"rdev", "--help"}
	defer func() { os.Args = originalArgs }()
	Main()
}

func TestCLIPostGatewayJSONAndCommandDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("authorization header missing")
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()
	if payload, status, err := postGatewayJSON(context.Background(), server.URL+"/ok", map[string]any{"value": "x"}, "token"); err != nil || status != http.StatusOK || payload["ok"] != true {
		t.Fatalf("successful gateway post = %#v, %d, %v", payload, status, err)
	}
	if _, status, err := postGatewayJSON(context.Background(), server.URL+"/bad", map[string]any{}, ""); err == nil || status != http.StatusBadRequest {
		t.Fatalf("failed gateway post = %d, %v", status, err)
	}
	if _, _, err := postGatewayJSON(context.Background(), ":", nil, ""); err == nil {
		t.Fatal("invalid gateway endpoint was accepted")
	}

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{"mcp", "tools"}); err != nil {
		t.Fatal(err)
	}
	var toolsPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &toolsPayload); err != nil || len(toolsPayload["tools"].([]any)) != 8 {
		t.Fatalf("mcp tools payload = %#v, err=%v", toolsPayload, err)
	}
	for _, args := range [][]string{
		{"mcp"}, {"mcp", "unknown"}, {"gateway"}, {"gateway", "unknown"},
		{"gateway", "storage"}, {"gateway", "storage", "unknown"},
		{"gateway", "storage", "verify", "--provider", "unknown"},
	} {
		if err := app.Run(context.Background(), args); err == nil {
			t.Fatalf("expected command %v to fail", args)
		}
	}
}

func TestCLICommandGroupGuardsAndReadOnlyTunnelCommands(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	for _, group := range []string{
		"bootstrap", "tunnel", "host", "invite", "connection-entry", "ticket",
		"policy", "demo", "operator-auth", "hosted-provider", "relay-adapter", "release", "update",
		"deps", "enrollment", "trust", "audit", "skillkit", "workspace", "acceptance", "adapter",
		"desktop", "files", "task",
	} {
		if err := app.Run(context.Background(), []string{group}); err == nil {
			t.Fatalf("expected missing subcommand error for %q", group)
		}
	}
	for _, nearMiss := range []string{"hosts", "tickets", "invites", "policies", "gateways", "file", "connections", "connection_entry", "support_session", "mcp-serve", "host-serve"} {
		if err := app.Run(context.Background(), []string{nearMiss}); err == nil {
			t.Fatalf("expected near-miss command %q to fail", nearMiss)
		}
	}
	if err := app.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	for _, args := range [][]string{{"tunnel", "providers", "--json"}, {"tunnel", "probe", "--json"}} {
		stdout.Reset()
		if err := app.Run(context.Background(), args); err != nil {
			t.Fatalf("read-only tunnel command %v failed: %v", args, err)
		}
		if stdout.Len() == 0 {
			t.Fatalf("read-only tunnel command %v emitted no JSON", args)
		}
	}
}

func TestCLITaskAndDesktopValidationSurfaces(t *testing.T) {
	if _, err := readTaskPolicy("{}", "file"); err == nil {
		t.Fatal("accepted both task policy sources")
	}
	if _, err := readTaskPolicy("", ""); err == nil {
		t.Fatal("accepted missing task policy source")
	}
	if _, err := readTaskPolicy("{", ""); err == nil {
		t.Fatal("accepted malformed task policy")
	}
	if parsed, err := readTaskPolicy(`{"capability":"shell.user"}`, ""); err != nil || parsed["capability"] != "shell.user" {
		t.Fatalf("task policy = %#v, err=%v", parsed, err)
	}
	for _, subcommand := range []string{"windows", "screenshot", "record", "focus", "move", "url"} {
		if action, _, err := desktopCommandAction(subcommand, "", ""); err != nil || action == "" {
			t.Fatalf("desktop action %q = %q, err=%v", subcommand, action, err)
		}
	}
	for _, input := range []struct {
		subcommand string
		appAction  string
		text       string
		want       string
	}{
		{"input", "", "text", "input.keyboard"},
		{"input", "", "", "input.mouse"},
		{"app", "", "", "app.launch"},
		{"app", "close", "", "app.close"},
		{"clipboard", "", "", "clipboard.read"},
		{"clipboard", "write", "", "clipboard.write"},
	} {
		if action, _, err := desktopCommandAction(input.subcommand, input.appAction, input.text); err != nil || action != input.want {
			t.Fatalf("desktop action %#v = %q, err=%v", input, action, err)
		}
	}
	for _, invalid := range [][3]string{{"app", "bad", ""}, {"clipboard", "bad", ""}, {"unknown", "", ""}} {
		if _, _, err := desktopCommandAction(invalid[0], invalid[1], invalid[2]); err == nil {
			t.Fatalf("desktop action %#v was accepted", invalid)
		}
	}
	for _, action := range []string{"window.inspect", "screen.screenshot", "screen.record", "window.focus", "window.move", "input.keyboard", "input.mouse", "app.launch", "app.close", "url.open", "clipboard.read", "clipboard.write", "unknown"} {
		if capability, _ := desktopCapabilityForAction(action); capability == "" {
			t.Fatalf("empty desktop capability for %q", action)
		}
	}
	if _, _, err := fileCommandTransferExpectation("%%%", "base64"); err == nil {
		t.Fatal("invalid base64 content was accepted")
	}
	if _, _, err := fileCommandTransferExpectation("content", "unknown"); err == nil {
		t.Fatal("unknown file encoding was accepted")
	}
	if bytes, hash, err := fileCommandTransferExpectation("hello", "utf-8"); err != nil || bytes != 5 || !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("file transfer expectation = %d, %q, err=%v", bytes, hash, err)
	}
	if content, err := fileCommandContent("inline", "file"); err == nil || content != "" {
		t.Fatal("accepted both inline and file content")
	}
	if action, _, err := desktopCommandAction("input", "", ""); err != nil || action != "input.mouse" {
		t.Fatalf("mouse action = %q, err=%v", action, err)
	}

	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	for _, args := range [][]string{
		{"support-session", "connect", "--ttl-seconds", "1"},
		{"support-session", "handoff", "--ttl-seconds", "1"},
		{"files", "list"}, {"files", "list", "--gateway-url", "http://127.0.0.1"},
		{"desktop", "input"}, {"desktop", "input", "--gateway-url", "http://127.0.0.1"},
		{"desktop", "input", "--gateway-url", "http://127.0.0.1", "--session-id", "ses_1"},
		{"desktop", "app", "--gateway-url", "http://127.0.0.1", "--session-id", "ses_1", "--action", "bad"},
		{"task", "policy-template", "--capability", "shell.user"},
	} {
		if err := app.Run(context.Background(), args); args[0] == "task" && err != nil {
			t.Fatalf("task policy template failed: %v", err)
		} else if args[0] != "task" && err == nil {
			t.Fatalf("expected validation error for %v", args)
		}
	}
}

func TestCLISubcommandHelpCoverage(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	commands := map[string][]string{
		"bootstrap":        {"agent-plan"},
		"tunnel":           {"providers", "probe"},
		"mcp":              {"serve"},
		"host":             {"serve", "install-service", "service-status", "service-control", "uninstall-service"},
		"invite":           {"create"},
		"connection-entry": {"plan", "run"},
		"ticket":           {"create"},
		"policy":           {"explain", "explain-shell"},
		"demo":             {"local"},
		"operator-auth":    {"init", "verify", "verify-hosted", "verify-oidc-jwks", "verify-saml"},
		"hosted-provider":  {"package", "verify"},
		"relay-adapter":    {"package", "verify"},
		"release":          {"sign", "verify", "create-bundle", "verify-bundle", "prepare-candidate", "verify-candidate"},
		"update":           {"check", "plan"},
		"deps":             {"install"},
		"enrollment":       {"issue-certificate", "sign-certificate", "verify-certificate", "renew-certificate", "revoke-certificate", "init-revocations", "verify-revocations", "fetch-revocations"},
		"trust":            {"init", "rotate", "revoke", "verify"},
		"audit":            {"export", "verify"},
		"skillkit":         {"export", "verify", "plan-install", "verify-install-plan", "install"},
		"workspace":        {"lock", "status", "unlock", "prepare-worktree"},
		"acceptance":       {"fresh-agent-support-session", "managed-mac", "managed-mac-service", "windows-temporary", "windows-managed-service", "linux-managed-service", "verify", "verify-windows-temporary", "verify-managed-mac-service", "verify-windows-managed-service", "verify-linux-managed-service", "verify-relay-adapter-package", "verify-hosted-provider-runtime-package", "verify-post-release-download-package", "scaffold-evidence", "evidence-status", "scaffold-post-release-download", "post-release-evidence-status", "package-windows-temporary", "package-managed-mac-service", "package-linux-managed-service", "package-relay-adapter", "package-hosted-provider-runtime", "package-post-release-download", "release-evidence-index"},
		"adapter":          {"scaffold", "verify-result", "verify-lifecycle", "verify-cancellation", "verify-runtime"},
		"support-session":  {"connect", "handoff", "prepare", "start", "create", "plan", "status", "recover", "report", "audit-capabilities", "smoke-test", "live-e2e", "cleanup"},
	}
	for group, subcommands := range commands {
		for _, subcommand := range subcommands {
			if err := app.Run(context.Background(), []string{group, subcommand, "--help"}); err != nil && !strings.Contains(strings.ToLower(err.Error()), "help requested") {
				t.Fatalf("%s %s --help failed: %v", group, subcommand, err)
			}
		}
	}
}

func TestCLIEarlyValidationCoverage(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.supportSessionStart(context.Background(), supportSessionStartOptions{TTLSeconds: 1}); err == nil {
		t.Fatal("accepted invalid support-session start TTL")
	}
	if err := app.supportSessionConnect(context.Background(), supportSessionConnectOptions{TTLSeconds: 1}); err == nil {
		t.Fatal("accepted invalid support-session connect TTL")
	}
	if err := app.supportSessionCreate(context.Background(), supportSessionCreateOptions{}); err == nil {
		t.Fatal("accepted support-session create without gateway")
	}
	if err := app.supportSessionAuditCapabilities(context.Background(), supportSessionAuditCapabilitiesOptions{}); err == nil {
		t.Fatal("accepted capability audit without gateway")
	}
	for _, opts := range []any{
		supportSessionConnectOptions{GatewayURL: "http://127.0.0.1:1", TTLSeconds: 60},
		supportSessionCreateOptions{GatewayURL: "http://127.0.0.1:1", TTLSeconds: 60},
		supportSessionAuditCapabilitiesOptions{GatewayURL: "http://127.0.0.1:1", SessionID: "ses_coverage"},
	} {
		var err error
		switch value := opts.(type) {
		case supportSessionConnectOptions:
			err = app.supportSessionConnect(context.Background(), value)
		case supportSessionCreateOptions:
			err = app.supportSessionCreate(context.Background(), value)
		case supportSessionAuditCapabilitiesOptions:
			err = app.supportSessionAuditCapabilities(context.Background(), value)
		}
		if err == nil {
			t.Fatalf("network-dependent support-session path unexpectedly succeeded: %#v", opts)
		}
	}
	if err := app.hostServe(context.Background(), hostServeOptions{Mode: "invalid"}); err == nil {
		t.Fatal("accepted invalid host mode")
	}
	if err := app.hostServe(context.Background(), hostServeOptions{Mode: "temporary", FetchEnrollmentRevocations: true}); err == nil {
		t.Fatal("accepted revocation fetch without certificate")
	}
	if err := app.hostServe(context.Background(), hostServeOptions{Mode: "temporary", RenewEnrollmentCertificate: true, EnrollmentCertificatePath: "cert"}); err == nil {
		t.Fatal("accepted renewal without root")
	}
	if err := app.hostServe(context.Background(), hostServeOptions{Mode: "temporary", EnrollmentRootPublicKey: "root:key"}); err == nil {
		t.Fatal("accepted enrollment root without action")
	}
	operatorAuthPath := filepath.Join(t.TempDir(), "operator-auth.json")
	if err := app.operatorAuthInit(operatorAuthPath, filepath.Join(t.TempDir(), "operator-tokens"), false); err != nil {
		t.Fatal(err)
	}
	hostedPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostedAuthPath := filepath.Join(t.TempDir(), "hosted-auth.json")
	hostedAuth, err := json.Marshal(map[string]any{
		"schema_version": "rdev.hosted-operator-auth.v1",
		"issuer":         "https://issuer.example",
		"audience":       "rdev-gateway",
		"keys":           []map[string]string{{"key_id": "hosted-root", "public_key": base64.RawURLEncoding.EncodeToString(hostedPublic)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostedAuthPath, hostedAuth, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.gatewayServeDev(gatewayServeOptions{StorageProvider: "unknown"}); err == nil {
		t.Fatal("accepted unknown gateway storage provider")
	}
	if err := app.gatewayServeDev(gatewayServeOptions{StatePath: filepath.Join(t.TempDir(), "gateway.json")}); err == nil {
		t.Fatal("accepted persistent gateway state without signing key")
	}
	if err := app.gatewayServeDev(gatewayServeOptions{
		Addr:                   "[invalid",
		StatePath:              filepath.Join(t.TempDir(), "gateway.json"),
		SigningKeyPath:         filepath.Join(t.TempDir(), "gateway-key.json"),
		ManifestSigningKeyPath: filepath.Join(t.TempDir(), "manifest-key.json"),
		EnrollmentKeyPath:      filepath.Join(t.TempDir(), "enrollment-key.json"),
		AuditLog:               filepath.Join(t.TempDir(), "audit.jsonl"),
		OperatorAuthPath:       operatorAuthPath,
		HostedOperatorAuthPath: hostedAuthPath,
		RdevAssetsDir:          t.TempDir(),
		AutoBuildRdevAssets:    false,
	}); err == nil {
		t.Fatal("gateway server accepted an invalid listen address")
	}
	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"policy", "explain", "--capability", "shell.user"}); err != nil {
		t.Fatalf("policy explain failed: %v", err)
	}
	if err := app.Run(context.Background(), []string{"policy", "explain-shell", "--policy-json", `{"workspace_root":".","argv":["go","env"],"allow_commands":["go"]}`}); err != nil {
		t.Fatalf("policy explain-shell failed: %v", err)
	}
	if err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "file", "--path", filepath.Join(t.TempDir(), "gateway.json")}); err != nil {
		t.Fatalf("file gateway storage verification failed: %v", err)
	}
	for _, args := range [][]string{
		{"desktop", "screenshot", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage"},
		{"desktop", "record", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage"},
		{"desktop", "focus", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage"},
		{"desktop", "move", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage"},
		{"desktop", "input", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage", "--text", "hello"},
		{"desktop", "clipboard", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage", "--action", "write", "--text", "hello"},
		{"desktop", "url", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage", "--url", "https://example.test"},
		{"files", "list", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage"},
		{"files", "read", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage"},
		{"files", "write", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage", "--content", "hello"},
		{"files", "upload", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage", "--content", "hello"},
		{"files", "delete", "--gateway-url", "http://127.0.0.1:1", "--session-id", "ses_coverage"},
	} {
		if err := app.Run(context.Background(), args); err == nil {
			t.Fatalf("unavailable gateway command unexpectedly succeeded: %v", args)
		}
	}
}

func TestCLIGatewayConfigurationCoverage(t *testing.T) {
	if config, err := gatewayTLSClientConfig(hostServeOptions{}); err != nil || config != nil {
		t.Fatalf("default gateway client TLS config = %#v, err=%v", config, err)
	}
	if _, err := gatewayTLSClientConfig(hostServeOptions{GatewayClientCertPath: "cert.pem"}); err == nil {
		t.Fatal("incomplete gateway client mTLS configuration was accepted")
	}
	if _, err := gatewayTLSClientConfig(hostServeOptions{GatewayCACertPath: "missing-ca.pem"}); err == nil {
		t.Fatal("missing gateway client CA was accepted")
	}
	badCAPath := filepath.Join(t.TempDir(), "bad-ca.pem")
	if err := os.WriteFile(badCAPath, []byte("not-a-certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := gatewayTLSClientConfig(hostServeOptions{GatewayCACertPath: badCAPath}); err == nil {
		t.Fatal("invalid gateway client CA was accepted")
	}
	if config, err := gatewayTLSConfig(gatewayServeOptions{}); err != nil || config != nil {
		t.Fatalf("default gateway TLS config = %#v, err=%v", config, err)
	}
	if _, err := gatewayTLSConfig(gatewayServeOptions{ClientCAPath: "ca.pem"}); err == nil {
		t.Fatal("client CA without TLS certificates was accepted")
	}
	if _, err := gatewayTLSConfig(gatewayServeOptions{TLSCertPath: "cert.pem"}); err == nil {
		t.Fatal("incomplete TLS configuration was accepted")
	}
	assets := gatewayAssetConfig(gatewayServeOptions{
		RdevAssetsDir:                 "/assets",
		RdevBootstrapWindowsAMD64Path: "/custom/windows.exe",
		RdevBootstrapDarwinARM64Path:  "/custom/darwin-arm64",
		RdevBootstrapDarwinAMD64Path:  "/custom/darwin-amd64",
		RdevBootstrapLinuxAMD64Path:   "/custom/linux-amd64",
		RdevBootstrapLinuxARM64Path:   "/custom/linux-arm64",
	})
	if assets.RdevBootstrapWindowsAMD64Path != "/custom/windows.exe" || assets.RdevBootstrapDarwinARM64Path != "/custom/darwin-arm64" || assets.RdevBootstrapLinuxARM64Path != "/custom/linux-arm64" {
		t.Fatalf("gateway asset config = %#v", assets)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := listenAndServeGatewayContext(ctx, "127.0.0.1:0", http.NewServeMux(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled gateway server = %v", err)
	}
}

func TestCLISkillkitStableBundleWorkflow(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(t.TempDir(), "bundle")
	planDir := filepath.Join(t.TempDir(), "plan")
	targetDir := filepath.Join(t.TempDir(), "skills")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	for _, args := range [][]string{
		{"skillkit", "export", "--source-root", repoRoot, "--out", bundleDir},
		{"skillkit", "verify", "--bundle", bundleDir},
		{"skillkit", "plan-install", "--bundle", bundleDir, "--out", planDir, "--frameworks", "codex"},
		{"skillkit", "verify-install-plan", "--plan", filepath.Join(planDir, "install-plan.json")},
		{"skillkit", "install", "--bundle", bundleDir, "--framework", "codex", "--target", targetDir},
		{"skillkit", "install", "--bundle", bundleDir, "--framework", "codex", "--target", targetDir, "--execute", "--force"},
	} {
		if err := app.Run(context.Background(), args); err != nil {
			t.Fatalf("skillkit workflow %v failed: %v", args, err)
		}
	}
}

func TestCLIProtocolAndFileHelperCoverage(t *testing.T) {
	inviteTokenPath := filepath.Join(t.TempDir(), "operator-token")
	if err := os.WriteFile(inviteTokenPath, []byte("invite-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	inviteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer invite-token" {
			t.Fatalf("invite authorization header missing")
		}
		if r.URL.Path != "/v1/tickets" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ticket":{"code":"ABCD-1234","mode":"attended-temporary"},"joinUrl":"http://localhost:8787/join/ABCD-1234","manifestUrl":"http://127.0.0.1:8787/v1/tickets/ABCD-1234/manifest","manifestRootPublicKey":"root:key"}`))
	}))
	invite, err := createGatewayInviteTicket(context.Background(), inviteServer.Client(), inviteCreateOptions{
		GatewayURL:        inviteServer.URL,
		Mode:              model.HostModeAttendedTemporary,
		TTLSeconds:        600,
		OperatorTokenFile: inviteTokenPath,
	})
	inviteServer.Close()
	if err != nil || invite.Ticket.Code != "ABCD-1234" {
		t.Fatalf("invite payload = %#v, err=%v", invite, err)
	}
	badInviteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invite denied"}`))
	}))
	if _, err := createGatewayInviteTicket(context.Background(), badInviteServer.Client(), inviteCreateOptions{
		GatewayURL: badInviteServer.URL,
		Mode:       model.HostModeAttendedTemporary,
		TTLSeconds: 600,
	}); err == nil {
		t.Fatal("failed invite response was accepted")
	}
	badInviteServer.Close()

	noHostReport := supportSessionTicketReportWithoutSelectedHost("https://gateway", "TICKET", map[string]any{"active_hosts": 0}, 0)
	if noHostReport["ok"] != false || !strings.Contains(noHostReport["next_action"].(string), "No active") {
		t.Fatalf("no-host support report = %#v", noHostReport)
	}
	var responseOut bytes.Buffer
	if err := writeHTTPResponseJSON(&responseOut, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}); err != nil || !strings.Contains(responseOut.String(), `"ok": true`) {
		t.Fatalf("successful HTTP response forwarding = %q, err=%v", responseOut.String(), err)
	}
	if err := writeHTTPResponseJSON(io.Discard, &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"error":"bad"}`))}); err == nil {
		t.Fatal("HTTP error response was accepted")
	}
	serverExit := make(chan error, 1)
	serverExit <- errors.New("server failed")
	if err := waitForGatewayHealthOrServerExit(context.Background(), gatewayServerHandle{errCh: serverExit}, "", time.Millisecond); err == nil {
		t.Fatal("gateway server failure was hidden")
	}
	leaseSecret := strings.Repeat("l", 16)
	joinPayload, err := json.Marshal(map[string]any{
		"session":  controlplane.Session{ID: "ses_join"},
		"endpoint": controlplane.Endpoint{ID: "end_join"},
		"lease":    controlplane.Lease{Secret: leaseSecret},
		"events":   []controlplane.Event{{Type: controlplane.EventTypeHello}},
	})
	if err != nil {
		t.Fatal(err)
	}
	joinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(joinPayload)
	}))
	joined, endpoint, lease, events, err := joinSessionByCode(context.Background(), http.DefaultClient, joinServer.URL, "JOIN-CODE", controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget})
	joinServer.Close()
	if err != nil || joined.ID != "ses_join" || endpoint.ID != "end_join" || lease.Secret != leaseSecret || len(events) != 1 {
		t.Fatalf("joined session = %#v %#v %#v %#v, err=%v", joined, endpoint, lease, events, err)
	}
	manifestGateway := gateway.NewMemoryGateway()
	ticket, err := manifestGateway.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "manifest coverage")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manifestGateway.JoinManifest(ticket.Code, "https://gateway.example", "https://gateway.example/join/"+ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	manifestPayload, err := json.Marshal(map[string]any{"manifest": manifest})
	if err != nil {
		t.Fatal(err)
	}
	manifestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(manifestPayload) }))
	if fetched, err := fetchJoinManifest(context.Background(), http.DefaultClient, manifestServer.URL, "", ""); err != nil || fetched.TicketCode != ticket.Code {
		t.Fatalf("fetched manifest = %#v, err=%v", fetched, err)
	}
	manifestServer.Close()
	multiHostReport := supportSessionTicketReportWithoutSelectedHost("https://gateway", "TICKET", map[string]any{}, 2)
	if !strings.Contains(multiHostReport["next_action"].(string), "Multiple active") {
		t.Fatalf("multi-host support report = %#v", multiHostReport)
	}
	if got := manifestGatewayFallbackURLs([]model.JoinManifestGatewayCandidate{{URL: "https://one/"}, {URL: "https://one"}, {URL: "https://two"}}, "https://one"); len(got) != 1 || got[0] != "https://two" {
		t.Fatalf("manifest fallback URLs = %#v", got)
	}
	if responseGatewayTime(nil) != (time.Time{}) || responseGatewayTime(&http.Response{Header: http.Header{"Date": []string{"bad"}}}) != (time.Time{}) {
		t.Fatal("invalid gateway response time was accepted")
	}
	if responseGatewayTime(&http.Response{Header: http.Header{"Date": []string{"Mon, 02 Jan 2006 15:04:05 GMT"}}}).IsZero() {
		t.Fatal("valid gateway response time was not parsed")
	}
	if selectJoinManifestGatewayURL(context.Background(), http.DefaultClient, model.JoinManifest{GatewayURL: "https://fallback"}) != "https://fallback" {
		t.Fatal("manifest fallback gateway was not selected")
	}
	if _, err := fetchJoinManifest(context.Background(), http.DefaultClient, ":", "", ""); err == nil {
		t.Fatal("invalid join manifest URL was accepted")
	}
	if joinManifestGatewayReachable(context.Background(), http.DefaultClient, "http://127.0.0.1:1", "") {
		t.Fatal("unreachable manifest gateway was reported as reachable")
	}
	if got := selectJoinManifestGatewayURL(context.Background(), http.DefaultClient, model.JoinManifest{GatewayURL: "https://fallback", GatewayCandidates: []model.JoinManifestGatewayCandidate{{URL: "http://127.0.0.1:1"}}}); got != "https://fallback" {
		t.Fatalf("unreachable manifest candidate selected: %q", got)
	}
	transient := transientGatewayResponseError{Status: "503", Body: "busy", Cause: errors.New("retry")}
	if !isTransientGatewayResponseError(transient) || !strings.Contains(transient.Error(), "status=503") {
		t.Fatal("transient gateway error formatting changed")
	}
	if gatewayErrorMessage("500", []byte(`{"error":"denied"}`), nil) != "denied" || gatewayErrorMessage("500", []byte("  plain error  "), nil) != "plain error" || !strings.Contains(gatewayErrorMessage("500", nil, errors.New("cause")), "cause") {
		t.Fatal("gateway error message fallback changed")
	}
	arrayValues, arrayErr := parseJSONStringArray(`["a","b"]`, "values")
	matrixValues, matrixErr := parseJSONStringMatrix(`[["a"]]`, "values")
	if arrayErr != nil || matrixErr != nil || len(arrayValues) != 2 || len(matrixValues) != 1 {
		t.Fatal("JSON list helpers did not parse valid input")
	}
	if _, err := parseJSONStringArray("{", "values"); err == nil {
		t.Fatal("invalid JSON array was accepted")
	}
	if _, err := parseJSONStringMatrix("{", "values"); err == nil {
		t.Fatal("invalid JSON matrix was accepted")
	}
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(" token \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if token, err := readOptionalTokenFile(tokenPath); err != nil || token != "token" {
		t.Fatalf("optional token = %q, err=%v", token, err)
	}
	if token, err := readOptionalTokenFile(""); err != nil || token != "" {
		t.Fatalf("empty optional token = %q, err=%v", token, err)
	}
	if loadOperatorToken(tokenPath) != "token" {
		t.Fatal("operator token file loading changed")
	}
	contentPath := filepath.Join(t.TempDir(), "content")
	if err := os.WriteFile(contentPath, []byte("file content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if content, err := fileCommandContent("", contentPath); err != nil || content != "file content" {
		t.Fatalf("file command content = %q, err=%v", content, err)
	}
	if sessionEventLimit("long-poll") != 16 || sessionEventLimit("poll") != 64 {
		t.Fatal("session event limit changed")
	}
	if len(supportSessionRequiredAssets("windows")) != 1 || len(supportSessionRequiredAssets("macos")) != 2 || len(supportSessionRequiredAssets("linux")) != 2 || len(supportSessionRequiredAssets("unknown")) != 5 {
		t.Fatal("support-session asset selection changed")
	}
	for input, want := range map[string]string{"windows": "windows", "win": "windows", "macos": "macos", "darwin": "macos", "mac": "macos", "linux": "linux", "other": "auto"} {
		if normalizeSupportSessionTarget(input) != want {
			t.Fatalf("normalized target %q = %q, want %q", input, normalizeSupportSessionTarget(input), want)
		}
	}
	if isLocalDevGatewayURL("http://localhost") || isLocalDevGatewayURL("ftp://127.0.0.1:8787") || !isLocalDevGatewayURL("http://127.0.0.1:8787") {
		t.Fatal("local gateway URL classification changed")
	}
	if isLocalDevGatewayURL(":") || isSignedManifestGatewayURL("https://gateway.example", false) {
		t.Fatal("invalid or unsigned gateway URL classification changed")
	}
	if isPrivateOrLANHost("gateway.example") || !isPrivateOrLANHost("127.0.0.1") || !isPrivateOrLANHost("host.local") {
		t.Fatal("private/LAN gateway host classification changed")
	}
	if !isPrivateOrLANHost("10.0.0.8") || !isPrivateOrLANHost("::1") || !isPrivateOrLANHost("localhost") || isPrivateOrLANHost("not a host") || !isSignedManifestGatewayURL("https://gateway.example", true) || !isSignedManifestGatewayURL("http://10.0.0.8:8787", true) {
		t.Fatal("signed/private gateway URL classification changed")
	}
	if isSignedManifestGatewayURL("http://10.0.0.8", true) || isSignedManifestGatewayURL("http://gateway.example:8787", true) || isSignedManifestGatewayURL("https://", true) || isSignedManifestGatewayURL("ftp://gateway.example", true) {
		t.Fatal("incomplete or unsupported signed gateway URL was accepted")
	}
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", "https://stable.example")
	if continuity := supportSessionConnectionContinuity("https://stable.example"); continuity["stable_configured"] != true {
		t.Fatalf("stable gateway continuity = %#v", continuity)
	}
	if probe := probeGatewayAsset(context.Background(), http.DefaultClient, "", "asset.sha256"); probe["ok"] != false {
		t.Fatal("empty gateway asset probe unexpectedly succeeded")
	}
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	if probe := probeGatewayAsset(context.Background(), http.DefaultClient, assetServer.URL, "asset.sha256"); probe["ok"] != true {
		t.Fatalf("successful gateway asset probe = %#v", probe)
	}
	assetServer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(sleepOrDone(ctx, time.Hour), context.Canceled) {
		t.Fatal("sleepOrDone did not honor cancellation")
	}
	if foregroundGatewayHealthProbeURLs("http://127.0.0.1:1", "http://127.0.0.1:1") != nil || len(foregroundGatewayHealthProbeURLs("http://127.0.0.1:1", "https://gateway")) != 1 {
		t.Fatal("foreground gateway probe URL selection changed")
	}
	if err := waitForGatewayHealth(context.Background(), "", time.Millisecond); err == nil {
		t.Fatal("empty gateway health URL was accepted")
	}

	started := map[string]any{"target_handoff_envelope": map[string]any{"full_text": "handoff"}}
	handoffPath := filepath.Join(t.TempDir(), "handoff.txt")
	if err := writeSupportSessionHandoffTextFile0600(handoffPath, started); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(handoffPath); err != nil || string(data) != "handoff\n" {
		t.Fatalf("handoff file = %q, err=%v", data, err)
	}
	reportPath := filepath.Join(t.TempDir(), "report.txt")
	if err := writeSupportSessionConnectedReportFile0600(reportPath, map[string]any{"connected_next_steps": map[string]any{"user_report": "connected"}}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(reportPath); err != nil || string(data) != "connected\n" {
		t.Fatalf("connected report = %q, err=%v", data, err)
	}
	if allAcceptanceChecksPassed(nil) || !allAcceptanceChecksPassed([]acceptance.Check{{Passed: true}}) || allAcceptanceChecksPassed([]acceptance.Check{{Passed: false}}) {
		t.Fatal("acceptance check aggregation changed")
	}
	if connectionEntryChecksPassed(nil) || !connectionEntryChecksPassed([]connectionentry.Check{{Passed: true}}) || connectionEntryChecksPassed([]connectionentry.Check{{Passed: false}}) {
		t.Fatal("connection entry check aggregation changed")
	}
	if plist, err := servicePlistPath(hostServiceOptions{}); err != nil || plist == "" {
		t.Fatalf("service plist path = %q, err=%v", plist, err)
	}
	if unit, err := serviceUnitPath(hostServiceOptions{}); err != nil || unit == "" {
		t.Fatalf("service unit path = %q, err=%v", unit, err)
	}
	if plist, err := servicePlistPath(hostServiceOptions{Plist: "explicit.plist"}); err != nil || plist != "explicit.plist" {
		t.Fatalf("explicit plist path = %q, err=%v", plist, err)
	}
	if unit, err := serviceUnitPath(hostServiceOptions{Unit: "explicit.service"}); err != nil || unit != "explicit.service" {
		t.Fatalf("explicit unit path = %q, err=%v", unit, err)
	}
	if err := writeTextFile0600("", "text"); err == nil {
		t.Fatal("empty text output path was accepted")
	}
	for _, mode := range []string{"", "managed", "temporary", "attended-temporary", "break-glass"} {
		if _, err := parseEnrollmentHostMode(mode); err != nil {
			t.Fatalf("enrollment host mode %q failed: %v", mode, err)
		}
	}
	if _, err := parseEnrollmentHostMode("unsupported"); err == nil {
		t.Fatal("unsupported enrollment host mode was accepted")
	}
	if _, err := readTrustBundleFile(""); err == nil {
		t.Fatal("empty trust bundle path was accepted")
	}
	if _, err := readEnrollmentCertificateFile(""); err == nil {
		t.Fatal("empty enrollment certificate path was accepted")
	}
	if _, err := readEnrollmentRevocationListFile(""); err == nil {
		t.Fatal("empty revocation list path was accepted")
	}
	invalidPath := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustBundleFile(invalidPath); err == nil {
		t.Fatal("invalid trust bundle was accepted")
	}
	if _, err := readEnrollmentCertificateFile(invalidPath); err == nil {
		t.Fatal("invalid enrollment certificate was accepted")
	}
	if _, err := readEnrollmentRevocationListFile(invalidPath); err == nil {
		t.Fatal("invalid revocation list was accepted")
	}
	if err := writeTrustBundleFile("", model.SignedTrustBundle{}, false); err == nil {
		t.Fatal("empty trust bundle output path was accepted")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{BundleID: "cli-coverage", Sequence: 1, SigningKeyID: "cli-root", Keys: []model.TrustKey{model.NewTrustKey("cli-root", publicKey, model.TrustKeyStatusActive, time.Now())}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := unsigned.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "trust.json")
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePath, bundleJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if parsed, err := readTrustBundleFile(bundlePath); err != nil || parsed.BundleID != bundle.BundleID {
		t.Fatalf("read trust bundle = %#v, err=%v", parsed, err)
	}
	trustServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/trust" {
			_ = json.NewEncoder(w).Encode(map[string]any{"trust": model.NewTrustBundle("cli-root", publicKey)})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	if trust, err := fetchTrustBundle(context.Background(), http.DefaultClient, trustServer.URL, ""); err != nil || trust.SigningKeyID != "cli-root" {
		t.Fatalf("fetched trust bundle = %#v, err=%v", trust, err)
	}
	trustServer.Close()
	badTrustServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad trust"}`))
	}))
	if _, err := fetchTrustBundle(context.Background(), http.DefaultClient, badTrustServer.URL, ""); err == nil {
		t.Fatal("bad trust response was accepted")
	}
	badTrustServer.Close()
	if text, err := supportSessionHandoffText(map[string]any{"user_handoff": map[string]any{"message": "message", "copy_paste": "command"}}); err != nil || !strings.Contains(text, "message") || !strings.Contains(text, "command") {
		t.Fatalf("fallback handoff text = %q, err=%v", text, err)
	}
	if _, err := supportSessionHandoffText(map[string]any{}); err == nil {
		t.Fatal("empty handoff payload was accepted")
	}
	if splitCapabilities(" a, ,b ")[0] != "a" || len(splitCommaList("a,,b")) != 2 {
		t.Fatal("CSV capability helpers changed")
	}
	if domain, err := resolveLaunchctlDomain(context.Background(), "custom/domain"); err != nil || domain != "custom/domain" {
		t.Fatalf("custom launchctl domain = %q, err=%v", domain, err)
	}
	if domain, err := resolveLaunchctlDomain(context.Background(), "gui/$(id -u)"); err != nil || !strings.HasPrefix(domain, "gui/") {
		t.Fatalf("resolved launchctl domain = %q, err=%v", domain, err)
	}
	if _, err := runServiceCommand(context.Background(), nil); err == nil {
		t.Fatal("empty service command was accepted")
	}
	if result, err := runLaunchctl(context.Background(), []string{"sh", "-c", "printf ok"}); err != nil || result.ExitCode != 0 || result.Stdout != "ok" {
		t.Fatalf("successful service command = %#v, err=%v", result, err)
	}
	if result, err := runLaunchctl(context.Background(), []string{"sh", "-c", "exit 7"}); err == nil || result.ExitCode != 7 {
		t.Fatalf("failed service command = %#v, err=%v", result, err)
	}
	if results, err := runServiceCommands(context.Background(), [][]string{{"true"}, {"false"}, {"true"}}); err == nil || len(results) != 2 {
		t.Fatalf("service command sequence = %#v, err=%v", results, err)
	}
	if processExitCode(nil) != 0 || processExitCode(errors.New("generic")) != -1 {
		t.Fatal("service process exit code mapping changed")
	}
	if gatewayHasExplicitAssetConfig(gatewayServeOptions{}) || !gatewayHasExplicitAssetConfig(gatewayServeOptions{RdevAssetsDir: "assets"}) {
		t.Fatal("gateway asset configuration detection changed")
	}
	if stringValueFromAny(" value ") != "value" || stringValueFromAny(1) != "" || intValueFromAny(int64(2)) != 2 || intValueFromAny(float64(3)) != 3 || intValueFromAny(json.Number("4")) != 4 || intValueFromAny("5") != 0 {
		t.Fatal("generic value conversion changed")
	}
	t.Setenv("RDEV_OPERATOR_TOKEN", "env-token")
	if loadOperatorToken("") != "env-token" {
		t.Fatal("operator token environment fallback changed")
	}
	primary := tunnel.Candidate{ProviderID: "one", URL: "https://one"}
	if !publishedPrimaryRemains(tunnel.AvailabilitySet{Candidates: []tunnel.Candidate{primary}}, tunnel.AvailabilitySet{Candidates: []tunnel.Candidate{primary}}) || publishedPrimaryRemains(tunnel.AvailabilitySet{}, tunnel.AvailabilitySet{Candidates: []tunnel.Candidate{primary}}) {
		t.Fatal("published primary continuity changed")
	}
	if _, err := startPinggyTunnel(context.Background(), io.Discard, "8787", filepath.Join(t.TempDir(), "missing-known-hosts")); err == nil {
		t.Fatal("pinggy tunnel accepted missing known-hosts file")
	}
	degraded := availabilityWithoutCandidates(tunnel.AvailabilitySet{
		Candidates: []tunnel.Candidate{primary},
		Attempts:   []tunnel.Attempt{{ProviderID: "one", Status: tunnel.AttemptHealthy}},
	}, "probe_failed")
	if len(degraded.Candidates) != 0 || degraded.Attempts[0].Status != tunnel.AttemptDegraded || degraded.Attempts[0].ErrorClass != "probe_failed" {
		t.Fatalf("availability degradation = %#v", degraded)
	}
	manifestCandidates := manifestGatewayCandidatesFromRuntime([]tunnel.Candidate{{ProviderID: "one", URL: "https://one/"}, {ProviderID: "two"}})
	if len(manifestCandidates) != 1 || !manifestCandidates[0].Recommended || len(gatewayURLCandidatesFromManifest(manifestCandidates)) != 1 {
		t.Fatalf("manifest candidate conversion = %#v", manifestCandidates)
	}
}

func TestCLISupportSessionAuditFailureReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/ses_audit":
			_, _ = w.Write([]byte(`{"snapshot":{"endpoints":[{"id":"end_target","role":"target","platform":"linux/amd64"}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/ses_audit/tasks":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"task rejected"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer server.Close()

	report, err := runSupportSessionCapabilityAudit(context.Background(), supportSessionAuditCapabilitiesOptions{
		GatewayURL: server.URL,
		SessionID:  "ses_audit",
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	results, ok := report["results"].([]map[string]any)
	if !ok || report["ok"] != false || len(results) != len(auditCapabilityProbes("linux")) {
		t.Fatalf("audit failure report = %#v", report)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.supportSessionAuditCapabilities(context.Background(), supportSessionAuditCapabilitiesOptions{
		GatewayURL: server.URL,
		SessionID:  "ses_audit",
		Timeout:    time.Second,
	}); err == nil || stdout.Len() == 0 {
		t.Fatalf("audit command failure = %v, output=%q", err, stdout.String())
	}
}

func TestCLISupportSessionAuditSuccessReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/ses_audit_success":
			_, _ = w.Write([]byte(`{"snapshot":{"endpoints":[{"id":"end_target","role":"target","platform":"linux/amd64"}],"tasks":[{"id":"task_audit","status":"succeeded"}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/ses_audit_success/tasks":
			_, _ = w.Write([]byte(`{"task":{"id":"task_audit"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer server.Close()

	report, err := runSupportSessionCapabilityAudit(context.Background(), supportSessionAuditCapabilitiesOptions{
		GatewayURL:    server.URL,
		SessionID:     "ses_audit_success",
		RemoteControl: true,
		Timeout:       time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report["ok"] != true || report["remote_control_probe_count"] != 2 {
		t.Fatalf("audit success report = %#v", report)
	}
	results, ok := report["results"].([]map[string]any)
	if !ok || len(results) != 6 {
		t.Fatalf("audit success results = %#v", report["results"])
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.supportSessionSmokeTest(context.Background(), supportSessionSmokeTestOptions{
		GatewayURL:    server.URL,
		SessionID:     "ses_audit_success",
		TicketCode:    "TICKET",
		RemoteControl: true,
		Timeout:       time.Second,
	}); err != nil || stdout.Len() == 0 {
		t.Fatalf("smoke test report = %v, output=%q", err, stdout.String())
	}
}

func TestCLIArtifactPublicationLifecycle(t *testing.T) {
	if err := stageSupportSessionArtifacts([]*stagedSupportSessionArtifact{{label: "missing"}}); err == nil {
		t.Fatal("artifact without a path was accepted")
	}

	target := filepath.Join(t.TempDir(), "nested", "artifact.json")
	artifact := &stagedSupportSessionArtifact{path: target, label: "artifact", data: []byte("new")}
	if err := stageSupportSessionArtifacts([]*stagedSupportSessionArtifact{artifact}); err != nil {
		t.Fatal(err)
	}
	if artifact.tempPath == "" || !pathExists(artifact.tempPath) {
		t.Fatalf("staged artifact missing: %#v", artifact)
	}
	if err := prepareSupportSessionArtifactBackups([]*stagedSupportSessionArtifact{artifact}); err != nil {
		t.Fatal(err)
	}
	if err := commitSupportSessionArtifacts([]*stagedSupportSessionArtifact{artifact}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatalf("published artifact = %q, err=%v", data, err)
	}
	if err := finalizeSupportSessionArtifacts([]*stagedSupportSessionArtifact{artifact}); err != nil {
		t.Fatal(err)
	}

	oldTarget := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(oldTarget, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	rollback := &stagedSupportSessionArtifact{path: oldTarget, label: "rollback", data: []byte("replacement")}
	if err := prepareSupportSessionArtifactBackups([]*stagedSupportSessionArtifact{rollback}); err != nil {
		t.Fatal(err)
	}
	if !rollback.hadOriginal || rollback.backupPath == "" {
		t.Fatalf("backup was not prepared: %#v", rollback)
	}
	if err := stageSupportSessionArtifacts([]*stagedSupportSessionArtifact{rollback}); err != nil {
		t.Fatal(err)
	}
	if err := commitSupportSessionArtifacts([]*stagedSupportSessionArtifact{rollback}); err != nil {
		t.Fatal(err)
	}
	if err := restoreSupportSessionArtifacts([]*stagedSupportSessionArtifact{rollback}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(oldTarget); err != nil || string(data) != "old" {
		t.Fatalf("restored artifact = %q, err=%v", data, err)
	}
	if err := cleanupStagedSupportSessionArtifacts([]*stagedSupportSessionArtifact{rollback}); err != nil {
		t.Fatal(err)
	}
}

func TestCLIEnrollmentLifecycleFleetRenewalPlan(t *testing.T) {
	rootPublic, rootPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostPublicText := base64.RawURLEncoding.EncodeToString(hostPublic)
	hostSum := sha256.Sum256(hostPublic)
	now := time.Now().UTC()
	registration := model.HostRegistration{
		TicketCode:          "ABCD-1234",
		Name:                "coverage-host",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        []string{"shell.user"},
		IdentityKeyID:       "coverage-host-key",
		IdentityPublicKey:   hostPublicText,
		IdentityFingerprint: "sha256:" + hex.EncodeToString(hostSum[:]),
	}
	ticket := model.Ticket{Code: "ABCD-1234", Mode: model.HostModeManaged, Capabilities: []string{"shell.user"}}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, "coverage-root", rootPrivate, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certificatesPath := filepath.Join(t.TempDir(), "certificates.json")
	certificatesJSON, err := json.Marshal([]model.HostEnrollmentCertificate{certificate})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certificatesPath, certificatesJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList(nil, "coverage-root", rootPrivate, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	revocationsPath := filepath.Join(t.TempDir(), "revocations.json")
	revocationsJSON, err := json.Marshal(revocations)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(revocationsPath, revocationsJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	rootPublicKey := "coverage-root:" + base64.RawURLEncoding.EncodeToString(rootPublic)
	outPath := filepath.Join(t.TempDir(), "renewal-plan.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.enrollmentLifecycleFleetRenewalPlan(certificatesPath, revocationsPath, rootPublicKey, time.Hour, 24*time.Hour, time.Minute, true, outPath, false); err != nil {
		t.Fatal(err)
	}
	if !pathExists(outPath) || !strings.Contains(stdout.String(), "renewal-plan") {
		t.Fatalf("renewal plan output missing: path=%v stdout=%q", pathExists(outPath), stdout.String())
	}
}

func TestCLIServicePlanSurfaces(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	plistPath := filepath.Join(t.TempDir(), "rdev.plist")
	unitPath := filepath.Join(t.TempDir(), "rdev.service")
	if err := app.hostInstallService(hostInstallServiceOptions{Platform: "unknown", BinaryPath: "/usr/local/bin/rdev"}); err == nil {
		t.Fatal("unknown service platform was accepted")
	}
	if err := app.hostInstallService(hostInstallServiceOptions{
		Platform:   "macos",
		Label:      "com.example.rdev.coverage",
		BinaryPath: "/usr/local/bin/rdev",
		GatewayURL: "https://gateway.example",
		TicketCode: "ABCD-1234",
		PlistOut:   plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostInstallService(hostInstallServiceOptions{
		Platform:   "linux",
		Label:      "rdev.service",
		BinaryPath: "/usr/local/bin/rdev",
		GatewayURL: "https://gateway.example",
		TicketCode: "ABCD-1234",
		UnitOut:    unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := app.hostInstallService(hostInstallServiceOptions{
		Platform:   "windows",
		Label:      "RdevCoverage",
		BinaryPath: `C:\\rdev.exe`,
		GatewayURL: "https://gateway.example",
		TicketCode: "ABCD-1234",
	}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := app.hostWindowsServiceStatus(hostServiceOptions{Label: "RdevCoverage"}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostMacOSServiceStatus(hostServiceOptions{Platform: "macos", Label: "com.example.rdev.coverage", Plist: plistPath}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostLinuxSystemdServiceStatus(hostServiceOptions{Platform: "linux", Label: "rdev.service", Unit: unitPath}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostMacOSServiceControl(context.Background(), hostServiceControlOptions{Platform: "macos", Action: "start", Label: "com.example.rdev.coverage", Plist: plistPath}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostLinuxSystemdServiceControl(context.Background(), hostServiceControlOptions{Platform: "linux", Action: "start", Label: "rdev.service", Unit: unitPath}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostServiceStatus(hostServiceOptions{Platform: "macos", Label: "com.example.rdev.coverage", Plist: plistPath}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostServiceStatus(hostServiceOptions{Platform: "linux", Label: "rdev.service", Unit: unitPath}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostServiceStatus(hostServiceOptions{Platform: "windows", Label: "RdevCoverage"}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostServiceControl(context.Background(), hostServiceControlOptions{Platform: "windows", Action: "start", Label: "RdevCoverage"}); err != nil {
		t.Fatal(err)
	}
	if err := app.hostServiceStatus(hostServiceOptions{Platform: "unknown"}); err == nil {
		t.Fatal("unknown service status platform was accepted")
	}
	if err := app.hostServiceControl(context.Background(), hostServiceControlOptions{Platform: "unknown", Action: "start"}); err == nil {
		t.Fatal("unknown service control platform was accepted")
	}
	if err := app.hostWindowsServiceControl(context.Background(), hostServiceControlOptions{}); err == nil {
		t.Fatal("empty Windows service action was accepted")
	}
	stdout.Reset()
	if err := app.hostWindowsServiceControl(context.Background(), hostServiceControlOptions{Action: "start", Label: "RdevCoverage"}); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("Windows service control emitted no plan")
	}
	if err := app.hostWindowsServiceControl(context.Background(), hostServiceControlOptions{Action: "start", Label: "RdevCoverage", Execute: true}); err == nil {
		t.Fatal("Windows service execute unexpectedly succeeded on the test host")
	}
}

func TestCLIHostServeJoinsOnce(t *testing.T) {
	rootPublic, rootPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSum := sha256.Sum256(hostPublic)
	registration := model.HostRegistration{
		TicketCode:          "ABCD-1234",
		Name:                "enrolled-host",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        []string{"shell.user"},
		IdentityKeyID:       "enrolled-host-key",
		IdentityPublicKey:   base64.RawURLEncoding.EncodeToString(hostPublic),
		IdentityFingerprint: "sha256:" + hex.EncodeToString(hostSum[:]),
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary, Capabilities: []string{"shell.user"}}, "enrollment-root", rootPrivate, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList(nil, "enrollment-root", rootPrivate, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certificatePath := filepath.Join(t.TempDir(), "enrollment.json")
	revocationsPath := filepath.Join(t.TempDir(), "revocations.json")
	certificateJSON, _ := json.Marshal(certificate)
	revocationsJSON, _ := json.Marshal(revocations)
	if err := os.WriteFile(certificatePath, certificateJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(revocationsPath, revocationsJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	rootPublicKey := "enrollment-root:" + base64.RawURLEncoding.EncodeToString(rootPublic)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/enrollment/revocations" {
			_ = json.NewEncoder(w).Encode(map[string]any{"revocations": revocations})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/session-joins" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session":{"id":"ses_host"},"endpoint":{"id":"end_host"},"lease":{"secret":"lease_host"},"events":[]}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.hostServe(context.Background(), hostServeOptions{
		Mode:                       "temporary",
		GatewayURL:                 server.URL,
		TicketCode:                 "ABCD-1234",
		IdentityStorePath:          filepath.Join(t.TempDir(), "identity.json"),
		EnrollmentCertificatePath:  certificatePath,
		EnrollmentRootPublicKey:    rootPublicKey,
		FetchEnrollmentRevocations: true,
		Once:                       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "session-joined") || !strings.Contains(stdout.String(), "ses_host") {
		t.Fatalf("host serve output = %q", stdout.String())
	}
}

func TestCLIRunSessionTasksProcessesOffer(t *testing.T) {
	_, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trustPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	task := controlplane.Task{
		ID:               "task_run",
		SessionID:        "ses_run",
		TargetEndpointID: "end_run",
		Adapter:          "shell",
		Intent:           "coverage shell task",
		Capabilities:     []string{"shell.user"},
		Payload: map[string]any{
			"workspace_root": workspace,
			"write_scope":    []string{"."},
			"argv":           []string{"sh", "-c", "printf ok"},
			"allow_commands": []string{"sh"},
		},
		Limits: map[string]any{
			"max_duration_seconds": 10,
			"max_output_bytes":     1000,
			"network":              "default-deny",
		},
		Status: controlplane.TaskStatusQueued,
	}
	event := controlplane.Event{Seq: 1, Type: controlplane.EventTypeTask, TaskID: task.ID, Payload: map[string]any{"action": "offer"}}
	legacyTrust := model.NewTrustBundle("coverage-root", trustPublic)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/trust-bundle":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"signed trust unavailable"}`))
		case r.URL.Path == "/v1/trust":
			_ = json.NewEncoder(w).Encode(map[string]any{"trust": legacyTrust})
		case r.URL.Path == "/v1/sessions/ses_run/events":
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []controlplane.Event{event}})
		case r.URL.Path == "/v1/sessions/ses_run":
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshot": map[string]any{"tasks": []controlplane.Task{task}}})
		case r.URL.Path == "/v1/sessions/ses_run/tasks/task_run/result":
			_ = json.NewEncoder(w).Encode(map[string]any{"task": task, "event": event})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	processed, err := app.runSessionTasks(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		Transport:    "poll",
		MaxTasks:     1,
		PollInterval: time.Millisecond,
	}, http.DefaultClient, "ses_run", "end_run", "sha256:identity", "lease", controlplane.Lease{Secret: "lease"})
	if err != nil || processed != 1 {
		t.Fatalf("processed tasks = %d, err=%v, output=%q", processed, err, stdout.String())
	}
	if err := app.runSessionTask(context.Background(), hostServeOptions{
		GatewayURL:           server.URL,
		CapabilityCeiling:    []string{"fs.read"},
		CapabilityCeilingSet: true,
	}, http.DefaultClient, "ses_run", "end_run", "sha256:identity", "lease", task); err != nil {
		t.Fatalf("capability-ceiling task result = %v", err)
	}
}

func TestCLISupportSessionStatusWaitAndRecover(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/support-session/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"connected":false,"status":"waiting","stale_hosts":[{"id":"host_old","status":"stale","name":"old","os":"linux","arch":"amd64"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"connected":true,"status":"connected","stale_hosts":[]}`))
	}))
	defer server.Close()

	status, err := supportSessionStatus(context.Background(), http.DefaultClient, supportSessionStatusOptions{
		GatewayURL: server.URL,
		TicketCode: "ABCD-1234",
		Wait:       true,
		Interval:   time.Millisecond,
		Timeout:    time.Second,
	})
	if err != nil || status["connected"] != true {
		t.Fatalf("waited status = %#v, err=%v", status, err)
	}
	requests = 0
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.supportSessionRecover(context.Background(), supportSessionRecoverOptions{GatewayURL: server.URL, TicketCode: "ABCD-1234"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "host_old") || !strings.Contains(stdout.String(), "support-session-recovery") {
		t.Fatalf("recovery output = %q", stdout.String())
	}
	timeoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"connected":false,"status":"waiting"}`))
	}))
	defer timeoutServer.Close()
	timedOut, err := supportSessionStatus(context.Background(), http.DefaultClient, supportSessionStatusOptions{
		GatewayURL: timeoutServer.URL,
		TicketCode: "ABCD-1234",
		Wait:       true,
		Interval:   time.Millisecond,
		Timeout:    time.Millisecond,
	})
	if err != nil || timedOut["timed_out"] != true {
		t.Fatalf("timed out status = %#v, err=%v", timedOut, err)
	}
}

func TestCLIRebuildStartedSupportSession(t *testing.T) {
	gatewayStore := gateway.NewMemoryGateway()
	ticket, err := gatewayStore.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "coverage")
	if err != nil {
		t.Fatal(err)
	}
	started := map[string]any{
		"session": map[string]any{
			"target":                     "windows",
			"locale":                     "zh-CN",
			"auto_activate":              true,
			"manifest_root_public_key":   "root:key",
			"watch_connection_status":    []string{"rdev-custom"},
			"target_handoff_envelope":    map[string]any{"full_text": "connect"},
			"target_bootstrap_readiness": map[string]any{"ready": true},
		},
		"gateway": map[string]any{"addr": "127.0.0.1:8787", "work_dir": "/tmp/rdev"},
	}
	live := tunnel.AvailabilitySet{Candidates: []tunnel.Candidate{{ProviderID: "cloudflare", URL: "https://edge.example/"}}}
	startedResult, err := rebuildStartedSupportSession(foregroundSupportSessionOptions{
		Gateway:   gatewayStore,
		TicketID:  ticket.ID,
		Started:   started,
		ReadyFile: "/tmp/ready.json",
	}, live, []model.JoinManifestGatewayCandidate{{URL: "https://edge.example", Kind: "cloudflare", Scope: "public", Recommended: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(startedResult) == 0 {
		t.Fatalf("rebuilt started session = %#v", startedResult)
	}
}

func TestCLIRefreshPublishedSupportSession(t *testing.T) {
	gatewayStore := gateway.NewMemoryGateway()
	ticket, err := gatewayStore.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "coverage")
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingStateStore{}
	started := map[string]any{
		"session":                 map[string]any{"target": "windows", "locale": "en"},
		"gateway":                 map[string]any{"addr": "127.0.0.1:8787", "work_dir": t.TempDir()},
		"target_handoff_envelope": map[string]any{"full_text": "connect"},
	}
	root := t.TempDir()
	refreshed, err := refreshPublishedSupportSession(foregroundSupportSessionOptions{
		Out:             io.Discard,
		StatusFile:      filepath.Join(root, "status.json"),
		ReadyFile:       filepath.Join(root, "ready.json"),
		HandoffTextFile: filepath.Join(root, "handoff.txt"),
		JournalPath:     filepath.Join(root, "journal.json"),
		Gateway:         gatewayStore,
		Store:           store,
		TicketID:        ticket.ID,
		Started:         started,
	}, tunnel.AvailabilitySet{Candidates: []tunnel.Candidate{{ProviderID: "cloudflare", URL: "https://edge.example"}}})
	if err != nil || len(refreshed) == 0 {
		t.Fatalf("refreshed started session = %#v, err=%v", refreshed, err)
	}
}

func TestCLIAuthorityInitializationSurfaces(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	authPath := filepath.Join(t.TempDir(), "operator-auth.json")
	tokenDir := filepath.Join(t.TempDir(), "tokens")
	if err := app.operatorAuthInit(authPath, tokenDir, false); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := app.operatorAuthVerify(authPath); err != nil || stdout.Len() == 0 {
		t.Fatalf("operator auth verify = %v, output=%q", err, stdout.String())
	}

	rootKey := filepath.Join(t.TempDir(), "root-key.json")
	gatewayKey := filepath.Join(t.TempDir(), "gateway-key.json")
	trustPath := filepath.Join(t.TempDir(), "trust.json")
	rotatedPath := filepath.Join(t.TempDir(), "trust-rotated.json")
	stdout.Reset()
	if err := app.trustInit(trustInitOptions{
		OutPath:      trustPath,
		RootKeyPath:  rootKey,
		RootKeyID:    "coverage-root",
		GatewayPath:  gatewayKey,
		GatewayKeyID: "coverage-gateway",
		BundleID:     "coverage-trust",
		ValidHours:   24,
	}); err != nil {
		t.Fatal(err)
	}
	var trustSummary map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &trustSummary); err != nil {
		t.Fatal(err)
	}
	rootPublicKey, _ := trustSummary["root_public_key"].(string)
	stdout.Reset()
	if err := app.trustRotate(trustRotateOptions{
		CurrentPath:  trustPath,
		OutPath:      rotatedPath,
		RootKeyPath:  rootKey,
		GatewayPath:  filepath.Join(t.TempDir(), "gateway-key-2.json"),
		GatewayKeyID: "coverage-gateway-2",
		ValidHours:   24,
		RetireKeyIDs: []string{"coverage-gateway"},
	}); err != nil {
		t.Fatal(err)
	}
	if !pathExists(rotatedPath) || stdout.Len() == 0 {
		t.Fatalf("rotated trust bundle missing: path=%v output=%q", pathExists(rotatedPath), stdout.String())
	}
	revokedPath := filepath.Join(t.TempDir(), "trust-revoked.json")
	stdout.Reset()
	if err := app.trustRevoke(trustRevokeOptions{
		CurrentPath: rotatedPath,
		OutPath:     revokedPath,
		RootKeyPath: rootKey,
		KeyID:       "coverage-gateway-2",
		Reason:      "coverage",
		ValidHours:  24,
	}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := app.trustVerify(revokedPath, rootPublicKey); err != nil {
		t.Fatal(err)
	}
}

func TestCLIOperatorAuthSAMLConfigurationSurface(t *testing.T) {
	hostedPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostedPath := filepath.Join(t.TempDir(), "hosted.json")
	hostedJSON, err := json.Marshal(map[string]any{
		"schema_version": "rdev.hosted-operator-auth.v1",
		"issuer":         "https://issuer.example",
		"audience":       "rdev-gateway",
		"keys":           []map[string]string{{"key_id": "hosted", "public_key": base64.RawURLEncoding.EncodeToString(hostedPublic)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostedPath, hostedJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rdev-saml-coverage"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	authPath := filepath.Join(t.TempDir(), "saml.json")
	authJSON, err := json.Marshal(map[string]any{
		"schema_version":         "rdev.saml-operator-auth.v1",
		"idp_issuer":             "https://idp.example",
		"audience":               "rdev-gateway",
		"assertion_consumer_url": "https://gateway.example/saml/acs",
		"role_attribute":         "Role",
		"certificate_pem":        string(certPEM),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, authJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.operatorAuthVerifyHosted(hostedPath); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := app.operatorAuthVerifySAML(authPath, "", ""); err != nil || stdout.Len() == 0 {
		t.Fatalf("SAML auth verification plan = %v, output=%q", err, stdout.String())
	}
	emptyResponse := filepath.Join(t.TempDir(), "empty-response")
	if err := os.WriteFile(emptyResponse, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.operatorAuthVerifySAML(authPath, emptyResponse, ""); err == nil {
		t.Fatal("empty SAML response was accepted")
	}
	badResponse := filepath.Join(t.TempDir(), "bad-response")
	if err := os.WriteFile(badResponse, []byte("not-a-saml-response"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.operatorAuthVerifySAML(authPath, badResponse, ""); err == nil {
		t.Fatal("invalid SAML response was accepted")
	}
}
