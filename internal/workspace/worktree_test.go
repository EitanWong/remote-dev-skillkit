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

func TestPrepareGitWorktreeHonorsRequireCleanWithoutChangingOriginalWorkspace(t *testing.T) {
	requireGit(t)
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	repo := initGitRepo(t)
	storeDir := filepath.Join(t.TempDir(), "locks")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# original dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareGitWorktree(context.Background(), GitWorktreeOptions{
		StoreDir:    storeDir,
		RepoRoot:    repo,
		HostID:      "hst_1",
		TaskID:      "task_require_clean",
		BaseRef:     "HEAD",
		DirtyPolicy: GitWorktreeDirtyPolicyRequireClean,
		TTL:         time.Hour,
	}, now)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("require-clean must reject a dirty source workspace, err=%v", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(repo, "README.md")); readErr != nil || string(got) != "# original dirty\n" {
		t.Fatalf("require-clean rejection must preserve original content, got=%q err=%v", got, readErr)
	}
	status, statusErr := NewFileLockStore(storeDir).Status(repo, now.Add(time.Minute))
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if status.Exists {
		t.Fatalf("require-clean rejection must not leave a lock: %#v", status)
	}
}

func TestPrepareGitWorktreeSanitizesDefaultBranch(t *testing.T) {
	if got := safeGitName("task:one/../two"); got != "task_one____two" {
		t.Fatalf("unexpected safe name %q", got)
	}
}

