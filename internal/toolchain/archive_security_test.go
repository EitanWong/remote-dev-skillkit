package toolchain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsTrustedHTTPSURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"nil", "", false},
		{"plain https", "https://nodejs.org/dist/v20/node.tar.gz", true},
		{"http rejected", "http://nodejs.org/dist/v20/node.tar.gz", false},
		{"userinfo rejected", "https://user@nodejs.org/dist/v20/node.tar.gz", false},
		{"empty host rejected", "https:///dist/v20/node.tar.gz", false},
		{"garbage", "not a url", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parsed *url.URL
			if tc.raw != "" {
				u, err := url.Parse(tc.raw)
				if err != nil {
					t.Fatal(err)
				}
				parsed = u
			}
			if got := isTrustedHTTPSURL(parsed); got != tc.want {
				t.Fatalf("isTrustedHTTPSURL(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSafeArchivePathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	good, err := safeArchivePath(root, "node/bin/node")
	if err != nil || !strings.HasPrefix(good, root) {
		t.Fatalf("valid path rejected: %q %v", good, err)
	}
	for _, name := range []string{"", ".", "..", "../evil", "a/../../evil", "/etc/passwd", "a/../.."} {
		if _, err := safeArchivePath(root, name); err == nil {
			t.Fatalf("unsafe path %q accepted", name)
		}
	}
	// Note: backslash forms (e.g. `..\evil`) are literal filenames on Unix and
	// are caught by the Rel() escape check on Windows, where they are separators.
}

func TestWriteSafeSymlinkRejectsEscape(t *testing.T) {
	root := t.TempDir()
	for _, target := range []string{"", "/etc/passwd", "../../../etc/passwd", "a/../../../../etc/passwd"} {
		path := filepath.Join(root, "node", "bin", "node")
		if err := writeSafeSymlink(root, path, target); err == nil {
			t.Fatalf("unsafe symlink target %q accepted", target)
		}
	}
	// A symlink that stays inside the root is fine.
	path := filepath.Join(root, "node", "bin", "node-link")
	if err := writeSafeSymlink(root, path, "../node-real"); err != nil {
		t.Fatalf("safe symlink rejected: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink not created: %v %v", info, err)
	}
}

func tarGZFixture(t *testing.T, entries []struct {
	name     string
	kind     byte
	linkname string
	content  string
}) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o600, Typeflag: entry.kind, Linkname: entry.linkname, Size: int64(len(entry.content))}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.content != "" {
			if _, err := tw.Write([]byte(entry.content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractTarGZArchiveHappyPath(t *testing.T) {
	archive := tarGZFixture(t, []struct {
		name     string
		kind     byte
		linkname string
		content  string
	}{
		{name: "node/bin/node", kind: tar.TypeReg, content: "#!/bin/sh\n"},
		{name: "node/lib/x", kind: tar.TypeReg, content: "data"},
		{name: "node/lib", kind: tar.TypeDir},
	})
	dest := t.TempDir()
	if err := extractTarGZArchive(archive, dest, 1<<20); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dest, "node", "bin", "node"))
	if err != nil || !strings.HasPrefix(string(content), "#!/bin/sh") {
		t.Fatalf("extracted file wrong: %q %v", content, err)
	}
}

func TestExtractTarGZArchiveRejectsTraversal(t *testing.T) {
	archive := tarGZFixture(t, []struct {
		name     string
		kind     byte
		linkname string
		content  string
	}{
		{name: "../evil", kind: tar.TypeReg, content: "pwned"},
	})
	dest := t.TempDir()
	if err := extractTarGZArchive(archive, dest, 1<<20); err == nil {
		t.Fatal("expected traversal entry to fail")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil")); !os.IsNotExist(err) {
		t.Fatal("traversal file escaped extraction root")
	}
}

func TestExtractTarGZArchiveRejectsEscapingSymlink(t *testing.T) {
	archive := tarGZFixture(t, []struct {
		name     string
		kind     byte
		linkname string
		content  string
	}{
		{name: "node/link", kind: tar.TypeSymlink, linkname: "../../../etc/passwd"},
	})
	dest := t.TempDir()
	if err := extractTarGZArchive(archive, dest, 1<<20); err == nil {
		t.Fatal("expected escaping symlink to fail")
	}
}

func TestExtractTarGZArchiveEnforcesByteLimit(t *testing.T) {
	archive := tarGZFixture(t, []struct {
		name     string
		kind     byte
		linkname string
		content  string
	}{
		{name: "node/big", kind: tar.TypeReg, content: strings.Repeat("x", 4096)},
	})
	dest := t.TempDir()
	if err := extractTarGZArchive(archive, dest, 100); err == nil {
		t.Fatal("expected byte limit violation to fail")
	}
}

func TestExtractTarGZArchiveRejectsBadGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.tar.gz")
	if err := os.WriteFile(path, []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGZArchive(path, t.TempDir(), 1<<20); err == nil {
		t.Fatal("expected bad gzip to fail")
	}
}

func TestVerifyArchiveSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive")
	content := []byte("content")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	good := hex.EncodeToString(sum[:])
	if err := verifyArchiveSHA256(path, good); err != nil {
		t.Fatalf("matching hash rejected: %v", err)
	}
	if err := verifyArchiveSHA256(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong hash accepted")
	}
	if err := verifyArchiveSHA256(filepath.Join(t.TempDir(), "missing"), good); err == nil {
		t.Fatal("missing file accepted")
	}
}
