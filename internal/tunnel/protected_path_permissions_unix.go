//go:build !windows

package tunnel

import (
	"fmt"
	"os"
	"syscall"
)

func validateProtectedPathPermissions(file *os.File, info os.FileInfo, directory bool) error {
	if err := validateProtectedExtendedACL(file, protectedACLAnyPermit); err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("protected path must be owned by the current user")
	}
	allowed := os.FileMode(0o600)
	if directory {
		allowed = 0o700
	}
	if info.Mode().Perm()&^allowed != 0 {
		return fmt.Errorf("protected path permissions must be %04o or narrower", allowed)
	}
	return nil
}
