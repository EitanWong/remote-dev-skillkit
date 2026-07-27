package toolchain

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
)

// ArtifactFetcher is injectable so tests can exercise extraction and fallback
// without a network. Production still hashes every downloaded archive again
// before extraction.
type ArtifactFetcher interface {
	Fetch(ctx context.Context, source contracts.ToolchainNodeSource, destination string, maxArchiveBytes int64) error
}

type httpArtifactFetcher struct{}

func (httpArtifactFetcher) Fetch(ctx context.Context, source contracts.ToolchainNodeSource, destination string, maxArchiveBytes int64) error {
	sourceURL, err := url.Parse(source.URL)
	if err != nil || !isTrustedHTTPSURL(sourceURL) {
		return fmt.Errorf("portable runtime source is not a trusted HTTPS URL")
	}
	client := &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if !isTrustedHTTPSURL(request.URL) || request.URL.Host != sourceURL.Host {
				return fmt.Errorf("redirect leaves HTTPS trusted-source boundary")
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return fmt.Errorf("create portable runtime request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download portable runtime archive: %w", err)
	}
	defer response.Body.Close()
	if response.Request == nil || !isTrustedHTTPSURL(response.Request.URL) || response.Request.URL.Host != sourceURL.Host {
		return fmt.Errorf("portable runtime response left HTTPS trusted-source boundary")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("portable runtime source returned HTTP %d", response.StatusCode)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create portable runtime archive: %w", err)
	}
	defer file.Close()
	written, err := io.Copy(file, io.LimitReader(response.Body, maxArchiveBytes+1))
	if err != nil {
		return fmt.Errorf("write portable runtime archive: %w", err)
	}
	if written > maxArchiveBytes {
		return fmt.Errorf("portable runtime archive exceeds policy byte limit")
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync portable runtime archive: %w", err)
	}
	return nil
}

func ensurePortableNode(ctx context.Context, nodeRoot string, bootstrap contracts.ToolchainNodeBootstrap, region string, options Options) (string, string, BootstrapResult, error) {
	result := BootstrapResult{Runtime: "node", Version: bootstrap.Version}
	goos := options.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	runtimePath := filepath.Join(nodeRoot, bootstrap.Version)
	if npmCommand, nodeBinDir, err := findPortableNodeRuntime(runtimePath, goos); err == nil {
		result.SourceID = "managed-cache"
		result.Cached = true
		return npmCommand, nodeBinDir, result, nil
	}
	if info, err := os.Stat(runtimePath); err == nil && info.IsDir() {
		return "", "", result, fmt.Errorf("managed portable Node runtime exists but is incomplete")
	} else if err != nil && !os.IsNotExist(err) {
		return "", "", result, fmt.Errorf("inspect managed portable Node runtime: %w", err)
	}
	if err := os.MkdirAll(nodeRoot, 0o700); err != nil {
		return "", "", result, fmt.Errorf("create managed portable Node root: %w", err)
	}
	fetcher := options.Fetcher
	if fetcher == nil {
		fetcher = httpArtifactFetcher{}
	}
	for _, source := range bootstrap.EligibleSources(region) {
		staging, err := os.MkdirTemp(nodeRoot, ".rdev-node-")
		if err != nil {
			return "", "", result, fmt.Errorf("create portable Node staging directory: %w", err)
		}
		archivePath := filepath.Join(staging, "node."+archiveExtension(source.Format))
		extractPath := filepath.Join(staging, "extracted")
		attempt := BootstrapAttempt{SourceID: source.ID}
		fetchErr := fetcher.Fetch(ctx, source, archivePath, bootstrap.MaxArchiveBytes)
		if fetchErr == nil {
			fetchErr = verifyArchiveSHA256(archivePath, source.SHA256)
		}
		if fetchErr == nil {
			fetchErr = extractNodeArchive(archivePath, extractPath, source.Format, bootstrap.MaxExtractedBytes)
		}
		if fetchErr == nil {
			_, _, fetchErr = findPortableNodeRuntime(extractPath, goos)
		}
		if fetchErr == nil {
			fetchErr = os.Rename(extractPath, runtimePath)
		}
		if fetchErr == nil {
			npmCommand, nodeBinDir, locateErr := findPortableNodeRuntime(runtimePath, goos)
			if locateErr == nil {
				attempt.Succeeded = true
				result.Attempts = append(result.Attempts, attempt)
				result.SourceID = source.ID
				_ = os.RemoveAll(staging)
				return npmCommand, nodeBinDir, result, nil
			}
			fetchErr = locateErr
		}
		result.Attempts = append(result.Attempts, attempt)
		_ = os.RemoveAll(staging)
	}
	return "", "", result, fmt.Errorf("no verified portable Node source completed")
}

func isTrustedHTTPSURL(value *url.URL) bool {
	return value != nil && value.Scheme == "https" && value.Host != "" && value.User == nil
}

