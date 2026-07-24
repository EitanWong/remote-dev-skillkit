package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	GitWorktreeEvidenceSchemaVersion = "rdev.git-worktree-evidence.v1"

	GitWorktreeDirtyPolicyPreserve     = "preserve"
	GitWorktreeDirtyPolicyRequireClean = "require-clean"

	GitWorktreeCleanupPreserve GitWorktreeCleanupPolicy = "preserve"
	GitWorktreeCleanupRemove   GitWorktreeCleanupPolicy = "remove"
	GitWorktreeCleanupRollback GitWorktreeCleanupPolicy = "rollback"
)

type GitWorktreeCleanupPolicy string

type GitWorktreeFinalizeOptions struct {
	StoreDir string
	TaskID   string
	Worktree GitWorktreeResult
	Cleanup  GitWorktreeCleanupPolicy
}

type GitChangedFile struct {
	Status       string `json:"status"`
	Path         string `json:"path"`
	OriginalPath string `json:"original_path,omitempty"`
}

type GitWorktreeLockRelease struct {
	Attempted bool   `json:"attempted"`
	Released  bool   `json:"released"`
	Error     string `json:"error,omitempty"`
}

// IsGitRepository detects whether root belongs to a local Git work tree. A
// normal non-Git directory is a supported false result; failures to launch
// Git or inspect an otherwise malformed workspace remain errors.
func IsGitRepository(ctx context.Context, root string) (bool, error) {
	canonicalRoot, err := CanonicalDir(root)
	if err != nil {
		return false, err
	}
	evidence, err := runGit(ctx, canonicalRoot, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if evidence.ExitCode == 128 {
			return false, nil
		}
		return false, fmt.Errorf("git repository check: %w", err)
	}
	return strings.TrimSpace(evidence.Stdout) == "true", nil
}

// GitWorktreeEvidence is a bounded final-state record. It intentionally keeps
// only paths and hashes; no diff body, environment, or command output is
// included in task-visible evidence.
type GitWorktreeEvidence struct {
	SchemaVersion    string                   `json:"schema_version"`
	RepoRoot         string                   `json:"repo_root"`
	GitTopLevel      string                   `json:"git_top_level"`
	WorktreePath     string                   `json:"worktree_path"`
	Branch           string                   `json:"branch"`
	BaseRef          string                   `json:"base_ref"`
	BaseSHA          string                   `json:"base_sha"`
	FinalSHA         string                   `json:"final_sha,omitempty"`
	Dirty            bool                     `json:"dirty"`
	ChangedFiles     []GitChangedFile         `json:"changed_files,omitempty"`
	ChangesTruncated bool                     `json:"changes_truncated"`
	DiffStatSHA256   string                   `json:"diff_stat_sha256,omitempty"`
	DiffSHA256       string                   `json:"diff_sha256,omitempty"`
	LockAcquire      Lock                     `json:"lock_acquire"`
	LockRelease      GitWorktreeLockRelease   `json:"lock_release"`
	Cleanup          GitWorktreeCleanupPolicy `json:"cleanup"`
	CleanupSucceeded bool                     `json:"cleanup_succeeded"`
	Commands         []CommandEvidence        `json:"commands,omitempty"`
}

// FinalizeGitWorktree records the task worktree's terminal state, applies an
// explicit cleanup policy, and releases the lock acquired by
// PrepareGitWorktree. Cleanup and release are attempted even if inspection
// fails so an interrupted task does not strand the repository lock.
func FinalizeGitWorktree(ctx context.Context, opts GitWorktreeFinalizeOptions) (evidence GitWorktreeEvidence, err error) {
	if strings.TrimSpace(opts.TaskID) == "" {
		return GitWorktreeEvidence{}, fmt.Errorf("task id is required")
	}
	worktree := opts.Worktree
	if strings.TrimSpace(worktree.GitTopLevel) == "" || strings.TrimSpace(worktree.WorktreePath) == "" {
		return GitWorktreeEvidence{}, fmt.Errorf("git worktree result is required")
	}
	cleanup, err := normalizeGitWorktreeCleanup(opts.Cleanup)
	if err != nil {
		return GitWorktreeEvidence{}, err
	}
	evidence = GitWorktreeEvidence{
		SchemaVersion: GitWorktreeEvidenceSchemaVersion,
		RepoRoot:      worktree.RepoRoot,
		GitTopLevel:   worktree.GitTopLevel,
		WorktreePath:  worktree.WorktreePath,
		Branch:        worktree.Branch,
		BaseRef:       worktree.BaseRef,
		BaseSHA:       firstNonEmpty(worktree.BaseSHA, worktree.BaseRef),
		LockAcquire:   worktree.Lock,
		Cleanup:       cleanup,
	}

	defer func() {
		store := NewFileLockStore(opts.StoreDir)
		evidence.LockRelease.Attempted = true
		_, released, releaseErr := store.Release(worktree.GitTopLevel, opts.TaskID, false)
		evidence.LockRelease.Released = released
		if releaseErr != nil {
			evidence.LockRelease.Error = releaseErr.Error()
			if err == nil {
				err = fmt.Errorf("release git worktree lock: %w", releaseErr)
			}
		}
	}()

	if err = inspectGitWorktree(ctx, &evidence); err != nil {
		return evidence, err
	}
	if err = cleanupGitWorktree(ctx, &evidence); err != nil {
		return evidence, err
	}
	return evidence, nil
}

