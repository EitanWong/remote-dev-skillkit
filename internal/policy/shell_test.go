package policy

import (
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestExplainShellJobAllowsScopedCommand(t *testing.T) {
	explanation := ExplainShellJob(model.HostModeAttendedTemporary, map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user", "fs.write.scoped"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
		"write_scope":    []string{"."},
	})
	if !explanation.Allowed {
		t.Fatalf("expected shell job to be allowed: %#v", explanation)
	}
	if explanation.ApprovalRequired {
		t.Fatalf("expected no approval requirement: %#v", explanation)
	}
}

func TestExplainShellJobRejectsMissingWorkspace(t *testing.T) {
	explanation := ExplainShellJob(model.HostModeAttendedTemporary, map[string]any{
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if explanation.Allowed {
		t.Fatal("expected missing workspace to be denied")
	}
	if !containsReason(explanation.Denials, "workspace root is required") {
		t.Fatalf("unexpected denials: %#v", explanation.Denials)
	}
}

func TestExplainShellJobRejectsCommandNotAllowlisted(t *testing.T) {
	explanation := ExplainShellJob(model.HostModeAttendedTemporary, map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"git"},
	})
	if explanation.Allowed {
		t.Fatal("expected non-allowlisted command to be denied")
	}
	if !containsReason(explanation.Denials, "not allowlisted") {
		t.Fatalf("unexpected denials: %#v", explanation.Denials)
	}
}

func TestExplainShellJobRejectsEscapingWriteScope(t *testing.T) {
	explanation := ExplainShellJob(model.HostModeAttendedTemporary, map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user", "fs.write.scoped"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
		"write_scope":    []string{".."},
	})
	if explanation.Allowed {
		t.Fatal("expected escaping write scope to be denied")
	}
	if !containsReason(explanation.Denials, "escapes workspace root") {
		t.Fatalf("unexpected denials: %#v", explanation.Denials)
	}
}

func TestExplainShellJobRejectsWindowsEscapingWriteScope(t *testing.T) {
	explanation := ExplainShellJob(model.HostModeAttendedTemporary, map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user", "fs.write.scoped"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
		"write_scope":    []string{`..\outside`},
	})
	if explanation.Allowed {
		t.Fatal("expected Windows escaping write scope to be denied")
	}
	if !containsReason(explanation.Denials, "escapes workspace root") {
		t.Fatalf("unexpected denials: %#v", explanation.Denials)
	}
}

func TestExplainShellJobRequiresApprovalForNetwork(t *testing.T) {
	explanation := ExplainShellJob(model.HostModeAttendedTemporary, map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
		"network":        "egress",
	})
	if !explanation.Allowed {
		t.Fatalf("network warning should not deny by itself: %#v", explanation)
	}
	if !explanation.ApprovalRequired {
		t.Fatal("non-default network should require approval")
	}
	if !containsReason(explanation.RequiredApprovals, "network.egress") {
		t.Fatalf("unexpected approvals: %#v", explanation.RequiredApprovals)
	}
}

func TestExplainPowerShellJobAllowsStandardWindowsProbe(t *testing.T) {
	explanation := ExplainAdapterJob(model.HostModeAttendedTemporary, "powershell", map[string]any{
		"workspace_root":       ".",
		"capabilities":         []string{"powershell.user"},
		"command":              "Write-Output $env:COMPUTERNAME; whoami; Get-Location",
		"allow_commands":       []string{"powershell.exe", "powershell", "pwsh"},
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	})
	if !explanation.Allowed || explanation.Adapter != "powershell" {
		t.Fatalf("expected powershell job to be allowed: %#v", explanation)
	}
	if explanation.ApprovalRequired {
		t.Fatalf("expected no approval requirement: %#v", explanation)
	}
}

func TestExplainPowerShellJobRejectsMissingPowerShellCapability(t *testing.T) {
	explanation := ExplainAdapterJob(model.HostModeAttendedTemporary, "powershell", map[string]any{
		"workspace_root":       ".",
		"capabilities":         []string{"shell.user"},
		"command":              "Get-Location",
		"allow_commands":       []string{"powershell.exe", "powershell", "pwsh"},
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
	})
	if explanation.Allowed {
		t.Fatal("expected missing powershell.user to be denied")
	}
	if !containsReason(explanation.Denials, "missing powershell.user") {
		t.Fatalf("unexpected denials: %#v", explanation.Denials)
	}
}

func TestExplainFileJobAllowsScopedRead(t *testing.T) {
	explanation := ExplainAdapterJob(model.HostModeAttendedTemporary, "file", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"file.transfer.read", "fs.read"},
		"action":         "read",
		"path":           "README.md",
	})
	if !explanation.Allowed || explanation.Adapter != "file" {
		t.Fatalf("expected file read to be allowed: %#v", explanation)
	}
	if explanation.ApprovalRequired {
		t.Fatalf("expected no approval for scoped file read: %#v", explanation)
	}
}

func TestExplainFileJobRejectsWriteWithoutScope(t *testing.T) {
	explanation := ExplainAdapterJob(model.HostModeAttendedTemporary, "file", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"file.transfer.write", "fs.write.scoped"},
		"action":         "write",
		"path":           "notes.txt",
	})
	if explanation.Allowed {
		t.Fatal("expected file write without write_scope to be denied")
	}
	if !containsReason(explanation.Denials, "write_scope is required") {
		t.Fatalf("unexpected denials: %#v", explanation.Denials)
	}
}

func TestExplainFileJobRequiresApprovalForDelete(t *testing.T) {
	explanation := ExplainAdapterJob(model.HostModeAttendedTemporary, "file", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"file.transfer.write", "fs.write.scoped"},
		"action":         "delete",
		"path":           "old.txt",
		"write_scope":    []string{"."},
	})
	if !explanation.Allowed {
		t.Fatalf("expected scoped delete to be available behind approval: %#v", explanation)
	}
	if !explanation.ApprovalRequired || !containsReason(explanation.RequiredApprovals, "file.delete") {
		t.Fatalf("expected file.delete approval: %#v", explanation)
	}
}

func TestExplainDesktopJobRequiresApprovalForScreenshot(t *testing.T) {
	explanation := ExplainAdapterJob(model.HostModeAttendedTemporary, "desktop", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"screen.screenshot"},
		"action":         "screen.screenshot",
	})
	if !explanation.Allowed {
		t.Fatalf("expected screenshot to be available behind approval: %#v", explanation)
	}
	if !explanation.ApprovalRequired || !containsReason(explanation.RequiredApprovals, "screen.screenshot") {
		t.Fatalf("expected screenshot approval requirement: %#v", explanation)
	}
}

func TestExplainDesktopJobAllowsWindowInspectWithoutApproval(t *testing.T) {
	explanation := ExplainAdapterJob(model.HostModeAttendedTemporary, "desktop", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"window.inspect"},
		"action":         "window.inspect",
	})
	if !explanation.Allowed {
		t.Fatalf("expected window inspect to be allowed: %#v", explanation)
	}
	if explanation.ApprovalRequired {
		t.Fatalf("expected no approval for window inspect: %#v", explanation)
	}
}

func containsReason(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
