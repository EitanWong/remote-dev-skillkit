package mcpstdio

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestServerInitializeAndToolsList(t *testing.T) {
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
	if lines[1]["error"] != nil {
		t.Fatalf("tools/list failed: %v", lines[1]["error"])
	}
}

func TestServerToolCallCreateTicket(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rdev.tickets.create","arguments":{"mode":"attended-temporary","ttl_seconds":600,"reason":"test"}}}` + "\n"
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}
	result, ok := lines[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %#v", lines[0])
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("missing structured content: %#v", result)
	}
	if _, ok := structured["joinUrl"].(string); !ok {
		t.Fatalf("expected joinUrl in structured content: %#v", structured)
	}
}

func TestServerToolCallCreateJobReturnsEnvelope(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "local",
		OS:         "darwin",
		Arch:       "arm64",
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rdev.jobs.create","arguments":{"host_id":"` + host.ID + `","adapter":"codex","intent":"fix tests","policy":{"workspace_root":"/repo","capabilities":["fs.read","fs.write.scoped","dev.codex"]}}}}` + "\n"
	var out bytes.Buffer
	server := NewServer(gw)

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	envelope := structured["envelope"].(map[string]any)
	if envelope["signature"] == "" {
		t.Fatalf("expected signed envelope, got %#v", envelope)
	}
	if envelope["host_id"] != host.ID {
		t.Fatalf("expected envelope host binding %q, got %#v", host.ID, envelope["host_id"])
	}
}

func TestServerToolCallExplainShellPolicy(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rdev.policy.explain_shell","arguments":{"mode":"attended-temporary","policy":{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}}}}` + "\n"
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["allowed"] != true {
		t.Fatalf("expected shell policy allowed, got %#v", structured)
	}
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
