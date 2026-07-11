package tunnel

import (
	"fmt"
	"io"
	"math"
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

// ValidateProtectedParentChain verifies that untrusted local users cannot
// replace path through a writable ancestor after the file itself is checked.
func ValidateProtectedParentChain(path string) error {
	if err := validateProtectedParentChain(path); err != nil {
		return fmt.Errorf("validate protected parent chain: %w", err)
	}
	return nil
}

// ReadProtectedRegularFile reads a local confidential regular file through the
// same handle used for path, type, identity, and permission validation.
func ReadProtectedRegularFile(path string, maxBytes int64) ([]byte, error) {
	if maxBytes < 1 || maxBytes == math.MaxInt64 {
		return nil, fmt.Errorf("protected file size limit must be between 1 and %d", math.MaxInt64-1)
	}
	file, err := openProtectedPath(path, false)
	if err != nil {
		return nil, fmt.Errorf("open protected file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat protected file: %w", err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect protected file path: %w", err)
	}
	if !info.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) {
		return nil, fmt.Errorf("protected file must be a stable non-symlink regular file")
	}
	if err := validateProtectedPathPermissions(file, info, false); err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("protected file exceeds %d bytes", maxBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read protected file: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("protected file exceeds %d bytes", maxBytes)
	}
	return content, nil
}
