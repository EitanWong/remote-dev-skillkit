//go:build !rdev_bootstrap_focused

package release

import (
	"net/url"
	"path"
	"strings"
)

func validRelativeAssetPath(value string) bool {
	u, err := url.Parse(value)
	if err != nil || value == "" || u.IsAbs() || path.IsAbs(u.Path) || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || strings.Contains(value, "#") {
		return false
	}
	if strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return false
	}
	clean := path.Clean(value)
	decodedClean := path.Clean(u.Path)
	return clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") &&
		decodedClean == u.Path && decodedClean != "." && decodedClean != ".." &&
		!strings.HasPrefix(decodedClean, "../") && !strings.Contains(u.Path, "\\")
}
