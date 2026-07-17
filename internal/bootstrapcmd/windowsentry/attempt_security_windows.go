//go:build windows

package windowsentry

import (
	"fmt"
	"os"
)

func validatePrivateAttemptDirectory(path string) (os.FileInfo, error) {
	if err := validateWindowsLocalPath(path); err != nil {
		return nil, err
	}
	if err := rejectWindowsReparseAncestors(path); err != nil {
		return nil, err
	}
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		return nil, err
	}
	if err := validateWindowsPrivatePath(path, true, true, trustees); err != nil {
		return nil, err
	}
	return os.Lstat(path)
}

func createPrivateAttemptFile(directory, name string) (*os.File, error) {
	return createPrivateTemporaryFile(directory, name)
}

func openPrivateAttemptFile(path string, maxBytes int64) (*os.File, error) {
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		return nil, err
	}
	file, err := openWindowsPrivateFile(path, false, trustees)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || maxBytes >= 0 && info.Size() > maxBytes {
		file.Close()
		return nil, fmt.Errorf("attempt file exceeded its byte bound")
	}
	return file, nil
}

func validatePrivateAttemptFile(path string, expectedSize int64) (os.FileInfo, error) {
	file, err := openPrivateAttemptFile(path, -1)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || expectedSize >= 0 && info.Size() != expectedSize {
		return nil, fmt.Errorf("attempt file is not a private regular file")
	}
	return info, nil
}

func replacePrivateAttemptFile(source, destination string) error {
	return os.Rename(source, destination)
}
