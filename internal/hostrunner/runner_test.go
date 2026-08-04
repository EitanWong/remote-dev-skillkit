package hostrunner

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

func TestSessionTaskRunnerDoesNotExposeRetiredJobRunner(t *testing.T) {
	content, err := os.ReadFile("runner.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, forbidden := range []string{
		"RunDevJob",
		"runDevJob",
		"AuthorizationRequiredSchemaVersion",
		"AuthorizationRequiredError",
		"hostnonce",
		"hostauthorization",
		"JobEnvelope",
		"JobClaimReceipt",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("hostrunner must expose only session task execution, found retired symbol %q", forbidden)
		}
	}
}

func TestRunSessionTaskAcceptsScopedShellTask(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()

	result, err := RunSessionTaskWithOptionsContext(context.Background(), shellSessionTask(repo, []string{"shell.user"}), now, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"exit_code": 0`) {
		t.Fatalf("expected successful shell artifact, got %s", result.ArtifactContent)
	}
}

func TestRunSessionTaskRejectsMissingCapabilityWithDenial(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()

	result, err := RunSessionTaskWithOptionsContext(context.Background(), shellSessionTask(repo, nil), now, Options{})
	assertDenial(t, result, err, "missing_capability")
}

func TestRunSessionTaskRejectsMalformedEngineeringTaskBeforeExecution(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	spec := shellSessionTask(repo, []string{"shell.user"})
	spec.Payload["engineering_task"] = map[string]any{
		"schema_version": "rdev.engineering-task.v1",
		"goal":           "Reject before running a host process.",
		"workspace": map[string]any{
			"root":         repo,
			"base_sha":     "not-a-sha",
			"isolation":    "git-worktree",
			"dirty_policy": "preserve",
			"read_scope":   []string{"."},
			"write_scope":  []string{"."},
		},
		"plan":       []string{"Inspect."},
		"acceptance": []string{"Tests pass."},
		"verification": map[string]any{
			"commands":       [][]string{{"go", "test", "./..."}},
			"allow_commands": []string{"go"},
		},
		"limits": map[string]any{
			"max_duration_seconds": 30,
			"max_output_bytes":     4096,
			"max_attempts":         1,
		},
		"network_policy":        "default-deny",
		"required_capabilities": []string{"shell.user"},
		"idempotency_key":       "host-invalid-engineering-task",
	}

	result, err := RunSessionTaskWithOptionsContext(context.Background(), spec, now, Options{})
	assertDenial(t, result, err, "invalid_engineering_task")
}

func TestNormalizeEngineeringSessionTaskSpecMapsContractFields(t *testing.T) {
	repo := t.TempDir()
	spec := SessionTaskSpec{
		Adapter: "shell",
		Payload: map[string]any{
			"engineering_task": map[string]any{
				"schema_version": "rdev.engineering-task.v1",
				"goal":           "Run the scoped engineering task.",
				"workspace": map[string]any{
					"root":         repo,
					"base_sha":     "0123456789abcdef0123456789abcdef01234567",
					"branch":       "rdev/task-shell",
					"isolation":    "git-worktree",
					"dirty_policy": "preserve",
					"read_scope":   []string{"."},
					"write_scope":  []string{"."},
				},
				"plan":       []string{"Inspect."},
				"acceptance": []string{"Tests pass."},
				"verification": map[string]any{
					"commands":       [][]string{{"go", "test", "./..."}},
					"allow_commands": []string{"go"},
				},
				"limits": map[string]any{
					"max_duration_seconds": 30,
					"max_output_bytes":     4096,
					"max_attempts":         2,
				},
				"network_policy":        "default-deny",
				"required_capabilities": []string{"shell.user"},
				"interrupts_required":   []string{"interrupt.network"},
				"idempotency_key":       "host-engineering-task",
			},
		},
	}

	normalized, err := normalizeEngineeringSessionTaskSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Intent != "Run the scoped engineering task." || normalized.Workspace.Root != repo || normalized.Workspace.Branch != "rdev/task-shell" || normalized.Workspace.BaseSHA != "0123456789abcdef0123456789abcdef01234567" || normalized.Workspace.Isolation != "git-worktree" || normalized.Workspace.DirtyPolicy != "preserve" {
		t.Fatalf("contract did not normalize task workspace: %#v", normalized)
	}
	if normalized.Payload["worktree_cleanup"] != "preserve" || normalized.Payload["max_attempts"] != 2 {
		t.Fatalf("contract did not normalize bounded worktree execution values: %#v", normalized.Payload)
	}
	if normalized.Limits.MaxDurationSeconds != 30 || normalized.Limits.MaxOutputBytes != 4096 || normalized.Limits.Network != "default-deny" {
		t.Fatalf("contract did not normalize limits: %#v", normalized.Limits)
	}
	envelope := sessionTaskEnvelope(normalized, time.Now())
	if len(envelope.InterruptsRequired) != 1 || envelope.InterruptsRequired[0] != "interrupt.network" {
		t.Fatalf("contract did not carry required interrupts to the runtime: %#v", envelope)
	}
}

func TestRunSessionTaskAcquiresAndReleasesWorkspaceLock(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	lockStore := t.TempDir()

	result, err := RunSessionTaskWithOptionsContext(context.Background(), shellSessionTask(repo, []string{"shell.user"}), now, Options{
		WorkspaceLockStore: lockStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"exit_code": 0`) {
		t.Fatalf("expected successful shell artifact, got %s", result.ArtifactContent)
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected workspace lock to be released, got %#v", status.Lock)
	}
}

