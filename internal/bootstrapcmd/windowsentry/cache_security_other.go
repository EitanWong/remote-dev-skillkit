//go:build !windows

package windowsentry

import (
	"fmt"
	"os"
	"path/filepath"
)

func preparePrivateCache(root string, directories []string, files []managedCacheFile) error {
	if err := rejectNearestCacheSymlink(root); err != nil {
		return err
	}
	for _, directory := range directories {
		if err := ensurePrivateCacheDirectory(directory); err != nil {
			return err
		}
	}
	for _, file := range files {
		info, err := os.Lstat(file.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > file.maxBytes {
			return fmt.Errorf("layered cache contains an unsafe managed file")
		}
		if err := os.Chmod(file.path, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func rejectNearestCacheSymlink(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("layered cache must not traverse a symbolic link")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

func ensurePrivateCacheDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("layered cache directory is not a private directory")
	}
	return os.Chmod(path, 0o700)
}

func validatePrivateCacheFile(path string, expectedSize int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != expectedSize {
		return fmt.Errorf("layered cache file is not a private regular file with the signed size")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("layered cache file permissions are not private")
	}
	return nil
}

func openPrivateRuntime(path string) (*os.File, error) {
	return os.Open(path)
}
