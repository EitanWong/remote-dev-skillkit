//go:build !windows

package connectionentry

import (
	"errors"
	"fmt"
	"os"
)

type windowsLayeredArchiveFileIdentity struct {
	info os.FileInfo
}

func createWindowsLayeredArchiveTempFile(dir, base string) (*os.File, error) {
	file, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		closeErr := file.Close()
		removeErr := os.Remove(file.Name())
		return nil, errors.Join(
			fmt.Errorf("set archive temp file mode 0600: %w", err),
			wrapArchiveTempCleanupError("close", closeErr),
			wrapArchiveTempCleanupError("remove", removeErr),
		)
	}
	return file, nil
}

func wrapArchiveTempCleanupError(operation string, err error) error {
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("%s archive temp file after protection failure: %w", operation, err)
}

func validateWindowsLayeredArchiveHandle(file *os.File) (windowsLayeredArchiveFileIdentity, string, error) {
	info, err := file.Stat()
	if err != nil {
		return windowsLayeredArchiveFileIdentity{}, "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return windowsLayeredArchiveFileIdentity{}, "", fmt.Errorf("archive mode is %o, want regular mode 0600", info.Mode().Perm())
	}
	return windowsLayeredArchiveFileIdentity{info: info}, "mode 0600", nil
}

func sameWindowsLayeredArchiveFileIdentity(first, second windowsLayeredArchiveFileIdentity) bool {
	return first.info != nil && second.info != nil && os.SameFile(first.info, second.info)
}

func publishWindowsLayeredArchiveHandle(file *os.File, path string) (bool, error) {
	err := os.Rename(file.Name(), path)
	return err == nil, err
}

func openPublishedWindowsLayeredArchive(path string) (*os.File, error) {
	return os.Open(path)
}

func protectWindowsLayeredArchive(path string) (string, error) {
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return validateWindowsLayeredArchiveProtection(path)
}

func validateWindowsLayeredArchiveProtection(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm() != 0o600 {
		return "", fmt.Errorf("archive mode is %o, want 0600", info.Mode().Perm())
	}
	return "mode 0600", nil
}
