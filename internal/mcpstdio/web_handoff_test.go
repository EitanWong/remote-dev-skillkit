package mcpstdio

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func TestRemoteSessionsMCPHandoffProxiesOperatorRequest(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/ses_handoff/host-handoffs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer operator-secret" {
			t.Fatalf("operator bearer did not reach configured gateway")
		}
		var request struct {
			Platform    string `json:"platform"`
			ExpiresInMS int    `json:"expires_in_ms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Platform != "windows-amd64" || request.ExpiresInMS != 600000 {
			t.Fatalf("unexpected handoff request: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"handoff":{"id":"hnd_test","url":"https://remote.example/connect/hnd_test#proof","expires_at":"2026-07-28T12:00:00Z"}}`)
	}))
	defer remote.Close()

	server := NewServerWithRemoteGatewayAndOperatorToken(gateway.NewMemoryGateway(), remote.URL, "operator-secret")
	result := callSessionTool(t, server, "rdev.sessions.handoff", map[string]any{
		"session_id":    "ses_handoff",
		"platform":      "windows-amd64",
		"expires_in_ms": float64(600000),
	})
	handoff := mapValue(t, result, "handoff")
	if stringValue(t, handoff, "url") != "https://remote.example/connect/hnd_test#proof" {
		t.Fatalf("unexpected MCP handoff result: %#v", result)
	}
}
