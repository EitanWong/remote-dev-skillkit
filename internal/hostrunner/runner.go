package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const DenialSchemaVersion = "rdev.host-denial.v1"

type Result struct {
	ArtifactContent       string `json:"artifact_content"`
	RuntimeFixtureContent string `json:"runtime_fixture_content,omitempty"`
}

type Options struct {
	IdentityFingerprint   string
	WorkspaceLockStore    string
	WorkspaceLockTTL      time.Duration
	CaptureRuntimeFixture bool
}

type SessionTaskSpec struct {
	TaskID              string
	EndpointID          string
	IdentityFingerprint string
	Adapter             string
	Intent              string
	Workspace           model.TaskWorkspace
	Capabilities        []string
	Limits              model.TaskLimits
	Payload             map[string]any
}

type taskEnvelope struct {
	SchemaVersion      string
	TaskID             string
	EndpointID         string
	EndpointIdentity   string
	Adapter            string
	Intent             string
	Workspace          model.TaskWorkspace
	Capabilities       []string
	Limits             model.TaskLimits
	Payload            map[string]any
	InterruptsRequired []string
}

type taskRef struct {
	TaskID     string
	EndpointID string
	Adapter    string
}

type DenialExplanation struct {
	SchemaVersion string `json:"schema_version"`
	Code          string `json:"code"`
	Summary       string `json:"summary"`
	Detail        string `json:"detail,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	EndpointID    string `json:"endpoint_id,omitempty"`
	Adapter       string `json:"adapter,omitempty"`
	Capability    string `json:"capability,omitempty"`
	Retryable     bool   `json:"retryable"`
}

type DenialError struct {
	Explanation DenialExplanation
	Cause       error
}

func (e DenialError) Error() string {
	if e.Explanation.Summary != "" {
		return e.Explanation.Summary
	}
	return e.Explanation.Code
}

func (e DenialError) Unwrap() error {
	return e.Cause
}

func RunSessionTaskWithOptionsContext(ctx context.Context, spec SessionTaskSpec, now time.Time, opts Options) (Result, error) {
	envelope := sessionTaskEnvelope(spec, now)
	ref := taskRef{TaskID: envelope.TaskID, EndpointID: envelope.EndpointID, Adapter: envelope.Adapter}
	if !supportedAdapter(envelope.Adapter) {
		return denyTask(ref, denialSpec{
			Code:      "unsupported_adapter",
			Summary:   "Requested adapter is not supported by this host runner.",
			Detail:    fmt.Sprintf("Adapter %q is not available in the current host runner.", envelope.Adapter),
			Adapter:   envelope.Adapter,
			Retryable: true,
		}, fmt.Errorf("unsupported dev adapter %q", envelope.Adapter))
	}
	if envelope.Workspace.Root == "" {
		return denyTask(ref, denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for adapter execution.",
			Detail:    "Host adapters only run inside an explicit workspace root.",
			Adapter:   envelope.Adapter,
			Retryable: true,
		}, fmt.Errorf("workspace root is required"))
	}
	if missing := missingAdapterCapability(envelope); missing != "" {
		return denyTask(ref, denialSpec{
			Code:       "missing_capability",
			Summary:    fmt.Sprintf("Task is missing the %s capability.", missing),
			Detail:     fmt.Sprintf("The host requires %s before running the %s adapter.", missing, envelope.Adapter),
			Adapter:    envelope.Adapter,
			Capability: missing,
			Retryable:  true,
		}, fmt.Errorf("missing %s capability", missing))
	}
	releaseWorkspaceLock, err := acquireWorkspaceLock(spec.EndpointID, envelope, opts, now)
	if err != nil {
		return denyTask(ref, workspaceLockDenial(err), err)
	}
	releasedWorkspaceLock := false
	releaseWorkspaceLockOnce := func() {
		if releasedWorkspaceLock {
			return
		}
		releasedWorkspaceLock = true
		releaseWorkspaceLock()
	}
	defer releaseWorkspaceLockOnce()
	execution, err := executeJobAdapter(ctx, envelope, opts.CaptureRuntimeFixture, releaseWorkspaceLockOnce)
	result := Result{
		ArtifactContent:       execution.ArtifactContent,
		RuntimeFixtureContent: execution.RuntimeFixtureContent,
	}
	if err != nil {
		if denial, ok := adapterDenial(envelope.Adapter, err); ok {
			return denyTask(ref, denial, err)
		}
		return result, err
	}
	return result, nil
}

func sessionTaskEnvelope(spec SessionTaskSpec, now time.Time) taskEnvelope {
	limits := spec.Limits
	if limits.MaxDurationSeconds == 0 {
		limits.MaxDurationSeconds = model.DefaultTaskTTLSeconds
	}
	if limits.MaxOutputBytes == 0 {
		limits.MaxOutputBytes = model.DefaultTaskMaxOutputBytes
	}
	if strings.TrimSpace(limits.Network) == "" {
		limits.Network = "default-deny"
	}
	return taskEnvelope{
		SchemaVersion:    "rdev.session-task.v1",
		TaskID:           spec.TaskID,
		EndpointID:       spec.EndpointID,
		EndpointIdentity: spec.IdentityFingerprint,
		Adapter:          spec.Adapter,
		Intent:           spec.Intent,
		Workspace:        spec.Workspace,
		Capabilities:     append([]string(nil), spec.Capabilities...),
		Limits:           limits,
		Payload:          cloneMap(spec.Payload),
	}
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func supportedAdapter(adapter string) bool {
	switch adapter {
	case "shell", "powershell", "codex", "claude-code", "acpx", "file", "desktop":
		return true
	default:
		return false
	}
}

func missingAdapterCapability(envelope taskEnvelope) string {
	switch envelope.Adapter {
	case "shell":
		if !hasCapability(envelope.Capabilities, "shell.user") {
			return "shell.user"
		}
	case "powershell":
		if !hasCapability(envelope.Capabilities, "powershell.user") {
			return "powershell.user"
		}
	case "codex":
		if !hasCapability(envelope.Capabilities, "codex.run") {
			return "codex.run"
		}
		if !hasCapability(envelope.Capabilities, "git.diff") {
			return "git.diff"
		}
	case "claude-code":
		if !hasCapability(envelope.Capabilities, "claude-code.run") {
			return "claude-code.run"
		}
		if !hasCapability(envelope.Capabilities, "git.diff") {
			return "git.diff"
		}
	case "acpx":
		if !hasCapability(envelope.Capabilities, "acpx.run") {
			return "acpx.run"
		}
		if !hasCapability(envelope.Capabilities, "git.diff") {
			return "git.diff"
		}
	case "file":
		return missingFileCapability(envelope)
	case "desktop":
		return missingDesktopCapability(envelope)
	}
	return ""
}

func missingFileCapability(envelope taskEnvelope) string {
	switch normalizeAdapterAction(stringValue(envelope.Payload, "action", "")) {
	case "list", "read", "download":
		if !hasCapability(envelope.Capabilities, "file.transfer.read") {
			return "file.transfer.read"
		}
	case "write", "upload", "delete":
		if !hasCapability(envelope.Capabilities, "file.transfer.write") {
			return "file.transfer.write"
		}
		if !hasCapability(envelope.Capabilities, "fs.write.scoped") {
			return "fs.write.scoped"
		}
	default:
		return "file.transfer.read"
	}
	return ""
}

func missingDesktopCapability(envelope taskEnvelope) string {
	action := normalizeAdapterAction(stringValue(envelope.Payload, "action", ""))
	switch action {
	case "windows", "window.list", "window.inspect":
		if !hasCapability(envelope.Capabilities, "window.inspect") {
			return "window.inspect"
		}
	case "screenshot", "screen.screenshot":
		if !hasCapability(envelope.Capabilities, "screen.screenshot") {
			return "screen.screenshot"
		}
	case "record", "screen.record":
		if !hasCapability(envelope.Capabilities, "screen.record") {
			return "screen.record"
		}
	case "focus", "window.focus":
		if !hasCapability(envelope.Capabilities, "window.focus") {
			return "window.focus"
		}
	case "move", "window.move":
		if !hasCapability(envelope.Capabilities, "window.move") {
			return "window.move"
		}
	case "keyboard", "input.keyboard":
		if !hasCapability(envelope.Capabilities, "input.keyboard") {
			return "input.keyboard"
		}
	case "mouse", "input.mouse":
		if !hasCapability(envelope.Capabilities, "input.mouse") {
			return "input.mouse"
		}
	case "launch", "app.launch":
		if !hasCapability(envelope.Capabilities, "app.launch") {
			return "app.launch"
		}
	case "close", "app.close":
		if !hasCapability(envelope.Capabilities, "app.close") {
			return "app.close"
		}
	case "url", "open.url", "url.open":
		if !hasCapability(envelope.Capabilities, "url.open") {
			return "url.open"
		}
	case "clipboard.read":
		if !hasCapability(envelope.Capabilities, "clipboard.read") {
			return "clipboard.read"
		}
	case "clipboard.write":
		if !hasCapability(envelope.Capabilities, "clipboard.write") {
			return "clipboard.write"
		}
	case "unattended.access":
		if !hasCapability(envelope.Capabilities, "unattended.access") {
			return "unattended.access"
		}
	default:
		return "window.inspect"
	}
	return ""
}

func normalizeAdapterAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "_", ".")
	action = strings.ReplaceAll(action, "-", ".")
	return action
}

func acquireWorkspaceLock(hostID string, envelope taskEnvelope, opts Options, now time.Time) (func(), error) {
	if strings.TrimSpace(opts.WorkspaceLockStore) == "" {
		return func() {}, nil
	}
	ttl := opts.WorkspaceLockTTL
	if ttl <= 0 {
		ttl = workspace.DefaultLockTTL
	}
	store := workspace.NewFileLockStore(opts.WorkspaceLockStore)
	lock, err := store.Acquire(workspace.LockOptions{
		RepoRoot:     envelope.Workspace.Root,
		HostID:       hostID,
		TaskID:       envelope.TaskID,
		WorktreePath: envelope.Workspace.Root,
		BaseRef:      "",
		Branch:       envelope.Workspace.Branch,
		OwnerAdapter: envelope.Adapter,
		TTL:          ttl,
	}, now)
	if err != nil {
		return nil, err
	}
	return func() {
		_, _, _ = store.Release(lock.RepoRoot, envelope.TaskID, false)
	}, nil
}

func hasCapability(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func stringMatrixValue(values map[string]any, key string) [][]string {
	value, ok := values[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case [][]string:
		result := make([][]string, 0, len(typed))
		for _, row := range typed {
			result = append(result, append([]string(nil), row...))
		}
		return result
	case []any:
		result := make([][]string, 0, len(typed))
		for _, item := range typed {
			switch row := item.(type) {
			case []string:
				result = append(result, append([]string(nil), row...))
			case []any:
				var values []string
				for _, cell := range row {
					if text, ok := cell.(string); ok && text != "" {
						values = append(values, text)
					}
				}
				if len(values) > 0 {
					result = append(result, values)
				}
			}
		}
		return result
	default:
		return nil
	}
}

func stringValue(values map[string]any, key, fallback string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
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

type denialSpec struct {
	Code       string
	Summary    string
	Detail     string
	Adapter    string
	Capability string
	Retryable  bool
}

func denyTask(task taskRef, spec denialSpec, cause error) (Result, error) {
	explanation := DenialExplanation{
		SchemaVersion: DenialSchemaVersion,
		Code:          spec.Code,
		Summary:       spec.Summary,
		Detail:        spec.Detail,
		TaskID:        task.TaskID,
		EndpointID:    task.EndpointID,
		Adapter:       firstNonEmptyString(spec.Adapter, task.Adapter),
		Capability:    spec.Capability,
		Retryable:     spec.Retryable,
	}
	if explanation.Detail == "" && cause != nil {
		explanation.Detail = cause.Error()
	}
	content, _ := json.MarshalIndent(explanation, "", "  ")
	return Result{ArtifactContent: string(content)}, DenialError{
		Explanation: explanation,
		Cause:       cause,
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func workspaceLockDenial(err error) denialSpec {
	if errors.Is(err, workspace.ErrLocked) {
		return denialSpec{
			Code:      "workspace_locked",
			Summary:   "Workspace is already locked by another task.",
			Detail:    err.Error(),
			Retryable: true,
		}
	}
	return denialSpec{
		Code:      "workspace_invalid",
		Summary:   "Workspace lock could not be acquired.",
		Detail:    err.Error(),
		Retryable: true,
	}
}

func codexDenial(err error) (denialSpec, bool) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not allowlisted"):
		return denialSpec{
			Code:      "command_not_allowlisted",
			Summary:   "Codex verification command is not allowlisted.",
			Detail:    err.Error(),
			Adapter:   "codex",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Adapter:   "codex",
			Retryable: true,
		}, true
	case strings.Contains(text, "prompt is required"):
		return denialSpec{
			Code:      "adapter_payload_invalid",
			Summary:   "Codex prompt is required.",
			Detail:    err.Error(),
			Adapter:   "codex",
			Retryable: true,
		}, true
	case strings.Contains(text, "path is required") || strings.Contains(text, "resolve path") || strings.Contains(text, "stat path") || strings.Contains(text, "path must be a directory"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Adapter:   "codex",
			Retryable: true,
		}, true
	default:
		return denialSpec{}, false
	}
}

func claudeCodeDenial(err error) (denialSpec, bool) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not allowlisted"):
		return denialSpec{
			Code:      "command_not_allowlisted",
			Summary:   "Claude Code verification command is not allowlisted.",
			Detail:    err.Error(),
			Adapter:   "claude-code",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Adapter:   "claude-code",
			Retryable: true,
		}, true
	case strings.Contains(text, "prompt is required"):
		return denialSpec{
			Code:      "adapter_payload_invalid",
			Summary:   "Claude Code prompt is required.",
			Detail:    err.Error(),
			Adapter:   "claude-code",
			Retryable: true,
		}, true
	case strings.Contains(text, "path is required") || strings.Contains(text, "resolve path") || strings.Contains(text, "stat path") || strings.Contains(text, "path must be a directory"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Adapter:   "claude-code",
			Retryable: true,
		}, true
	default:
		return denialSpec{}, false
	}
}

func acpxDenial(err error) (denialSpec, bool) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not allowlisted"):
		return denialSpec{
			Code:      "command_not_allowlisted",
			Summary:   "acpx verification command is not allowlisted.",
			Detail:    err.Error(),
			Adapter:   "acpx",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Adapter:   "acpx",
			Retryable: true,
		}, true
	case strings.Contains(text, "prompt is required"):
		return denialSpec{
			Code:      "adapter_payload_invalid",
			Summary:   "acpx prompt is required.",
			Detail:    err.Error(),
			Adapter:   "acpx",
			Retryable: true,
		}, true
	case strings.Contains(text, "path is required") || strings.Contains(text, "resolve path") || strings.Contains(text, "stat path") || strings.Contains(text, "path must be a directory"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Adapter:   "acpx",
			Retryable: true,
		}, true
	default:
		return denialSpec{}, false
	}
}

func shellDenial(err error) (denialSpec, bool) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not allowlisted"):
		return denialSpec{
			Code:      "command_not_allowlisted",
			Summary:   "Shell command is not allowlisted.",
			Detail:    err.Error(),
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root is required"):
		return denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for shell execution.",
			Detail:    err.Error(),
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root must"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Adapter:   "shell",
			Retryable: true,
		}, true
	default:
		return denialSpec{}, false
	}
}

func powershellDenial(err error) (denialSpec, bool) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not allowlisted"):
		return denialSpec{
			Code:      "command_not_allowlisted",
			Summary:   "PowerShell executable is not allowlisted.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root is required"):
		return denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for PowerShell execution.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root must"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "powershell command is required") || strings.Contains(text, "powershell executable is required"):
		return denialSpec{
			Code:      "adapter_payload_invalid",
			Summary:   "PowerShell command payload is invalid.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	default:
		return denialSpec{}, false
	}
}
