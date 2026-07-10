//go:build windows

package tunnel

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func validateProtectedJSONPermissions(file *os.File, _ os.FileInfo) error {
	descriptor, err := windows.GetSecurityInfo(
		windows.Handle(file.Fd()),
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect protected JSON Windows security descriptor: %w", err)
	}
	if descriptor == nil || !descriptor.IsValid() {
		return fmt.Errorf("inspect protected JSON Windows security descriptor: invalid descriptor")
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("inspect protected JSON Windows owner: %w", err)
	}
	if owner == nil || !owner.IsValid() {
		return fmt.Errorf("inspect protected JSON Windows owner: invalid owner SID")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("protected JSON Windows DACL is missing: %w", err)
	}

	currentUser, administrator, system, err := protectedWindowsTrustees()
	if err != nil {
		return err
	}
	acl := windowsProtectedFileACL{
		OwnerSID:         owner.String(),
		CurrentUserSID:   currentUser,
		AdministratorSID: administrator,
		SystemSID:        system,
		DACLPresent:      true,
		DACLNull:         dacl == nil,
	}
	if dacl != nil {
		acl.ACEs = make([]windowsProtectedFileACE, 0, dacl.AceCount)
		for index := uint16(0); index < dacl.AceCount; index++ {
			ace, err := readWindowsProtectedFileACE(dacl, uint32(index))
			if err != nil {
				return err
			}
			acl.ACEs = append(acl.ACEs, ace)
		}
	}
	return validateWindowsProtectedFileACL(acl)
}

func protectedWindowsTrustees() (currentUser string, administrator string, system string, err error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return "", "", "", fmt.Errorf("open current Windows process token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", "", "", fmt.Errorf("read current Windows process user: %w", err)
	}
	if user == nil || user.User.Sid == nil || !user.User.Sid.IsValid() {
		return "", "", "", fmt.Errorf("read current Windows process user: invalid user SID")
	}
	administratorSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return "", "", "", fmt.Errorf("create Windows Administrators SID: %w", err)
	}
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return "", "", "", fmt.Errorf("create Windows SYSTEM SID: %w", err)
	}
	currentUser = user.User.Sid.String()
	administrator = administratorSID.String()
	system = systemSID.String()
	if currentUser == "" || administrator == "" || system == "" {
		return "", "", "", fmt.Errorf("read protected JSON Windows trustee SID strings")
	}
	return currentUser, administrator, system, nil
}

func readWindowsProtectedFileACE(dacl *windows.ACL, index uint32) (windowsProtectedFileACE, error) {
	var raw *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, index, &raw); err != nil {
		return windowsProtectedFileACE{}, fmt.Errorf("read protected JSON Windows ACE %d: %w", index, err)
	}
	if raw == nil {
		return windowsProtectedFileACE{}, fmt.Errorf("read protected JSON Windows ACE %d: nil ACE", index)
	}
	var aceType windowsACEType
	switch raw.Header.AceType {
	case windows.ACCESS_ALLOWED_ACE_TYPE:
		aceType = windowsACEAllowed
	case windows.ACCESS_DENIED_ACE_TYPE:
		aceType = windowsACEDenied
	default:
		return windowsProtectedFileACE{}, fmt.Errorf("protected JSON Windows DACL contains an unsupported ACE type %d", raw.Header.AceType)
	}
	sidOffset := unsafe.Offsetof(raw.SidStart)
	const minimumSIDBytes = 8
	if err := validateWindowsACESIDBounds(uintptr(raw.Header.AceSize), sidOffset, minimumSIDBytes); err != nil {
		return windowsProtectedFileACE{}, fmt.Errorf("protected JSON Windows ACE %d is malformed", index)
	}
	sid := (*windows.SID)(unsafe.Pointer(&raw.SidStart))
	if !sid.IsValid() {
		return windowsProtectedFileACE{}, fmt.Errorf("protected JSON Windows ACE %d has an invalid SID", index)
	}
	if err := validateWindowsACESIDBounds(uintptr(raw.Header.AceSize), sidOffset, uintptr(sid.Len())); err != nil {
		return windowsProtectedFileACE{}, fmt.Errorf("protected JSON Windows ACE %d has an invalid SID", index)
	}
	return windowsProtectedFileACE{
		Type:  aceType,
		Flags: raw.Header.AceFlags,
		SID:   sid.String(),
		Mask:  uint32(raw.Mask),
	}, nil
}
