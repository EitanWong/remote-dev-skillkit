package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const testManagedReleaseURL = "https://github.com/bdecrem/tunn3l/releases/download/test/tool.gz"

func TestTunn3lManagedAsset(t *testing.T) {
	if tunn3lManagedVersion != "v0.5.1" {
		t.Fatalf("version = %q", tunn3lManagedVersion)
	}
	want := map[string]managedToolAsset{
		"darwin/arm64": {Name: "tunn3l-darwin-arm64.gz", URL: "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-darwin-arm64.gz", CompressedSHA256: "360669bd64595709cdc111e9bf430040c4608ad823582d035f955464fa1f45e4"},
		"darwin/amd64": {Name: "tunn3l-darwin-x64.gz", URL: "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-darwin-x64.gz", CompressedSHA256: "35d559e55cbd40afcaf3acbe806020b7cafd9d8559d3fb6db2c3d16844c10bd6"},
		"linux/arm64":  {Name: "tunn3l-linux-arm64.gz", URL: "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-linux-arm64.gz", CompressedSHA256: "9df47cad6d1e09313e5b01f76c69e4cde0c901ee424fa7943269cd101db3b1e1"},
		"linux/amd64":  {Name: "tunn3l-linux-x64.gz", URL: "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-linux-x64.gz", CompressedSHA256: "902bc626033efb7bddde141542a145d95f55d256bd310e439bc71290a0ad6d58"},
	}
	for platform, expected := range want {
		goos, goarch, _ := strings.Cut(platform, "/")
		got, ok := tunn3lManagedAsset(goos, goarch)
		if !ok || got != expected {
			t.Fatalf("tunn3lManagedAsset(%q, %q) = %#v, %v, want %#v, true", goos, goarch, got, ok, expected)
		}
	}
	for _, platform := range [][2]string{{"windows", "amd64"}, {"linux", "386"}, {"freebsd", "arm64"}} {
		if got, ok := tunn3lManagedAsset(platform[0], platform[1]); ok || got != (managedToolAsset{}) {
			t.Fatalf("unsupported asset = %#v, %v", got, ok)
		}
	}
	if maxManagedToolCompressedBytes != 64<<20 || maxManagedToolExpandedBytes != 128<<20 {
		t.Fatalf("managed tool limits = %d/%d", maxManagedToolCompressedBytes, maxManagedToolExpandedBytes)
	}
}

func TestManagedGzipInstallerDefaultClientUsesWebPKI(t *testing.T) {
	client := newManagedToolHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil || transport.TLSClientConfig == nil {
		t.Fatalf("transport = %#v", client.Transport)
	}
	if client == http.DefaultClient || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || transport.TLSClientConfig.RootCAs != nil || !transport.DisableCompression {
		t.Fatalf("unsafe managed client: client=%p tls=%#v disableCompression=%v", client, transport.TLSClientConfig, transport.DisableCompression)
	}
}

func TestManagedGzipInstallerInstallsVerifiedExecutable(t *testing.T) {
	expanded := []byte("verified executable content")
	compressed := gzipBytes(t, expanded)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
		}
		if r.URL.Path != "/payload" {
			http.Redirect(w, r, "https://release-assets.githubusercontent.com/payload", http.StatusFound)
			return
		}
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	installer := managedInstallerForServer(t, server)
	root := managedToolTestRoot(t)
	path, err := installer.Ensure(context.Background(), root, managedAssetForTest("tool.gz", testManagedReleaseURL, compressed))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, expanded) {
		t.Fatalf("installed content = %q, err = %v", got, err)
	}
	if !strings.HasPrefix(filepath.Base(path), "tunn3l-") {
		t.Fatalf("installed path = %q", path)
	}
	if mode := mustStat(t, path).Mode().Perm(); runtime.GOOS != "windows" && mode != 0o700 {
		t.Fatalf("mode=%#o", mode)
	}
	if mode := mustStat(t, root).Mode().Perm(); runtime.GOOS != "windows" && mode != 0o700 {
		t.Fatalf("root mode=%#o", mode)
	}
	assertNoManagedToolTemps(t, root)
}

func TestManagedGzipInstallerReusesVerifiedCompressedCache(t *testing.T) {
	expanded := []byte("cached executable")
	compressed := gzipBytes(t, expanded)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	root := managedToolTestRoot(t)
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	asset := managedAssetForTest("cached.gz", testManagedReleaseURL, compressed)
	if err := os.WriteFile(filepath.Join(root, asset.Name), compressed, 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := managedInstallerForServer(t, server).Ensure(context.Background(), root, asset)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatalf("cache reuse made %d requests", requests.Load())
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, expanded) {
		t.Fatalf("installed content = %q, err = %v", got, err)
	}
}

