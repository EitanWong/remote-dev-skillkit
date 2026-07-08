package jobtemplate

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

func TestPolicyTemplateDesktopScreenshotRequiresApproval(t *testing.T) {
	template := PolicyTemplate("screen.screenshot", "windows")
	if template["adapter"] != "desktop" {
		t.Fatalf("expected desktop adapter, got %#v", template["adapter"])
	}
	policy := template["policy"].(map[string]any)
	approvals := policy["approvals_required"].([]string)
	if len(approvals) != 1 || approvals[0] != "screen.screenshot" {
		t.Fatalf("expected screenshot approval, got %#v", policy)
	}
}

func TestPolicyTemplateFileDeleteRequiresApproval(t *testing.T) {
	template := PolicyTemplate("file.delete", "windows")
	if template["adapter"] != "file" {
		t.Fatalf("expected file adapter, got %#v", template["adapter"])
	}
	policy := template["policy"].(map[string]any)
	approvals := policy["approvals_required"].([]string)
	if policy["action"] != "delete" || len(approvals) != 1 || approvals[0] != "file.delete" {
		t.Fatalf("expected delete approval template, got %#v", policy)
	}
}
