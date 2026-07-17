//go:build windows && !rdev_bootstrap_focused

package assetdownload

import "golang.org/x/sys/windows"

func atomicReplace(sourcePath, destinationPath string) error {
	return windows.Rename(sourcePath, destinationPath)
}
