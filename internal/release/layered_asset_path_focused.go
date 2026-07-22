//go:build rdev_bootstrap_focused

package release

import (
	"path"
	"strings"
)

func validRelativeAssetPath(value string) bool {
	clean := path.Clean(value)
	return value != "" && clean == value && clean != "." && clean != ".." &&
		!strings.HasPrefix(clean, "../") && !strings.HasPrefix(value, "/") &&
		!strings.ContainsAny(value, "\\?#:%")
}
