//go:build windows

package windowsentry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/release"
)

func TestWindowsEntryPrivateCacheDACLAndRuntimeLock(t *testing.T) {
	cacheRoot := windowsEntryTestCacheDir(t)
	asset := release.LayeredAsset{
		RelativePath: "rdev-core.exe",
		SHA256:       "sha256:" + strings.Repeat("a", 64),
		SizeBytes:    4,
	}
	outputPath, _, err := cachePaths(cacheRoot, asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte("core"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateCacheFile(outputPath, 4); err != nil {
		t.Fatal(err)
	}
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsPrivatePath(filepath.Dir(outputPath), true, true, trustees); err != nil {
		t.Fatal(err)
	}
	runtimeFile, err := openPrivateRuntime(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(outputPath); err == nil {
		_ = runtimeFile.Close()
		t.Fatal("locked runtime allowed pathname deletion")
	}
	if err := runtimeFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(outputPath); err != nil {
		t.Fatalf("runtime remained locked after handle close: %v", err)
	}
}

func TestWindowsEntryPrivateCurlTemporaryFiles(t *testing.T) {
	directory, err := createPrivateTemporaryDirectory("rdev-bootstrap-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	file, err := createPrivateTemporaryFile(directory, "body.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateTemporaryFile(file, file.Name(), 4); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file.Name()); err == nil {
		_ = file.Close()
		t.Fatal("private temporary file allowed pathname deletion while its identity handle was open")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
}
