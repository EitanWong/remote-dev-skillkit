//go:build windows && rdev_bootstrap_focused

package assetdownload

import (
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func atomicReplace(sourcePath, destinationPath string) error {
	source, err := syscall.UTF16PtrFromString(sourcePath)
	if err != nil {
		return err
	}
	destination, err := syscall.UTF16PtrFromString(destinationPath)
	if err != nil {
		return err
	}
	result, _, callErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(source)),
		uintptr(unsafe.Pointer(destination)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}
