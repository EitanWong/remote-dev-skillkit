package assetdownload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDownloadReusesVerifiedCache(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	payload := []byte("cached verified helper")
	cachePath := filepath.Join(dir, "helper.cache")
	outPath := filepath.Join(dir, "helper.exe")
	if err := os.WriteFile(cachePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Download(ctx, Options{
		Mirrors:        []Mirror{{URL: "https://example.invalid/helper.exe"}},
		OutputPath:     outPath,
		CachePath:      cachePath,
		ExpectedSHA256: sha256String(payload),
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatalf("cache hit should not make HTTP request to %s", req.URL.String())
			return nil, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FromCache || result.SourceURL != "cache" {
		t.Fatalf("expected cache reuse result, got %#v", result)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected output content %q", string(got))
	}
}

func TestDownloadResumesPartialFileWithRange(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	payload := []byte("0123456789abcdef")
	outPath := filepath.Join(dir, "helper.exe")
	if err := os.WriteFile(outPath+".part", payload[:6], 0o600); err != nil {
		t.Fatal(err)
	}
	var sawRange string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sawRange = r.Header.Get("Range")
		if sawRange != "bytes=6-" {
			t.Fatalf("expected resume range bytes=6-, got %q", sawRange)
		}
		return testResponse(r, http.StatusPartialContent, payload[6:]), nil
	})}

	result, err := Download(ctx, Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		Client:         client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Resumed || sawRange != "bytes=6-" {
		t.Fatalf("expected resumed download with Range, got result=%#v range=%q", result, sawRange)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected resumed content %q", string(got))
	}
}

func TestDownloadPromotesCompleteVerifiedPartialWithoutRequest(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("complete verified partial")
	outPath := filepath.Join(dir, "runtime.exe")
	if err := os.WriteFile(outPath+".part", payload, 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("complete verified partial must not request %s", req.URL)
		return nil, nil
	})}

	result, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/runtime.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		ExpectedSize:   int64(len(payload)),
		Client:         client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Resumed || result.FromCache || result.SourceURL != "partial" {
		t.Fatalf("unexpected complete partial result: %#v", result)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("promoted partial changed content: %q", got)
	}
	if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
		t.Fatalf("verified partial was not atomically promoted, stat err=%v", statErr)
	}
}

func TestDownloadFallsBackToSecondMirrorAfterEOF(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	payload := []byte("second mirror payload")
	outPath := filepath.Join(dir, "helper.exe")
	attempts := map[string]int{}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts[req.URL.Host]++
		if req.URL.Host == "first.example.invalid" {
			return nil, io.ErrUnexpectedEOF
		}
		if req.URL.Host != "second.example.invalid" {
			t.Fatalf("unexpected host %s", req.URL.Host)
		}
		return testResponse(req, http.StatusOK, payload), nil
	})}

	result, err := Download(ctx, Options{
		Mirrors: []Mirror{
			{URL: "https://first.example.invalid/helper.exe"},
			{URL: "https://second.example.invalid/helper.exe"},
		},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		Client:         client,
		MaxAttempts:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceURL != "https://second.example.invalid/helper.exe" ||
		attempts["first.example.invalid"] != 1 ||
		attempts["second.example.invalid"] != 1 {
		t.Fatalf("expected second mirror fallback, got result=%#v attempts=%#v", result, attempts)
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "helper.exe")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return testResponse(req, http.StatusOK, []byte("tampered helper")), nil
	})}

	_, err := Download(ctx, Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String([]byte("expected helper")),
		Client:         client,
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched output should not be promoted, stat err=%v", statErr)
	}
}

func TestDownloadRejectsResponseLargerThanExpectedSize(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int64
	}{
		{name: "declared content length", contentLength: 9},
		{name: "chunked body", contentLength: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			outPath := filepath.Join(dir, "runtime.exe")
			body := &countingReadCloser{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), 64))}
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				response := testResponse(req, http.StatusOK, nil)
				response.Body = body
				response.ContentLength = test.contentLength
				return response, nil
			})}

			_, err := Download(context.Background(), Options{
				Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/runtime.exe"}},
				OutputPath:     outPath,
				ExpectedSHA256: sha256String([]byte("xxxxxxxx")),
				ExpectedSize:   8,
				Client:         client,
				MaxAttempts:    1,
			})
			if err == nil || !strings.Contains(err.Error(), "exceeds expected size") {
				t.Fatalf("expected signed size limit failure, got %v", err)
			}
			if test.contentLength > 0 && body.BytesRead != 0 {
				t.Fatalf("declared oversized response should be rejected before reading, read %d bytes", body.BytesRead)
			}
			if test.contentLength < 0 && body.BytesRead > 9 {
				t.Fatalf("chunked oversized response read beyond the one-byte limit probe: %d", body.BytesRead)
			}
			if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
				t.Fatalf("oversized partial file must be removed, stat err=%v", statErr)
			}
		})
	}
}

func TestDownloadRejectsOversizedExistingPartialBeforeRequest(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "runtime.exe")
	if err := os.WriteFile(outPath+".part", []byte("123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("oversized existing partial must fail before requesting %s", req.URL)
		return nil, nil
	})}

	_, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/runtime.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String([]byte("12345678")),
		ExpectedSize:   8,
		Client:         client,
		MaxAttempts:    1,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds expected size") {
		t.Fatalf("expected oversized partial failure, got %v", err)
	}
	if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
		t.Fatalf("oversized existing partial must be removed, stat err=%v", statErr)
	}
}

func TestDefaultCachePathUsesUserCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "local-app-data"))

	path, ok := DefaultCachePath("rdev-windows-amd64.exe")
	if !ok {
		t.Fatal("expected default cache path")
	}
	if !strings.Contains(path, "remote-dev-skillkit") ||
		!strings.Contains(path, "helpers") ||
		filepath.Base(path) != "rdev-windows-amd64.exe" {
		t.Fatalf("unexpected cache path %q", path)
	}
	if _, ok := DefaultCachePath("../rdev.exe"); ok {
		t.Fatalf("path traversal asset should not produce cache path")
	}
	if _, ok := DefaultCachePath(`dir\rdev.exe`); ok {
		t.Fatalf("windows path separator asset should not produce cache path")
	}
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

type countingReadCloser struct {
	*bytes.Reader
	BytesRead int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.BytesRead += n
	return n, err
}

func (r *countingReadCloser) Close() error { return nil }

func sha256String(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