func TestManagedGzipInstallerRejectsHTTPFailures(t *testing.T) {
	compressed := gzipBytes(t, []byte("tool"))
	t.Run("non-2xx", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()
		_, err := managedInstallerForServer(t, server).Ensure(context.Background(), managedToolTestRoot(t), managedAssetForTest("tool.gz", testManagedReleaseURL, compressed))
		if err == nil || !strings.Contains(err.Error(), "status") {
			t.Fatalf("non-2xx error = %v", err)
		}
	})
	t.Run("initial HTTP URL", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("HTTP URL reached the network")
		}))
		defer server.Close()
		asset := managedAssetForTest("tool.gz", strings.Replace(testManagedReleaseURL, "https://", "http://", 1), compressed)
		_, err := managedInstallerForServer(t, server).Ensure(context.Background(), managedToolTestRoot(t), asset)
		if err == nil || !strings.Contains(err.Error(), "HTTPS") {
			t.Fatalf("HTTP URL error = %v", err)
		}
	})
	t.Run("redirect downgrade", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://"+r.Host+"/payload", http.StatusFound)
		}))
		defer server.Close()
		_, err := managedInstallerForServer(t, server).Ensure(context.Background(), managedToolTestRoot(t), managedAssetForTest("tool.gz", testManagedReleaseURL, compressed))
		if err == nil || !strings.Contains(err.Error(), "HTTPS") {
			t.Fatalf("redirect downgrade error = %v", err)
		}
	})
	t.Run("more than five redirects", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			n, _ := strconv.Atoi(r.URL.Query().Get("n"))
			http.Redirect(w, r, fmt.Sprintf("/?n=%d", n+1), http.StatusFound)
		}))
		defer server.Close()
		_, err := managedInstallerForServer(t, server).Ensure(context.Background(), managedToolTestRoot(t), managedAssetForTest("tool.gz", testManagedReleaseURL, compressed))
		if err == nil || !strings.Contains(err.Error(), "redirect") {
			t.Fatalf("redirect-limit error = %v", err)
		}
		if requests.Load() != 6 {
			t.Fatalf("requests = %d, want 6", requests.Load())
		}
	})
	t.Run("exactly five redirects", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			n, _ := strconv.Atoi(r.URL.Query().Get("n"))
			if n < 5 {
				http.Redirect(w, r, fmt.Sprintf("/?n=%d", n+1), http.StatusFound)
				return
			}
			_, _ = w.Write(compressed)
		}))
		defer server.Close()
		path, err := managedInstallerForServer(t, server).Ensure(context.Background(), managedToolTestRoot(t), managedAssetForTest("tool.gz", testManagedReleaseURL, compressed))
		if err != nil {
			t.Fatal(err)
		}
		if requests.Load() != 6 || path == "" {
			t.Fatalf("requests = %d, path = %q", requests.Load(), path)
		}
	})
}