func normalizeGitWorktreeCleanup(value GitWorktreeCleanupPolicy) (GitWorktreeCleanupPolicy, error) {
	if strings.TrimSpace(string(value)) == "" {
		return GitWorktreeCleanupPreserve, nil
	}
	switch value {
	case GitWorktreeCleanupPreserve, GitWorktreeCleanupRemove, GitWorktreeCleanupRollback:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported git worktree cleanup policy %q", value)
	}
}

func normalizeGitWorktreeDirtyPolicy(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return GitWorktreeDirtyPolicyPreserve, nil
	}
	switch value {
	case GitWorktreeDirtyPolicyPreserve, GitWorktreeDirtyPolicyRequireClean:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported git worktree dirty policy %q", value)
	}
}

func inspectGitWorktree(ctx context.Context, evidence *GitWorktreeEvidence) error {
	finalSHA, err := runGit(ctx, evidence.WorktreePath, "rev-parse", "HEAD")
	evidence.Commands = append(evidence.Commands, boundedCommandEvidence(finalSHA))
	if err != nil {
		return fmt.Errorf("read worktree HEAD: %w", err)
	}
	evidence.FinalSHA = strings.TrimSpace(finalSHA.Stdout)

	status, err := runGit(ctx, evidence.WorktreePath, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	evidence.Commands = append(evidence.Commands, boundedCommandEvidence(status))
	if err != nil {
		return fmt.Errorf("inspect worktree changes: %w", err)
	}
	evidence.ChangedFiles, evidence.ChangesTruncated = parseGitStatusChanges(status.Stdout)
	evidence.Dirty = len(evidence.ChangedFiles) > 0

	diffStat, err := runGit(ctx, evidence.WorktreePath, "diff", "--stat", "HEAD")
	evidence.Commands = append(evidence.Commands, boundedCommandEvidence(diffStat))
	if err != nil {
		return fmt.Errorf("calculate worktree diff stat: %w", err)
	}
	evidence.DiffStatSHA256 = hashGitOutput(diffStat.Stdout)

	diff, err := runGit(ctx, evidence.WorktreePath, "diff", "--binary", "HEAD")
	evidence.Commands = append(evidence.Commands, boundedCommandEvidence(diff))
	if err != nil {
		return fmt.Errorf("calculate worktree diff: %w", err)
	}
	evidence.DiffSHA256 = hashGitOutput(diff.Stdout)
	return nil
}

func cleanupGitWorktree(ctx context.Context, evidence *GitWorktreeEvidence) error {
	switch evidence.Cleanup {
	case GitWorktreeCleanupPreserve:
		evidence.CleanupSucceeded = true
		return nil
	case GitWorktreeCleanupRollback:
		reset, err := runGit(ctx, evidence.WorktreePath, "reset", "--hard", evidence.BaseSHA)
		evidence.Commands = append(evidence.Commands, boundedCommandEvidence(reset))
		if err != nil {
			return fmt.Errorf("rollback worktree: %w", err)
		}
		fallthrough
	case GitWorktreeCleanupRemove:
		remove, err := runGit(ctx, evidence.GitTopLevel, "worktree", "remove", "--force", evidence.WorktreePath)
		evidence.Commands = append(evidence.Commands, boundedCommandEvidence(remove))
		if err != nil {
			return fmt.Errorf("remove worktree: %w", err)
		}
		prune, err := runGit(ctx, evidence.GitTopLevel, "worktree", "prune")
		evidence.Commands = append(evidence.Commands, boundedCommandEvidence(prune))
		if err != nil {
			return fmt.Errorf("prune worktrees: %w", err)
		}
		evidence.CleanupSucceeded = true
		return nil
	default:
		return fmt.Errorf("unsupported git worktree cleanup policy %q", evidence.Cleanup)
	}
}

func parseGitStatusChanges(raw string) ([]GitChangedFile, bool) {
	const maxChangedFiles = 200
	entries := strings.Split(raw, "\x00")
	files := make([]GitChangedFile, 0, minInt(len(entries), maxChangedFiles))
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if entry == "" {
			continue
		}
		if len(files) >= maxChangedFiles {
			return files, true
		}
		if len(entry) < 4 {
			continue
		}
		status := entry[:2]
		file := GitChangedFile{Status: status, Path: entry[3:]}
		if isGitRenameOrCopyStatus(status) {
			if index+1 >= len(entries) || entries[index+1] == "" {
				return append(files, file), true
			}
			file.OriginalPath = entries[index+1]
			index++
		}
		files = append(files, file)
	}
	return files, false
}

func isGitRenameOrCopyStatus(status string) bool {
	return len(status) == 2 && (status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C')
}

func boundedCommandEvidence(command CommandEvidence) CommandEvidence {
	return CommandEvidence{
		Argv:     append([]string(nil), command.Argv...),
		Dir:      command.Dir,
		ExitCode: command.ExitCode,
	}
}

func hashGitOutput(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
