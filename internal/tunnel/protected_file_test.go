package tunnel

import (
	"crypto/sha256"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVerifyProtectedRegularFileSHA256(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows acceptance uses an explicitly controlled DACL")
	}
	content := []byte("reviewed artifact")
	want := sha256.Sum256(content)
	path := filepath.Join(resolvedProtectedTestDir(t), "asset")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProtectedRegularFileSHA256(path, 1024, want); err != nil {
		t.Fatal(err)
	}
	opened, err := OpenVerifiedProtectedRegularFileSHA256(path, 1024, want)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(opened)
	closeErr := opened.Close()
	if readErr != nil || closeErr != nil || string(got) != string(content) {
		t.Fatalf("verified handle content = %q, readErr = %v, closeErr = %v", got, readErr, closeErr)
	}
	if opened, err := OpenVerifiedProtectedExecutableSHA256(path, 1024, want); err == nil {
		_ = opened.Close()
		t.Fatal("executable verifier accepted non-executable permissions")
	}
	wrong := sha256.Sum256([]byte("wrong"))
	if err := VerifyProtectedRegularFileSHA256(path, 1024, wrong); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("digest mismatch error = %v", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProtectedRegularFileSHA256(path, 1024, want); err == nil {
		t.Fatal("generic protected-file verifier accepted executable permissions")
	}
	executable, err := OpenVerifiedProtectedExecutableSHA256(path, 1024, want)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr = io.ReadAll(executable)
	closeErr = executable.Close()
	if readErr != nil || closeErr != nil || string(got) != string(content) {
		t.Fatalf("verified executable content = %q, readErr = %v, closeErr = %v", got, readErr, closeErr)
	}
}

func TestVerifyProtectedRegularFileSHA256RejectsUnsafeInputs(t *testing.T) {
	content := []byte("oversize")
	want := sha256.Sum256(content)
	t.Run("invalid limit", func(t *testing.T) {
		if err := VerifyProtectedRegularFileSHA256("unused", math.MaxInt64, want); err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("invalid-limit error = %v", err)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows acceptance uses an explicitly controlled DACL")
		}
		path := filepath.Join(resolvedProtectedTestDir(t), "asset")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := VerifyProtectedRegularFileSHA256(path, int64(len(content)-1), want); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversize error = %v", err)
		}
	})
	t.Run("symlink or reparse point", func(t *testing.T) {
		dir := resolvedProtectedTestDir(t)
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, content, 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := VerifyProtectedRegularFileSHA256(link, 1024, want); err == nil {
			t.Fatal("symlink or reparse point accepted")
		}
	})
}

func resolvedProtectedTestDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReadProtectedRegularFileRejectsOverflowingLimit(t *testing.T) {
	if _, err := ReadProtectedRegularFile("unused", math.MaxInt64); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("ReadProtectedRegularFile() error = %v, want size-limit rejection", err)
	}
}

type protectedJSONFixture struct {
	Name string `json:"name"`
}

func TestReadProtectedJSONFileRejectsUnsafeOrInvalidInputs(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{name: "directory", setup: func(t *testing.T) string { return t.TempDir() }},
		{name: "symlink", setup: func(t *testing.T) string {
			dir := t.TempDir()
			target := filepath.Join(dir, "target.json")
			if err := os.WriteFile(target, []byte(`{"name":"safe"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "link.json")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			return link
		}},
		{name: "trailing JSON", setup: protectedJSONPath(`{"name":"safe"}{}`, 0o600)},
		{name: "unknown field", setup: protectedJSONPath(`{"name":"safe","secret":"no"}`, 0o600)},
		{name: "oversize", setup: protectedJSONPath(`{"name":"safe"}`+strings.Repeat(" ", MaxProtectedJSONBytes+1), 0o600)},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests,
			struct {
				name  string
				setup func(*testing.T) string
			}{name: "device", setup: func(*testing.T) string { return "/dev/null" }},
			struct {
				name  string
				setup func(*testing.T) string
			}{name: "group writable", setup: protectedJSONPath(`{"name":"safe"}`, 0o660)},
		)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got protectedJSONFixture
			if err := ReadProtectedJSONFile(tt.setup(t), &got); err == nil {
				t.Fatal("expected protected JSON input to be rejected")
			}
		})
	}
}

func TestReadProtectedJSONFileAcceptsStrictProtectedJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows acceptance uses an explicitly controlled DACL")
	}
	path := protectedJSONPath(`{"name":"safe"}`, 0o600)(t)
	var got protectedJSONFixture
	if err := ReadProtectedJSONFile(path, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "safe" {
		t.Fatalf("unexpected decoded value: %#v", got)
	}
}

func protectedJSONPath(body string, mode os.FileMode) func(*testing.T) string {
	return func(t *testing.T) string {
		path := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
}
