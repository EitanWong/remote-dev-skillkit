//go:build windows

package connectionentry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func assertWindowsLayeredArchivePrivate(t *testing.T, path string) {
	t.Helper()
	protection, err := inspectWindowsLayeredArchiveProtection(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsLayeredArchiveProtectionState(protection); err != nil {
		t.Fatalf("Windows handoff archive protection is not private: %v", err)
	}
}

func TestWriteWindowsLayeredArchiveInstallsProtectedDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.zip")
	report, err := writeWindowsLayeredArchive(path, time.Unix(0, 0), []windowsLayeredArchiveFile{{
		name: "entry.txt", content: []byte("entry\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Private || report.PrivacyDetail == "" {
		t.Fatalf("archive report must record verified Windows DACL protection: %#v", report)
	}
	assertWindowsLayeredArchivePrivate(t, path)
}

func TestCreateWindowsLayeredArchiveTempFileStartsProtected(t *testing.T) {
	file, err := createWindowsLayeredArchiveTempFile(t.TempDir(), "Windows-ConnectionEntry.zip")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if info, statErr := file.Stat(); statErr != nil {
		t.Fatal(statErr)
	} else if info.Size() != 0 {
		t.Fatalf("new protected archive temp file must be empty, got %d bytes", info.Size())
	}
	if _, err := validateWindowsLayeredArchiveProtection(path); err != nil {
		t.Fatalf("archive temp file was not protected at creation time: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWindowsLayeredArchiveTempFileDeniesWriteAndDeleteSharing(t *testing.T) {
	file, err := createWindowsLayeredArchiveTempFile(t.TempDir(), "Windows-ConnectionEntry.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	pointer, err := windows.UTF16PtrFromString(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	for name, access := range map[string]uint32{
		"write":  windows.GENERIC_WRITE,
		"delete": windows.DELETE,
	} {
		t.Run(name, func(t *testing.T) {
			handle, openErr := windows.CreateFile(
				pointer,
				access,
				windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
				nil,
				windows.OPEN_EXISTING,
				windows.FILE_ATTRIBUTE_NORMAL,
				0,
			)
			if openErr == nil {
				windows.CloseHandle(handle)
				t.Fatalf("protected archive creation handle allowed a concurrent %s open", name)
			}
			if !errors.Is(openErr, windows.ERROR_SHARING_VIOLATION) {
				t.Fatalf("concurrent %s open error = %v, want sharing violation", name, openErr)
			}
		})
	}
}

func TestOpenPublishedWindowsLayeredArchiveDeniesWriteAndDeleteSharing(t *testing.T) {
	temporary, err := createWindowsLayeredArchiveTempFile(t.TempDir(), "Windows-ConnectionEntry.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := temporary.Write([]byte("archive\n")); err != nil {
		t.Fatal(err)
	}
	defer temporary.Close()
	path := filepath.Join(filepath.Dir(temporary.Name()), "Windows-ConnectionEntry.zip")
	if published, err := publishWindowsLayeredArchiveHandle(temporary, path); err != nil {
		t.Fatal(err)
	} else if !published {
		t.Fatal("archive handle was not published")
	}
	file, err := openPublishedWindowsLayeredArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, access := range map[string]uint32{
		"write":  windows.GENERIC_WRITE,
		"delete": windows.DELETE,
	} {
		t.Run(name, func(t *testing.T) {
			handle, openErr := windows.CreateFile(
				pointer,
				access,
				windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
				nil,
				windows.OPEN_EXISTING,
				windows.FILE_ATTRIBUTE_NORMAL,
				0,
			)
			if openErr == nil {
				windows.CloseHandle(handle)
				t.Fatalf("published archive validation handle allowed a concurrent %s open", name)
			}
			if !errors.Is(openErr, windows.ERROR_SHARING_VIOLATION) {
				t.Fatalf("concurrent %s open error = %v, want sharing violation", name, openErr)
			}
		})
	}
}

func TestPublishWindowsLayeredArchiveHandleAcrossDirectories(t *testing.T) {
	root := t.TempDir()
	temporary, err := createWindowsLayeredArchiveTempFile(root, "Windows-ConnectionEntry.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer temporary.Close()
	if _, err := temporary.Write([]byte("archive\n")); err != nil {
		t.Fatal(err)
	}
	destinationDir := filepath.Join(root, "entry")
	if err := os.Mkdir(destinationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	destinationPath := filepath.Join(destinationDir, "Windows-ConnectionEntry.zip")
	published, err := publishWindowsLayeredArchiveHandle(temporary, destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("archive handle was not published across directories")
	}
	if _, err := os.Stat(destinationPath); err != nil {
		t.Fatalf("published archive path is unavailable: %v", err)
	}
}
