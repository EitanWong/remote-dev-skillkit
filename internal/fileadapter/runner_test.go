package fileadapter

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

func TestExecuteWriteReportsExpectedTransferMatch(t *testing.T) {
	root := t.TempDir()
	content := []byte("verified upload")
	sum := sha256.Sum256(content)

	write, err := Execute(Spec{
		WorkspaceRoot:  root,
		WriteScope:     []string{"out"},
		Action:         "upload",
		Path:           "out/result.txt",
		Content:        string(content),
		ExpectedBytes:  len(content),
		ExpectedSHA256: "sha256:" + hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if write.ByteCompare != "match" ||
		write.ExpectedBytes != len(content) ||
		write.ExpectedSHA256 != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("expected transfer match evidence, got %#v", write)
	}
}

func TestExecuteWriteReportsExpectedTransferMismatch(t *testing.T) {
	root := t.TempDir()

	write, err := Execute(Spec{
		WorkspaceRoot:  root,
		WriteScope:     []string{"out"},
		Action:         "upload",
		Path:           "out/result.txt",
		Content:        "actual",
		ExpectedBytes:  len("expected"),
		ExpectedSHA256: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if write.ByteCompare != "mismatch" {
		t.Fatalf("expected transfer mismatch evidence, got %#v", write)
	}
}

func TestExecuteReadSupportsResumableChunks(t *testing.T) {
	root := t.TempDir()
	content := []byte("0123456789abcdef")
	path := filepath.Join(root, "artifact.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	expectedHash := "sha256:" + hex.EncodeToString(sum[:])

	first, err := Execute(Spec{
		WorkspaceRoot: root,
		Action:        "read",
		Path:          "artifact.bin",
		Offset:        0,
		ChunkBytes:    5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentBase64 != base64.StdEncoding.EncodeToString(content[:5]) ||
		first.Offset != 0 || first.NextOffset != 5 || first.TotalBytes != int64(len(content)) ||
		first.Complete || first.SHA256 != expectedHash || first.ChunkSHA256 == "" {
		t.Fatalf("unexpected first chunk: %#v", first)
	}

	second, err := Execute(Spec{
		WorkspaceRoot: root,
		Action:        "download",
		Path:          "artifact.bin",
		Offset:        first.NextOffset,
		ChunkBytes:    32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ContentBase64 != base64.StdEncoding.EncodeToString(content[5:]) ||
		second.Offset != 5 || second.NextOffset != int64(len(content)) ||
		!second.Complete || second.SHA256 != expectedHash {
		t.Fatalf("unexpected final chunk: %#v", second)
	}
}

func TestExecuteReadRejectsInvalidChunkOffset(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "artifact.bin"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(Spec{
		WorkspaceRoot: root,
		Action:        "read",
		Path:          "artifact.bin",
		Offset:        5,
		ChunkBytes:    2,
	}); err == nil {
		t.Fatal("expected offset beyond file size to be rejected")
	}
}

func TestExecuteExpandsHomeWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(home, "note.txt"), []byte("from home"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}

	read, err := Execute(Spec{WorkspaceRoot: "~", Action: "read", Path: "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if read.WorkspaceRoot != canonicalHome || read.ContentText != "from home" {
		t.Fatalf("expected ~ to resolve to home, got %#v", read)
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
