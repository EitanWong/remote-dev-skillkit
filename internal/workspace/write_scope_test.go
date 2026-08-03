package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkspaceWritePathRejectsEscape(t *testing.T) {
	root := t.TempDir()
	for _, raw := range []string{"", "  ", "/etc/passwd", "..", "../evil", "a/../../evil", "C:/windows/evil", "c:\\windows\\evil"} {
		if _, err := ResolveWorkspaceWritePath(root, raw); err == nil {
			t.Fatalf("write target %q accepted", raw)
		}
	}
}

func TestResolveWorkspaceWritePathHappyPath(t *testing.T) {
	root := t.TempDir()
	target, err := ResolveWorkspaceWritePath(root, "internal/contracts/tools.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(target, root) || !strings.HasSuffix(target, filepath.Join("internal", "contracts", "tools.go")) {
		t.Fatalf("unexpected target %q", target)
	}
}

func TestResolveWorkspaceWritePathRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWorkspaceWritePath(root, "linked/secret"); err == nil {
		t.Fatal("write through escaping symlink accepted")
	}
}

func TestResolveScopedWorkspaceWritePathEnforcesScope(t *testing.T) {
	root := t.TempDir()
	scopes := []string{"internal", "cmd"}
	if _, err := ResolveScopedWorkspaceWritePath(root, "internal/x.go", scopes); err != nil {
		t.Fatalf("in-scope write rejected: %v", err)
	}
	if _, err := ResolveScopedWorkspaceWritePath(root, "docs/x.md", scopes); err == nil {
		t.Fatal("out-of-scope write accepted")
	}
	if _, err := ResolveScopedWorkspaceWritePath(root, "internal/x.go", nil); err == nil {
		t.Fatal("empty scopes accepted")
	}
	if _, err := ResolveScopedWorkspaceWritePath(root, "internal/x.go", []string{"../outside"}); err == nil {
		t.Fatal("escaping scope accepted")
	}
}

func TestResolveWriteScopeBasics(t *testing.T) {
	root := t.TempDir()
	resolved, err := ResolveWriteScope(root, "internal/deep")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resolved, root) || !strings.HasSuffix(resolved, filepath.Join("internal", "deep")) {
		t.Fatalf("unexpected scope resolution %q", resolved)
	}
	if _, err := ResolveWriteScope(root, ".."); err == nil {
		t.Fatal("escaping scope accepted")
	}
	if _, err := ResolveWriteScope(root, "/etc"); err == nil {
		t.Fatal("absolute escaping scope accepted")
	}
	// A symlink prefix that resolves outside the root must be rejected.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWriteScope(root, "escape/x"); err == nil {
		t.Fatal("symlink-escaped scope accepted")
	}
}
