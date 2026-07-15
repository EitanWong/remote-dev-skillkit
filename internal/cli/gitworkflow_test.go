package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gitworkflow"
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

func TestRunGitPRPlanRejectsExecuteFlag(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	runCLIGit(t, repo, "checkout", "-b", "feat/123-git-cli")
	marker := installFakeGHForCLI(t)

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{
		"git", "pr", "plan", "--repo", repo, "--execute", "--title", "feat: add git cli", "--body", "Closes #123",
	})
	if err == nil {
		t.Fatal("App.Run() error = nil, want execute rejection")
	}
	if !strings.Contains(err.Error(), "execute") {
		t.Fatalf("App.Run() error = %v, want execute rejection", err)
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

	listPayload := runGitWorkflowCLIJSON(t, repo, []string{"git", "worktree", "list", "--repo", repo})
	if got := listPayload["root"]; got == root {
		t.Fatalf("list root = %v, want default root", got)
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

func TestRunGitWorktreeListRejectsRootFlag(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"git", "worktree", "list", "--repo", repo, "--root", filepath.Join(t.TempDir(), "root")})
	if err == nil {
		t.Fatal("App.Run() error = nil, want root flag rejection")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("App.Run() error = %v, want root flag rejection", err)
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
	if got := payload["base"]; got != "main" {
		t.Fatalf("base = %v, want main", got)
	}
	args := anyStrings(nestedSlice(t, payload, "args"))
	want := []string{"gh", "pr", "create", "--base", "main", "--head", "feat/123-git-cli", "--title", "feat: update git cli", "--body", "Closes #123"}
	if len(args) != len(want) {
		t.Fatalf("args length = %d, want %d; args=%#v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q; args=%#v", i, args[i], want[i], args)
		}
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

func TestRunGitBranchCreateEmitsJSON(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)

	payload := runGitWorkflowCLIJSON(t, repo, []string{
		"git", "branch", "create", "--repo", repo, "--type", "feat", "--issue", "123", "--slug", "branch-create",
	})
	if got := payload["operation"]; got != "branch.create" {
		t.Fatalf("operation = %v, want branch.create", got)
	}
	branchPayload := nestedMap(t, payload, "branch")
	if got := branchPayload["name"]; got != "feat/123-branch-create" {
		t.Fatalf("branch.name = %v, want feat/123-branch-create", got)
	}
	if got := branchPayload["type"]; got != "feat" {
		t.Fatalf("branch.type = %v, want feat", got)
	}
	commands := nestedSlice(t, payload, "commands")
	if len(commands) != 1 {
		t.Fatalf("commands length = %d, want 1", len(commands))
	}
	if branch := strings.TrimSpace(readGitCommandOutput(t, repo, "branch", "--show-current")); branch != "feat/123-branch-create" {
		t.Fatalf("current branch = %q, want feat/123-branch-create", branch)
	}
}

func TestRunGitSyncEmitsJSON(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	remote := filepath.Join(t.TempDir(), "origin.git")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	runCLIGit(t, remote, "init", "--bare")
	runCLIGit(t, repo, "remote", "add", "origin", remote)

	payload := runGitWorkflowCLIJSON(t, repo, []string{"git", "sync", "--repo", repo, "--prune"})
	if got := payload["operation"]; got != "sync" {
		t.Fatalf("operation = %v, want sync", got)
	}
	if got := payload["prune"]; got != true {
		t.Fatalf("prune = %v, want true", got)
	}
	commands := nestedSlice(t, payload, "commands")
	if len(commands) != 1 {
		t.Fatalf("commands length = %d, want 1", len(commands))
	}
	command := commands[0].(map[string]any)
	argv := anyStrings(command["argv"].([]any))
	if !contains(argv, "--prune") {
		t.Fatalf("argv = %#v, want --prune", argv)
	}
}

func TestRunGitPRCreateExecutesFakeGHAndRedactsEvidence(t *testing.T) {
	repo := initCLIGitWorkflowRepo(t)
	runCLIGit(t, repo, "checkout", "-b", "feat/123-git-cli")
	marker := filepath.Join(t.TempDir(), "gh-called")
	fakeBin := t.TempDir()
	fakeGH := filepath.Join(fakeBin, "gh")
	if err := os.WriteFile(fakeGH, []byte("#!/bin/sh\nprintf 'created with token ghp_secret1234567890\\n'\ntouch \"$RDEV_GH_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RDEV_GH_MARKER", marker)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	payload := runGitWorkflowCLIJSON(t, repo, []string{
		"git", "pr", "create", "--repo", repo, "--execute", "--title", "feat: add git cli", "--body", "Closes #123",
	})
	if got := payload["operation"]; got != "pr.create" {
		t.Fatalf("operation = %v, want pr.create", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("gh was not executed: %v", err)
	}
	commands := nestedSlice(t, payload, "commands")
	if len(commands) != 1 {
		t.Fatalf("commands length = %d, want 1", len(commands))
	}
	command := commands[0].(map[string]any)
	if stdout, _ := command["stdout"].(string); strings.Contains(stdout, "ghp_") || !strings.Contains(stdout, "[REDACTED]") {
		t.Fatalf("stdout = %q, want redacted token", stdout)
	}
}

func TestGitWorkflowDispatchAndHelpers(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	ctx := context.Background()

	if err := app.git(ctx, nil); err == nil || !strings.Contains(err.Error(), "missing git subcommand") {
		t.Fatalf("git(nil) error = %v, want missing subcommand", err)
	}
	if err := app.git(ctx, []string{"unknown"}); err == nil || !strings.Contains(err.Error(), "unknown git subcommand") {
		t.Fatalf("git(unknown) error = %v, want unknown subcommand", err)
	}
	if err := app.gitBranch(ctx, nil); err == nil || !strings.Contains(err.Error(), "usage: rdev git branch create") {
		t.Fatalf("gitBranch(nil) error = %v, want usage", err)
	}
	if err := app.gitWorktree(ctx, nil); err == nil || !strings.Contains(err.Error(), "missing git worktree subcommand") {
		t.Fatalf("gitWorktree(nil) error = %v, want missing subcommand", err)
	}
	if err := app.gitWorktree(ctx, []string{"unknown"}); err == nil || !strings.Contains(err.Error(), "unknown git worktree subcommand") {
		t.Fatalf("gitWorktree(unknown) error = %v, want unknown subcommand", err)
	}
	if err := app.gitPolicy(ctx, nil); err == nil || !strings.Contains(err.Error(), "usage: rdev git policy check") {
		t.Fatalf("gitPolicy(nil) error = %v, want usage", err)
	}
	if err := app.gitPR(ctx, nil); err == nil || !strings.Contains(err.Error(), "missing git pr subcommand") {
		t.Fatalf("gitPR(nil) error = %v, want missing subcommand", err)
	}
	if err := app.gitPR(ctx, []string{"unknown"}); err == nil || !strings.Contains(err.Error(), "unknown git pr subcommand") {
		t.Fatalf("gitPR(unknown) error = %v, want unknown subcommand", err)
	}

	repo := initCLIGitWorkflowRepo(t)
	if _, _, err := discoverGitWorkflowRepo(ctx, ""); err == nil || !strings.Contains(err.Error(), "repository path is required") {
		t.Fatalf("discoverGitWorkflowRepo(empty) error = %v, want path guidance", err)
	}
	if _, _, err := discoverGitWorkflowRepo(ctx, filepath.Join(repo, "missing")); err == nil || !strings.Contains(err.Error(), "discover repository") {
		t.Fatalf("discoverGitWorkflowRepo(missing) error = %v, want discover failure", err)
	}
	if _, err := currentGitBranch(ctx, gitWorkflowRunnerStub{stdout: "\n"}, repo); err == nil || !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("currentGitBranch(detached) error = %v, want detached HEAD", err)
	}
	if got, err := currentGitBranch(ctx, gitWorkflowRunnerStub{stdout: "feat/123-helper\n"}, repo); err != nil || got != "feat/123-helper" {
		t.Fatalf("currentGitBranch() = %q, %v, want feat/123-helper", got, err)
	}
	fs := flag.NewFlagSet("git", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := parseGitWorkflowFlags(fs, []string{"extra"}); err == nil || !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("parseGitWorkflowFlags(extra) error = %v, want unexpected arguments", err)
	}
	if got := defaultPRTitle(gitworkflow.BranchRef{Type: "feat", Slug: "branch-create"}, ""); got != "feat: update branch create" {
		t.Fatalf("defaultPRTitle() = %q, want feat: update branch create", got)
	}
	if got := defaultPRBody(gitworkflow.BranchRef{Issue: 123}, ""); got != "Closes #123" {
		t.Fatalf("defaultPRBody() = %q, want Closes #123", got)
	}
	if err := app.gitWorktreeReport(ctx, []string{"--repo", repo}, "unsupported"); err == nil || !strings.Contains(err.Error(), "unsupported worktree operation") {
		t.Fatalf("gitWorktreeReport(unsupported) error = %v, want unsupported operation", err)
	}
}

func readGitCommandOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s failed: %v\n%s", repo, strings.Join(args, " "), err, output)
	}
	return string(output)
}

type gitWorkflowRunnerStub struct {
	stdout string
	err    error
}

func (s gitWorkflowRunnerStub) Run(context.Context, string, ...string) (gitworkflow.CommandEvidence, error) {
	return gitworkflow.CommandEvidence{Stdout: s.stdout}, s.err
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
