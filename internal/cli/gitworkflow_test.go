package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
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
	marker := installFakeGHForCLI(t)

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{
		"git", "pr", "plan", "--repo", repo, "--title", "feat: add git cli", "--body", "Closes #123",
	})
	if err != nil {
		t.Fatalf("App.Run() error = %v; stderr=%s", err, stderr.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("gh was executed during plan: stat error = %v", err)
	}
}

func TestRunGitPRCreateRequiresExecuteBeforeGitHub(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	runCLIGit(t, repo, "checkout", "-b", "feat/123-git-cli")
	marker := installFakeGHForCLI(t)

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{"git", "pr", "create", "--repo", repo})
	if err == nil {
		t.Fatal("App.Run() error = nil, want execute gate error")
	}
	if !strings.Contains(err.Error(), "--execute") {
		t.Fatalf("App.Run() error = %v, want execute guidance", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("gh was executed without --execute: stat error = %v", err)
	}
}

func TestRunGitNestedHelpReturnsErrHelpAndDoesNotExecute(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	marker := installFakeGHForCLI(t)

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{"git", "pr", "create", "--repo", repo, "--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("App.Run() error = %v, want flag.ErrHelp", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("gh was executed during help: stat error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage of git pr:") {
		t.Fatalf("stderr = %q, want flag help output", stderr.String())
	}
}

func TestRunGitWorktreeCommandsHonorCustomRoot(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	root := filepath.Join(t.TempDir(), "custom-root")

	createPayload := runGitWorkflowCLIJSON(t, repo, []string{
		"git", "worktree", "create", "--repo", repo, "--root", root, "--branch", "feat/123-custom-root", "--base", "main",
	})
	createResult := nestedMap(t, createPayload, "result")
	if got := createResult["root"]; got != root {
		t.Fatalf("create root = %v, want %q", got, root)
	}
	worktreePath, _ := createResult["worktree"].(string)
	if !strings.HasPrefix(worktreePath, root+string(os.PathSeparator)) {
		t.Fatalf("create worktree = %q, want under %q", worktreePath, root)
	}

	listPayload := runGitWorkflowCLIJSON(t, repo, []string{"git", "worktree", "list", "--repo", repo, "--root", root})
	if got := listPayload["root"]; got != root {
		t.Fatalf("list root = %v, want %q", got, root)
	}
	entries := nestedSlice(t, listPayload, "entries")
	if len(entries) != 1 {
		t.Fatalf("list entries = %d, want 1", len(entries))
	}

	doctorPayload := runGitWorkflowCLIJSON(t, repo, []string{"git", "worktree", "doctor", "--repo", repo, "--root", root})
	doctorResult := nestedMap(t, doctorPayload, "result")
	if got := doctorResult["root"]; got != root {
		t.Fatalf("doctor root = %v, want %q", got, root)
	}

	cleanPayload := runGitWorkflowCLIJSON(t, repo, []string{"git", "worktree", "clean", "--repo", repo, "--root", root})
	cleanResult := nestedMap(t, cleanPayload, "result")
	if got := cleanResult["root"]; got != root {
		t.Fatalf("clean root = %v, want %q", got, root)
	}

	removePayload := runGitWorkflowCLIJSON(t, repo, []string{
		"git", "worktree", "create", "--repo", repo, "--root", root, "--branch", "feat/124-custom-root-remove", "--base", "main",
	})
	removeResult := nestedMap(t, removePayload, "result")
	removePath, _ := removeResult["worktree"].(string)
	runGitWorkflowCLIJSON(t, repo, []string{
		"git", "worktree", "remove", "--repo", repo, "--root", root, "--branch", "feat/124-custom-root-remove",
	})
	if _, err := os.Stat(removePath); !os.IsNotExist(err) {
		t.Fatalf("removed worktree still exists: stat error = %v", err)
	}
}

func TestRunGitPRPlanUsesDeterministicDefaults(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	runCLIGit(t, repo, "checkout", "-b", "feat/123-git-cli")

	payload := runGitWorkflowCLIJSON(t, repo, []string{"git", "pr", "plan", "--repo", repo})
	if got := payload["title"]; got != "feat: update git cli" {
		t.Fatalf("title = %v, want %q", got, "feat: update git cli")
	}
	body, _ := payload["body"].(string)
	if body != "Closes #123" {
		t.Fatalf("body = %q, want %q", body, "Closes #123")
	}
	args := anyStrings(nestedSlice(t, payload, "args"))
	if !slicesContain(args, "--title", "feat: update git cli") {
		t.Fatalf("args missing default title: %#v", args)
	}
	if !slicesContain(args, "--body", "Closes #123") {
		t.Fatalf("args missing default body: %#v", args)
	}
}

func TestRunGitBaseFlagsRejectInvalidRefs(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	runCLIGit(t, repo, "checkout", "-b", "feat/123-git-cli")

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "branch create",
			args: []string{"git", "branch", "create", "--repo", repo, "--type", "feat", "--issue", "124", "--slug", "invalid-base", "--base", "origin/main..bad"},
		},
		{
			name: "worktree create",
			args: []string{"git", "worktree", "create", "--repo", repo, "--branch", "feat/124-invalid-base", "--base", "origin/main..bad"},
		},
		{
			name: "policy check",
			args: []string{"git", "policy", "check", "--repo", repo, "--base", "origin/main..bad"},
		},
		{
			name: "pr plan",
			args: []string{"git", "pr", "plan", "--repo", repo, "--base", "origin/main..bad"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			app := NewApp(&stdout, &stderr)
			err := app.Run(context.Background(), tt.args)
			if err == nil {
				t.Fatal("App.Run() error = nil, want invalid base error")
			}
			if !strings.Contains(err.Error(), "base reference") {
				t.Fatalf("App.Run() error = %v, want base reference error", err)
			}
		})
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

func installFakeGHForCLI(t *testing.T) string {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "gh-called")
	fakeBin := t.TempDir()
	fakeGH := filepath.Join(fakeBin, "gh")
	if err := os.WriteFile(fakeGH, []byte("#!/bin/sh\ntouch \"$RDEV_GH_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RDEV_GH_MARKER", marker)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func runGitWorkflowCLIJSON(t *testing.T, repo string, args []string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), args); err != nil {
		t.Fatalf("App.Run(%v) error = %v; stderr=%s", args, err, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v; stdout=%s", err, stdout.String())
	}
	return payload
}

func nestedMap(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want object", key, payload[key])
	}
	return value
}

func nestedSlice(t *testing.T, payload map[string]any, key string) []any {
	t.Helper()
	value, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want array", key, payload[key])
	}
	return value
}

func slicesContain(args []string, flagValue string, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flagValue && args[index+1] == value {
			return true
		}
	}
	return false
}
