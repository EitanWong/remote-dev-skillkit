package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
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

func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
