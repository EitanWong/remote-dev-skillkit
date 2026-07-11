//go:build windows

package tunnel

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func validateWindowsLocalVolumePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	pointer, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return err
	}
	volumePath := make([]uint16, 32768)
	if err := windows.GetVolumePathName(pointer, &volumePath[0], uint32(len(volumePath))); err != nil {
		return fmt.Errorf("resolve protected path Windows volume: %w", err)
	}
	root := windows.UTF16ToString(volumePath)
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return err
	}
	driveType := windows.GetDriveType(rootPointer)
	if !windowsDriveTypeIsLocal(driveType) {
		return fmt.Errorf("protected path must be stored on a local Windows volume")
	}
	return nil
}
