package mcpstdio

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestServerInitializeAndToolsListExposesCurrentSessionProtocol(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(lines))
	}
	if lines[0]["error"] != nil {
		t.Fatalf("initialize failed: %v", lines[0]["error"])
	}
	if lines[0]["result"].(map[string]any)["protocolVersion"] != "2025-11-25" {
		t.Fatalf("unexpected protocol version: %#v", lines[0])
	}
	result, ok := lines[1]["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list result is not an object: %#v", lines[1])
	}
	rawTools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list tools is not an array: %#v", result)
	}
	if len(rawTools) != len(contracts.Tools()) {
		t.Fatalf("tools/list count=%d, contract count=%d", len(rawTools), len(contracts.Tools()))
	}
	for index, rawTool := range rawTools {
		tool := rawTool.(map[string]any)
		if tool["name"] != contracts.Tools()[index].Name {
			t.Fatalf("tools/list tool %d=%v, contract=%s", index, tool["name"], contracts.Tools()[index].Name)
		}
	}
}

func TestServerRejectsUnknownSessionTool(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rdev.sessions.unknown","arguments":{}}}` + "\n"
	var out bytes.Buffer
	if err := NewServer(gateway.NewMemoryGateway()).Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if len(lines) != 1 {
		t.Fatalf("responses = %#v", lines)
	}
	errPayload, ok := lines[0]["error"].(map[string]any)
	if !ok || errPayload["code"] != float64(-32602) || !strings.Contains(errPayload["message"].(string), "unknown tool") {
		t.Fatalf("unknown tool response = %#v", lines[0])
	}
}

func TestRemoteGatewayConfigurationHandlesInvalidAndDefaultClients(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	if server.remoteClient() != http.DefaultClient {
		t.Fatal("local MCP server did not use the default HTTP client")
	}
	remote := NewServerWithRemoteGateway(gateway.NewMemoryGateway(), "http://[::1")
	if remote.RemoteGateway != "http://[::1" {
		t.Fatalf("invalid remote gateway URL was unexpectedly rewritten: %q", remote.RemoteGateway)
	}
}

func TestProxyPOSTToRetriesSessionTaskWithIdempotencyKey(t *testing.T) {
	attempts := 0
	keys := []string{}
	server := NewServer(gateway.NewMemoryGateway())
	server.httpClient = &http.Client{Transport: retryingMCPTransport{
		MaxRetries: 2,
		Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			keys = append(keys, req.Header.Get("Idempotency-Key"))
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), `"idempotency_key":"task-1"`) {
				t.Fatalf("unexpected body on attempt %d: %s", attempts, string(body))
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
		}),
	}}
	result, err := server.proxyPOSTTo("http://example.test", "/v1/sessions/sess_1/tasks", map[string]any{
		"adapter":         "shell",
		"intent":          "demo",
		"idempotency_key": "task-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || attempts != 2 {
		t.Fatalf("expected retry result after two attempts, result=%#v attempts=%d", result, attempts)
	}
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("expected stable idempotency key across retries, got %#v", keys)
	}
}

