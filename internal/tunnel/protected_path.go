package tunnel

import (
	"fmt"
	"os"
)

func ValidateProtectedDirectory(path string) error {
	file, err := openProtectedPath(path, true)
	if err != nil {
		return fmt.Errorf("open protected directory: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat protected directory: %w", err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect protected directory path: %w", err)
	}
	if !info.IsDir() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) {
		return fmt.Errorf("protected directory must be a stable non-symlink directory")
	}
	return validateProtectedPathPermissions(file, info, true)
}

func ValidateProtectedRegularFileIfExists(path string) error {
	file, err := openProtectedPath(path, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open protected file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat protected file: %w", err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect protected file path: %w", err)
	}
	if !info.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) {
		return fmt.Errorf("protected file must be a stable non-symlink regular file")
	}
	return validateProtectedPathPermissions(file, info, false)
}
