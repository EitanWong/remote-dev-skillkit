//go:build !windows

package windowsentry

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

type coreLifecycle struct{}

func newCoreLifecycle(cmd *exec.Cmd) (*coreLifecycle, error) {
	if cmd == nil {
		return nil, fmt.Errorf("core command is required")
	}
	attributes := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attributes = *cmd.SysProcAttr
	}
	attributes.Setpgid = true
	attributes.Pgid = 0
	cmd.SysProcAttr = &attributes
	return &coreLifecycle{}, nil
}

func (*coreLifecycle) run(ctx context.Context, cmd *exec.Cmd) (bool, error) {
	if err := cmd.Start(); err != nil {
		return true, err
	}
	processGroupID := cmd.Process.Pid
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var waitErr error
	canceled := false
	select {
	case waitErr = <-waitDone:
	case <-ctx.Done():
		canceled = true
		waitErr = ctx.Err()
	}
	killErr := killCoreProcessGroup(processGroupID)
	if canceled {
		waitErr = errors.Join(waitErr, <-waitDone)
	}
	emptyErr := waitCoreProcessGroupEmpty(processGroupID)
	return killErr == nil && emptyErr == nil, errors.Join(waitErr, killErr, emptyErr)
}

func (*coreLifecycle) close() error { return nil }

func killCoreProcessGroup(processGroupID int) error {
	err := syscall.Kill(-processGroupID, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func waitCoreProcessGroupEmpty(processGroupID int) error {
	for attempt := 0; attempt < 1000; attempt++ {
		err := syscall.Kill(-processGroupID, 0)
		if err == syscall.ESRCH {
			return nil
		}
		if err != nil && err != syscall.EPERM {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("core process group did not exit")
}
