//go:build windows

package tunnel

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadProtectedJSONFileAcceptsSafeWindowsDACL(t *testing.T) {
	path := writeWindowsProtectedJSON(t, false)
	var got protectedJSONFixture
	if err := ReadProtectedJSONFile(path, &got); err != nil {
		t.Fatalf("safe Windows DACL rejected: %v", err)
	}
	if got.Name != "safe" {
		t.Fatalf("unexpected decoded value: %#v", got)
	}
}

func TestReadProtectedJSONFileRejectsUntrustedWindowsWriter(t *testing.T) {
	path := writeWindowsProtectedJSON(t, true)
	var got protectedJSONFixture
	err := ReadProtectedJSONFile(path, &got)
	if err == nil || !strings.Contains(err.Error(), "write access") {
		t.Fatalf("expected untrusted writer rejection, got %v", err)
	}
}

func writeWindowsProtectedJSON(t *testing.T, grantEveryoneWrite bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(`{"name":"safe"}`); err != nil {
		t.Fatal(err)
	}

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
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}
	if grantEveryoneWrite {
		everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
		if err != nil {
			t.Fatal(err)
		}
		pinner.Pin(everyone)
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_WRITE,
			AccessMode:        windows.GRANT_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeValue: windows.TrusteeValueFromSID(everyone),
			},
		})
	}
	dacl, err := windows.ACLFromEntries(entries, nil)
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
	return path
}
