package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
)

func TestVersionAndDoctorExposeSessionRuntime(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"version", "--json"}); err != nil {
		t.Fatal(err)
	}
	var version map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	if version["session_protocol_only"] != true || version["name"] != "rdev" {
		t.Fatalf("unexpected version payload: %#v", version)
	}

	stdout.Reset()
	if err := app.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	var doctor map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doctor); err != nil {
		t.Fatal(err)
	}
	if doctor["protocol"] != "rdev.session.v1" || doctor["host_capabilities"] == nil {
		t.Fatalf("unexpected doctor payload: %#v", doctor)
	}
}

func TestMCPToolsExposeSessionControlPlane(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"mcp", "tools"}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"rdev.sessions.create":    true,
		"rdev.sessions.status":    true,
		"rdev.sessions.events":    true,
		"rdev.sessions.task":      true,
		"rdev.sessions.interrupt": true,
		"rdev.sessions.artifacts": true,
		"rdev.sessions.close":     true,
	}
	for _, tool := range payload.Tools {
		delete(want, tool.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing session tools: %#v", want)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	err := NewApp(&bytes.Buffer{}, &bytes.Buffer{}).Run(context.Background(), []string{"unknown"})
	if err == nil {
		t.Fatal("unknown command was accepted")
	}
}

func TestGatewayRequiresLoopbackAddress(t *testing.T) {
	if err := requireLoopbackAddress("127.0.0.1:8787"); err != nil {
		t.Fatalf("loopback address rejected: %v", err)
	}
	if err := requireLoopbackAddress("0.0.0.0:8787"); err == nil {
		t.Fatal("public gateway address accepted")
	}
}

func TestAppUsageAndGatewayServeExposeCurrentSurface(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "session-native remote development toolkit") {
		t.Fatalf("top-level usage = %q", stdout.String())
	}
	for _, args := range [][]string{{"mcp", "--help"}, {"host", "--help"}, {"gateway", "--help"}} {
		stdout.Reset()
		if err := app.Run(context.Background(), args); err != nil {
			t.Fatalf("%v help: %v", args[0], err)
		}
		if !strings.Contains(stdout.String(), "rdev "+args[0]) {
			t.Fatalf("%v help = %q", args[0], stdout.String())
		}
	}

	stdout.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Run(ctx, []string{"gateway", "serve", "--addr", "127.0.0.1:0", "--dev"}); err != nil {
		t.Fatalf("gateway serve with canceled context: %v", err)
	}
	var gatewayReady map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &gatewayReady); err != nil {
		t.Fatal(err)
	}
	if gatewayReady["protocol"] != "rdev.session.v1" || gatewayReady["mode"] != "development" || gatewayReady["operator_auth_enabled"] != false {
		t.Fatalf("gateway ready payload = %#v", gatewayReady)
	}

	authPath := t.TempDir() + "/operators.json"
	if err := operatorauth.WriteFile(authPath, operatorauth.File{
		SchemaVersion: operatorauth.SchemaVersion,
		HashAlg:       "sha256",
		Principals: []operatorauth.Principal{{
			ID:        "operator",
			Roles:     []string{operatorauth.RoleOperator},
			TokenHash: operatorauth.HashToken("fixture-operator-token"),
		}},
	}, false); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := app.Run(ctx, []string{"gateway", "serve", "--addr", "127.0.0.1:0", "--dev", "--operator-auth-file", authPath}); err != nil {
		t.Fatalf("authenticated gateway serve with canceled context: %v", err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &gatewayReady); err != nil {
		t.Fatal(err)
	}
	if gatewayReady["operator_auth_enabled"] != true {
		t.Fatalf("authenticated gateway ready payload = %#v", gatewayReady)
	}

	stdout.Reset()
	if err := app.Run(context.Background(), []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout.String(), "rdev ") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestCurrentCLIRejectsInvalidMCPAndGatewayCalls(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	for _, args := range [][]string{
		{"mcp"},
		{"mcp", "unknown"},
		{"mcp", "serve", "--gateway-url", "https://gateway.example", "--operator-token-file", t.TempDir() + "/missing"},
		{"mcp", "serve", "--unknown-flag"},
		{"gateway"},
		{"gateway", "unknown"},
		{"gateway", "serve", "--addr", "0.0.0.0:8787"},
		{"gateway", "serve", "--operator-auth-file", t.TempDir() + "/missing"},
	} {
		if err := app.Run(context.Background(), args); err == nil {
			t.Fatalf("%v was accepted", args)
		}
	}
}

func TestGatewayHandlerLoadsOperatorAuthFile(t *testing.T) {
	authPath := t.TempDir() + "/operators.json"
	if err := operatorauth.WriteFile(authPath, operatorauth.File{
		SchemaVersion: operatorauth.SchemaVersion,
		HashAlg:       "sha256",
		Principals: []operatorauth.Principal{{
			ID:        "operator",
			Roles:     []string{operatorauth.RoleOperator},
			TokenHash: operatorauth.HashToken("fixture-operator-token"),
		}},
	}, false); err != nil {
		t.Fatal(err)
	}
	handler, enabled, err := gatewayHandler(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("operator auth was not reported as enabled")
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated session create status = %d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/sessions", nil)
	request.Header.Set("Authorization", "Bearer fixture-operator-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("authenticated empty session create status = %d, want %d", response.Code, http.StatusBadRequest)
	}

	localHandler, localAuthEnabled, err := gatewayHandler("")
	if err != nil {
		t.Fatal(err)
	}
	if localAuthEnabled {
		t.Fatal("local gateway unexpectedly reported operator auth")
	}
	response = httptest.NewRecorder()
	localHandler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/sessions", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("local empty session create status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestReadProtectedTokenFile(t *testing.T) {
	if token, err := readProtectedTokenFile(""); err != nil || token != "" {
		t.Fatalf("empty token path = %q, %v", token, err)
	}
	path := t.TempDir() + "/operator-token"
	if err := os.WriteFile(path, []byte("\nfixture-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if token, err := readProtectedTokenFile(path); err != nil || token != "fixture-token" {
		t.Fatalf("token file = %q, %v", token, err)
	}
	if err := os.WriteFile(path, []byte("\n	"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtectedTokenFile(path); err == nil {
		t.Fatal("empty token file was accepted")
	}
	if _, err := readProtectedTokenFile(path + ".missing"); err == nil {
		t.Fatal("missing token file was accepted")
	}
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
