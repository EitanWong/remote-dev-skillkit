package policy

import (
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestExplainShellTaskAllowsScopedCommand(t *testing.T) {
	explanation := ExplainShellTask(model.HostModeAttendedTemporary, map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user", "fs.write.scoped"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
		"write_scope":    []string{"."},
	})
	if !explanation.Allowed {
		t.Fatalf("expected shell task to be allowed: %#v", explanation)
	}
	if explanation.AuthorizationRequired {
		t.Fatalf("expected no authorization requirement: %#v", explanation)
	}
}

func TestExplainShellTaskRejectsMissingWorkspace(t *testing.T) {
	explanation := ExplainShellTask(model.HostModeAttendedTemporary, map[string]any{
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

func TestExplainShellTaskRejectsCommandNotAllowlisted(t *testing.T) {
	explanation := ExplainShellTask(model.HostModeAttendedTemporary, map[string]any{
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

func TestExplainShellTaskRejectsEscapingWriteScope(t *testing.T) {
	explanation := ExplainShellTask(model.HostModeAttendedTemporary, map[string]any{
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

func TestExplainShellTaskRejectsWindowsEscapingWriteScope(t *testing.T) {
	explanation := ExplainShellTask(model.HostModeAttendedTemporary, map[string]any{
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

func TestExplainShellTaskRequiresAuthorizationForNetwork(t *testing.T) {
	explanation := ExplainShellTask(model.HostModeAttendedTemporary, map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
		"network":        "egress",
	})
	if !explanation.Allowed {
		t.Fatalf("network warning should not deny by itself: %#v", explanation)
	}
	if !explanation.AuthorizationRequired {
		t.Fatal("non-default network should require authorization")
	}
	if !containsReason(explanation.RequiredAuthorizations, "network.egress") {
		t.Fatalf("unexpected authorizations: %#v", explanation.RequiredAuthorizations)
	}
}

func TestExplainPowerShellTaskAllowsStandardWindowsProbe(t *testing.T) {
	explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "powershell", map[string]any{
		"workspace_root":       ".",
		"capabilities":         []string{"powershell.user"},
		"command":              "Write-Output $env:COMPUTERNAME; whoami; Get-Location",
		"allow_commands":       []string{"powershell.exe", "powershell", "pwsh"},
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	})
	if !explanation.Allowed || explanation.Adapter != "powershell" {
		t.Fatalf("expected powershell task to be allowed: %#v", explanation)
	}
	if explanation.AuthorizationRequired {
		t.Fatalf("expected no authorization requirement: %#v", explanation)
	}
}

func TestExplainPowerShellTaskRejectsMissingPowerShellCapability(t *testing.T) {
	explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "powershell", map[string]any{
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

func TestExplainPowerShellTaskExplainsArgvSchemaMismatch(t *testing.T) {
	explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "powershell", map[string]any{
		"workspace_root":       ".",
		"capabilities":         []string{"powershell.user"},
		"argv":                 []string{"powershell.exe", "-NoProfile", "-Command", "Get-Location"},
		"allow_commands":       []string{"powershell.exe", "powershell", "pwsh"},
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
	})
	if explanation.Allowed {
		t.Fatal("expected powershell argv-only policy to be denied")
	}
	if !containsReason(explanation.Denials, "PowerShell adapter expects policy.command or policy.script; policy.argv is for shell adapter") {
		t.Fatalf("expected schema guidance for argv mismatch, got %#v", explanation.Denials)
	}
}

func TestExplainFileTaskAllowsScopedRead(t *testing.T) {
	explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "file", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"file.transfer.read", "fs.read"},
		"action":         "read",
		"path":           "README.md",
	})
	if !explanation.Allowed || explanation.Adapter != "file" {
		t.Fatalf("expected file read to be allowed: %#v", explanation)
	}
	if explanation.AuthorizationRequired {
		t.Fatalf("expected no authorization for scoped file read: %#v", explanation)
	}
}

func TestExplainFileTaskRejectsWriteWithoutScope(t *testing.T) {
	explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "file", map[string]any{
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

func TestExplainFileTaskRequiresAuthorizationForDelete(t *testing.T) {
	explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "file", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"file.transfer.write", "fs.write.scoped"},
		"action":         "delete",
		"path":           "old.txt",
		"write_scope":    []string{"."},
	})
	if !explanation.Allowed {
		t.Fatalf("expected scoped delete to be available behind authorization: %#v", explanation)
	}
	if !explanation.AuthorizationRequired || !containsReason(explanation.RequiredAuthorizations, "file.delete") {
		t.Fatalf("expected file.delete authorization: %#v", explanation)
	}
}

func TestExplainDesktopTasksDoNotRequireTaskAuthorization(t *testing.T) {
	for _, action := range []string{
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
		t.Run(action, func(t *testing.T) {
			capability, _ := desktopCapabilityAndAuthorization(action)
			explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "desktop", map[string]any{
				"workspace_root": ".",
				"capabilities":   []string{capability},
				"action":         action,
			})
			if !explanation.Allowed {
				t.Fatalf("expected desktop action to be allowed: %#v", explanation)
			}
			if explanation.AuthorizationRequired {
				t.Fatalf("expected no task authorization for %s: %#v", action, explanation)
			}
		})
	}
}

func TestExplainDesktopTaskAllowsScreenshotWithoutAuthorization(t *testing.T) {
	explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "desktop", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"screen.screenshot"},
		"action":         "screen.screenshot",
	})
	if !explanation.Allowed {
		t.Fatalf("expected screenshot to be allowed: %#v", explanation)
	}
	if explanation.AuthorizationRequired {
		t.Fatalf("expected no screenshot authorization requirement: %#v", explanation)
	}
}

func TestExplainDesktopTaskAllowsWindowInspectWithoutAuthorization(t *testing.T) {
	explanation := ExplainAdapterTask(model.HostModeAttendedTemporary, "desktop", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"window.inspect"},
		"action":         "window.inspect",
	})
	if !explanation.Allowed {
		t.Fatalf("expected window inspect to be allowed: %#v", explanation)
	}
	if explanation.AuthorizationRequired {
		t.Fatalf("expected no authorization for window inspect: %#v", explanation)
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
