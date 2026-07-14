package gitworkflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultWorktreeRootIsOutsideRepository(t *testing.T) {
	repo := initGitRepo(t)

	root, err := DefaultWorktreeRoot(repo)
	if err != nil {
		t.Fatalf("DefaultWorktreeRoot() error = %v", err)
	}

	want := filepath.Join(filepath.Dir(repo), ".worktrees", filepath.Base(repo))
	if root != want {
		t.Fatalf("DefaultWorktreeRoot() = %q, want %q", root, want)
	}
	if isWithinPath(repo, root) {
		t.Fatalf("worktree root %q is inside repository %q", root, repo)
	}
}

func TestCreateDeveloperWorktreeUsesNormalizedBranchDirectory(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}

	report, err := manager.Create(context.Background(), "feat/123-worktree-governance", "main")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	wantPath := filepath.Join(root, "feat-123-worktree-governance")
	if report.Worktree != wantPath {
		t.Fatalf("Create() worktree = %q, want %q", report.Worktree, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("created worktree missing: %v", err)
	}
	if report.OK != true || len(report.Commands) == 0 {
		t.Fatalf("Create() report = %#v, want successful command evidence", report)
	}
}

func TestCreateRejectsMainAndDuplicateBinding(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}

	if _, err := manager.Create(context.Background(), "main", "main"); err == nil {
		t.Fatal("Create(main) expected error")
	}

	branch := "feat/123-duplicate-binding"
	if _, err := manager.Create(context.Background(), branch, "main"); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if _, err := manager.Create(context.Background(), branch, "main"); err == nil {
		t.Fatal("second Create() expected duplicate binding error")
	}
}

func TestDoctorReportsDirtyAndDetachedWorktrees(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}

	if _, err := manager.Create(context.Background(), "feat/123-doctor", "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	dirtyPath := filepath.Join(root, "feat-123-doctor")
	if err := os.WriteFile(filepath.Join(dirtyPath, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	detachedPath := filepath.Join(root, "detached")
	runGitForTest(t, repo, "worktree", "add", "--detach", detachedPath, "HEAD")

	report, err := manager.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}

	var sawDirty, sawDetached bool
	for _, entry := range report.Entries {
		if entry.Branch == "feat/123-doctor" && !entry.Clean {
			sawDirty = true
		}
		if entry.Path == canonicalPathForTest(t, detachedPath) && entry.Detached {
			sawDetached = true
		}
	}
	if !sawDirty || !sawDetached {
		t.Fatalf("Doctor() entries = %#v, want dirty and detached entries", report.Entries)
	}
}

func TestRemoveRejectsDirtyWorktreeWithoutForce(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}

	branch := "feat/123-dirty-remove"
	if _, err := manager.Create(context.Background(), branch, "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "feat-123-dirty-remove", "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Remove(context.Background(), branch, false); err == nil {
		t.Fatal("Remove() expected dirty-worktree error")
	}
	if _, err := os.Stat(filepath.Join(root, "feat-123-dirty-remove")); err != nil {
		t.Fatalf("dirty worktree was removed: %v", err)
	}
}

func TestCleanRemovesMergedCleanWorktreeOnly(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}

	mergedBranch := "feat/123-merged-cleanup"
	if _, err := manager.Create(context.Background(), mergedBranch, "main"); err != nil {
		t.Fatalf("Create(merged) error = %v", err)
	}
	mergedPath := filepath.Join(root, "feat-123-merged-cleanup")
	if err := os.WriteFile(filepath.Join(mergedPath, "merged.txt"), []byte("merged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, mergedPath, "add", "merged.txt")
	runGitForTest(t, mergedPath, "commit", "-m", "merge worktree")
	runGitForTest(t, repo, "merge", "--ff-only", mergedBranch)

	unmergedBranch := "feat/124-unmerged-cleanup"
	if _, err := manager.Create(context.Background(), unmergedBranch, "main"); err != nil {
		t.Fatalf("Create(unmerged) error = %v", err)
	}
	unmergedPath := filepath.Join(root, "feat-124-unmerged-cleanup")
	if err := os.WriteFile(filepath.Join(unmergedPath, "unmerged.txt"), []byte("unmerged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, unmergedPath, "add", "unmerged.txt")
	runGitForTest(t, unmergedPath, "commit", "-m", "leave unmerged")

	report, err := manager.Clean(context.Background())
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if !report.OK {
		t.Fatalf("Clean() report = %#v, want successful cleanup", report)
	}
	if _, err := os.Stat(mergedPath); !os.IsNotExist(err) {
		t.Fatalf("merged worktree still exists: %v", err)
	}
	if _, err := os.Stat(unmergedPath); err != nil {
		t.Fatalf("unmerged worktree was removed or missing: %v", err)
	}
	if strings.Contains(strings.Join(report.Errors, "\n"), mergedBranch) {
		t.Fatalf("Clean() reported an error for merged worktree: %#v", report.Errors)
	}
}
