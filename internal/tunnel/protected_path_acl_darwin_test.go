//go:build darwin

package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadProtectedRegularFileRejectsDarwinPermitACL(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "known_hosts")
	if err := os.WriteFile(path, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("chmod", "+a", "everyone allow write,delete", path).Run(); err != nil {
		t.Fatal(err)
	}
	_, err = ReadProtectedRegularFile(path, 1024)
	if err == nil || !strings.Contains(err.Error(), "ACL") {
		t.Fatalf("expected Darwin permit ACL rejection, got %v", err)
	}
}

func TestValidateProtectedParentChainRejectsDarwinReplacementACL(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "pins")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("chmod", "+a", "everyone allow delete_child", parent).Run(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "known_hosts")
	if err := os.WriteFile(path, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = ValidateProtectedParentChain(path)
	if err == nil || !strings.Contains(err.Error(), "ACL") {
		t.Fatalf("expected Darwin ancestor ACL rejection, got %v", err)
	}
}

func TestValidateProtectedParentChainAllowsDarwinDenyACL(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "pins")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("chmod", "+a", "everyone deny delete", parent).Run(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("chmod", "-N", parent).Run()
	})
	path := filepath.Join(parent, "known_hosts")
	if err := os.WriteFile(path, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProtectedParentChain(path); err != nil {
		t.Fatalf("safe Darwin deny ACL rejected: %v", err)
	}
}
