package bootstrapcmd

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
)

func TestUpgradePreconnectsDownloadsAndVerifiesHelper(t *testing.T) {
	helper := []byte("fake verified rdev-host helper\n")
	sum := sha256.Sum256(helper)
	expectedSHA := hex.EncodeToString(sum[:])
	gzipped := gzipBytes(t, helper)
	var preconnect map[string]any
	gatewayURL := "https://gateway.test"

	client := testHTTPClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/support-session/preconnect":
			if err := json.NewDecoder(r.Body).Decode(&preconnect); err != nil {
				t.Fatalf("decode preconnect: %v", err)
			}
			return testResponse(r, http.StatusAccepted, nil), nil
		case "/assets/rdev-host-test":
			return testResponse(r, http.StatusNotFound, []byte("not found")), nil
		case "/assets/rdev-host-test.gz":
			return testResponse(r, http.StatusOK, gzipped), nil
		case "/assets/rdev-host-test.sha256":
			return testResponse(r, http.StatusOK, []byte(expectedSHA+"\n")), nil
		default:
			return testResponse(r, http.StatusNotFound, []byte("not found")), nil
		}
	})

	out := filepath.Join(t.TempDir(), "rdev-host-test")
	var stdout bytes.Buffer
	app := App{Stdout: &stdout, Stderr: &bytes.Buffer{}, Client: client}
	err := app.Run(t.Context(), []string{
		"upgrade",
		"--gateway-url", gatewayURL,
		"--ticket-code", "ABCD-1234",
		"--asset", "rdev-host-test",
		"--out", out,
		"--no-exec",
	})
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, helper) {
		t.Fatalf("downloaded helper mismatch: got %q want %q", written, helper)
	}
	if preconnect["ticket_code"] != "ABCD-1234" ||
		preconnect["phase"] != "downloading-helper" ||
		preconnect["asset"] != "rdev-host-test" ||
		preconnect["source"] != "rdev-bootstrap-native" {
		t.Fatalf("unexpected preconnect body: %#v", preconnect)
	}
	if !strings.Contains(stdout.String(), `"verified":true`) ||
		!strings.Contains(stdout.String(), `"executed":false`) {
		t.Fatalf("expected no-exec JSON status, got %s", stdout.String())
	}
}

func TestUpgradeRejectsSHA256MismatchAndRemovesHelper(t *testing.T) {
	helper := []byte("fake verified rdev-host helper\n")
	gzipped := gzipBytes(t, helper)
	gatewayURL := "https://gateway.test"
	client := testHTTPClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/support-session/preconnect":
			return testResponse(r, http.StatusAccepted, nil), nil
		case "/assets/rdev-host-test":
			return testResponse(r, http.StatusNotFound, []byte("not found")), nil
		case "/assets/rdev-host-test.gz":
			return testResponse(r, http.StatusOK, gzipped), nil
		case "/assets/rdev-host-test.sha256":
			return testResponse(r, http.StatusOK, []byte(strings.Repeat("0", 64)+"\n")), nil
		default:
			return testResponse(r, http.StatusNotFound, []byte("not found")), nil
		}
	})

	out := filepath.Join(t.TempDir(), "rdev-host-test")
	app := App{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Client: client}
	err := app.Run(t.Context(), []string{
		"upgrade",
		"--gateway-url", gatewayURL,
		"--ticket-code", "ABCD-1234",
		"--asset", "rdev-host-test",
		"--out", out,
		"--no-exec",
	})
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("expected SHA-256 mismatch, got %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("expected mismatched helper to be removed, stat err=%v", statErr)
	}
}

