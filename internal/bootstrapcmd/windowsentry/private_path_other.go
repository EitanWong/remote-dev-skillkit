//go:build !windows

package windowsentry

import "os"

func ProtectPrivatePath(path string, directory bool) error {
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return ValidatePrivatePath(path, directory)
}

func ValidatePrivatePath(path string, directory bool) error {
	return validatePrivateLauncherPath(path, directory)
}
