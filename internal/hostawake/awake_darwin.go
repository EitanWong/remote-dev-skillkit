//go:build darwin

package hostawake

import (
	"context"
	"fmt"
	"os/exec"
)

func acquire(ctx context.Context) Lease {
	if _, err := exec.LookPath("caffeinate"); err != nil {
		return commandUnavailable("caffeinate")
	}
	cmd := exec.CommandContext(ctx, "caffeinate", "-dimsu")
	if err := cmd.Start(); err != nil {
		return unavailable("caffeinate", err)
	}
	return Lease{
		Enabled: true,
		Method:  "caffeinate",
		Detail:  fmt.Sprintf("pid=%d flags=-dimsu best_effort_no_idle_sleep", cmd.Process.Pid),
		close: func() error {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return cmd.Wait()
		},
	}
}
