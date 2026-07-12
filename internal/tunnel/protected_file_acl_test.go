package tunnel

import (
	"strings"
	"testing"
)

func TestValidateWindowsProtectedFileACLAcceptsTrustedWriters(t *testing.T) {
	acl := windowsProtectedFileACL{
		OwnerSID:         "S-1-5-21-1000",
		CurrentUserSID:   "S-1-5-21-1000",
		AdministratorSID: "S-1-5-32-544",
		SystemSID:        "S-1-5-18",
		DACLPresent:      true,
		ACEs: []windowsProtectedFileACE{
			{Type: windowsACEAllowed, SID: "S-1-1-0", Mask: windowsFileReadData},
			{Type: windowsACEAllowed, Flags: windowsACEInheritOnly, SID: "S-1-1-0", Mask: windowsGenericWrite},
			{Type: windowsACEAllowed, SID: "S-1-5-21-1000", Mask: windowsFileWriteData},
			{Type: windowsACEAllowed, SID: "S-1-5-32-544", Mask: windowsWriteDAC},
			{Type: windowsACEAllowed, SID: "S-1-5-18", Mask: windowsGenericAll},
			{Type: windowsACEDenied, SID: "S-1-1-0", Mask: windowsGenericWrite},
		},
	}

	if err := validateWindowsProtectedFileACL(acl); err != nil {
		t.Fatalf("trusted Windows ACL rejected: %v", err)
	}
}

