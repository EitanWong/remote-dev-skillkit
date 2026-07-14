package gitworkflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultBaseBranch = "main"

type WorktreeEntry struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	Bare     bool   `json:"bare"`
	Clean    bool   `json:"clean"`
	Merged   bool   `json:"merged"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
}

type WorktreeReport struct {
	OK       bool              `json:"ok"`
	RepoRoot string            `json:"repo_root"`
	Root     string            `json:"root"`
	Branch   string            `json:"branch,omitempty"`
	Worktree string            `json:"worktree,omitempty"`
	Entries  []WorktreeEntry   `json:"entries,omitempty"`
	Commands []CommandEvidence `json:"commands"`
	Errors   []string          `json:"errors,omitempty"`
}

type WorktreeManager struct {
	RepoRoot string
	Root     string
	Git      Runner
}

func DefaultWorktreeRoot(repoRoot string) (string, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if err := ensureDirectoryExists(root); err != nil {
		return "", err
	}
	evidence, err := (ExecRunner{}).Run(context.Background(), root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	commonDir := strings.TrimSpace(evidence.Stdout)
	if commonDir == "" {
		return "", fmt.Errorf("git rev-parse --git-common-dir returned empty output")
	}
	commonRoot, err := filepath.Abs(commonDir)
	if err != nil {
		return "", err
	}
	commonRoot = filepath.Clean(commonRoot)
	if filepath.Base(commonRoot) != ".git" {
		return "", fmt.Errorf("git common directory %q must end in .git", commonRoot)
	}
	repositoryRoot := filepath.Dir(commonRoot)
	return filepath.Join(filepath.Dir(repositoryRoot), ".worktrees", filepath.Base(repositoryRoot)), nil
}

func NewWorktreeManager(repoRoot, root string, git Runner) (WorktreeManager, error) {
	if git == nil {
		return WorktreeManager{}, fmt.Errorf("git runner is required")
	}
	canonicalRoot, err := canonicalGitDir(repoRoot)
	if err != nil {
		return WorktreeManager{}, err
	}
	if strings.TrimSpace(root) == "" {
		root, err = DefaultWorktreeRoot(canonicalRoot)
		if err != nil {
			return WorktreeManager{}, err
		}
	} else {
		root, err = filepath.Abs(root)
		if err != nil {
			return WorktreeManager{}, err
		}
		root = filepath.Clean(root)
	}
	if err := rejectSymlinkBoundary(root); err != nil {
		return WorktreeManager{}, err
	}
	if isWithinPath(canonicalRoot, root) {
		return WorktreeManager{}, fmt.Errorf("worktree root %q must be outside repository %q", root, canonicalRoot)
	}
	return WorktreeManager{RepoRoot: canonicalRoot, Root: root, Git: git}, nil
}

func (m WorktreeManager) Create(ctx context.Context, branch, base string) (WorktreeReport, error) {
	report := m.newReport()
	if branch == "main" {
		return m.failReport(report, "branch main is reserved"), fmt.Errorf("branch main is reserved")
	}
	if _, err := ParseBranch(branch); err != nil {
		return m.failReport(report, err.Error()), err
	}
	if strings.TrimSpace(base) == "" {
		base = defaultBaseBranch
	}

	allEntries, _, evidence, err := m.listWorktrees(ctx)
	report.Commands = append(report.Commands, evidence...)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	for _, entry := range allEntries {
		if entry.Branch == branch {
			err := fmt.Errorf("branch %q is already bound to worktree %q", branch, entry.Path)
			return m.failReport(report, err.Error()), err
		}
	}

	target := filepath.Join(m.Root, normalizedBranchDirectory(branch))
	if !isWithinPath(m.Root, target) {
		err := fmt.Errorf("worktree path %q is outside root %q", target, m.Root)
		return m.failReport(report, err.Error()), err
	}
	if err := ensureDirectory(m.Root); err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}

	createEvidence, err := m.Git.Run(ctx, m.RepoRoot, "worktree", "add", "-b", branch, target, base)
	report.Commands = append(report.Commands, createEvidence)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}

	report.OK = true
	report.Branch = branch
	report.Worktree = target
	report.Entries = []WorktreeEntry{{
		Path:   target,
		Branch: branch,
		Clean:  true,
	}}
	return report, nil
}

func (m WorktreeManager) List(ctx context.Context) ([]WorktreeEntry, []CommandEvidence, error) {
	return m.list(ctx)
}

func (m WorktreeManager) Doctor(ctx context.Context) (WorktreeReport, error) {
	report := m.newReport()
	entries, evidence, err := m.list(ctx)
	report.Commands = append(report.Commands, evidence...)
	report.Entries = entries
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	report.OK = true
	return report, nil
}

func (m WorktreeManager) Clean(ctx context.Context) (WorktreeReport, error) {
	report := m.newReport()
	allEntries, entries, evidence, err := m.listWorktrees(ctx)
	report.Commands = append(report.Commands, evidence...)
	report.Entries = entries
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	mainCheckout := mainCheckoutPath(allEntries)

	for _, entry := range entries {
		if entry.Branch == defaultBaseBranch || entry.Bare || entry.Detached || entry.Branch == "" || !isWithinPath(m.Root, entry.Path) {
			continue
		}
		if !entry.Clean || !entry.Merged {
			continue
		}
		if err := m.removeEntry(ctx, &report, entry, false, mainCheckout); err != nil {
			report.Errors = append(report.Errors, err.Error())
			return report, err
		}
	}
	report.OK = true
	return report, nil
}

func (m WorktreeManager) Remove(ctx context.Context, branch string, force bool) (WorktreeReport, error) {
	report := m.newReport()
	if branch == "main" {
		return m.failReport(report, "branch main is reserved"), fmt.Errorf("branch main is reserved")
	}
	if _, err := ParseBranch(branch); err != nil {
		return m.failReport(report, err.Error()), err
	}

	allEntries, entries, evidence, err := m.listWorktrees(ctx)
	report.Commands = append(report.Commands, evidence...)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	mainCheckout := mainCheckoutPath(allEntries)
	for _, entry := range allEntries {
		if entry.Branch == branch && m.isManagerCheckout(entry) {
			err := fmt.Errorf("cannot remove manager checkout %q at %q", branch, entry.Path)
			return m.failReport(report, err.Error()), err
		}
		if entry.Branch == branch && !isWithinPath(m.Root, entry.Path) {
			err := fmt.Errorf("worktree path %q for branch %q is outside developer root %q", entry.Path, branch, m.Root)
			return m.failReport(report, err.Error()), err
		}
	}
	for _, entry := range entries {
		if entry.Branch != branch {
			continue
		}
		if !isWithinPath(m.Root, entry.Path) {
			err := fmt.Errorf("worktree path %q is outside root %q", entry.Path, m.Root)
			return m.failReport(report, err.Error()), err
		}
		if !entry.Clean && !force {
			err := fmt.Errorf("worktree %q is dirty; use force to remove it", branch)
			return m.failReport(report, err.Error()), err
		}
		if !entry.Merged {
			err := fmt.Errorf("worktree %q is not merged into %s", branch, defaultBaseBranch)
			return m.failReport(report, err.Error()), err
		}
		if err := m.removeEntry(ctx, &report, entry, force, mainCheckout); err != nil {
			report.Errors = append(report.Errors, err.Error())
			return report, err
		}
		report.OK = true
		report.Branch = branch
		report.Worktree = entry.Path
		return report, nil
	}
	err = fmt.Errorf("worktree for branch %q was not found", branch)
	return m.failReport(report, err.Error()), err
}

func (m WorktreeManager) list(ctx context.Context) ([]WorktreeEntry, []CommandEvidence, error) {
	_, entries, commands, err := m.listWorktrees(ctx)
	return entries, commands, err
}

func (m WorktreeManager) listWorktrees(ctx context.Context) ([]WorktreeEntry, []WorktreeEntry, []CommandEvidence, error) {
	commonRoot, commonEvidence, err := m.commonRepositoryRoot(ctx)
	commands := []CommandEvidence{}
	if len(commonEvidence.Argv) > 0 {
		commands = append(commands, commonEvidence)
	}
	if err != nil {
		return nil, nil, commands, err
	}
	evidence, err := m.Git.Run(ctx, m.RepoRoot, "worktree", "list", "--porcelain")
	commands = append(commands, evidence)
	if err != nil {
		return nil, nil, commands, err
	}
	allEntries, err := parseWorktreePorcelain(evidence.Stdout)
	if err != nil {
		return nil, nil, commands, err
	}
	if err := rejectDuplicateBranches(allEntries); err != nil {
		return allEntries, nil, commands, err
	}
	runtimeRoots := m.runtimeRoots(commonRoot)
	var entries []WorktreeEntry
	for index := range allEntries {
		if m.isManagerCheckout(allEntries[index]) {
			continue
		}
		if isRuntimeEntry(runtimeRoots, allEntries[index]) {
			continue
		}
		if !isManagedEntry(m.Root, allEntries[index]) {
			continue
		}
		entry := allEntries[index]
		if entry.Bare {
			entry.Clean = true
			entries = append(entries, entry)
			continue
		}
		statusEvidence, statusErr := m.Git.Run(ctx, entry.Path, "status", "--porcelain=v1")
		commands = append(commands, statusEvidence)
		if statusErr != nil {
			return allEntries, entries, commands, statusErr
		}
		entry.Clean = strings.TrimSpace(statusEvidence.Stdout) == ""

		if entry.Detached || entry.Branch == "" {
			entries = append(entries, entry)
			continue
		}
		merged, mergeEvidence, mergeErr := m.isMerged(ctx, entry.Branch)
		commands = append(commands, mergeEvidence...)
		if mergeErr != nil {
			return allEntries, entries, commands, mergeErr
		}
		entry.Merged = merged

		ahead, behind, distanceEvidence, distanceErr := m.branchDistance(ctx, entry.Branch)
		commands = append(commands, distanceEvidence...)
		if distanceErr != nil {
			return allEntries, entries, commands, distanceErr
		}
		entry.Ahead = ahead
		entry.Behind = behind
		entries = append(entries, entry)
	}
	return allEntries, entries, commands, nil
}

func (m WorktreeManager) isManagerCheckout(entry WorktreeEntry) bool {
	return samePath(m.RepoRoot, entry.Path)
}

func (m WorktreeManager) commonRepositoryRoot(ctx context.Context) (string, CommandEvidence, error) {
	evidence, err := m.Git.Run(ctx, m.RepoRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", evidence, err
	}
	commonDir := strings.TrimSpace(evidence.Stdout)
	if commonDir == "" {
		return "", evidence, fmt.Errorf("git rev-parse --git-common-dir returned empty output")
	}
	commonRoot, err := filepath.Abs(commonDir)
	if err != nil {
		return "", evidence, err
	}
	commonRoot = filepath.Clean(commonRoot)
	if filepath.Base(commonRoot) != ".git" {
		return "", evidence, fmt.Errorf("git common directory %q must end in .git", commonRoot)
	}
	return filepath.Dir(commonRoot), evidence, nil
}

func (m WorktreeManager) runtimeRoots(commonRoot string) []string {
	roots := []string{
		filepath.Join(m.RepoRoot, ".rdev", "worktrees"),
	}
	if commonRoot != "" && !samePath(commonRoot, m.RepoRoot) {
		roots = append(roots, filepath.Join(commonRoot, ".rdev", "worktrees"))
	}
	return roots
}

func isManagedEntry(root string, entry WorktreeEntry) bool {
	return entry.Branch != defaultBaseBranch && isWithinPath(root, entry.Path)
}

func isRuntimeEntry(runtimeRoots []string, entry WorktreeEntry) bool {
	for _, root := range runtimeRoots {
		if isWithinPath(root, entry.Path) {
			return true
		}
	}
	return false
}

func rejectDuplicateBranches(entries []WorktreeEntry) error {
	paths := make(map[string]string)
	for _, entry := range entries {
		if entry.Branch == "" {
			continue
		}
		if previous, ok := paths[entry.Branch]; ok {
			return fmt.Errorf("branch %q is bound to multiple worktrees: %q and %q", entry.Branch, previous, entry.Path)
		}
		paths[entry.Branch] = entry.Path
	}
	return nil
}

func mainCheckoutPath(entries []WorktreeEntry) string {
	for _, entry := range entries {
		if entry.Branch == defaultBaseBranch && !entry.Bare {
			return entry.Path
		}
	}
	return ""
}

func (m WorktreeManager) isMerged(ctx context.Context, branch string) (bool, []CommandEvidence, error) {
	evidence, err := m.Git.Run(ctx, m.RepoRoot, "merge-base", "--is-ancestor", branch, defaultBaseBranch)
	if err == nil {
		return true, []CommandEvidence{evidence}, nil
	}
	if evidence.ExitCode == 1 {
		return false, []CommandEvidence{evidence}, nil
	}
	return false, []CommandEvidence{evidence}, err
}

func (m WorktreeManager) branchDistance(ctx context.Context, branch string) (int, int, []CommandEvidence, error) {
	evidence, err := m.Git.Run(ctx, m.RepoRoot, "rev-list", "--left-right", "--count", defaultBaseBranch+"..."+branch)
	if err != nil {
		return 0, 0, []CommandEvidence{evidence}, err
	}
	fields := strings.Fields(evidence.Stdout)
	if len(fields) != 2 {
		return 0, 0, []CommandEvidence{evidence}, fmt.Errorf("git rev-list returned invalid branch distance %q", strings.TrimSpace(evidence.Stdout))
	}
	behind, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, []CommandEvidence{evidence}, err
	}
	ahead, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, []CommandEvidence{evidence}, err
	}
	return ahead, behind, []CommandEvidence{evidence}, nil
}

func (m WorktreeManager) removeEntry(ctx context.Context, report *WorktreeReport, entry WorktreeEntry, force bool, mainCheckout string) error {
	stableCheckout := mainCheckout
	if stableCheckout == "" {
		stableCheckout = m.RepoRoot
	}
	if err := ensureDirectoryExists(stableCheckout); err != nil {
		return fmt.Errorf("stable checkout %q is unavailable for branch deletion: %w", stableCheckout, err)
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, entry.Path)
	evidence, err := m.Git.Run(ctx, m.RepoRoot, args...)
	report.Commands = append(report.Commands, evidence)
	if err != nil {
		return err
	}
	if mainCheckout != "" {
		evidence, err = m.Git.Run(ctx, mainCheckout, "branch", "-d", entry.Branch)
	} else {
		// No main checkout is registered. The earlier merge-base proof established
		// that this branch is merged into main before its worktree was removed,
		// so force deletion is safe from the still-existing manager checkout.
		evidence, err = m.Git.Run(ctx, m.RepoRoot, "branch", "-D", entry.Branch)
	}
	report.Commands = append(report.Commands, evidence)
	return err
}

func (m WorktreeManager) newReport() WorktreeReport {
	return WorktreeReport{RepoRoot: m.RepoRoot, Root: m.Root, Commands: []CommandEvidence{}}
}

func (m WorktreeManager) failReport(report WorktreeReport, message string) WorktreeReport {
	report.OK = false
	report.Errors = append(report.Errors, message)
	return report
}

func parseWorktreePorcelain(output string) ([]WorktreeEntry, error) {
	var entries []WorktreeEntry
	var current *WorktreeEntry
	flush := func() error {
		if current == nil {
			return nil
		}
		if current.Path == "" {
			return fmt.Errorf("git worktree list entry is missing path")
		}
		entries = append(entries, *current)
		current = nil
		return nil
	}

	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		key, value, hasValue := strings.Cut(line, " ")
		switch key {
		case "worktree":
			if err := flush(); err != nil {
				return nil, err
			}
			path, err := filepath.Abs(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			current = &WorktreeEntry{Path: filepath.Clean(path)}
		case "HEAD":
			if current == nil || !hasValue {
				return nil, fmt.Errorf("git worktree list has invalid HEAD record")
			}
			current.Head = strings.TrimSpace(value)
		case "branch":
			if current == nil || !hasValue {
				return nil, fmt.Errorf("git worktree list has invalid branch record")
			}
			current.Branch = strings.TrimPrefix(strings.TrimSpace(value), "refs/heads/")
		case "detached":
			if current == nil {
				return nil, fmt.Errorf("git worktree list has detached record without worktree")
			}
			current.Detached = true
		case "bare":
			if current == nil {
				return nil, fmt.Errorf("git worktree list has bare record without worktree")
			}
			current.Bare = true
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return entries, nil
}

func normalizedBranchDirectory(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

func ensureDirectory(path string) error {
	return os.MkdirAll(path, 0o755)
}

func ensureDirectoryExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path must be a directory")
	}
	return nil
}

func rejectSymlinkBoundary(path string) error {
	current, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("worktree root %q crosses symlink %q", path, current)
			}
			return nil
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func isWithinPath(root, path string) bool {
	rootAbs, rootErr := comparablePath(root)
	pathAbs, pathErr := comparablePath(path)
	if rootErr != nil || pathErr != nil {
		return false
	}
	rootAbs = filepath.Clean(rootAbs)
	pathAbs = filepath.Clean(pathAbs)
	if rootAbs == pathAbs {
		return true
	}
	relative, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func samePath(left, right string) bool {
	leftComparable, leftErr := comparablePath(left)
	rightComparable, rightErr := comparablePath(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftComparable) == filepath.Clean(rightComparable)
}

func comparablePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	remaining := []string{}
	for current := abs; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(remaining) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, remaining[index])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
		remaining = append(remaining, filepath.Base(current))
	}
}
