//go:build !rdev_bootstrap_focused

package release

import "time"

// IsCanonicalUTCTimestamp reports whether value is the canonical UTC form used
// by signed layered manifests and local layered-attempt state.
func IsCanonicalUTCTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.UTC().Format(time.RFC3339Nano) == value
}
