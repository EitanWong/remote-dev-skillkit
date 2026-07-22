//go:build windows

package windowsentry

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/release"
)

func TestWindowsEntryPrivateCacheDACLAndRuntimeLock(t *testing.T) {
	cacheRoot := windowsEntryTestCacheDir(t)
	asset := release.LayeredAsset{
		RelativePath: "rdev-core.exe",
		SHA256:       "sha256:" + strings.Repeat("a", 64),
		SizeBytes:    4,
	}
	outputPath, _, err := cachePaths(cacheRoot, asset)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(outputPath); err != nil || info.Size() != 0 {
		t.Fatalf("cache preparation must create a private placeholder for managed files: info=%v err=%v", info, err)
	}
	if attributes, err := windowsPathAttributes(outputPath); err != nil {
		t.Fatal(err)
	} else if attributes&winFileAttributeTemporary != 0 {
		t.Fatal("managed cache placeholder must not use the temporary-file attribute")
	}
	if err := os.WriteFile(outputPath, []byte("core"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateCacheFile(outputPath, 4); err != nil {
		t.Fatal(err)
	}
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsPrivatePath(filepath.Dir(outputPath), true, true, trustees); err != nil {
		t.Fatal(err)
	}
	runtimeFile, err := openPrivateRuntime(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsPrivateHandle(syscall.Handle(runtimeFile.Fd()), false, true, 3, trustees); err != nil {
		t.Fatalf("private cache file must use a protected exact file DACL: %v", err)
	}
	if err := os.Remove(outputPath); err == nil {
		_ = runtimeFile.Close()
		t.Fatal("locked runtime allowed pathname deletion")
	}
	if err := runtimeFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(outputPath); err != nil {
		t.Fatalf("runtime remained locked after handle close: %v", err)
	}
}

func TestWindowsPrivateDescriptorUsesExactProtectedDACL(t *testing.T) {
	directory, err := createPrivateTemporaryDirectory("rdev-dacl-shape-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	file, err := createPrivateTemporaryFile(directory, "shape.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{directory, file.Name()} {
		isDirectory := path == directory
		state := securityStateForTest(t, path, isDirectory)
		if err := validateWindowsPrivateSecurityState(state, true, 3, trustees); err != nil {
			t.Fatalf("private descriptor for %s is not exact: %v", filepath.Base(path), err)
		}
	}
}

func securityStateForTest(t *testing.T, path string, directory bool) winSecurityState {
	t.Helper()
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	flags := uint32(syscall.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= syscall.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := syscall.CreateFile(pointer, winReadControl|winFileReadAttributes, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, flags, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	state, err := readWinSecurityState(handle)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestWindowsEntryPrivateCurlTemporaryFiles(t *testing.T) {
	directory, err := createPrivateTemporaryDirectory("rdev-bootstrap-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	file, err := createPrivateTemporaryFile(directory, "body.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateTemporaryFile(file, file.Name(), 4); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file.Name()); err == nil {
		_ = file.Close()
		t.Fatal("private temporary file allowed pathname deletion while its identity handle was open")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWindowsPrivateSecurityStateRejectsEveryNonExactDACL(t *testing.T) {
	trustees := windowsPrivateTrustees{
		current:        "S-1-5-21-1000",
		administrators: "S-1-5-32-544",
		system:         "S-1-5-18",
	}
	valid := winSecurityState{
		control: winSEDACLPresent | winSEDACLProtected,
		owner:   trustees.current,
		aces: []winACE{
			{typeID: winAccessAllowedACEType, flags: 3, mask: winPrivateFullControl, sid: trustees.current},
			{typeID: winAccessAllowedACEType, flags: 3, mask: winPrivateFullControl, sid: trustees.administrators},
			{typeID: winAccessAllowedACEType, flags: 3, mask: winPrivateFullControl, sid: trustees.system},
		},
	}
	if err := validateWindowsPrivateSecurityState(valid, true, 3, trustees); err != nil {
		t.Fatalf("exact protected file DACL was rejected: %v", err)
	}

	tests := []struct {
		name  string
		state winSecurityState
	}{
		{name: "missing DACL", state: winSecurityState{owner: valid.owner, aces: valid.aces}},
		{name: "unprotected DACL", state: winSecurityState{control: winSEDACLPresent, owner: valid.owner, aces: valid.aces}},
		{name: "untrusted owner", state: winSecurityState{control: valid.control, owner: "S-1-5-32-545", aces: valid.aces}},
		{name: "missing ACE", state: winSecurityState{control: valid.control, owner: valid.owner, aces: valid.aces[:2]}},
		{name: "extra ACE", state: winSecurityState{control: valid.control, owner: valid.owner, aces: append(append([]winACE{}, valid.aces...), winACE{typeID: winAccessAllowedACEType, mask: winPrivateFullControl, sid: "S-1-5-32-545"})}},
		{name: "duplicate ACE", state: winSecurityState{control: valid.control, owner: valid.owner, aces: []winACE{valid.aces[0], valid.aces[1], valid.aces[0]}}},
		{name: "untrusted SID", state: winSecurityState{control: valid.control, owner: valid.owner, aces: []winACE{valid.aces[0], valid.aces[1], {typeID: winAccessAllowedACEType, mask: winPrivateFullControl, sid: "S-1-5-32-545"}}}},
		{name: "wrong ACE type", state: winSecurityState{control: valid.control, owner: valid.owner, aces: []winACE{{typeID: 1, mask: winPrivateFullControl, sid: trustees.current}, valid.aces[1], valid.aces[2]}}},
		{name: "wrong mask", state: winSecurityState{control: valid.control, owner: valid.owner, aces: []winACE{{typeID: winAccessAllowedACEType, mask: winPrivateFullControl - 1, sid: trustees.current}, valid.aces[1], valid.aces[2]}}},
		{name: "wrong file flags", state: winSecurityState{control: valid.control, owner: valid.owner, aces: []winACE{{typeID: winAccessAllowedACEType, mask: winPrivateFullControl, sid: trustees.current}, valid.aces[1], valid.aces[2]}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateWindowsPrivateSecurityState(test.state, true, 3, trustees); err == nil {
				t.Fatal("non-exact private DACL was accepted")
			}
		})
	}
}
