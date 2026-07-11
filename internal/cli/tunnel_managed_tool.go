package cli

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

const (
	tunn3lManagedVersion          = "v0.5.1"
	maxManagedToolCompressedBytes = int64(64 << 20)
	maxManagedToolExpandedBytes   = int64(128 << 20)
	managedToolRedirectLimit      = 5
)

type managedToolAsset struct {
	Name, URL, CompressedSHA256 string
}

var tunn3lManagedAssets = map[string]managedToolAsset{
	"darwin/arm64": {"tunn3l-darwin-arm64.gz", "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-darwin-arm64.gz", "360669bd64595709cdc111e9bf430040c4608ad823582d035f955464fa1f45e4"},
	"darwin/amd64": {"tunn3l-darwin-x64.gz", "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-darwin-x64.gz", "35d559e55cbd40afcaf3acbe806020b7cafd9d8559d3fb6db2c3d16844c10bd6"},
	"linux/arm64":  {"tunn3l-linux-arm64.gz", "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-linux-arm64.gz", "9df47cad6d1e09313e5b01f76c69e4cde0c901ee424fa7943269cd101db3b1e1"},
	"linux/amd64":  {"tunn3l-linux-x64.gz", "https://github.com/bdecrem/tunn3l/releases/download/v0.5.1/tunn3l-linux-x64.gz", "902bc626033efb7bddde141542a145d95f55d256bd310e439bc71290a0ad6d58"},
}

func tunn3lManagedAsset(goos, goarch string) (managedToolAsset, bool) {
	asset, ok := tunn3lManagedAssets[strings.ToLower(strings.TrimSpace(goos)+"/"+strings.TrimSpace(goarch))]
	return asset, ok
}

type managedGzipInstaller struct {
	client          *http.Client
	compressedLimit int64
	expandedLimit   int64
}

var managedToolInstallGate = make(chan struct{}, 1)

func (installer managedGzipInstaller) Ensure(ctx context.Context, root string, asset managedToolAsset) (string, error) {
	if ctx == nil {
		return "", errors.New("managed tool context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateManagedToolAsset(asset); err != nil {
		return "", err
	}
	compressedLimit := installer.compressedLimit
	if compressedLimit <= 0 || compressedLimit > maxManagedToolCompressedBytes {
		compressedLimit = maxManagedToolCompressedBytes
	}
	expandedLimit := installer.expandedLimit
	if expandedLimit <= 0 || expandedLimit > maxManagedToolExpandedBytes {
		expandedLimit = maxManagedToolExpandedBytes
	}
	select {
	case managedToolInstallGate <- struct{}{}:
		defer func() { <-managedToolInstallGate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	protectedRoot, err := prepareManagedToolRoot(root)
	if err != nil {
		return "", err
	}
	expected, err := managedToolDigest(asset.CompressedSHA256)
	if err != nil {
		return "", err
	}
	compressed, err := installer.ensureCompressed(ctx, protectedRoot, asset, expected, compressedLimit)
	if err != nil {
		return "", err
	}
	defer compressed.Close()
	return installer.expandVerifiedGzip(ctx, protectedRoot, compressed, expandedLimit)
}

func validateManagedToolAsset(asset managedToolAsset) error {
	if asset.Name == "" || strings.TrimSpace(asset.Name) != asset.Name || filepath.IsAbs(asset.Name) || filepath.Base(asset.Name) != asset.Name || strings.Contains(asset.Name, "..") {
		return errors.New("managed tool asset name is unsafe")
	}
	for _, character := range []byte(asset.Name) {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-') {
			return errors.New("managed tool asset name is unsafe")
		}
	}
	if _, err := managedToolDigest(asset.CompressedSHA256); err != nil {
		return err
	}
	parsed, err := url.Parse(asset.URL)
	if err != nil || parsed == nil || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || (parsed.Port() != "" && parsed.Port() != "443") || !managedToolHostAllowed(parsed.Hostname()) {
		return errors.New("managed tool URL must be an HTTPS GitHub release URL")
	}
	return nil
}

func managedToolDigest(value string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != sha256.Size {
		return digest, errors.New("managed tool compressed digest is invalid")
	}
	copy(digest[:], decoded)
	return digest, nil
}

func managedToolHostAllowed(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(host, ".")) {
	case "github.com", "release-assets.githubusercontent.com":
		return !strings.HasSuffix(host, ".")
	default:
		return false
	}
}

func prepareManagedToolRoot(root string) (string, error) {
	volume := filepath.VolumeName(root)
	if root == "" || strings.TrimSpace(root) != root || filepath.Clean(root) != root || strings.Contains(strings.TrimPrefix(root, volume), ":") {
		return "", errors.New("managed tool root is unsafe")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve managed tool root: %w", err)
	}
	canonical, err := canonicalPathThroughExistingAncestor(filepath.Clean(abs))
	if err != nil {
		return "", fmt.Errorf("resolve managed tool root: %w", err)
	}
	existing := canonical
	for {
		_, statErr := os.Lstat(existing)
		if statErr == nil {
			break
		}
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect managed tool root: %w", statErr)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", errors.New("managed tool root has no protected ancestor")
		}
		existing = parent
	}
	if err := tunnel.ValidateProtectedDirectory(existing); err != nil {
		return "", fmt.Errorf("unsafe managed tool ancestor: %w", err)
	}
	if err := tunnel.ValidateProtectedParentChain(existing); err != nil {
		return "", fmt.Errorf("unsafe managed tool ancestor chain: %w", err)
	}
	relative, err := filepath.Rel(existing, canonical)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("managed tool root escapes protected ancestor")
	}
	current := existing
	if relative != "." {
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			if part == "" || part == "." {
				continue
			}
			current = filepath.Join(current, part)
			if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
				return "", fmt.Errorf("create managed tool directory: %w", err)
			}
			if err := tunnel.ValidateProtectedDirectory(current); err != nil {
				return "", fmt.Errorf("unsafe managed tool directory: %w", err)
			}
		}
	}
	if err := tunnel.ValidateProtectedDirectory(canonical); err != nil {
		return "", fmt.Errorf("unsafe managed tool root: %w", err)
	}
	if err := tunnel.ValidateProtectedParentChain(canonical); err != nil {
		return "", fmt.Errorf("unsafe managed tool root chain: %w", err)
	}
	return canonical, nil
}

