//go:build !windows

package tunnel

import (
	"fmt"
	"os"
)

func validateProtectedJSONPermissions(_ *os.File, info os.FileInfo) error {
	if info.Mode().Perm()&^os.FileMode(0o600) != 0 {
		return fmt.Errorf("protected JSON permissions must be 0600 or narrower")
	}
	return nil
}
