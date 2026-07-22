//go:build !rdev_bootstrap_focused

package assetdownload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type transportFunc func(context.Context, TransportRequest) (TransportResponse, error)

func (f transportFunc) Fetch(ctx context.Context, req TransportRequest) (TransportResponse, error) {
	return f(ctx, req)
}

type recordingTransport struct {
	requests []TransportRequest
	fetch    transportFunc
}

func (t *recordingTransport) Fetch(ctx context.Context, req TransportRequest) (TransportResponse, error) {
	t.requests = append(t.requests, req)
	return t.fetch(ctx, req)
}

func TestDownloadTransportIsRequired(t *testing.T) {
	dir := t.TempDir()
	_, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     filepath.Join(dir, "helper.exe"),
		ExpectedSHA256: sha256String([]byte("helper")),
	})
	if err == nil || !strings.Contains(err.Error(), "transport is required") {
		t.Fatalf("expected required transport error, got %v", err)
	}
}

func TestDownloadTransportStartsAtOffsetZero(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("fresh helper")
	outPath := filepath.Join(dir, "helper.exe")
	transport := &recordingTransport{fetch: func(_ context.Context, req TransportRequest) (TransportResponse, error) {
		return testTransportResponse(http.StatusOK, payload), nil
	}}

	result, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		ExpectedSize:   int64(len(payload)),
		Transport:      transport,
		MaxAttempts:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("expected one transport request, got %#v", transport.requests)
	}
	request := transport.requests[0]
	if request.URL != "https://mirror.example.invalid/helper.exe" || request.Offset != 0 || request.MaxBytes != int64(len(payload)) {
		t.Fatalf("unexpected initial transport request: %#v", request)
	}
	if result.SourceURL != request.URL || result.Resumed {
		t.Fatalf("unexpected initial download result: %#v", result)
	}
}

func TestDownloadTransportValidatesOptions(t *testing.T) {
	transport := transportFunc(func(_ context.Context, _ TransportRequest) (TransportResponse, error) {
		t.Fatal("invalid options must fail before fetching")
		return TransportResponse{}, nil
	})
	validDigest := sha256String([]byte("helper"))
	tests := []struct {
		name    string
		options func(string) Options
		message string
	}{
		{
			name: "digest",
			options: func(outputPath string) Options {
				return Options{OutputPath: outputPath, Transport: transport}
			},
			message: "expected sha256 is required",
		},
		{
			name: "output path",
			options: func(_ string) Options {
				return Options{ExpectedSHA256: validDigest, Transport: transport}
			},
			message: "output path is required",
		},
		{
			name: "expected size",
			options: func(outputPath string) Options {
				return Options{OutputPath: outputPath, ExpectedSHA256: validDigest, ExpectedSize: -1, Transport: transport}
			},
			message: "expected size must not be negative",
		},
		{
			name: "mirrors",
			options: func(outputPath string) Options {
				return Options{Mirrors: []Mirror{{URL: "  "}}, OutputPath: outputPath, ExpectedSHA256: validDigest, Transport: transport}
			},
			message: "at least one mirror is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Download(context.Background(), test.options(filepath.Join(t.TempDir(), "helper.exe")))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %q, got %v", test.message, err)
			}
		})
	}
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
		Transport: transportFunc(func(_ context.Context, req TransportRequest) (TransportResponse, error) {
			t.Fatalf("cache hit should not make transport request to %s", req.URL)
			return TransportResponse{}, nil
		}),
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

