package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
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
