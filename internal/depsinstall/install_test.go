package depsinstall

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallDryRunReportsPlanOnly(t *testing.T) {
	report, err := Install(context.Background(), nil, Options{
		Tool:           "chisel",
		Scope:          "user",
		Platform:       "linux/amd64",
		URL:            "https://example.invalid/chisel.zip",
		ExpectedSHA256: strings.Repeat("a", 64),
		InstallDir:     filepath.Join(t.TempDir(), "tools"),
		Execute:        false,
		Now:            time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() || report.Executed || report.InstalledBinary != "" {
		t.Fatalf("expected dry-run report only, got %#v", report)
	}
}

func TestInstallDownloadsVerifiesAndExtractsChisel(t *testing.T) {
	archive := zipBytes(t, "chisel", "fake-chisel")
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	installDir := filepath.Join(t.TempDir(), "tools")
	report, err := Install(context.Background(), server.Client(), Options{
		Tool:           "chisel",
		Scope:          "workspace",
		Platform:       "linux/amd64",
		URL:            server.URL + "/chisel.zip",
		ExpectedSHA256: hex.EncodeToString(sum[:]),
		InstallDir:     installDir,
		Execute:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() || !report.Executed || report.InstalledBinary == "" {
		t.Fatalf("expected executed install report, got %#v", report)
	}
	content, err := os.ReadFile(report.InstalledBinary)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "fake-chisel" {
		t.Fatalf("unexpected binary content %q", string(content))
	}
}

func TestInstallRejectsChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not expected"))
	}))
	defer server.Close()
	_, err := Install(context.Background(), server.Client(), Options{
		Tool:           "frpc",
		Scope:          "user",
		Platform:       "linux/amd64",
		URL:            server.URL + "/frpc.tar.gz",
		ExpectedSHA256: strings.Repeat("b", 64),
		InstallDir:     filepath.Join(t.TempDir(), "tools"),
		Execute:        true,
	})
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestInstallRejectsPrivilegedTools(t *testing.T) {
	report, err := Install(context.Background(), nil, Options{
		Tool:           "wireguard",
		Scope:          "user",
		Platform:       "linux/amd64",
		URL:            "https://example.invalid/wg.zip",
		ExpectedSHA256: strings.Repeat("c", 64),
		InstallDir:     filepath.Join(t.TempDir(), "tools"),
		Execute:        false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected unsupported privileged tool to fail checks: %#v", report)
	}
}

func zipBytes(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	file, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
