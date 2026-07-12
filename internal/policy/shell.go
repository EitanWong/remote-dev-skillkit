package policy

import (
	"path/filepath"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type TaskPolicyExplanation struct {
	Mode                   string   `json:"mode"`
	Adapter                string   `json:"adapter"`
	Allowed                bool     `json:"allowed"`
	AuthorizationRequired  bool     `json:"authorization_required"`
	RequiredAuthorizations []string `json:"required_authorizations,omitempty"`
	Denials                []string `json:"denials,omitempty"`
	Warnings               []string `json:"warnings,omitempty"`
	Reasons                []string `json:"reasons,omitempty"`
}

func ExplainShellTask(mode model.HostMode, taskPolicy map[string]any) TaskPolicyExplanation {
	explanation := TaskPolicyExplanation{
		Mode:    string(mode),
		Adapter: "shell",
	}
	if !mode.Valid() {
		explanation.Denials = append(explanation.Denials, "unknown host mode")
		return finalizeTaskExplanation(explanation)
	}

	workspaceRoot := stringValue(taskPolicy, "workspace_root", "")
	if workspaceRoot == "" {
		workspaceRoot = stringValue(taskPolicy, "cwd", "")
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		explanation.Denials = append(explanation.Denials, "workspace root is required")
	} else {
		explanation.Reasons = append(explanation.Reasons, "workspace root is declared")
	}

	capabilities := stringSliceValue(taskPolicy, "capabilities")
	if !containsString(capabilities, string(CapabilityShellUser)) {
		explanation.Denials = append(explanation.Denials, "missing shell.user capability")
	} else {
		explanation.Reasons = append(explanation.Reasons, "shell.user capability is present")
	}

	argv := stringSliceValue(taskPolicy, "argv")
	allowCommands := stringSliceValue(taskPolicy, "allow_commands")
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		explanation.Denials = append(explanation.Denials, "argv is required")
	} else if len(allowCommands) == 0 {
		explanation.Denials = append(explanation.Denials, "allow_commands is required")
	} else if !allowedCommand(argv[0], allowCommands) {
		explanation.Denials = append(explanation.Denials, "argv command is not allowlisted")
	} else {
		explanation.Reasons = append(explanation.Reasons, "argv command is allowlisted")
	}

	for _, scope := range stringSliceValue(taskPolicy, "write_scope") {
		if writeScopeEscapes(scope) {
			explanation.Denials = append(explanation.Denials, "write scope escapes workspace root: "+scope)
		}
	}
	if len(stringSliceValue(taskPolicy, "write_scope")) > 0 && !containsString(capabilities, string(CapabilityFSWriteScoped)) {
		explanation.Warnings = append(explanation.Warnings, "write_scope is set without fs.write.scoped capability")
	}

	maxDuration := intValue(taskPolicy, "max_duration_seconds", model.DefaultTaskTTLSeconds)
	if maxDuration <= 0 {
		explanation.Denials = append(explanation.Denials, "max_duration_seconds must be positive")
	}
	maxOutput := intValue(taskPolicy, "max_output_bytes", model.DefaultTaskMaxOutputBytes)
	if maxOutput <= 0 {
		explanation.Denials = append(explanation.Denials, "max_output_bytes must be positive")
	}
	network := stringValue(taskPolicy, "network", "default-deny")
	if network != "default-deny" {
		explanation.RequiredAuthorizations = appendUnique(explanation.RequiredAuthorizations, "network."+network)
		explanation.Warnings = append(explanation.Warnings, "non-default network policy requires authorization")
	}
	for _, authorization := range stringSliceValue(taskPolicy, "authorizations_required") {
		explanation.RequiredAuthorizations = appendUnique(explanation.RequiredAuthorizations, authorization)
	}
	if len(explanation.Denials) == 0 {
		explanation.Reasons = append(explanation.Reasons, "host will still validate the session task payload and canonical paths before execution")
	}
	return finalizeTaskExplanation(explanation)
}

