package depsinstall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestDownloadRetriesTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "busy", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("payload"))
	}))
	defer server.Close()

	out := filepath.Join(t.TempDir(), "tool")
	if err := download(context.Background(), server.Client(), server.URL+"/tool", out); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
	content, err := os.ReadFile(out)
	if err != nil || string(content) != "payload" {
		t.Fatalf("downloaded content wrong: %q %v", content, err)
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file not cleaned up")
	}
}

func TestDownloadFailsFastOnNonRetryableStatus(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	if err := download(context.Background(), server.Client(), server.URL+"/tool", filepath.Join(t.TempDir(), "tool")); err == nil {
		t.Fatal("expected 404 failure")
	}
	if attempts.Load() != 1 {
		t.Fatalf("expected single attempt, got %d", attempts.Load())
	}
}

func TestDownloadGivesUpAfterRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if err := download(context.Background(), server.Client(), server.URL+"/tool", filepath.Join(t.TempDir(), "tool")); err == nil {
		t.Fatal("expected failure after retries")
	}
}

func TestDownloadWritesWithoutLeavingTempOnServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	defer server.Close()

	out := filepath.Join(t.TempDir(), "tool")
	if err := download(context.Background(), server.Client(), server.URL+"/tool", out); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind on failure")
	}
}

func TestDownloadContextCancelStopsRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "busy", http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := download(ctx, server.Client(), server.URL+"/tool", filepath.Join(t.TempDir(), "tool")); err == nil {
		t.Fatal("expected context cancellation failure")
	}
	// A pre-cancelled context fails before any request reaches the server.
	if attempts.Load() != 0 {
		t.Fatalf("expected no network attempts on cancelled context, got %d", attempts.Load())
	}
}

func TestDownloadRejectsGarbageURL(t *testing.T) {
	if err := download(context.Background(), http.DefaultClient, "://bad", filepath.Join(t.TempDir(), "tool")); err == nil {
		t.Fatal("expected URL parse failure")
	}
}
