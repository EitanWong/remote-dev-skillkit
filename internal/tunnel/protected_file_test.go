package tunnel

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
