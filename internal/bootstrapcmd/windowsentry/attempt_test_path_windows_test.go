//go:build windows && !rdev_bootstrap_focused

package windowsentry

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
)

var windowsAttemptTestSequence atomic.Uint64

func privateAttemptDirForTest(t *testing.T) string {
	t.Helper()
	localAppData, err := winKnownLocalAppData()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(
		localAppData,
		"RemoteDevSkillkit",
		"attempts",
		"test-"+strconv.Itoa(os.Getpid())+"-"+strconv.FormatUint(windowsAttemptTestSequence.Add(1), 10),
	)
	if err := preparePrivateAttemptDirectory(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func privateLauncherFileForTest(t *testing.T, directory, name string) string {
	t.Helper()
	file, err := createPrivateAttemptFile(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		t.Fatal(err)
	}
	aclTool := filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe")
	if err := exec.Command(aclTool, path, "/inheritance:r", "/grant:r", "*"+trustees.current+":F", "*S-1-5-18:F", "*S-1-5-32-544:F").Run(); err != nil {
		t.Fatal(err)
	}
	return path
}

func attemptProcessRunningForTest(pid int) bool {
	const synchronize = 0x00100000
	handle, err := syscall.OpenProcess(synchronize, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	result, err := syscall.WaitForSingleObject(handle, 0)
	return err == nil && result == syscall.WAIT_TIMEOUT
}

func killAttemptProcessForTest(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func assertPrivateAttemptPathForTest(t *testing.T, path string, directory bool) {
	t.Helper()
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsPrivatePath(path, directory, directory, trustees); err != nil {
		t.Fatal(err)
	}
}
