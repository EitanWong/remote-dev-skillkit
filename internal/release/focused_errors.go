//go:build rdev_bootstrap_focused

package release

import "errors"

var (
	ErrManifestInvalid   = errors.New("release manifest invalid")
	ErrManifestSignature = errors.New("release manifest signature invalid")
)
