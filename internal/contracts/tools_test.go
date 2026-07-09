package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	if seen["rdev.adapter.verify_result"] {
		t.Fatal("old adapter verification tools must not be part of Control Plane v1 MCP")
	}
	wantSessionTools(t, seen)
}

func TestSessionsCreateIsFirstTool(t *testing.T) {
	tools := Tools()
	if len(tools) == 0 {
		t.Fatal("expected tools")
	}
	if tools[0].Name != "rdev.sessions.create" {
		t.Fatalf("Control Plane v1 MCP must lead with sessions.create, got %q", tools[0].Name)
	}
}

func TestSessionToolsExposeGatewayURLAndAgentFields(t *testing.T) {
	for _, name := range sessionToolNames() {
		tool := findTool(name)
		if tool == nil {
			t.Fatalf("missing tool %s", name)
		}
		properties, _ := tool.InputSchema["properties"].(map[string]any)
		if _, ok := properties["gateway_url"]; !ok {
			t.Fatalf("session tool %s must expose gateway_url in live MCP schema: %#v", name, properties)
		}
		if !strings.Contains(tool.Description, "agent_next_action") ||
			!strings.Contains(tool.Description, "user_summary") {
			t.Fatalf("session tool %s must describe Agent-native recovery fields: %s", name, tool.Description)
		}
	}
}

func TestToolsDoNotExposeOldExperimentalHostJobContracts(t *testing.T) {
	forbidden := []string{
		"rdev.hosts.list",
		"rdev.hosts.capabilities",
		"rdev.hosts.authorize",
		"rdev.hosts.revoke",
		"rdev.jobs.create",
		"rdev.jobs.policy_template",
		"rdev.jobs.status",
		"rdev.jobs.cancel",
		"rdev.jobs.authorize",
		"rdev.artifacts.list",
		"rdev.artifacts.read",
	}
	for _, tool := range Tools() {
		for _, name := range forbidden {
			if tool.Name == name {
				t.Fatalf("old experimental MCP tool %s must be absent from Control Plane v1", name)
			}
		}
		forbiddenText := []string{
			"recommended gateway_url_candidates entry",
			"use the returned gateway_url_candidates",
			"use gateway_url_candidates",
			"turn gateway_url_candidates",
			"authorization_id",
			"jobs.authorize",
			"host registration",
		}
		for _, text := range forbiddenText {
			if strings.Contains(tool.Description, text) {
				t.Fatalf("tool %s description contains old contract wording %q: %s", tool.Name, text, tool.Description)
			}
		}
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

func findTool(name string) *Tool {
	for _, tool := range Tools() {
		if tool.Name == name {
			return &tool
		}
	}
	return nil
}

func wantSessionTools(t *testing.T, seen map[string]bool) {
	t.Helper()
	for _, name := range sessionToolNames() {
		if !seen[name] {
			t.Fatalf("missing Control Plane v1 session tool %s", name)
		}
	}
}

func sessionToolNames() []string {
	return []string{
		"rdev.sessions.create",
		"rdev.sessions.status",
		"rdev.sessions.events",
		"rdev.sessions.task",
		"rdev.sessions.interrupt",
		"rdev.sessions.artifacts",
		"rdev.sessions.close",
	}
}
