//go:build windows

package tunnel

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func openProtectedJSONFile(path string) (*os.File, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode protected JSON path: %w", err)
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.GENERIC_READ|windows.READ_CONTROL,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	finalPath, err := protectedWindowsFinalPath(handle)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("resolve protected JSON handle path: %w", err)
	}
	if windowsFinalPathIsRemote(finalPath) {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("protected JSON input must be stored on a local Windows volume")
	}
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("inspect protected JSON handle: %w", err)
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("protected JSON input must not be a reparse point")
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("wrap protected JSON handle")
	}
	return file, nil
}

func protectedWindowsFinalPath(handle windows.Handle) (string, error) {
	buffer := make([]uint16, 512)
	for {
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if length == 0 {
			return "", fmt.Errorf("empty final path")
		}
		if int(length) < len(buffer) {
			return windows.UTF16ToString(buffer[:length]), nil
		}
		buffer = make([]uint16, int(length)+1)
	}
}
