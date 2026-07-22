//go:build !windows

package assetdownload

import "os"

func atomicReplace(sourcePath, destinationPath string) error {
	return os.Rename(sourcePath, destinationPath)
}
