package mcpstdio

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
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
	if root, ok := structured["manifestRootPublicKey"].(string); !ok || root == "" {
		t.Fatalf("expected manifestRootPublicKey in structured content: %#v", structured)
	}
}

func TestServerToolCallCreateInvite(t *testing.T) {
	input := mcpRequestLine(t, "rdev.invites.create", map[string]any{
		"gateway_url":  "https://api.example.com/v1",
		"mode":         "attended-temporary",
		"ttl_seconds":  600,
		"reason":       "repair target host",
		"capabilities": []string{"shell.user", "codex.run"},
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.agent-invite.v1" {
		t.Fatalf("expected agent invite schema, got %#v", structured)
	}
	legacyEntryField := "customer" + "_bootstrap"
	legacyPlanField := "connector" + "_package_plan"
	if _, ok := structured[legacyEntryField]; ok {
		t.Fatalf("structured content should use connection_entry, got %#v", structured)
	}
	if _, ok := structured[legacyPlanField]; ok {
		t.Fatalf("structured content should use connection_entry_plan, got %#v", structured)
	}
	hostCommand, ok := structured["host_command"].(string)
	if !ok || !strings.Contains(hostCommand, "host serve --manifest-url https://api.example.com/v1/tickets/") || !strings.Contains(hostCommand, "--transport auto") || !strings.Contains(hostCommand, "--manifest-root-public-key") {
		t.Fatalf("expected target host auto command, got %#v", structured)
	}
	if root, ok := structured["manifest_root_public_key"].(string); !ok || root == "" {
		t.Fatalf("expected manifest root in invite structured content, got %#v", structured)
	}
	if _, ok := structured["agent_next_actions"].([]any); !ok {
		t.Fatalf("expected agent next actions, got %#v", structured)
	}
	plan, ok := structured["transport_plan"].(map[string]any)
	if !ok || plan["mode"] != "auto" {
		t.Fatalf("expected auto transport plan, got %#v", structured)
	}
	connectionPlan, ok := structured["connection_plan"].(map[string]any)
	if !ok || connectionPlan["schema_version"] != "rdev.connection-plan.v1" {
		t.Fatalf("expected connection plan, got %#v", structured)
	}
	connectionEntryPlan, ok := structured["connection_entry_plan"].(map[string]any)
	if !ok ||
		connectionEntryPlan["schema_version"] != "rdev.connection-entry-plan.v1" ||
		connectionEntryPlan["mode"] != "universal-agent-selected-entry" ||
		connectionEntryPlan["package_plan_schema"] != "rdev.connection-entry.package-plan.v1" {
		t.Fatalf("expected connection entry plan, got %#v", structured)
	}
	flow, ok := connectionEntryPlan["required_agent_flow"].([]any)
	if !ok || !containsAnyString(flow, "materialize the invite with rdev.connection_entry.plan or rdev connection-entry plan before giving target-side instructions") {
		t.Fatalf("expected required materialization flow, got %#v", connectionEntryPlan)
	}
	selectionPolicy, ok := connectionEntryPlan["target_selection_policy"].(map[string]any)
	if !ok ||
		selectionPolicy["schema_version"] != "rdev.target-selection-policy.v1" ||
		selectionPolicy["default_owned_mode"] != "managed" ||
		selectionPolicy["default_third_party_mode"] != "attended-temporary" {
		t.Fatalf("expected target selection policy, got %#v", connectionEntryPlan)
	}
	if rules, ok := selectionPolicy["agent_rules"].([]any); !ok || !containsAnyString(rules, "never make target-side humans choose between ticket, root, gateway, transport, release, or checksum values") {
		t.Fatalf("expected no-manual-assembly target selection rule, got %#v", selectionPolicy)
	}
	implemented, ok := connectionPlan["implemented"].([]any)
	if !ok || len(implemented) < 4 {
		t.Fatalf("expected implemented connection protocols, got %#v", connectionPlan)
	}
	agentManaged, ok := connectionPlan["agent_managed"].([]any)
	if !ok || len(agentManaged) < 3 {
		t.Fatalf("expected agent-managed connection protocols, got %#v", connectionPlan)
	}
	discoveryPlan, ok := connectionPlan["discovery_plan"].(map[string]any)
	if !ok || discoveryPlan["schema_version"] != "rdev.discovery-plan.v1" {
		t.Fatalf("expected discovery plan, got %#v", connectionPlan)
	}
	authority, ok := structured["authority_profile"].(map[string]any)
	if !ok || authority["schema_version"] != "rdev.agent-authority.v1" || authority["profile"] != "max-control" {
		t.Fatalf("expected max-control authority profile, got %#v", structured)
	}
	connectionEntry, ok := structured["connection_entry"].(map[string]any)
	if !ok || connectionEntry["schema_version"] != "rdev.connection-entry.v1" || connectionEntry["handoff_name"] != "Connection Entry" {
		t.Fatalf("expected connection entry, got %#v", structured)
	}
	handoffContract, ok := connectionEntry["handoff_contract"].([]any)
	if !ok || !containsAnyString(handoffContract, "Target-side humans must not assemble ticket codes, gateway URLs, manifest roots, transports, release roots, or checksums by hand.") {
		t.Fatalf("expected universal handoff contract, got %#v", connectionEntry)
	}
	hostContextPlan, ok := structured["host_context_plan"].(map[string]any)
	if !ok || hostContextPlan["schema_version"] != "rdev.host-context-plan.v1" || hostContextPlan["storage_location"] != "remote-host-first" {
		t.Fatalf("expected host context plan, got %#v", structured)
	}
	provisioningPlan, ok := structured["agent_provisioning_plan"].(map[string]any)
	if !ok || provisioningPlan["schema_version"] != "rdev.agent-provisioning-plan.v1" || provisioningPlan["mode"] != "adaptive-host-local" {
		t.Fatalf("expected agent provisioning plan, got %#v", structured)
	}
	collaborationPlan, ok := structured["agent_collaboration_plan"].(map[string]any)
	if !ok || collaborationPlan["schema_version"] != "rdev.agent-collaboration-plan.v1" || collaborationPlan["mode"] != "host-local-peer-collaboration" {
		t.Fatalf("expected agent collaboration plan, got %#v", structured)
	}
	localizationPlan, ok := structured["localization_plan"].(map[string]any)
	if !ok || localizationPlan["schema_version"] != "rdev.localization-plan.v1" || localizationPlan["mode"] != "target-host-language-auto" {
		t.Fatalf("expected localization plan, got %#v", structured)
	}
	managedDevPlan, ok := structured["managed_development_plan"].(map[string]any)
	if !ok || managedDevPlan["schema_version"] != "rdev.managed-development-plan.v1" || managedDevPlan["mode"] != "owned-long-running-developer-workstation" {
		t.Fatalf("expected managed development plan, got %#v", structured)
	}
}

func TestServerToolCallConnectionEntryPlan(t *testing.T) {
	inviteInput := mcpRequestLine(t, "rdev.invites.create", map[string]any{
		"gateway_url": "https://api.example.com/v1",
		"mode":        "attended-temporary",
		"ttl_seconds": 600,
		"reason":      "repair target host",
		"transport":   "auto",
	})
	var inviteOut bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())
	if err := server.Serve(context.Background(), strings.NewReader(inviteInput), &inviteOut); err != nil {
		t.Fatal(err)
	}
	inviteLines := responseLines(t, inviteOut.String())
	inviteResult := inviteLines[0]["result"].(map[string]any)
	inviteStructured := inviteResult["structuredContent"].(map[string]any)
	inviteJSON, err := json.Marshal(inviteStructured)
	if err != nil {
		t.Fatal(err)
	}

	input := mcpRequestLine(t, "rdev.connection_entry.plan", map[string]any{
		"invite_json": string(inviteJSON),
		"target_os":   "windows",
		"ownership":   "third-party",
	})
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.connection-entry.materialization-plan.v1" ||
		structured["session_mode"] != "attended-temporary" ||
		structured["target_os"] != "windows" {
		t.Fatalf("unexpected connection entry plan: %#v", structured)
	}
	if structured["connection_entry_name"] != "Connection Entry" ||
		structured["entry_package_plan_schema"] != "rdev.connection-entry.package-plan.v1" {
		t.Fatalf("expected universal connection entry contract fields, got %#v", structured)
	}
	if text, ok := structured["mode_decision"].(string); !ok || !strings.Contains(text, "attended-temporary") {
		t.Fatalf("expected attended temporary mode decision, got %#v", structured)
	}
	if contract, ok := structured["handoff_contract"].([]any); !ok || !containsAnyString(contract, "Agents must use rdev.connection_entry.plan or rdev connection-entry plan before giving target-side instructions.") {
		t.Fatalf("expected handoff contract, got %#v", structured)
	}
	if surface, ok := structured["human_surface"].([]any); !ok || !containsAnyString(surface, "connection_entry.entry_url") {
		t.Fatalf("expected human connection entry surface, got %#v", structured)
	}
	if metadata, ok := structured["agent_metadata"].([]any); !ok || !containsAnyString(metadata, "manifest root public key") {
		t.Fatalf("expected agent-only metadata, got %#v", structured)
	}
	if _, ok := structured["entry_package_plan"]; ok {
		t.Fatalf("missing release inputs should not produce an entry package plan: %#v", structured)
	}
	missing, ok := structured["missing_inputs"].([]any)
	if !ok || !containsAnyString(missing, "release_bundle_url") {
		t.Fatalf("expected missing release inputs, got %#v", structured)
	}
}

