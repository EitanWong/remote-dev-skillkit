//go:build windows

package windowsentry

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWindowsCoreLifecycleCancelsProcessTree(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "core-tree.pids")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsCoreLifecycleDirectHelper$")
	cmd.Env = append(os.Environ(), "RDEV_WINDOWS_CORE_DIRECT=1", "RDEV_WINDOWS_CORE_READY="+readyPath)
	lifecycle, err := newCoreLifecycle(cmd)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		cleaned bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		cleaned, runErr := lifecycle.run(ctx, cmd)
		done <- result{cleaned: cleaned, err: runErr}
	}()
	pids := waitWindowsCorePIDs(t, readyPath)
	cancel()
	got := <-done
	if !got.cleaned || ctx.Err() != context.Canceled || got.err == nil {
		t.Fatalf("canceled Windows core lifecycle = cleaned %t, error %v", got.cleaned, got.err)
	}
	for _, pid := range pids {
		if waitWindowsCoreProcessExit(pid) {
			continue
		}
		process, _ := os.FindProcess(pid)
		_ = process.Kill()
		t.Fatalf("Windows job process %d remained alive", pid)
	}
}

func TestWindowsCoreLifecycleDirectHelper(t *testing.T) {
	if os.Getenv("RDEV_WINDOWS_CORE_DIRECT") != "1" {
		return
	}
	grandchild := exec.Command(os.Args[0], "-test.run=^TestWindowsCoreLifecycleGrandchildHelper$")
	grandchild.Env = append(os.Environ(), "RDEV_WINDOWS_CORE_GRANDCHILD=1")
	if err := grandchild.Start(); err != nil {
		t.Fatal(err)
	}
	content := strconv.Itoa(os.Getpid()) + " " + strconv.Itoa(grandchild.Process.Pid)
	if err := os.WriteFile(os.Getenv("RDEV_WINDOWS_CORE_READY"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestWindowsCoreLifecycleGrandchildHelper(t *testing.T) {
	if os.Getenv("RDEV_WINDOWS_CORE_GRANDCHILD") != "1" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}

func waitWindowsCorePIDs(t *testing.T, path string) []int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil {
			fields := strings.Fields(string(content))
			if len(fields) == 2 {
				pids := make([]int, 2)
				for index, field := range fields {
					pid, parseErr := strconv.Atoi(field)
					if parseErr != nil {
						t.Fatal(parseErr)
					}
					pids[index] = pid
				}
				return pids
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Windows core process tree")
	return nil
}

func waitWindowsCoreProcessExit(pid int) bool {
	const synchronize = 0x00100000
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		handle, err := syscall.OpenProcess(synchronize, false, uint32(pid))
		if err != nil {
			return true
		}
		status, waitErr := syscall.WaitForSingleObject(handle, 0)
		_ = syscall.CloseHandle(handle)
		if waitErr == nil && status != syscall.WAIT_TIMEOUT {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
