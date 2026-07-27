package hostrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const (
	worktreeCleanupPayloadKey = "worktree_cleanup"
	worktreeCleanupTimeout    = 30 * time.Second
)

type gitWorktreeLease struct {
	prepared workspace.GitWorktreeResult
	storeDir string
	taskID   string
}

type workspaceScopeLease struct {
	before             workspace.WorkspaceScopeSnapshot
	declaredWriteScope []string
}

func wantsGitWorktree(envelope taskEnvelope) (bool, error) {
	isolation := strings.TrimSpace(envelope.Workspace.Isolation)
	if isolation == "" {
		isolation = strings.TrimSpace(stringValue(envelope.Payload, "isolation", ""))
	}
	switch isolation {
	case "":
		switch envelope.Adapter {
		case "codex", "claude-code", "acpx":
			return true, nil
		default:
			return false, nil
		}
	case "workspace-lock":
		return false, nil
	case "git-worktree":
		return true, nil
	default:
		return false, fmt.Errorf("unsupported workspace isolation %q", isolation)
	}
}

func prepareGitWorktreeLease(ctx context.Context, envelope *taskEnvelope, opts Options, now time.Time) (*gitWorktreeLease, error) {
	wantsWorktree, err := wantsGitWorktree(*envelope)
	if err != nil || !wantsWorktree {
		return nil, err
	}
	isGitRepository, err := workspace.IsGitRepository(ctx, envelope.Workspace.Root)
	if err != nil {
		return nil, fmt.Errorf("inspect workspace Git repository: %w", err)
	}
	if !isGitRepository {
		return nil, nil
	}
	storeDir, worktreeRoot := taskWorktreePaths(opts)
	baseRef := firstNonEmptyString(envelope.Workspace.BaseSHA, stringValue(envelope.Payload, "base_sha", ""), "HEAD")
	dirtyPolicy := firstNonEmptyString(envelope.Workspace.DirtyPolicy, stringValue(envelope.Payload, "dirty_policy", ""), workspace.GitWorktreeDirtyPolicyPreserve)
	var prepared workspace.GitWorktreeResult
	resumedWorktree := opts.engineeringResume != nil
	if resumedWorktree {
		checkpoint := opts.engineeringResume.Checkpoint
		if checkpoint.Isolation != "git-worktree" || strings.TrimSpace(checkpoint.WorkspaceRoot) == "" {
			return nil, fmt.Errorf("engineering resume checkpoint does not reference a Git worktree")
		}
		prepared, err = workspace.ResumeGitWorktree(ctx, workspace.GitWorktreeResumeOptions{
			StoreDir:     storeDir,
			RepoRoot:     envelope.Workspace.Root,
			WorktreeRoot: checkpoint.WorkspaceRoot,
			HostID:       firstNonEmptyString(envelope.EndpointID, envelope.EndpointIdentity, "local-host"),
			TaskID:       envelope.TaskID,
			OwnerAdapter: envelope.Adapter,
			BaseSHA:      checkpoint.BaseSHA,
			Branch:       checkpoint.Branch,
			TTL:          opts.WorkspaceLockTTL,
		}, now)
	} else {
		prepared, err = workspace.PrepareGitWorktree(ctx, workspace.GitWorktreeOptions{
			StoreDir:     storeDir,
			RepoRoot:     envelope.Workspace.Root,
			HostID:       firstNonEmptyString(envelope.EndpointID, envelope.EndpointIdentity, "local-host"),
			TaskID:       envelope.TaskID,
			OwnerAdapter: envelope.Adapter,
			BaseRef:      baseRef,
			Branch:       envelope.Workspace.Branch,
			DirtyPolicy:  dirtyPolicy,
			WorktreeRoot: worktreeRoot,
			TTL:          opts.WorkspaceLockTTL,
		}, now)
	}
	if err != nil {
		return nil, err
	}
	taskID := envelope.TaskID
	cleanupOnSetupFailure := workspace.GitWorktreeCleanupRollback
	if resumedWorktree {
		cleanupOnSetupFailure = workspace.GitWorktreeCleanupPreserve
	}
	handoff := false
	defer func() {
		if !handoff {
			finalizeCtx, cancel := worktreeFinalizationContext()
			defer cancel()
			_, _ = workspace.FinalizeGitWorktree(finalizeCtx, workspace.GitWorktreeFinalizeOptions{
				StoreDir: storeDir,
				TaskID:   taskID,
				Worktree: prepared,
				Cleanup:  cleanupOnSetupFailure,
			})
		}
	}()
	originalRoot, err := workspace.CanonicalDir(envelope.Workspace.Root)
	if err != nil {
		return nil, err
	}
	relativeRoot, err := filepath.Rel(prepared.GitTopLevel, originalRoot)
	if err != nil || relativeRoot == ".." || strings.HasPrefix(relativeRoot, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("workspace root %q is outside Git top-level %q", originalRoot, prepared.GitTopLevel)
	}
	isolatedRoot := prepared.WorktreePath
	if relativeRoot != "." {
		isolatedRoot = filepath.Join(prepared.WorktreePath, relativeRoot)
	}
	if info, statErr := os.Stat(isolatedRoot); statErr != nil || !info.IsDir() {
		return nil, fmt.Errorf("isolated workspace root %q is unavailable: %w", isolatedRoot, statErr)
	}
	*envelope = remapEnvelopeForWorktree(*envelope, originalRoot, isolatedRoot, prepared)
	handoff = true
	return &gitWorktreeLease{prepared: prepared, storeDir: storeDir, taskID: envelope.TaskID}, nil
}

