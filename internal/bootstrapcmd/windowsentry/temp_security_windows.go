//go:build windows

package windowsentry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

const privateWindowsTempCreateAttempts = 128

var privateWindowsTempSequence atomic.Uint64

func actualWindowsDirectory() (string, error) {
	directory, err := winSystemWindowsDirectory()
	if err != nil {
		return "", fmt.Errorf("resolve Windows system directory: %w", err)
	}
	if err := validateWindowsLocalPath(filepath.Clean(directory)); err != nil {
		return "", err
	}
	return filepath.Clean(directory), nil
}

func validateWindowsSystemExecutable(path string) error {
	if err := rejectWindowsReparseAncestors(filepath.Dir(path)); err != nil {
		return err
	}
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := syscall.CreateFile(
		pointer,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return fmt.Errorf("open Windows system curl: %w", err)
	}
	defer syscall.CloseHandle(handle)
	var information syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &information); err != nil {
		return err
	}
	if information.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || information.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fmt.Errorf("Windows system curl is not a local regular file")
	}
	return nil
}

func createPrivateTemporaryDirectory(prefix string) (string, error) {
	if prefix == "" || filepath.Base(prefix) != prefix {
		return "", fmt.Errorf("invalid private temporary directory prefix")
	}
	localAppData, err := winKnownLocalAppData()
	if err != nil {
		return "", err
	}
	base := filepath.Join(filepath.Clean(localAppData), "Temp")
	if err := rejectWindowsReparseAncestors(base); err != nil {
		return "", err
	}
	attributes, err := windowsPathAttributes(base)
	if err != nil || attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return "", fmt.Errorf("Windows user temporary directory is unavailable")
	}
	descriptor, trustees, err := windowsPrivateSecurityDescriptor()
	if err != nil {
		return "", err
	}
	defer descriptor.close()
	for attempt := 0; attempt < privateWindowsTempCreateAttempts; attempt++ {
		name := prefix + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(privateWindowsTempSequence.Add(1), 36)
		path := filepath.Join(base, name)
		err := createWindowsPrivateDirectory(path, descriptor)
		if errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := validateWindowsPrivatePath(path, true, true, trustees); err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("create private Windows temporary directory after %d attempts", privateWindowsTempCreateAttempts)
}

func createPrivateTemporaryFile(directory, name string) (*os.File, error) {
	if filepath.Base(name) != name || name == "" {
		return nil, fmt.Errorf("invalid private temporary filename")
	}
	descriptor, trustees, err := windowsPrivateSecurityDescriptor()
	if err != nil {
		return nil, err
	}
	defer descriptor.close()
	if err := validateWindowsPrivatePath(directory, true, true, trustees); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, name)
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(
		pointer,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE|winReadControl,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		descriptor.attributes(),
		syscall.CREATE_NEW,
		winFileAttributeTemporary,
		0,
	)
	if err != nil {
		return nil, err
	}
	if err := validateWindowsPrivateHandle(handle, false, true, trustees); err != nil {
		syscall.CloseHandle(handle)
		_ = os.Remove(path)
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		syscall.CloseHandle(handle)
		_ = os.Remove(path)
		return nil, fmt.Errorf("wrap private Windows temporary file")
	}
	return file, nil
}

func validatePrivateTemporaryFile(file *os.File, path string, maxBytes int64) error {
	if file == nil {
		return fmt.Errorf("private Windows temporary file handle is required")
	}
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		return err
	}
	if err := validateWindowsPrivateHandle(syscall.Handle(file.Fd()), false, true, trustees); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	pathInfo, pathErr := os.Lstat(path)
	if pathErr != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) || !info.Mode().IsRegular() || info.Size() > maxBytes {
		return fmt.Errorf("private Windows temporary file changed or exceeded its byte bound")
	}
	return nil
}