func TestDownloadTransportReusesVerifiedOutput(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("verified output")
	outPath := filepath.Join(dir, "helper.exe")
	if err := os.WriteFile(outPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	transport := transportFunc(func(_ context.Context, req TransportRequest) (TransportResponse, error) {
		t.Fatalf("verified output must not fetch %s", req.URL)
		return TransportResponse{}, nil
	})

	result, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		ExpectedSize:   int64(len(payload)),
		Transport:      transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FromCache || result.SourceURL != "output" || result.Bytes != int64(len(payload)) || result.SHA256 != sha256String(payload) {
		t.Fatalf("unexpected verified output result: %#v", result)
	}
	if len(result.Transcript) != 1 || result.Transcript[0].Phase != "output-hit" || result.Transcript[0].Bytes != int64(len(payload)) {
		t.Fatalf("unexpected verified output transcript: %#v", result.Transcript)
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
	transport := &recordingTransport{fetch: func(_ context.Context, req TransportRequest) (TransportResponse, error) {
		return testTransportResponse(http.StatusPartialContent, payload[req.Offset:]), nil
	}}

	result, err := Download(ctx, Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		Transport:      transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Offset != 6 {
		t.Fatalf("expected resume from exact partial size, got %#v", transport.requests)
	}
	if !result.Resumed {
		t.Fatalf("expected resumed download, got result=%#v", result)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected resumed content %q", string(got))
	}
}

func TestDownloadHTTPTransportResumesAfterInterruptedResponse(t *testing.T) {
	payload := bytes.Repeat([]byte("range-resume-fixture-"), 4096)
	half := len(payload) / 2
	requests := 0
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		ranges = append(ranges, request.Header.Get("Range"))
		if requests == 1 {
			connection, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = fmt.Fprintf(buffer, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: application/octet-stream\r\n\r\n", len(payload))
			_, _ = buffer.Write(payload[:half])
			_ = buffer.Flush()
			_ = connection.Close()
			return
		}
		if request.Header.Get("Range") != fmt.Sprintf("bytes=%d-", half) {
			t.Errorf("Range = %q, want bytes=%d-", request.Header.Get("Range"), half)
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)-half))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[half:])
	}))
	defer server.Close()

	outPath := filepath.Join(t.TempDir(), "runtime.exe")
	result, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: server.URL + "/runtime.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		ExpectedSize:   int64(len(payload)),
		Transport:      HTTPTransport{Client: server.Client()},
		MaxAttempts:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Resumed || requests != 2 || len(ranges) != 2 || ranges[0] != "" || ranges[1] != fmt.Sprintf("bytes=%d-", half) {
		t.Fatalf("unexpected Range recovery: result=%#v requests=%d ranges=%#v", result, requests, ranges)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, payload) {
		t.Fatal("resumed HTTP download bytes changed")
	}
}

func TestDownloadTransportRestartsWhenOffsetResponseIsOK(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("replacement helper")
	outPath := filepath.Join(dir, "helper.exe")
	if err := os.WriteFile(outPath+".part", []byte("stale-"), 0o600); err != nil {
		t.Fatal(err)
	}
	transport := &recordingTransport{fetch: func(_ context.Context, req TransportRequest) (TransportResponse, error) {
		return testTransportResponse(http.StatusOK, payload), nil
	}}

	result, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		ExpectedSize:   int64(len(payload)),
		Transport:      transport,
		MaxAttempts:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Offset != int64(len("stale-")) {
		t.Fatalf("expected offset request from partial size, got %#v", transport.requests)
	}
	if result.Resumed {
		t.Fatalf("status 200 response must restart rather than resume: %#v", result)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("restart did not replace stale partial: %q", got)
	}
}

