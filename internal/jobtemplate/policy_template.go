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
	approvals := []string{}
	intent := "basic remote identity and shell probe"
	adapter := "shell"
	action := ""
	path := ""
	content := ""
	encoding := ""

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
	case "file.transfer.read", "file.read", "file.list", "file.download":
		capability = "file.transfer.read"
		adapter = "file"
		caps = []string{"file.transfer.read", "fs.read"}
		intent = "scoped file read through native rdev file adapter"
		action = "read"
		path = "."
	case "file.transfer.write", "file.write", "file.upload":
		capability = "file.transfer.write"
		adapter = "file"
		caps = []string{"file.transfer.write", "fs.write.scoped"}
		writeScope = []string{"."}
		intent = "scoped file write through native rdev file adapter"
		action = "write"
		path = "rdev-file-adapter-test.txt"
		content = "rdev file adapter test"
		encoding = "utf-8"
	case "file.delete", "file.remove":
		capability = "file.delete"
		adapter = "file"
		caps = []string{"file.transfer.write", "fs.write.scoped"}
		writeScope = []string{"."}
		approvals = []string{"file.delete"}
		intent = "delete a scoped file through native rdev file adapter"
		action = "delete"
		path = "rdev-file-adapter-delete-target.txt"
	case "window.inspect", "desktop.windows", "desktop.window.inspect":
		capability = "window.inspect"
		adapter = "desktop"
		caps = []string{"window.inspect"}
		intent = "inspect visible desktop windows through native rdev desktop adapter"
		action = "window.inspect"
	case "screen.screenshot", "desktop.screenshot":
		capability = "screen.screenshot"
		adapter = "desktop"
		caps = []string{"screen.screenshot"}
		approvals = []string{"screen.screenshot"}
		intent = "capture a desktop screenshot through native rdev desktop adapter"
		action = "screen.screenshot"
	case "screen.record", "desktop.record":
		capability = "screen.record"
		adapter = "desktop"
		caps = []string{"screen.record"}
		approvals = []string{"screen.record"}
		intent = "capture a short desktop PNG frame bundle through native rdev desktop adapter"
		action = "screen.record"
	case "window.focus", "desktop.focus":
		capability = "window.focus"
		adapter = "desktop"
		caps = []string{"window.focus"}
		approvals = []string{"window.focus"}
		intent = "focus a visible desktop window through native rdev desktop adapter"
		action = "window.focus"
	case "window.move", "desktop.move":
		capability = "window.move"
		adapter = "desktop"
		caps = []string{"window.move"}
		approvals = []string{"window.move"}
		intent = "move a visible desktop window through native rdev desktop adapter"
		action = "window.move"
	case "input.keyboard", "desktop.keyboard":
		capability = "input.keyboard"
		adapter = "desktop"
		caps = []string{"input.keyboard"}
		approvals = []string{"input.keyboard"}
		intent = "send approved keyboard input through native rdev desktop adapter"
		action = "input.keyboard"
	case "input.mouse", "desktop.mouse":
		capability = "input.mouse"
		adapter = "desktop"
		caps = []string{"input.mouse"}
		approvals = []string{"input.mouse"}
		intent = "send approved mouse input through native rdev desktop adapter"
		action = "input.mouse"
	case "app.launch", "desktop.app.launch":
		capability = "app.launch"
		adapter = "desktop"
		caps = []string{"app.launch"}
		approvals = []string{"app.launch"}
		intent = "launch an approved desktop app through native rdev desktop adapter"
		action = "app.launch"
	case "app.close", "desktop.app.close":
		capability = "app.close"
		adapter = "desktop"
		caps = []string{"app.close"}
		approvals = []string{"app.close"}
		intent = "gracefully close an approved desktop window through native rdev desktop adapter"
		action = "app.close"
	case "url.open", "desktop.url.open":
		capability = "url.open"
		adapter = "desktop"
		caps = []string{"url.open"}
		approvals = []string{"url.open"}
		intent = "open an approved URL through native rdev desktop adapter"
		action = "url.open"
	case "clipboard.read":
		capability = "clipboard.read"
		adapter = "desktop"
		caps = []string{"clipboard.read"}
		approvals = []string{"clipboard.read"}
		intent = "read clipboard through native rdev desktop adapter"
		action = "clipboard.read"
	case "clipboard.write":
		capability = "clipboard.write"
		adapter = "desktop"
		caps = []string{"clipboard.write"}
		approvals = []string{"clipboard.write"}
		intent = "write clipboard through native rdev desktop adapter"
		action = "clipboard.write"
	case "unattended.access":
		capability = "unattended.access"
		adapter = "desktop"
		caps = []string{"unattended.access"}
		approvals = []string{"unattended.access"}
		intent = "request managed-host unattended access capability"
		action = "unattended.access"
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
	} else if adapter == "file" || adapter == "desktop" {
		policy["action"] = action
		delete(policy, "allow_commands")
		if path != "" {
			policy["path"] = path
		}
		if content != "" {
			policy["content"] = content
		}
		if encoding != "" {
			policy["encoding"] = encoding
		}
		if len(approvals) > 0 {
			policy["approvals_required"] = approvals
		}
		if adapter == "desktop" {
			switch action {
			case "input.keyboard":
				policy["text"] = "rdev"
			case "input.mouse":
				policy["x"] = 0
				policy["y"] = 0
				policy["button"] = "move"
			case "app.launch":
				if windows {
					policy["app"] = "notepad.exe"
				} else {
					policy["app"] = "<APP_PATH_OR_NAME>"
				}
			case "app.close", "window.focus":
				policy["title"] = "<WINDOW_TITLE_CONTAINS>"
			case "window.move":
				policy["title"] = "<WINDOW_TITLE_CONTAINS>"
				policy["x"] = 100
				policy["y"] = 100
				policy["width"] = 900
				policy["height"] = 700
			case "url.open":
				policy["url"] = "https://example.com"
			case "screen.record":
				policy["frames"] = 3
				policy["interval_millis"] = 500
			}
		}
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
