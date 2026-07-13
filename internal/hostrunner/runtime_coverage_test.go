package hostrunner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
)

func TestHostRunnerCapabilityAndPayloadHelpers(t *testing.T) {
	for _, adapter := range []string{"shell", "powershell", "codex", "claude-code", "acpx", "file", "desktop"} {
		if !supportedAdapter(adapter) {
			t.Fatalf("adapter %q should be supported", adapter)
		}
	}
	if supportedAdapter("retired") {
		t.Fatal("retired adapter should not be supported")
	}

	fileCases := []struct {
		action string
		caps   []string
		want   string
	}{
		{"read", nil, "file.transfer.read"},
		{"read", []string{"file.transfer.read"}, ""},
		{"write", []string{"file.transfer.write"}, "fs.write.scoped"},
		{"write", []string{"file.transfer.write", "fs.write.scoped"}, ""},
		{"unknown", []string{"file.transfer.read"}, "file.transfer.read"},
	}
	for _, tc := range fileCases {
		got := missingAdapterCapability(taskEnvelope{Adapter: "file", Payload: map[string]any{"action": tc.action}, Capabilities: tc.caps})
		if got != tc.want {
			t.Fatalf("file action %q capability=%q, want %q", tc.action, got, tc.want)
		}
	}
	desktop := []struct {
		action string
		caps   []string
		want   string
	}{
		{"window.inspect", nil, "window.inspect"},
		{"screen.screenshot", []string{"screen.screenshot"}, ""},
		{"clipboard.write", nil, "clipboard.write"},
		{"unattended.access", nil, "unattended.access"},
		{"unknown", nil, "window.inspect"},
	}
	for _, tc := range desktop {
		got := missingAdapterCapability(taskEnvelope{Adapter: "desktop", Payload: map[string]any{"action": tc.action}, Capabilities: tc.caps})
		if got != tc.want {
			t.Fatalf("desktop action %q capability=%q, want %q", tc.action, got, tc.want)
		}
	}
	if normalizeAdapterAction(" Screen_Screenshot ") != "screen.screenshot" {
		t.Fatal("adapter action normalization failed")
	}

	values := map[string]any{
		"strings": []any{"a", 3, "b"},
		"matrix":  []any{[]any{"go", "test"}, []any{""}, []string{"shell"}},
		"int":     float64(3),
		"int64":   int64(4),
		"text":    " value ",
	}
	if got := stringSliceValue(values, "strings"); strings.Join(got, ",") != "a,b" {
		t.Fatalf("string slice conversion = %#v", got)
	}
	if got := stringMatrixValue(values, "matrix"); len(got) != 2 || strings.Join(got[0], ",") != "go,test" || got[1][0] != "shell" {
		t.Fatalf("string matrix conversion = %#v", got)
	}
	if stringValue(values, "text", "") != " value " || intValue(values, "int", 0) != 3 || int64Value(values, "int64", 0) != 4 {
		t.Fatalf("scalar conversion failed")
	}
	if intValue(values, "missing", 9) != 9 {
		t.Fatal("scalar fallback conversion failed")
	}

	if safeArtifactTaskID("task/with spaces") != "taskwithspaces" || safeArtifactTaskID("") != "desktop-artifact" {
		t.Fatal("artifact task id sanitization failed")
	}
	if rawArtifact(`{"ok":true}`) == nil || rawArtifact("not-json") != nil || formatRawArtifact(nil) != "" {
		t.Fatal("artifact conversion failed")
	}
	if !errors.Is(normalizeRuntimeAdapterError(errors.New("task canceled")), context.Canceled) || !errors.Is(normalizeRuntimeAdapterError(errors.New("task timed out")), context.DeadlineExceeded) {
		t.Fatal("runtime error normalization failed")
	}
	for _, adapter := range []string{"shell", "powershell", "codex", "claude-code", "acpx", "file", "desktop"} {
		if resultSchemaForAdapter(adapter) == "" {
			t.Fatalf("missing result schema for %s", adapter)
		}
	}
}

