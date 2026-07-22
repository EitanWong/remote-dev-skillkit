//go:build windows

package connectionentry

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsLayeredArchivePrivacyDetail = "protected DACL: current user, SYSTEM, Administrators"

const windowsLayeredArchiveTempCreateAttempts = 128

const windowsLayeredArchiveFullControlMask uint32 = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

type windowsLayeredArchiveACE struct {
	typeID uint8
	flags  uint8
	mask   uint32
	sid    string
}

type windowsLayeredArchiveProtection struct {
	ownerSID      string
	daclPresent   bool
	daclProtected bool
	daclNull      bool
	aces          []windowsLayeredArchiveACE
}

type windowsLayeredArchiveFileIdentity struct {
	volumeSerialNumber uint32
	fileIndexHigh      uint32
	fileIndexLow       uint32
}

type windowsLayeredArchiveRenameInformation struct {
	replaceIfExists uint8
	rootDirectory   windows.Handle
	fileNameLength  uint32
	fileName        [1]uint16
}

func createWindowsLayeredArchiveTempFile(dir, base string) (*os.File, error) {
	if base == "" || filepath.Base(base) != base {
		return nil, fmt.Errorf("invalid Windows layered archive temp basename %q", base)
	}
	currentUser, _, _, err := windowsLayeredArchiveTrustees()
	if err != nil {
		return nil, err
	}
	currentUserSID := currentUser.String()
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf(
		"O:%sD:P(A;;FA;;;%s)(A;;FA;;;SY)(A;;FA;;;BA)",
		currentUserSID,
		currentUserSID,
	))
	if err != nil {
		return nil, fmt.Errorf("create Windows layered archive security descriptor: %w", err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}

	for attempt := 0; attempt < windowsLayeredArchiveTempCreateAttempts; attempt++ {
		var randomSuffix [16]byte
		if _, err := rand.Read(randomSuffix[:]); err != nil {
			return nil, fmt.Errorf("generate Windows layered archive temp basename: %w", err)
		}
		path := filepath.Join(dir, "."+base+".tmp-"+hex.EncodeToString(randomSuffix[:]))
		pointer, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return nil, fmt.Errorf("encode Windows layered archive temp path: %w", err)
		}
		handle, err := windows.CreateFile(
			pointer,
			windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
			windows.FILE_SHARE_READ,
			&attributes,
			windows.CREATE_NEW,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		runtime.KeepAlive(descriptor)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("atomically create protected Windows layered archive temp file: %w", err)
		}
		file := os.NewFile(uintptr(handle), path)
		if file == nil {
			closeErr := windows.CloseHandle(handle)
			removeErr := os.Remove(path)
			return nil, errors.Join(
				fmt.Errorf("wrap protected Windows layered archive temp handle"),
				wrapWindowsLayeredArchiveHandleCloseError(closeErr),
				wrapWindowsLayeredArchiveCreateCleanupError("remove", removeErr),
			)
		}
		if _, _, err := validateWindowsLayeredArchiveHandle(file); err != nil {
			closeErr := file.Close()
			removeErr := os.Remove(path)
			return nil, errors.Join(
				fmt.Errorf("validate new Windows layered archive temp protection: %w", err),
				wrapWindowsLayeredArchiveCreateCleanupError("close", closeErr),
				wrapWindowsLayeredArchiveCreateCleanupError("remove", removeErr),
			)
		}
		return file, nil
	}
	return nil, fmt.Errorf("create unique Windows layered archive temp file after %d attempts", windowsLayeredArchiveTempCreateAttempts)
}

func validateWindowsLayeredArchiveHandle(file *os.File) (windowsLayeredArchiveFileIdentity, string, error) {
	handle := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return windowsLayeredArchiveFileIdentity{}, "", fmt.Errorf("inspect Windows layered archive file identity: %w", err)
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return windowsLayeredArchiveFileIdentity{}, "", fmt.Errorf("inspect Windows layered archive handle security descriptor: %w", err)
	}
	protection, err := windowsLayeredArchiveProtectionFromDescriptor(descriptor)
	if err != nil {
		return windowsLayeredArchiveFileIdentity{}, "", err
	}
	if err := validateWindowsLayeredArchiveProtectionState(protection); err != nil {
		return windowsLayeredArchiveFileIdentity{}, "", err
	}
	runtime.KeepAlive(file)
	return windowsLayeredArchiveFileIdentity{
		volumeSerialNumber: info.VolumeSerialNumber,
		fileIndexHigh:      info.FileIndexHigh,
		fileIndexLow:       info.FileIndexLow,
	}, windowsLayeredArchivePrivacyDetail, nil
}

func sameWindowsLayeredArchiveFileIdentity(first, second windowsLayeredArchiveFileIdentity) bool {
	return first == second
}

