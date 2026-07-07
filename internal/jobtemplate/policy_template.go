package jobtemplate

import "strings"

// PolicyTemplate returns a small, safe starter policy for common support-session
// probes. It is shared by CLI and MCP so fresh Agents see one contract.
func PolicyTemplate(capability, targetOS string) map[string]any {
	capability = strings.ToLower(strings.TrimSpace(capability))
	targetOS = strings.ToLower(strings.TrimSpace(targetOS))
	if targetOS == "macos" || targetOS == "darwin" {
		targetOS = "unix"
	}
	if targetOS == "linux" {
		targetOS = "unix"
	}
	windows := targetOS == "windows"
	cmd := []string{"sh", "-c", "hostname && whoami && pwd"}
	allow := []string{"sh"}
	caps := []string{"shell.user"}
	writeScope := []string{}
	intent := "basic remote identity and shell probe"
	adapter := "shell"

	switch capability {
	case "powershell.user", "powershell":
		capability = "powershell.user"
		adapter = "powershell"
		caps = []string{"powershell.user"}
		intent = "basic PowerShell identity probe"
		cmd = nil
		allow = []string{"powershell.exe", "powershell", "pwsh"}
	case "fs.read":
		caps = []string{"shell.user", "fs.read"}
		intent = "basic scoped directory read probe"
		if windows {
			cmd = []string{"cmd", "/c", "dir /b ."}
			allow = []string{"cmd"}
		} else {
			cmd = []string{"sh", "-c", "ls -la . | head -40"}
		}
	case "fs.write.scoped":
		caps = []string{"shell.user", "fs.write.scoped"}
		writeScope = []string{"."}
		intent = "basic scoped write and cleanup probe"
		if windows {
			cmd = []string{"cmd", "/c", "echo rdev-test> rdev_policy_template_test.txt && type rdev_policy_template_test.txt && del rdev_policy_template_test.txt && if not exist rdev_policy_template_test.txt echo cleanup-ok"}
			allow = []string{"cmd"}
		} else {
			cmd = []string{"sh", "-c", "printf rdev-test > rdev_policy_template_test.txt && cat rdev_policy_template_test.txt && rm rdev_policy_template_test.txt && test ! -e rdev_policy_template_test.txt && echo cleanup-ok"}
		}
	case "process.inspect":
		caps = []string{"shell.user", "process.inspect"}
		intent = "basic process inspection probe"
		if windows {
			cmd = []string{"tasklist"}
			allow = []string{"tasklist"}
		} else {
			cmd = []string{"sh", "-c", "ps -o pid,comm= -p $$"}
		}
	case "tool.availability", "tools", "tool":
		capability = "tool.availability"
		caps = []string{"shell.user"}
		intent = "basic developer tool availability probe"
		if windows {
			cmd = []string{"cmd", "/c", "where powershell && where git && where curl && where winget"}
			allow = []string{"cmd"}
		} else {
			cmd = []string{"sh", "-c", "command -v sh; command -v git || true; command -v curl || true; command -v go || true"}
		}
	default:
		capability = "shell.user"
		if windows {
			cmd = []string{"cmd", "/c", "hostname && whoami && ver && echo CD=%CD%"}
			allow = []string{"cmd"}
		}
	}

	policy := map[string]any{
		"workspace_root":       ".",
		"capabilities":         caps,
		"allow_commands":       allow,
		"max_duration_seconds": 15,
		"max_output_bytes":     20000,
		"network":              "default-deny",
	}
	if adapter == "powershell" {
		policy["command"] = "Write-Output $env:COMPUTERNAME; whoami; Get-Location"
	} else {
		policy["argv"] = cmd
	}
	if len(writeScope) > 0 {
		policy["write_scope"] = writeScope
	}
	return map[string]any{
		"schema_version": "rdev.job-policy-template.v1",
		"capability":     capability,
		"target_os":      targetOS,
		"adapter":        adapter,
		"intent":         intent,
		"policy":         policy,
		"job_create_example": map[string]any{
			"command": []string{
				"rdev", "job", "create",
				"--gateway-url", "<ACTIVE_GATEWAY_URL>",
				"--host-id", "<HOST_ID>",
				"--adapter", adapter,
				"--intent", intent,
				"--policy-json", "<COPY policy OBJECT FROM THIS OUTPUT>",
			},
		},
		"agent_rule": "Use this policy object directly with rdev job create or rdev.jobs.create; do not search source code or hand-roll policy JSON.",
	}
}