func (lease *gitWorktreeLease) finalize(_ context.Context, payload map[string]any) (workspace.GitWorktreeEvidence, error) {
	finalizeCtx, cancel := worktreeFinalizationContext()
	defer cancel()
	return workspace.FinalizeGitWorktree(finalizeCtx, workspace.GitWorktreeFinalizeOptions{
		StoreDir: lease.storeDir,
		TaskID:   lease.taskID,
		Worktree: lease.prepared,
		Cleanup:  workspace.GitWorktreeCleanupPolicy(stringValue(payload, worktreeCleanupPayloadKey, "")),
	})
}

func worktreeFinalizationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), worktreeCleanupTimeout)
}

func prepareWorkspaceScopeLease(envelope taskEnvelope) (*workspaceScopeLease, error) {
	declaredWriteScope := envelope.Workspace.WriteScope
	if len(declaredWriteScope) == 0 {
		declaredWriteScope = stringSliceValue(envelope.Payload, "write_scope")
	}
	before, err := workspace.SnapshotWorkspaceScope(envelope.Workspace.Root, []string{"."})
	if err != nil {
		return nil, err
	}
	return &workspaceScopeLease{
		before:             before,
		declaredWriteScope: append([]string(nil), declaredWriteScope...),
	}, nil
}

func (lease *workspaceScopeLease) finalize() (workspace.WorkspaceScopeEvidence, error) {
	after, err := workspace.SnapshotWorkspaceScope(lease.before.Root, []string{"."})
	if err != nil {
		return workspace.WorkspaceScopeEvidence{}, err
	}
	evidence := workspace.CompareWorkspaceScope(lease.before, after)
	evidence.DeclaredWriteScope = append([]string(nil), lease.declaredWriteScope...)
	return evidence, nil
}

func taskWorktreePaths(opts Options) (storeDir, worktreeRoot string) {
	if strings.TrimSpace(opts.WorkspaceLockStore) != "" {
		return opts.WorkspaceLockStore, filepath.Join(filepath.Dir(opts.WorkspaceLockStore), "worktrees")
	}
	if cacheDir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cacheDir) != "" {
		base := filepath.Join(cacheDir, "rdev")
		return filepath.Join(base, "workspace-locks"), filepath.Join(base, "worktrees")
	}
	base := filepath.Join(os.TempDir(), "rdev")
	return filepath.Join(base, "workspace-locks"), filepath.Join(base, "worktrees")
}

func remapEnvelopeForWorktree(envelope taskEnvelope, oldRoot, newRoot string, prepared workspace.GitWorktreeResult) taskEnvelope {
	envelope.Workspace.Root = newRoot
	envelope.Workspace.Branch = prepared.Branch
	envelope.Workspace.BaseSHA = prepared.BaseSHA
	envelope.Workspace.Isolation = "git-worktree"
	envelope.Workspace.WriteScope = remapWorkspacePaths(envelope.Workspace.WriteScope, oldRoot, newRoot)
	envelope.Payload = cloneMap(envelope.Payload)
	envelope.Payload["workspace_root"] = newRoot
	envelope.Payload["branch"] = prepared.Branch
	envelope.Payload["base_sha"] = prepared.BaseSHA
	envelope.Payload["isolation"] = "git-worktree"
	for _, key := range []string{"read_scope", "write_scope"} {
		if values := stringSliceValue(envelope.Payload, key); values != nil {
			envelope.Payload[key] = remapWorkspacePaths(values, oldRoot, newRoot)
		}
	}
	if path := stringValue(envelope.Payload, "path", ""); path != "" {
		envelope.Payload["path"] = remapWorkspacePath(path, oldRoot, newRoot)
	}
	if argv := stringSliceValue(envelope.Payload, "argv"); argv != nil {
		envelope.Payload["argv"] = remapWorkspacePaths(argv, oldRoot, newRoot)
	}
	if commands := commandMatrixValue(envelope.Payload["verification_commands"]); commands != nil {
		for index := range commands {
			commands[index] = remapWorkspacePaths(commands[index], oldRoot, newRoot)
		}
		envelope.Payload["verification_commands"] = commands
	}
	return envelope
}

func remapWorkspacePaths(values []string, oldRoot, newRoot string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, remapWorkspacePath(value, oldRoot, newRoot))
	}
	return out
}

func remapWorkspacePath(value, oldRoot, newRoot string) string {
	if !filepath.IsAbs(value) {
		return value
	}
	relative, err := filepath.Rel(oldRoot, value)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return value
	}
	return filepath.Join(newRoot, relative)
}

