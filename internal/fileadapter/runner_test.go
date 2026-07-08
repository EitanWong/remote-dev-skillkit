package fileadapter

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteListReadWriteWithinWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello rdev"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := Execute(Spec{WorkspaceRoot: root, Action: "list", Path: "."})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 1 || list.Entries[0].Path != "note.txt" {
		t.Fatalf("expected listed workspace file, got %#v", list.Entries)
	}

	read, err := Execute(Spec{WorkspaceRoot: root, Action: "read", Path: "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if read.ContentText != "hello rdev" || read.ContentBase64 != base64.StdEncoding.EncodeToString([]byte("hello rdev")) {
		t.Fatalf("expected text and base64 content, got %#v", read)
	}

	write, err := Execute(Spec{
		WorkspaceRoot: root,
		WriteScope:    []string{"out"},
		Action:        "write",
		Path:          "out/result.txt",
		Content:       "done",
	})
	if err != nil {
		t.Fatal(err)
	}
	if write.SHA256 == "" {
		t.Fatalf("expected write hash evidence, got %#v", write)
	}
	content, err := os.ReadFile(filepath.Join(root, "out", "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "done" {
		t.Fatalf("expected written content, got %q", string(content))
	}
}

func TestExecuteRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	parentFile := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(parentFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Execute(Spec{WorkspaceRoot: root, Action: "read", Path: "../outside.txt"}); err == nil {
		t.Fatal("expected path escape to be rejected")
	}
}

func TestExecuteRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if _, err := Execute(Spec{WorkspaceRoot: root, Action: "read", Path: "linked/secret.txt"}); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestExecuteDeleteWithinWriteScope(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "out", "old.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("remove me"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Execute(Spec{
		WorkspaceRoot: root,
		WriteScope:    []string{"out"},
		Action:        "delete",
		Path:          "out/old.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Deleted {
		t.Fatalf("expected delete evidence, got %#v", result)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target deleted, stat err=%v", err)
	}
}

func TestExecuteDeleteRejectsPathOutsideWriteScope(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "old.txt")
	if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(Spec{
		WorkspaceRoot: root,
		WriteScope:    []string{"out"},
		Action:        "delete",
		Path:          "old.txt",
	}); err == nil {
		t.Fatal("expected delete outside write_scope to be rejected")
	}
}

func TestExecuteRequiresWriteScope(t *testing.T) {
	root := t.TempDir()
	if _, err := Execute(Spec{
		WorkspaceRoot: root,
		Action:        "write",
		Path:          "result.txt",
		Content:       "nope",
	}); err == nil {
		t.Fatal("expected missing write_scope to be rejected")
	}
}
