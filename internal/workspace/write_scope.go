package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const maxWriteScopeEntries = 50_000

// ValidateWriteScopes verifies that declared write scopes remain inside root
// after resolving existing symlink prefixes and nested symlinks. It permits
// missing descendants so a task can create a new path inside a declared scope.
func ValidateWriteScopes(root string, scopes []string) error {
	canonicalRoot, err := CanonicalDir(root)
	if err != nil {
		return fmt.Errorf("canonicalize workspace root: %w", err)
	}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		resolved, err := resolveWriteScope(canonicalRoot, scope)
		if err != nil {
			return err
		}
		if !writeScopePathWithin(canonicalRoot, resolved) {
			return fmt.Errorf("write scope %q escapes workspace root", scope)
		}
		if err := validateWriteScopeSymlinks(canonicalRoot, resolved, scope); err != nil {
			return err
		}
	}
	return nil
}

// ResolveWriteScope returns the effective path for one declared scope after
// resolving every existing symlink prefix. Callers that need to trust the
// scope tree itself must first call ValidateWriteScopes.
func ResolveWriteScope(root, scope string) (string, error) {
	canonicalRoot, err := CanonicalDir(root)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	return resolveWriteScope(canonicalRoot, strings.TrimSpace(scope))
}

func resolveWriteScope(root, scope string) (string, error) {
	candidate := scope
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)
	if !writeScopePathWithin(root, candidate) {
		return "", fmt.Errorf("write scope %q escapes workspace root", scope)
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", fmt.Errorf("resolve write scope %q: %w", scope, err)
	}
	if relative == "." {
		return root, nil
	}
	parts := strings.FieldsFunc(relative, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	current := root
	for index, part := range parts {
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if os.IsNotExist(err) {
			return filepath.Join(append([]string{current}, parts[index:]...)...), nil
		}
		if err != nil {
			return "", fmt.Errorf("resolve write scope %q: %w", scope, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			current = next
			continue
		}
		resolved, err := filepath.EvalSymlinks(next)
		if err != nil {
			return "", fmt.Errorf("resolve write scope %q symlink: %w", scope, err)
		}
		resolved = filepath.Clean(resolved)
		if !writeScopePathWithin(root, resolved) {
			return "", fmt.Errorf("write scope %q escapes workspace root", scope)
		}
		current = resolved
	}
	return current, nil
}

func validateWriteScopeSymlinks(root, scopePath, declaredScope string) error {
	if _, err := os.Lstat(scopePath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect write scope %q: %w", declaredScope, err)
	}
	entries := 0
	return filepath.WalkDir(scopePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > maxWriteScopeEntries {
			return fmt.Errorf("write scope %q exceeds %d entries; cannot validate symlink boundary", declaredScope, maxWriteScopeEntries)
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve write scope %q symlink %q: %w", declaredScope, path, err)
		}
		if writeScopePathWithin(root, filepath.Clean(resolved)) {
			return nil
		}
		return fmt.Errorf("write scope %q contains symlink %q that escapes workspace root", declaredScope, path)
	})
}

func writeScopePathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
