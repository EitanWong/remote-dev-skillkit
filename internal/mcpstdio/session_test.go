package mcpstdio

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
)

func TestSessionsToolsListExposesOnlySessionControlPlane(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	tools := result["tools"].([]any)
	seen := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		seen[tool["name"].(string)] = true
	}
	for _, name := range []string{
		"rdev.sessions.create",
		"rdev.sessions.connect",
		"rdev.sessions.status",
		"rdev.sessions.events",
		"rdev.sessions.task",
		"rdev.sessions.interrupt",
		"rdev.sessions.artifacts",
		"rdev.sessions.close",
	} {
		if !seen[name] {
			t.Fatalf("missing session tool %s from tools/list: %#v", name, seen)
		}
	}
	if seen["rdev.support_session.connect"] {
		t.Fatal("old support-session connect tool must be absent from tools/list")
	}
	for _, old := range []string{"rdev.tickets.create", "rdev.tickets.revoke", "rdev.hosts.list", "rdev.jobs.create", "rdev.jobs.authorize", "rdev.artifacts.list"} {
		if seen[old] {
			t.Fatalf("old experimental tool %s must be absent from tools/list", old)
		}
	}
}

func TestSessionsMCPCreateStatusTaskEventsAndClose(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)

	created := callSessionTool(t, server, "rdev.sessions.create", map[string]any{
		"reason":             "repair target",
		"join_policy":        "single-target",
		"reconnect_grace_ms": float64(120000),
	})
	session := mapValue(t, created, "session")
	sessionID := stringValue(t, session, "id")
	joinCode := stringValue(t, session, "join_code")
	if sessionID == "" || joinCode == "" {
		t.Fatalf("sessions.create missing session id/join_code: %#v", created)
	}
	if stringValue(t, mapValue(t, created, "status"), "agent_next_action") == "" {
		t.Fatalf("sessions.create should expose Agent-native status: %#v", created)
	}

	_, endpoint, _, _, err := gw.JoinSessionByCode(joinCode, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-winbox",
		Capabilities:        []string{"shell", "fs"},
		Transport:           controlplane.TransportLongPoll,
	})
	if err != nil {
		t.Fatalf("JoinSessionByCode() error = %v", err)
	}

	status := callSessionTool(t, server, "rdev.sessions.status", map[string]any{"session_id": sessionID})
	if stringValue(t, mapValue(t, status, "status"), "status") == "" {
		t.Fatalf("sessions.status missing status summary: %#v", status)
	}
	statusBytes := strings.Join([]string{
		stringValue(t, mapValue(t, status, "status"), "user_summary"),
		stringValue(t, mapValue(t, status, "status"), "agent_next_action"),
	}, " ")
	if strings.TrimSpace(statusBytes) == "" {
		t.Fatalf("sessions.status should include user_summary and agent_next_action: %#v", status)
	}

	taskResult := callSessionTool(t, server, "rdev.sessions.task", map[string]any{
		"session_id":      sessionID,
		"adapter":         "shell",
		"intent":          "hostname",
		"capabilities":    []any{"shell"},
		"idempotency_key": "task-1",
	})
	task := mapValue(t, taskResult, "task")
	if stringValue(t, task, "target_endpoint_id") != endpoint.ID {
		t.Fatalf("task did not route to joined endpoint: %#v", taskResult)
	}

	events := callSessionTool(t, server, "rdev.sessions.events", map[string]any{
		"session_id": sessionID,
		"after_seq":  float64(0),
		"limit":      float64(10),
	})
	if _, ok := events["events"].([]any); !ok {
		t.Fatalf("sessions.events missing event array: %#v", events)
	}
	if _, ok := events["snapshot_required"].(bool); !ok {
		t.Fatalf("sessions.events missing replay hints: %#v", events)
	}

	closed := callSessionTool(t, server, "rdev.sessions.close", map[string]any{
		"session_id": sessionID,
		"reason":     "done",
	})
	if stringValue(t, mapValue(t, closed, "status"), "status") != string(controlplane.StatusClosed) {
		t.Fatalf("sessions.close should return closed status: %#v", closed)
	}
}

