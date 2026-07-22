//go:build !windows

package connectionentry

import (
	"errors"
	"os"
	"testing"
)

func assertWindowsLayeredArchivePrivate(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Windows handoff archive must have mode 0600, got %o", info.Mode().Perm())
	}
}

func TestProtectWindowsLayeredArchiveUsesPrivateMode(t *testing.T) {
	path := t.TempDir() + "/handoff.zip"
	if err := os.WriteFile(path, []byte("archive"), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := protectWindowsLayeredArchive(path); err != nil {
		t.Fatal(err)
	}
	assertWindowsLayeredArchivePrivate(t, path)
}

func TestCreateWindowsLayeredArchiveTempFileUsesPrivateMode(t *testing.T) {
	file, err := createWindowsLayeredArchiveTempFile(t.TempDir(), "Windows-ConnectionEntry.zip")
	if err != nil {
		t.Fatal(err)
	}
	if info, statErr := file.Stat(); statErr != nil {
		t.Fatal(statErr)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("new archive temp file must have mode 0600, got %o", info.Mode().Perm())
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWrapArchiveTempCleanupError(t *testing.T) {
	if err := wrapArchiveTempCleanupError("remove", nil); err != nil {
		t.Fatalf("nil cleanup error must remain nil: %v", err)
	}
	sentinel := errors.New("cleanup failed")
	if err := wrapArchiveTempCleanupError("remove", sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("archive temp cleanup error must be wrapped, got %v", err)
	}
}

func TestValidateWindowsLayeredArchiveHandleRejectsNonPrivateMode(t *testing.T) {
	path := t.TempDir() + "/handoff.zip"
	if err := os.WriteFile(path, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, _, err := validateWindowsLayeredArchiveHandle(file); err == nil {
		t.Fatal("archive handle validation must reject a non-private mode")
	}
}
