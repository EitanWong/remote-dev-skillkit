//go:build !windows

package hostcmd

import (
	"context"
	"fmt"
)

func (a App) service(_ context.Context, _ []string) error {
	return fmt.Errorf("managed service commands are supported only on Windows")
}
