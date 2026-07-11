//go:build windows

package tunnel

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func validateProtectedPathPermissions(file *os.File, info os.FileInfo, _ bool) error {
	acl, err := protectedWindowsPathACL(file)
	if err != nil {
		return err
	}
	return validateWindowsConfidentialPathACL(acl)
}

func validateProtectedWindowsAncestorPermissions(file *os.File) error {
	acl, err := protectedWindowsPathACL(file)
	if err != nil {
		return err
	}
	return validateWindowsProtectedAncestorACL(acl)
}

func protectedWindowsPathACL(file *os.File) (windowsProtectedFileACL, error) {
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return windowsProtectedFileACL{}, err
	}
	if descriptor == nil || !descriptor.IsValid() {
		return windowsProtectedFileACL{}, fmt.Errorf("inspect protected path Windows security descriptor: invalid descriptor")
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return windowsProtectedFileACL{}, fmt.Errorf("inspect protected path Windows owner: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return windowsProtectedFileACL{}, err
	}
	currentUser, administrator, system, err := protectedWindowsTrustees()
	if err != nil {
		return windowsProtectedFileACL{}, err
	}
	acl := windowsProtectedFileACL{
		OwnerSID: owner.String(), CurrentUserSID: currentUser, AdministratorSID: administrator, SystemSID: system,
		DACLPresent: true, DACLNull: dacl == nil, TrustedOwnerSIDs: protectedWindowsTrustedOwnerSIDs(),
	}
	if dacl != nil {
		for index := uint16(0); index < dacl.AceCount; index++ {
			ace, err := readWindowsProtectedFileACE(dacl, uint32(index))
			if err != nil {
				return windowsProtectedFileACL{}, err
			}
			acl.ACEs = append(acl.ACEs, ace)
		}
	}
	return acl, nil
}

func protectedWindowsTrustedOwnerSIDs() []string {
	sid, _, _, err := windows.LookupSID("", `NT SERVICE\TrustedInstaller`)
	if err != nil || sid == nil || !sid.IsValid() {
		return nil
	}
	return []string{sid.String()}
}