func TestUpgradeUsesMirrorFallbackAndCache(t *testing.T) {
	helper := []byte("raw helper from second mirror\n")
	sum := sha256.Sum256(helper)
	expectedSHA := hex.EncodeToString(sum[:])
	var preconnects int
	var firstMirrorHits int
	var secondMirrorHits int

	gatewayURL := "https://gateway.test"
	firstMirrorURL := "https://first-mirror.test/rdev-host-test"
	secondMirrorURL := "https://second-mirror.test/rdev-host-test"
	client := testHTTPClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "gateway.test":
			switch r.URL.Path {
			case "/v1/support-session/preconnect":
				preconnects++
				return testResponse(r, http.StatusAccepted, nil), nil
			case "/assets/rdev-host-test.sha256":
				return testResponse(r, http.StatusOK, []byte(expectedSHA+"\n")), nil
			default:
				return testResponse(r, http.StatusNotFound, []byte("not found")), nil
			}
		case "first-mirror.test":
			firstMirrorHits++
			return testResponse(r, http.StatusBadGateway, []byte("weak mirror")), nil
		case "second-mirror.test":
			secondMirrorHits++
			return testResponse(r, http.StatusOK, helper), nil
		default:
			t.Fatalf("unexpected host %s", r.URL.Host)
			return nil, nil
		}
	})

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache", "rdev-host-test")
	out := filepath.Join(dir, "rdev-host-test")
	var stdout bytes.Buffer
	app := App{Stdout: &stdout, Stderr: &bytes.Buffer{}, Client: client}
	err := app.Run(t.Context(), []string{
		"upgrade",
		"--gateway-url", gatewayURL,
		"--ticket-code", "ABCD-1234",
		"--asset", "rdev-host-test",
		"--mirror", firstMirrorURL,
		"--mirror", secondMirrorURL,
		"--cache", cachePath,
		"--out", out,
		"--no-exec",
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstMirrorHits != 3 || secondMirrorHits != 1 {
		t.Fatalf("expected first mirror retries then second mirror success, first=%d second=%d", firstMirrorHits, secondMirrorHits)
	}
	if cached, err := os.ReadFile(cachePath); err != nil || !bytes.Equal(cached, helper) {
		t.Fatalf("expected verified cache copy, content=%q err=%v", string(cached), err)
	}
	if !strings.Contains(stdout.String(), `"source_url":"`+secondMirrorURL+`"`) ||
		!strings.Contains(stdout.String(), `"from_cache":false`) {
		t.Fatalf("expected no-exec JSON to include mirror download result, got %s", stdout.String())
	}

	stdout.Reset()
	if err := os.Remove(out); err != nil {
		t.Fatal(err)
	}
	err = app.Run(t.Context(), []string{
		"upgrade",
		"--gateway-url", gatewayURL,
		"--ticket-code", "ABCD-1234",
		"--asset", "rdev-host-test",
		"--mirror", firstMirrorURL,
		"--mirror", secondMirrorURL,
		"--cache", cachePath,
		"--out", out,
		"--no-exec",
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstMirrorHits != 3 || secondMirrorHits != 1 {
		t.Fatalf("expected cache hit to avoid mirror requests, first=%d second=%d", firstMirrorHits, secondMirrorHits)
	}
	if !strings.Contains(stdout.String(), `"source_url":"cache"`) ||
		!strings.Contains(stdout.String(), `"from_cache":true`) ||
		preconnects != 2 {
		t.Fatalf("expected cache hit result and preconnect per run, preconnects=%d stdout=%s", preconnects, stdout.String())
	}
}

func TestUpgradeUsesDefaultCacheWhenCacheFlagOmitted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "local-app-data"))

	helper := []byte("helper cached by default path\n")
	sum := sha256.Sum256(helper)
	expectedSHA := hex.EncodeToString(sum[:])
	var rawAssetHits int
	var preconnects int

	gatewayURL := "https://gateway.test"
	client := testHTTPClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/support-session/preconnect":
			preconnects++
			return testResponse(r, http.StatusAccepted, nil), nil
		case "/assets/rdev-host-test.sha256":
			return testResponse(r, http.StatusOK, []byte(expectedSHA+"\n")), nil
		case "/assets/rdev-host-test":
			rawAssetHits++
			return testResponse(r, http.StatusOK, helper), nil
		default:
			return testResponse(r, http.StatusNotFound, []byte("not found")), nil
		}
	})

	cachePath, ok := assetdownload.DefaultCachePath("rdev-host-test")
	if !ok {
		t.Fatal("expected default cache path")
	}
	out := filepath.Join(dir, "first-helper")
	var stdout bytes.Buffer
	app := App{Stdout: &stdout, Stderr: &bytes.Buffer{}, Client: client}
	err := app.Run(t.Context(), []string{
		"upgrade",
		"--gateway-url", gatewayURL,
		"--ticket-code", "ABCD-1234",
		"--asset", "rdev-host-test",
		"--out", out,
		"--no-exec",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rawAssetHits != 1 {
		t.Fatalf("expected first run to download raw asset once, got %d", rawAssetHits)
	}
	if cached, err := os.ReadFile(cachePath); err != nil || !bytes.Equal(cached, helper) {
		t.Fatalf("expected default cache file, content=%q err=%v", string(cached), err)
	}

	stdout.Reset()
	out = filepath.Join(dir, "second-helper")
	err = app.Run(t.Context(), []string{
		"upgrade",
		"--gateway-url", gatewayURL,
		"--ticket-code", "ABCD-1234",
		"--asset", "rdev-host-test",
		"--out", out,
		"--no-exec",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rawAssetHits != 1 || preconnects != 2 {
		t.Fatalf("expected second run to reuse cache after preconnect, raw hits=%d preconnects=%d", rawAssetHits, preconnects)
	}
	if !strings.Contains(stdout.String(), `"source_url":"cache"`) ||
		!strings.Contains(stdout.String(), `"from_cache":true`) {
		t.Fatalf("expected default cache hit in JSON, got %s", stdout.String())
	}
}

func TestUpgradeExecutesDownloadedRelativeHelperPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell fixture")
	}
	helper := []byte("#!/bin/sh\nprintf executed > \"$1\"\n")
	sum := sha256.Sum256(helper)
	expectedSHA := hex.EncodeToString(sum[:])
	gzipped := gzipBytes(t, helper)
	gatewayURL := "https://gateway.test"
	client := testHTTPClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/support-session/preconnect":
			return testResponse(r, http.StatusAccepted, nil), nil
		case "/assets/rdev-host-test":
			return testResponse(r, http.StatusNotFound, []byte("not found")), nil
		case "/assets/rdev-host-test.gz":
			return testResponse(r, http.StatusOK, gzipped), nil
		case "/assets/rdev-host-test.sha256":
			return testResponse(r, http.StatusOK, []byte(expectedSHA+"\n")), nil
		default:
			return testResponse(r, http.StatusNotFound, []byte("not found")), nil
		}
	})

	dir := t.TempDir()
	t.Chdir(dir)
	marker := filepath.Join(dir, "marker.txt")
	app := App{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Client: client}
	err := app.Run(t.Context(), []string{
		"upgrade",
		"--gateway-url", gatewayURL,
		"--ticket-code", "ABCD-1234",
		"--asset", "rdev-host-test",
		"--out", "downloaded-helper",
		"--", marker,
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "executed" {
		t.Fatalf("expected downloaded helper to execute, got %q", content)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testHTTPClient(handler func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: roundTripFunc(handler)}
}

func testResponse(req *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func gzipBytes(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
