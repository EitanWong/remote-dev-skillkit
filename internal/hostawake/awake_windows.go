//go:build windows

package hostawake

import (
	"context"
	"fmt"
	"syscall"
)

const (
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
	esContinuous      = 0x80000000
)

func acquire(ctx context.Context) Lease {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("SetThreadExecutionState")
	if err := kernel32.Load(); err != nil {
		return unavailable("SetThreadExecutionState", err)
	}
	state := uintptr(esContinuous | esSystemRequired | esDisplayRequired)
	ret, _, callErr := proc.Call(state)
	if ret == 0 {
		return unavailable("SetThreadExecutionState", callErr)
	}
	return Lease{
		Enabled: true,
		Method:  "SetThreadExecutionState",
		Detail:  "ES_CONTINUOUS|ES_SYSTEM_REQUIRED|ES_DISPLAY_REQUIRED best_effort_no_idle_sleep",
		close: func() error {
			ret, _, closeErr := proc.Call(uintptr(esContinuous))
			if ret == 0 {
				return fmt.Errorf("restore SetThreadExecutionState: %w", closeErr)
			}
			return nil
		},
	}
}