func publishWindowsLayeredArchiveHandle(file *os.File, path string) (bool, error) {
	directory := filepath.Clean(filepath.Dir(path))
	directoryPointer, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		return false, fmt.Errorf("encode Windows layered archive publication directory: %w", err)
	}
	directoryHandle, err := windows.CreateFile(
		directoryPointer,
		windows.FILE_TRAVERSE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return false, fmt.Errorf("open Windows layered archive publication directory: %w", err)
	}
	name, err := windows.UTF16FromString(filepath.Base(path))
	if err != nil {
		closeErr := windows.CloseHandle(directoryHandle)
		return false, errors.Join(
			fmt.Errorf("encode Windows layered archive publication name: %w", err),
			wrapWindowsLayeredArchiveDirectoryCloseError(closeErr),
		)
	}
	fileNameLength := (len(name) - 1) * 2
	var layout windowsLayeredArchiveRenameInformation
	bufferSize := int(unsafe.Offsetof(layout.fileName)) + fileNameLength
	buffer := make([]byte, bufferSize)
	rename := (*windowsLayeredArchiveRenameInformation)(unsafe.Pointer(&buffer[0]))
	rename.rootDirectory = directoryHandle
	rename.fileNameLength = uint32(fileNameLength)
	copy(unsafe.Slice(&rename.fileName[0], fileNameLength/2), name[:len(name)-1])
	var status windows.IO_STATUS_BLOCK
	err = windows.NtSetInformationFile(
		windows.Handle(file.Fd()),
		&status,
		&buffer[0],
		uint32(len(buffer)),
		windows.FileRenameInformation,
	)
	runtime.KeepAlive(file)
	closeErr := windows.CloseHandle(directoryHandle)
	if err != nil {
		return false, errors.Join(
			fmt.Errorf("rename Windows layered archive by handle: %w", err),
			wrapWindowsLayeredArchiveDirectoryCloseError(closeErr),
		)
	}
	return true, wrapWindowsLayeredArchiveDirectoryCloseError(closeErr)
}

func wrapWindowsLayeredArchiveDirectoryCloseError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close Windows layered archive publication directory: %w", err)
}

func openPublishedWindowsLayeredArchive(path string) (*os.File, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode published Windows layered archive path: %w", err)
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.GENERIC_READ|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		closeErr := windows.CloseHandle(handle)
		return nil, errors.Join(fmt.Errorf("wrap published Windows layered archive validation handle"), wrapWindowsLayeredArchiveHandleCloseError(closeErr))
	}
	return file, nil
}

func wrapWindowsLayeredArchiveHandleCloseError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close protected Windows layered archive temp handle: %w", err)
}

func wrapWindowsLayeredArchiveCreateCleanupError(operation string, err error) error {
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("%s new Windows layered archive temp file: %w", operation, err)
}

func protectWindowsLayeredArchive(path string) (string, error) {
	currentUser, administrators, system, err := windowsLayeredArchiveTrustees()
	if err != nil {
		return "", err
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()
	for _, sid := range []*windows.SID{currentUser, administrators, system} {
		pinner.Pin(sid)
	}
	dacl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		windowsLayeredArchiveAccess(currentUser, windows.TRUSTEE_IS_USER),
		windowsLayeredArchiveAccess(system, windows.TRUSTEE_IS_USER),
		windowsLayeredArchiveAccess(administrators, windows.TRUSTEE_IS_GROUP),
	}, nil)
	if err != nil {
		return "", fmt.Errorf("create Windows layered archive DACL: %w", err)
	}

	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", fmt.Errorf("encode Windows layered archive path: %w", err)
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.READ_CONTROL|windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return "", fmt.Errorf("open Windows layered archive security descriptor: %w", err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		return "", fmt.Errorf("set Windows layered archive DACL: %w", err)
	}
	return validateWindowsLayeredArchiveProtection(path)
}

func windowsLayeredArchiveAccess(sid *windows.SID, trusteeType windows.TRUSTEE_TYPE) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  trusteeType,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}

func windowsLayeredArchiveTrustees() (currentUser, administrators, system *windows.SID, err error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open current Windows process token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read current Windows process user: %w", err)
	}
	if user == nil || user.User.Sid == nil || !user.User.Sid.IsValid() {
		return nil, nil, nil, fmt.Errorf("read current Windows process user: invalid SID")
	}
	currentUser, err = user.User.Sid.Copy()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("copy current Windows process user SID: %w", err)
	}
	administrators, err = windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create Windows Administrators SID: %w", err)
	}
	system, err = windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create Windows SYSTEM SID: %w", err)
	}
	return currentUser, administrators, system, nil
}