func TestServerToolCallConnectionEntryPlanWritesPackagePlan(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	inviteIn := mcpRequestLine(t, "rdev.invites.create", map[string]any{
		"gateway_url": "https://api.example.com/v1",
		"mode":        "attended-temporary",
		"reason":      "repair target host",
		"transport":   "auto",
	})
	var inviteOut bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(inviteIn), &inviteOut); err != nil {
		t.Fatal(err)
	}
	inviteLines := responseLines(t, inviteOut.String())
	inviteResult := inviteLines[0]["result"].(map[string]any)
	inviteStructured := inviteResult["structuredContent"].(map[string]any)
	inviteJSON, err := json.Marshal(inviteStructured)
	if err != nil {
		t.Fatal(err)
	}

	bootstrap := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(bootstrap, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "entry")
	input := mcpRequestLine(t, "rdev.connection_entry.plan", map[string]any{
		"invite_json":                   string(inviteJSON),
		"out_dir":                       outDir,
		"target_os":                     "windows",
		"ownership":                     "third-party",
		"windows_bootstrap_script":      bootstrap,
		"windows_host_download_url":     "https://agent.example.com/rdev-host.exe",
		"windows_host_sha256":           strings.Repeat("a", 64),
		"release_bundle_url":            "https://agent.example.com/release-bundle.json",
		"release_root_public_key":       "release-root:" + strings.Repeat("b", 43),
		"windows_verifier_download_url": "https://agent.example.com/rdev-verify.exe",
		"windows_verifier_sha256":       strings.Repeat("c", 64),
	})
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	packagePlan, ok := structured["entry_package_plan"].(map[string]any)
	if !ok || packagePlan["schema_version"] != "rdev.connection-entry.package-plan.v1" {
		t.Fatalf("expected generic entry package plan, got %#v", structured)
	}
	launcher, ok := packagePlan["launcher_path"].(string)
	if !ok || !fileExistsForMCPTest(launcher) {
		t.Fatalf("expected generated launcher path, got %#v", packagePlan)
	}
	humanMessage, ok := structured["human_message_path"].(string)
	if !ok || !fileExistsForMCPTest(humanMessage) {
		t.Fatalf("expected generated human message, got %#v", structured)
	}
	if _, err := os.Stat(filepath.Join(outDir, "connection-entry-plan.json")); err != nil {
		t.Fatalf("expected materialized plan JSON: %v", err)
	}
}

