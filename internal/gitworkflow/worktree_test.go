package gitworkflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestDefaultWorktreeRootIsOutsideRepository(t *testing.T) {
	repo := initGitRepo(t)

	root, err := DefaultWorktreeRoot(repo)
	if err != nil {
		t.Fatalf("DefaultWorktreeRoot() error = %v", err)
	}

	canonicalRepo := canonicalPathForTest(t, repo)
	want := filepath.Join(filepath.Dir(canonicalRepo), ".worktrees", filepath.Base(canonicalRepo))
	if root != want {
		t.Fatalf("DefaultWorktreeRoot() = %q, want %q", root, want)
	}
	if isWithinPath(repo, root) {
		t.Fatalf("worktree root %q is inside repository %q", root, repo)
	}
}

func TestDefaultWorktreeRootUsesCommonRepositoryNameForLinkedCheckout(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	developerRoot := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, developerRoot, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	if _, err := manager.Create(context.Background(), "feat/123-linked-root", "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	linkedCheckout := filepath.Join(developerRoot, "feat-123-linked-root")
	t.Cleanup(func() {
		runGitForTest(t, repo, "worktree", "remove", "--force", linkedCheckout)
	})

	root, err := DefaultWorktreeRoot(linkedCheckout)
	if err != nil {
		t.Fatalf("DefaultWorktreeRoot() error = %v", err)
	}

	canonicalRepo := canonicalPathForTest(t, repo)
	want := filepath.Join(filepath.Dir(canonicalRepo), ".worktrees", filepath.Base(canonicalRepo))
	if root != want {
		t.Fatalf("DefaultWorktreeRoot() = %q, want %q", root, want)
	}
}

func TestLinkedManagerNeverCleansOrRemovesItsOwnCheckout(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	developerRoot, err := DefaultWorktreeRoot(repo)
	if err != nil {
		t.Fatalf("DefaultWorktreeRoot() error = %v", err)
	}
	manager, err := NewWorktreeManager(repo, developerRoot, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	branch := "feat/123-linked-manager"
	if _, err := manager.Create(context.Background(), branch, "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	linkedCheckout := filepath.Join(developerRoot, "feat-123-linked-manager")
	t.Cleanup(func() {
		runGitForTest(t, repo, "worktree", "remove", "--force", linkedCheckout)
	})

	linkedManager, err := NewWorktreeManager(linkedCheckout, "", ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager(linked checkout) error = %v", err)
	}
	if _, err := linkedManager.Clean(context.Background()); err != nil {
		t.Fatalf("linked manager Clean() error = %v", err)
	}
	if _, err := os.Stat(linkedCheckout); err != nil {
		t.Fatalf("linked manager checkout was removed by Clean(): %v", err)
	}

	if _, err := linkedManager.Remove(context.Background(), branch, false); err == nil {
		t.Fatal("linked manager Remove() expected manager-checkout rejection")
	} else if !strings.Contains(err.Error(), "cannot remove manager checkout") {
		t.Fatalf("linked manager Remove() error = %v, want manager-checkout rejection", err)
	}
	if _, err := os.Stat(linkedCheckout); err != nil {
		t.Fatalf("linked manager checkout was removed by Remove(): %v", err)
	}
	runGitForTest(t, repo, "show-ref", "--verify", "refs/heads/"+branch)
}

func TestLinkedManagerCleansDifferentMergedWorktreeFromMainCheckout(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	developerRoot, err := DefaultWorktreeRoot(repo)
	if err != nil {
		t.Fatalf("DefaultWorktreeRoot() error = %v", err)
	}
	manager, err := NewWorktreeManager(repo, developerRoot, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	managerBranch := "feat/123-linked-clean-manager"
	targetBranch := "feat/124-linked-clean-target"
	if _, err := manager.Create(context.Background(), managerBranch, "main"); err != nil {
		t.Fatalf("Create(manager) error = %v", err)
	}
	if _, err := manager.Create(context.Background(), targetBranch, "main"); err != nil {
		t.Fatalf("Create(target) error = %v", err)
	}
	managerPath := filepath.Join(developerRoot, "feat-123-linked-clean-manager")
	targetPath := filepath.Join(developerRoot, "feat-124-linked-clean-target")
	t.Cleanup(func() {
		runGitForTest(t, repo, "worktree", "remove", "--force", managerPath)
		runGitForTest(t, repo, "branch", "-D", managerBranch)
	})

	if err := os.WriteFile(filepath.Join(targetPath, "merged.txt"), []byte("merged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, targetPath, "add", "merged.txt")
	runGitForTest(t, targetPath, "commit", "-m", "merge target worktree")
	runGitForTest(t, repo, "merge", "--ff-only", targetBranch)

	linkedManager, err := NewWorktreeManager(managerPath, "", ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager(linked manager) error = %v", err)
	}
	if _, err := linkedManager.Clean(context.Background()); err != nil {
		t.Fatalf("linked manager Clean() error = %v", err)
	}
	if _, err := os.Stat(managerPath); err != nil {
		t.Fatalf("manager checkout was removed: %v", err)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("target worktree was not removed: %v", err)
	}
	if err := runGitForTestOutput(t, repo, "show-ref", "--verify", "refs/heads/"+targetBranch); err == nil {
		t.Fatalf("target branch %q still exists after cleanup", targetBranch)
	}
}

func TestLinkedManagerCleanWithBroadRootSkipsMainWithoutDestructiveCommands(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	defaultRoot, err := DefaultWorktreeRoot(repo)
	if err != nil {
		t.Fatalf("DefaultWorktreeRoot() error = %v", err)
	}
	manager, err := NewWorktreeManager(repo, defaultRoot, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	branch := "feat/125-broad-root-manager"
	if _, err := manager.Create(context.Background(), branch, "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	managerPath := filepath.Join(defaultRoot, "feat-125-broad-root-manager")
	t.Cleanup(func() {
		runGitForTest(t, repo, "worktree", "remove", "--force", managerPath)
		runGitForTest(t, repo, "branch", "-D", branch)
	})

	linkedManager, err := NewWorktreeManager(managerPath, filepath.Dir(repo), ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager(linked manager, broad root) error = %v", err)
	}
	report, err := linkedManager.Clean(context.Background())
	if err != nil {
		t.Fatalf("linked manager Clean() error = %v", err)
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatalf("main checkout was removed: %v", err)
	}
	if _, err := os.Stat(managerPath); err != nil {
		t.Fatalf("manager checkout was removed: %v", err)
	}
	for _, evidence := range report.Commands {
		if len(evidence.Argv) >= 5 && evidence.Argv[3] == "worktree" && evidence.Argv[4] == "remove" {
			t.Fatalf("Clean() attempted destructive worktree removal: %#v", report.Commands)
		}
		if len(evidence.Argv) >= 5 && evidence.Argv[3] == "branch" && (evidence.Argv[4] == "-d" || evidence.Argv[4] == "-D") {
			t.Fatalf("Clean() attempted destructive branch deletion: %#v", report.Commands)
		}
	}
}

func TestLinkedCheckoutRuntimeWorktreesAreIgnoredByDoctorAndClean(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	defaultRoot, err := DefaultWorktreeRoot(repo)
	if err != nil {
		t.Fatalf("DefaultWorktreeRoot() error = %v", err)
	}
	manager, err := NewWorktreeManager(repo, defaultRoot, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	branch := "feat/126-runtime-linked-manager"
	if _, err := manager.Create(context.Background(), branch, "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	managerPath := filepath.Join(defaultRoot, "feat-126-runtime-linked-manager")
	runtimePath := filepath.Join(managerPath, ".rdev", "worktrees", "task-126")
	runtimeBranch := "feat/996-runtime-linked"
	t.Cleanup(func() {
		runGitForTest(t, repo, "worktree", "remove", "--force", runtimePath)
		runGitForTest(t, repo, "worktree", "remove", "--force", managerPath)
		runGitForTest(t, repo, "branch", "-D", branch)
		runGitForTest(t, repo, "branch", "-D", runtimeBranch)
	})
	runGitForTest(t, repo, "worktree", "add", "-b", runtimeBranch, runtimePath, "main")

	linkedManager, err := NewWorktreeManager(managerPath, "", ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager(linked manager) error = %v", err)
	}
	doctorReport, err := linkedManager.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	for _, entry := range doctorReport.Entries {
		if entry.Path == canonicalPathForTest(t, runtimePath) {
			t.Fatalf("Doctor() reported linked runtime worktree %#v", entry)
		}
	}
	cleanReport, err := linkedManager.Clean(context.Background())
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if _, err := os.Stat(runtimePath); err != nil {
		t.Fatalf("linked runtime worktree was removed: %v", err)
	}
	for _, evidence := range cleanReport.Commands {
		if len(evidence.Argv) >= 6 && evidence.Argv[3] == "worktree" && evidence.Argv[4] == "remove" && samePath(evidence.Argv[5], runtimePath) {
			t.Fatalf("Clean() attempted runtime worktree removal: %#v", cleanReport.Commands)
		}
	}
}

func TestBroadRootRuntimeWorktreesAreIgnoredByDoctorAndClean(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	defaultRoot, err := DefaultWorktreeRoot(repo)
	if err != nil {
		t.Fatalf("DefaultWorktreeRoot() error = %v", err)
	}
	manager, err := NewWorktreeManager(repo, defaultRoot, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	branch := "feat/127-runtime-broad-root-manager"
	if _, err := manager.Create(context.Background(), branch, "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	managerPath := filepath.Join(defaultRoot, "feat-127-runtime-broad-root-manager")
	runtimePath := filepath.Join(repo, ".rdev", "worktrees", "task-127")
	runtimeBranch := "feat/997-runtime-broad"
	t.Cleanup(func() {
		runGitForTest(t, repo, "worktree", "remove", "--force", runtimePath)
		runGitForTest(t, repo, "worktree", "remove", "--force", managerPath)
		runGitForTest(t, repo, "branch", "-D", branch)
		runGitForTest(t, repo, "branch", "-D", runtimeBranch)
	})
	runGitForTest(t, repo, "worktree", "add", "-b", runtimeBranch, runtimePath, "main")

	linkedManager, err := NewWorktreeManager(managerPath, filepath.Dir(repo), ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager(linked manager, broad root) error = %v", err)
	}
	doctorReport, err := linkedManager.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	for _, entry := range doctorReport.Entries {
		if entry.Path == canonicalPathForTest(t, runtimePath) {
			t.Fatalf("Doctor() reported broad-root runtime worktree %#v", entry)
		}
	}
	cleanReport, err := linkedManager.Clean(context.Background())
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if _, err := os.Stat(runtimePath); err != nil {
		t.Fatalf("broad-root runtime worktree was removed: %v", err)
	}
	for _, evidence := range cleanReport.Commands {
		if len(evidence.Argv) >= 6 && evidence.Argv[3] == "worktree" && evidence.Argv[4] == "remove" && samePath(evidence.Argv[5], runtimePath) {
			t.Fatalf("Clean() attempted runtime worktree removal: %#v", cleanReport.Commands)
		}
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

func TestDoctorIgnoresRuntimeWorktreesOutsideDeveloperRoot(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	runtimePath := filepath.Join(repo, ".rdev", "worktrees", "task-123")
	runGitForTest(t, repo, "worktree", "add", "-b", "feat/999-runtime-task", runtimePath, "main")

	if _, err := manager.Create(context.Background(), "feat/123-runtime-coexistence", "main"); err != nil {
		t.Fatalf("Create() with runtime worktree error = %v", err)
	}
	developerPath := filepath.Join(root, "feat-123-runtime-coexistence")
	if err := os.WriteFile(filepath.Join(developerPath, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := manager.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	for _, entry := range report.Entries {
		if entry.Path == canonicalPathForTest(t, runtimePath) {
			t.Fatalf("Doctor() included runtime worktree entry %#v", entry)
		}
	}
	if _, err := manager.Clean(context.Background()); err != nil {
		t.Fatalf("Clean() with runtime worktree error = %v", err)
	}
	if _, err := os.Stat(runtimePath); err != nil {
		t.Fatalf("runtime worktree was touched or removed: %v", err)
	}
}

func TestRemoveRejectsOutOfRootRuntimeWorktreeByBranch(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	runtimeBranch := "feat/999-runtime-task"
	runtimePath := filepath.Join(repo, ".rdev", "worktrees", "task-123")
	runGitForTest(t, repo, "worktree", "add", "-b", runtimeBranch, runtimePath, "main")

	_, err = manager.Remove(context.Background(), runtimeBranch, false)
	if err == nil {
		t.Fatal("Remove() expected out-of-root error")
	}
	if !strings.Contains(err.Error(), "outside developer root") {
		t.Fatalf("Remove() error = %v, want outside developer root", err)
	}
}

func TestCreateRejectsSymlinkEscapeOutsideConfiguredRoot(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	rootBase := t.TempDir()
	root := filepath.Join(rootBase, "managed")
	if err := os.MkdirAll(rootBase, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Skipf("symlink creation failed: %v", err)
	}
	if _, err := NewWorktreeManager(repo, root, ExecRunner{}); err == nil {
		t.Fatal("NewWorktreeManager() expected symlink boundary error")
	}
}

func TestRemoveRejectsUnmergedWorktree(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	branch := "feat/123-unmerged-remove"
	if _, err := manager.Create(context.Background(), branch, "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	worktreePath := filepath.Join(root, "feat-123-unmerged-remove")
	if err := os.WriteFile(filepath.Join(worktreePath, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, worktreePath, "add", "change.txt")
	runGitForTest(t, worktreePath, "commit", "-m", "unmerged change")

	if _, err := manager.Remove(context.Background(), branch, true); err == nil {
		t.Fatal("Remove() expected unmerged error")
	}
}

func TestRemoveForceRemovesDirtyMergedWorktree(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	branch := "feat/123-force-remove"
	if _, err := manager.Create(context.Background(), branch, "main"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	worktreePath := filepath.Join(root, "feat-123-force-remove")
	if err := os.WriteFile(filepath.Join(worktreePath, "merged.txt"), []byte("merged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, worktreePath, "add", "merged.txt")
	runGitForTest(t, worktreePath, "commit", "-m", "merged change")
	runGitForTest(t, repo, "merge", "--ff-only", branch)
	if err := os.WriteFile(filepath.Join(worktreePath, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := manager.Remove(context.Background(), branch, true)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !report.OK {
		t.Fatalf("Remove() report = %#v", report)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("forced remove left worktree behind: %v", err)
	}
}

func TestCleanRejectsDuplicateBindingsBeforeDestructiveCommands(t *testing.T) {
	repo := canonicalPathForTest(t, t.TempDir())
	root := filepath.Join(t.TempDir(), ".worktrees", "repo")
	runner := &scriptedRunner{responses: map[string]scriptedResponse{
		keyFor(repo, "rev-parse", "--path-format=absolute", "--git-common-dir"): {
			evidence: CommandEvidence{
				Argv:   []string{"git", "-C", repo, "rev-parse", "--path-format=absolute", "--git-common-dir"},
				Dir:    repo,
				Stdout: filepath.Join(repo, ".git") + "\n",
			},
		},
		keyFor(repo, "worktree", "list", "--porcelain"): {
			evidence: CommandEvidence{
				Argv:   []string{"git", "-C", repo, "worktree", "list", "--porcelain"},
				Dir:    repo,
				Stdout: "worktree /managed/one\nHEAD abc\nbranch refs/heads/feat/123-dup\n\nworktree /managed/two\nHEAD def\nbranch refs/heads/feat/123-dup\n",
			},
		},
	}}
	manager := WorktreeManager{RepoRoot: repo, Root: root, Git: runner}

	_, err := manager.Clean(context.Background())
	if err == nil {
		t.Fatal("Clean() expected duplicate-binding error")
	}
	if !strings.Contains(err.Error(), "multiple worktrees") {
		t.Fatalf("Clean() error = %v", err)
	}
	for _, call := range runner.calls {
		if slices.Equal(call.args, []string{"worktree", "remove", "/managed/one"}) || slices.Equal(call.args, []string{"worktree", "remove", "/managed/two"}) {
			t.Fatalf("Clean() attempted destructive remove: %#v", runner.calls)
		}
	}

	removeRunner := &scriptedRunner{responses: map[string]scriptedResponse{
		keyFor(repo, "rev-parse", "--path-format=absolute", "--git-common-dir"): {
			evidence: CommandEvidence{
				Argv:   []string{"git", "-C", repo, "rev-parse", "--path-format=absolute", "--git-common-dir"},
				Dir:    repo,
				Stdout: filepath.Join(repo, ".git") + "\n",
			},
		},
		keyFor(repo, "worktree", "list", "--porcelain"): {
			evidence: CommandEvidence{
				Argv:   []string{"git", "-C", repo, "worktree", "list", "--porcelain"},
				Dir:    repo,
				Stdout: "worktree /managed/one\nHEAD abc\nbranch refs/heads/feat/123-dup\n\nworktree /managed/two\nHEAD def\nbranch refs/heads/feat/123-dup\n",
			},
		},
	}}
	removeManager := WorktreeManager{RepoRoot: repo, Root: root, Git: removeRunner}
	if _, err := removeManager.Remove(context.Background(), "feat/123-dup", false); err == nil {
		t.Fatal("Remove() expected duplicate-binding error")
	}
	for _, call := range removeRunner.calls {
		if slices.Equal(call.args, []string{"worktree", "remove", "/managed/one"}) || slices.Equal(call.args, []string{"worktree", "remove", "/managed/two"}) {
			t.Fatalf("Remove() attempted destructive remove: %#v", removeRunner.calls)
		}
	}
}

func TestCommonRootDiscoveryFailureFailsClosedBeforeDestructiveCommands(t *testing.T) {
	repo := canonicalPathForTest(t, t.TempDir())
	root := filepath.Join(t.TempDir(), ".worktrees", "repo")
	runner := &scriptedRunner{responses: map[string]scriptedResponse{
		keyFor(repo, "rev-parse", "--path-format=absolute", "--git-common-dir"): {
			evidence: CommandEvidence{
				Argv: []string{"git", "-C", repo, "rev-parse", "--path-format=absolute", "--git-common-dir"},
				Dir:  repo,
			},
			err: fmt.Errorf("common dir unavailable"),
		},
	}}
	manager := WorktreeManager{RepoRoot: repo, Root: root, Git: runner}

	_, err := manager.Clean(context.Background())
	if err == nil {
		t.Fatal("Clean() expected common-root discovery error")
	}
	if !strings.Contains(err.Error(), "common dir unavailable") {
		t.Fatalf("Clean() error = %v", err)
	}
	for _, call := range runner.calls {
		if len(call.args) >= 2 && call.args[0] == "worktree" && call.args[1] == "remove" {
			t.Fatalf("Clean() attempted destructive remove after discovery failure: %#v", runner.calls)
		}
		if len(call.args) >= 2 && call.args[0] == "branch" && (call.args[1] == "-d" || call.args[1] == "-D") {
			t.Fatalf("Clean() attempted branch deletion after discovery failure: %#v", runner.calls)
		}
	}
}

func TestParseWorktreePorcelainPreservesPlatformSafePathsAndFlags(t *testing.T) {
	output := strings.Join([]string{
		"worktree " + filepath.Join(string(filepath.Separator), "tmp", "repo"),
		"HEAD abc123",
		"branch refs/heads/feat/123-example",
		"",
		"worktree " + filepath.Join(string(filepath.Separator), "tmp", "detached"),
		"HEAD def456",
		"detached",
		"",
	}, "\n")

	entries, err := parseWorktreePorcelain(output)
	if err != nil {
		t.Fatalf("parseWorktreePorcelain() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Branch != "feat/123-example" || entries[1].Detached != true {
		t.Fatalf("entries = %#v", entries)
	}
	if runtime.GOOS == "windows" && strings.Contains(entries[0].Path, "/tmp/") {
		t.Fatalf("path was not normalized for platform: %q", entries[0].Path)
	}
}

func TestCreateAndDoctorPreserveCommandEvidence(t *testing.T) {
	requireGit(t)
	repo := initGitRepo(t)
	root := filepath.Join(t.TempDir(), ".worktrees", filepath.Base(repo))
	manager, err := NewWorktreeManager(repo, root, ExecRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}

	createReport, err := manager.Create(context.Background(), "feat/123-evidence", "main")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	doctorReport, err := manager.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}

	for _, report := range []WorktreeReport{createReport, doctorReport} {
		if len(report.Commands) == 0 {
			t.Fatalf("report %#v missing command evidence", report)
		}
		for _, evidence := range report.Commands {
			if len(evidence.Argv) == 0 || evidence.Dir == "" {
				t.Fatalf("invalid command evidence %#v", evidence)
			}
		}
	}
}

type scriptedRunner struct {
	responses map[string]scriptedResponse
	calls     []scriptedCall
}

type scriptedResponse struct {
	evidence CommandEvidence
	err      error
}

type scriptedCall struct {
	dir  string
	args []string
}

func (r *scriptedRunner) Run(_ context.Context, dir string, args ...string) (CommandEvidence, error) {
	r.calls = append(r.calls, scriptedCall{dir: dir, args: append([]string(nil), args...)})
	key := keyFor(dir, args...)
	response, ok := r.responses[key]
	if !ok {
		return CommandEvidence{Argv: append([]string{"git", "-C", dir}, args...), Dir: dir}, fmt.Errorf("unexpected command %s", key)
	}
	if len(response.evidence.Argv) == 0 {
		response.evidence.Argv = append([]string{"git", "-C", dir}, args...)
	}
	if response.evidence.Dir == "" {
		response.evidence.Dir = dir
	}
	return response.evidence, response.err
}

func keyFor(dir string, args ...string) string {
	return dir + "::" + strings.Join(args, "\x00")
}

func runGitForTestOutput(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git -C %s %s failed: %v\n%s", dir, strings.Join(args, " "), err, string(output))
	}
	return nil
}