func archiveExtension(format string) string {
	if format == contracts.ToolchainArchiveTarGZ {
		return "tar.gz"
	}
	return "zip"
}

func verifyArchiveSHA256(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open portable runtime archive: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash portable runtime archive: %w", err)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(actual, expected) {
		return fmt.Errorf("portable runtime archive SHA-256 does not match policy")
	}
	return nil
}

func extractNodeArchive(archivePath, destination, format string, maxExtractedBytes int64) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return fmt.Errorf("create portable runtime extraction directory: %w", err)
	}
	switch format {
	case contracts.ToolchainArchiveZIP:
		return extractZipArchive(archivePath, destination, maxExtractedBytes)
	case contracts.ToolchainArchiveTarGZ:
		return extractTarGZArchive(archivePath, destination, maxExtractedBytes)
	default:
		return fmt.Errorf("unsupported portable runtime archive format")
	}
}

func extractZipArchive(archivePath, destination string, maxExtractedBytes int64) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open portable runtime zip archive: %w", err)
	}
	defer reader.Close()
	var extracted int64
	for _, entry := range reader.File {
		path, err := safeArchivePath(destination, entry.Name)
		if err != nil {
			return err
		}
		mode := entry.Mode()
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
			continue
		}
		if mode&os.ModeType != 0 {
			return fmt.Errorf("portable runtime zip contains unsupported non-regular entry")
		}
		content, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open portable runtime zip entry: %w", err)
		}
		err = writeExtractedFile(content, path, mode, &extracted, maxExtractedBytes)
		closeErr := content.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func extractTarGZArchive(archivePath, destination string, maxExtractedBytes int64) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open portable runtime tar archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open portable runtime gzip stream: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var extracted int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read portable runtime tar entry: %w", err)
		}
		path, err := safeArchivePath(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeExtractedFile(reader, path, fs.FileMode(header.Mode), &extracted, maxExtractedBytes); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := writeSafeSymlink(destination, path, header.Linkname); err != nil {
				return err
			}
		default:
			return fmt.Errorf("portable runtime tar contains unsupported entry type")
		}
	}
}

func safeArchivePath(root, name string) (string, error) {
	path := filepath.Clean(filepath.FromSlash(name))
	if path == "." || path == "" || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("portable runtime archive contains unsafe path")
	}
	resolved := filepath.Join(root, path)
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("portable runtime archive path escapes extraction root")
	}
	return resolved, nil
}

func writeSafeSymlink(root, path, target string) error {
	if strings.TrimSpace(target) == "" || filepath.IsAbs(filepath.FromSlash(target)) {
		return fmt.Errorf("portable runtime archive contains unsafe symlink")
	}
	resolvedTarget := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(target)))
	relative, err := filepath.Rel(root, resolvedTarget)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("portable runtime archive symlink escapes extraction root")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

func writeExtractedFile(source io.Reader, path string, mode fs.FileMode, extracted *int64, maxExtractedBytes int64) error {
	if *extracted >= maxExtractedBytes {
		return fmt.Errorf("portable runtime extraction exceeds policy byte limit")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	permission := mode.Perm() & 0o700
	if permission == 0 {
		permission = 0o600
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, permission)
	if err != nil {
		return err
	}
	remaining := maxExtractedBytes - *extracted
	written, copyErr := io.Copy(file, io.LimitReader(source, remaining+1))
	closeErr := file.Close()
	*extracted += written
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > remaining {
		return fmt.Errorf("portable runtime extraction exceeds policy byte limit")
	}
	return nil
}

func findPortableNodeRuntime(root, goos string) (string, string, error) {
	var nodePath string
	var npmPath string
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if goos == "windows" {
			if !strings.EqualFold(entry.Name(), "node.exe") {
				return nil
			}
			candidateNPM := filepath.Join(filepath.Dir(path), "npm.cmd")
			if _, err := os.Stat(candidateNPM); err == nil {
				nodePath, npmPath = path, candidateNPM
				return fs.SkipAll
			}
			return nil
		}
		if entry.Name() != "node" || filepath.Base(filepath.Dir(path)) != "bin" {
			return nil
		}
		candidateNPM := filepath.Join(filepath.Dir(path), "npm")
		if _, err := os.Stat(candidateNPM); err == nil {
			nodePath, npmPath = path, candidateNPM
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return "", "", walkErr
	}
	if nodePath == "" || npmPath == "" {
		return "", "", fmt.Errorf("portable Node runtime does not contain node and npm")
	}
	if goos != "windows" {
		if err := os.Chmod(nodePath, 0o700); err != nil {
			return "", "", err
		}
		if err := os.Chmod(npmPath, 0o700); err != nil {
			return "", "", err
		}
	}
	return npmPath, filepath.Dir(nodePath), nil
}
