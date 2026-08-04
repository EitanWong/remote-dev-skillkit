package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const DenialSchemaVersion = "rdev.host-denial.v1"

type Result struct {
	ArtifactContent       string `json:"artifact_content"`
	RuntimeFixtureContent string `json:"runtime_fixture_content,omitempty"`
}

type Options struct {
	IdentityFingerprint           string
	ToolchainRoot                 string
	WorkspaceLockStore            string
	WorkspaceLockTTL              time.Duration
	CaptureRuntimeFixture         bool
	EngineeringCheckpointStore    string
	EngineeringProgressReporter   EngineeringProgressReporter
	EngineeringResumeCheckpointID string
	EngineeringResumeSourceTaskID string
	engineeringResume             *engineeringResumeState
}

type SessionTaskSpec struct {
	TaskID              string
	AttemptID           string
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
	AttemptID          string
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
	// Hint is an actionable, agent-directed fix for the denial: the exact
	// field to add or change so a retry succeeds without trial and error.
	Hint       string `json:"hint,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	EndpointID string `json:"endpoint_id,omitempty"`
	Adapter    string `json:"adapter,omitempty"`
	Capability string `json:"capability,omitempty"`
	Retryable  bool   `json:"retryable"`
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
	normalizedSpec, err := normalizeEngineeringSessionTaskSpec(spec)
	if err != nil {
		ref := taskRef{TaskID: spec.TaskID, EndpointID: spec.EndpointID, Adapter: spec.Adapter}
		return denyTask(ref, denialSpec{
			Code:      "invalid_engineering_task",
			Summary:   "Engineering task preflight failed on the host.",
			Detail:    err.Error(),
			Adapter:   spec.Adapter,
			Retryable: false,
		}, err)
	}
	spec = normalizedSpec
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
			Hint:      "Add an absolute workspace_root to the task payload (required for every adapter, e.g. C:\\Users\\Public on Windows).",
			Adapter:   envelope.Adapter,
			Retryable: true,
		}, fmt.Errorf("workspace root is required"))
	}
	if missing := missingAdapterCapability(envelope); missing != "" {
		return denyTask(ref, denialSpec{
			Code:       "missing_capability",
			Summary:    fmt.Sprintf("Task is missing the %s capability.", missing),
			Detail:     fmt.Sprintf("The host requires %s before running the %s adapter.", missing, envelope.Adapter),
			Hint:       fmt.Sprintf("Add %q to the task-level capabilities list (the session already authorizes it).", missing),
			Adapter:    envelope.Adapter,
			Capability: missing,
			Retryable:  true,
		}, fmt.Errorf("missing %s capability", missing))
	}
	if _, engineering := engineeringTaskFromEnvelope(envelope); engineering {
		resumedOptions, resumeErr := prepareEngineeringResume(envelope, opts)
		if resumeErr != nil {
			return denyTask(ref, denialSpec{
				Code:      "resume_checkpoint_invalid",
				Summary:   "Engineering task resume checkpoint is unavailable or does not match this task.",
				Detail:    "Request a fresh task or a checkpoint produced by the same bounded engineering task.",
				Adapter:   envelope.Adapter,
				Retryable: false,
			}, resumeErr)
		}
		opts = resumedOptions
	}
	wantsWorktree, isolationErr := wantsGitWorktree(envelope)
	if isolationErr != nil {
		return denyTask(ref, denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Task requested an unsupported workspace isolation policy.",
			Detail:    isolationErr.Error(),
			Adapter:   envelope.Adapter,
			Retryable: false,
		}, isolationErr)
	}
	if wantsWorktree && !hasCapability(envelope.Capabilities, "git.diff") {
		missing := "git.diff"
		return denyTask(ref, denialSpec{
			Code:       "missing_capability",
			Summary:    fmt.Sprintf("Task is missing the %s capability.", missing),
			Detail:     "Git worktree isolation requires git.diff before host-local Git commands may run.",
			Hint:       fmt.Sprintf("Add %q to the task-level capabilities list.", missing),
			Adapter:    envelope.Adapter,
			Capability: missing,
			Retryable:  true,
		}, fmt.Errorf("missing %s capability", missing))
	}
	worktreeLease, err := prepareGitWorktreeLease(ctx, &envelope, opts, now)
	if err != nil {
		return denyTask(ref, workspaceLockDenial(err), err)
	}
	releaseWorkspaceLock := func() {}
	if worktreeLease == nil {
		releaseWorkspaceLock, err = acquireWorkspaceLock(spec.EndpointID, envelope, opts, now)
		if err != nil {
			return denyTask(ref, workspaceLockDenial(err), err)
		}
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
	var scopeLease *workspaceScopeLease
	if worktreeLease == nil && wantsWorktree {
		scopeLease, err = prepareWorkspaceScopeLease(envelope)
		if err != nil {
			return denyTask(ref, workspaceLockDenial(err), err)
		}
		if scopeLease.before.Truncated && len(scopeLease.declaredWriteScope) > 0 {
			err := fmt.Errorf("non-Git workspace snapshot is truncated; cannot verify declared write scope")
			return denyTask(ref, denialSpec{
				Code:      "write_scope_unverifiable",
				Summary:   "Task write scope cannot be verified for this workspace.",
				Detail:    err.Error(),
				Adapter:   envelope.Adapter,
				Retryable: false,
			}, err)
		}
	}
	runtimeRelease := releaseWorkspaceLockOnce
	if worktreeLease != nil || scopeLease != nil {
		runtimeRelease = nil
	}
	var execution adapterExecution
	var executionErr error
	if _, engineering := engineeringTaskFromEnvelope(envelope); engineering && isEngineeringRepairAdapter(envelope.Adapter) {
		execution, executionErr = executeEngineeringTask(ctx, envelope, opts.CaptureRuntimeFixture, opts)
	} else {
		execution, executionErr = executeJobAdapterWithToolchainRoot(ctx, envelope, opts.CaptureRuntimeFixture, runtimeRelease, opts.ToolchainRoot)
	}
	result := Result{
		ArtifactContent:       execution.ArtifactContent,
		RuntimeFixtureContent: execution.RuntimeFixtureContent,
	}
	if worktreeLease != nil {
		liveScopeErr := validateLiveWriteScope(envelope.Workspace.Root, envelope.Workspace.WriteScope)
		evidence, finalizeErr := worktreeLease.finalize(ctx, envelope.Payload)
		result.ArtifactContent = attachGitWorktreeEvidence(result.ArtifactContent, evidence)
		if finalizeErr != nil {
			if executionErr != nil {
				return result, fmt.Errorf("%v; additionally failed to finalize task worktree: %w", executionErr, finalizeErr)
			}
			return result, finalizeErr
		}
		if liveScopeErr != nil {
			return preserveExecutionDenial(result, ref, denialSpec{
				Code:      "write_scope_violation",
				Summary:   "Task changed files outside its declared write scope.",
				Detail:    liveScopeErr.Error(),
				Adapter:   envelope.Adapter,
				Retryable: false,
			}, liveScopeErr)
		}
		if scopeErr := validateWorktreeWriteScope(evidence, envelope.Workspace.Root, envelope.Workspace.WriteScope); scopeErr != nil {
			return preserveExecutionDenial(result, ref, denialSpec{
				Code:      "write_scope_violation",
				Summary:   "Task changed files outside its declared write scope.",
				Detail:    scopeErr.Error(),
				Adapter:   envelope.Adapter,
				Retryable: false,
			}, scopeErr)
		}
	}
	if scopeLease != nil {
		evidence, finalizeErr := scopeLease.finalize()
		result.ArtifactContent = attachWorkspaceScopeEvidence(result.ArtifactContent, evidence)
		if finalizeErr != nil {
			if executionErr != nil {
				return result, fmt.Errorf("%v; additionally failed to snapshot non-Git workspace scope: %w", executionErr, finalizeErr)
			}
			return result, finalizeErr
		}
		if scopeErr := validateWorkspaceScopeWriteScope(evidence, envelope.Workspace.Root, envelope.Workspace.WriteScope); scopeErr != nil {
			return preserveExecutionDenial(result, ref, denialSpec{
				Code:      "write_scope_violation",
				Summary:   "Task changed files outside its declared write scope.",
				Detail:    scopeErr.Error(),
				Adapter:   envelope.Adapter,
				Retryable: false,
			}, scopeErr)
		}
	}
	if executionErr != nil {
		if denial, ok := adapterDenial(envelope.Adapter, executionErr); ok {
			return preserveExecutionDenial(result, ref, denial, executionErr)
		}
		return result, executionErr
	}
	return result, nil
}

func normalizeEngineeringSessionTaskSpec(spec SessionTaskSpec) (SessionTaskSpec, error) {
	raw, ok := spec.Payload["engineering_task"]
	if !ok || raw == nil {
		return spec, nil
	}
	engineering, err := contracts.DecodeEngineeringTask(raw)
	if err != nil {
		return SessionTaskSpec{}, err
	}
	if err := engineering.ValidateForAdapter(spec.Adapter); err != nil {
		return SessionTaskSpec{}, err
	}
	if strings.TrimSpace(spec.Intent) != "" && strings.TrimSpace(spec.Intent) != engineering.Goal {
		return SessionTaskSpec{}, fmt.Errorf("intent must match engineering_task.goal when engineering_task is present")
	}
	if strings.TrimSpace(spec.Workspace.Root) != "" && strings.TrimSpace(spec.Workspace.Root) != engineering.Workspace.Root {
		return SessionTaskSpec{}, fmt.Errorf("workspace.root must match engineering_task.workspace.root")
	}
	if strings.TrimSpace(spec.Workspace.Branch) != "" && strings.TrimSpace(spec.Workspace.Branch) != engineering.Workspace.Branch {
		return SessionTaskSpec{}, fmt.Errorf("workspace.branch must match engineering_task.workspace.branch")
	}
	if strings.TrimSpace(spec.Workspace.BaseSHA) != "" && strings.TrimSpace(spec.Workspace.BaseSHA) != engineering.Workspace.BaseSHA {
		return SessionTaskSpec{}, fmt.Errorf("workspace.base_sha must match engineering_task.workspace.base_sha")
	}
	if strings.TrimSpace(spec.Workspace.Isolation) != "" && strings.TrimSpace(spec.Workspace.Isolation) != engineering.Workspace.Isolation {
		return SessionTaskSpec{}, fmt.Errorf("workspace.isolation must match engineering_task.workspace.isolation")
	}
	if strings.TrimSpace(spec.Workspace.DirtyPolicy) != "" && strings.TrimSpace(spec.Workspace.DirtyPolicy) != engineering.Workspace.DirtyPolicy {
		return SessionTaskSpec{}, fmt.Errorf("workspace.dirty_policy must match engineering_task.workspace.dirty_policy")
	}
	if len(spec.Workspace.WriteScope) > 0 && !sameStringSet(spec.Workspace.WriteScope, engineering.Workspace.WriteScope) {
		return SessionTaskSpec{}, fmt.Errorf("workspace.write_scope must match engineering_task.workspace.write_scope")
	}
	if len(spec.Capabilities) > 0 && !sameStringSet(spec.Capabilities, engineering.RequiredCapabilities) {
		return SessionTaskSpec{}, fmt.Errorf("capabilities must exactly match engineering_task.required_capabilities")
	}
	if spec.Limits.MaxDurationSeconds != 0 && spec.Limits.MaxDurationSeconds != engineering.Limits.MaxDurationSeconds {
		return SessionTaskSpec{}, fmt.Errorf("limits.max_duration_seconds must match engineering_task.limits.max_duration_seconds")
	}
	if spec.Limits.MaxOutputBytes != 0 && spec.Limits.MaxOutputBytes != engineering.Limits.MaxOutputBytes {
		return SessionTaskSpec{}, fmt.Errorf("limits.max_output_bytes must match engineering_task.limits.max_output_bytes")
	}
	if strings.TrimSpace(spec.Limits.Network) != "" && strings.TrimSpace(spec.Limits.Network) != engineering.NetworkPolicy {
		return SessionTaskSpec{}, fmt.Errorf("limits.network must match engineering_task.network_policy")
	}
	spec.Intent = engineering.Goal
	spec.Workspace.Root = engineering.Workspace.Root
	spec.Workspace.Branch = engineering.Workspace.Branch
	spec.Workspace.BaseSHA = engineering.Workspace.BaseSHA
	spec.Workspace.Isolation = engineering.Workspace.Isolation
	spec.Workspace.DirtyPolicy = engineering.Workspace.DirtyPolicy
	spec.Workspace.WriteScope = append([]string(nil), engineering.Workspace.WriteScope...)
	spec.Capabilities = append([]string(nil), engineering.RequiredCapabilities...)
	spec.Limits = model.TaskLimits{
		MaxDurationSeconds: engineering.Limits.MaxDurationSeconds,
		MaxOutputBytes:     engineering.Limits.MaxOutputBytes,
		Network:            engineering.NetworkPolicy,
	}
	spec.Payload = engineering.TaskPayload(spec.Payload)
	return spec, nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]bool, len(left))
	for _, value := range left {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	for _, value := range right {
		if !seen[value] {
			return false
		}
	}
	return true
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
		SchemaVersion:      "rdev.session-task.v1",
		TaskID:             spec.TaskID,
		AttemptID:          firstNonEmptyString(spec.AttemptID, "attempt:"+spec.TaskID),
		EndpointID:         spec.EndpointID,
		EndpointIdentity:   spec.IdentityFingerprint,
		Adapter:            spec.Adapter,
		Intent:             spec.Intent,
		Workspace:          spec.Workspace,
		Capabilities:       append([]string(nil), spec.Capabilities...),
		Limits:             limits,
		Payload:            cloneMap(spec.Payload),
		InterruptsRequired: stringSliceValue(spec.Payload, "interrupts_required"),
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
	case "shell", "powershell", "codex", "claude-code", "acpx", "toolchain", "file", "desktop":
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
	case "toolchain":
		if !hasCapability(envelope.Capabilities, "package.install.requiresAuthorization") {
			return "package.install.requiresAuthorization"
		}
	case "file":
		return missingFileCapability(envelope)
	case "desktop":
		return missingDesktopCapability(envelope)
	}
	return ""
}

func missingFileCapability(envelope taskEnvelope) string {
	action := normalizeAdapterAction(stringValue(envelope.Payload, "action", ""))
	switch action {
	case "describe":
		if !hasCapability(envelope.Capabilities, "file.transfer.read") {
			return "file.transfer.read"
		}
		if !hasCapability(envelope.Capabilities, "git.diff") {
			return "git.diff"
		}
	case "list", "read", "download", "search", "read.slice", "symbols", "diagnostics":
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

func int64Value(values map[string]any, key string, fallback int64) int64 {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return fallback
	}
}

type denialSpec struct {
	Code       string
	Summary    string
	Detail     string
	Hint       string
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
		Hint:          spec.Hint,
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

// preserveExecutionDenial retains adapter and workspace evidence collected
// before a policy denial. Preflight denials still use denyTask directly.
func preserveExecutionDenial(result Result, task taskRef, spec denialSpec, cause error) (Result, error) {
	_, err := denyTask(task, spec, cause)
	var denial DenialError
	if !errors.As(err, &denial) {
		return result, err
	}
	artifact := make(map[string]any)
	if strings.TrimSpace(result.ArtifactContent) != "" && json.Unmarshal([]byte(result.ArtifactContent), &artifact) == nil {
		artifact["task_denial"] = denial.Explanation
		if content, marshalErr := json.MarshalIndent(artifact, "", "  "); marshalErr == nil {
			result.ArtifactContent = string(content)
		}
	}
	return result, denial
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
			Hint:      "Add the executable name (e.g. sh, cmd, powershell.exe, pwsh) to payload.allow_commands.",
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Hint:      "Keep payload.write_scope inside workspace_root.",
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root is required"):
		return denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for shell execution.",
			Detail:    err.Error(),
			Hint:      "Add an absolute workspace_root to the task payload.",
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root must"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Hint:      "workspace_root must be an absolute path (e.g. C:\\Users\\Public on Windows).",
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
			Hint:      "Add the PowerShell executable name (powershell.exe or pwsh) to payload.allow_commands; on Windows also set payload.powershell_command to the bare executable name.",
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Hint:      "Keep payload.write_scope inside workspace_root.",
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root is required"):
		return denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for PowerShell execution.",
			Detail:    err.Error(),
			Hint:      "Add an absolute workspace_root to the task payload.",
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root must"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Hint:      "workspace_root must be an absolute path (e.g. C:\\Users\\Public on Windows).",
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
