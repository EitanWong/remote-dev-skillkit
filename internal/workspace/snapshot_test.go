package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotWorkspaceScopeIsBoundedAndDetectsScopedChanges(t *testing.T) {
	root := t.TempDir()
	mustWriteWorkspaceSnapshotFile(t, filepath.Join(root, "scoped", "tracked.txt"), "first\n")
	mustWriteWorkspaceSnapshotFile(t, filepath.Join(root, "outside", "ignored.txt"), "outside\n")

	before, err := SnapshotWorkspaceScope(root, []string{"scoped"})
	if err != nil {
		t.Fatal(err)
	}
	if before.SchemaVersion != WorkspaceScopeSnapshotSchemaVersion || before.FileCount != 1 || before.TreeSHA256 == "" {
		t.Fatalf("unexpected initial scope snapshot: %#v", before)
	}
	if err := os.WriteFile(filepath.Join(root, "scoped", "tracked.txt"), []byte("second longer value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := SnapshotWorkspaceScope(root, []string{"scoped"})
	if err != nil {
		t.Fatal(err)
	}
	if before.TreeSHA256 == after.TreeSHA256 {
		t.Fatalf("scoped tree hash must change after a scoped file changes: before=%#v after=%#v", before, after)
	}
	if _, err := SnapshotWorkspaceScope(root, []string{"../outside"}); err == nil {
		t.Fatal("scope escaping workspace root must be rejected")
	}
}

func mustWriteWorkspaceSnapshotFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