func TestHostRunnerCapabilityRequirementsAcrossAdapters(t *testing.T) {
	cases := []struct {
		adapter string
		caps    []string
		want    string
	}{
		{"shell", nil, "shell.user"},
		{"powershell", nil, "powershell.user"},
		{"codex", []string{"codex.run"}, "git.diff"},
		{"claude-code", []string{"claude-code.run"}, "git.diff"},
		{"acpx", []string{"acpx.run"}, "git.diff"},
		{"file", []string{"file.transfer.write"}, "fs.write.scoped"},
		{"desktop", []string{"window.inspect"}, "screen.screenshot"},
	}
	for _, tc := range cases {
		envelope := taskEnvelope{Adapter: tc.adapter, Capabilities: tc.caps}
		if tc.adapter == "file" {
			envelope.Payload = map[string]any{"action": "write"}
		}
		if tc.adapter == "desktop" {
			envelope.Payload = map[string]any{"action": "screen.screenshot"}
		}
		if got := missingAdapterCapability(envelope); got != tc.want {
			t.Fatalf("adapter %s missing capability=%q, want %q", tc.adapter, got, tc.want)
		}
	}
	for _, action := range []string{"list", "read", "download"} {
		if got := missingFileCapability(taskEnvelope{Payload: map[string]any{"action": action}, Capabilities: []string{"file.transfer.read"}}); got != "" {
			t.Fatalf("read action %s should be allowed, got %q", action, got)
		}
	}
	for _, action := range []string{"write", "upload", "delete"} {
		if got := missingFileCapability(taskEnvelope{Payload: map[string]any{"action": action}, Capabilities: []string{"file.transfer.write", "fs.write.scoped"}}); got != "" {
			t.Fatalf("write action %s should be allowed, got %q", action, got)
		}
	}
	for _, action := range []string{"windows", "record", "focus", "move", "keyboard", "mouse", "launch", "close", "url.open", "clipboard.read", "clipboard.write"} {
		if got := missingDesktopCapability(taskEnvelope{Payload: map[string]any{"action": action}, Capabilities: []string{
			"window.inspect", "screen.record", "window.focus", "window.move", "input.keyboard", "input.mouse", "app.launch", "app.close", "url.open", "clipboard.read", "clipboard.write",
		}}); got != "" {
			t.Fatalf("desktop action %s should be allowed, got %q", action, got)
		}
	}
}

func TestHostRuntimeAdapterLifecycleCapturesShellArtifact(t *testing.T) {
	workspaceRoot := t.TempDir()
	envelope := taskEnvelope{
		TaskID:       "task-runtime",
		Adapter:      "shell",
		Intent:       "runtime lifecycle",
		Workspace:    model.TaskWorkspace{Root: workspaceRoot, WriteScope: []string{workspaceRoot}},
		Capabilities: []string{"shell.user"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 4096},
		Payload:      map[string]any{"argv": []string{"go", "env", "GOOS"}, "allow_commands": []string{"go"}},
	}
	released := 0
	runtimeAdapter := &hostRuntimeAdapter{envelope: envelope, releaseWorkspaceLock: func() { released++ }}
	request := adapterkit.RuntimeRequest{Adapter: "shell", TaskID: envelope.TaskID, WorkspaceRoot: workspaceRoot, Intent: envelope.Intent}
	if _, err := runtimeAdapter.Detect(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeAdapter.Plan(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeAdapter.Prepare(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	run, err := runtimeAdapter.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if run.ArtifactSchema == "" || runtimeAdapter.artifactContent == "" {
		t.Fatalf("runtime run did not capture artifact: %#v", run)
	}
	collect, err := runtimeAdapter.Collect(context.Background(), request)
	if err != nil || collect.ResultArtifact == nil {
		t.Fatalf("runtime collect failed: %#v %v", collect, err)
	}
	if _, err := runtimeAdapter.Cleanup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("cleanup release count=%d, want 1", released)
	}

	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID: "task-fixture", EndpointID: "endpoint", Adapter: "shell", Intent: "fixture",
		Workspace: envelope.Workspace, Capabilities: envelope.Capabilities, Limits: envelope.Limits, Payload: envelope.Payload,
	}, time.Now().UTC(), Options{CaptureRuntimeFixture: true})
	if err != nil || result.RuntimeFixtureContent == "" {
		t.Fatalf("runtime fixture capture failed: %v %#v", err, result)
	}
}

func TestHostRuntimeAdapterEdgeStatesAndDirectAdapters(t *testing.T) {
	missing := &hostRuntimeAdapter{}
	if _, err := missing.Collect(context.Background(), adapterkit.RuntimeRequest{}); err == nil {
		t.Fatal("collect without an artifact should fail")
	}
	if _, err := missing.Cleanup(context.Background(), adapterkit.RuntimeRequest{}); err != nil {
		t.Fatal(err)
	}
	if normalizeRuntimeAdapterError(errors.New("ordinary adapter error")).Error() != "ordinary adapter error" {
		t.Fatal("ordinary runtime error should remain unchanged")
	}
	if _, err := executeJobAdapter(context.Background(), taskEnvelope{
		TaskID: "task-direct", Adapter: "shell", Workspace: model.TaskWorkspace{Root: t.TempDir()},
		Limits:  model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 1024},
		Payload: map[string]any{"argv": []string{"go", "env", "GOOS"}, "allow_commands": []string{"go"}},
	}, false, nil); err != nil {
		t.Fatal(err)
	}
	fileRoot := t.TempDir()
	if _, err := executeJobAdapterDirect(context.Background(), taskEnvelope{
		TaskID: "file-task", Adapter: "file", Workspace: model.TaskWorkspace{Root: fileRoot},
		Limits:  model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 1024},
		Payload: map[string]any{"action": "list", "path": "."},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := executeJobAdapterDirect(context.Background(), taskEnvelope{
		TaskID: "desktop/task", Adapter: "desktop", Workspace: model.TaskWorkspace{Root: fileRoot},
		Limits:  model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 1024},
		Payload: map[string]any{"action": "window.inspect"},
	}); err == nil {
		t.Fatal("desktop adapter should fail closed on this non-Windows test host")
	}
	if workspaceLockDenial(workspace.ErrLocked).Code != "workspace_locked" {
		t.Fatal("locked workspace should produce retryable workspace_locked denial")
	}
}

