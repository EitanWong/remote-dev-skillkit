package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/gitworkflow"
)

const defaultGitWorkflowBase = "origin/main"

func (a App) git(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing git subcommand")
	}
	switch args[0] {
	case "branch":
		return a.gitBranch(ctx, args[1:])
	case "worktree":
		return a.gitWorktree(ctx, args[1:])
	case "policy":
		return a.gitPolicy(ctx, args[1:])
	case "sync":
		return a.gitSync(ctx, args[1:])
	case "pr":
		return a.gitPR(ctx, args[1:])
	default:
		return fmt.Errorf("unknown git subcommand %q", args[0])
	}
}

func (a App) gitBranch(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("usage: rdev git branch create --type TYPE --issue N --slug SLUG [--base REF] [--repo PATH]")
	}
	fs := flag.NewFlagSet("git branch create", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	branchType := fs.String("type", "", "branch workflow type")
	issue := fs.Int64("issue", 0, "issue number")
	slug := fs.String("slug", "", "lowercase branch slug")
	base := fs.String("base", defaultGitWorkflowBase, "base ref")
	repoPath := fs.String("repo", ".", "repository path")
	if err := parseGitWorkflowFlags(fs, args[1:]); err != nil {
		return err
	}
	if *issue <= 0 {
		return fmt.Errorf("issue is required and must be a positive integer")
	}
	if err := gitworkflow.ValidateBaseRef(*base); err != nil {
		return err
	}
	branchName := fmt.Sprintf("%s/%d-%s", *branchType, *issue, *slug)
	branch, err := gitworkflow.ParseBranch(branchName)
	if err != nil {
		return err
	}
	repo, runner, err := discoverGitWorkflowRepo(ctx, *repoPath)
	if err != nil {
		return err
	}
	evidence, err := runner.Run(ctx, repo.Root, "switch", "-c", branch.Name, *base)
	if err != nil {
		return err
	}
	return a.emitGitWorkflow(map[string]any{
		"schema":    gitworkflow.SchemaVersion,
		"ok":        true,
		"operation": "branch.create",
		"repo_root": repo.Root,
		"branch":    branch,
		"base":      *base,
		"commands":  []gitworkflow.CommandEvidence{evidence},
	})
}

func (a App) gitWorktree(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing git worktree subcommand")
	}
	switch args[0] {
	case "create":
		return a.gitWorktreeCreate(ctx, args[1:])
	case "list":
		return a.gitWorktreeList(ctx, args[1:])
	case "doctor":
		return a.gitWorktreeReport(ctx, args[1:], "doctor")
	case "clean":
		return a.gitWorktreeReport(ctx, args[1:], "clean")
	case "remove":
		return a.gitWorktreeRemove(ctx, args[1:])
	default:
		return fmt.Errorf("unknown git worktree subcommand %q", args[0])
	}
}

func (a App) gitWorktreeCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("git worktree create", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	branch := fs.String("branch", "", "branch name")
	base := fs.String("base", defaultGitWorkflowBase, "base ref")
	repoPath := fs.String("repo", ".", "repository path")
	root := fs.String("root", "", "developer worktree root")
	if err := parseGitWorkflowFlags(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*branch) == "" {
		return fmt.Errorf("branch is required")
	}
	if err := gitworkflow.ValidateBaseRef(*base); err != nil {
		return err
	}
	repo, runner, err := discoverGitWorkflowRepo(ctx, *repoPath)
	if err != nil {
		return err
	}
	manager, err := gitworkflow.NewWorktreeManager(repo.Root, *root, runner)
	if err != nil {
		return err
	}
	report, err := manager.Create(ctx, *branch, *base)
	if err != nil {
		return err
	}
	return a.emitGitWorkflow(map[string]any{
		"schema":    gitworkflow.SchemaVersion,
		"ok":        true,
		"operation": "worktree.create",
		"result":    report,
	})
}

