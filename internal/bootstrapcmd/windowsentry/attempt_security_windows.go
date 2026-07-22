//go:build windows

package windowsentry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func validatePrivateAttemptDirectory(path string) (os.FileInfo, error) {
	if _, err := validateWindowsAttemptPath(path); err != nil {
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

func preparePrivateAttemptDirectory(path string) error {
	root, err := validateWindowsAttemptPath(path)
	if err != nil {
		return err
	}
	descriptor, trustees, err := windowsPrivateSecurityDescriptor()
	if err != nil {
		return err
	}
	defer descriptor.close()
	if err := ensureWindowsPrivateDirectory(root, descriptor, trustees); err != nil {
		return err
	}
	if err := createWindowsPrivateDirectory(path, descriptor); err != nil {
		return err
	}
	return validateWindowsPrivatePath(path, true, true, trustees)
}

func validatePrivateLauncherPath(path string, directory bool) error {
	if err := validateWindowsLocalPath(path); err != nil {
		return err
	}
	if err := rejectWindowsReparseAncestors(path); err != nil {
		return err
	}
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	flags := uint32(syscall.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= syscall.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := syscall.CreateFile(
		pointer,
		winReadControl|winFileReadAttributes,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		return err
	}
	expectedFlags := 0
	if directory {
		expectedFlags = 3
	}
	return validateWindowsPrivateHandle(handle, directory, true, expectedFlags, trustees)
}

func validateWindowsAttemptPath(path string) (string, error) {
	if err := validateWindowsLocalPath(path); err != nil {
		return "", err
	}
	localAppData, err := winKnownLocalAppData()
	if err != nil {
		return "", err
	}
	root := filepath.Join(filepath.Clean(localAppData), "RemoteDevSkillkit", "attempts")
	if !strings.EqualFold(filepath.Dir(path), root) || !validWindowsCacheBasename(filepath.Base(path)) {
		return "", fmt.Errorf("attempt must be a direct child of the Windows LocalAppData attempts root")
	}
	if err := rejectWindowsReparseAncestors(path); err != nil {
		return "", err
	}
	return root, nil
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
