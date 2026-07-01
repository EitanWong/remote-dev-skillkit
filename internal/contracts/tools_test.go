package contracts

import "testing"

func TestToolsHaveUniqueNamesAndSchemas(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range Tools() {
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
	if !seen["rdev.update.check"] || !seen["rdev.update.plan"] {
		t.Fatal("expected update check and plan tools")
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

func containsEnum(values []any, want string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}
