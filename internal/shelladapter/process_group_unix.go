//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package shelladapter

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureCommandCancellation gives each Unix task command its own process
// group. exec.CommandContext otherwise terminates only the direct child, which
// can leave descendants holding the adapter's output pipes after cancellation.
func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil || cmd.Process.Pid <= 0 {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
}
