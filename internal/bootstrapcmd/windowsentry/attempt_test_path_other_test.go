//go:build !windows && !rdev_bootstrap_focused

package windowsentry

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func privateAttemptDirForTest(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(canonicalWindowsEntryTestTempDir(t), "attempt-opaque-id")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func attemptProcessRunningForTest(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func killAttemptProcessForTest(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}

func assertPrivateAttemptPathForTest(t *testing.T, path string, directory bool) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() != directory {
		t.Fatalf("attempt path has unsafe type: %s", path)
	}
	want := os.FileMode(0o600)
	if directory {
		want = 0o700
	}
	if info.Mode().Perm() != want {
		t.Fatalf("attempt path mode = %o, want %o", info.Mode().Perm(), want)
	}
}
