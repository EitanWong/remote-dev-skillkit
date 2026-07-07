package jobtemplate

import "testing"

func TestPolicyTemplatePowerShellUserUsesPowerShellAdapter(t *testing.T) {
	payload := PolicyTemplate("powershell.user", "windows")
	if payload["schema_version"] != "rdev.job-policy-template.v1" ||
		payload["capability"] != "powershell.user" ||
		payload["adapter"] != "powershell" {
		t.Fatalf("unexpected PowerShell template metadata: %#v", payload)
	}
	policy, _ := payload["policy"].(map[string]any)
	if policy == nil {
		t.Fatalf("missing policy object: %#v", payload)
	}
	if _, ok := policy["command"].(string); !ok {
		t.Fatalf("PowerShell template must use command, got %#v", policy)
	}
	if _, ok := policy["argv"]; ok {
		t.Fatalf("PowerShell template must not use shell argv, got %#v", policy)
	}
	caps, _ := policy["capabilities"].([]string)
	if len(caps) != 1 || caps[0] != "powershell.user" {
		t.Fatalf("PowerShell template must require powershell.user, got %#v", caps)
	}
}