func TestServerToolCallConnectionEntryPlanWritesManagedPackagePlan(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	inviteIn := mcpRequestLine(t, "rdev.invites.create", map[string]any{
		"gateway_url": "https://api.example.com/v1",
		"mode":        "managed",
		"reason":      "owned workstation",
		"transport":   "auto",
	})
	var inviteOut bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(inviteIn), &inviteOut); err != nil {
		t.Fatal(err)
	}
	inviteLines := responseLines(t, inviteOut.String())
	inviteResult := inviteLines[0]["result"].(map[string]any)
	inviteStructured := inviteResult["structuredContent"].(map[string]any)
	inviteJSON, err := json.Marshal(inviteStructured)
	if err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "managed-entry")
	input := mcpRequestLine(t, "rdev.connection_entry.plan", map[string]any{
		"invite_json":                       string(inviteJSON),
		"out_dir":                           outDir,
		"target_os":                         "linux",
		"ownership":                         "owned",
		"managed_binary_path":               "/opt/rdev/rdev",
		"release_bundle_path":               "/opt/rdev/release-bundle.json",
		"release_root_public_key":           "release-root:" + strings.Repeat("b", 43),
		"release_bundle_required_artifacts": "rdev,rdev-host,rdev-verify",
	})
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	packagePlan, ok := structured["entry_package_plan"].(map[string]any)
	if !ok ||
		packagePlan["schema_version"] != "rdev.connection-entry.package-plan.v1" ||
		packagePlan["package_mode"] != "reviewed-managed-service-connection-entry" ||
		packagePlan["platform_plan_kind"] != "linux-managed-service-plan" {
		t.Fatalf("expected generic managed entry package plan, got %#v", structured)
	}
	launcher, ok := packagePlan["launcher_path"].(string)
	if !ok || !fileExistsForMCPTest(launcher) {
		t.Fatalf("expected generated Linux service unit, got %#v", packagePlan)
	}
	if _, err := os.Stat(filepath.Join(outDir, "managed-linux", "linux-managed-service-plan.json")); err != nil {
		t.Fatalf("expected generated Linux managed service plan: %v", err)
	}
}

