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
