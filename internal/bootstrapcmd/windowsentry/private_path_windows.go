//go:build windows

package windowsentry

import (
	"fmt"
	"syscall"
	"unsafe"
)

const winProtectedDACLSecurityInfo uint32 = 0x80000000

var winSetFileSecurityW = winAdvapi32.NewProc("SetFileSecurityW")

func ProtectPrivatePath(path string, directory bool) error {
	if err := validateWindowsLocalPath(path); err != nil {
		return err
	}
	if err := rejectWindowsReparseAncestors(path); err != nil {
		return err
	}
	attributes, err := windowsPathAttributes(path)
	if err != nil {
		return err
	}
	if attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (attributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		return fmt.Errorf("private Windows path has an unexpected type")
	}
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		return err
	}
	flags := ""
	if directory {
		flags = "OICI"
	}
	descriptor, err := newWinSecurityDescriptor(fmt.Sprintf(
		"D:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)(A;%s;FA;;;BA)",
		flags, trustees.current, flags, flags,
	))
	if err != nil {
		return err
	}
	defer descriptor.close()
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, callErr := winSetFileSecurityW.Call(
		uintptr(unsafe.Pointer(pointer)),
		uintptr(winDACLSecurityInfo|winProtectedDACLSecurityInfo),
		descriptor.pointer,
	)
	if result == 0 {
		return winCallError(callErr)
	}
	return ValidatePrivatePath(path, directory)
}

func ValidatePrivatePath(path string, directory bool) error {
	return validatePrivateLauncherPath(path, directory)
}
