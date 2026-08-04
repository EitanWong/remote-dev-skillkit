package contracts

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDecodeEngineeringTaskValidatesAndNormalizes(t *testing.T) {
	task, err := DecodeEngineeringTask(validEngineeringTask())
	if err != nil {
		t.Fatal(err)
	}
	if task.SchemaVersion != EngineeringTaskSchemaVersion {
		t.Fatalf("unexpected schema version %q", task.SchemaVersion)
	}
	if task.Goal != "Repair the bounded engineering task contract." {
		t.Fatalf("unexpected goal %q", task.Goal)
	}
	if task.Workspace.Root != "/workspace/repo" || task.Workspace.Isolation != EngineeringIsolationGitWorktree {
		t.Fatalf("unexpected workspace %#v", task.Workspace)
	}
	if task.Workspace.DirtyPolicy != EngineeringDirtyPolicyPreserve || task.Workspace.Cleanup != EngineeringWorktreeCleanupPreserve {
		t.Fatalf("unexpected workspace cleanup policy: %#v", task.Workspace)
	}
	if !reflect.DeepEqual(task.RequiredCapabilities, []string{"codex.run", "git.diff"}) {
		t.Fatalf("unexpected capabilities %#v", task.RequiredCapabilities)
	}
	if task.Limits.MaxAttempts != 2 || task.Limits.MaxDurationSeconds != 600 || task.Limits.MaxOutputBytes != 65536 {
		t.Fatalf("unexpected limits %#v", task.Limits)
	}
	payload := task.TaskPayload(nil)
	if payload["max_attempts"] != 2 {
		t.Fatalf("typed task must preserve bounded retry limit in host payload: %#v", payload)
	}
	if err := task.ValidateForAdapter("codex"); err != nil {
		t.Fatalf("valid codex engineering task rejected: %v", err)
	}
}

func TestDecodeEngineeringTaskRejectsMalformedOrUnsafeInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "unknown field",
			mutate: func(task map[string]any) {
				task["unexpected"] = true
			},
			want: "unknown field",
		},
		{
			name: "invalid base sha",
			mutate: func(task map[string]any) {
				task["workspace"].(map[string]any)["base_sha"] = "not-a-sha"
			},
			want: "base_sha",
		},
		{
			name: "unsupported isolation",
			mutate: func(task map[string]any) {
				task["workspace"].(map[string]any)["isolation"] = "shared-workspace"
			},
			want: "isolation",
		},
		{
			name: "unsupported worktree cleanup",
			mutate: func(task map[string]any) {
				task["workspace"].(map[string]any)["cleanup"] = "discard-later"
			},
			want: "cleanup",
		},
		{
			name: "empty verification command",
			mutate: func(task map[string]any) {
				task["verification"].(map[string]any)["commands"] = []any{[]any{}}
			},
			want: "verification command",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validEngineeringTask()
			test.mutate(input)
			_, err := DecodeEngineeringTask(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeEngineeringTask() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestEngineeringTaskRequiresWriteScopeForCodingAdapters(t *testing.T) {
	input := validEngineeringTask()
	input["workspace"].(map[string]any)["write_scope"] = []any{}
	task, err := DecodeEngineeringTask(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.ValidateForAdapter("codex"); err == nil || !strings.Contains(err.Error(), "write_scope") {
		t.Fatalf("ValidateForAdapter(codex) error = %v, want write_scope rejection", err)
	}
}

func TestEngineeringTaskSchemaPublishesAllBuiltInAdapterProfiles(t *testing.T) {
	schema := EngineeringTaskSchema()
	profiles, ok := schema["x-rdev-adapter-profiles"].([]AdapterTaskProfile)
	if !ok {
		t.Fatalf("engineering task schema has no typed adapter profiles: %#v", schema)
	}
	want := map[string]bool{
		"shell": true, "powershell": true, "codex": true, "claude-code": true,
		"acpx": true, "file": true, "desktop": true, "host-update": true,
	}
	for _, profile := range profiles {
		if !want[profile.Adapter] {
			t.Fatalf("unexpected adapter profile %#v", profile)
		}
		delete(want, profile.Adapter)
		if profile.SchemaVersion == "" || len(profile.RequiredCapabilities) == 0 || profile.PayloadExample == nil {
			t.Fatalf("incomplete adapter profile %#v", profile)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing adapter profiles %#v", want)
	}
}

// TestAdapterProfilesDocumentWorkspaceRequirement guards the agent-facing
// contract against regressing to the state where a submitted task payload
// could only be validated through denial round trips: every built-in adapter
// requires an absolute workspace_root, and examples must show it.
func TestAdapterProfilesDocumentWorkspaceRequirement(t *testing.T) {
	for _, profile := range AdapterTaskProfiles() {
		if profile.Adapter == "host-update" {
			if profile.WorkspaceRootRequired {
				t.Fatalf("host-update must declare workspace_root_required=false (control-plane adapter)")
			}
			continue
		}
		if !profile.WorkspaceRootRequired {
			t.Fatalf("adapter %s must declare workspace_root_required", profile.Adapter)
		}
		example := profile.PayloadExample
		if _, ok := example["workspace_root"]; !ok {
			t.Fatalf("adapter %s payload example must include workspace_root: %#v", profile.Adapter, example)
		}
	}
}

// TestPowerShellAdapterProfileDocumentsWindowsExecutableContract pins the
// Windows allowlist quirk that cost multiple denial round trips: the example
// must declare the bare executable name in allow_commands AND set
// powershell_command, or LookPath-resolved full paths fail exact-match
// allowlisting on the host.
func TestPowerShellAdapterProfileDocumentsWindowsExecutableContract(t *testing.T) {
	var profile AdapterTaskProfile
	for _, p := range AdapterTaskProfiles() {
		if p.Adapter == "powershell" {
			profile = p
			break
		}
	}
	if profile.Adapter == "" {
		t.Fatal("missing powershell adapter profile")
	}
	example := profile.PayloadExample
	if example["powershell_command"] != "powershell.exe" {
		t.Fatalf("powershell example must set powershell_command to the bare executable name: %#v", example)
	}
	allow, _ := example["allow_commands"].([]string)
	if !slices.Contains(allow, "powershell.exe") {
		t.Fatalf("powershell example allow_commands must include powershell.exe: %#v", allow)
	}
}

func validEngineeringTask() map[string]any {
	return map[string]any{
		"schema_version": EngineeringTaskSchemaVersion,
		"goal":           "Repair the bounded engineering task contract.",
		"workspace": map[string]any{
			"root":         "/workspace/repo",
			"base_sha":     "0123456789abcdef0123456789abcdef01234567",
			"branch":       "rdev/task_contract",
			"isolation":    EngineeringIsolationGitWorktree,
			"dirty_policy": EngineeringDirtyPolicyPreserve,
			"read_scope":   []any{"."},
			"write_scope":  []any{"internal/contracts", "internal/mcpstdio"},
		},
		"plan":       []any{"Inspect the current contract.", "Add the typed preflight."},
		"acceptance": []any{"Focused tests pass."},
		"verification": map[string]any{
			"commands":       []any{[]any{"go", "test", "./internal/contracts"}},
			"allow_commands": []any{"go"},
		},
		"limits": map[string]any{
			"max_duration_seconds": 600,
			"max_output_bytes":     65536,
			"max_attempts":         2,
		},
		"network_policy":        "default-deny",
		"required_capabilities": []any{"codex.run", "git.diff"},
		"interrupts_required":   []any{"interrupt.network"},
		"idempotency_key":       "engineering-task-contract-1",
	}
}
