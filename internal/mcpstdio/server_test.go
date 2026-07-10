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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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
	packageCatalog, ok := connectionEntry["package_catalog"].(map[string]any)
	if !ok || packageCatalog["schema_version"] != model.ConnectionEntryPackageCatalogSchemaVersion {
		t.Fatalf("expected package catalog in connection entry, got %#v", connectionEntry)
	}
	candidates, ok := packageCatalog["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		t.Fatalf("expected package catalog candidates, got %#v", packageCatalog)
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

func TestServerToolCallSupportSessionPlan(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.plan", map[string]any{
		"repo_root":     ".",
		"work_dir":      filepath.Join(t.TempDir(), "support"),
		"gateway_url":   "http://192.0.2.44:8787",
		"target":        "windows",
		"reason":        "company computer support",
		"auto_activate": true,
		"locale":        "zh-CN",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-plan.v1" {
		t.Fatalf("expected support-session plan schema, got %#v", structured)
	}
	autoActivate := structured["auto_activate"].(map[string]any)
	if autoActivate["enabled"] != true || !strings.Contains(autoActivate["scope"].(string), "attended-temporary") {
		t.Fatalf("expected scoped auto authorize, got %#v", autoActivate)
	}
	commands := structured["commands"].(map[string]any)
	startGateway := strings.Join(anyStrings(commands["start_gateway"].([]any)), "\x00")
	if !strings.Contains(startGateway, "--rdev-windows-amd64") ||
		!strings.Contains(startGateway, "--rdev-linux-arm64") ||
		!strings.Contains(startGateway, "--manifest-signing-key") {
		t.Fatalf("expected gateway assets in plan, got %s", startGateway)
	}
	createInvite := strings.Join(anyStrings(commands["create_invite_cli"].([]any)), "\x00")
	if !strings.Contains(createInvite, "--auto-activate") {
		t.Fatalf("expected CLI auto authorize in plan, got %s", createInvite)
	}
	target := structured["target_user_instructions"].(map[string]any)
	if !strings.Contains(target["message"].(string), "目标电脑") ||
		!strings.Contains(target["windows"].(string), "bootstrap.ps1") ||
		strings.Contains(target["windows"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("expected localized visible Windows command, got %#v", target)
	}
	forbidden := structured["forbidden"].([]any)
	if !containsAnyString(forbidden, "manual ticket/root/gateway/transport assembly for target user") {
		t.Fatalf("expected manual assembly prohibition, got %#v", forbidden)
	}
}

func TestServerToolCallSupportSessionLiveE2EPlan(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.live_e2e_plan", map[string]any{
		"gateway_url":        "https://gateway.example.test/rdev/",
		"ticket_code":        "ABCD-1234",
		"host_id":            "hst_1",
		"session_id":         "ses_1",
		"target_endpoint_id": "ep_1",
		"rdev_command":       "rdev-test",
		"timeout_seconds":    180,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if lines[0]["error"] != nil {
		t.Fatalf("live E2E plan tool should be callable, got %#v", lines[0]["error"])
	}
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-live-e2e-plan.v1" ||
		structured["dry_run"] != true ||
		structured["execute"] != false ||
		structured["gateway_url"] != "https://gateway.example.test/rdev" ||
		structured["host_id"] != "hst_1" ||
		structured["session_id"] != "ses_1" ||
		structured["target_endpoint_id"] != "ep_1" ||
		structured["target_os"] != "windows" {
		t.Fatalf("unexpected live E2E plan header: %#v", structured)
	}
	gates := structured["gates"].([]any)
	if len(gates) != 3 {
		t.Fatalf("expected three live E2E gates, got %#v", gates)
	}
	var smokeGate map[string]any
	var interruptGate map[string]any
	for _, value := range gates {
		gate := value.(map[string]any)
		if gate["name"] == "windows_support_session_smoke_remote_control" {
			smokeGate = gate
		}
		if gate["name"] == "windows_session_interrupt_flow" {
			interruptGate = gate
		}
	}
	if smokeGate == nil {
		t.Fatalf("expected smoke proof gate, got %#v", gates)
	}
	smokeCommand := anyStrings(smokeGate["proof_command"].([]any))
	smokeArgs := smokeGate["mcp_arguments"].(map[string]any)
	if !slices.Contains(smokeCommand, "--session-id") ||
		!slices.Contains(smokeCommand, "ses_1") ||
		!slices.Contains(smokeCommand, "--target-endpoint-id") ||
		!slices.Contains(smokeCommand, "ep_1") ||
		slices.Contains(smokeCommand, "--host-id") ||
		smokeArgs["session_id"] != "ses_1" ||
		smokeArgs["target_endpoint_id"] != "ep_1" {
		t.Fatalf("unexpected MCP smoke selectors: command=%#v args=%#v", smokeCommand, smokeArgs)
	}
	if interruptGate == nil ||
		interruptGate["mcp_tool"] != "rdev.sessions.interrupt" ||
		!containsAnyString(interruptGate["required_evidence"].([]any), "rdev.sessions.events replays the interrupt after reconnect") {
		t.Fatalf("expected MCP session-interrupt proof gate, got %#v", interruptGate)
	}
}

func TestServerToolCallSupportSessionHandoff(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.handoff", map[string]any{
		"gateway_url":   "http://192.0.2.44:8787",
		"target":        "windows",
		"reason":        "company computer support",
		"auto_activate": true,
		"locale":        "zh-CN",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	args := structured["mcp_next_arguments"].(map[string]any)
	forbidden := strings.Join(anyStrings(structured["forbidden"].([]any)), "\n")
	if structured["schema_version"] != "rdev.support-session-handoff.v1" ||
		structured["selected_path"] != "create-with-reachable-gateway" ||
		structured["mcp_next_tool"] != "rdev.support_session.create" ||
		args["gateway_url"] != "http://192.0.2.44:8787" ||
		args["target"] != "windows" ||
		!strings.Contains(structured["agent_next_step"].(string), "target_handoff_envelope.full_text") ||
		!strings.Contains(forbidden, "Agent-authored PowerShell") {
		t.Fatalf("expected support-session handoff contract, got %#v", structured)
	}
}

func TestServerToolCallSupportSessionHandoffWithoutGatewayUsesStart(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.handoff", map[string]any{
		"target": "auto",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	startCommand := strings.Join(anyStrings(structured["foreground_start_command"].([]any)), "\x00")
	if structured["schema_version"] != "rdev.support-session-handoff.v1" ||
		structured["selected_path"] != "start-foreground-gateway" ||
		structured["mcp_next_tool"] != "" ||
		!strings.Contains(startCommand, "support-session\x00start") ||
		!strings.Contains(structured["agent_rule"].(string), "do not choose support-session plan") {
		t.Fatalf("expected foreground start handoff, got %#v", structured)
	}
}

func TestServerToolCallSupportSessionHandoffUsesConfiguredGateway(t *testing.T) {
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", "https://hosted.example.test/rdev")
	input := mcpRequestLine(t, "rdev.support_session.handoff", map[string]any{
		"target": "auto",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	args := structured["mcp_next_arguments"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-handoff.v1" ||
		structured["selected_path"] != "create-with-reachable-gateway" ||
		structured["mcp_next_tool"] != "rdev.support_session.create" ||
		structured["gateway_url"] != "https://hosted.example.test/rdev" ||
		args["gateway_url"] != "https://hosted.example.test/rdev" {
		t.Fatalf("expected configured gateway handoff, got %#v", structured)
	}
}

func TestServerToolCallSupportSessionConnectWithoutGatewayReturnsStart(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.connect", map[string]any{
		"target": "auto",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	startCommand := strings.Join(anyStrings(structured["foreground_start_command"].([]any)), "\x00")
	if structured["schema_version"] != "rdev.support-session-connect.v1" ||
		structured["selected_path"] != "start-foreground-gateway" ||
		structured["ready_to_send_to_human"] != false ||
		!strings.Contains(startCommand, "support-session\x00start") ||
		!strings.Contains(structured["agent_next_step"].(string), "ready_file.path") ||
		!strings.Contains(structured["agent_next_step"].(string), "status_file.path") {
		t.Fatalf("expected connect tool to return foreground start path, got %#v", structured)
	}
}

func TestServerToolCallSupportSessionConnectPropagatesTunnelPolicy(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "providers.json")
	input := mcpRequestLine(t, "rdev.support_session.connect", map[string]any{
		"target":                        "auto",
		"region":                        "cn-mainland",
		"provider_policy":               policyPath,
		"allow_degraded_direct_handoff": false,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	startCommand := strings.Join(anyStrings(structured["cli_start_now_command"].([]any)), "\x00")
	if !strings.Contains(startCommand, "--region\x00cn-mainland") ||
		!strings.Contains(startCommand, "--provider-policy\x00"+policyPath) ||
		strings.Contains(startCommand, "--allow-degraded-direct-handoff") {
		t.Fatalf("MCP connect dropped or changed tunnel policy: %#v", structured)
	}
	availability := structured["availability_set"].(map[string]any)
	if availability["region"] != "cn-mainland" ||
		structured["regional_evidence"] == nil ||
		structured["ready_to_send"] != false ||
		structured["ready_to_activate"] != false ||
		structured["ready_to_execute"] != false ||
		structured["degraded_single_entry"] != false {
		t.Fatalf("MCP connect omitted readiness aliases: %#v", structured)
	}
}

func TestServerToolCallSupportSessionConnectExplicitOverrideIsSendableButDegraded(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.connect", map[string]any{
		"gateway_url":                   "https://gateway.example.test",
		"target":                        "windows",
		"region":                        "global",
		"allow_degraded_direct_handoff": true,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	availability := structured["availability_set"].(map[string]any)
	candidates := availability["candidates"].([]any)
	if structured["ready_to_send"] != true ||
		structured["ready_to_send_to_human"] != true ||
		structured["ready_to_activate"] != false ||
		structured["ready_to_execute"] != false ||
		structured["degraded_single_entry"] != true ||
		len(candidates) != 1 {
		t.Fatalf("explicit override must remain degraded single-entry: %#v", structured)
	}
	encoded, err := json.Marshal(structured)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "super-secret-token") || strings.Contains(string(encoded), "AAAAC3NzaKnownHostsSecret") {
		t.Fatalf("MCP response leaked protected tunnel material: %s", encoded)
	}
}

func TestServerToolCallSupportSessionConnectAutoDetectsStableRdevCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable bits for the fallback binary fixture")
	}
	for _, envName := range []string{
		"RDEV_HOSTED_GATEWAY_URL",
		"RDEV_RELAY_GATEWAY_URL",
		"RDEV_MESH_GATEWAY_URL",
		"RDEV_VPN_GATEWAY_URL",
		"RDEV_SSH_GATEWAY_URL",
		"RDEV_CLOUDFLARED_GATEWAY_URL",
	} {
		t.Setenv(envName, "")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	goBin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goBinRdev := filepath.Join(goBin, "rdev")
	if err := os.WriteFile(goBinRdev, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	input := mcpRequestLine(t, "rdev.support_session.connect", map[string]any{"target": "auto"})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	startCommand := anyStrings(structured["cli_start_now_command"].([]any))
	if len(startCommand) == 0 || startCommand[0] != goBinRdev {
		t.Fatalf("expected MCP connect to auto-detect stable rdev command, got %#v", startCommand)
	}
}

func TestServerToolCallSupportSessionConnectWithGatewayCreatesReadyHandoff(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.connect", map[string]any{
		"gateway_url":   "http://192.0.2.44:8787",
		"target":        "windows",
		"reason":        "company computer support",
		"auto_activate": true,
		"locale":        "zh-CN",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	handoff := structured["user_handoff"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-connect.v1" ||
		structured["selected_path"] != "created-with-reachable-gateway" ||
		structured["ready_to_send_to_human"] != false ||
		structured["ready_to_send"] != false ||
		structured["ready_to_activate"] != false ||
		structured["ready_to_execute"] != false ||
		handoff["schema_version"] != "rdev.support-session-user-handoff.v1" ||
		handoff["copy_paste_kind"] != "windows" ||
		!strings.Contains(handoff["copy_paste"].(string), "powershell -NoProfile -Command") ||
		!strings.Contains(handoff["copy_paste"].(string), "bootstrap.ps1") ||
		!strings.Contains(handoff["copy_paste"].(string), "-UseBasicParsing") ||
		strings.Contains(handoff["copy_paste"].(string), "-EncodedCommand") ||
		strings.Contains(handoff["copy_paste"].(string), "$urls has been generated by rdev") ||
		strings.Contains(handoff["copy_paste"].(string), "ProgressPrference") ||
		strings.Contains(handoff["copy_paste"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("expected connect tool to create ready handoff, got %#v", structured)
	}
}

func TestServerToolCallSupportSessionPrepare(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.prepare", map[string]any{
		"repo_root":   ".",
		"work_dir":    filepath.Join(t.TempDir(), "support"),
		"gateway_url": "http://192.0.2.44:8787",
		"target":      "windows",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-prepare.v1" {
		t.Fatalf("expected support-session prepare schema, got %#v", structured)
	}
	readiness := structured["connection_readiness"].(map[string]any)
	if readiness["human_gets_one_command"] != true {
		t.Fatalf("expected one-command readiness contract, got %#v", readiness)
	}
	connectivity := structured["connectivity_strategy"].(map[string]any)
	order := anyStrings(connectivity["selection_order"].([]any))
	if connectivity["schema_version"] != "rdev.support-session-connectivity-strategy.v1" ||
		!slices.Contains(order, "native-lan-gateway") ||
		!slices.Contains(order, "existing-frp-or-chisel-relay") {
		t.Fatalf("expected adaptive connectivity strategy, got %#v", connectivity)
	}
	recovery := anyStrings(structured["standard_recovery"].([]any))
	if !slices.Contains(recovery, "do not write custom PowerShell, relay, activation polling, ticket substitution, or bootstrap glue") {
		t.Fatalf("expected no-improvisation recovery contract, got %#v", recovery)
	}
	runbook := structured["agent_connection_runbook"].(map[string]any)
	if runbook["schema_version"] != "rdev.support-session-agent-runbook.v1" ||
		runbook["phase"] != "prepare" ||
		!strings.Contains(fmt.Sprintf("%v", runbook["sequence"]), "target_handoff_envelope.full_text") {
		t.Fatalf("expected MCP prepare Agent runbook, got %#v", runbook)
	}
}

func TestServerToolCallSupportSessionCreate(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "https://relay.example.test/rdev")
	input := mcpRequestLine(t, "rdev.support_session.create", map[string]any{
		"gateway_url":   "http://192.0.2.44:8787",
		"target":        "windows",
		"reason":        "company computer support",
		"auto_activate": true,
		"locale":        "zh-CN",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-created.v1" ||
		structured["recommended_surface"] != "windows" ||
		structured["auto_activate"] != true {
		t.Fatalf("expected created support session payload, got %#v", structured)
	}
	ticketCode, _ := structured["ticket_code"].(string)
	targetCommand, _ := structured["target_command"].(string)
	gatewayCandidates, _ := structured["gateway_url_candidates"].([]any)
	if ticketCode == "" ||
		!strings.Contains(targetCommand, "powershell -NoProfile -Command") ||
		!strings.Contains(targetCommand, "bootstrap.ps1") ||
		!strings.Contains(targetCommand, "-UseBasicParsing") ||
		strings.Contains(targetCommand, "gateway_url_candidates=") ||
		strings.Contains(targetCommand, "-EncodedCommand") ||
		strings.Contains(targetCommand, "foreach ($u in $urls)") ||
		strings.Contains(targetCommand, "$urls has been generated by rdev") ||
		strings.Contains(targetCommand, "ProgressPrference") ||
		strings.Contains(targetCommand, "<ticket-code>") ||
		strings.Contains(targetCommand, "ExecutionPolicy Bypass") {
		t.Fatalf("expected ready safe target command: ticket=%q command=%q", ticketCode, targetCommand)
	}
	attemptPolicy := structured["connection_attempt_policy"].(map[string]any)
	if attemptPolicy["schema_version"] != "rdev.connection-attempt-policy.v1" ||
		attemptPolicy["windows_download_timeout_sec"] != float64(10) ||
		attemptPolicy["curl_connect_timeout_sec"] != float64(2) {
		t.Fatalf("expected bounded connection attempt policy, got %#v", attemptPolicy)
	}
	continuityPolicy := structured["connection_continuity_policy"].(map[string]any)
	stableKinds := anyStrings(continuityPolicy["stable_fallback_kinds"].([]any))
	if continuityPolicy["schema_version"] != "rdev.support-session-continuity-policy.v1" ||
		continuityPolicy["stable_after_lan_change"] != true ||
		continuityPolicy["has_stable_configured_fallback"] != true ||
		!slices.Contains(stableKinds, "relay") ||
		continuityPolicy["assessment"] != "stable-fallback-configured" {
		t.Fatalf("expected configured relay continuity policy, got %#v", continuityPolicy)
	}
	handoff := structured["user_handoff"].(map[string]any)
	if handoff["schema_version"] != "rdev.support-session-user-handoff.v1" ||
		handoff["copy_paste_kind"] != "windows" ||
		handoff["copy_paste"] != targetCommand ||
		!strings.Contains(handoff["message"].(string), "目标电脑") ||
		!strings.Contains(strings.ToLower(handoff["agent_next_step"].(string)), "do not send") {
		t.Fatalf("expected ready user handoff, got %#v", handoff)
	}
	if len(gatewayCandidates) == 0 {
		t.Fatalf("expected created payload to carry gateway candidates: %#v", structured)
	}
	if len(gatewayCandidates) < 2 ||
		gatewayCandidates[1].(map[string]any)["url"] != "https://relay.example.test/rdev" ||
		gatewayCandidates[1].(map[string]any)["kind"] != "relay" ||
		gatewayCandidates[1].(map[string]any)["source"] != "env:RDEV_RELAY_GATEWAY_URL" {
		t.Fatalf("expected configured relay fallback candidate, got %#v", gatewayCandidates)
	}
	runbook := structured["agent_connection_runbook"].(map[string]any)
	watchRunbook := runbook["watch"].(map[string]any)
	if runbook["schema_version"] != "rdev.support-session-agent-runbook.v1" ||
		runbook["phase"] != "created" ||
		watchRunbook["mcp_tool"] != "rdev.support_session.status" ||
		!strings.Contains(fmt.Sprintf("%v", runbook["forbidden"]), "Agent-authored PowerShell") {
		t.Fatalf("expected MCP create Agent runbook, got %#v", runbook)
	}
	watch := strings.Join(anyStrings(structured["watch_connection_status"].([]any)), "\x00")
	if !strings.Contains(watch, ticketCode) ||
		strings.Contains(watch, "<ticket-code>") ||
		!strings.Contains(watch, "--wait") {
		t.Fatalf("expected ready status watcher, got %s", watch)
	}
	configuredWatcher := structured["watch_connection_status_configured_gateway"].(map[string]any)
	configuredWatch := strings.Join(anyStrings(configuredWatcher["command"].([]any)), "\x00")
	if !strings.Contains(configuredWatch, ticketCode) ||
		!strings.Contains(configuredWatch, "--wait") ||
		strings.Contains(configuredWatch, "--gateway-url") ||
		!strings.Contains(configuredWatcher["agent_rule"].(string), "RDEV_*_GATEWAY_URL") {
		t.Fatalf("expected configured gateway status watcher, got %#v", configuredWatcher)
	}
}

func TestServerToolCallSupportSessionCreateUsesConfiguredGateway(t *testing.T) {
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", "https://hosted.example.test/rdev")
	input := mcpRequestLine(t, "rdev.support_session.create", map[string]any{
		"target":        "auto",
		"reason":        "company computer support",
		"auto_activate": true,
		"locale":        "en",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	ticketCode, _ := structured["ticket_code"].(string)
	if structured["schema_version"] != "rdev.support-session-created.v1" ||
		structured["gateway_url"] != "https://hosted.example.test/rdev" ||
		ticketCode == "" ||
		!strings.Contains(structured["target_command"].(string), "Windows PowerShell") ||
		!strings.Contains(structured["target_command"].(string), "macOS/Linux terminal") ||
		!strings.Contains(structured["target_command"].(string), "https://hosted.example.test/rdev/join/"+ticketCode) {
		t.Fatalf("expected configured gateway create payload, got %#v", structured)
	}
	gatewayCandidates := structured["gateway_url_candidates"].([]any)
	if len(gatewayCandidates) == 0 ||
		gatewayCandidates[0].(map[string]any)["url"] != "https://hosted.example.test/rdev" ||
		gatewayCandidates[0].(map[string]any)["recommended"] != true {
		t.Fatalf("expected configured gateway candidate, got %#v", gatewayCandidates)
	}
}

func TestServerToolCallSupportSessionStatus(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "company computer support", map[string]string{
		"auto_activate": "attended-temporary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "win-dev",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: []string{"shell.user"},
	}); err != nil {
		t.Fatal(err)
	}
	input := mcpRequestLine(t, "rdev.support_session.status", map[string]any{
		"ticket_code": ticket.Code,
		"locale":      "zh-CN",
	})
	var out bytes.Buffer
	server := NewServer(gw)

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-status.v1" ||
		structured["connected"] != true ||
		structured["status"] != "connected" ||
		!strings.Contains(structured["feedback"].(string), "连接已经建立") {
		t.Fatalf("expected connected support session status, got %#v", structured)
	}
	next := structured["connected_next_steps"].(map[string]any)
	calls := next["mcp_next_calls"].([]any)
	if next["schema_version"] != "rdev.support-session-connected-next-steps.v1" ||
		next["connected"] != true ||
		next["host_id"] == "" ||
		!strings.Contains(next["user_report"].(string), "连接已经建立") ||
		len(calls) != 1 ||
		calls[0].(map[string]any)["tool"] != "rdev.sessions.status" {
		t.Fatalf("expected connected next-step contract, got %#v", next)
	}
}

func TestServerToolCallSupportSessionStatusUsesGatewayURL(t *testing.T) {
	hit := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if r.URL.Path != "/v1/support-session/status" ||
			r.URL.Query().Get("ticket_code") != "REMOTE-1234" {
			t.Fatalf("unexpected remote status request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":"rdev.support-session-status.v1","ticket_code":"REMOTE-1234","status":"connected","connected":true,"connected_next_steps":{"schema_version":"rdev.support-session-connected-next-steps.v1","connected":true,"user_report":"remote connected"}}` + "\n"))
	}))
	defer remote.Close()

	input := mcpRequestLine(t, "rdev.support_session.status", map[string]any{
		"gateway_url":     remote.URL,
		"ticket_code":     "REMOTE-1234",
		"locale":          "en",
		"wait":            true,
		"timeout_seconds": 2,
		"interval_millis": 100,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if !hit ||
		structured["ticket_code"] != "REMOTE-1234" ||
		structured["connected"] != true {
		t.Fatalf("expected remote status response, hit=%v structured=%#v", hit, structured)
	}
}

func TestServerToolCallSupportSessionReport(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/sessions/sess_remote":
			_, _ = w.Write([]byte(`{"snapshot":{"id":"sess_remote","endpoints":[{"id":"ep_remote","role":"target","state":"online","name":"win-dev","platform":"windows/amd64","identity_fingerprint":"sha256:mcp-remote-fingerprint"}],"tasks":[{"id":"task_1","target_endpoint_id":"ep_remote","status":"succeeded","adapter":"shell","intent":"identity probe","attempt_id":"attempt_1"}]}}` + "\n"))
		default:
			t.Fatalf("unexpected report request: %s", r.URL.String())
		}
	}))
	defer remote.Close()

	input := mcpRequestLine(t, "rdev.support_session.report", map[string]any{
		"gateway_url": remote.URL,
		"host_id":     "hst_remote",
		"session_id":  "sess_remote",
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	tasks := structured["tasks"].([]any)
	remoteEntry := structured["remote_control_entry"].(map[string]any)
	livePlan, _ := structured["live_remote_e2e_plan"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-report.v1" ||
		structured["host_id"] != "hst_remote" ||
		structured["session_id"] != "sess_remote" ||
		len(tasks) != 1 ||
		remoteEntry["support_device_id_source"] != "host_identity_fingerprint" ||
		remoteEntry["explicit_disconnect_required"] != true ||
		livePlan["schema_version"] != "rdev.support-session-live-e2e-plan.v1" ||
		livePlan["dry_run"] != true ||
		!strings.Contains(structured["human_report"].(string), "identity probe") {
		t.Fatalf("expected support session report, got %#v", structured)
	}
	gates, _ := livePlan["gates"].([]any)
	if len(gates) != 3 {
		t.Fatalf("expected report to include live E2E gates, got %#v", livePlan)
	}
}

func TestServerToolCallSupportSessionReportTicketCodeSelectsSingleActiveHost(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "mcp-ticket-report-host",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ActivateHost(host.ID, []string{"shell.user"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Reason:       "mcp ticket report",
		Capabilities: []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, endpoint, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "mcp-ticket-report-host",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-mcp-ticket",
		Capabilities:        []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := gw.SubmitSessionTask(session.ID, controlplane.TaskSpec{
		TargetEndpointID: endpoint.ID,
		Adapter:          "shell",
		Intent:           "identity probe",
		Capabilities:     []string{"shell.user"},
		Payload: map[string]any{
			"workspace_root": ".",
			"argv":           []any{"cmd", "/c", "hostname"},
			"allow_commands": []any{"cmd"},
		},
		IdempotencyKey: "mcp-ticket-report-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CompleteSessionTask(session.ID, task.ID, map[string]any{"status": "succeeded", "artifact_content": "MCP-REPORT-HOST"}); err != nil {
		t.Fatal(err)
	}
	input := mcpRequestLine(t, "rdev.support_session.report", map[string]any{
		"ticket_code": ticket.Code,
		"session_id":  session.ID,
	})
	var out bytes.Buffer
	server := NewServer(gw)

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	tasks := structured["tasks"].([]any)
	activeHosts := structured["active_hosts"].([]any)
	remoteEntry := structured["remote_control_entry"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-report.v1" ||
		structured["ok"] != true ||
		structured["ticket_code"] != ticket.Code ||
		structured["host_id"] != host.ID ||
		structured["session_id"] != session.ID ||
		len(activeHosts) != 1 ||
		len(tasks) != 1 ||
		remoteEntry["session_passcode"] != ticket.Code ||
		remoteEntry["explicit_disconnect_required"] != true ||
		!strings.Contains(structured["human_report"].(string), "identity probe") ||
		!strings.Contains(structured["stale_host_rule"].(string), "Do not send new session tasks") {
		t.Fatalf("expected ticket-code support session report, got %#v", structured)
	}
}

func TestRemoteControlSmokePoliciesDefaultToHomeWorkspace(t *testing.T) {
	policies := []map[string]any{
		mcpFileListSmokePolicy(),
		mcpDesktopWindowInspectSmokePolicy(),
		mcpPowerShellAuditPolicy("Get-Location"),
		mcpShellAuditPolicy([]string{"shell.user"}, []string{"sh", "-c", "pwd"}, []string{"sh"}),
		mcpShellAuditPolicyWithWriteScope([]string{"shell.user", "fs.write.scoped"}, []string{"sh", "-c", "true"}, []string{"sh"}, []string{"."}),
	}
	for _, policy := range policies {
		if policy["workspace_root"] != "~" {
			t.Fatalf("expected generated remote-control policy to default workspace_root to home, got %#v", policy)
		}
	}
}

func TestServerToolCallSupportSessionSmokeTestRunsStandardProbes(t *testing.T) {
	taskCounter := 0
	createdIntents := []string{}
	tasks := []map[string]any{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/sess_remote":
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshot": map[string]any{
				"id": "sess_remote",
				"endpoints": []map[string]any{{
					"id":       "ep_remote",
					"role":     "target",
					"state":    "online",
					"name":     "win-dev",
					"platform": "windows/amd64",
				}},
				"tasks": tasks,
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/sess_remote/tasks":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode task create: %v", err)
			}
			if body["adapter"] != "shell" && body["adapter"] != "powershell" {
				t.Fatalf("unexpected smoke task body: %#v", body)
			}
			if body["target_endpoint_id"] != "ep_remote" {
				t.Fatalf("expected smoke task to target selected endpoint, got %#v", body)
			}
			createdIntents = append(createdIntents, body["intent"].(string))
			taskCounter++
			task := map[string]any{
				"id":                 fmt.Sprintf("task_%d", taskCounter),
				"target_endpoint_id": "ep_remote",
				"status":             "succeeded",
				"adapter":            body["adapter"],
				"intent":             body["intent"],
			}
			tasks = append(tasks, task)
			_ = json.NewEncoder(w).Encode(map[string]any{"task": task})
		default:
			t.Fatalf("unexpected smoke-test request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer remote.Close()

	input := mcpRequestLine(t, "rdev.support_session.smoke_test", map[string]any{
		"gateway_url":     remote.URL,
		"session_id":      "sess_remote",
		"timeout_seconds": 5,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	audit := structured["capability_audit"].(map[string]any)
	results := audit["results"].([]any)
	remoteEntry := structured["remote_control_entry"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-smoke-test.v1" ||
		structured["ok"] != true ||
		structured["session_id"] != "sess_remote" ||
		structured["target_endpoint_id"] != "ep_remote" ||
		len(results) != 5 ||
		remoteEntry["explicit_disconnect_required"] != true ||
		!strings.Contains(structured["next_action"].(string), "keep the target endpoint connected") ||
		!strings.Contains(structured["human_report"].(string), "Connection: keep alive") {
		t.Fatalf("expected successful smoke-test report, got %#v", structured)
	}
	if len(createdIntents) != 5 || !strings.Contains(strings.Join(createdIntents, "\n"), "capability audit PowerShell identity") {
		t.Fatalf("expected standard probe tasks, got %#v", createdIntents)
	}
}

func TestServerToolCallSupportSessionSmokeTestRemoteControlCreatesStandardAdapterProbes(t *testing.T) {
	taskCounter := 0
	created := []map[string]any{}
	tasks := []map[string]any{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/sess_remote":
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshot": map[string]any{
				"id": "sess_remote",
				"endpoints": []map[string]any{{
					"id":       "ep_remote",
					"role":     "target",
					"state":    "online",
					"name":     "win-dev",
					"platform": "windows/amd64",
				}},
				"tasks": tasks,
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/sess_remote/tasks":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode task create: %v", err)
			}
			if body["target_endpoint_id"] != "ep_remote" {
				t.Fatalf("expected remote-control task to target selected endpoint, got %#v", body)
			}
			created = append(created, body)
			taskCounter++
			task := map[string]any{
				"id":                 fmt.Sprintf("task_%d", taskCounter),
				"target_endpoint_id": "ep_remote",
				"status":             "succeeded",
				"adapter":            body["adapter"],
				"intent":             body["intent"],
			}
			tasks = append(tasks, task)
			_ = json.NewEncoder(w).Encode(map[string]any{"task": task})
		default:
			t.Fatalf("unexpected smoke-test request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer remote.Close()

	input := mcpRequestLine(t, "rdev.support_session.smoke_test", map[string]any{
		"gateway_url":     remote.URL,
		"session_id":      "sess_remote",
		"timeout_seconds": 5,
		"remote_control":  true,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	audit := structured["capability_audit"].(map[string]any)
	if structured["remote_control_requested"] != true ||
		audit["remote_control_requested"] != true ||
		int(audit["remote_control_probe_count"].(float64)) != 2 {
		t.Fatalf("expected remote-control smoke metadata, got %#v", structured)
	}
	adapters := []string{}
	actions := []string{}
	for _, body := range created {
		adapters = append(adapters, body["adapter"].(string))
		if payload, _ := body["payload"].(map[string]any); payload != nil {
			if action, _ := payload["action"].(string); action != "" {
				actions = append(actions, action)
			}
		}
	}
	if !slices.Contains(adapters, "file") ||
		!slices.Contains(adapters, "desktop") ||
		!slices.Contains(actions, "list") ||
		!slices.Contains(actions, "window.inspect") {
		t.Fatalf("expected standard file/desktop smoke probes, adapters=%#v actions=%#v created=%#v", adapters, actions, created)
	}
}

func TestServerToolCallOldJobToolsAreUnknown(t *testing.T) {
	for _, tool := range []string{
		"rdev.jobs.policy_template",
		"rdev.jobs.create",
		"rdev.jobs.status",
		"rdev.jobs.cancel",
		"rdev.jobs.authorize",
	} {
		var out bytes.Buffer
		server := NewServer(gateway.NewMemoryGateway())
		if err := server.Serve(context.Background(), strings.NewReader(mcpRequestLine(t, tool, map[string]any{
			"job_id":           "job_old",
			"host_id":          "hst_old",
			"adapter":          "shell",
			"intent":           "old job contract",
			"policy":           map[string]any{},
			"authorization_id": "screen.screenshot",
			"decision":         "authorized",
			"idempotency_key":  "old",
			"capability":       "process.inspect",
			"timeout_seconds":  float64(1),
			"max_output_bytes": float64(1),
		})), &out); err != nil {
			t.Fatal(err)
		}
		lines := responseLines(t, out.String())
		errPayload, ok := lines[0]["error"].(map[string]any)
		if !ok || !strings.Contains(errPayload["message"].(string), "unknown tool") {
			t.Fatalf("old tool %s should be unknown, got %#v", tool, lines[0])
		}
	}
}

func TestServerToolCallOldFileAndDesktopToolsAreUnknown(t *testing.T) {
	for _, tool := range []string{
		"rdev.files.read",
		"rdev.files.upload",
		"rdev.files.delete",
		"rdev.desktop.screenshot",
		"rdev.desktop.clipboard",
	} {
		var out bytes.Buffer
		server := NewServer(gateway.NewMemoryGateway())
		if err := server.Serve(context.Background(), strings.NewReader(mcpRequestLine(t, tool, map[string]any{
			"gateway_url": "https://gateway.example.test",
			"host_id":     "hst_old",
			"path":        "README.md",
			"action":      "write",
			"text":        "old desktop tool",
		})), &out); err != nil {
			t.Fatal(err)
		}
		lines := responseLines(t, out.String())
		errPayload, ok := lines[0]["error"].(map[string]any)
		if !ok || !strings.Contains(errPayload["message"].(string), "unknown tool") {
			t.Fatalf("old tool %s should be unknown, got %#v", tool, lines[0])
		}
	}
}

func TestServerToolCallSupportSessionStatusWaitTimeout(t *testing.T) {
	input := mcpRequestLine(t, "rdev.support_session.status", map[string]any{
		"ticket_code":     "WAIT-1234",
		"locale":          "en",
		"wait":            true,
		"timeout_seconds": 1,
		"interval_millis": 100,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.support-session-status.v1" ||
		structured["connected"] != false ||
		structured["timed_out"] != true ||
		structured["ok"] != false {
		t.Fatalf("expected wait timeout status, got %#v", structured)
	}
	recovery := structured["connection_recovery"].(map[string]any)
	actions := strings.Join(anyStrings(recovery["agent_next_actions"].([]any)), "\n")
	forbidden := strings.Join(anyStrings(recovery["forbidden"].([]any)), "\n")
	if recovery["schema_version"] != "rdev.support-session-connection-recovery.v1" ||
		recovery["timed_out"] != true ||
		!strings.Contains(actions, "connection-entry failure") ||
		!strings.Contains(actions, "rdev.support_session.prepare") ||
		!strings.Contains(forbidden, "Agent-authored PowerShell") {
		t.Fatalf("expected timeout recovery contract, got %#v", recovery)
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

func TestServerToolCallScaffoldAcceptanceEvidence(t *testing.T) {
	root := t.TempDir()
	relayDir := filepath.Join(root, "relay-adapter")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "ssh-tunnel",
		GeneratedAt: time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(root, "scaffold")
	input := mcpRequestLine(t, "rdev.acceptance.scaffold_evidence", map[string]any{
		"relay_adapter_package": filepath.Join(relayDir, "relay-adapter.json"),
		"out_dir":               outDir,
		"create_placeholders":   true,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.acceptance-evidence-scaffold.v1" ||
		structured["plan_schema"] != "rdev.relay-adapter-acceptance-evidence-plan.v1" ||
		structured["plan_kind"] != "relay-adapter" ||
		structured["ready_for_packaging"] != false {
		t.Fatalf("unexpected scaffold payload: %#v", structured)
	}
	if _, err := os.Stat(filepath.Join(outDir, "AGENT_CHECKLIST.md")); err != nil {
		t.Fatalf("expected checklist: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(outDir, "runner-result.json")); err != nil || !strings.Contains(string(content), `"placeholder": true`) {
		t.Fatalf("expected placeholder runner result, err=%v content=%s", err, string(content))
	}

	statusInput := mcpRequestLine(t, "rdev.acceptance.evidence_status", map[string]any{
		"scaffold": outDir,
	})
	out.Reset()
	if err := server.Serve(context.Background(), strings.NewReader(statusInput), &out); err != nil {
		t.Fatal(err)
	}
	lines = responseLines(t, out.String())
	result = lines[0]["result"].(map[string]any)
	structured = result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.acceptance-evidence-status.v1" ||
		structured["ready_for_packaging"] != false ||
		structured["placeholder_count"].(float64) == 0 {
		t.Fatalf("unexpected evidence status payload: %#v", structured)
	}
}

func TestServerToolCallScaffoldPostReleaseDownloadEvidence(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "post-release-install-plan.json")
	planVerification := filepath.Join(root, "post-release-install-verification.json")
	if err := os.WriteFile(plan, []byte(`{
  "schema_version": "rdev.post-release-install-plan.v1",
  "repo": "EitanWong/remote-dev-skillkit",
  "tag": "v0.1.28-dev",
  "platforms": [{"target": "linux/amd64"}],
  "skillkit": {"archive": {"name": "remote-dev-skillkit.tar.gz"}}
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planVerification, []byte(`{"schema_version":"rdev.post-release-install-verification.v1","ok":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(root, "scaffold")
	input := mcpRequestLine(t, "rdev.acceptance.scaffold_post_release_download", map[string]any{
		"post_release_install_dir": root,
		"out_dir":                  outDir,
		"create_placeholders":      true,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.post-release-download-evidence-scaffold.v1" ||
		structured["ready_for_packaging"] != false ||
		structured["skillkit_included"] != true {
		t.Fatalf("unexpected post-release scaffold payload: %#v", structured)
	}
	statusInput := mcpRequestLine(t, "rdev.acceptance.post_release_evidence_status", map[string]any{
		"scaffold": outDir,
	})
	out.Reset()
	if err := server.Serve(context.Background(), strings.NewReader(statusInput), &out); err != nil {
		t.Fatal(err)
	}
	lines = responseLines(t, out.String())
	result = lines[0]["result"].(map[string]any)
	structured = result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.post-release-download-evidence-status.v1" ||
		structured["ready_for_packaging"] != false ||
		structured["placeholder_count"].(float64) == 0 {
		t.Fatalf("unexpected post-release status payload: %#v", structured)
	}
}

func TestServerToolCallReleaseEvidenceIndex(t *testing.T) {
	root := t.TempDir()
	input := mcpRequestLine(t, "rdev.acceptance.release_evidence_index", map[string]any{
		"out_dir": root,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.acceptance-release-evidence-index.v1" ||
		structured["ok"] != false {
		t.Fatalf("unexpected release evidence index payload: %#v", structured)
	}
	checks := structured["checks"].([]any)
	if len(checks) == 0 {
		t.Fatalf("expected missing-gate checks: %#v", structured)
	}
	if _, err := os.Stat(filepath.Join(root, "release-evidence-index.json")); err != nil {
		t.Fatalf("expected release evidence index file: %v", err)
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

func TestServerToolCallOldCreateJobEnvelopeToolIsUnknown(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rdev.jobs.create","arguments":{"host_id":"hst_old","adapter":"codex","intent":"fix tests","policy":{"workspace_root":"/repo","capabilities":["fs.read","fs.write.scoped","dev.codex"]}}}}` + "\n"
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	errPayload, ok := lines[0]["error"].(map[string]any)
	if !ok || !strings.Contains(errPayload["message"].(string), "unknown tool") {
		t.Fatalf("old rdev.jobs.create should be unknown, got %#v", lines[0])
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
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_authorizations": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": true},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_tasks": false,
    "adapter_authorizes_dangerous_actions": false,
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
  "task_id": "task_123",
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

func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func fileExistsForMCPTest(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
