//go:build windows

package hostrunner

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hostUpdateEndpointIDHeader must match the gateway's
// httpapi.hostUpdateEndpointHeader.
const hostUpdateEndpointIDHeader = "X-Rdev-Endpoint-Id"

// launchDetachedHostUpdater starts `rdev-host service update` as a fully
// detached process: the service stop performed by the updater kills this
// parent process, so the updater must survive without it.
func launchDetachedHostUpdater(releaseDir string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate host executable: %w", err)
	}
	command := exec.Command(executable, "service", "update", "--release", releaseDir)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start detached host updater: %w", err)
	}
	_ = command.Process.Release()
	return nil
}