func TestManagedGzipInstallerRejectsUnsafeInputs(t *testing.T) {
	compressed := gzipBytes(t, []byte("tool"))
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	installer := managedInstallerForServer(t, server)
	for _, name := range []string{"", ".", "..", "tool..gz", "../tool.gz", "/tmp/tool.gz", `dir\tool.gz`, "dir/tool.gz", "tool.gz:stream", " tool.gz"} {
		t.Run("name "+strconv.Quote(name), func(t *testing.T) {
			asset := managedAssetForTest(name, testManagedReleaseURL, compressed)
			if _, err := installer.Ensure(context.Background(), managedToolTestRoot(t), asset); err == nil {
				t.Fatal("unsafe asset name accepted")
			}
		})
	}
	for _, rawURL := range []string{
		"https://github.com.evil.test/tool.gz",
		"https://github.com:444/tool.gz",
		"https://user@github.com/tool.gz",
	} {
		t.Run("URL "+rawURL, func(t *testing.T) {
			asset := managedAssetForTest("tool.gz", rawURL, compressed)
			if _, err := installer.Ensure(context.Background(), managedToolTestRoot(t), asset); err == nil {
				t.Fatal("unsafe asset URL accepted")
			}
		})
	}
	if _, err := installer.Ensure(context.Background(), "", managedAssetForTest("tool.gz", testManagedReleaseURL, compressed)); err == nil {
		t.Fatal("empty root accepted")
	}
	base := filepath.Dir(managedToolTestRoot(t))
	separator := string(filepath.Separator)
	for _, root := range []string{
		base + separator + "nested" + separator + ".." + separator + "tools",
		base + separator + "tools:stream",
	} {
		if _, err := installer.Ensure(context.Background(), root, managedAssetForTest("tool.gz", testManagedReleaseURL, compressed)); err == nil {
			t.Fatalf("unsafe root %q accepted", root)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("unsafe inputs made %d requests", requests.Load())
	}
}

func TestManagedGzipInstallerRejectsSizeAndIntegrityFailures(t *testing.T) {
	t.Run("compressed body over limit", func(t *testing.T) {
		body := bytes.Repeat([]byte("x"), 65)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
		defer server.Close()
		installer := managedInstallerForServer(t, server)
		installer.compressedLimit = 64
		root := managedToolTestRoot(t)
		_, err := installer.Ensure(context.Background(), root, managedAssetForTest("tool.gz", testManagedReleaseURL, body))
		if err == nil || !strings.Contains(err.Error(), "compressed") {
			t.Fatalf("compressed-limit error = %v", err)
		}
		assertNoManagedToolTemps(t, root)
	})
	t.Run("expanded body over limit", func(t *testing.T) {
		expanded := bytes.Repeat([]byte("y"), 65)
		compressed := gzipBytes(t, expanded)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(compressed) }))
		defer server.Close()
		installer := managedInstallerForServer(t, server)
		installer.expandedLimit = 64
		root := managedToolTestRoot(t)
		_, err := installer.Ensure(context.Background(), root, managedAssetForTest("tool.gz", testManagedReleaseURL, compressed))
		if err == nil || !strings.Contains(err.Error(), "expanded") {
			t.Fatalf("expanded-limit error = %v", err)
		}
		assertNoManagedToolTemps(t, root)
	})
	t.Run("download digest mismatch", func(t *testing.T) {
		compressed := gzipBytes(t, []byte("tool"))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(compressed) }))
		defer server.Close()
		asset := managedAssetForTest("tool.gz", testManagedReleaseURL, compressed)
		asset.CompressedSHA256 = strings.Repeat("0", 64)
		root := managedToolTestRoot(t)
		_, err := managedInstallerForServer(t, server).Ensure(context.Background(), root, asset)
		if err == nil || !strings.Contains(err.Error(), "digest") {
			t.Fatalf("digest-mismatch error = %v", err)
		}
		assertNoManagedToolTemps(t, root)
	})
	t.Run("corrupt cached gzip", func(t *testing.T) {
		compressed := gzipBytes(t, []byte("tool"))
		var requests atomic.Int32
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			_, _ = w.Write(compressed)
		}))
		defer server.Close()
		root := managedToolTestRoot(t)
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		asset := managedAssetForTest("tool.gz", testManagedReleaseURL, compressed)
		if err := os.WriteFile(filepath.Join(root, asset.Name), []byte("corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := managedInstallerForServer(t, server).Ensure(context.Background(), root, asset)
		if err == nil || !strings.Contains(err.Error(), "digest") {
			t.Fatalf("corrupt-cache error = %v", err)
		}
		if requests.Load() != 0 {
			t.Fatalf("corrupt cache triggered %d requests", requests.Load())
		}
	})
	t.Run("corrupt existing executable", func(t *testing.T) {
		expanded := []byte("tool")
		compressed := gzipBytes(t, expanded)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(compressed) }))
		defer server.Close()
		root := managedToolTestRoot(t)
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		asset := managedAssetForTest("tool.gz", testManagedReleaseURL, compressed)
		if err := os.WriteFile(filepath.Join(root, asset.Name), compressed, 0o600); err != nil {
			t.Fatal(err)
		}
		expandedDigest := sha256.Sum256(expanded)
		executablePath := filepath.Join(root, "tunn3l-"+hex.EncodeToString(expandedDigest[:]))
		if err := os.WriteFile(executablePath, []byte("corrupt"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(executablePath, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := managedInstallerForServer(t, server).Ensure(context.Background(), root, asset)
		if err == nil || !strings.Contains(err.Error(), "digest") {
			t.Fatalf("corrupt executable error = %v", err)
		}
		if got, readErr := os.ReadFile(executablePath); readErr != nil || string(got) != "corrupt" {
			t.Fatalf("corrupt executable was replaced: content=%q err=%v", got, readErr)
		}
	})
	t.Run("gzip checksum mismatch", func(t *testing.T) {
		compressed := gzipBytes(t, []byte("tool"))
		compressed[len(compressed)-1] ^= 0xff
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(compressed) }))
		defer server.Close()
		root := managedToolTestRoot(t)
		_, err := managedInstallerForServer(t, server).Ensure(context.Background(), root, managedAssetForTest("tool.gz", testManagedReleaseURL, compressed))
		if err == nil || !strings.Contains(err.Error(), "gzip") {
			t.Fatalf("gzip checksum error = %v", err)
		}
		assertNoManagedToolTemps(t, root)
	})
}

