//go:build windows

package windowsentry

import (
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

var windowsEntryTestCacheSequence atomic.Uint64

func windowsEntryTestCacheDir(t *testing.T) string {
	t.Helper()
	localAppData, err := winKnownLocalAppData()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(
		localAppData,
		"RemoteDevSkillkit",
		"cache",
		"test-"+strconv.Itoa(os.Getpid())+"-"+strconv.FormatUint(windowsEntryTestCacheSequence.Add(1), 10),
	)
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}
