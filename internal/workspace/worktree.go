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
	JobID        string
	OwnerAdapter string
	BaseRef      string
	Branch       string
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
	Lock          Lock              `json:"lock"`
	Commands      []CommandEvidence `json:"commands"`
}

func PrepareGitWorktree(ctx context.Context, opts GitWorktreeOptions, now time.Time) (GitWorktreeResult, error) {
	if strings.TrimSpace(opts.JobID) == "" {
		return GitWorktreeResult{}, fmt.Errorf("job id is required")
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
		branch = "rdev/job_" + safeGitName(opts.JobID)
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
	worktreePath := opts.WorktreePath
	if strings.TrimSpace(worktreePath) == "" {
		worktreeRoot := opts.WorktreeRoot
		if strings.TrimSpace(worktreeRoot) == "" {
			worktreeRoot = filepath.Join(topLevel, ".rdev", "worktrees")
		}
		if !filepath.IsAbs(worktreeRoot) {
			worktreeRoot = filepath.Join(topLevel, worktreeRoot)
		}
		worktreePath = filepath.Join(worktreeRoot, "job_"+safeGitName(opts.JobID))
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
		JobID:        opts.JobID,
		WorktreePath: worktreePath,
		BaseRef:      baseRef,
		Branch:       branch,
		OwnerAdapter: opts.OwnerAdapter,
		TTL:          opts.TTL,
	}, now)
	if err != nil {
		return result, err
	}
	result.Lock = lock
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		_, _, _ = store.Release(topLevel, opts.JobID, false)
		return result, err
	}
	addEvidence, err := runGit(ctx, topLevel, "worktree", "add", "-b", branch, worktreePath, baseRef)
	result.Commands = append(result.Commands, addEvidence)
	if err != nil {
		_, _, _ = store.Release(topLevel, opts.JobID, false)
		return result, fmt.Errorf("create git worktree: %w", err)
	}
	return result, nil
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
		return "job"
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
		return "job"
	}
	return cleaned
}
