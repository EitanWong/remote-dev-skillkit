//go:build !windows

package tunnel

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadProtectedJSONFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		var got protectedJSONFixture
		done <- ReadProtectedJSONFile(path, &got)
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected FIFO to be rejected")
		}
	case <-time.After(time.Second):
		t.Fatal("protected JSON FIFO open blocked")
	}
}
