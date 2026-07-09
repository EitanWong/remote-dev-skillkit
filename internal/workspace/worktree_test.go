package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrepareGitWorktreeCreatesWorktreeAndLock(t *testing.T) {
	requireGit(t)
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repo := initGitRepo(t)
	storeDir := filepath.Join(t.TempDir(), "locks")

	result, err := PrepareGitWorktree(context.Background(), GitWorktreeOptions{
		StoreDir:     storeDir,
		RepoRoot:     repo,
		HostID:       "hst_1",
		TaskID:       "task_1",
		OwnerAdapter: "codex",
		BaseRef:      "HEAD",
		TTL:          time.Hour,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != GitWorktreePlanSchemaVersion {
		t.Fatalf("unexpected schema %q", result.SchemaVersion)
	}
	if result.Lock.TaskID != "task_1" || result.Lock.OwnerAdapter != "codex" {
		t.Fatalf("unexpected lock %#v", result.Lock)
	}
	if result.Branch != "rdev/task_task_1" {
		t.Fatalf("unexpected branch %q", result.Branch)
	}
	if _, err := os.Stat(filepath.Join(result.WorktreePath, "README.md")); err != nil {
		t.Fatalf("expected worktree checkout: %v", err)
	}
	status, err := NewFileLockStore(storeDir).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists || status.Lock == nil || status.Lock.WorktreePath != result.WorktreePath {
		t.Fatalf("expected worktree lock, got %#v", status)
	}
	if len(result.Commands) < 2 {
		t.Fatalf("expected command evidence, got %#v", result.Commands)
	}
}

func TestPrepareGitWorktreeRejectsConcurrentWriter(t *testing.T) {
	requireGit(t)
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repo := initGitRepo(t)
	storeDir := filepath.Join(t.TempDir(), "locks")
	if _, err := PrepareGitWorktree(context.Background(), GitWorktreeOptions{
		StoreDir: storeDir,
		RepoRoot: repo,
		HostID:   "hst_1",
		TaskID:   "task_1",
		TTL:      time.Hour,
	}, now); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareGitWorktree(context.Background(), GitWorktreeOptions{
		StoreDir: storeDir,
		RepoRoot: repo,
		HostID:   "hst_1",
		TaskID:   "task_2",
		TTL:      time.Hour,
	}, now.Add(time.Minute))
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
}

func TestPrepareGitWorktreeReleasesLockWhenGitFails(t *testing.T) {
	requireGit(t)
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repo := initGitRepo(t)
	storeDir := filepath.Join(t.TempDir(), "locks")

	_, err := PrepareGitWorktree(context.Background(), GitWorktreeOptions{
		StoreDir: storeDir,
		RepoRoot: repo,
		HostID:   "hst_1",
		TaskID:   "task_1",
		BaseRef:  "missing-ref-for-rdev-test",
		TTL:      time.Hour,
	}, now)
	if err == nil {
		t.Fatal("expected missing base ref to fail")
	}
	status, statusErr := NewFileLockStore(storeDir).Status(repo, now.Add(time.Minute))
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if status.Exists {
		t.Fatalf("expected lock cleanup after failure, got %#v", status)
	}
}

func TestPrepareGitWorktreeSanitizesDefaultBranch(t *testing.T) {
	if got := safeGitName("task:one/../two"); got != "task_one____two" {
		t.Fatalf("unexpected safe name %q", got)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitForTest(t, repo, "init")
	runGitForTest(t, repo, "config", "user.email", "rdev-test@example.com")
	runGitForTest(t, repo, "config", "user.name", "Rdev Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, repo, "add", "README.md")
	runGitForTest(t, repo, "commit", "-m", "initial")
	return repo
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
