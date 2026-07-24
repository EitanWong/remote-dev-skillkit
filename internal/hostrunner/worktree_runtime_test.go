package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

func TestHostRunnerGitWorktreeIsolatesTaskExecution(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree isolation test")
	}
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt is required for worktree isolation test")
	}
	repo := t.TempDir()
	first := "package fixture\n\nfunc First(){}\n"
	second := "package fixture\n\nfunc Second(){}\n"
	if err := os.WriteFile(filepath.Join(repo, "first.go"), []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "second.go"), []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	runnerGit(t, repo, "init")
	runnerGit(t, repo, "config", "user.email", "rdev@example.test")
	runnerGit(t, repo, "config", "user.name", "Remote Dev")
	runnerGit(t, repo, "add", ".")
	runnerGit(t, repo, "commit", "-m", "isolation fixture")

	lockStore := filepath.Join(t.TempDir(), "workspace-locks")
	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-worktree-isolation",
		EndpointID: "endpoint-worktree-isolation",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:        repo,
			WriteScope:  []string{"."},
			Branch:      "rdev/test_worktree_isolation",
			BaseSHA:     "HEAD",
			Isolation:   "git-worktree",
			DirtyPolicy: "preserve",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           []string{"gofmt", "-w", "first.go", "second.go"},
			"allow_commands": []string{"gofmt"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: lockStore})
	if err != nil {
		t.Fatal(err)
	}

	var artifact struct {
		WorkspaceRoot      string                         `json:"workspace_root"`
		WorkspaceIsolation *workspace.GitWorktreeEvidence `json:"workspace_isolation"`
	}
	if err := json.Unmarshal([]byte(result.ArtifactContent), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.WorkspaceIsolation == nil {
		t.Fatalf("task artifact must include worktree evidence: %s", result.ArtifactContent)
	}
	evidence := artifact.WorkspaceIsolation
	if evidence.WorktreePath == "" || evidence.WorktreePath == repo || artifact.WorkspaceRoot != evidence.WorktreePath {
		t.Fatalf("adapter must run in the isolated worktree: artifact=%#v evidence=%#v", artifact, evidence)
	}
	if evidence.BaseSHA == "" || evidence.FinalSHA == "" || !evidence.Dirty || !hasWorktreeChange(evidence.ChangedFiles, "first.go") || !hasWorktreeChange(evidence.ChangedFiles, "second.go") {
		t.Fatalf("missing terminal worktree evidence: %#v", evidence)
	}
	if !evidence.LockRelease.Attempted || !evidence.LockRelease.Released || evidence.Cleanup != workspace.GitWorktreeCleanupPreserve {
		t.Fatalf("expected preserved worktree and released lock: %#v", evidence)
	}
	if got, readErr := os.ReadFile(filepath.Join(repo, "first.go")); readErr != nil || string(got) != first {
		t.Fatalf("original checkout must remain unchanged, content=%q err=%v", got, readErr)
	}
	if output, statusErr := exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput(); statusErr != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("original checkout must remain clean, output=%q err=%v", output, statusErr)
	}
	if _, statErr := os.Stat(evidence.WorktreePath); statErr != nil {
		t.Fatalf("default preserve policy must keep worktree for review: %v", statErr)
	}
	status, statusErr := workspace.NewFileLockStore(lockStore).Status(repo, time.Now().UTC())
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if status.Exists {
		t.Fatalf("worktree task lock must be released: %#v", status)
	}
}

