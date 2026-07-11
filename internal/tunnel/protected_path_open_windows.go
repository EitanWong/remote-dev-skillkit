//go:build windows

package tunnel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func openProtectedPath(path string, directory bool) (*os.File, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(pointer, windows.GENERIC_READ|windows.READ_CONTROL, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return nil, err
	}
	finalPath, err := protectedWindowsFinalPath(handle)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("resolve protected path handle: %w", err)
	}
	if windowsFinalPathIsRemote(finalPath) {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("protected path must be stored on a local Windows volume")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	normalizedFinal := strings.TrimPrefix(finalPath, `\\?\`)
	if !strings.EqualFold(filepath.Clean(normalizedFinal), filepath.Clean(abs)) {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("protected path must not traverse a reparse point")
	}
	if err := validateWindowsLocalVolumePath(abs); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("protected path must not be a reparse point")
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("wrap protected path handle")
	}
	return file, nil
}