func TestFinalizeGitWorktreeCapturesEvidenceReleasesLockAndPreservesOriginalDirty(t *testing.T) {
	requireGit(t)
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	repo := initGitRepo(t)
	storeDir := filepath.Join(t.TempDir(), "locks")
	worktreeRoot := filepath.Join(t.TempDir(), "worktrees")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# original dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prepared, err := PrepareGitWorktree(context.Background(), GitWorktreeOptions{
		StoreDir:     storeDir,
		WorktreeRoot: worktreeRoot,
		RepoRoot:     repo,
		HostID:       "hst_1",
		TaskID:       "task_finalize",
		OwnerAdapter: "codex",
		BaseRef:      "HEAD",
		DirtyPolicy:  GitWorktreeDirtyPolicyPreserve,
		TTL:          time.Hour,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.WorktreePath, "README.md"), []byte("# task change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.WorktreePath, "new.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	evidence, err := FinalizeGitWorktree(context.Background(), GitWorktreeFinalizeOptions{
		StoreDir: storeDir,
		TaskID:   "task_finalize",
		Worktree: prepared,
		Cleanup:  GitWorktreeCleanupPreserve,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.SchemaVersion != GitWorktreeEvidenceSchemaVersion || evidence.BaseSHA == "" || evidence.FinalSHA == "" || !evidence.Dirty {
		t.Fatalf("expected final worktree evidence, got %#v", evidence)
	}
	if !hasGitChangedFile(evidence.ChangedFiles, "README.md") || !hasGitChangedFile(evidence.ChangedFiles, "new.go") || evidence.DiffStatSHA256 == "" || evidence.DiffSHA256 == "" {
		t.Fatalf("expected changed-file and diff evidence, got %#v", evidence)
	}
	if !evidence.LockRelease.Attempted || !evidence.LockRelease.Released {
		t.Fatalf("expected lock release evidence, got %#v", evidence.LockRelease)
	}
	if got, err := os.ReadFile(filepath.Join(repo, "README.md")); err != nil || string(got) != "# original dirty\n" {
		t.Fatalf("original dirty workspace must be preserved, content=%q err=%v", got, err)
	}
	if _, err := os.Stat(prepared.WorktreePath); err != nil {
		t.Fatalf("preserve cleanup should retain worktree: %v", err)
	}
	status, err := NewFileLockStore(storeDir).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("finalize must release the lock: %#v", status)
	}
}

func TestResumeGitWorktreeReusesPreservedTaskDiff(t *testing.T) {
	requireGit(t)
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	repo := initGitRepo(t)
	storeDir := filepath.Join(t.TempDir(), "locks")
	prepared, err := PrepareGitWorktree(context.Background(), GitWorktreeOptions{
		StoreDir:     storeDir,
		WorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
		RepoRoot:     repo,
		HostID:       "hst_1",
		TaskID:       "task_resume_initial",
		OwnerAdapter: "codex",
		BaseRef:      "HEAD",
		TTL:          time.Hour,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.WorktreePath, "resume.txt"), []byte("preserved repair state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizeGitWorktree(context.Background(), GitWorktreeFinalizeOptions{
		StoreDir: storeDir,
		TaskID:   "task_resume_initial",
		Worktree: prepared,
		Cleanup:  GitWorktreeCleanupPreserve,
	}); err != nil {
		t.Fatal(err)
	}

	resumed, err := ResumeGitWorktree(context.Background(), GitWorktreeResumeOptions{
		StoreDir:     storeDir,
		RepoRoot:     repo,
		WorktreeRoot: prepared.WorktreePath,
		HostID:       "hst_1",
		TaskID:       "task_resume_next",
		OwnerAdapter: "codex",
		BaseSHA:      prepared.BaseSHA,
		Branch:       prepared.Branch,
		TTL:          time.Hour,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.WorktreePath != prepared.WorktreePath || resumed.Branch != prepared.Branch || resumed.BaseSHA != prepared.BaseSHA || resumed.Lock.TaskID != "task_resume_next" {
		t.Fatalf("resume did not reacquire the preserved worktree: prepared=%#v resumed=%#v", prepared, resumed)
	}
	if got, err := os.ReadFile(filepath.Join(resumed.WorktreePath, "resume.txt")); err != nil || string(got) != "preserved repair state\n" {
		t.Fatalf("resume must retain the previous task diff, content=%q err=%v", got, err)
	}
	if _, err := FinalizeGitWorktree(context.Background(), GitWorktreeFinalizeOptions{
		StoreDir: storeDir,
		TaskID:   "task_resume_next",
		Worktree: resumed,
		Cleanup:  GitWorktreeCleanupPreserve,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeGitWorktreeRemovesExplicitCleanupTarget(t *testing.T) {
	requireGit(t)
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	repo := initGitRepo(t)
	storeDir := filepath.Join(t.TempDir(), "locks")
	prepared, err := PrepareGitWorktree(context.Background(), GitWorktreeOptions{
		StoreDir:     storeDir,
		WorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
		RepoRoot:     repo,
		HostID:       "hst_1",
		TaskID:       "task_remove",
		BaseRef:      "HEAD",
		TTL:          time.Hour,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := FinalizeGitWorktree(context.Background(), GitWorktreeFinalizeOptions{
		StoreDir: storeDir,
		TaskID:   "task_remove",
		Worktree: prepared,
		Cleanup:  GitWorktreeCleanupRemove,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Cleanup != GitWorktreeCleanupRemove || !evidence.CleanupSucceeded || !evidence.LockRelease.Released {
		t.Fatalf("expected completed explicit cleanup evidence, got %#v", evidence)
	}
	if _, err := os.Stat(prepared.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("explicit cleanup should remove worktree, stat err=%v", err)
	}
}

func TestParseGitStatusChangesPreservesRenameAndCopySources(t *testing.T) {
	raw := "R  allowed/moved.txt\x00outside.txt\x00C  allowed/copied.txt\x00source.txt\x00 M  spaced name \x00"
	files, truncated := parseGitStatusChanges(raw)
	if truncated || len(files) != 3 {
		t.Fatalf("expected complete rename/copy status records, files=%#v truncated=%v", files, truncated)
	}
	if files[0].Status != "R " || files[0].Path != "allowed/moved.txt" || files[0].OriginalPath != "outside.txt" {
		t.Fatalf("rename source was not retained: %#v", files[0])
	}
	if files[1].Status != "C " || files[1].Path != "allowed/copied.txt" || files[1].OriginalPath != "source.txt" {
		t.Fatalf("copy source was not retained: %#v", files[1])
	}
	if files[2].Path != " spaced name " {
		t.Fatalf("status path whitespace must be preserved exactly: %#v", files[2])
	}
}

func TestParseGitStatusChangesFailsClosedForMalformedRenamePair(t *testing.T) {
	files, truncated := parseGitStatusChanges("R  allowed/moved.txt\x00")
	if !truncated || len(files) != 1 || files[0].Path != "allowed/moved.txt" || files[0].OriginalPath != "" {
		t.Fatalf("malformed rename status must retain bounded evidence and mark it incomplete: files=%#v truncated=%v", files, truncated)
	}
}

func hasGitChangedFile(files []GitChangedFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
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
