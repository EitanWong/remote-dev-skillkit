//go:build !darwin && !linux && !windows

package hostawake

import "context"

func acquire(ctx context.Context) Lease {
	return unavailable("unsupported-platform", nil)
}
