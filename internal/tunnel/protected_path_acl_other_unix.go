//go:build !darwin && !windows

package tunnel

import "os"

func validateProtectedExtendedACL(_ *os.File, _ uint32) error {
	return nil
}
