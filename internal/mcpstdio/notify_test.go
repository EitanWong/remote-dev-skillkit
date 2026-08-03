package mcpstdio

import (
	"net/http/httptest"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
)

func TestMCPSessionNotifyLocalSetGetClear(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "notify mcp"})
	if err != nil {
		t.Fatal(err)
	}
	mcp := NewServer(gw)

	// get on an unsubscribed session returns empty notify_url.
	got := callSessionTool(t, mcp, "rdev.sessions.notify", map[string]any{
		"session_id": session.ID,
		"action":     "get",
	})
	if got["notify_url"] != "" {
		t.Fatalf("expected empty notify_url, got %#v", got)
	}

	set := callSessionTool(t, mcp, "rdev.sessions.notify", map[string]any{
		"session_id": session.ID,
		"action":     "set",
		"notify_url": "https://hooks.example.test/hermes",
	})
	if set["notify_url"] != "https://hooks.example.test/hermes" {
		t.Fatalf("set failed: %#v", set)
	}

	got = callSessionTool(t, mcp, "rdev.sessions.notify", map[string]any{
		"session_id": session.ID,
		"action":     "get",
	})
	if got["notify_url"] != "https://hooks.example.test/hermes" {
		t.Fatalf("get after set failed: %#v", got)
	}

	cleared := callSessionTool(t, mcp, "rdev.sessions.notify", map[string]any{
		"session_id": session.ID,
		"action":     "clear",
	})
	if cleared["notify_url"] != "" {
		t.Fatalf("clear failed: %#v", cleared)
	}
}

func TestMCPSessionNotifyRejectsMissingURLOnSet(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "notify mcp"})
	if err != nil {
		t.Fatal(err)
	}
	mcp := NewServer(gw)

	if msg := callSessionToolError(t, mcp, "rdev.sessions.notify", map[string]any{
		"session_id": session.ID,
		"action":     "set",
	}); msg == "" {
		t.Fatal("expected missing notify_url to fail")
	}
}

func TestMCPSessionNotifyProxiesToRemoteGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "notify proxy"})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer httpServer.Close()
	mcp := NewServerWithRemoteGateway(gateway.NewMemoryGateway(), httpServer.URL)

	set := callSessionTool(t, mcp, "rdev.sessions.notify", map[string]any{
		"session_id": session.ID,
		"action":     "set",
		"notify_url": "https://hooks.example.test/openclaw",
	})
	if notifyURL := nestedSessionString(set, "notify_url"); notifyURL != "https://hooks.example.test/openclaw" {
		t.Fatalf("remote set failed: %#v", set)
	}

	got := callSessionTool(t, mcp, "rdev.sessions.notify", map[string]any{
		"session_id": session.ID,
		"action":     "get",
	})
	if notifyURL := nestedSessionString(got, "notify_url"); notifyURL != "https://hooks.example.test/openclaw" {
		t.Fatalf("remote get failed: %v", got)
	}
}

func nestedSessionString(result map[string]any, field string) string {
	// Remote proxy responses: {"session": {...}} (set) or
	// {"snapshot": {"session": {...}, ...}} (get snapshot).
	for _, key := range []string{"session", "snapshot"} {
		container, _ := result[key].(map[string]any)
		if value, ok := container[field].(string); ok {
			return value
		}
		if inner, ok := container["session"].(map[string]any); ok {
			if value, ok := inner[field].(string); ok {
				return value
			}
		}
	}
	return ""
}
