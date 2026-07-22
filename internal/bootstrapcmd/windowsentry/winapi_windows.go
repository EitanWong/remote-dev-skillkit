//go:build windows

package windowsentry

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	winReadControl            uint32 = 0x00020000
	winFileReadAttributes     uint32 = 0x00000080
	winFileAttributeTemporary uint32 = 0x00000100
	winDriveRemovable         uint32 = 2
	winDriveRemote            uint32 = 4
	winDriveRAMDisk           uint32 = 6
	winSEFileObject           uint32 = 1
	winOwnerSecurityInfo      uint32 = 0x00000001
	winDACLSecurityInfo       uint32 = 0x00000004
	winSEDACLPresent          uint16 = 0x0004
	winSEDACLProtected        uint16 = 0x1000
	winAccessAllowedACEType   byte   = 0
	winPrivateFullControl     uint32 = 0x001f01ff
)

var (
	winAdvapi32                         = syscall.NewLazyDLL("advapi32.dll")
	winKernel32                         = syscall.NewLazyDLL("kernel32.dll")
	winShell32                          = syscall.NewLazyDLL("shell32.dll")
	winConvertStringSecurityDescriptorW = winAdvapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	winGetSecurityInfo                  = winAdvapi32.NewProc("GetSecurityInfo")
	winIsValidSid                       = winAdvapi32.NewProc("IsValidSid")
	winGetDriveTypeW                    = winKernel32.NewProc("GetDriveTypeW")
	winGetSystemWindowsDirectoryW       = winKernel32.NewProc("GetSystemWindowsDirectoryW")
	winSHGetKnownFolderPath             = winShell32.NewProc("SHGetKnownFolderPath")
	winLocalAppDataFolderID             = winGUID{0xf1b32785, 0x6fba, 0x4fcf, [8]byte{0x9d, 0x55, 0x7b, 0x8e, 0x7f, 0x15, 0x70, 0x91}}
)

type winGUID struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

type winSecurityDescriptor struct {
	pointer uintptr
}

func newWinSecurityDescriptor(sddl string) (winSecurityDescriptor, error) {
	encoded, err := syscall.UTF16PtrFromString(sddl)
	if err != nil {
		return winSecurityDescriptor{}, err
	}
	var descriptor uintptr
	result, _, callErr := winConvertStringSecurityDescriptorW.Call(
		uintptr(unsafe.Pointer(encoded)),
		1,
		uintptr(unsafe.Pointer(&descriptor)),
		0,
	)
	if result == 0 {
		return winSecurityDescriptor{}, winCallError(callErr)
	}
	if descriptor == 0 {
		return winSecurityDescriptor{}, fmt.Errorf("create private Windows security descriptor: empty result")
	}
	return winSecurityDescriptor{pointer: descriptor}, nil
}

func (descriptor winSecurityDescriptor) close() error {
	if descriptor.pointer == 0 {
		return nil
	}
	_, err := syscall.LocalFree(syscall.Handle(descriptor.pointer))
	return err
}

func (descriptor winSecurityDescriptor) attributes() *syscall.SecurityAttributes {
	return &syscall.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(syscall.SecurityAttributes{})),
		SecurityDescriptor: descriptor.pointer,
	}
}

type winACE struct {
	typeID byte
	flags  byte
	mask   uint32
	sid    string
}

type winSecurityState struct {
	control uint16
	owner   string
	aces    []winACE
}

type winACLHeader struct {
	revision byte
	reserved byte
	size     uint16
	aceCount uint16
	padding  uint16
}

type winACEHeader struct {
	typeID byte
	flags  byte
	size   uint16
}