func commandMatrixValue(value any) [][]string {
	switch commands := value.(type) {
	case [][]string:
		out := make([][]string, 0, len(commands))
		for _, command := range commands {
			out = append(out, append([]string(nil), command...))
		}
		return out
	case []any:
		encoded, err := json.Marshal(commands)
		if err != nil {
			return nil
		}
		var out [][]string
		if json.Unmarshal(encoded, &out) != nil {
			return nil
		}
		return out
	default:
		return nil
	}
}

func validateWorktreeWriteScope(evidence workspace.GitWorktreeEvidence, taskWorkspaceRoot string, writeScope []string) error {
	if len(writeScope) == 0 {
		return nil
	}
	if evidence.ChangesTruncated {
		return fmt.Errorf("worktree changed-file evidence is truncated; cannot validate declared write scope")
	}
	violations := make([]string, 0)
	for _, changed := range evidence.ChangedFiles {
		for _, path := range gitChangedPaths(changed) {
			changedPath, err := scopedEvidencePath(evidence.WorktreePath, path.value)
			if err == nil && worktreePathWithinAnyScope(changedPath, taskWorkspaceRoot, writeScope) {
				continue
			}
			violation := path.value
			if path.original {
				violation += " (rename source)"
			}
			violations = append(violations, violation)
			if len(violations) >= 10 {
				break
			}
		}
		if len(violations) >= 10 {
			break
		}
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("worktree changes escape declared write scope: %s", strings.Join(violations, ", "))
}

func worktreePathWithinAnyScope(path, taskWorkspaceRoot string, scopes []string) bool {
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if !filepath.IsAbs(scope) {
			scope = filepath.Join(taskWorkspaceRoot, scope)
		}
		relative, err := filepath.Rel(filepath.Clean(scope), filepath.Clean(path))
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func validateWorkspaceScopeWriteScope(evidence workspace.WorkspaceScopeEvidence, taskWorkspaceRoot string, writeScope []string) error {
	if len(writeScope) == 0 {
		return nil
	}
	if err := workspace.ValidateWriteScopes(taskWorkspaceRoot, writeScope); err != nil {
		return fmt.Errorf("validate final non-Git write scope: %w", err)
	}
	if evidence.ChangesTruncated || evidence.Before.Truncated || evidence.After.Truncated {
		return fmt.Errorf("non-Git workspace change evidence is truncated; cannot validate declared write scope")
	}
	violations := make([]string, 0)
	for _, changed := range evidence.ChangedFiles {
		changedPath, err := scopedEvidencePath(taskWorkspaceRoot, changed)
		if err == nil && worktreePathWithinAnyScope(changedPath, taskWorkspaceRoot, writeScope) {
			continue
		}
		violations = append(violations, changed)
		if len(violations) >= 10 {
			break
		}
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("non-Git workspace changes escape declared write scope: %s", strings.Join(violations, ", "))
}

func validateLiveWriteScope(root string, writeScope []string) error {
	if len(writeScope) == 0 {
		return nil
	}
	return workspace.ValidateWriteScopes(root, writeScope)
}

type changedPath struct {
	value    string
	original bool
}

func gitChangedPaths(changed workspace.GitChangedFile) []changedPath {
	paths := []changedPath{{value: changed.Path}}
	if changed.OriginalPath != "" && isGitRenameStatus(changed.Status) {
		paths = append(paths, changedPath{value: changed.OriginalPath, original: true})
	}
	return paths
}

func isGitRenameStatus(status string) bool {
	return len(status) == 2 && (status[0] == 'R' || status[1] == 'R')
}

func scopedEvidencePath(root, raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("changed-file evidence path is empty")
	}
	relative := filepath.FromSlash(raw)
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("changed-file evidence path %q is absolute", raw)
	}
	relative = filepath.Clean(relative)
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("changed-file evidence path %q escapes workspace root", raw)
	}
	path := filepath.Join(root, relative)
	resolved, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("changed-file evidence path %q escapes workspace root", raw)
	}
	return path, nil
}

func attachGitWorktreeEvidence(content string, evidence workspace.GitWorktreeEvidence) string {
	artifact := make(map[string]any)
	if strings.TrimSpace(content) != "" && json.Unmarshal([]byte(content), &artifact) == nil {
		artifact["workspace_isolation"] = evidence
	} else {
		artifact = map[string]any{
			"schema_version":      "rdev.workspace-isolation-result.v1",
			"workspace_isolation": evidence,
		}
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return content
	}
	return string(encoded)
}

func attachWorkspaceScopeEvidence(content string, evidence workspace.WorkspaceScopeEvidence) string {
	artifact := make(map[string]any)
	if strings.TrimSpace(content) != "" && json.Unmarshal([]byte(content), &artifact) == nil {
		artifact["workspace_scope"] = evidence
	} else {
		artifact = map[string]any{
			"schema_version":  "rdev.workspace-scope-result.v1",
			"workspace_scope": evidence,
		}
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return content
	}
	return string(encoded)
}