func TestRemoteGatewayForwardsBearerOnlyToConfiguredGateway(t *testing.T) {
	configuredAuthorization := []string{}
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configuredAuthorization = append(configuredAuthorization, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer configured.Close()

	overrideAuthorization := ""
	override := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		overrideAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer override.Close()

	server := NewServerWithRemoteGatewayAndOperatorToken(gateway.NewMemoryGateway(), configured.URL, "operator-secret")
	if _, err := server.proxyPOSTTo(configured.URL, "/v1/sessions", map[string]any{"reason": "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.proxyGETTo(configured.URL, "/v1/sessions/session_1"); err != nil {
		t.Fatal(err)
	}
	if len(configuredAuthorization) != 2 {
		t.Fatalf("expected two configured gateway requests, got %#v", configuredAuthorization)
	}
	for _, authorization := range configuredAuthorization {
		if authorization != "Bearer operator-secret" {
			t.Fatalf("configured gateway did not receive bearer token: %#v", configuredAuthorization)
		}
	}
	if _, err := server.createSession(map[string]any{
		"reason":      "same endpoint override",
		"gateway_url": configured.URL,
	}); err != nil {
		t.Fatal(err)
	}
	if len(configuredAuthorization) != 3 || configuredAuthorization[2] != "" {
		t.Fatalf("operator bearer token leaked to same-endpoint per-call override: %#v", configuredAuthorization)
	}

	if _, err := server.proxyPOSTTo(override.URL, "/v1/sessions", map[string]any{"reason": "override"}); err != nil {
		t.Fatal(err)
	}
	if overrideAuthorization != "" {
		t.Fatalf("operator bearer token leaked to per-call gateway override: %q", overrideAuthorization)
	}
}

func TestRemoteGatewayWithOperatorTokenDoesNotFollowRedirects(t *testing.T) {
	redirectTargetCalls := 0
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer redirectTarget.Close()

	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", redirectTarget.URL+"/v1/sessions/session_1")
		w.WriteHeader(http.StatusFound)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer configured.Close()

	server := NewServerWithRemoteGatewayAndOperatorToken(gateway.NewMemoryGateway(), configured.URL, "operator-secret")
	if _, err := server.proxyGETTo(configured.URL, "/v1/sessions/session_1"); err == nil {
		t.Fatal("operator-authenticated remote request should reject redirect")
	}
	if redirectTargetCalls != 0 {
		t.Fatalf("operator-authenticated request followed redirect %d times", redirectTargetCalls)
	}
}

func TestMCPArgumentAndProtocolHelpers(t *testing.T) {
	if got := requiredString(map[string]any{"value": "ok"}, "value"); got != "ok" {
		t.Fatal(got)
	}
	for _, args := range []map[string]any{nil, map[string]any{"value": 3}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("requiredString(%#v) should panic", args)
				}
			}()
			_ = requiredString(args, "missing")
		}()
	}
	if intArg(map[string]any{"value": float64(3)}, "value", 0) != 3 || intArg(map[string]any{"value": 4}, "value", 0) != 4 || intArg(nil, "value", 9) != 9 {
		t.Fatal("intArg branches failed")
	}
	if !boolArg(map[string]any{"value": true}, "value", false) || boolArg(map[string]any{"value": "true"}, "value", false) {
		t.Fatal("boolArg branches failed")
	}
	if objectArg(map[string]any{"value": map[string]any{"ok": true}}, "value")["ok"] != true || len(objectArg(map[string]any{"value": "invalid"}, "value")) != 0 {
		t.Fatal("objectArg branches failed")
	}

	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())
	input := strings.Join([]string{
		`not-json`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"not/method"}`,
		"",
	}, "\n")
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if len(lines) != 2 || lines[0]["error"].(map[string]any)["code"] != float64(-32700) || lines[1]["error"].(map[string]any)["code"] != float64(-32601) {
		t.Fatalf("unexpected protocol error responses: %#v", lines)
	}
}

func TestRemoteGatewayNormalizesV1BaseURLBeforeProxying(t *testing.T) {
	path := ""
	selectedGateway := ""
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		selectedGateway, _ = request["selected_gateway_url"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer configured.Close()

	server := NewServerWithRemoteGateway(gateway.NewMemoryGateway(), configured.URL+"/v1")
	if _, err := server.createSession(map[string]any{"reason": "normalize gateway base"}); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/sessions" {
		t.Fatalf("gateway API path = %q, want /v1/sessions", path)
	}
	if selectedGateway != configured.URL {
		t.Fatalf("selected gateway = %q, want %q", selectedGateway, configured.URL)
	}
}

func ptrStrings(values []string) *[]string {
	return &values
}

func mcpRequestLine(t *testing.T, tool string, arguments map[string]any) string {
	t.Helper()
	content, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": arguments,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(content) + "\n"
}

func responseLines(t *testing.T, output string) []map[string]any {
	t.Helper()
	parts := strings.Split(strings.TrimSpace(output), "\n")
	responses := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(part), &decoded); err != nil {
			t.Fatalf("invalid response line %q: %v", part, err)
		}
		responses = append(responses, decoded)
	}
	return responses
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
