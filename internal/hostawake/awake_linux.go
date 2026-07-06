//go:build linux

package hostawake

import (
	"context"
	"fmt"
	"os/exec"
)

func acquire(ctx context.Context) Lease {
	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		return commandUnavailable("systemd-inhibit")
	}
	cmd := exec.CommandContext(
		ctx,
		"systemd-inhibit",
		"--what=sleep:idle",
		"--why=Remote Dev Skillkit host session is active",
		"--mode=block",
		"sleep",
		"infinity",
	)
	if err := cmd.Start(); err != nil {
		return unavailable("systemd-inhibit", err)
	}
	return Lease{
		Enabled: true,
		Method:  "systemd-inhibit",
		Detail:  fmt.Sprintf("pid=%d what=sleep:idle mode=block best_effort_no_idle_sleep", cmd.Process.Pid),
		close: func() error {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return cmd.Wait()
		},
	}
}
