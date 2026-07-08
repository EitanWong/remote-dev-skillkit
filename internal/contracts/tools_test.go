package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestToolsHaveUniqueNamesAndSchemas(t *testing.T) {
	seen := map[string]bool{}
	tools := Tools()
	for _, tool := range tools {
		if tool.Name == "" {
			t.Fatal("tool name must not be empty")
		}
		if seen[tool.Name] {
			t.Fatalf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Description == "" {
			t.Fatalf("tool %s missing description", tool.Name)
		}
		if tool.Safety == "" {
			t.Fatalf("tool %s missing safety note", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %s missing input schema", tool.Name)
		}
	}
	if !seen["rdev.adapter.verify_result"] {
		t.Fatal("expected adapter result verification tool")
	}
	if !seen["rdev.enrollment.verify_certificate"] {
		t.Fatal("expected enrollment certificate verification tool")
	}
	if !seen["rdev.adapter.verify_lifecycle"] {
		t.Fatal("expected adapter lifecycle verification tool")
	}
	if !seen["rdev.adapter.verify_cancellation"] {
		t.Fatal("expected adapter cancellation verification tool")
	}
	if !seen["rdev.invites.create"] {
		t.Fatal("expected agent-first invite creation tool")
	}
	if !seen["rdev.support_session.handoff"] {
		t.Fatal("expected support session handoff tool")
	}
	if !seen["rdev.support_session.report"] {
		t.Fatal("expected support session report tool")
	}
	if !seen["rdev.support_session.smoke_test"] {
		t.Fatal("expected support session smoke-test tool")
	}
	if !seen["rdev.jobs.policy_template"] {
		t.Fatal("expected job policy template tool")
	}
	if !seen["rdev.update.check"] || !seen["rdev.update.plan"] {
		t.Fatal("expected update check and plan tools")
	}
}

func TestSupportSessionConnectIsFirstTool(t *testing.T) {
	tools := Tools()
	if len(tools) == 0 {
		t.Fatal("expected tools")
	}
	if tools[0].Name != "rdev.support_session.connect" {
		t.Fatalf("fresh-agent tool list must lead with high-level connect, got %q", tools[0].Name)
	}
	for index, tool := range tools {
		if tool.Name == "rdev.invites.create" && index == 0 {
			t.Fatal("low-level invite tool must not be the first tool for fresh Agents")
		}
	}
}

func TestGatewayBackedToolsExposeGatewayURL(t *testing.T) {
	want := []string{
		"rdev.support_session.status",
		"rdev.support_session.report",
		"rdev.support_session.smoke_test",
		"rdev.hosts.list",
		"rdev.hosts.capabilities",
		"rdev.hosts.approve",
		"rdev.hosts.revoke",
		"rdev.jobs.create",
		"rdev.jobs.status",
		"rdev.jobs.cancel",
		"rdev.jobs.approve",
		"rdev.artifacts.list",
		"rdev.artifacts.read",
		"rdev.audit.query",
	}
	for _, name := range want {
		tool := findTool(name)
		if tool == nil {
			t.Fatalf("missing tool %s", name)
		}
		properties, _ := tool.InputSchema["properties"].(map[string]any)
		if _, ok := properties["gateway_url"]; !ok {
			t.Fatalf("tool %s must expose gateway_url in live MCP schema: %#v", name, properties)
		}
	}
}

func TestToolDescriptionsDoNotPromoteGatewayCandidateAssembly(t *testing.T) {
	for _, tool := range Tools() {
		forbidden := []string{
			"recommended gateway_url_candidates entry",
			"use the returned gateway_url_candidates",
			"use gateway_url_candidates",
			"turn gateway_url_candidates",
		}
		for _, text := range forbidden {
			if strings.Contains(tool.Description, text) {
				t.Fatalf("tool %s description promotes gateway candidate assembly with %q: %s", tool.Name, text, tool.Description)
			}
		}
	}
}

func TestSupportSessionReportSchemaAcceptsTicketCodeOrHostID(t *testing.T) {
	tool := findTool("rdev.support_session.report")
	if tool == nil {
		t.Fatal("missing rdev.support_session.report")
	}
	if !strings.Contains(tool.Description, "remote_control_entry") ||
		!strings.Contains(tool.Description, "disconnect automatically") {
		t.Fatalf("report description should expose remote_control_entry and no-auto-disconnect policy: %s", tool.Description)
	}
	properties, _ := tool.InputSchema["properties"].(map[string]any)
	if _, ok := properties["host_id"]; !ok {
		t.Fatalf("report schema must keep host_id: %#v", properties)
	}
	if _, ok := properties["ticket_code"]; !ok {
		t.Fatalf("report schema must expose ticket_code: %#v", properties)
	}
	required, _ := tool.InputSchema["required"].([]string)
	if slices.Contains(required, "host_id") || slices.Contains(required, "ticket_code") {
		t.Fatalf("report schema should allow either host_id or ticket_code, got required=%#v", required)
	}
}

func TestSupportSessionSmokeTestSchemaAcceptsTicketCodeOrHostID(t *testing.T) {
	tool := findTool("rdev.support_session.smoke_test")
	if tool == nil {
		t.Fatal("missing rdev.support_session.smoke_test")
	}
	if !strings.Contains(tool.Description, "remote_control_entry") ||
		!strings.Contains(tool.Description, "keep the host connected") ||
		!strings.Contains(tool.Description, "remote_control=true") {
		t.Fatalf("smoke-test description should expose remote_control_entry and keep-alive policy: %s", tool.Description)
	}
	properties, _ := tool.InputSchema["properties"].(map[string]any)
	for _, name := range []string{"host_id", "ticket_code", "gateway_url", "timeout_seconds", "remote_control"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("smoke-test schema must expose %s: %#v", name, properties)
		}
	}
	required, _ := tool.InputSchema["required"].([]string)
	if slices.Contains(required, "host_id") || slices.Contains(required, "ticket_code") {
		t.Fatalf("smoke-test schema should allow either host_id or ticket_code, got required=%#v", required)
	}
}

func TestStaticMCPToolsJSONMatchesLiveContract(t *testing.T) {
	staticPath := filepath.Join("..", "..", "mcp", "tools.json")
	data, err := os.ReadFile(staticPath)
	if err != nil {
		t.Fatalf("read static MCP tools contract: %v", err)
	}
	var staticPayload struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(data, &staticPayload); err != nil {
		t.Fatalf("parse static MCP tools contract: %v", err)
	}
	if len(staticPayload.Tools) == 0 {
		t.Fatal("static MCP tools contract must contain tools")
	}
	livePayload := struct {
		Tools []Tool `json:"tools"`
	}{Tools: Tools()}
	liveData, err := json.Marshal(livePayload)
	if err != nil {
		t.Fatalf("marshal live MCP tools contract: %v", err)
	}
	var normalizedLive struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(liveData, &normalizedLive); err != nil {
		t.Fatalf("normalize live MCP tools contract: %v", err)
	}
	if !reflect.DeepEqual(staticPayload.Tools, normalizedLive.Tools) {
		t.Fatalf("mcp/tools.json is stale; regenerate it with `rdev mcp tools`")
	}
}

func TestJobsCreateAdapterEnumIncludesClaudeCode(t *testing.T) {
	for _, tool := range Tools() {
		if tool.Name != "rdev.jobs.create" {
			continue
		}
		properties, _ := tool.InputSchema["properties"].(map[string]any)
		adapterSchema, _ := properties["adapter"].(map[string]any)
		values, _ := adapterSchema["enum"].([]any)
		if !containsEnum(values, "claude-code") {
			t.Fatalf("jobs.create adapter enum should include claude-code: %#v", values)
		}
		if containsEnum(values, "claude") {
			t.Fatalf("jobs.create adapter enum should use claude-code, not ambiguous claude: %#v", values)
		}
		return
	}
	t.Fatal("missing rdev.jobs.create tool")
}

func findTool(name string) *Tool {
	for _, tool := range Tools() {
		if tool.Name == name {
			return &tool
		}
	}
	return nil
}

func containsEnum(values []any, want string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}
