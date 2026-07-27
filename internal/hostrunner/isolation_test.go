package hostrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

func TestCodingAdaptersDefaultToGitWorktreeIsolation(t *testing.T) {
	for _, adapter := range []string{"codex", "claude-code", "acpx"} {
		isolated, err := wantsGitWorktree(taskEnvelope{Adapter: adapter})
		if err != nil || !isolated {
			t.Fatalf("adapter %q must default to Git worktree isolation, isolated=%v err=%v", adapter, isolated, err)
		}
	}
	if isolated, err := wantsGitWorktree(taskEnvelope{Adapter: "shell"}); err != nil || isolated {
		t.Fatalf("shell must keep its legacy workspace-lock default, isolated=%v err=%v", isolated, err)
	}
	if isolated, err := wantsGitWorktree(taskEnvelope{Adapter: "codex", Workspace: model.TaskWorkspace{Isolation: "workspace-lock"}}); err != nil || isolated {
		t.Fatalf("explicit workspace-lock must opt out of default worktree isolation, isolated=%v err=%v", isolated, err)
	}
	if _, err := wantsGitWorktree(taskEnvelope{Adapter: "shell", Workspace: model.TaskWorkspace{Isolation: "unsupported"}}); err == nil {
		t.Fatal("unsupported isolation must be rejected")
	}
}

func TestWorktreeEnvelopeRemappingPreservesExternalPathsAndPayloadShapes(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	externalRoot := t.TempDir()
	internalPath := filepath.Join(oldRoot, "internal")
	envelope := taskEnvelope{
		Workspace: model.TaskWorkspace{
			Root:       oldRoot,
			WriteScope: []string{internalPath, "relative-scope"},
		},
		Payload: map[string]any{
			"read_scope":  []any{internalPath, "relative-read"},
			"write_scope": []any{internalPath, "relative-write"},
			"path":        filepath.Join(oldRoot, "internal", "file.go"),
			"argv": []string{
				"go", "test", filepath.Join(oldRoot, "cmd"), filepath.Join(externalRoot, "outside"),
			},
			"verification_commands": []any{[]any{"go", "test", filepath.Join(oldRoot, "verify")}},
		},
	}
	prepared := workspace.GitWorktreeResult{
		WorktreePath: newRoot,
		Branch:       "rdev/task-remap",
		BaseSHA:      "0123456789abcdef0123456789abcdef01234567",
	}
	remapped := remapEnvelopeForWorktree(envelope, oldRoot, newRoot, prepared)
	if remapped.Workspace.Root != newRoot || remapped.Workspace.Branch != prepared.Branch || remapped.Workspace.BaseSHA != prepared.BaseSHA || remapped.Workspace.Isolation != "git-worktree" {
		t.Fatalf("worktree workspace was not remapped: %#v", remapped.Workspace)
	}
	if got := remapped.Workspace.WriteScope; len(got) != 2 || got[0] != filepath.Join(newRoot, "internal") || got[1] != "relative-scope" {
		t.Fatalf("write scope was not remapped: %#v", got)
	}
	if got := remapped.Payload["path"]; got != filepath.Join(newRoot, "internal", "file.go") {
		t.Fatalf("file payload path was not remapped: %#v", got)
	}
	argv, ok := remapped.Payload["argv"].([]string)
	if !ok || len(argv) != 4 || argv[2] != filepath.Join(newRoot, "cmd") || argv[3] != filepath.Join(externalRoot, "outside") {
		t.Fatalf("argv remapping changed the wrong paths: %#v", remapped.Payload["argv"])
	}
	commands := commandMatrixValue(remapped.Payload["verification_commands"])
	if len(commands) != 1 || len(commands[0]) != 3 || commands[0][2] != filepath.Join(newRoot, "verify") {
		t.Fatalf("verification command paths were not remapped: %#v", commands)
	}
	if commandMatrixValue([]any{"not-a-command"}) != nil || commandMatrixValue("not-a-matrix") != nil {
		t.Fatal("invalid command matrices must be rejected")
	}
}

