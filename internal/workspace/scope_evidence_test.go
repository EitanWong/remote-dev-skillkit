package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangedWorkspaceSnapshotFiles(t *testing.T) {
	changed, truncated := changedWorkspaceSnapshotFiles(nil, nil)
	if changed != nil || truncated {
		t.Fatalf("nil maps: %v %v", changed, truncated)
	}
	before := map[string]string{"a.txt": "rec-a", "b.txt": "rec-b"}
	after := map[string]string{"a.txt": "rec-a-changed", "b.txt": "rec-b", "c.txt": "rec-c"}
	changed, truncated = changedWorkspaceSnapshotFiles(before, after)
	if truncated || len(changed) != 2 || changed[0] != "a.txt" || changed[1] != "c.txt" {
		t.Fatalf("changed files wrong: %v truncated=%v", changed, truncated)
	}

	bigBefore := map[string]string{}
	bigAfter := map[string]string{}
	for i := 0; i < 250; i++ {
		bigBefore[filepath.Join("x", string(rune('a'+i%26))+string(rune('0'+i/26)))] = "old"
		bigAfter[filepath.Join("x", string(rune('a'+i%26))+string(rune('0'+i/26)))] = "new"
	}
	changed, truncated = changedWorkspaceSnapshotFiles(bigBefore, bigAfter)
	if !truncated || len(changed) != 200 {
		t.Fatalf("expected 200-file truncation, got %d truncated=%v", len(changed), truncated)
	}
}

func TestCompareWorkspaceScopeDetectsChange(t *testing.T) {
	same := WorkspaceScopeSnapshot{TreeSHA256: "sha256:abc", FileCount: 1}
	evidence := CompareWorkspaceScope(same, same)
	if evidence.Changed || len(evidence.ChangedFiles) != 0 {
		t.Fatalf("identical snapshots must be unchanged: %#v", evidence)
	}

	before := WorkspaceScopeSnapshot{
		TreeSHA256: "sha256:abc",
		FileCount:  2,
		entries:    map[string]string{"a": "1", "b": "2"},
	}
	after := WorkspaceScopeSnapshot{
		TreeSHA256: "sha256:def",
		FileCount:  3,
		entries:    map[string]string{"a": "1", "b": "9", "c": "3"},
	}
	evidence = CompareWorkspaceScope(before, after)
	if !evidence.Changed || len(evidence.ChangedFiles) != 2 {
		t.Fatalf("expected changed evidence: %#v", evidence)
	}
}

func TestSnapshotWorkspaceScopeDetectsFileMutation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "code.go"), []byte("package x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// .git must be excluded from the digest.
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref"), 0o600); err != nil {
		t.Fatal(err)
	}

	before, err := SnapshotWorkspaceScope(root, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	if before.FileCount != 2 || before.Truncated {
		t.Fatalf("unexpected snapshot: %#v", before)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("version two with more bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := SnapshotWorkspaceScope(root, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	evidence := CompareWorkspaceScope(before, after)
	if !evidence.Changed || len(evidence.ChangedFiles) != 1 || evidence.ChangedFiles[0] != "keep.txt" {
		t.Fatalf("expected keep.txt change: %#v", evidence)
	}
}

func TestSnapshotWorkspaceScopeRejectsEscapingScope(t *testing.T) {
	root := t.TempDir()
	if _, err := SnapshotWorkspaceScope(root, []string{".."}); err == nil {
		t.Fatal("escaping scope accepted")
	}
	if _, err := SnapshotWorkspaceScope(root, []string{"/etc"}); err == nil {
		t.Fatal("absolute escaping scope accepted")
	}
}

func TestSnapshotWorkspaceScopeRequiresExistingScope(t *testing.T) {
	root := t.TempDir()
	if _, err := SnapshotWorkspaceScope(root, []string{"missing-dir"}); err == nil {
		t.Fatal("missing scope directory accepted")
	}
}

func TestSnapshotWorkspaceScopeDefaultsToRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := SnapshotWorkspaceScope(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Scopes) != 1 || snapshot.Scopes[0] != "." || snapshot.FileCount != 1 {
		t.Fatalf("unexpected default-scope snapshot: %#v", snapshot)
	}
	if !strings.HasPrefix(snapshot.TreeSHA256, "sha256:") {
		t.Fatalf("tree hash missing prefix: %q", snapshot.TreeSHA256)
	}
}