func (a App) gitWorktreeList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("git worktree list", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	repoPath := fs.String("repo", ".", "repository path")
	root := fs.String("root", "", "developer worktree root")
	if err := parseGitWorkflowFlags(fs, args); err != nil {
		return err
	}
	repo, runner, err := discoverGitWorkflowRepo(ctx, *repoPath)
	if err != nil {
		return err
	}
	manager, err := gitworkflow.NewWorktreeManager(repo.Root, *root, runner)
	if err != nil {
		return err
	}
	entries, commands, err := manager.List(ctx)
	if err != nil {
		return err
	}
	return a.emitGitWorkflow(map[string]any{
		"schema":    gitworkflow.SchemaVersion,
		"ok":        true,
		"operation": "worktree.list",
		"repo_root": repo.Root,
		"root":      manager.Root,
		"entries":   entries,
		"commands":  commands,
	})
}

func (a App) gitWorktreeReport(ctx context.Context, args []string, operation string) error {
	fs := flag.NewFlagSet("git worktree "+operation, flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	repoPath := fs.String("repo", ".", "repository path")
	root := fs.String("root", "", "developer worktree root")
	if err := parseGitWorkflowFlags(fs, args); err != nil {
		return err
	}
	repo, runner, err := discoverGitWorkflowRepo(ctx, *repoPath)
	if err != nil {
		return err
	}
	manager, err := gitworkflow.NewWorktreeManager(repo.Root, *root, runner)
	if err != nil {
		return err
	}
	var report gitworkflow.WorktreeReport
	switch operation {
	case "doctor":
		report, err = manager.Doctor(ctx)
	case "clean":
		report, err = manager.Clean(ctx)
	default:
		return fmt.Errorf("unsupported worktree operation %q", operation)
	}
	if err != nil {
		return err
	}
	return a.emitGitWorkflow(map[string]any{
		"schema":    gitworkflow.SchemaVersion,
		"ok":        true,
		"operation": "worktree." + operation,
		"result":    report,
	})
}

func (a App) gitWorktreeRemove(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("git worktree remove", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	branch := fs.String("branch", "", "branch name")
	repoPath := fs.String("repo", ".", "repository path")
	root := fs.String("root", "", "developer worktree root")
	force := fs.Bool("force", false, "remove a dirty or unmerged worktree")
	if err := parseGitWorkflowFlags(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*branch) == "" {
		return fmt.Errorf("branch is required")
	}
	repo, runner, err := discoverGitWorkflowRepo(ctx, *repoPath)
	if err != nil {
		return err
	}
	manager, err := gitworkflow.NewWorktreeManager(repo.Root, *root, runner)
	if err != nil {
		return err
	}
	report, err := manager.Remove(ctx, *branch, *force)
	if err != nil {
		return err
	}
	return a.emitGitWorkflow(map[string]any{
		"schema":    gitworkflow.SchemaVersion,
		"ok":        true,
		"operation": "worktree.remove",
		"result":    report,
	})
}

func (a App) gitPolicy(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return fmt.Errorf("usage: rdev git policy check [--repo PATH] [--base REF]")
	}
	fs := flag.NewFlagSet("git policy check", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	repoPath := fs.String("repo", ".", "repository path")
	base := fs.String("base", defaultGitWorkflowBase, "base ref")
	if err := parseGitWorkflowFlags(fs, args[1:]); err != nil {
		return err
	}
	repo, runner, err := discoverGitWorkflowRepo(ctx, *repoPath)
	if err != nil {
		return err
	}
	if err := gitworkflow.ValidateBaseRef(*base); err != nil {
		return err
	}
	report, err := gitworkflow.CheckPolicy(ctx, repo, runner, *base)
	if err != nil {
		return err
	}
	return a.emitGitWorkflow(report)
}

func (a App) gitSync(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("git sync", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	repoPath := fs.String("repo", ".", "repository path")
	prune := fs.Bool("prune", false, "prune deleted remote-tracking branches")
	if err := parseGitWorkflowFlags(fs, args); err != nil {
		return err
	}
	repo, runner, err := discoverGitWorkflowRepo(ctx, *repoPath)
	if err != nil {
		return err
	}
	gitArgs := []string{"fetch"}
	if *prune {
		gitArgs = append(gitArgs, "--prune")
	}
	evidence, err := runner.Run(ctx, repo.Root, gitArgs...)
	if err != nil {
		return err
	}
	return a.emitGitWorkflow(map[string]any{
		"schema":    gitworkflow.SchemaVersion,
		"ok":        true,
		"operation": "sync",
		"repo_root": repo.Root,
		"prune":     *prune,
		"commands":  []gitworkflow.CommandEvidence{evidence},
	})
}

func (a App) gitPR(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing git pr subcommand")
	}
	switch args[0] {
	case "plan":
		return a.gitPRPlan(ctx, args[1:], false)
	case "create":
		return a.gitPRPlan(ctx, args[1:], true)
	default:
		return fmt.Errorf("unknown git pr subcommand %q", args[0])
	}
}

func (a App) gitPRPlan(ctx context.Context, args []string, execute bool) error {
	fs := flag.NewFlagSet("git pr", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	executeFlag := fs.Bool("execute", false, "execute gh pr create")
	repoPath := fs.String("repo", ".", "repository path")
	title := fs.String("title", "", "pull request title")
	body := fs.String("body", "", "pull request body")
	base := fs.String("base", defaultGitWorkflowBase, "base ref")
	if err := parseGitWorkflowFlags(fs, args); err != nil {
		return err
	}
	if execute && !*executeFlag {
		return fmt.Errorf("git pr create requires --execute")
	}
	if err := gitworkflow.ValidateBaseRef(*base); err != nil {
		return err
	}
	repo, runner, err := discoverGitWorkflowRepo(ctx, *repoPath)
	if err != nil {
		return err
	}
	branchName, err := currentGitBranch(ctx, runner, repo.Root)
	if err != nil {
		return err
	}
	branch, err := gitworkflow.ParseBranch(branchName)
	if err != nil {
		return err
	}
	plan, err := gitworkflow.PlanPR(repo, branch, defaultPRTitle(branch, *title), defaultPRBody(branch, *body))
	if err != nil {
		return err
	}
	plan.Base = *base
	plan.Args = []string{"gh", "pr", "create", "--base", plan.Base, "--head", plan.Head, "--title", plan.Title, "--body", plan.Body}
	if !execute {
		return a.emitGitWorkflow(plan)
	}
	evidence, err := gitworkflow.ExecutePR(ctx, gitworkflow.GitHubRunner{}, repo.Root, plan)
	if err != nil {
		return err
	}
	return a.emitGitWorkflow(map[string]any{
		"schema":    gitworkflow.SchemaVersion,
		"ok":        true,
		"operation": "pr.create",
		"plan":      plan,
		"commands":  []gitworkflow.CommandEvidence{evidence},
	})
}

func discoverGitWorkflowRepo(ctx context.Context, path string) (gitworkflow.Repo, gitworkflow.ExecRunner, error) {
	if strings.TrimSpace(path) == "" {
		return gitworkflow.Repo{}, gitworkflow.ExecRunner{}, fmt.Errorf("repository path is required")
	}
	runner := gitworkflow.ExecRunner{}
	repo, err := gitworkflow.DiscoverRepo(ctx, runner, path)
	if err != nil {
		return gitworkflow.Repo{}, runner, fmt.Errorf("discover repository %q: %w", path, err)
	}
	return repo, runner, nil
}

func currentGitBranch(ctx context.Context, runner gitworkflow.Runner, repoRoot string) (string, error) {
	evidence, err := runner.Run(ctx, repoRoot, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(evidence.Stdout)
	if branch == "" {
		return "", fmt.Errorf("repository is in detached HEAD state")
	}
	return branch, nil
}

func parseGitWorkflowFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flag.ErrHelp
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return nil
}

func defaultPRTitle(branch gitworkflow.BranchRef, title string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	return fmt.Sprintf("%s: update %s", branch.Type, strings.ReplaceAll(branch.Slug, "-", " "))
}

func defaultPRBody(branch gitworkflow.BranchRef, body string) string {
	if strings.TrimSpace(body) != "" {
		return body
	}
	return fmt.Sprintf("Closes #%d", branch.Issue)
}

func (a App) emitGitWorkflow(value any) error {
	encoder := json.NewEncoder(a.Stdout)
	return encoder.Encode(value)
}
