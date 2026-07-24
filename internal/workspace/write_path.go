package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorkspaceWritePath resolves a workspace-relative write target without
// permitting an existing symlink prefix or target to leave the workspace.
func ResolveWorkspaceWritePath(root, raw string) (string, error) {
	canonicalRoot, err := CanonicalDir(root)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) {
		return "", fmt.Errorf("write target must be workspace-relative")
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || (len(clean) >= 2 && clean[1] == ':') {
		return "", fmt.Errorf("write target escapes workspace root")
	}
	lexicalTarget := filepath.Join(canonicalRoot, clean)
	if !writeScopePathWithin(canonicalRoot, lexicalTarget) {
		return "", fmt.Errorf("write target escapes workspace root")
	}
	parentCanonical, err := resolveWriteScope(canonicalRoot, filepath.Dir(clean))
	if err != nil {
		return "", fmt.Errorf("resolve write target parent: %w", err)
	}
	if !writeScopePathWithin(canonicalRoot, parentCanonical) {
		return "", fmt.Errorf("write target parent escapes workspace root")
	}
	targetCanonical := filepath.Join(parentCanonical, filepath.Base(clean))
	if info, statErr := os.Lstat(lexicalTarget); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		targetCanonical, err = filepath.EvalSymlinks(lexicalTarget)
		if err != nil {
			return "", fmt.Errorf("write target symlink must resolve inside workspace: %w", err)
		}
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", fmt.Errorf("inspect write target: %w", statErr)
	}
	if !writeScopePathWithin(canonicalRoot, targetCanonical) {
		return "", fmt.Errorf("write target escapes workspace root")
	}
	return targetCanonical, nil
}

// ResolveScopedWorkspaceWritePath resolves a workspace-relative target and
// verifies that its actual filesystem location remains within a declared
// write scope.
func ResolveScopedWorkspaceWritePath(root, raw string, scopes []string) (string, error) {
	if len(scopes) == 0 {
		return "", fmt.Errorf("write_scope is required")
	}
	if err := ValidateWriteScopes(root, scopes); err != nil {
		return "", fmt.Errorf("validate write_scope: %w", err)
	}
	target, err := ResolveWorkspaceWritePath(root, raw)
	if err != nil {
		return "", err
	}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "" {
			continue
		}
		scopePath, err := ResolveWriteScope(root, scope)
		if err != nil {
			return "", fmt.Errorf("resolve write_scope: %w", err)
		}
		if writeScopePathWithin(scopePath, target) {
			return target, nil
		}
	}
	return "", fmt.Errorf("write target is outside declared write_scope")
}