func ExplainAdapterTask(mode model.HostMode, adapter string, taskPolicy map[string]any) TaskPolicyExplanation {
	switch strings.ToLower(strings.TrimSpace(adapter)) {
	case "powershell":
		return ExplainPowerShellTask(mode, taskPolicy)
	case "file":
		return ExplainFileTask(mode, taskPolicy)
	case "desktop":
		return ExplainDesktopTask(mode, taskPolicy)
	default:
		return ExplainShellTask(mode, taskPolicy)
	}
}

func ExplainPowerShellTask(mode model.HostMode, taskPolicy map[string]any) TaskPolicyExplanation {
	explanation := TaskPolicyExplanation{
		Mode:    string(mode),
		Adapter: "powershell",
	}
	if !mode.Valid() {
		explanation.Denials = append(explanation.Denials, "unknown host mode")
		return finalizeTaskExplanation(explanation)
	}

	workspaceRoot := stringValue(taskPolicy, "workspace_root", "")
	if workspaceRoot == "" {
		workspaceRoot = stringValue(taskPolicy, "cwd", "")
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		explanation.Denials = append(explanation.Denials, "workspace root is required")
	} else {
		explanation.Reasons = append(explanation.Reasons, "workspace root is declared")
	}

	capabilities := stringSliceValue(taskPolicy, "capabilities")
	if !containsString(capabilities, string(CapabilityPowerShellUser)) {
		explanation.Denials = append(explanation.Denials, "missing powershell.user capability")
	} else {
		explanation.Reasons = append(explanation.Reasons, "powershell.user capability is present")
	}

	command := stringValue(taskPolicy, "command", "")
	if command == "" {
		command = stringValue(taskPolicy, "script", "")
	}
	allowCommands := stringSliceValue(taskPolicy, "allow_commands")
	if strings.TrimSpace(command) == "" {
		if len(stringSliceValue(taskPolicy, "argv")) > 0 {
			explanation.Denials = append(explanation.Denials, "PowerShell adapter expects policy.command or policy.script; policy.argv is for shell adapter")
		} else {
			explanation.Denials = append(explanation.Denials, "command is required")
		}
	} else if len(allowCommands) == 0 {
		explanation.Denials = append(explanation.Denials, "allow_commands is required")
	} else if !powershellCommandAllowlisted(stringValue(taskPolicy, "powershell_command", ""), allowCommands) {
		explanation.Denials = append(explanation.Denials, "powershell executable is not allowlisted")
	} else {
		explanation.Reasons = append(explanation.Reasons, "powershell executable is allowlisted")
	}

	for _, scope := range stringSliceValue(taskPolicy, "write_scope") {
		if writeScopeEscapes(scope) {
			explanation.Denials = append(explanation.Denials, "write scope escapes workspace root: "+scope)
		}
	}
	if len(stringSliceValue(taskPolicy, "write_scope")) > 0 && !containsString(capabilities, string(CapabilityFSWriteScoped)) {
		explanation.Warnings = append(explanation.Warnings, "write_scope is set without fs.write.scoped capability")
	}

	maxDuration := intValue(taskPolicy, "max_duration_seconds", model.DefaultTaskTTLSeconds)
	if maxDuration <= 0 {
		explanation.Denials = append(explanation.Denials, "max_duration_seconds must be positive")
	}
	maxOutput := intValue(taskPolicy, "max_output_bytes", model.DefaultTaskMaxOutputBytes)
	if maxOutput <= 0 {
		explanation.Denials = append(explanation.Denials, "max_output_bytes must be positive")
	}
	network := stringValue(taskPolicy, "network", "default-deny")
	if network != "default-deny" {
		explanation.RequiredAuthorizations = appendUnique(explanation.RequiredAuthorizations, "network."+network)
		explanation.Warnings = append(explanation.Warnings, "non-default network policy requires authorization")
	}
	for _, authorization := range stringSliceValue(taskPolicy, "authorizations_required") {
		explanation.RequiredAuthorizations = appendUnique(explanation.RequiredAuthorizations, authorization)
	}
	if len(explanation.Denials) == 0 {
		explanation.Reasons = append(explanation.Reasons, "host will still validate the session task payload and canonical paths before execution")
	}
	return finalizeTaskExplanation(explanation)
}

