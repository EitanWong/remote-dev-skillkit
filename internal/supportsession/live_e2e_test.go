package supportsession

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildLiveE2EPlanUsesSmokeTestSupportedSelectors(t *testing.T) {
	plan := BuildLiveE2EPlan(LiveE2EPlanOptions{
		GatewayURL: "https://gateway.example.test/rdev",
		TicketCode: "TICKET-1",
		HostID:     "hst_1",
	})
	smoke := liveE2EGateByName(t, plan, "windows_support_session_smoke_remote_control")
	command := liveE2EStringSlice(t, smoke["proof_command"])

	if slices.Contains(command, "--host-id") {
		t.Fatalf("smoke-test does not accept --host-id: %#v", command)
	}
	if !slices.Contains(command, "--session-id") || !slices.Contains(command, "<session-id>") {
		t.Fatalf("expected required session-id selector in smoke proof command, got %#v", command)
	}
}

func TestBuildLiveE2EPlanHostIDOnlyUsesSessionPlaceholderForSmoke(t *testing.T) {
	plan := BuildLiveE2EPlan(LiveE2EPlanOptions{
		GatewayURL:  "https://gateway.example.test/rdev",
		HostID:      "hst_1",
		RdevCommand: "rdev-test",
	})
	smoke := liveE2EGateByName(t, plan, "windows_support_session_smoke_remote_control")
	command := liveE2EStringSlice(t, smoke["proof_command"])
	joined := strings.Join(command, " ")

	if slices.Contains(command, "--host-id") || slices.Contains(command, "hst_1") {
		t.Fatalf("host ID is not a valid smoke-test selector: %#v", command)
	}
	if slices.Contains(command, "--ticket-code") || strings.Contains(joined, "<ticket-code>") {
		t.Fatalf("host-id-only smoke proof command should not include ticket-code placeholders: %#v", command)
	}
	if !slices.Contains(command, "--session-id") || !slices.Contains(command, "<session-id>") {
		t.Fatalf("host-id-only plan should require a Control Plane session for smoke-test: %#v", command)
	}
	mcpArgs := liveE2EMap(t, smoke["mcp_arguments"])
	if smoke["proof_interface"] != "cli-only" || smoke["mcp_tool"] != "" || len(mcpArgs) != 0 {
		t.Fatalf("smoke-test proof must remain CLI-only, got %#v", smoke)
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
	if !slices.Contains(command, "--session-id") || !slices.Contains(command, "<session-id>") {
		t.Fatalf("ticket-code-only plan should require a Control Plane session for smoke-test: %#v", command)
	}
	mcpArgs := liveE2EMap(t, smoke["mcp_arguments"])
	if smoke["proof_interface"] != "cli-only" || smoke["mcp_tool"] != "" || len(mcpArgs) != 0 {
		t.Fatalf("smoke-test proof must remain CLI-only, got %#v", smoke)
	}
}

func TestBuildLiveE2EPlanDoesNotOverclaimInterruptCancellation(t *testing.T) {
	plan := BuildLiveE2EPlan(LiveE2EPlanOptions{})
	interrupt := liveE2EGateByName(t, plan, "windows_session_interrupt_flow")
	agentRule, _ := interrupt["agent_rule"].(string)
	evidence := liveE2EStringSlice(t, interrupt["required_evidence"])

	if !strings.Contains(agentRule, "does not prove process cancellation") {
		t.Fatalf("interrupt gate must state the current cancellation limitation: %#v", interrupt)
	}
	if !slices.Contains(evidence, "process_cancellation_proven=false") {
		t.Fatalf("interrupt gate must require an explicit non-cancellation result: %#v", evidence)
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
