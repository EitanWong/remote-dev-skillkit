//go:build windows

package tunnel

import (
	"crypto/sha256"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

type windowsTestACLGrant struct {
	SIDType           windows.WELL_KNOWN_SID_TYPE
	AccessPermissions windows.ACCESS_MASK
}

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

func TestValidateWindowsLocalVolumePathAcceptsLocalTemp(t *testing.T) {
	if err := validateWindowsLocalVolumePath(t.TempDir()); err != nil {
		t.Fatalf("local Windows temp volume rejected: %v", err)
	}
}

func TestVerifyProtectedRegularFileSHA256AcceptsSafeWindowsDACL(t *testing.T) {
	content := []byte("reviewed Windows artifact")
	path := writeWindowsProtectedPayload(t, content, nil)
	want := sha256.Sum256(content)

	if err := VerifyProtectedRegularFileSHA256(path, 1024, want); err != nil {
		t.Fatalf("safe confidential Windows DACL rejected: %v", err)
	}
	opened, err := OpenVerifiedProtectedRegularFileSHA256(strings.ToUpper(path), 1024, want)
	if err != nil {
		t.Fatalf("case-normalized protected path rejected: %v", err)
	}
	got, readErr := io.ReadAll(opened)
	closeErr := opened.Close()
	if readErr != nil || closeErr != nil || string(got) != string(content) {
		t.Fatalf("rewound verified handle content = %q, readErr = %v, closeErr = %v", got, readErr, closeErr)
	}

	wrong := sha256.Sum256([]byte("wrong digest"))
	if err := VerifyProtectedRegularFileSHA256(path, 1024, wrong); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("wrong digest error = %v", err)
	}
}

func TestVerifyProtectedRegularFileSHA256RejectsUntrustedWindowsAccess(t *testing.T) {
	content := []byte("confidential")
	want := sha256.Sum256(content)
	tests := []struct {
		name       string
		permission windows.ACCESS_MASK
		wantError  string
	}{
		{name: "write", permission: windows.GENERIC_WRITE, wantError: "write access"},
		{name: "read", permission: windows.GENERIC_READ, wantError: "read access"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeWindowsProtectedPayload(t, content, []windowsTestACLGrant{{
				SIDType: windows.WinWorldSid, AccessPermissions: tt.permission,
			}})
			err := VerifyProtectedRegularFileSHA256(path, 1024, want)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected untrusted %s rejection, got %v", tt.name, err)
			}
		})
	}
}

func TestValidateProtectedParentChainRejectsUntrustedWindowsReplacement(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "protected-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "asset")
	if err := os.WriteFile(path, []byte("confidential"), 0o600); err != nil {
		t.Fatal(err)
	}
	setWindowsProtectedPathDACL(t, path, false, nil)
	setWindowsProtectedPathDACL(t, parent, true, []windowsTestACLGrant{{
		SIDType: windows.WinWorldSid, AccessPermissions: windows.ACCESS_MASK(windowsFileDeleteChild),
	}})

	err := ValidateProtectedParentChain(path)
	if err == nil || !strings.Contains(err.Error(), "replacement access") {
		t.Fatalf("expected untrusted parent replacement rejection, got %v", err)
	}
}

func TestOpenVerifiedProtectedRegularFileRejectsWindowsJunction(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("junction target")
	path := filepath.Join(target, "asset")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	setWindowsProtectedPathDACL(t, path, false, nil)

	junction := filepath.Join(root, "junction")
	if err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, target).Run(); err != nil {
		t.Fatalf("Windows junction fixture unavailable; runtime reparse evidence is blocked")
	}
	junctionPath := filepath.Join(junction, "asset")
	want := sha256.Sum256(content)
	if _, err := OpenVerifiedProtectedRegularFileSHA256(junctionPath, 1024, want); err == nil || !strings.Contains(err.Error(), "reparse") {
		t.Fatalf("Windows junction path was not rejected as a reparse traversal: %v", err)
	}
}

func TestWindowsRemotePathClassificationRejectsUNC(t *testing.T) {
	for _, path := range []string{
		`\\server\share\known_hosts`,
		`\\?\UNC\server\share\known_hosts`,
		`\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1\known_hosts`,
	} {
		if !windowsFinalPathIsRemote(path) {
			t.Fatalf("remote Windows path classified as local: %q", path)
		}
	}
	if windowsFinalPathIsRemote(`\\?\C:\Users\Administrator\known_hosts`) {
		t.Fatal("local Windows device path classified as remote")
	}
}

func writeWindowsProtectedPayload(t *testing.T, content []byte, grants []windowsTestACLGrant) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "asset")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	setWindowsProtectedPathDACL(t, path, false, grants)
	return path
}

func setWindowsProtectedPathDACL(t *testing.T, path string, directory bool, grants []windowsTestACLGrant) {
	t.Helper()
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	if directory {
		flags = windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.WRITE_DAC|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		flags,
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
	for _, grant := range grants {
		sid, err := windows.CreateWellKnownSid(grant.SIDType)
		if err != nil {
			t.Fatal(err)
		}
		pinner.Pin(sid)
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: grant.AccessPermissions,
			AccessMode:        windows.GRANT_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	dacl, err := windows.ACLFromEntries(entries, nil)
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

func writeWindowsProtectedJSON(t *testing.T, grantEveryoneWrite bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"name":"safe"}`); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	grants := []windowsTestACLGrant(nil)
	if grantEveryoneWrite {
		grants = []windowsTestACLGrant{{SIDType: windows.WinWorldSid, AccessPermissions: windows.GENERIC_WRITE}}
	}
	setWindowsProtectedPathDACL(t, path, false, grants)
	return path
}