func TestDownloadPromotesCompleteVerifiedPartialWithoutRequest(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("complete verified partial")
	outPath := filepath.Join(dir, "runtime.exe")
	if err := os.WriteFile(outPath+".part", payload, 0o600); err != nil {
		t.Fatal(err)
	}
	transport := transportFunc(func(_ context.Context, req TransportRequest) (TransportResponse, error) {
		t.Fatalf("complete verified partial must not request %s", req.URL)
		return TransportResponse{}, nil
	})

	result, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/runtime.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		ExpectedSize:   int64(len(payload)),
		Transport:      transport,
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
	transport := transportFunc(func(_ context.Context, req TransportRequest) (TransportResponse, error) {
		attempts[req.URL]++
		if req.URL == "https://first.example.invalid/helper.exe" {
			return TransportResponse{}, io.ErrUnexpectedEOF
		}
		if req.URL != "https://second.example.invalid/helper.exe" {
			t.Fatalf("unexpected URL %s", req.URL)
		}
		return testTransportResponse(http.StatusOK, payload), nil
	})

	result, err := Download(ctx, Options{
		Mirrors: []Mirror{
			{URL: "https://first.example.invalid/helper.exe"},
			{URL: "https://second.example.invalid/helper.exe"},
		},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String(payload),
		Transport:      transport,
		MaxAttempts:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceURL != "https://second.example.invalid/helper.exe" ||
		attempts["https://first.example.invalid/helper.exe"] != 1 ||
		attempts["https://second.example.invalid/helper.exe"] != 1 {
		t.Fatalf("expected second mirror fallback, got result=%#v attempts=%#v", result, attempts)
	}
}

func TestDownloadTransportRetriesRetryableFailure(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("retried helper")
	attempts := 0
	transport := &recordingTransport{fetch: func(_ context.Context, _ TransportRequest) (TransportResponse, error) {
		attempts++
		if attempts == 1 {
			return TransportResponse{}, io.ErrUnexpectedEOF
		}
		return testTransportResponse(http.StatusOK, payload), nil
	}}

	result, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     filepath.Join(dir, "helper.exe"),
		ExpectedSHA256: sha256String(payload),
		Transport:      transport,
		MaxAttempts:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || len(result.Transcript) != 2 || result.Transcript[0].Phase != "download-error" || result.Transcript[1].Phase != "download-verified" {
		t.Fatalf("retry or transcript behavior changed: attempts=%d result=%#v", attempts, result)
	}
}

func TestDownloadTransportPreservesCancellation(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport := &recordingTransport{fetch: func(ctx context.Context, _ TransportRequest) (TransportResponse, error) {
		return TransportResponse{}, ctx.Err()
	}}

	_, err := Download(ctx, Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     filepath.Join(dir, "helper.exe"),
		ExpectedSHA256: sha256String([]byte("helper")),
		Transport:      transport,
		MaxAttempts:    3,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("canceled download should not retry, got %#v", transport.requests)
	}
}

func TestDownloadTransportRejectsNilResponseBody(t *testing.T) {
	dir := t.TempDir()
	_, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     filepath.Join(dir, "helper.exe"),
		ExpectedSHA256: sha256String([]byte("helper")),
		Transport: transportFunc(func(_ context.Context, _ TransportRequest) (TransportResponse, error) {
			return TransportResponse{StatusCode: http.StatusOK}, nil
		}),
		MaxAttempts: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "response body is required") {
		t.Fatalf("expected missing response body error, got %v", err)
	}
}

func TestDownloadTransportClosesResponseBodyWhenFetchReturnsError(t *testing.T) {
	dir := t.TempDir()
	fetchErr := errors.New("transport interrupted")
	body := &closeTrackingReadCloser{Reader: strings.NewReader("unused")}
	_, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     filepath.Join(dir, "helper.exe"),
		ExpectedSHA256: sha256String([]byte("helper")),
		Transport: transportFunc(func(_ context.Context, _ TransportRequest) (TransportResponse, error) {
			return TransportResponse{Body: body}, fetchErr
		}),
		MaxAttempts: 1,
	})
	if !errors.Is(err, fetchErr) {
		t.Fatalf("expected fetch error, got %v", err)
	}
	if !body.Closed {
		t.Fatal("transport response body was not closed after fetch error")
	}
}

func TestDownloadTransportPropagatesResponseBodyCloseFailure(t *testing.T) {
	payload := []byte("verified payload")
	closeErr := errors.New("temporary response cleanup failed")
	body := &errorCloseReadCloser{Reader: bytes.NewReader(payload), Err: closeErr}
	_, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     filepath.Join(t.TempDir(), "helper.exe"),
		ExpectedSHA256: sha256String(payload),
		ExpectedSize:   int64(len(payload)),
		Transport: transportFunc(func(_ context.Context, _ TransportRequest) (TransportResponse, error) {
			return TransportResponse{StatusCode: http.StatusOK, ContentLength: int64(len(payload)), Body: body}, nil
		}),
		MaxAttempts: 1,
	})
	if err == nil || !errors.Is(err, closeErr) {
		t.Fatalf("response cleanup failure was not propagated: %v", err)
	}
}