func validateWindowsLayeredArchiveProtection(path string) (string, error) {
	protection, err := inspectWindowsLayeredArchiveProtection(path)
	if err != nil {
		return "", err
	}
	if err := validateWindowsLayeredArchiveProtectionState(protection); err != nil {
		return "", err
	}
	return windowsLayeredArchivePrivacyDetail, nil
}

func inspectWindowsLayeredArchiveProtection(path string) (windowsLayeredArchiveProtection, error) {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return windowsLayeredArchiveProtection{}, fmt.Errorf("inspect Windows layered archive security descriptor: %w", err)
	}
	return windowsLayeredArchiveProtectionFromDescriptor(descriptor)
}

func windowsLayeredArchiveProtectionFromDescriptor(descriptor *windows.SECURITY_DESCRIPTOR) (windowsLayeredArchiveProtection, error) {
	if descriptor == nil || !descriptor.IsValid() {
		return windowsLayeredArchiveProtection{}, fmt.Errorf("inspect Windows layered archive security descriptor: invalid descriptor")
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return windowsLayeredArchiveProtection{}, fmt.Errorf("inspect Windows layered archive DACL control: %w", err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return windowsLayeredArchiveProtection{}, fmt.Errorf("inspect Windows layered archive owner: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return windowsLayeredArchiveProtection{}, fmt.Errorf("inspect Windows layered archive DACL: %w", err)
	}
	protection := windowsLayeredArchiveProtection{
		ownerSID:      owner.String(),
		daclPresent:   control&windows.SE_DACL_PRESENT != 0,
		daclProtected: control&windows.SE_DACL_PROTECTED != 0,
		daclNull:      dacl == nil,
	}
	if dacl == nil {
		return protection, nil
	}
	protection.aces = make([]windowsLayeredArchiveACE, 0, dacl.AceCount)
	for index := uint16(0); index < dacl.AceCount; index++ {
		ace, err := readWindowsLayeredArchiveACE(dacl, uint32(index))
		if err != nil {
			return windowsLayeredArchiveProtection{}, err
		}
		protection.aces = append(protection.aces, ace)
	}
	return protection, nil
}

func readWindowsLayeredArchiveACE(dacl *windows.ACL, index uint32) (windowsLayeredArchiveACE, error) {
	var raw *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, index, &raw); err != nil {
		return windowsLayeredArchiveACE{}, fmt.Errorf("read Windows layered archive ACE %d: %w", index, err)
	}
	if raw == nil {
		return windowsLayeredArchiveACE{}, fmt.Errorf("read Windows layered archive ACE %d: nil ACE", index)
	}
	sidOffset := unsafe.Offsetof(raw.SidStart)
	aceSize := uintptr(raw.Header.AceSize)
	if aceSize < sidOffset+8 {
		return windowsLayeredArchiveACE{}, fmt.Errorf("read Windows layered archive ACE %d: malformed SID", index)
	}
	sid := (*windows.SID)(unsafe.Pointer(&raw.SidStart))
	if !sid.IsValid() || uintptr(sid.Len()) > aceSize-sidOffset {
		return windowsLayeredArchiveACE{}, fmt.Errorf("read Windows layered archive ACE %d: invalid SID", index)
	}
	return windowsLayeredArchiveACE{
		typeID: raw.Header.AceType,
		flags:  raw.Header.AceFlags,
		mask:   uint32(raw.Mask),
		sid:    sid.String(),
	}, nil
}

func validateWindowsLayeredArchiveProtectionState(protection windowsLayeredArchiveProtection) error {
	if !protection.daclPresent || protection.daclNull || !protection.daclProtected {
		return fmt.Errorf("Windows layered archive DACL must be present, non-null, and protected")
	}
	currentUser, administrators, system, err := windowsLayeredArchiveTrustees()
	if err != nil {
		return err
	}
	trusted := map[string]struct{}{
		currentUser.String():    {},
		administrators.String(): {},
		system.String():         {},
	}
	if _, ok := trusted[protection.ownerSID]; !ok {
		return fmt.Errorf("Windows layered archive owner is not trusted")
	}
	if len(protection.aces) != len(trusted) {
		return fmt.Errorf("Windows layered archive DACL must contain exactly three ACEs")
	}
	seen := make(map[string]struct{}, len(protection.aces))
	for _, ace := range protection.aces {
		if ace.typeID != windows.ACCESS_ALLOWED_ACE_TYPE || ace.flags != 0 || ace.mask != windowsLayeredArchiveFullControlMask {
			return fmt.Errorf("Windows layered archive DACL contains an unsupported ACE")
		}
		if _, ok := trusted[ace.sid]; !ok {
			return fmt.Errorf("Windows layered archive DACL grants an untrusted SID")
		}
		if _, ok := seen[ace.sid]; ok {
			return fmt.Errorf("Windows layered archive DACL repeats a trusted SID")
		}
		seen[ace.sid] = struct{}{}
	}
	return nil
}
