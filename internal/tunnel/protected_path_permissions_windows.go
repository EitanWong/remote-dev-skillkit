//go:build windows

package tunnel

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func validateProtectedPathPermissions(file *os.File, info os.FileInfo, _ bool) error {
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if descriptor == nil || !descriptor.IsValid() {
		return fmt.Errorf("inspect protected path Windows security descriptor: invalid descriptor")
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return fmt.Errorf("inspect protected path Windows owner: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	currentUser, administrator, system, err := protectedWindowsTrustees()
	if err != nil {
		return err
	}
	acl := windowsProtectedFileACL{
		OwnerSID: owner.String(), CurrentUserSID: currentUser, AdministratorSID: administrator, SystemSID: system,
		DACLPresent: true, DACLNull: dacl == nil,
	}
	if dacl != nil {
		for index := uint16(0); index < dacl.AceCount; index++ {
			ace, err := readWindowsProtectedFileACE(dacl, uint32(index))
			if err != nil {
				return err
			}
			acl.ACEs = append(acl.ACEs, ace)
		}
	}
	return validateWindowsConfidentialPathACL(acl)
}
