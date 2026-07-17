//go:build windows && !rdev_bootstrap_focused

package windowsentry

import (
	"os"
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