func (installer managedGzipInstaller) ensureCompressed(ctx context.Context, root string, asset managedToolAsset, expected [sha256.Size]byte, maxBytes int64) (*os.File, error) {
	cachePath := filepath.Join(root, asset.Name)
	if _, err := os.Lstat(cachePath); err == nil {
		file, verifyErr := tunnel.OpenVerifiedProtectedRegularFileSHA256(cachePath, maxBytes, expected)
		if verifyErr != nil {
			return nil, fmt.Errorf("cached compressed asset digest mismatch: %w", verifyErr)
		}
		return file, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect cached compressed asset: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(root, "."+asset.Name+".download-*")
	if err != nil {
		return nil, fmt.Errorf("create managed tool download: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return nil, fmt.Errorf("protect managed tool download: %w", err)
	}
	response, err := installer.download(ctx, asset.URL)
	if err != nil {
		_ = temp.Close()
		return nil, err
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = temp.Close()
		return nil, fmt.Errorf("managed tool download returned status %d", response.StatusCode)
	}
	if response.ContentLength > maxBytes {
		_ = temp.Close()
		return nil, fmt.Errorf("managed tool compressed body exceeds %d bytes", maxBytes)
	}
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, digest), io.LimitReader(&contextReader{ctx: ctx, reader: response.Body}, maxBytes+1))
	if err != nil {
		_ = temp.Close()
		return nil, fmt.Errorf("read managed tool download: %w", err)
	}
	if written > maxBytes {
		_ = temp.Close()
		return nil, fmt.Errorf("managed tool compressed body exceeds %d bytes", maxBytes)
	}
	if !bytesEqualDigest(digest.Sum(nil), expected[:]) {
		_ = temp.Close()
		return nil, errors.New("managed tool compressed digest mismatch")
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return nil, fmt.Errorf("sync managed tool download: %w", err)
	}
	if err := temp.Close(); err != nil {
		return nil, fmt.Errorf("close managed tool download: %w", err)
	}
	if err := tunnel.VerifyProtectedRegularFileSHA256(tempPath, maxBytes, expected); err != nil {
		return nil, fmt.Errorf("verify managed tool download: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(cachePath); err == nil {
		winner, verifyErr := tunnel.OpenVerifiedProtectedRegularFileSHA256(cachePath, maxBytes, expected)
		if verifyErr != nil {
			return nil, fmt.Errorf("cached compressed asset digest mismatch: %w", verifyErr)
		}
		return winner, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect cached compressed asset: %w", err)
	}
	published, err := publishManagedToolNoReplace(tempPath, cachePath)
	if err != nil {
		return nil, fmt.Errorf("publish managed tool download: %w", err)
	}
	removeTemp = false
	if err := syncSupportSessionArtifactDirectory(cachePath); err != nil {
		return nil, fmt.Errorf("sync managed tool cache: %w", err)
	}
	if !published {
		winner, verifyErr := tunnel.OpenVerifiedProtectedRegularFileSHA256(cachePath, maxBytes, expected)
		if verifyErr != nil {
			return nil, fmt.Errorf("competing cached asset failed verification: %w", verifyErr)
		}
		return winner, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := tunnel.OpenVerifiedProtectedRegularFileSHA256(cachePath, maxBytes, expected)
	if err != nil {
		return nil, fmt.Errorf("verify managed tool cache: %w", err)
	}
	return file, nil
}

func (installer managedGzipInstaller) download(ctx context.Context, rawURL string) (*http.Response, error) {
	client := newManagedToolHTTPClient()
	if installer.client != nil {
		client = installer.client
	}
	copyClient := *client
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > managedToolRedirectLimit {
			return errors.New("managed tool download redirect limit exceeded")
		}
		if err := validateManagedDownloadURL(request.URL); err != nil {
			return fmt.Errorf("managed tool download redirect rejected: %w", err)
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("managed tool download request is invalid")
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := copyClient.Do(request)
	if err == nil {
		return response, nil
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		cause := urlError.Err.Error()
		switch {
		case strings.Contains(cause, "redirect limit"):
			return nil, errors.New("managed tool download redirect limit exceeded")
		case strings.Contains(cause, "redirect rejected") || strings.Contains(cause, "must remain HTTPS"):
			return nil, errors.New("managed tool download redirect rejected: URL must remain HTTPS on an approved host")
		}
	}
	return nil, errors.New("managed tool download failed")
}

func newManagedToolHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          4,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: time.Second,
			DisableCompression:    true,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

func validateManagedDownloadURL(value *url.URL) error {
	if value == nil || !strings.EqualFold(value.Scheme, "https") || value.User != nil || value.Opaque != "" || value.Fragment != "" || value.Hostname() == "" || (value.Port() != "" && value.Port() != "443") || !managedToolHostAllowed(value.Hostname()) {
		return errors.New("managed tool URL must remain HTTPS on an approved host")
	}
	return nil
}

func (installer managedGzipInstaller) expandVerifiedGzip(ctx context.Context, root string, compressed *os.File, maxBytes int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	reader, err := gzip.NewReader(&contextReader{ctx: ctx, reader: compressed})
	if err != nil {
		return "", fmt.Errorf("open managed tool gzip: %w", err)
	}
	temp, err := os.CreateTemp(root, ".tunn3l-expand-*")
	if err != nil {
		_ = reader.Close()
		return "", fmt.Errorf("create managed tool executable: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		_ = reader.Close()
		return "", fmt.Errorf("protect managed tool executable: %w", err)
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, digest), io.LimitReader(&contextReader{ctx: ctx, reader: reader}, maxBytes+1))
	closeErr := reader.Close()
	if copyErr != nil {
		_ = temp.Close()
		return "", fmt.Errorf("expand managed tool gzip: %w", copyErr)
	}
	if closeErr != nil {
		_ = temp.Close()
		return "", fmt.Errorf("close managed tool gzip: %w", closeErr)
	}
	if written > maxBytes {
		_ = temp.Close()
		return "", fmt.Errorf("managed tool expanded body exceeds %d bytes", maxBytes)
	}
	if err := ctx.Err(); err != nil {
		_ = temp.Close()
		return "", err
	}
	expandedDigest := digest.Sum(nil)
	if runtime.GOOS != "windows" {
		if err := temp.Chmod(0o700); err != nil {
			_ = temp.Close()
			return "", fmt.Errorf("make managed tool executable: %w", err)
		}
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("sync managed tool executable: %w", err)
	}
	if err := verifyManagedExecutableHandle(temp, tempPath, maxBytes, expandedDigest); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close managed tool executable: %w", err)
	}
	executablePath := filepath.Join(root, "tunn3l-"+hex.EncodeToString(expandedDigest))
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := os.Lstat(executablePath); err == nil {
		winner, verifyErr := tunnel.OpenVerifiedProtectedExecutableSHA256(executablePath, maxBytes, bytesToDigest(expandedDigest))
		if verifyErr != nil {
			return "", fmt.Errorf("existing managed tool executable digest mismatch: %w", verifyErr)
		}
		_ = winner.Close()
		return executablePath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect managed tool executable: %w", err)
	}
	published, err := publishManagedToolNoReplace(tempPath, executablePath)
	if err != nil {
		return "", fmt.Errorf("publish managed tool executable: %w", err)
	}
	removeTemp = false
	if err := syncSupportSessionArtifactDirectory(executablePath); err != nil {
		return "", fmt.Errorf("sync managed tool executable: %w", err)
	}
	if !published {
		winner, verifyErr := tunnel.OpenVerifiedProtectedExecutableSHA256(executablePath, maxBytes, bytesToDigest(expandedDigest))
		if verifyErr != nil {
			return "", fmt.Errorf("competing managed tool executable failed verification: %w", verifyErr)
		}
		if err := winner.Close(); err != nil {
			return "", fmt.Errorf("close competing managed tool executable: %w", err)
		}
		return executablePath, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	verified, err := tunnel.OpenVerifiedProtectedExecutableSHA256(executablePath, maxBytes, bytesToDigest(expandedDigest))
	if err != nil {
		return "", fmt.Errorf("verify managed tool executable: %w", err)
	}
	if err := verified.Close(); err != nil {
		return "", fmt.Errorf("close verified managed tool executable: %w", err)
	}
	return executablePath, nil
}

// publishManagedToolNoReplace atomically gives targetPath a hard link to the
// complete same-directory temporary file without ever replacing an existing
// target. A false result means another publisher won the target name.
func publishManagedToolNoReplace(tempPath, targetPath string) (bool, error) {
	tempAbsolute, err := filepath.Abs(tempPath)
	if err != nil {
		return false, fmt.Errorf("resolve managed tool temporary path: %w", err)
	}
	targetAbsolute, err := filepath.Abs(targetPath)
	if err != nil {
		return false, fmt.Errorf("resolve managed tool target path: %w", err)
	}
	tempDirectory := filepath.Dir(tempAbsolute)
	targetDirectory := filepath.Dir(targetAbsolute)
	sameDirectory := filepath.Clean(tempDirectory) == filepath.Clean(targetDirectory)
	samePath := filepath.Clean(tempAbsolute) == filepath.Clean(targetAbsolute)
	if runtime.GOOS == "windows" {
		sameDirectory = strings.EqualFold(filepath.Clean(tempDirectory), filepath.Clean(targetDirectory))
		samePath = strings.EqualFold(filepath.Clean(tempAbsolute), filepath.Clean(targetAbsolute))
	}
	if !sameDirectory || samePath {
		return false, errors.New("managed tool publication paths must be distinct files in one directory")
	}
	if err := os.Link(tempPath, targetPath); err != nil {
		if !os.IsExist(err) {
			return false, fmt.Errorf("link managed tool target: %w", err)
		}
		if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove losing managed tool temporary file: %w", err)
		}
		return false, nil
	}
	if err := os.Remove(tempPath); err != nil {
		return true, fmt.Errorf("remove published managed tool temporary file: %w", err)
	}
	return true, nil
}

func verifyManagedExecutableHandle(file *os.File, path string, maxBytes int64, expected []byte) error {
	if file == nil || len(expected) != sha256.Size {
		return errors.New("managed tool executable verification input is invalid")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind managed tool executable: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat managed tool executable: %w", err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect managed tool executable path: %w", err)
	}
	if !info.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		return errors.New("managed tool executable is not a stable private regular file")
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, maxBytes+1))
	if err != nil {
		return fmt.Errorf("hash managed tool executable: %w", err)
	}
	if written > maxBytes || !bytesEqualDigest(digest.Sum(nil), expected) {
		return errors.New("managed tool executable digest mismatch")
	}
	postInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("restat managed tool executable: %w", err)
	}
	postPathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reinspect managed tool executable path: %w", err)
	}
	if !postInfo.Mode().IsRegular() || postPathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(postInfo, postPathInfo) || postInfo.Size() != written || (runtime.GOOS != "windows" && postInfo.Mode().Perm() != 0o700) {
		return errors.New("managed tool executable changed during verification")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind managed tool executable: %w", err)
	}
	return nil
}

func bytesToDigest(value []byte) (digest [sha256.Size]byte) {
	copy(digest[:], value)
	return digest
}

func bytesEqualDigest(left, right []byte) bool {
	return subtle.ConstantTimeCompare(left, right) == 1
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