func TestManagedGzipInstallerHonorsCanceledContext(t *testing.T) {
	compressed := gzipBytes(t, []byte("tool"))
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := managedInstallerForServer(t, server).Ensure(ctx, managedToolTestRoot(t), managedAssetForTest("tool.gz", testManagedReleaseURL, compressed))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

func TestManagedGzipInstallerConcurrentEnsure(t *testing.T) {
	expanded := []byte("concurrent verified executable")
	compressed := gzipBytes(t, expanded)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(compressed)
	}))
	defer server.Close()
	installer := managedInstallerForServer(t, server)
	asset := managedAssetForTest("tool.gz", testManagedReleaseURL, compressed)
	root := managedToolTestRoot(t)

	const workers = 12
	paths := make([]string, workers)
	errs := make([]error, workers)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for index := range workers {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			paths[index], errs[index] = installer.Ensure(context.Background(), root, asset)
		}(index)
	}
	close(start)
	wait.Wait()
	for index := range workers {
		if errs[index] != nil {
			t.Fatalf("worker %d: %v", index, errs[index])
		}
		if paths[index] != paths[0] {
			t.Fatalf("worker %d path = %q, want %q", index, paths[index], paths[0])
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
	if got, err := os.ReadFile(paths[0]); err != nil || !bytes.Equal(got, expanded) {
		t.Fatalf("installed content = %q, err = %v", got, err)
	}
	assertNoManagedToolTemps(t, root)
}

func TestPublishManagedToolNoReplace(t *testing.T) {
	t.Run("existing target wins without replacement", func(t *testing.T) {
		root := managedToolTestRoot(t)
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		tempPath := filepath.Join(root, ".tool.tmp")
		targetPath := filepath.Join(root, "tool")
		if err := os.WriteFile(tempPath, []byte("candidate"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(targetPath, []byte("winner"), 0o600); err != nil {
			t.Fatal(err)
		}
		published, err := publishManagedToolNoReplace(tempPath, targetPath)
		if err != nil {
			t.Fatal(err)
		}
		if published {
			t.Fatal("existing target was reported as newly published")
		}
		if got, err := os.ReadFile(targetPath); err != nil || string(got) != "winner" {
			t.Fatalf("existing target changed: content=%q err=%v", got, err)
		}
		if _, err := os.Lstat(tempPath); !os.IsNotExist(err) {
			t.Fatalf("temporary candidate remains: %v", err)
		}
	})

	t.Run("missing target is atomically published", func(t *testing.T) {
		root := managedToolTestRoot(t)
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		tempPath := filepath.Join(root, ".tool.tmp")
		targetPath := filepath.Join(root, "tool")
		if err := os.WriteFile(tempPath, []byte("candidate"), 0o600); err != nil {
			t.Fatal(err)
		}
		published, err := publishManagedToolNoReplace(tempPath, targetPath)
		if err != nil {
			t.Fatal(err)
		}
		if !published {
			t.Fatal("missing target was not published")
		}
		if got, err := os.ReadFile(targetPath); err != nil || string(got) != "candidate" {
			t.Fatalf("published target content=%q err=%v", got, err)
		}
		if _, err := os.Lstat(tempPath); !os.IsNotExist(err) {
			t.Fatalf("temporary candidate remains: %v", err)
		}
	})
}

func managedInstallerForServer(t *testing.T, server *httptest.Server) managedGzipInstaller {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := *server.Client()
	client.Transport = managedToolRewriteTransport{target: parsed, base: client.Transport}
	return managedGzipInstaller{client: &client}
}

func managedToolTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "tools")
}

type managedToolRewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (transport managedToolRewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	rewritten := request.Clone(request.Context())
	rewrittenURL := *request.URL
	rewrittenURL.Scheme = transport.target.Scheme
	rewrittenURL.Host = transport.target.Host
	rewritten.URL = &rewrittenURL
	rewritten.Host = ""
	response, err := transport.base.RoundTrip(rewritten)
	if response != nil {
		response.Request = request
	}
	return response, err
}

func managedAssetForTest(name, rawURL string, compressed []byte) managedToolAsset {
	digest := sha256.Sum256(compressed)
	return managedToolAsset{Name: name, URL: rawURL, CompressedSHA256: hex.EncodeToString(digest[:])}
}

func gzipBytes(t *testing.T, expanded []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(expanded); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func assertNoManagedToolTemps(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Fatalf("temporary file remains: %q", entry.Name())
		}
	}
}