func TestHostRunnerAdapterDenialMappings(t *testing.T) {
	cases := []struct {
		adapter string
		message string
		code    string
	}{
		{"shell", "command is not allowlisted", "command_not_allowlisted"},
		{"powershell", "powershell command is required", "adapter_payload_invalid"},
		{"codex", "prompt is required", "adapter_payload_invalid"},
		{"claude-code", "prompt is required", "adapter_payload_invalid"},
		{"acpx", "prompt is required", "adapter_payload_invalid"},
	}
	for _, tc := range cases {
		denial, ok := adapterDenial(tc.adapter, errors.New(tc.message))
		if !ok || denial.Code != tc.code {
			t.Fatalf("adapter %s denial=%#v ok=%v, want %s", tc.adapter, denial, ok, tc.code)
		}
	}
}

func TestHostRunnerDenialMappingsCoverSafetyBranches(t *testing.T) {
	for _, tc := range []struct {
		adapter string
		message string
		code    string
	}{
		{"shell", "write scope escapes workspace root", "workspace_escape"},
		{"shell", "workspace root is required", "workspace_required"},
		{"shell", "workspace root must be absolute", "workspace_invalid"},
		{"powershell", "write scope escapes workspace root", "workspace_escape"},
		{"powershell", "workspace root is required", "workspace_required"},
		{"powershell", "workspace root must be absolute", "workspace_invalid"},
		{"powershell", "powershell executable is required", "adapter_payload_invalid"},
		{"codex", "write scope escapes workspace root", "workspace_escape"},
		{"codex", "path is required", "workspace_invalid"},
		{"claude-code", "write scope escapes workspace root", "workspace_escape"},
		{"claude-code", "path is required", "workspace_invalid"},
		{"acpx", "write scope escapes workspace root", "workspace_escape"},
		{"acpx", "path is required", "workspace_invalid"},
	} {
		denial, ok := adapterDenial(tc.adapter, errors.New(tc.message))
		if !ok || denial.Code != tc.code {
			t.Fatalf("adapter %s message %q denial=%#v ok=%v, want %s", tc.adapter, tc.message, denial, ok, tc.code)
		}
	}
	if _, ok := adapterDenial("shell", errors.New("unrelated")); ok {
		t.Fatal("unrelated adapter error should not become a denial")
	}
	if _, ok := adapterDenial("desktop", errors.New("unrelated")); ok {
		t.Fatal("unrelated desktop error should not become a shell denial")
	}

	locked := workspaceLockDenial(errors.New("lock conflict"))
	if locked.Code != "workspace_invalid" {
		t.Fatalf("unexpected generic workspace denial: %#v", locked)
	}
	denial := DenialError{Explanation: DenialExplanation{Code: "test", Summary: "summary"}, Cause: errors.New("cause")}
	if denial.Error() != "summary" || !errors.Is(denial, denial.Cause) {
		t.Fatal("denial error did not preserve summary and cause")
	}
}

func TestRunSessionTaskRejectsUnsupportedAndIncompleteTasks(t *testing.T) {
	now := time.Now().UTC()
	base := shellSessionTask(t.TempDir(), []string{"shell.user"})
	base.Adapter = "retired"
	if _, err := RunSessionTaskWithOptionsContext(context.Background(), base, now, Options{}); err == nil {
		t.Fatal("unsupported adapter should be denied")
	}
	base.Adapter = "shell"
	base.Workspace.Root = ""
	if _, err := RunSessionTaskWithOptionsContext(context.Background(), base, now, Options{}); err == nil {
		t.Fatal("missing workspace should be denied")
	}
	base.Workspace.Root = t.TempDir()
	base.Payload = map[string]any{"argv": []string{"not-allowed"}, "allow_commands": []string{"go"}}
	if _, err := RunSessionTaskWithOptionsContext(context.Background(), base, now, Options{}); err == nil {
		t.Fatal("adapter-side command denial should be returned")
	}

	envelope := sessionTaskEnvelope(SessionTaskSpec{TaskID: "defaults", Adapter: "shell", Payload: map[string]any{"key": "value"}}, now)
	if envelope.Limits.MaxDurationSeconds == 0 || envelope.Limits.MaxOutputBytes == 0 || envelope.Limits.Network != "default-deny" {
		t.Fatalf("session task defaults were not applied: %#v", envelope.Limits)
	}
	if cloneMap(nil) != nil || cloneMap(map[string]any{"key": "value"})["key"] != "value" {
		t.Fatal("task payload cloning failed")
	}
}
