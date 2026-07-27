package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWriteScopesRejectsNestedExternalSymlink(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(allowed, "outside-link")); err != nil {
		t.Skipf("symlink creation is not available: %v", err)
	}

	if err := ValidateWriteScopes(root, []string{"allowed"}); err == nil {
		t.Fatal("nested symlink that resolves outside workspace must be rejected")
	}
}

func TestValidateWriteScopesAllowsNestedInternalSymlink(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	target := filepath.Join(allowed, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(allowed, "internal-link")); err != nil {
		t.Skipf("symlink creation is not available: %v", err)
	}

	if err := ValidateWriteScopes(root, []string{"allowed"}); err != nil {
		t.Fatalf("nested symlink inside workspace must be allowed: %v", err)
	}
}

func TestValidateWriteScopesAllowsMissingNestedScopeInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := ValidateWriteScopes(root, []string{filepath.Join("missing", "nested")}); err != nil {
		t.Fatalf("missing nested scope inside workspace must remain allowed: %v", err)
	}
}
