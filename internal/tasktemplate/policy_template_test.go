package tasktemplate

import "testing"

func TestPolicyTemplateFileRead(t *testing.T) {
	template := PolicyTemplate("file.transfer.read", "windows")
	if template["adapter"] != "file" {
		t.Fatalf("expected file adapter, got %#v", template["adapter"])
	}
	policy := template["policy"].(map[string]any)
	if policy["action"] != "read" {
		t.Fatalf("expected read action, got %#v", policy)
	}
}

func TestPolicyTemplatePowerShellUsesCommandSchema(t *testing.T) {
	template := PolicyTemplate("powershell.user", "windows")
	if template["adapter"] != "powershell" {
		t.Fatalf("expected powershell adapter, got %#v", template["adapter"])
	}
	policy := template["policy"].(map[string]any)
	if _, ok := policy["command"].(string); !ok {
		t.Fatalf("expected PowerShell command field, got %#v", policy)
	}
	if _, ok := policy["argv"]; ok {
		t.Fatalf("PowerShell template must not use shell argv field: %#v", policy)
	}
}

func TestPolicyTemplateSessionTaskExampleUsesTemplateCapabilities(t *testing.T) {
	template := PolicyTemplate("process.inspect", "windows")
	policy := template["policy"].(map[string]any)
	example := template["session_task_example"].(map[string]any)
	arguments := example["arguments"].(map[string]any)
	policyCapabilities := policy["capabilities"].([]string)
	exampleCapabilities := arguments["capabilities"].([]string)
	if len(policyCapabilities) != len(exampleCapabilities) {
		t.Fatalf("expected matching capabilities, policy=%#v example=%#v", policyCapabilities, exampleCapabilities)
	}
	for index := range policyCapabilities {
		if policyCapabilities[index] != exampleCapabilities[index] {
			t.Fatalf("expected matching capabilities, policy=%#v example=%#v", policyCapabilities, exampleCapabilities)
		}
	}
}

func TestPolicyTemplateUsesHomeWorkspaceRoot(t *testing.T) {
	for _, capability := range []string{"powershell.user", "file.transfer.read", "file.transfer.write", "fs.write.scoped"} {
		t.Run(capability, func(t *testing.T) {
			template := PolicyTemplate(capability, "windows")
			policy := template["policy"].(map[string]any)
			if policy["workspace_root"] != "~" {
				t.Fatalf("expected workspace_root to default to user home, got %#v in %#v", policy["workspace_root"], policy)
			}
		})
	}
}

func TestPolicyTemplateDesktopActionsDoNotRequireTaskAuthorization(t *testing.T) {
	for _, capability := range []string{
		"screen.screenshot",
		"screen.record",
		"window.inspect",
		"window.focus",
		"window.move",
		"input.keyboard",
		"input.mouse",
		"app.launch",
		"app.close",
		"url.open",
		"clipboard.read",
		"clipboard.write",
	} {
		t.Run(capability, func(t *testing.T) {
			template := PolicyTemplate(capability, "windows")
			if template["adapter"] != "desktop" {
				t.Fatalf("expected desktop adapter, got %#v", template["adapter"])
			}
			policy := template["policy"].(map[string]any)
			if _, ok := policy["authorizations_required"]; ok {
				t.Fatalf("expected no GUI task authorization, got %#v", policy)
			}
			if policy["max_output_bytes"] != 65536 {
				t.Fatalf("expected GUI artifact budget, got %#v", policy["max_output_bytes"])
			}
		})
	}
}

func TestPolicyTemplateFileDeleteRequiresAuthorization(t *testing.T) {
	template := PolicyTemplate("file.delete", "windows")
	if template["adapter"] != "file" {
		t.Fatalf("expected file adapter, got %#v", template["adapter"])
	}
	policy := template["policy"].(map[string]any)
	authorizations := policy["authorizations_required"].([]string)
	if policy["action"] != "delete" || len(authorizations) != 1 || authorizations[0] != "file.delete" {
		t.Fatalf("expected delete authorization template, got %#v", policy)
	}
}
