//go:build !windows

package windowsentry

import (
	"fmt"
	"os"
	"path/filepath"
)

func validatePrivateAttemptDirectory(path string) (os.FileInfo, error) {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("attempt path has an unsafe ancestor")
		}
		if current == path && info.Mode().Perm() != 0o700 {
			return nil, fmt.Errorf("attempt directory permissions are not private")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return os.Lstat(path)
		}
	}
}

func createPrivateAttemptFile(directory, name string) (*os.File, error) {
	if filepath.Base(name) != name || name == "" {
		return nil, fmt.Errorf("invalid attempt filename")
	}
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func openPrivateAttemptFile(path string, maxBytes int64) (*os.File, error) {
	pathInfo, err := validatePrivateAttemptFile(path, -1)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !os.SameFile(info, pathInfo) || maxBytes >= 0 && info.Size() > maxBytes {
		file.Close()
		return nil, fmt.Errorf("attempt file changed or exceeded its byte bound")
	}
	return file, nil
}

func validatePrivateAttemptFile(path string, expectedSize int64) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || expectedSize >= 0 && info.Size() != expectedSize {
		return nil, fmt.Errorf("attempt file is not a private regular file")
	}
	return info, nil
}

func replacePrivateAttemptFile(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	return errorsJoinClose(directory.Sync(), directory.Close())
}

func errorsJoinClose(first, second error) error {
	if first != nil {
		return first
	}
	return second
}
