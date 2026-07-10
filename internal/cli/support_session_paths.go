package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func prepareAndValidateSupportSessionPublicationPaths(paths []string) error {
	canonical := make([]string, len(paths))
	for index, path := range paths {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("support-session publication path is required")
		}
		abs, err := canonicalPathThroughExistingAncestor(strings.TrimSpace(path))
		if err != nil {
			return fmt.Errorf("resolve support-session publication path: %w", err)
		}
		canonical[index] = filepath.Clean(abs)
		for previous := 0; previous < index; previous++ {
			equal := canonical[previous] == canonical[index]
			if runtime.GOOS == "windows" {
				equal = strings.EqualFold(canonical[previous], canonical[index])
			}
			if equal {
				return fmt.Errorf("support-session publication paths must be distinct")
			}
		}
	}
	for _, path := range canonical {
		parent := filepath.Dir(path)
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return fmt.Errorf("create support-session publication directory: %w", err)
		}
		if err := tunnel.ValidateProtectedDirectory(parent); err != nil {
			return fmt.Errorf("unsafe support-session publication directory %q: %w", parent, err)
		}
		if err := tunnel.ValidateProtectedRegularFileIfExists(path); err != nil {
			return fmt.Errorf("unsafe existing support-session publication file %q: %w", path, err)
		}
	}
	for left := 0; left < len(canonical); left++ {
		leftInfo, leftErr := os.Stat(canonical[left])
		if leftErr != nil {
			continue
		}
		for right := left + 1; right < len(canonical); right++ {
			rightInfo, rightErr := os.Stat(canonical[right])
			if rightErr == nil && os.SameFile(leftInfo, rightInfo) {
				return fmt.Errorf("support-session publication paths must not alias the same file")
			}
		}
	}
	return nil
}

func canonicalPathThroughExistingAncestor(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	ancestor := filepath.Clean(abs)
	suffix := make([]string, 0)
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("no existing ancestor for %q", abs)
		}
		suffix = append([]string{filepath.Base(ancestor)}, suffix...)
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	if filepath.Clean(resolved) != filepath.Clean(ancestor) {
		trustedDarwinVar := runtime.GOOS == "darwin" && (ancestor == "/var" || strings.HasPrefix(ancestor, "/var/")) && filepath.Clean(resolved) == filepath.Clean("/private"+ancestor)
		if !trustedDarwinVar {
			return "", fmt.Errorf("path must not traverse symlinked ancestors")
		}
	}
	parts := append([]string{resolved}, suffix...)
	return filepath.Join(parts...), nil
}
