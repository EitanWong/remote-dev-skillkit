package supportsession

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildLiveE2EPlanHostIDOnlyOmitsTicketCodePlaceholder(t *testing.T) {
	plan := BuildLiveE2EPlan(LiveE2EPlanOptions{
		GatewayURL:  "https://gateway.example.test/rdev",
		HostID:      "hst_1",
		RdevCommand: "rdev-test",
	})
	smoke := liveE2EGateByName(t, plan, "windows_support_session_smoke_remote_control")
	command := liveE2EStringSlice(t, smoke["proof_command"])
	joined := strings.Join(command, " ")

	if !slices.Contains(command, "--host-id") || !slices.Contains(command, "hst_1") {
		t.Fatalf("expected host-id selector in smoke proof command, got %#v", command)
	}
	if slices.Contains(command, "--ticket-code") || strings.Contains(joined, "<ticket-code>") {
		t.Fatalf("host-id-only smoke proof command should not include ticket-code placeholders: %#v", command)
	}
	mcpArgs := liveE2EMap(t, smoke["mcp_arguments"])
	if mcpArgs["host_id"] != "hst_1" {
		t.Fatalf("expected host_id MCP argument, got %#v", mcpArgs)
	}
	if _, ok := mcpArgs["ticket_code"]; ok {
		t.Fatalf("host-id-only MCP arguments should omit ticket_code, got %#v", mcpArgs)
	}
}

func TestBuildLiveE2EPlanTicketCodeOnlyOmitsHostIDPlaceholder(t *testing.T) {
	plan := BuildLiveE2EPlan(LiveE2EPlanOptions{
		GatewayURL:  "https://gateway.example.test/rdev",
		TicketCode:  "TICKET-1",
		RdevCommand: "rdev-test",
	})
	smoke := liveE2EGateByName(t, plan, "windows_support_session_smoke_remote_control")
	command := liveE2EStringSlice(t, smoke["proof_command"])
	joined := strings.Join(command, " ")

	if !slices.Contains(command, "--ticket-code") || !slices.Contains(command, "TICKET-1") {
		t.Fatalf("expected ticket-code selector in smoke proof command, got %#v", command)
	}
	if slices.Contains(command, "--host-id") || strings.Contains(joined, "<host-id>") {
		t.Fatalf("ticket-code-only smoke proof command should not include host-id placeholders: %#v", command)
	}
	mcpArgs := liveE2EMap(t, smoke["mcp_arguments"])
	if mcpArgs["ticket_code"] != "TICKET-1" {
		t.Fatalf("expected ticket_code MCP argument, got %#v", mcpArgs)
	}
	if _, ok := mcpArgs["host_id"]; ok {
		t.Fatalf("ticket-code-only MCP arguments should omit host_id, got %#v", mcpArgs)
	}
}

func liveE2EGateByName(t *testing.T, plan map[string]any, name string) map[string]any {
	t.Helper()
	gates, ok := plan["gates"].([]map[string]any)
	if !ok {
		t.Fatalf("expected gates list, got %#v", plan["gates"])
	}
	for _, gate := range gates {
		if gate["name"] == name {
			return gate
		}
	}
	t.Fatalf("missing gate %q in %#v", name, gates)
	return nil
}

func liveE2EStringSlice(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]string)
	if !ok {
		t.Fatalf("expected []string, got %#v", value)
	}
	return items
}

func liveE2EMap(t *testing.T, value any) map[string]any {
	t.Helper()
	items, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %#v", value)
	}
	return items
}
