//go:build !windows

package tunnel

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestValidateProtectedPathRejectsSharedDirectorySymlinkAndLooseFile(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	protected := filepath.Join(root, "protected")
	if err := os.Mkdir(protected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProtectedDirectory(protected); err != nil {
		t.Fatalf("protected directory rejected: %v", err)
	}
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProtectedDirectory(shared); err == nil {
		t.Fatal("shared writable directory accepted")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(protected, link); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProtectedDirectory(link); err == nil {
		t.Fatal("symlink directory accepted")
	}
	loose := filepath.Join(protected, "loose.json")
	if err := os.WriteFile(loose, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProtectedRegularFileIfExists(loose); err == nil {
		t.Fatal("0644 existing artifact accepted")
	}
}

func TestValidateProtectedRegularFileRejectsFIFOWithoutBlocking(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "input.fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- ValidateProtectedRegularFileIfExists(path) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as protected regular file")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO validation blocked")
	}
}

func TestValidateProtectedPathRejectsAncestorSymlink(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(target, "protected.json")
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "ancestor-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProtectedDirectory(link); err == nil {
		t.Fatal("ancestor symlink directory accepted")
	}
	if err := ValidateProtectedRegularFileIfExists(filepath.Join(link, "protected.json")); err == nil {
		t.Fatal("ancestor symlink file accepted")
	}
}
