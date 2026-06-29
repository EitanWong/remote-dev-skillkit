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
}
