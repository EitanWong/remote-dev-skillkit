package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGitPolicyCheckEmitsJSON(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	runCLIGit(t, repo, "checkout", "-b", "feat/123-git-cli")

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{"git", "policy", "check", "--repo", repo}); err != nil {
		t.Fatalf("App.Run() error = %v; stderr=%s", err, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v; stdout=%s", err, stdout.String())
	}
	if payload["schema"] != "rdev.git-workflow.v1" {
		t.Fatalf("schema = %v, want rdev.git-workflow.v1", payload["schema"])
	}
	if payload["ok"] != true {
		t.Fatalf("ok = %v, want true; payload=%s", payload["ok"], stdout.String())
	}
}

func TestRunGitBranchCreateRequiresIssue(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"git", "branch", "create", "--type", "feat", "--slug", "git-cli", "--repo", repo,
	})
	if err == nil {
		t.Fatal("App.Run() error = nil, want missing issue error")
	}
	if !strings.Contains(err.Error(), "issue") {
		t.Fatalf("App.Run() error = %v, want issue guidance", err)
	}
}

func TestRunGitWorktreeCreateRejectsMain(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"git", "worktree", "create", "--branch", "main", "--repo", repo,
	})
	if err == nil {
		t.Fatal("App.Run() error = nil, want main rejection")
	}
	if !strings.Contains(err.Error(), "branch main is reserved") {
		t.Fatalf("App.Run() error = %v, want main rejection", err)
	}
}

func TestRunGitPRPlanDoesNotExecuteGH(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	runCLIGit(t, repo, "checkout", "-b", "feat/123-git-cli")
	marker := filepath.Join(t.TempDir(), "gh-called")
	fakeBin := t.TempDir()
	fakeGH := filepath.Join(fakeBin, "gh")
	if err := os.WriteFile(fakeGH, []byte("#!/bin/sh\ntouch \"$RDEV_GH_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RDEV_GH_MARKER", marker)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{
		"git", "pr", "plan", "--repo", repo,
		"--title", "feat: add git cli",
		"--body", "Add the git CLI.",
	})
	if err != nil {
		t.Fatalf("App.Run() error = %v; stderr=%s", err, stderr.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("gh was executed during plan: stat error = %v", err)
	}
}

func initCLIGitWorkflowRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runCLIGit(t, repo, "init")
	runCLIGit(t, repo, "config", "user.email", "test@example.com")
	runCLIGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLIGit(t, repo, "add", "README.md")
	runCLIGit(t, repo, "commit", "-m", "chore: initialize test repository")
	runCLIGit(t, repo, "branch", "-M", "main")
	return repo
}

func runCLIGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %s failed: %v\n%s", repo, strings.Join(args, " "), err, output)
	}
}