func TestDownloadTransportRejectsStatusOutsideOKAndPartialContent(t *testing.T) {
	for _, status := range []int{http.StatusEarlyHints, http.StatusCreated, http.StatusRequestedRangeNotSatisfiable} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			dir := t.TempDir()
			outPath := filepath.Join(dir, "helper.exe")
			_, err := Download(context.Background(), Options{
				Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
				OutputPath:     outPath,
				ExpectedSHA256: sha256String([]byte("helper")),
				Transport: transportFunc(func(_ context.Context, _ TransportRequest) (TransportResponse, error) {
					return testTransportResponse(status, []byte("helper")), nil
				}),
				MaxAttempts: 1,
			})
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("status %d", status)) {
				t.Fatalf("expected HTTP %d rejection, got %v", status, err)
			}
			if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
				t.Fatalf("rejected response must not be promoted, stat err=%v", statErr)
			}
		})
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "helper.exe")
	transport := transportFunc(func(_ context.Context, _ TransportRequest) (TransportResponse, error) {
		return testTransportResponse(http.StatusOK, []byte("tampered helper")), nil
	})

	_, err := Download(ctx, Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/helper.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String([]byte("expected helper")),
		Transport:      transport,
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
			transport := transportFunc(func(_ context.Context, _ TransportRequest) (TransportResponse, error) {
				return TransportResponse{
					StatusCode:    http.StatusOK,
					ContentLength: test.contentLength,
					Body:          body,
				}, nil
			})

			_, err := Download(context.Background(), Options{
				Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/runtime.exe"}},
				OutputPath:     outPath,
				ExpectedSHA256: sha256String([]byte("xxxxxxxx")),
				ExpectedSize:   8,
				Transport:      transport,
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
	transport := transportFunc(func(_ context.Context, req TransportRequest) (TransportResponse, error) {
		t.Fatalf("oversized existing partial must fail before requesting %s", req.URL)
		return TransportResponse{}, nil
	})

	_, err := Download(context.Background(), Options{
		Mirrors:        []Mirror{{URL: "https://mirror.example.invalid/runtime.exe"}},
		OutputPath:     outPath,
		ExpectedSHA256: sha256String([]byte("12345678")),
		ExpectedSize:   8,
		Transport:      transport,
		MaxAttempts:    1,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds expected size") {
		t.Fatalf("expected oversized partial failure, got %v", err)
	}
	if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
		t.Fatalf("oversized existing partial must be removed, stat err=%v", statErr)
	}
}

func TestDownloadHTTPTransportAdaptsRangeAndResponse(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "request-context")
	body := io.NopCloser(strings.NewReader("payload"))
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != "https://mirror.example.invalid/helper.exe" {
			t.Fatalf("unexpected HTTP request: %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Range") != "bytes=7-" {
			t.Fatalf("unexpected Range header %q", req.Header.Get("Range"))
		}
		if req.Context().Value(struct{}{}) != "request-context" {
			t.Fatal("transport did not preserve request context")
		}
		return &http.Response{
			StatusCode:    http.StatusPartialContent,
			Status:        "206 Partial Content",
			ContentLength: 7,
			Body:          body,
			Request:       req,
		}, nil
	})}

	response, err := (HTTPTransport{Client: client}).Fetch(ctx, TransportRequest{
		URL:      "https://mirror.example.invalid/helper.exe",
		Offset:   7,
		MaxBytes: 14,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusPartialContent || response.ContentLength != 7 || response.Body != body {
		t.Fatalf("unexpected transport response: %#v", response)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadTransportPromotionFailurePreservesPreviousOutput(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "helper.exe")
	previous := []byte("previous verified helper")
	if err := os.WriteFile(outputPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}

	err := promotePart(filepath.Join(dir, "missing.part"), outputPath)
	if err == nil {
		t.Fatal("expected promotion to fail for a missing part")
	}
	got, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("previous output was removed before replacement failed: %v", readErr)
	}
	if !bytes.Equal(got, previous) {
		t.Fatalf("previous output changed after replacement failure: %q", got)
	}
}

func TestDefaultCachePathUsesUserCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "local-app-data"))

	path, ok := DefaultCachePath("rdev-host-windows-amd64.exe")
	if !ok {
		t.Fatal("expected default cache path")
	}
	if !strings.Contains(path, "remote-dev-skillkit") ||
		!strings.Contains(path, "helpers") ||
		filepath.Base(path) != "rdev-host-windows-amd64.exe" {
		t.Fatalf("unexpected cache path %q", path)
	}
	if _, ok := DefaultCachePath("../rdev.exe"); ok {
		t.Fatalf("path traversal asset should not produce cache path")
	}
	if _, ok := DefaultCachePath(`dir\rdev.exe`); ok {
		t.Fatalf("windows path separator asset should not produce cache path")
	}
}

func testTransportResponse(status int, body []byte) TransportResponse {
	return TransportResponse{
		StatusCode:    status,
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(bytes.NewReader(body)),
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

type closeTrackingReadCloser struct {
	io.Reader
	Closed bool
}

func (r *closeTrackingReadCloser) Close() error {
	r.Closed = true
	return nil
}

type errorCloseReadCloser struct {
	io.Reader
	Err error
}

func (reader *errorCloseReadCloser) Close() error {
	return reader.Err
}

func sha256String(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
