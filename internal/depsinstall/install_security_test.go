package depsinstall

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsRetryableDownloadStatus(t *testing.T) {
	retryable := []int{http.StatusRequestTimeout, http.StatusTooManyRequests, 500, 502, 503, 504, 599}
	for _, status := range retryable {
		if !isRetryableDownloadStatus(status) {
			t.Fatalf("status %d should be retryable", status)
		}
	}
	notRetryable := []int{200, 301, 400, 401, 403, 404, 409, 418, 451}
	for _, status := range notRetryable {
		if isRetryableDownloadStatus(status) {
			t.Fatalf("status %d should not be retryable", status)
		}
	}
}

func TestIsRetryableDownloadErr(t *testing.T) {
	if !isRetryableDownloadErr(retryableDownloadError{err: errors.New("boom")}) {
		t.Fatal("typed retryable error must be retryable")
	}
	for _, message := range []string{"unexpected EOF", "connection reset by peer", "broken pipe", "use of closed network connection"} {
		if !isRetryableDownloadErr(errors.New(message)) {
			t.Fatalf("%q should be retryable", message)
		}
	}
	if isRetryableDownloadErr(errors.New("permission denied")) {
		t.Fatal("unrelated error must not be retryable")
	}
}

func TestIsHexSHA256(t *testing.T) {
	valid := strings.Repeat("ab", 32)
	if !isHexSHA256(valid) {
		t.Fatal("valid sha256 rejected")
	}
	// Uppercase hex is valid input; verification elsewhere is case-insensitive.
	if !isHexSHA256(strings.ToUpper(valid)) {
		t.Fatal("uppercase sha256 rejected")
	}
	for _, value := range []string{"", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("zz", 32), "sha256:" + valid} {
		if isHexSHA256(value) {
			t.Fatalf("invalid hash %q accepted", value)
		}
	}
}

func TestSameFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !sameFilePath(path, path) {
		t.Fatal("same path should match")
	}
	if !sameFilePath(path, filepath.Join(dir, ".", "tool")) {
		t.Fatal("equivalent path should match")
	}
	if sameFilePath(path, filepath.Join(dir, "other")) {
		t.Fatal("different path should not match")
	}
}

func TestDefaultInstallDir(t *testing.T) {
	if got := defaultInstallDir("workspace"); got != filepath.Join(".rdev", "tools") {
		t.Fatalf("workspace scope: %q", got)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := defaultInstallDir("user"); got != filepath.Join(home, ".rdev", "tools") {
		t.Fatalf("user scope: %q", got)
	}
}

func TestCopyToFileUsesTempAndRename(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "bin", "tool")
	path, err := copyToFile(strings.NewReader("payload"), out)
	if err != nil {
		t.Fatal(err)
	}
	if path != out {
		t.Fatalf("copy path = %q, want %q", path, out)
	}
	content, err := os.ReadFile(out)
	if err != nil || string(content) != "payload" {
		t.Fatalf("copied content wrong: %q %v", content, err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("copied file mode wrong: %v %v", info, err)
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file must be removed by rename")
	}
}
