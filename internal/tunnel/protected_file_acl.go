package tunnel

import (
	"fmt"
	"strings"
)

type windowsACEType uint8

const (
	windowsACEUnknown windowsACEType = iota
	windowsACEAllowed
	windowsACEDenied
)

const windowsACEInheritOnly uint8 = 0x08

const (
	windowsFileReadData       uint32 = 0x00000001
	windowsFileWriteData      uint32 = 0x00000002
	windowsFileAppendData     uint32 = 0x00000004
	windowsFileWriteEA        uint32 = 0x00000010
	windowsFileDeleteChild    uint32 = 0x00000040
	windowsFileWriteAttribute uint32 = 0x00000100
	windowsFileReadEA         uint32 = 0x00000008
	windowsFileExecute        uint32 = 0x00000020
	windowsFileReadAttribute  uint32 = 0x00000080
	windowsReadControl        uint32 = 0x00020000
	windowsDelete             uint32 = 0x00010000
	windowsWriteDAC           uint32 = 0x00040000
	windowsWriteOwner         uint32 = 0x00080000
	windowsMaximumAllowed     uint32 = 0x02000000
	windowsGenericAll         uint32 = 0x10000000
	windowsGenericWrite       uint32 = 0x40000000
	windowsGenericExecute     uint32 = 0x20000000
	windowsGenericRead        uint32 = 0x80000000
)

const windowsProtectedWriteMask = windowsFileWriteData |
	windowsFileAppendData |
	windowsFileWriteEA |
	windowsFileDeleteChild |
	windowsFileWriteAttribute |
	windowsDelete |
	windowsWriteDAC |
	windowsWriteOwner |
	windowsMaximumAllowed |
	windowsGenericAll |
	windowsGenericWrite

const windowsProtectedAncestorReplacementMask = windowsFileWriteEA |
	windowsFileDeleteChild |
	windowsFileWriteAttribute |
	windowsDelete |
	windowsWriteDAC |
	windowsWriteOwner |
	windowsMaximumAllowed |
	windowsGenericAll |
	windowsGenericWrite

const windowsProtectedReadMask = windowsFileReadData |
	windowsFileReadEA |
	windowsFileExecute |
	windowsFileReadAttribute |
	windowsReadControl |
	windowsMaximumAllowed |
	windowsGenericAll |
	windowsGenericExecute |
	windowsGenericRead

type windowsProtectedFileACE struct {
	Type  windowsACEType
	Flags uint8
	SID   string
	Mask  uint32
}

func validateWindowsConfidentialPathACL(acl windowsProtectedFileACL) error {
	if err := validateWindowsProtectedFileACL(acl); err != nil {
		return err
	}
	trusted := map[string]struct{}{acl.CurrentUserSID: {}, acl.AdministratorSID: {}, acl.SystemSID: {}}
	for _, ace := range acl.ACEs {
		if ace.Flags&windowsACEInheritOnly != 0 || ace.Type != windowsACEAllowed || ace.Mask&windowsProtectedReadMask == 0 {
			continue
		}
		if _, ok := trusted[ace.SID]; !ok {
			return fmt.Errorf("protected path Windows DACL grants read access to an untrusted SID")
		}
	}
	return nil
}

func validateWindowsProtectedAncestorACL(acl windowsProtectedFileACL) error {
	if !acl.DACLPresent || acl.DACLNull {
		return fmt.Errorf("protected path ancestor Windows DACL must be present and non-null")
	}
	trusted := map[string]struct{}{
		acl.CurrentUserSID:   {},
		acl.AdministratorSID: {},
		acl.SystemSID:        {},
	}
	for _, sid := range acl.TrustedOwnerSIDs {
		if sid != "" {
			trusted[sid] = struct{}{}
		}
	}
	if acl.CurrentUserSID == "" || acl.AdministratorSID == "" || acl.SystemSID == "" {
		return fmt.Errorf("protected path ancestor Windows trusted SID is missing")
	}
	if _, ok := trusted[acl.OwnerSID]; acl.OwnerSID == "" || !ok {
		return fmt.Errorf("protected path ancestor Windows owner is not trusted")
	}
	for _, ace := range acl.ACEs {
		if ace.Type != windowsACEAllowed && ace.Type != windowsACEDenied {
			return fmt.Errorf("protected path ancestor Windows DACL contains an unsupported ACE")
		}
		if ace.SID == "" {
			return fmt.Errorf("protected path ancestor Windows ACE SID is missing")
		}
		if ace.Flags&windowsACEInheritOnly != 0 || ace.Type != windowsACEAllowed || ace.Mask&windowsProtectedAncestorReplacementMask == 0 {
			continue
		}
		if _, ok := trusted[ace.SID]; !ok {
			return fmt.Errorf("protected path ancestor Windows DACL grants replacement access to an untrusted SID")
		}
	}
	return nil
}

type windowsProtectedFileACL struct {
	OwnerSID         string
	CurrentUserSID   string
	AdministratorSID string
	SystemSID        string
	DACLPresent      bool
	DACLNull         bool
	ACEs             []windowsProtectedFileACE
	TrustedOwnerSIDs []string
}

func windowsDriveTypeIsLocal(driveType uint32) bool {
	return driveType >= 2 && driveType <= 6 && driveType != 4
}

func validateWindowsProtectedFileACL(acl windowsProtectedFileACL) error {
	if !acl.DACLPresent || acl.DACLNull {
		return fmt.Errorf("protected JSON Windows DACL must be present and non-null")
	}
	if acl.CurrentUserSID == "" || acl.AdministratorSID == "" || acl.SystemSID == "" {
		return fmt.Errorf("protected JSON Windows trusted SID is missing")
	}
	trusted := map[string]struct{}{
		acl.CurrentUserSID:   {},
		acl.AdministratorSID: {},
		acl.SystemSID:        {},
	}
	if acl.OwnerSID == "" {
		return fmt.Errorf("protected JSON Windows owner SID is missing")
	}
	if _, ok := trusted[acl.OwnerSID]; !ok {
		return fmt.Errorf("protected JSON Windows owner is not trusted")
	}
	for _, ace := range acl.ACEs {
		if ace.Type != windowsACEAllowed && ace.Type != windowsACEDenied {
			return fmt.Errorf("protected JSON Windows DACL contains an unsupported ACE")
		}
		if ace.SID == "" {
			return fmt.Errorf("protected JSON Windows ACE SID is missing")
		}
		if ace.Flags&windowsACEInheritOnly != 0 {
			continue
		}
		if ace.Type != windowsACEAllowed || ace.Mask&windowsProtectedWriteMask == 0 {
			continue
		}
		if _, ok := trusted[ace.SID]; !ok {
			return fmt.Errorf("protected JSON Windows DACL grants write access to an untrusted SID")
		}
	}
	return nil
}

func windowsFinalPathIsRemote(path string) bool {
	upperPath := strings.ToUpper(path)
	return strings.HasPrefix(upperPath, `\\?\UNC\`) ||
		strings.HasPrefix(upperPath, `\\?\GLOBALROOT\`) ||
		(strings.HasPrefix(upperPath, `\\`) && !strings.HasPrefix(upperPath, `\\?\`))
}

func validateWindowsACESIDBounds(aceSize, sidOffset, sidLength uintptr) error {
	const minimumSIDBytes = 8
	if sidLength < minimumSIDBytes || aceSize < sidOffset+minimumSIDBytes || sidLength > aceSize-sidOffset {
		return fmt.Errorf("Windows ACE SID extends beyond the ACE boundary")
	}
	return nil
}
