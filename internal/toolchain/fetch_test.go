package toolchain

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
)

// withTestTLSTransport swaps http.DefaultTransport to the httptest TLS client
// transport so httpArtifactFetcher's default client trusts the test cert.
func withTestTLSTransport(t *testing.T, server *httptest.Server) {
	t.Helper()
	client := server.Client()
	previous := http.DefaultTransport
	http.DefaultTransport = client.Transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func TestFetchHappyPath(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("archive-bytes"))
	}))
	defer server.Close()
	withTestTLSTransport(t, server)

	dest := filepath.Join(t.TempDir(), "node.tar.gz")
	err := (httpArtifactFetcher{}).Fetch(context.Background(), contracts.ToolchainNodeSource{
		URL:    server.URL + "/node.tar.gz",
		SHA256: strings.Repeat("ab", 32),
	}, dest, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(dest)
	if err != nil || string(content) != "archive-bytes" {
		t.Fatalf("downloaded content wrong: %q %v", content, err)
	}
	info, err := os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode wrong: %v %v", info, err)
	}
}

func TestFetchRejectsUntrustedSource(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "node.tar.gz")
	for _, source := range []contracts.ToolchainNodeSource{
		{URL: "http://nodejs.org/dist/node.tar.gz"},
		{URL: "https://user@nodejs.org/dist/node.tar.gz"},
		{URL: "not a url"},
	} {
		if err := (httpArtifactFetcher{}).Fetch(context.Background(), source, dest, 1<<20); err == nil {
			t.Fatalf("untrusted source accepted: %q", source.URL)
		}
	}
}

func TestFetchRejectsCrossHostRedirect(t *testing.T) {
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("evil"))
	}))
	defer evil.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/node.tar.gz", http.StatusFound)
	}))
	defer server.Close()
	withTestTLSTransport(t, server)

	dest := filepath.Join(t.TempDir(), "node.tar.gz")
	err := (httpArtifactFetcher{}).Fetch(context.Background(), contracts.ToolchainNodeSource{
		URL: server.URL + "/node.tar.gz",
	}, dest, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("expected cross-host redirect rejection, got %v", err)
	}
}

func TestFetchRejectsNonOKStatus(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()
	withTestTLSTransport(t, server)

	dest := filepath.Join(t.TempDir(), "node.tar.gz")
	if err := (httpArtifactFetcher{}).Fetch(context.Background(), contracts.ToolchainNodeSource{
		URL: server.URL + "/node.tar.gz",
	}, dest, 1<<20); err == nil {
		t.Fatal("expected non-200 rejection")
	}
}

func TestFetchEnforcesArchiveByteLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 4096))
	}))
	defer server.Close()
	withTestTLSTransport(t, server)

	dest := filepath.Join(t.TempDir(), "node.tar.gz")
	err := (httpArtifactFetcher{}).Fetch(context.Background(), contracts.ToolchainNodeSource{
		URL: server.URL + "/node.tar.gz",
	}, dest, 100)
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("expected byte limit rejection, got %v", err)
	}
}