func TestRunSessionTaskRejectsLockedWorkspace(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	lockStore := t.TempDir()
	if _, err := workspace.NewFileLockStore(lockStore).Acquire(workspace.LockOptions{
		RepoRoot:     repo,
		HostID:       "endpoint_other",
		TaskID:       "task_existing",
		OwnerAdapter: "codex",
		TTL:          time.Hour,
	}, now); err != nil {
		t.Fatal(err)
	}

	result, err := RunSessionTaskWithOptionsContext(context.Background(), shellSessionTask(repo, []string{"shell.user"}), now, Options{
		WorkspaceLockStore: lockStore,
	})
	assertDenial(t, result, err, "workspace_locked")
}

func shellSessionTask(repo string, capabilities []string) SessionTaskSpec {
	return SessionTaskSpec{
		TaskID:              "task_shell",
		EndpointID:          "endpoint_target",
		IdentityFingerprint: "sha256:test",
		Adapter:             "shell",
		Intent:              "session shell smoke",
		Workspace: model.TaskWorkspace{
			Root:       repo,
			WriteScope: []string{repo},
			Branch:     "rdev/task_shell",
		},
		Capabilities: append([]string(nil), capabilities...),
		Limits: model.TaskLimits{
			MaxDurationSeconds: 30,
			MaxOutputBytes:     4096,
		},
		Payload: map[string]any{
			"argv":           []string{"go", "env", "GOOS"},
			"allow_commands": []string{"go"},
		},
	}
}

func assertDenial(t *testing.T, result Result, err error, code string) {
	t.Helper()
	var denial DenialError
	if !errors.As(err, &denial) {
		t.Fatalf("expected DenialError, got %T %v", err, err)
	}
	if denial.Explanation.Code != code {
		t.Fatalf("expected denial %q, got %#v", code, denial.Explanation)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "`+DenialSchemaVersion+`"`) {
		t.Fatalf("expected denial artifact, got %s", result.ArtifactContent)
	}
}

// TestPreflightDenialsCarryAgentActionableHints pins the fix for the
// trial-and-error retry loop observed against a live Windows host: every
// common preflight denial must tell the Agent exactly which field to add, so
// a retry succeeds instead of guessing one missing field per round trip.
func TestPreflightDenialsCarryAgentActionableHints(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()

	// workspace_required: every adapter needs an absolute workspace_root.
	noRoot := shellSessionTask(repo, []string{"shell.user"})
	noRoot.Workspace.Root = ""
	_, err := RunSessionTaskWithOptionsContext(context.Background(), noRoot, now, Options{})
	var denial DenialError
	if !errors.As(err, &denial) {
		t.Fatalf("expected DenialError, got %T %v", err, err)
	}
	if denial.Explanation.Code != "workspace_required" || denial.Explanation.Hint == "" {
		t.Fatalf("workspace_required denial must carry a hint: %#v", denial.Explanation)
	}

	// missing_capability: hint names the exact capability to add.
	noCap := shellSessionTask(repo, nil)
	_, err = RunSessionTaskWithOptionsContext(context.Background(), noCap, now, Options{})
	if !errors.As(err, &denial) {
		t.Fatalf("expected DenialError, got %T %v", err, err)
	}
	if denial.Explanation.Code != "missing_capability" || !strings.Contains(denial.Explanation.Hint, "shell.user") {
		t.Fatalf("missing_capability denial must name the capability in its hint: %#v", denial.Explanation)
	}

	// command_not_allowlisted: the executable-name hint must cover Windows
	// PowerShell, whose allowlist additionally needs the bare executable name
	// (LookPath-resolved full paths fail exact match).
	for _, mapErr := range []struct {
		name string
		fn   func(error) (denialSpec, bool)
	}{{"shell", shellDenial}, {"powershell", powershellDenial}} {
		spec, ok := mapErr.fn(errors.New("command \"powershell.exe\" is not allowlisted"))
		if !ok || spec.Code != "command_not_allowlisted" || spec.Hint == "" {
			t.Fatalf("%s not-allowlisted denial must carry a hint: %#v ok=%v", mapErr.name, spec, ok)
		}
		if mapErr.name == "powershell" && !strings.Contains(spec.Hint, "powershell_command") {
			t.Fatalf("powershell not-allowlisted hint must mention powershell_command: %#v", spec)
		}
	}
}
