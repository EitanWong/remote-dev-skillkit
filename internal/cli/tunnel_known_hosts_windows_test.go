//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func protectKnownHostsTestFile(t *testing.T, path string, _ os.FileMode) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()
	pinner.Pin(user.User.Sid)
	dacl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetSecurityInfo(
		windows.Handle(file.Fd()),
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		t.Fatal(err)
	}
}

func TestValidateKnownHostsFileRejectsUntrustedWindowsParentReplacement(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "pins")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "known_hosts")
	if err := os.WriteFile(path, []byte("localhost.run ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	protectKnownHostsTestFile(t, path, 0o600)
	grantWindowsDirectoryReplacement(t, parent)

	err := validateKnownHostsFile(path, "localhost.run", 22)
	if err == nil || !strings.Contains(err.Error(), "replacement access") {
		t.Fatalf("expected untrusted parent replacement rejection, got %v", err)
	}
}

func TestValidateKnownHostsFileAcceptsWindowsCaseNormalizedPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "known_hosts")
	if err := os.WriteFile(path, []byte("localhost.run ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	protectKnownHostsTestFile(t, path, 0o600)
	if err := validateKnownHostsFile(strings.ToUpper(path), "localhost.run", 22); err != nil {
		t.Fatalf("Windows case-normalized known_hosts path rejected: %v", err)
	}
}

func grantWindowsDirectoryReplacement(t *testing.T, path string) {
	t.Helper()
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.WRITE_DAC|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)

	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()
	pinner.Pin(user.User.Sid)
	pinner.Pin(everyone)
	dacl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
			},
		},
		{
			AccessPermissions: 0x00000040, // FILE_DELETE_CHILD
			AccessMode:        windows.GRANT_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeValue: windows.TrusteeValueFromSID(everyone),
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		t.Fatal(err)
	}
}