func TestValidateWindowsProtectedFileACLRejectsUnsafeMetadata(t *testing.T) {
	base := windowsProtectedFileACL{
		OwnerSID:         "S-1-5-21-1000",
		CurrentUserSID:   "S-1-5-21-1000",
		AdministratorSID: "S-1-5-32-544",
		SystemSID:        "S-1-5-18",
		DACLPresent:      true,
		ACEs:             []windowsProtectedFileACE{{Type: windowsACEAllowed, SID: "S-1-5-21-1000", Mask: windowsFileWriteData}},
	}

	tests := []struct {
		name   string
		mutate func(*windowsProtectedFileACL)
		want   string
	}{
		{name: "missing DACL", mutate: func(acl *windowsProtectedFileACL) { acl.DACLPresent = false }, want: "DACL"},
		{name: "null DACL", mutate: func(acl *windowsProtectedFileACL) { acl.DACLNull = true }, want: "DACL"},
		{name: "missing current user", mutate: func(acl *windowsProtectedFileACL) { acl.CurrentUserSID = "" }, want: "trusted SID"},
		{name: "untrusted owner", mutate: func(acl *windowsProtectedFileACL) { acl.OwnerSID = "S-1-5-32-545" }, want: "owner"},
		{name: "untrusted writer", mutate: func(acl *windowsProtectedFileACL) { acl.ACEs[0].SID = "S-1-1-0" }, want: "write access"},
		{name: "generic writer", mutate: func(acl *windowsProtectedFileACL) {
			acl.ACEs[0] = windowsProtectedFileACE{Type: windowsACEAllowed, SID: "S-1-5-11", Mask: windowsGenericWrite}
		}, want: "write access"},
		{name: "delete writer", mutate: func(acl *windowsProtectedFileACL) {
			acl.ACEs[0] = windowsProtectedFileACE{Type: windowsACEAllowed, SID: "S-1-5-32-545", Mask: windowsDelete}
		}, want: "write access"},
		{name: "maximum allowed writer", mutate: func(acl *windowsProtectedFileACL) {
			acl.ACEs[0] = windowsProtectedFileACE{Type: windowsACEAllowed, SID: "S-1-5-32-545", Mask: windowsMaximumAllowed}
		}, want: "write access"},
		{name: "unknown ACE", mutate: func(acl *windowsProtectedFileACL) { acl.ACEs[0].Type = windowsACEUnknown }, want: "unsupported ACE"},
		{name: "unknown inherit-only ACE", mutate: func(acl *windowsProtectedFileACL) {
			acl.ACEs[0] = windowsProtectedFileACE{Type: windowsACEUnknown, Flags: windowsACEInheritOnly, SID: "S-1-1-0", Mask: windowsGenericWrite}
		}, want: "unsupported ACE"},
		{name: "malformed SID", mutate: func(acl *windowsProtectedFileACL) { acl.ACEs[0].SID = "" }, want: "SID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acl := base
			acl.ACEs = append([]windowsProtectedFileACE(nil), base.ACEs...)
			tt.mutate(&acl)
			err := validateWindowsProtectedFileACL(acl)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestValidateWindowsConfidentialPathACLRejectsUntrustedReaders(t *testing.T) {
	acl := windowsProtectedFileACL{
		OwnerSID: "S-1-5-21-1000", CurrentUserSID: "S-1-5-21-1000",
		AdministratorSID: "S-1-5-32-544", SystemSID: "S-1-5-18", DACLPresent: true,
		ACEs: []windowsProtectedFileACE{{Type: windowsACEAllowed, SID: "S-1-1-0", Mask: windowsGenericRead}},
	}
	if err := validateWindowsConfidentialPathACL(acl); err == nil {
		t.Fatal("confidential path ACL accepted untrusted reader")
	}
	acl.ACEs[0].SID = acl.CurrentUserSID
	if err := validateWindowsConfidentialPathACL(acl); err != nil {
		t.Fatalf("confidential path ACL rejected trusted reader: %v", err)
	}
}

func TestValidateWindowsProtectedAncestorACLRejectsReplacementRights(t *testing.T) {
	base := windowsProtectedFileACL{
		OwnerSID: "S-1-5-21-1000", CurrentUserSID: "S-1-5-21-1000",
		AdministratorSID: "S-1-5-32-544", SystemSID: "S-1-5-18", DACLPresent: true,
		ACEs: []windowsProtectedFileACE{{Type: windowsACEAllowed, SID: "S-1-1-0", Mask: windowsGenericRead}},
	}
	if err := validateWindowsProtectedAncestorACL(base); err != nil {
		t.Fatalf("read-only ancestor ACL rejected: %v", err)
	}
	addOnly := base
	addOnly.ACEs = []windowsProtectedFileACE{{
		Type: windowsACEAllowed, SID: "S-1-1-0", Mask: windowsFileWriteData | windowsFileAppendData,
	}}
	if err := validateWindowsProtectedAncestorACL(addOnly); err != nil {
		t.Fatalf("add-only ancestor ACL rejected: %v", err)
	}
	trustedInstaller := base
	trustedInstaller.OwnerSID = "S-1-5-80-trusted-installer"
	trustedInstaller.TrustedOwnerSIDs = []string{"S-1-5-80-trusted-installer"}
	trustedInstaller.ACEs = []windowsProtectedFileACE{{
		Type: windowsACEAllowed, SID: "S-1-5-80-trusted-installer", Mask: windowsGenericAll,
	}}
	if err := validateWindowsProtectedAncestorACL(trustedInstaller); err != nil {
		t.Fatalf("TrustedInstaller ancestor ACL rejected: %v", err)
	}
	for name, mask := range map[string]uint32{
		"delete child":  windowsFileDeleteChild,
		"delete":        windowsDelete,
		"write DACL":    windowsWriteDAC,
		"write owner":   windowsWriteOwner,
		"generic write": windowsGenericWrite,
	} {
		t.Run(name, func(t *testing.T) {
			acl := base
			acl.ACEs = []windowsProtectedFileACE{{Type: windowsACEAllowed, SID: "S-1-1-0", Mask: mask}}
			if err := validateWindowsProtectedAncestorACL(acl); err == nil {
				t.Fatalf("untrusted ancestor right %#x accepted", mask)
			}
		})
	}
}

func TestWindowsDriveTypeClassification(t *testing.T) {
	for driveType, want := range map[uint32]bool{
		0: false, // DRIVE_UNKNOWN
		1: false, // DRIVE_NO_ROOT_DIR
		2: true,  // DRIVE_REMOVABLE
		3: true,  // DRIVE_FIXED
		4: false, // DRIVE_REMOTE
		5: true,  // DRIVE_CDROM
		6: true,  // DRIVE_RAMDISK
	} {
		if got := windowsDriveTypeIsLocal(driveType); got != want {
			t.Errorf("windowsDriveTypeIsLocal(%d) = %t, want %t", driveType, got, want)
		}
	}
}

func TestWindowsFinalPathRemoteClassification(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: `\\?\C:\Users\Alice\policy.json`, want: false},
		{path: `\\?\UNC\server\share\policy.json`, want: true},
		{path: `\\?\GLOBALROOT\Device\Mup\server\share\policy.json`, want: true},
		{path: `\\?\GLOBALROOT\Device\LanmanRedirector\server\share\policy.json`, want: true},
		{path: `\\?\GLOBALROOT\Device\WebDavRedirector\server\share\policy.json`, want: true},
		{path: `\\server\share\policy.json`, want: true},
	}
	for _, tt := range tests {
		if got := windowsFinalPathIsRemote(tt.path); got != tt.want {
			t.Errorf("windowsFinalPathIsRemote(%q) = %t, want %t", tt.path, got, tt.want)
		}
	}
}

func TestValidateWindowsACESIDBounds(t *testing.T) {
	tests := []struct {
		name      string
		aceSize   uintptr
		sidOffset uintptr
		sidLength uintptr
		wantError bool
	}{
		{name: "minimum valid SID", aceSize: 16, sidOffset: 8, sidLength: 8},
		{name: "ACE shorter than SID header", aceSize: 15, sidOffset: 8, sidLength: 8, wantError: true},
		{name: "SID shorter than header", aceSize: 16, sidOffset: 8, sidLength: 4, wantError: true},
		{name: "SID extends beyond ACE", aceSize: 20, sidOffset: 8, sidLength: 16, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWindowsACESIDBounds(tt.aceSize, tt.sidOffset, tt.sidLength)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateWindowsACESIDBounds() error = %v, wantError %t", err, tt.wantError)
			}
		})
	}
}
