//go:build !windows && !rdev_bootstrap_focused

package windowsentry

import (
	"path/filepath"
	"testing"
)

func windowsEntryTestCacheDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(canonicalWindowsEntryTestTempDir(t), "cache")
}