func ExplainFileTask(mode model.HostMode, taskPolicy map[string]any) TaskPolicyExplanation {
	explanation := TaskPolicyExplanation{
		Mode:    string(mode),
		Adapter: "file",
	}
	if !mode.Valid() {
		explanation.Denials = append(explanation.Denials, "unknown host mode")
		return finalizeTaskExplanation(explanation)
	}
	if strings.TrimSpace(stringValue(taskPolicy, "workspace_root", stringValue(taskPolicy, "cwd", ""))) == "" {
		explanation.Denials = append(explanation.Denials, "workspace root is required")
	} else {
		explanation.Reasons = append(explanation.Reasons, "workspace root is declared")
	}
	action := normalizeRemoteAction(stringValue(taskPolicy, "action", ""))
	if action == "" {
		explanation.Denials = append(explanation.Denials, "file action is required")
	}
	capabilities := stringSliceValue(taskPolicy, "capabilities")
	switch action {
	case "list", "read", "download":
		requireCapability(&explanation, capabilities, string(CapabilityFileTransferRead))
		if !containsString(capabilities, string(CapabilityFSRead)) {
			explanation.Warnings = append(explanation.Warnings, "fs.read is recommended with file.transfer.read")
		}
	case "write", "upload", "delete":
		requireCapability(&explanation, capabilities, string(CapabilityFileTransferWrite))
		requireCapability(&explanation, capabilities, string(CapabilityFSWriteScoped))
		if len(stringSliceValue(taskPolicy, "write_scope")) == 0 {
			explanation.Denials = append(explanation.Denials, "write_scope is required for file write/upload/delete")
		}
		if action == "delete" {
			explanation.RequiredAuthorizations = appendUnique(explanation.RequiredAuthorizations, "file.delete")
			explanation.Warnings = append(explanation.Warnings, "file delete requires explicit authorization")
		}
	default:
		if action != "" {
			explanation.Denials = append(explanation.Denials, "unsupported file action: "+action)
		}
	}
	for _, scope := range stringSliceValue(taskPolicy, "write_scope") {
		if writeScopeEscapes(scope) {
			explanation.Denials = append(explanation.Denials, "write scope escapes workspace root: "+scope)
		}
	}
	appendPolicyAuthorizations(&explanation, taskPolicy)
	if len(explanation.Denials) == 0 {
		explanation.Reasons = append(explanation.Reasons, "file adapter will re-check canonical workspace and write-scope boundaries on the host")
	}
	return finalizeTaskExplanation(explanation)
}

func ExplainDesktopTask(mode model.HostMode, taskPolicy map[string]any) TaskPolicyExplanation {
	explanation := TaskPolicyExplanation{
		Mode:    string(mode),
		Adapter: "desktop",
	}
	if !mode.Valid() {
		explanation.Denials = append(explanation.Denials, "unknown host mode")
		return finalizeTaskExplanation(explanation)
	}
	if strings.TrimSpace(stringValue(taskPolicy, "workspace_root", stringValue(taskPolicy, "cwd", ""))) == "" {
		explanation.Denials = append(explanation.Denials, "workspace root is required")
	} else {
		explanation.Reasons = append(explanation.Reasons, "workspace root is declared")
	}
	action := normalizeRemoteAction(stringValue(taskPolicy, "action", ""))
	if action == "" {
		explanation.Denials = append(explanation.Denials, "desktop action is required")
	}
	capabilities := stringSliceValue(taskPolicy, "capabilities")
	capability, authorization := desktopCapabilityAndAuthorization(action)
	if capability == "" {
		if action != "" {
			explanation.Denials = append(explanation.Denials, "unsupported desktop action: "+action)
		}
	} else {
		requireCapability(&explanation, capabilities, capability)
		if authorization != "" {
			explanation.RequiredAuthorizations = appendUnique(explanation.RequiredAuthorizations, authorization)
			explanation.Warnings = append(explanation.Warnings, "desktop action requires explicit authorization: "+authorization)
		}
	}
	appendPolicyAuthorizations(&explanation, taskPolicy)
	if len(explanation.Denials) == 0 {
		explanation.Reasons = append(explanation.Reasons, "desktop adapter requires an unlocked interactive user session and will fail closed if unavailable")
	}
	return finalizeTaskExplanation(explanation)
}