func TestSessionsMCPInterruptAndArtifactAppend(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	created := callSessionTool(t, server, "rdev.sessions.create", map[string]any{"reason": "interrupt test"})
	sessionID := stringValue(t, mapValue(t, created, "session"), "id")

	interrupt := callSessionTool(t, server, "rdev.sessions.interrupt", map[string]any{
		"session_id":      sessionID,
		"reason":          "operator stop",
		"idempotency_key": "interrupt-1",
	})
	if stringValue(t, mapValue(t, interrupt, "event"), "type") != string(controlplane.EventTypeInterrupt) {
		t.Fatalf("unexpected interrupt event: %#v", interrupt)
	}

	artifact := callSessionTool(t, server, "rdev.sessions.artifacts", map[string]any{
		"session_id": sessionID,
		"id":         "art-1",
		"kind":       "text",
		"name":       "result.txt",
		"size_bytes": float64(4),
		"sha256":     strings.Repeat("a", 64),
		"complete":   true,
	})
	if stringValue(t, mapValue(t, artifact, "artifact"), "id") != "art-1" {
		t.Fatalf("unexpected artifact result: %#v", artifact)
	}
}

func TestRemoteSessionsMCPEventsAndArtifacts(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "remote MCP", JoinPolicy: "single-target"})
	if err != nil {
		t.Fatal(err)
	}
	_, endpoint, _, _, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role: controlplane.EndpointRoleTarget, Platform: "windows/amd64", IdentityFingerprint: "fp-remote-mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.UpsertSessionArtifact(session.ID, controlplane.ArtifactRef{
		ID: "art_remote", Kind: "evidence", Name: "result.json", SHA256: strings.Repeat("a", 64), Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer httpServer.Close()
	mcp := NewServerWithRemoteGateway(gateway.NewMemoryGateway(), httpServer.URL)

	events := callSessionTool(t, mcp, "rdev.sessions.events", map[string]any{"session_id": session.ID, "after_seq": float64(0)})
	rawEvents, ok := events["events"].([]any)
	if !ok || len(rawEvents) == 0 || rawEvents[0].(map[string]any)["to_endpoint_id"] != endpoint.ID {
		t.Fatalf("remote events missing joined endpoint: %#v", events)
	}
	artifacts := callSessionTool(t, mcp, "rdev.sessions.artifacts", map[string]any{"session_id": session.ID})
	rawArtifacts, ok := artifacts["artifacts"].([]any)
	if !ok || len(rawArtifacts) != 1 || rawArtifacts[0].(map[string]any)["id"] != "art_remote" {
		t.Fatalf("remote artifacts mismatch: %#v", artifacts)
	}
}

func TestSessionsMCPRejectsOldHostJobArtifactTools(t *testing.T) {
	for _, tool := range []string{"rdev.tickets.create", "rdev.tickets.revoke", "rdev.hosts.list", "rdev.jobs.authorize", "rdev.artifacts.list", "rdev.support_session.connect"} {
		server := NewServer(gateway.NewMemoryGateway())
		input := mcpRequestLine(t, tool, map[string]any{
			"host_id":          "hst_old",
			"job_id":           "job_old",
			"authorization_id": "screen.screenshot",
			"decision":         "authorized",
		})
		var out bytes.Buffer
		if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
			t.Fatal(err)
		}
		lines := responseLines(t, out.String())
		errPayload := lines[0]["error"].(map[string]any)
		if !strings.Contains(errPayload["message"].(string), "unknown tool") {
			t.Fatalf("old tool %s should be unknown, got %#v", tool, errPayload)
		}
	}
}

func callSessionTool(t *testing.T, server Server, tool string, args map[string]any) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(mcpRequestLine(t, tool, args)), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if lines[0]["error"] != nil {
		t.Fatalf("%s failed: %#v", tool, lines[0]["error"])
	}
	result := lines[0]["result"].(map[string]any)
	return result["structuredContent"].(map[string]any)
}

func mapValue(t *testing.T, values map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := values[key].(map[string]any)
	if !ok {
		t.Fatalf("field %s is not an object in %#v", key, values)
	}
	return value
}

func stringValue(t *testing.T, values map[string]any, key string) string {
	t.Helper()
	value, ok := values[key].(string)
	if !ok {
		t.Fatalf("field %s is not a string in %#v", key, values)
	}
	return value
}
