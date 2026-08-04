//go:build !windows

package hostrunner

import "fmt"

// hostUpdateEndpointIDHeader must match the gateway's
// httpapi.hostUpdateEndpointHeader.
const hostUpdateEndpointIDHeader = "X-Rdev-Endpoint-Id"

// launchDetachedHostUpdater rejects non-Windows hosts: host updates apply to
// the Windows managed service, and this stub keeps the adapter honest on
// other platforms (the download/staging steps are still exercised by tests).
func launchDetachedHostUpdater(releaseDir string) error {
	return fmt.Errorf("host updater is only supported on Windows")
}