func TestServerToolCallUpdatePlan(t *testing.T) {
	releaseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
  "tag_name": "v0.2.0",
  "name": "v0.2.0",
  "html_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/tag/v0.2.0",
  "draft": false,
  "prerelease": false,
  "published_at": "2026-07-02T00:00:00Z",
  "assets": [
    {
      "name": "remote-dev-skillkit-v0.2.0-linux-amd64.tar.gz",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/remote-dev-skillkit-v0.2.0-linux-amd64.tar.gz",
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "size": 123
    },
    {
      "name": "release-bundle.json",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/release-bundle.json",
      "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "size": 456
    }
  ]
}`)
	}))
	defer releaseServer.Close()

	input := mcpRequestLine(t, "rdev.update.plan", map[string]any{
		"repo":            "EitanWong/remote-dev-skillkit",
		"api_base_url":    releaseServer.URL,
		"current_version": "v0.1.0",
		"platform":        "linux/amd64",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.update-plan.v1" ||
		structured["platform"] != "linux/amd64" ||
		structured["update_available"] != true {
		t.Fatalf("unexpected update plan: %#v", structured)
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

func TestServerToolCallVerifyAdapterResult(t *testing.T) {
	artifact := `{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": 0,
  "timed_out": false,
  "canceled": false,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_result", map[string]any{
		"adapter":       "shell",
		"schema":        "rdev.shell-result.v1",
		"artifact_json": artifact,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.adapter-conformance-report.v1" || structured["ok"] != true {
		t.Fatalf("expected adapter conformance success, got %#v", structured)
	}
}

