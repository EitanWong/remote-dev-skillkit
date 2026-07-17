//go:build windows && !rdev_bootstrap_focused

package windowsentry

import (
	"os"
	"syscall"
	"testing"
)

func privateAttemptDirForTest(t *testing.T) string {
	t.Helper()
	directory, err := createPrivateTemporaryDirectory("rdev-attempt-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
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
