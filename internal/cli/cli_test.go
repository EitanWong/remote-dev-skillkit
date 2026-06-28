package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
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

func TestMCPServeProcessesInitialize(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = reader
	_, _ = writer.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}` + "\n")
	_ = writer.Close()

	if err := app.Run(context.Background(), []string{"mcp", "serve"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"protocolVersion":"2025-11-25"`) {
		t.Fatalf("expected initialize response, got %q", stdout.String())
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

func TestHostServeWithoutTicketExplainsPlaceholder(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "provide --ticket-code") {
		t.Fatalf("expected ticket-code guidance, got %q", stdout.String())
	}
}

func TestHostServeRegistersWithLocalGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary", "--gateway", server.URL, "--ticket-code", ticket.Code, "--name", "test-host"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status string `json:"status"`
		Host   struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "registered-pending-approval" {
		t.Fatalf("unexpected status %q", payload.Status)
	}
	if payload.Host.Name != "test-host" {
		t.Fatalf("expected host name override, got %q", payload.Host.Name)
	}
	if payload.Host.Status != "pending" {
		t.Fatalf("expected pending host, got %q", payload.Host.Status)
	}
}

func TestHostServePollsAndCompletesDevJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job, got %d", processed)
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", completed.Status)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
}

func TestHostServeReportsFailedDevJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"capabilities": []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, host.ID)
	if err == nil {
		t.Fatal("expected host runner failure")
	}
	if processed != 0 {
		t.Fatalf("expected 0 processed jobs, got %d", processed)
	}
	failed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.JobStatusFailed {
		t.Fatalf("expected failed job, got %s", failed.Status)
	}
	if failed.FailureReason == "" {
		t.Fatal("failure reason should be set")
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

func TestDemoLocalOutputsClosedLoop(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"demo", "local"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Host struct {
			Status string `json:"status"`
		} `json:"host"`
		Job struct {
			Status string `json:"status"`
		} `json:"job"`
		Audit []struct {
			Action string `json:"action"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Host.Status != "active" {
		t.Fatalf("host should be active, got %q", payload.Host.Status)
	}
	if payload.Job.Status != "succeeded" {
		t.Fatalf("job should succeed, got %q", payload.Job.Status)
	}
	if len(payload.Audit) != 5 {
		t.Fatalf("expected 5 audit events, got %d", len(payload.Audit))
	}
}

func TestGatewayServeRequiresDevFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"gateway", "serve"})
	if err == nil {
		t.Fatal("expected gateway serve without --dev to fail")
	}
	if !strings.Contains(err.Error(), "requires --dev") {
		t.Fatalf("unexpected error: %v", err)
	}
}
