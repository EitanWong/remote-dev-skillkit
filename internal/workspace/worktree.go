package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const GitWorktreePlanSchemaVersion = "rdev.git-worktree-plan.v1"

type GitWorktreeOptions struct {
	StoreDir     string
	RepoRoot     string
	HostID       string
	TaskID       string
	OwnerAdapter string
	BaseRef      string
	Branch       string
	DirtyPolicy  string
	WorktreeRoot string
	WorktreePath string
	TTL          time.Duration
}

type CommandEvidence struct {
	Argv     []string `json:"argv"`
	Dir      string   `json:"dir"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	ExitCode int      `json:"exit_code"`
}

type GitWorktreeResult struct {
	SchemaVersion string            `json:"schema_version"`
	RepoRoot      string            `json:"repo_root"`
	GitTopLevel   string            `json:"git_top_level"`
	WorktreePath  string            `json:"worktree_path"`
	Branch        string            `json:"branch"`
	BaseRef       string            `json:"base_ref"`
	BaseSHA       string            `json:"base_sha"`
	Lock          Lock              `json:"lock"`
	Commands      []CommandEvidence `json:"commands"`
}

type GitWorktreeResumeOptions struct {
	StoreDir     string
	RepoRoot     string
	WorktreeRoot string
	HostID       string
	TaskID       string
	OwnerAdapter string
	BaseSHA      string
	Branch       string
	TTL          time.Duration
}

func PrepareGitWorktree(ctx context.Context, opts GitWorktreeOptions, now time.Time) (GitWorktreeResult, error) {
	if strings.TrimSpace(opts.TaskID) == "" {
		return GitWorktreeResult{}, fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(opts.HostID) == "" {
		return GitWorktreeResult{}, fmt.Errorf("host id is required")
	}
	repoRoot, err := CanonicalDir(opts.RepoRoot)
	if err != nil {
		return GitWorktreeResult{}, err
	}
	baseRef := strings.TrimSpace(opts.BaseRef)
	if baseRef == "" {
		baseRef = "HEAD"
	}
	branch := strings.TrimSpace(opts.Branch)
	if branch == "" {
		branch = "rdev/task_" + safeGitName(opts.TaskID)
	}
	dirtyPolicy, err := normalizeGitWorktreeDirtyPolicy(opts.DirtyPolicy)
	if err != nil {
		return GitWorktreeResult{}, err
	}
	topLevelEvidence, err := runGit(ctx, repoRoot, "rev-parse", "--show-toplevel")
	result := GitWorktreeResult{
		SchemaVersion: GitWorktreePlanSchemaVersion,
		RepoRoot:      repoRoot,
		BaseRef:       baseRef,
		Branch:        branch,
		Commands:      []CommandEvidence{topLevelEvidence},
	}
	if err != nil {
		return result, fmt.Errorf("discover git top-level: %w", err)
	}
	topLevel := strings.TrimSpace(topLevelEvidence.Stdout)
	if topLevel == "" {
		return result, fmt.Errorf("git top-level is empty")
	}
	topLevel, err = CanonicalDir(topLevel)
	if err != nil {
		return result, err
	}
	result.GitTopLevel = topLevel
	if dirtyPolicy == GitWorktreeDirtyPolicyRequireClean {
		statusEvidence, statusErr := runGit(ctx, topLevel, "status", "--porcelain=v1", "--untracked-files=normal")
		result.Commands = append(result.Commands, statusEvidence)
		if statusErr != nil {
			return result, fmt.Errorf("inspect original workspace status: %w", statusErr)
		}
		if strings.TrimSpace(statusEvidence.Stdout) != "" {
			return result, fmt.Errorf("original workspace is dirty and dirty_policy requires a clean repository")
		}
	}
	baseEvidence, baseErr := runGit(ctx, topLevel, "rev-parse", "--verify", baseRef+"^{commit}")
	result.Commands = append(result.Commands, baseEvidence)
	if baseErr != nil {
		return result, fmt.Errorf("resolve git base ref %q: %w", baseRef, baseErr)
	}
	result.BaseSHA = strings.TrimSpace(baseEvidence.Stdout)
	if result.BaseSHA == "" {
		return result, fmt.Errorf("resolved git base SHA is empty")
	}
	worktreePath := opts.WorktreePath
	if strings.TrimSpace(worktreePath) == "" {
		worktreeRoot := opts.WorktreeRoot
		if strings.TrimSpace(worktreeRoot) == "" {
			worktreeRoot = filepath.Join(topLevel, ".rdev", "worktrees")
		}
		if !filepath.IsAbs(worktreeRoot) {
			worktreeRoot = filepath.Join(topLevel, worktreeRoot)
		}
		worktreePath = filepath.Join(worktreeRoot, "task_"+safeGitName(opts.TaskID))
	}
	if !filepath.IsAbs(worktreePath) {
		worktreePath = filepath.Join(topLevel, worktreePath)
	}
	worktreePath = filepath.Clean(worktreePath)
	result.WorktreePath = worktreePath

	storeDir := opts.StoreDir
	if strings.TrimSpace(storeDir) == "" {
		storeDir = filepath.Join(topLevel, ".rdev", "workspace-locks")
	}
	store := NewFileLockStore(storeDir)
	lock, err := store.Acquire(LockOptions{
		RepoRoot:     topLevel,
		HostID:       opts.HostID,
		TaskID:       opts.TaskID,
		WorktreePath: worktreePath,
		BaseRef:      result.BaseSHA,
		Branch:       branch,
		OwnerAdapter: opts.OwnerAdapter,
		TTL:          opts.TTL,
	}, now)
	if err != nil {
		return result, err
	}
	result.Lock = lock
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		_, _, _ = store.Release(topLevel, opts.TaskID, false)
		return result, err
	}
	addEvidence, err := runGit(ctx, topLevel, "worktree", "add", "-b", branch, worktreePath, result.BaseSHA)
	result.Commands = append(result.Commands, addEvidence)
	if err != nil {
		_, _, _ = store.Release(topLevel, opts.TaskID, false)
		return result, fmt.Errorf("create git worktree: %w", err)
	}
	return result, nil
}

// ResumeGitWorktree reacquires a task lock around an existing linked worktree
// without recreating its branch or discarding its uncommitted diff. Callers
// must supply the checkpointed worktree root and the expected immutable base.
func ResumeGitWorktree(ctx context.Context, opts GitWorktreeResumeOptions, now time.Time) (GitWorktreeResult, error) {
	if strings.TrimSpace(opts.TaskID) == "" {
		return GitWorktreeResult{}, fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(opts.HostID) == "" {
		return GitWorktreeResult{}, fmt.Errorf("host id is required")
	}
	if strings.TrimSpace(opts.WorktreeRoot) == "" {
		return GitWorktreeResult{}, fmt.Errorf("checkpoint worktree root is required")
	}
	repoRoot, err := CanonicalDir(opts.RepoRoot)
	if err != nil {
		return GitWorktreeResult{}, err
	}
	topLevelEvidence, err := runGit(ctx, repoRoot, "rev-parse", "--show-toplevel")
	result := GitWorktreeResult{
		SchemaVersion: GitWorktreePlanSchemaVersion,
		RepoRoot:      repoRoot,
		Commands:      []CommandEvidence{topLevelEvidence},
	}
	if err != nil {
		return result, fmt.Errorf("discover git top-level: %w", err)
	}
	topLevel, err := CanonicalDir(strings.TrimSpace(topLevelEvidence.Stdout))
	if err != nil {
		return result, err
	}
	result.GitTopLevel = topLevel
	worktreeRoot, err := CanonicalDir(opts.WorktreeRoot)
	if err != nil {
		return result, fmt.Errorf("resolve checkpoint worktree: %w", err)
	}
	worktreeEvidence, err := runGit(ctx, worktreeRoot, "rev-parse", "--show-toplevel")
	result.Commands = append(result.Commands, worktreeEvidence)
	if err != nil {
		return result, fmt.Errorf("inspect checkpoint worktree: %w", err)
	}
	worktreeTopLevel, err := CanonicalDir(strings.TrimSpace(worktreeEvidence.Stdout))
	if err != nil {
		return result, err
	}
	result.WorktreePath = worktreeTopLevel
	listed, err := runGit(ctx, topLevel, "worktree", "list", "--porcelain")
	result.Commands = append(result.Commands, listed)
	if err != nil {
		return result, fmt.Errorf("inspect registered git worktrees: %w", err)
	}
	if !gitWorktreeListed(listed.Stdout, worktreeTopLevel) {
		return result, fmt.Errorf("checkpoint worktree is not registered under the source repository")
	}
	branchEvidence, err := runGit(ctx, worktreeTopLevel, "branch", "--show-current")
	result.Commands = append(result.Commands, branchEvidence)
	if err != nil {
		return result, fmt.Errorf("inspect checkpoint worktree branch: %w", err)
	}
	result.Branch = strings.TrimSpace(branchEvidence.Stdout)
	if expected := strings.TrimSpace(opts.Branch); expected != "" && result.Branch != expected {
		return result, fmt.Errorf("checkpoint worktree branch %q does not match expected branch %q", result.Branch, expected)
	}
	baseRef := strings.TrimSpace(opts.BaseSHA)
	if baseRef == "" {
		return result, fmt.Errorf("checkpoint base SHA is required")
	}
	baseEvidence, err := runGit(ctx, topLevel, "rev-parse", "--verify", baseRef+"^{commit}")
	result.Commands = append(result.Commands, baseEvidence)
	if err != nil {
		return result, fmt.Errorf("resolve checkpoint base SHA: %w", err)
	}
	result.BaseSHA = strings.TrimSpace(baseEvidence.Stdout)
	result.BaseRef = result.BaseSHA
	storeDir := opts.StoreDir
	if strings.TrimSpace(storeDir) == "" {
		storeDir = filepath.Join(topLevel, ".rdev", "workspace-locks")
	}
	store := NewFileLockStore(storeDir)
	lock, err := store.Acquire(LockOptions{
		RepoRoot:     topLevel,
		HostID:       opts.HostID,
		TaskID:       opts.TaskID,
		WorktreePath: worktreeTopLevel,
		BaseRef:      result.BaseSHA,
		Branch:       result.Branch,
		OwnerAdapter: opts.OwnerAdapter,
		TTL:          opts.TTL,
	}, now)
	if err != nil {
		return result, err
	}
	result.Lock = lock
	return result, nil
}

func gitWorktreeListed(raw, target string) bool {
	target = filepath.Clean(target)
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		if filepath.Clean(strings.TrimSpace(strings.TrimPrefix(line, "worktree "))) == target {
			return true
		}
	}
	return false
}

func runGit(ctx context.Context, dir string, args ...string) (CommandEvidence, error) {
	argv := append([]string{"git", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	evidence := CommandEvidence{
		Argv:     argv,
		Dir:      dir,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: commandExitCode(err),
	}
	if err != nil {
		return evidence, fmt.Errorf("git command failed: %s", strings.TrimSpace(stderr.String()))
	}
	return evidence, nil
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func safeGitName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "task"
	}
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	cleaned := strings.Trim(builder.String(), ".-_")
	if cleaned == "" {
		return "task"
	}
	return cleaned
}
