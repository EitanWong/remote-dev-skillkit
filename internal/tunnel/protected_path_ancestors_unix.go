//go:build !windows

package tunnel

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func validateProtectedParentChain(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	if filepath.Clean(resolved) != filepath.Clean(abs) {
		return fmt.Errorf("protected path must not traverse symlinks")
	}

	parents := parentDirectories(filepath.Dir(resolved))
	for _, parent := range parents {
		var file *os.File
		if parent == string(filepath.Separator) {
			file, err = os.Open(parent)
		} else {
			file, err = openProtectedPath(parent, true)
		}
		if err != nil {
			return fmt.Errorf("open ancestor: %w", err)
		}
		info, statErr := file.Stat()
		aclErr := validateProtectedExtendedACL(file, protectedACLReplacementPermit)
		closeErr := file.Close()
		if statErr != nil {
			return fmt.Errorf("stat ancestor: %w", statErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close ancestor: %w", closeErr)
		}
		if aclErr != nil {
			return aclErr
		}
		if !info.IsDir() {
			return fmt.Errorf("protected path ancestor must be a directory")
		}
		if err := validateProtectedUnixAncestor(info); err != nil {
			return err
		}
	}
	return nil
}

func validateProtectedUnixAncestor(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect protected path ancestor owner")
	}
	if stat.Uid != 0 && int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("protected path ancestor must be owned by root or the current user")
	}
	if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
		return fmt.Errorf("protected path ancestor is writable by an untrusted user")
	}
	return nil
}

func parentDirectories(parent string) []string {
	var reversed []string
	for current := filepath.Clean(parent); ; current = filepath.Dir(current) {
		reversed = append(reversed, current)
		next := filepath.Dir(current)
		if next == current {
			break
		}
	}
	parents := make([]string, len(reversed))
	for index := range reversed {
		parents[index] = reversed[len(reversed)-1-index]
	}
	return parents
}
