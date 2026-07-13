package mcpstdio

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
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

func TestServerToolCallRetiredToolsAreProtocolErrors(t *testing.T) {
	for _, tool := range []string{
		"rdev.invites.create",
		"rdev.support_session.create",
		"rdev.adapter.verify_result",
		"rdev.files.read",
	} {
		var out bytes.Buffer
		server := NewServer(gateway.NewMemoryGateway())
		if err := server.Serve(context.Background(), strings.NewReader(mcpRequestLine(t, tool, map[string]any{})), &out); err != nil {
			t.Fatal(err)
		}
		lines := responseLines(t, out.String())
		errPayload, ok := lines[0]["error"].(map[string]any)
		if !ok {
			t.Fatalf("retired tool %s returned no protocol error: %#v", tool, lines[0])
		}
		if errPayload["code"] != float64(-32602) || !strings.Contains(errPayload["message"].(string), "unknown tool") {
			t.Fatalf("retired tool %s returned wrong error: %#v", tool, errPayload)
		}
	}
}

func TestServerToolCallSessionsConnectReturnsForegroundEntry(t *testing.T) {
	result := callSessionTool(t, NewServer(gateway.NewMemoryGateway()), "rdev.sessions.connect", map[string]any{
		"target": "auto",
	})
	if result["schema_version"] != "rdev.support-session-connect.v1" {
		t.Fatalf("unexpected sessions.connect schema: %#v", result)
	}
	if result["selected_path"] != "start-foreground-gateway" {
		t.Fatalf("expected foreground gateway path, got %#v", result)
	}
	if result["ready_to_send_to_human"] != false {
		t.Fatalf("foreground entry must not be ready before gateway startup: %#v", result)
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

func TestMCPProviderPolicyValidationAndRegionalEvidence(t *testing.T) {
	known := map[string]bool{"cloudflare-quick": true, "configured-gateway": true}
	valid := mcpTunnelProviderPolicyFile{AllowedProviderIDs: ptrStrings([]string{"cloudflare-quick"})}
	allowed, disabled, err := validateMCPProviderPolicy(valid, known)
	if err != nil || !allowed["cloudflare-quick"] || len(disabled) != 0 {
		t.Fatalf("valid provider policy = %#v %#v %v", allowed, disabled, err)
	}
	for _, tc := range []mcpTunnelProviderPolicyFile{
		{AllowedProviderIDs: ptrStrings([]string{" cloudflare-quick"})},
		{AllowedProviderIDs: ptrStrings([]string{"unknown"})},
		{AllowedProviderIDs: ptrStrings([]string{"cloudflare-quick", "cloudflare-quick"})},
		{DisabledProviderIDs: []string{"cloudflare-quick", "cloudflare-quick"}},
		{AllowedProviderIDs: ptrStrings([]string{"cloudflare-quick"}), DisabledProviderIDs: []string{"cloudflare-quick"}},
		{SSHKnownHostsPaths: map[string]string{"unknown": "/tmp/known_hosts"}},
	} {
		if _, _, err := validateMCPProviderPolicy(tc, known); err == nil {
			t.Fatalf("invalid provider policy unexpectedly passed: %#v", tc)
		}
	}
	if !mcpPolicyAllowsProvider("cloudflare-quick", allowed, disabled) || mcpPolicyAllowsProvider("configured-gateway", allowed, disabled) {
		t.Fatal("provider allowlist was not enforced")
	}
	if mcpPolicyAllowsProvider("cloudflare-quick", map[string]bool{}, map[string]bool{"cloudflare-quick": true}) {
		t.Fatal("disabled provider was allowed")
	}

	if values, err := decodeMCPRegionalEvidence([]byte(`{}`)); err != nil || len(values) != 1 {
		t.Fatalf("single regional evidence decode = %#v %v", values, err)
	}
	if values, err := decodeMCPRegionalEvidence([]byte(`[{},{ }]`)); err != nil || len(values) != 2 {
		t.Fatalf("regional evidence array decode = %#v %v", values, err)
	}
	for _, raw := range []string{"", `{} {}`, `{"unknown":true}`} {
		if _, err := decodeMCPRegionalEvidence([]byte(raw)); err == nil {
			t.Fatalf("invalid regional evidence %q unexpectedly passed", raw)
		}
	}

	policyPath := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(policyPath, []byte(`{"disabled_provider_ids":["cloudflare-quick"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if summaries, err := loadMCPRegionalEvidence(policyPath, tunnel.RegionGlobal, time.Now().UTC(), false); err != nil || len(summaries) != 0 {
		t.Fatalf("policy without evidence = %#v %v", summaries, err)
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
	if agentRdevCommand(" /custom/rdev ") != "/custom/rdev" || agentRdevCommand("") == "" {
		t.Fatal("agent rdev command selection failed")
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

func TestMCPRemoteAndSessionArgumentBranches(t *testing.T) {
	server := NewServerWithRemoteGateway(gateway.NewMemoryGateway(), "https://default.example.test/v1")
	if server.effectiveGatewayURL(nil) != "https://default.example.test/v1" || server.effectiveGatewayURL(map[string]any{"gateway_url": "https://override.example.test/v1"}) != "https://override.example.test/v1" {
		t.Fatal("gateway URL override selection failed")
	}
	if _, err := server.decodeRemoteResponse(&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("not-json"))}); err == nil {
		t.Fatal("malformed remote response should fail")
	}
	if _, err := server.decodeRemoteResponse(&http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader(`{"error":"gateway down"}`))}); err == nil {
		t.Fatal("remote gateway error should fail")
	}

	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	spec := sessionSpecFromArgs(map[string]any{"reason": "test", "expires_at": expires, "capabilities": []any{"shell.user"}})
	if spec.Reason != "test" || spec.ExpiresAt.IsZero() || len(spec.Capabilities) != 1 {
		t.Fatalf("session spec argument parsing failed: %#v", spec)
	}
	if _, err := loadMCPRegionalEvidence(filepath.Join(t.TempDir(), "missing.json"), tunnel.RegionGlobal, time.Now().UTC(), false); err == nil {
		t.Fatal("missing regional evidence policy should fail")
	}

	local := NewServer(gateway.NewMemoryGateway())
	created := callSessionTool(t, local, "rdev.sessions.create", map[string]any{"reason": "artifact list"})
	sessionID := stringValue(t, mapValue(t, created, "session"), "id")
	artifacts := callSessionTool(t, local, "rdev.sessions.artifacts", map[string]any{"session_id": sessionID})
	if _, ok := artifacts["artifacts"]; !ok {
		t.Fatalf("local artifact listing failed: %#v", artifacts)
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
