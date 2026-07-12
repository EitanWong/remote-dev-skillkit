package tunnel

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
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

// VerifyProtectedRegularFileSHA256 streams a confidential protected file
// through the same handle used for path, type, identity, and permission checks.
func VerifyProtectedRegularFileSHA256(path string, maxBytes int64, expected [sha256.Size]byte) error {
	file, err := OpenVerifiedProtectedRegularFileSHA256(path, maxBytes, expected)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close protected file: %w", err)
	}
	return nil
}

// OpenVerifiedProtectedRegularFileSHA256 returns the verified confidential file
// handle rewound to offset zero. The caller owns the handle and must close it.
func OpenVerifiedProtectedRegularFileSHA256(path string, maxBytes int64, expected [sha256.Size]byte) (*os.File, error) {
	return openVerifiedProtectedRegularFileSHA256(path, maxBytes, expected, false)
}

// OpenVerifiedProtectedExecutableSHA256 returns a verified private executable
// handle rewound to offset zero. Unix executables must have exactly 0700 mode.
func OpenVerifiedProtectedExecutableSHA256(path string, maxBytes int64, expected [sha256.Size]byte) (*os.File, error) {
	return openVerifiedProtectedRegularFileSHA256(path, maxBytes, expected, true)
}

func openVerifiedProtectedRegularFileSHA256(path string, maxBytes int64, expected [sha256.Size]byte, executable bool) (_ *os.File, returnErr error) {
	if maxBytes < 1 || maxBytes == math.MaxInt64 {
		return nil, fmt.Errorf("protected file size limit must be between 1 and %d", math.MaxInt64-1)
	}
	file, err := openProtectedPath(path, false)
	if err != nil {
		return nil, fmt.Errorf("open protected file: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = file.Close()
		}
	}()
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
	if err := validateProtectedPathPermissions(file, info, executable); err != nil {
		return nil, err
	}
	if executable && runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		return nil, fmt.Errorf("protected executable permissions must be 0700")
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("protected file exceeds %d bytes", maxBytes)
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("hash protected file: %w", err)
	}
	if written > maxBytes {
		return nil, fmt.Errorf("protected file exceeds %d bytes", maxBytes)
	}
	if subtle.ConstantTimeCompare(digest.Sum(nil), expected[:]) != 1 {
		return nil, fmt.Errorf("protected file digest mismatch")
	}
	postInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("restat protected file: %w", err)
	}
	postPathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("reinspect protected file path: %w", err)
	}
	if !postInfo.Mode().IsRegular() || postPathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(postInfo, postPathInfo) || postInfo.Size() != written {
		return nil, fmt.Errorf("protected file changed during verification")
	}
	if err := validateProtectedPathPermissions(file, postInfo, executable); err != nil {
		return nil, err
	}
	if executable && runtime.GOOS != "windows" && postInfo.Mode().Perm() != 0o700 {
		return nil, fmt.Errorf("protected executable permissions must be 0700")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind protected file: %w", err)
	}
	return file, nil
}