func TestWorkspaceScopeLeaseAndEvidenceHelpersCoverFallbackBoundaries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "allowed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "allowed", "file.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lease, err := prepareWorkspaceScopeLease(taskEnvelope{
		Workspace: model.TaskWorkspace{Root: root, WriteScope: []string{"allowed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "allowed", "file.txt"), []byte("after with a different length\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence, err := lease.finalize()
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Changed || len(evidence.DeclaredWriteScope) != 1 || evidence.DeclaredWriteScope[0] != "allowed" || !containsWorkspaceScopeChange(evidence.ChangedFiles, "allowed/file.txt") {
		t.Fatalf("scope lease did not produce bounded change evidence: %#v", evidence)
	}
	if _, err := prepareWorkspaceScopeLease(taskEnvelope{Workspace: model.TaskWorkspace{Root: filepath.Join(root, "missing")}}); err == nil {
		t.Fatal("missing workspace root must reject a scope snapshot lease")
	}
	if err := validateWorkspaceScopeWriteScope(evidence, root, []string{"allowed"}); err != nil {
		t.Fatalf("declared scope should accept its own change: %v", err)
	}
	if err := validateWorkspaceScopeWriteScope(evidence, root, []string{"other"}); err == nil {
		t.Fatal("out-of-scope fallback change must be rejected")
	}
}

func TestIsolationEvidenceAttachmentsAndScopeValidation(t *testing.T) {
	root := t.TempDir()
	gitEvidence := workspace.GitWorktreeEvidence{
		WorktreePath: root,
		ChangedFiles: []workspace.GitChangedFile{{Path: "allowed/file.go"}},
	}
	if err := validateWorktreeWriteScope(gitEvidence, root, []string{"allowed"}); err != nil {
		t.Fatalf("declared Git write scope should accept its change: %v", err)
	}
	gitEvidence.ChangedFiles = append(gitEvidence.ChangedFiles, workspace.GitChangedFile{Path: "outside.go"})
	if err := validateWorktreeWriteScope(gitEvidence, root, []string{"allowed"}); err == nil {
		t.Fatal("Git worktree change outside write scope must be rejected")
	}
	gitEvidence.ChangedFiles = []workspace.GitChangedFile{{Status: "R ", Path: "allowed/moved.go", OriginalPath: "outside.go"}}
	if err := validateWorktreeWriteScope(gitEvidence, root, []string{"allowed"}); err == nil {
		t.Fatal("Git rename source outside write scope must be rejected")
	}
	if err := validateWorktreeWriteScope(gitEvidence, root, nil); err != nil {
		t.Fatalf("empty legacy write scope must retain compatibility: %v", err)
	}

	attached := attachGitWorktreeEvidence(`{"adapter":"shell"}`, gitEvidence)
	var gitArtifact map[string]any
	if err := json.Unmarshal([]byte(attached), &gitArtifact); err != nil || gitArtifact["workspace_isolation"] == nil {
		t.Fatalf("Git evidence attachment failed: content=%s err=%v", attached, err)
	}
	fallback := attachWorkspaceScopeEvidence("not-json", workspace.WorkspaceScopeEvidence{SchemaVersion: workspace.WorkspaceScopeEvidenceSchemaVersion})
	if !strings.Contains(fallback, "workspace_scope") || !strings.Contains(fallback, "rdev.workspace-scope-result.v1") {
		t.Fatalf("fallback scope evidence attachment failed: %s", fallback)
	}

	store, worktreeRoot := taskWorktreePaths(Options{WorkspaceLockStore: filepath.Join(root, "locks")})
	if store != filepath.Join(root, "locks") || worktreeRoot != filepath.Join(root, "worktrees") {
		t.Fatalf("configured task worktree paths were not deterministic: store=%q root=%q", store, worktreeRoot)
	}
	defaultStore, defaultWorktreeRoot := taskWorktreePaths(Options{})
	if defaultStore == "" || defaultWorktreeRoot == "" || filepath.Base(defaultStore) != "workspace-locks" || filepath.Base(defaultWorktreeRoot) != "worktrees" {
		t.Fatalf("default task worktree paths are incomplete: store=%q root=%q", defaultStore, defaultWorktreeRoot)
	}
}

func TestScopedEvidencePathRejectsEscapesAndPreservesRelativePaths(t *testing.T) {
	root := t.TempDir()
	for _, raw := range []string{"", ".", "..", "../outside", filepath.Join(root, "absolute")} {
		if _, err := scopedEvidencePath(root, raw); err == nil {
			t.Fatalf("hostile changed-file evidence path %q must be rejected", raw)
		}
	}
	path, err := scopedEvidencePath(root, "allowed/file.go")
	if err != nil || path != filepath.Join(root, "allowed", "file.go") {
		t.Fatalf("relative changed-file evidence path must stay rooted: path=%q err=%v", path, err)
	}
}

func TestValidateWorkspaceScopeWriteScopeFailsClosedForTruncatedSnapshots(t *testing.T) {
	root := t.TempDir()
	for _, evidence := range []workspace.WorkspaceScopeEvidence{
		{Before: workspace.WorkspaceScopeSnapshot{Truncated: true}},
		{After: workspace.WorkspaceScopeSnapshot{Truncated: true}},
	} {
		if err := validateWorkspaceScopeWriteScope(evidence, root, []string{"allowed"}); err == nil {
			t.Fatalf("truncated non-Git snapshot must fail closed: %#v", evidence)
		}
	}
}

func TestGitChangedPathsChecksRenameSourceButNotCopySource(t *testing.T) {
	renamePaths := gitChangedPaths(workspace.GitChangedFile{Status: "R ", Path: "allowed/moved.go", OriginalPath: "outside.go"})
	if len(renamePaths) != 2 || !renamePaths[1].original || renamePaths[1].value != "outside.go" {
		t.Fatalf("rename source must be checked as a write: %#v", renamePaths)
	}
	copyPaths := gitChangedPaths(workspace.GitChangedFile{Status: "C ", Path: "allowed/copied.go", OriginalPath: "outside.go"})
	if len(copyPaths) != 1 || copyPaths[0].value != "allowed/copied.go" {
		t.Fatalf("copy source must remain evidence-only for write scope validation: %#v", copyPaths)
	}
}

func TestSameStringSetRejectsDuplicatesAndMismatchedMembers(t *testing.T) {
	if !sameStringSet([]string{"a", "b"}, []string{"b", "a"}) {
		t.Fatal("identical sets in different order must match")
	}
	for _, pair := range [][2][]string{
		{{"a"}, {"b"}},
		{{"a", "a"}, {"a", "a"}},
		{{""}, {""}},
		{{"a"}, {"a", "b"}},
	} {
		if sameStringSet(pair[0], pair[1]) {
			t.Fatalf("invalid set pair accepted: %#v", pair)
		}
	}
}
