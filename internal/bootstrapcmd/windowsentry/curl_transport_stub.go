//go:build !windows

package windowsentry

import (
	"fmt"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
)

func defaultTransport() (assetdownload.Transport, error) {
	return nil, fmt.Errorf("focused Windows transport requires Windows")
}