func TestServerToolCallVerifyEnrollmentCertificate(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	certificate, root := enrollmentCertificateForMCPTest(t, now)
	content, err := json.Marshal(certificate)
	if err != nil {
		t.Fatal(err)
	}
	input := mcpRequestLine(t, "rdev.enrollment.verify_certificate", map[string]any{
		"certificate_json": string(content),
		"root_public_key":  root,
		"verify_at":        now.Add(time.Minute).Format(time.RFC3339),
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != EnrollmentCertificateVerificationSchemaVersion || structured["ok"] != true {
		t.Fatalf("expected enrollment certificate verification success, got %#v", structured)
	}
	if structured["ticket_code"] != "ABCD-1234" || structured["issuer_key_id"] != "enrollment-root" {
		t.Fatalf("unexpected certificate identity in report: %#v", structured)
	}
}

func TestServerToolCallVerifyEnrollmentCertificateReportsFailure(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	certificate, _ := enrollmentCertificateForMCPTest(t, now)
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(certificate)
	if err != nil {
		t.Fatal(err)
	}
	input := mcpRequestLine(t, "rdev.enrollment.verify_certificate", map[string]any{
		"certificate_json": string(content),
		"root_public_key":  trustref.Encode("enrollment-root", wrongPublicKey),
		"verify_at":        now.Add(time.Minute).Format(time.RFC3339),
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if lines[0]["error"] != nil {
		t.Fatalf("verification failure should be structured content, got RPC error: %#v", lines[0]["error"])
	}
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["ok"] != false {
		t.Fatalf("expected enrollment certificate failure report, got %#v", structured)
	}
	errors, ok := structured["errors"].([]any)
	if !ok || len(errors) == 0 || !strings.Contains(errors[0].(string), "signature mismatch") {
		t.Fatalf("expected signature mismatch report, got %#v", structured)
	}
}

func TestServerToolCallVerifyEnrollmentCertificateReportsRevocation(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	certificate, root, issuerPrivateKey := enrollmentCertificateForMCPTestWithPrivateKey(t, now)
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: fingerprint,
			Reason:                 "host retired",
			RevokedAt:              now,
		},
	}, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certificateContent, err := json.Marshal(certificate)
	if err != nil {
		t.Fatal(err)
	}
	revocationsContent, err := json.Marshal(revocations)
	if err != nil {
		t.Fatal(err)
	}
	input := mcpRequestLine(t, "rdev.enrollment.verify_certificate", map[string]any{
		"certificate_json": string(certificateContent),
		"revocations_json": string(revocationsContent),
		"root_public_key":  root,
		"verify_at":        now.Add(time.Minute).Format(time.RFC3339),
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if lines[0]["error"] != nil {
		t.Fatalf("revocation failure should be structured content, got RPC error: %#v", lines[0]["error"])
	}
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["ok"] != false || structured["certificate_fingerprint"] != fingerprint {
		t.Fatalf("expected revoked enrollment certificate report, got %#v", structured)
	}
	errors, ok := structured["errors"].([]any)
	if !ok || len(errors) == 0 || !strings.Contains(errors[0].(string), "revoked") {
		t.Fatalf("expected revoked certificate error, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterResultReportsFailure(t *testing.T) {
	artifact := `{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_result", map[string]any{
		"adapter":       "shell",
		"schema":        "rdev.shell-result.v1",
		"artifact_json": artifact,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if lines[0]["error"] != nil {
		t.Fatalf("conformance failure should be structured content, got RPC error: %#v", lines[0]["error"])
	}
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["ok"] != false {
		t.Fatalf("expected adapter conformance failure report, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterLifecycle(t *testing.T) {
	manifest := `{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "claude-code",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version"]},
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_approvals": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": true},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_jobs": false,
    "adapter_approves_dangerous_actions": false,
    "adapter_installs_persistence": false,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {"supported": true, "evidence_field": "canceled", "timeout_exclusive": true, "cleanup_on_cancel": true}
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_lifecycle", map[string]any{
		"adapter":       "claude-code",
		"artifact_json": manifest,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.adapter-conformance-report.v1" || structured["artifact_schema"] != "rdev.adapter-lifecycle.v1" || structured["ok"] != true {
		t.Fatalf("expected adapter lifecycle conformance success, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterCancellation(t *testing.T) {
	artifact := `{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": -1,
  "timed_out": false,
  "canceled": true,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_cancellation", map[string]any{
		"adapter":       "shell",
		"schema":        "rdev.shell-result.v1",
		"artifact_json": artifact,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.adapter-conformance-report.v1" || structured["ok"] != true {
		t.Fatalf("expected adapter cancellation conformance success, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterRuntime(t *testing.T) {
	fixture := `{
  "schema_version": "rdev.adapter-runtime-fixture.v1",
  "adapter": "fake",
  "job_id": "job_123",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "canceled": false,
  "timed_out": false,
  "cleanup_attempted": true,
  "cleanup_ok": true,
  "result_artifact_schema": "rdev.fake-result.v1",
  "result_artifact": {"schema_version": "rdev.fake-result.v1", "adapter": "fake", "workspace_root": "/tmp/repo"},
  "phases": [
    {"phase": "detect", "ok": true, "evidence": ["version"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "plan", "ok": true, "evidence": ["commands"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "prepare", "ok": true, "evidence": ["workspace"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "run", "ok": true, "evidence": ["process"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "collect", "ok": true, "evidence": ["result"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "cleanup", "ok": true, "evidence": ["cleanup"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0}
  ]
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_runtime", map[string]any{
		"adapter":                 "fake",
		"artifact_json":           fixture,
		"require_result_artifact": true,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.adapter-conformance-report.v1" || structured["artifact_schema"] != "rdev.adapter-runtime-fixture.v1" || structured["ok"] != true {
		t.Fatalf("expected adapter runtime conformance success, got %#v", structured)
	}
}

func enrollmentCertificateForMCPTest(t *testing.T, now time.Time) (model.HostEnrollmentCertificate, string) {
	t.Helper()
	certificate, root, _ := enrollmentCertificateForMCPTestWithPrivateKey(t, now)
	return certificate, root
}

func enrollmentCertificateForMCPTestWithPrivateKey(t *testing.T, now time.Time) (model.HostEnrollmentCertificate, string, ed25519.PrivateKey) {
	t.Helper()
	hostPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          "ABCD-1234",
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        []string{"codex.run", "git.diff"},
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   base64.RawURLEncoding.EncodeToString(hostPublicKey),
		IdentityFingerprint: enrollmentFingerprintForMCPTest(hostPublicKey),
	}
	ticket := model.Ticket{
		Code:         registration.TicketCode,
		Mode:         model.HostModeManaged,
		Capabilities: registration.Capabilities,
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, trustref.Encode("enrollment-root", issuerPublicKey), issuerPrivateKey
}

func enrollmentFingerprintForMCPTest(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
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

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}

func fileExistsForMCPTest(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