func requireCapability(explanation *TaskPolicyExplanation, capabilities []string, capability string) {
	if !containsString(capabilities, capability) {
		explanation.Denials = append(explanation.Denials, "missing "+capability+" capability")
		return
	}
	explanation.Reasons = append(explanation.Reasons, capability+" capability is present")
}

func appendPolicyAuthorizations(explanation *TaskPolicyExplanation, taskPolicy map[string]any) {
	network := stringValue(taskPolicy, "network", "default-deny")
	if network != "default-deny" {
		explanation.RequiredAuthorizations = appendUnique(explanation.RequiredAuthorizations, "network."+network)
		explanation.Warnings = append(explanation.Warnings, "non-default network policy requires authorization")
	}
	for _, authorization := range stringSliceValue(taskPolicy, "authorizations_required") {
		explanation.RequiredAuthorizations = appendUnique(explanation.RequiredAuthorizations, authorization)
	}
}

func desktopCapabilityAndAuthorization(action string) (string, string) {
	switch action {
	case "windows", "window.inspect":
		return string(CapabilityWindowInspect), ""
	case "screenshot", "screen.screenshot":
		return string(CapabilityScreenScreenshot), ""
	case "record", "screen.record":
		return string(CapabilityScreenRecord), ""
	case "focus", "window.focus":
		return string(CapabilityWindowFocus), ""
	case "move", "window.move":
		return string(CapabilityWindowMove), ""
	case "keyboard", "input.keyboard":
		return string(CapabilityInputKeyboard), ""
	case "mouse", "input.mouse":
		return string(CapabilityInputMouse), ""
	case "app.launch":
		return string(CapabilityAppLaunch), ""
	case "app.close":
		return string(CapabilityAppClose), ""
	case "url.open":
		return string(CapabilityURLOpen), ""
	case "clipboard.read":
		return string(CapabilityClipboardRead), ""
	case "clipboard.write":
		return string(CapabilityClipboardWrite), ""
	case "unattended.access":
		return string(CapabilityUnattendedAccess), "unattended.access"
	default:
		return "", ""
	}
}

func normalizeRemoteAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "_", ".")
	return action
}

func powershellCommandAllowlisted(command string, allowCommands []string) bool {
	command = strings.TrimSpace(command)
	if command != "" {
		return allowedCommand(command, allowCommands)
	}
	for _, candidate := range []string{"powershell.exe", "powershell", "pwsh"} {
		if allowedCommand(candidate, allowCommands) {
			return true
		}
	}
	return false
}

func finalizeTaskExplanation(explanation TaskPolicyExplanation) TaskPolicyExplanation {
	explanation.AuthorizationRequired = len(explanation.RequiredAuthorizations) > 0
	explanation.Allowed = len(explanation.Denials) == 0
	return explanation
}

func stringValue(values map[string]any, key, fallback string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

func intValue(values map[string]any, key string, fallback int) int {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func stringSliceValue(values map[string]any, key string) []string {
	value, ok := values[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func allowedCommand(command string, allowlist []string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	name := commandName(command)
	for _, allowed := range allowlist {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if hasPathSeparator(command) || hasPathSeparator(allowed) {
			if command == allowed {
				return true
			}
			continue
		}
		if name == commandName(allowed) {
			return true
		}
	}
	return false
}

func commandName(command string) string {
	command = strings.TrimSpace(command)
	command = strings.ReplaceAll(command, "\\", "/")
	return filepath.Base(command)
}

func hasPathSeparator(command string) bool {
	return strings.Contains(command, "/") || strings.Contains(command, "\\")
}

func writeScopeEscapes(scope string) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return false
	}
	if len(scope) >= 2 && scope[1] == ':' {
		return true
	}
	if strings.HasPrefix(scope, "\\") {
		return true
	}
	if filepath.IsAbs(scope) {
		return true
	}
	normalized := strings.ReplaceAll(scope, "\\", "/")
	clean := filepath.Clean(normalized)
	return clean == ".." || strings.HasPrefix(clean, "../")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func appendUnique(values []string, next string) []string {
	if strings.TrimSpace(next) == "" || containsString(values, next) {
		return values
	}
	return append(values, next)
}