func TestHostRunnerNonGitFallbackLocksAndSnapshotsWriteScope(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt is required for non-Git workspace fallback test")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scoped"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scoped", "format.go"), []byte("package scoped\n\nfunc Format(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("unchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockStore := filepath.Join(t.TempDir(), "workspace-locks")
	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-non-git-snapshot",
		EndpointID: "endpoint-non-git-snapshot",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       root,
			WriteScope: []string{"scoped"},
			Isolation:  "git-worktree",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           []string{"gofmt", "-w", "scoped/format.go"},
			"allow_commands": []string{"gofmt"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: lockStore})
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		WorkspaceScope *workspace.WorkspaceScopeEvidence `json:"workspace_scope"`
	}
	if err := json.Unmarshal([]byte(result.ArtifactContent), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.WorkspaceScope == nil || !artifact.WorkspaceScope.Changed || artifact.WorkspaceScope.Before.FileCount != 2 || artifact.WorkspaceScope.After.FileCount != 2 || !containsWorkspaceScopeChange(artifact.WorkspaceScope.ChangedFiles, "scoped/format.go") {
		t.Fatalf("expected changed, bounded non-Git scope evidence: %#v", artifact.WorkspaceScope)
	}
	if got, readErr := os.ReadFile(filepath.Join(root, "outside.txt")); readErr != nil || string(got) != "unchanged\n" {
		t.Fatalf("fallback must not alter files outside write scope, content=%q err=%v", got, readErr)
	}
	status, statusErr := workspace.NewFileLockStore(lockStore).Status(root, time.Now().UTC())
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if status.Exists {
		t.Fatalf("non-Git fallback task lock must be released: %#v", status)
	}
}

func TestHostRunnerNonGitFallbackRejectsChangesOutsideWriteScope(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt is required for non-Git scope violation test")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "allowed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "allowed", "inside.go"), []byte("package allowed\n\nfunc Inside(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.go"), []byte("package fixture\n\nfunc Outside(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-non-git-scope",
		EndpointID: "endpoint-non-git-scope",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       root,
			WriteScope: []string{"allowed"},
			Isolation:  "git-worktree",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           []string{"gofmt", "-w", "allowed/inside.go", "outside.go"},
			"allow_commands": []string{"gofmt"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	var denial DenialError
	if !errors.As(err, &denial) || denial.Explanation.Code != "write_scope_violation" {
		t.Fatalf("expected non-Git write-scope denial, result=%#v err=%v", result, err)
	}
	var artifact struct {
		WorkspaceScope *workspace.WorkspaceScopeEvidence `json:"workspace_scope"`
		TaskDenial     *DenialExplanation                `json:"task_denial"`
	}
	if unmarshalErr := json.Unmarshal([]byte(result.ArtifactContent), &artifact); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if artifact.WorkspaceScope == nil || artifact.TaskDenial == nil || !containsWorkspaceScopeChange(artifact.WorkspaceScope.ChangedFiles, "outside.go") {
		t.Fatalf("non-Git scope violation must retain snapshot and denial evidence: %#v", artifact)
	}
}

func TestHostRunnerNonGitFallbackRejectsExternalSymlinkCreatedInWriteScope(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required for non-Git symlink scope test")
	}
	if _, err := exec.LookPath("ln"); err != nil {
		t.Skip("ln is required for non-Git symlink scope test")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "allowed"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-non-git-created-symlink",
		EndpointID: "endpoint-non-git-created-symlink",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       root,
			WriteScope: []string{"allowed"},
			Isolation:  "git-worktree",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           externalSymlinkCreationCommand(outside),
			"allow_commands": []string{"sh"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	var denial DenialError
	if !errors.As(err, &denial) || denial.Explanation.Code != "write_scope_violation" {
		t.Fatalf("new external symlink in non-Git scope must be denied, result=%#v err=%v", result, err)
	}
	var artifact struct {
		WorkspaceScope *workspace.WorkspaceScopeEvidence `json:"workspace_scope"`
		TaskDenial     *DenialExplanation                `json:"task_denial"`
	}
	if err := json.Unmarshal([]byte(result.ArtifactContent), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.WorkspaceScope == nil || artifact.TaskDenial == nil || !containsWorkspaceScopeChange(artifact.WorkspaceScope.ChangedFiles, "allowed/outside-link") {
		t.Fatalf("new external symlink denial must retain non-Git evidence: %#v", artifact)
	}
}

func TestHostRunnerNonGitFallbackRejectsUnverifiableTruncatedWorkspaceBeforeExecution(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required for non-Git truncated workspace test")
	}
	root := t.TempDir()
	seedOversizedNonGitScopeFixture(t, root)
	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-non-git-unverifiable-scope",
		EndpointID: "endpoint-non-git-unverifiable-scope",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       root,
			WriteScope: []string{"allowed"},
			Isolation:  "git-worktree",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           []string{"sh", "-c", "printf ran > command-ran.txt"},
			"allow_commands": []string{"sh"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	var denial DenialError
	if !errors.As(err, &denial) || denial.Explanation.Code != "write_scope_unverifiable" {
		t.Fatalf("truncated non-Git workspace must be rejected before execution, result=%#v err=%v", result, err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "command-ran.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("unverifiable workspace must not execute the command, stat err=%v", statErr)
	}
}

func TestHostRunnerNonGitFallbackRejectsTruncatedWriteScopeEvidence(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required for non-Git truncated scope test")
	}
	root := t.TempDir()
	seedTruncatedWriteScopeFixture(t, root)
	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-non-git-truncated-scope",
		EndpointID: "endpoint-non-git-truncated-scope",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       root,
			WriteScope: []string{"allowed"},
			Isolation:  "git-worktree",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           truncatedWriteScopeCommand(),
			"allow_commands": []string{"sh"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	var denial DenialError
	if !errors.As(err, &denial) || denial.Explanation.Code != "write_scope_violation" {
		t.Fatalf("truncated non-Git write evidence must fail closed, result=%#v err=%v", result, err)
	}
	var artifact struct {
		WorkspaceScope *workspace.WorkspaceScopeEvidence `json:"workspace_scope"`
		TaskDenial     *DenialExplanation                `json:"task_denial"`
	}
	if err := json.Unmarshal([]byte(result.ArtifactContent), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.WorkspaceScope == nil || artifact.TaskDenial == nil || !artifact.WorkspaceScope.ChangesTruncated || containsWorkspaceScopeChange(artifact.WorkspaceScope.ChangedFiles, "zz-outside.txt") {
		t.Fatalf("truncated non-Git evidence must retain the bounded snapshot and denial without falsely claiming a complete path list: %#v", artifact)
	}
}

func TestHostRunnerGitWorktreeHonorsExplicitRemoveCleanup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree cleanup test")
	}
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt is required for worktree cleanup test")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "format.go"), []byte("package fixture\n\nfunc Format(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runnerGit(t, repo, "init")
	runnerGit(t, repo, "config", "user.email", "rdev@example.test")
	runnerGit(t, repo, "config", "user.name", "Remote Dev")
	runnerGit(t, repo, "add", ".")
	runnerGit(t, repo, "commit", "-m", "cleanup fixture")

	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-worktree-remove",
		EndpointID: "endpoint-worktree-remove",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       repo,
			WriteScope: []string{"."},
			Isolation:  "git-worktree",
			BaseSHA:    "HEAD",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":             []string{"gofmt", "-w", "format.go"},
			"allow_commands":   []string{"gofmt"},
			"worktree_cleanup": string(workspace.GitWorktreeCleanupRemove),
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		WorkspaceIsolation *workspace.GitWorktreeEvidence `json:"workspace_isolation"`
	}
	if err := json.Unmarshal([]byte(result.ArtifactContent), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.WorkspaceIsolation == nil || artifact.WorkspaceIsolation.Cleanup != workspace.GitWorktreeCleanupRemove || !artifact.WorkspaceIsolation.CleanupSucceeded {
		t.Fatalf("expected explicit remove cleanup evidence: %#v", artifact.WorkspaceIsolation)
	}
	if _, statErr := os.Stat(artifact.WorkspaceIsolation.WorktreePath); !os.IsNotExist(statErr) {
		t.Fatalf("explicit cleanup must remove the worktree, stat err=%v", statErr)
	}
}

func TestHostRunnerGitWorktreeRejectsChangesOutsideWriteScope(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree write-scope test")
	}
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt is required for worktree write-scope test")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "allowed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "allowed", "inside.go"), []byte("package allowed\n\nfunc Inside(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "outside.go"), []byte("package fixture\n\nfunc Outside(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runnerGit(t, repo, "init")
	runnerGit(t, repo, "config", "user.email", "rdev@example.test")
	runnerGit(t, repo, "config", "user.name", "Remote Dev")
	runnerGit(t, repo, "add", ".")
	runnerGit(t, repo, "commit", "-m", "scope fixture")

	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-worktree-scope",
		EndpointID: "endpoint-worktree-scope",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       repo,
			WriteScope: []string{"allowed"},
			Isolation:  "git-worktree",
			BaseSHA:    "HEAD",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           []string{"gofmt", "-w", "allowed/inside.go", "outside.go"},
			"allow_commands": []string{"gofmt"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	var denial DenialError
	if !errors.As(err, &denial) || denial.Explanation.Code != "write_scope_violation" {
		t.Fatalf("expected write-scope denial, result=%#v err=%v", result, err)
	}
	var artifact struct {
		WorkspaceIsolation *workspace.GitWorktreeEvidence `json:"workspace_isolation"`
		TaskDenial         *DenialExplanation             `json:"task_denial"`
	}
	if unmarshalErr := json.Unmarshal([]byte(result.ArtifactContent), &artifact); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if artifact.WorkspaceIsolation == nil || artifact.TaskDenial == nil || !hasWorktreeChange(artifact.WorkspaceIsolation.ChangedFiles, "outside.go") {
		t.Fatalf("scope violation must retain worktree and denial evidence: %#v", artifact)
	}
}

func TestHostRunnerGitWorktreeRejectsRenameFromOutsideWriteScope(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree rename scope test")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required for worktree rename scope test")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "allowed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "allowed", ".keep"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "outside.txt"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runnerGit(t, repo, "init")
	runnerGit(t, repo, "config", "user.email", "rdev@example.test")
	runnerGit(t, repo, "config", "user.name", "Remote Dev")
	runnerGit(t, repo, "add", ".")
	runnerGit(t, repo, "commit", "-m", "rename scope fixture")

	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-worktree-rename-scope",
		EndpointID: "endpoint-worktree-rename-scope",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       repo,
			WriteScope: []string{"allowed"},
			Isolation:  "git-worktree",
			BaseSHA:    "HEAD",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           renameIntoAllowedScopeCommand(),
			"allow_commands": []string{"sh"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	var denial DenialError
	if !errors.As(err, &denial) || denial.Explanation.Code != "write_scope_violation" {
		t.Fatalf("rename from outside declared scope must be denied, result=%#v err=%v", result, err)
	}
	var artifact struct {
		WorkspaceIsolation *workspace.GitWorktreeEvidence `json:"workspace_isolation"`
		TaskDenial         *DenialExplanation             `json:"task_denial"`
	}
	if err := json.Unmarshal([]byte(result.ArtifactContent), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.WorkspaceIsolation == nil || artifact.TaskDenial == nil || !hasWorktreeChange(artifact.WorkspaceIsolation.ChangedFiles, "allowed/moved.txt") {
		t.Fatalf("rename scope denial must retain destination evidence: %#v", artifact)
	}
}

func TestHostRunnerGitWorktreeRejectsExternalSymlinkCreatedInWriteScope(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree symlink scope test")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required for worktree symlink scope test")
	}
	if _, err := exec.LookPath("ln"); err != nil {
		t.Skip("ln is required for worktree symlink scope test")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "allowed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "allowed", ".keep"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runnerGit(t, repo, "init")
	runnerGit(t, repo, "config", "user.email", "rdev@example.test")
	runnerGit(t, repo, "config", "user.name", "Remote Dev")

	runnerGit(t, repo, "add", ".")
	runnerGit(t, repo, "commit", "-m", "symlink scope fixture")
	outside := t.TempDir()

	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-worktree-created-symlink",
		EndpointID: "endpoint-worktree-created-symlink",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       repo,
			WriteScope: []string{"allowed"},
			Isolation:  "git-worktree",
			BaseSHA:    "HEAD",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":             externalSymlinkCreationCommand(outside),
			"allow_commands":   []string{"sh"},
			"worktree_cleanup": string(workspace.GitWorktreeCleanupRemove),
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	var denial DenialError
	if !errors.As(err, &denial) || denial.Explanation.Code != "write_scope_violation" {
		t.Fatalf("new external symlink in Git scope must be denied, result=%#v err=%v", result, err)
	}
	var artifact struct {
		WorkspaceIsolation *workspace.GitWorktreeEvidence `json:"workspace_isolation"`
		TaskDenial         *DenialExplanation             `json:"task_denial"`
	}
	if err := json.Unmarshal([]byte(result.ArtifactContent), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.WorkspaceIsolation == nil || artifact.TaskDenial == nil || !hasWorktreeChange(artifact.WorkspaceIsolation.ChangedFiles, "allowed/outside-link") || artifact.WorkspaceIsolation.Cleanup != workspace.GitWorktreeCleanupRemove || !artifact.WorkspaceIsolation.CleanupSucceeded {
		t.Fatalf("new external symlink denial must retain Git evidence: %#v", artifact)
	}
	if _, statErr := os.Stat(artifact.WorkspaceIsolation.WorktreePath); !os.IsNotExist(statErr) {
		t.Fatalf("scope denial with remove cleanup must remove the worktree, stat err=%v", statErr)
	}
}

func TestHostRunnerGitWorktreeRejectsTruncatedWriteScopeEvidence(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree truncated scope test")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required for worktree truncated scope test")
	}
	repo := t.TempDir()
	seedTruncatedWriteScopeFixture(t, repo)
	runnerGit(t, repo, "init")
	runnerGit(t, repo, "config", "user.email", "rdev@example.test")
	runnerGit(t, repo, "config", "user.name", "Remote Dev")
	runnerGit(t, repo, "add", ".")
	runnerGit(t, repo, "commit", "-m", "truncated scope fixture")

	result, err := RunSessionTaskWithOptionsContext(context.Background(), SessionTaskSpec{
		TaskID:     "task-worktree-truncated-scope",
		EndpointID: "endpoint-worktree-truncated-scope",
		Adapter:    "shell",
		Workspace: model.TaskWorkspace{
			Root:       repo,
			WriteScope: []string{"allowed"},
			Isolation:  "git-worktree",
			BaseSHA:    "HEAD",
		},
		Capabilities: []string{"shell.user", "git.diff"},
		Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
		Payload: map[string]any{
			"argv":           truncatedWriteScopeCommand(),
			"allow_commands": []string{"sh"},
		},
	}, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	var denial DenialError
	if !errors.As(err, &denial) || denial.Explanation.Code != "write_scope_violation" {
		t.Fatalf("truncated Git write evidence must fail closed, result=%#v err=%v", result, err)
	}
	var artifact struct {
		WorkspaceIsolation *workspace.GitWorktreeEvidence `json:"workspace_isolation"`
		TaskDenial         *DenialExplanation             `json:"task_denial"`
	}
	if err := json.Unmarshal([]byte(result.ArtifactContent), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.WorkspaceIsolation == nil || artifact.TaskDenial == nil || !artifact.WorkspaceIsolation.ChangesTruncated || hasWorktreeChange(artifact.WorkspaceIsolation.ChangedFiles, "zz-outside.txt") {
		t.Fatalf("truncated Git evidence must retain the bounded diff and denial without falsely claiming a complete path list: %#v", artifact)
	}
}

func TestHostRunnerGitWorktreeCancellationFinalizesAndReleasesLock(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree cancellation test")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required for worktree cancellation test")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep is required for worktree cancellation test")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runnerGit(t, repo, "init")
	runnerGit(t, repo, "config", "user.email", "rdev@example.test")
	runnerGit(t, repo, "config", "user.name", "Remote Dev")
	runnerGit(t, repo, "add", ".")
	runnerGit(t, repo, "commit", "-m", "cancel fixture")
	lockStore := filepath.Join(t.TempDir(), "workspace-locks")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type taskOutcome struct {
		result Result
		err    error
	}
	done := make(chan taskOutcome, 1)
	go func() {
		result, err := RunSessionTaskWithOptionsContext(ctx, SessionTaskSpec{
			TaskID:     "task-worktree-cancel",
			EndpointID: "endpoint-worktree-cancel",
			Adapter:    "shell",
			Workspace: model.TaskWorkspace{
				Root:       repo,
				WriteScope: []string{"."},
				Isolation:  "git-worktree",
				BaseSHA:    "HEAD",
			},
			Capabilities: []string{"shell.user", "git.diff"},
			Limits:       model.TaskLimits{MaxDurationSeconds: 30, MaxOutputBytes: 64 * 1024},
			Payload: map[string]any{
				"argv":           []string{"sh", "-c", "printf started > .rdev-cancel-started && sleep 5"},
				"allow_commands": []string{"sh"},
			},
		}, time.Now().UTC(), Options{WorkspaceLockStore: lockStore})
		done <- taskOutcome{result: result, err: err}
	}()

	worktreePath := filepath.Join(filepath.Dir(lockStore), "worktrees", "task_task-worktree-cancel")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(worktreePath, ".rdev-cancel-started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for task execution to start in worktree")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case outcome := <-done:
		if outcome.err == nil {
			t.Fatal("canceled worktree task must return an error")
		}
		var artifact struct {
			WorkspaceIsolation *workspace.GitWorktreeEvidence `json:"workspace_isolation"`
		}
		if err := json.Unmarshal([]byte(outcome.result.ArtifactContent), &artifact); err != nil {
			t.Fatal(err)
		}
		if artifact.WorkspaceIsolation == nil || !artifact.WorkspaceIsolation.LockRelease.Released || artifact.WorkspaceIsolation.Cleanup != workspace.GitWorktreeCleanupPreserve {
			t.Fatalf("canceled task must retain final worktree and release lock evidence: %#v", artifact.WorkspaceIsolation)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled worktree task did not finish")
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("canceled task lock must be released: %#v", status)
	}
}

func hasWorktreeChange(files []workspace.GitChangedFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func containsWorkspaceScopeChange(files []string, path string) bool {
	for _, file := range files {
		if file == path {
			return true
		}
	}
	return false
}

func seedTruncatedWriteScopeFixture(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "allowed"), 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= 200; index++ {
		path := filepath.Join(root, "allowed", fmt.Sprintf("%03d.txt", index))
		if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "zz-outside.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func seedOversizedNonGitScopeFixture(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "allowed"), 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= 20_000; index++ {
		path := filepath.Join(root, "allowed", fmt.Sprintf("%05d.txt", index))
		if err := os.WriteFile(path, []byte("fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func truncatedWriteScopeCommand() []string {
	return []string{"sh", "-c", `for file in allowed/*.txt; do printf changed > "$file"; done; printf escaped > zz-outside.txt`}
}

func externalSymlinkCreationCommand(outside string) []string {
	return []string{"sh", "-c", `ln -s "$1" allowed/outside-link`, "rdev", outside}
}

func renameIntoAllowedScopeCommand() []string {
	return []string{"sh", "-c", `mv outside.txt allowed/moved.txt && git add -A`}
}
