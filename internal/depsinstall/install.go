package depsinstall

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const ReportSchemaVersion = "rdev.dependency-install-report.v1"

type Options struct {
	Tool           string
	Scope          string
	Version        string
	Platform       string
	URL            string
	ExpectedSHA256 string
	InstallDir     string
	Execute        bool
	Now            time.Time
}

type Report struct {
	SchemaVersion      string    `json:"schema_version"`
	GeneratedAt        time.Time `json:"generated_at"`
	Tool               string    `json:"tool"`
	Scope              string    `json:"scope"`
	Version            string    `json:"version,omitempty"`
	Platform           string    `json:"platform"`
	URL                string    `json:"url"`
	ExpectedSHA256     string    `json:"expected_sha256,omitempty"`
	DownloadPath       string    `json:"download_path,omitempty"`
	InstallDir         string    `json:"install_dir"`
	InstalledBinary    string    `json:"installed_binary,omitempty"`
	Execute            bool      `json:"execute"`
	Executed           bool      `json:"executed"`
	Checks             []Check   `json:"checks"`
	RecommendedActions []string  `json:"recommended_actions,omitempty"`
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func Install(ctx context.Context, client *http.Client, opts Options) (Report, error) {
	opts = normalizeOptions(opts)
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	report := Report{
		SchemaVersion:  ReportSchemaVersion,
		GeneratedAt:    now.UTC(),
		Tool:           opts.Tool,
		Scope:          opts.Scope,
		Version:        opts.Version,
		Platform:       opts.Platform,
		URL:            opts.URL,
		ExpectedSHA256: opts.ExpectedSHA256,
		InstallDir:     opts.InstallDir,
		Execute:        opts.Execute,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	add("tool_supported", supportedTool(opts.Tool), opts.Tool)
	add("scope_user_or_workspace", opts.Scope == "user" || opts.Scope == "workspace", opts.Scope)
	add("platform_supported", supportedPlatform(opts.Platform), opts.Platform)
	add("download_url_present", strings.TrimSpace(opts.URL) != "", opts.URL)
	add("expected_sha256_present", isHexSHA256(opts.ExpectedSHA256), opts.ExpectedSHA256)
	add("install_dir_present", strings.TrimSpace(opts.InstallDir) != "", opts.InstallDir)
	add("no_privileged_install", true, "no service, driver, PATH, firewall, DNS, route, or policy mutation")
	report.RecommendedActions = []string{
		"Run with --execute only after reviewing the URL and SHA-256.",
		"Keep helper credentials and relay addresses outside public artifacts.",
		"Use explicit service or mesh enrollment plans for persistent or privileged networking.",
	}
	if !report.OK() {
		return report, nil
	}
	if !opts.Execute {
		return report, nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	if err := os.MkdirAll(opts.InstallDir, 0o700); err != nil {
		return report, err
	}
	downloadPath := filepath.Join(opts.InstallDir, filepath.Base(opts.URL))
	report.DownloadPath = downloadPath
	if err := download(ctx, client, opts.URL, downloadPath); err != nil {
		return report, err
	}
	digest, err := fileSHA256(downloadPath)
	if err != nil {
		return report, err
	}
	add("download_sha256_matches", strings.EqualFold(digest, opts.ExpectedSHA256), digest)
	if !strings.EqualFold(digest, opts.ExpectedSHA256) {
		return report, fmt.Errorf("download SHA-256 mismatch for %s", opts.Tool)
	}
	binaryPath, err := extractTool(downloadPath, opts.InstallDir, opts.Tool)
	if err != nil {
		return report, err
	}
	if err := os.Chmod(binaryPath, 0o700); err != nil {
		return report, err
	}
	report.InstalledBinary = binaryPath
	report.Executed = true
	add("binary_installed", true, binaryPath)
	return report, nil
}

func (r Report) OK() bool {
	if len(r.Checks) == 0 {
		return false
	}
	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func normalizeOptions(opts Options) Options {
	opts.Tool = normalizeTool(opts.Tool)
	if strings.TrimSpace(opts.Scope) == "" {
		opts.Scope = "user"
	}
	if strings.TrimSpace(opts.Platform) == "" {
		opts.Platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	opts.Platform = strings.ToLower(strings.TrimSpace(opts.Platform))
	opts.ExpectedSHA256 = strings.TrimSpace(opts.ExpectedSHA256)
	opts.URL = strings.TrimSpace(opts.URL)
	if strings.TrimSpace(opts.InstallDir) == "" {
		opts.InstallDir = defaultInstallDir(opts.Scope)
	}
	return opts
}

func normalizeTool(value string) string {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".exe")
	switch value {
	case "chisel":
		return "chisel"
	case "frp", "frpc":
		return "frpc"
	case "headscale-tailscale", "tailscale-compatible", "tailscale":
		return "tailscale"
	case "wireguard", "wireguard-tools", "wg", "wg-quick":
		return "wg"
	default:
		return value
	}
}

func supportedTool(tool string) bool {
	switch normalizeTool(tool) {
	case "chisel", "frpc", "tailscale", "wg":
		return true
	default:
		return false
	}
}

func supportedPlatform(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64":
		return true
	default:
		return false
	}
}

func defaultInstallDir(scope string) string {
	if strings.TrimSpace(scope) == "workspace" {
		return filepath.Join(".rdev", "tools")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".rdev", "tools")
	}
	return filepath.Join(home, ".rdev", "tools")
}

func download(ctx context.Context, client *http.Client, rawURL, out string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	tmp := out + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractTool(archivePath, installDir, tool string) (string, error) {
	tool = normalizeTool(tool)
	switch {
	case strings.HasSuffix(strings.ToLower(archivePath), ".zip"):
		return extractZipTool(archivePath, installDir, tool)
	case strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz"), strings.HasSuffix(strings.ToLower(archivePath), ".tgz"):
		return extractTarGzTool(archivePath, installDir, tool)
	default:
		base := executableName(tool)
		out := filepath.Join(installDir, base)
		if sameFilePath(archivePath, out) {
			return out, nil
		}
		return copyFile(archivePath, out)
	}
}

func extractZipTool(archivePath, installDir, tool string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if !archiveEntryMatchesTool(file.Name, tool) {
			continue
		}
		src, err := file.Open()
		if err != nil {
			return "", err
		}
		defer src.Close()
		return writeExtractedTool(src, installDir, tool)
	}
	return "", fmt.Errorf("%s not found in zip archive", tool)
}

func extractTarGzTool(archivePath, installDir, tool string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag != tar.TypeReg || !archiveEntryMatchesTool(header.Name, tool) {
			continue
		}
		return writeExtractedTool(tarReader, installDir, tool)
	}
	return "", fmt.Errorf("%s not found in tar.gz archive", tool)
}

func archiveEntryMatchesTool(name, tool string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(name)), ".exe")
	return base == normalizeTool(tool)
}

func executableName(tool string) string {
	if runtime.GOOS == "windows" {
		return normalizeTool(tool) + ".exe"
	}
	return normalizeTool(tool)
}

func writeExtractedTool(src io.Reader, installDir, tool string) (string, error) {
	out := filepath.Join(installDir, executableName(tool))
	return copyToFile(src, out)
}

func copyFile(srcPath, out string) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()
	return copyToFile(src, out)
}

func copyToFile(src io.Reader, out string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return "", err
	}
	tmp := out + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return "", err
	}
	if err := dst.Close(); err != nil {
		return "", err
	}
	return out, os.Rename(tmp, out)
}

func sameFilePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && aa == bb
}

func isHexSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
