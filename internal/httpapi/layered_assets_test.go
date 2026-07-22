package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func TestLayeredAssetsServeOnlyExactConfiguredPaths(t *testing.T) {
	dir := t.TempDir()
	manifestContent := []byte("{\"schema_version\":\"rdev.layered-assets.v1\"}\n")
	manifestPath := filepath.Join(dir, "layered-assets.json")
	hostContent := []byte("signed Windows host runtime\n")
	hostPath := filepath.Join(dir, "rdev-host-windows-amd64.exe")
	for path, content := range map[string][]byte{
		manifestPath: manifestContent,
		hostPath:     hostContent,
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	server := NewServer(gateway.NewMemoryGateway())
	server.Assets.LayeredAssetManifestPath = manifestPath
	server.Assets.RdevHostWindowsAMD64Path = hostPath
	handler := server.Handler()

	for requestPath, want := range map[string][]byte{
		"/layered-assets.json":                manifestContent,
		"/assets/rdev-host-windows-amd64.exe": hostContent,
	} {
		rec := serveAssetRequestForTest(handler, requestPath)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want) {
			t.Fatalf("exact asset %q = %d %q, want 200 and %q", requestPath, rec.Code, rec.Body.Bytes(), want)
		}
	}

	for _, requestPath := range []string{
		"/layered-assets.json?download=1",
		"/layered-assets.json?",
		"/%6cayered-assets.json",
		"/layered%2dassets.json",
		"/assets/%2e%2e/layered-assets.json",
		"/assets/../layered-assets.json",
		"/layered-assets.json.sha256",
		"/layered-assets.json.extra",
		"/nested/layered-assets.json",
		"/layered-assets.json/",
	} {
		rec := serveAssetRequestForTest(handler, requestPath)
		if rec.Code == http.StatusOK || bytes.Contains(rec.Body.Bytes(), manifestContent) {
			t.Fatalf("manifest alias %q exposed configured content: %d %q", requestPath, rec.Code, rec.Body.Bytes())
		}
	}

	fragmentRequest := httptest.NewRequest(http.MethodGet, "/layered-assets.json", nil)
	fragmentRequest.URL.Fragment = "payload"
	fragmentResponse := httptest.NewRecorder()
	handler.ServeHTTP(fragmentResponse, fragmentRequest)
	if fragmentResponse.Code == http.StatusOK || bytes.Contains(fragmentResponse.Body.Bytes(), manifestContent) {
		t.Fatalf("fragment alias exposed configured content: %d %q", fragmentResponse.Code, fragmentResponse.Body.Bytes())
	}

	for _, requestPath := range []string{
		"/assets/rdev-host-windows-amd64.exe?download=1",
		"/assets/rdev-host-windows-amd64.exe?",
		"/assets/rdev-host-windows-amd64%2eexe",
		"/assets/%2e/rdev-host-windows-amd64.exe",
		"/assets/nested/rdev-host-windows-amd64.exe",
		"/assets/rdev-host-windows-amd64.exe.extra",
		"/assets/rdev-host-windows-amd64.exe/",
	} {
		rec := serveAssetRequestForTest(handler, requestPath)
		if rec.Code == http.StatusOK || bytes.Contains(rec.Body.Bytes(), hostContent) {
			t.Fatalf("core asset alias %q exposed configured content: %d %q", requestPath, rec.Code, rec.Body.Bytes())
		}
	}

	unconfigured := NewServer(gateway.NewMemoryGateway()).Handler()
	rec := serveAssetRequestForTest(unconfigured, "/layered-assets.json")
	if rec.Code == http.StatusOK || bytes.Contains(rec.Body.Bytes(), manifestContent) {
		t.Fatalf("unconfigured manifest path exposed content: %d %q", rec.Code, rec.Body.Bytes())
	}
}

func serveAssetRequestForTest(handler http.Handler, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