func readWinSecurityState(handle syscall.Handle) (winSecurityState, error) {
	var owner unsafe.Pointer
	var dacl unsafe.Pointer
	var descriptor unsafe.Pointer
	result, _, _ := winGetSecurityInfo.Call(
		uintptr(handle),
		uintptr(winSEFileObject),
		uintptr(winOwnerSecurityInfo|winDACLSecurityInfo),
		uintptr(unsafe.Pointer(&owner)),
		0,
		uintptr(unsafe.Pointer(&dacl)),
		0,
		uintptr(unsafe.Pointer(&descriptor)),
	)
	if result != 0 {
		return winSecurityState{}, syscall.Errno(result)
	}
	if descriptor == nil {
		return winSecurityState{}, fmt.Errorf("private Windows security descriptor is empty")
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(descriptor)))
	if owner == nil || !validWinSID(owner) {
		return winSecurityState{}, fmt.Errorf("private Windows owner SID is invalid")
	}
	ownerString, err := (*syscall.SID)(owner).String()
	if err != nil {
		return winSecurityState{}, err
	}
	state := winSecurityState{
		control: *(*uint16)(unsafe.Add(descriptor, 2)),
		owner:   ownerString,
	}
	if dacl == nil {
		return state, nil
	}
	header := (*winACLHeader)(dacl)
	const aclHeaderBytes = uintptr(8)
	if uintptr(header.size) < aclHeaderBytes {
		return winSecurityState{}, fmt.Errorf("private Windows DACL is malformed")
	}
	offset := aclHeaderBytes
	state.aces = make([]winACE, 0, header.aceCount)
	for index := uint16(0); index < header.aceCount; index++ {
		if offset+unsafe.Sizeof(winACEHeader{}) > uintptr(header.size) {
			return winSecurityState{}, fmt.Errorf("private Windows ACE %d exceeds the DACL", index)
		}
		acePointer := unsafe.Add(dacl, offset)
		aceHeader := (*winACEHeader)(acePointer)
		aceSize := uintptr(aceHeader.size)
		const sidOffset = uintptr(8)
		if aceSize < sidOffset+8 || offset+aceSize > uintptr(header.size) {
			return winSecurityState{}, fmt.Errorf("private Windows ACE %d is malformed", index)
		}
		sidPointer := unsafe.Add(acePointer, sidOffset)
		subAuthorityCount := uintptr(*(*byte)(unsafe.Add(sidPointer, 1)))
		sidLength := uintptr(8) + 4*subAuthorityCount
		if *(*byte)(sidPointer) != 1 || subAuthorityCount > 15 || sidLength > aceSize-sidOffset {
			return winSecurityState{}, fmt.Errorf("private Windows ACE %d SID exceeds the ACE", index)
		}
		if !validWinSID(sidPointer) {
			return winSecurityState{}, fmt.Errorf("private Windows ACE %d has an invalid SID", index)
		}
		sid := (*syscall.SID)(sidPointer)
		if uintptr(sid.Len()) != sidLength {
			return winSecurityState{}, fmt.Errorf("private Windows ACE %d SID exceeds the ACE", index)
		}
		sidString, err := sid.String()
		if err != nil {
			return winSecurityState{}, err
		}
		state.aces = append(state.aces, winACE{
			typeID: aceHeader.typeID,
			flags:  aceHeader.flags,
			mask:   *(*uint32)(unsafe.Add(acePointer, 4)),
			sid:    sidString,
		})
		offset += aceSize
	}
	return state, nil
}

func validWinSID(pointer unsafe.Pointer) bool {
	result, _, _ := winIsValidSid.Call(uintptr(pointer))
	return result != 0
}

func currentWinUserSID() (string, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil {
		return "", fmt.Errorf("read current Windows user SID: %w", err)
	}
	return user.User.Sid.String()
}

func winKnownLocalAppData() (string, error) {
	var path *uint16
	result, _, _ := winSHGetKnownFolderPath.Call(
		uintptr(unsafe.Pointer(&winLocalAppDataFolderID)),
		0,
		0,
		uintptr(unsafe.Pointer(&path)),
	)
	if result != 0 {
		return "", fmt.Errorf("resolve Windows LocalAppData: HRESULT 0x%x", uint32(result))
	}
	if path == nil {
		return "", fmt.Errorf("resolve Windows LocalAppData: empty path")
	}
	value, valueErr := winUTF16PointerString(path)
	freeErr := winFreeCOMMemory(unsafe.Pointer(path))
	if valueErr != nil {
		return "", valueErr
	}
	if freeErr != nil {
		return "", freeErr
	}
	return value, nil
}

func winFreeCOMMemory(pointer unsafe.Pointer) error {
	windowsDirectory, err := winSystemWindowsDirectory()
	if err != nil {
		return err
	}
	dll, err := syscall.LoadDLL(filepath.Join(windowsDirectory, "System32", "ole32.dll"))
	if err != nil {
		return fmt.Errorf("load Windows COM allocator: %w", err)
	}
	defer dll.Release()
	procedure, err := dll.FindProc("CoTaskMemFree")
	if err != nil {
		return fmt.Errorf("resolve Windows COM allocator: %w", err)
	}
	procedure.Call(uintptr(pointer))
	return nil
}

func winSystemWindowsDirectory() (string, error) {
	size := uint32(260)
	for {
		buffer := make([]uint16, size)
		length, _, callErr := winGetSystemWindowsDirectoryW.Call(
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(size),
		)
		if length == 0 {
			return "", winCallError(callErr)
		}
		if length < uintptr(size) {
			return syscall.UTF16ToString(buffer[:length]), nil
		}
		size = uint32(length) + 1
		if size > 32768 {
			return "", fmt.Errorf("Windows system directory path is too long")
		}
	}
}

func winGetDriveType(root *uint16) uint32 {
	result, _, _ := winGetDriveTypeW.Call(uintptr(unsafe.Pointer(root)))
	return uint32(result)
}

func winUTF16PointerString(pointer *uint16) (string, error) {
	for length := 0; length < 32768; length++ {
		if *(*uint16)(unsafe.Add(unsafe.Pointer(pointer), uintptr(length)*2)) == 0 {
			return syscall.UTF16ToString(unsafe.Slice(pointer, length)), nil
		}
	}
	return "", fmt.Errorf("Windows path exceeds 32767 UTF-16 code units")
}

func winCallError(err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno != 0 {
		return errno
	}
	return syscall.EINVAL
}
