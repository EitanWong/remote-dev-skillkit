//go:build windows

package tunnel

import (
	"fmt"
	"path/filepath"
)

func validateProtectedParentChain(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, parent := range parentDirectories(filepath.Dir(abs)) {
		file, err := openProtectedPath(parent, true)
		if err != nil {
			return fmt.Errorf("open ancestor: %w", err)
		}
		info, statErr := file.Stat()
		if statErr != nil {
			file.Close()
			return fmt.Errorf("stat ancestor: %w", statErr)
		}
		if !info.IsDir() {
			file.Close()
			return fmt.Errorf("protected path ancestor must be a directory")
		}
		if err := validateProtectedWindowsAncestorPermissions(file); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close ancestor: %w", err)
		}
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
