package assetdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	downloadStatusOK              = 200
	downloadStatusPartialContent  = 206
	downloadStatusRequestTimeout  = 408
	downloadStatusTooManyRequests = 429
)

type Mirror struct {
	URL    string
	Kind   string
	Weight int
}

type Options struct {
	Mirrors        []Mirror
	OutputPath     string
	CachePath      string
	ExpectedSHA256 string
	ExpectedSize   int64
	Transport      Transport
	MaxAttempts    int
}

type Event struct {
	Phase   string `json:"phase"`
	URL     string `json:"url,omitempty"`
	Message string `json:"message,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
}

type Result struct {
	OutputPath string  `json:"output_path"`
	SourceURL  string  `json:"source_url"`
	FromCache  bool    `json:"from_cache"`
	Resumed    bool    `json:"resumed"`
	Bytes      int64   `json:"bytes"`
	SHA256     string  `json:"sha256"`
	Transcript []Event `json:"transcript,omitempty"`
}

func DefaultCachePath(assetName string) (string, bool) {
	assetName = strings.TrimSpace(assetName)
	if assetName == "" ||
		strings.Contains(assetName, "/") ||
		strings.Contains(assetName, `\`) ||
		filepath.Base(assetName) != assetName {
		return "", false
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cacheDir) == "" {
		return "", false
	}
	return filepath.Join(cacheDir, "remote-dev-skillkit", "helpers", assetName), true
}

func Download(ctx context.Context, opts Options) (Result, error) {
	expected := normalizeSHA256(opts.ExpectedSHA256)
	if expected == "" {
		return Result{}, fmt.Errorf("expected sha256 is required")
	}
	outputPath := strings.TrimSpace(opts.OutputPath)
	if outputPath == "" {
		return Result{}, fmt.Errorf("output path is required")
	}
	if opts.ExpectedSize < 0 {
		return Result{}, fmt.Errorf("expected size must not be negative")
	}
	if opts.Transport == nil {
		return Result{}, fmt.Errorf("transport is required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return Result{}, err
	}
	result := Result{OutputPath: outputPath}
	if cachePath := strings.TrimSpace(opts.CachePath); cachePath != "" {
		if sha, size, ok := verifiedFile(cachePath, expected, opts.ExpectedSize); ok {
			if err := copyVerifiedFile(cachePath, outputPath); err != nil {
				return Result{}, err
			}
			result.SourceURL = "cache"
			result.FromCache = true
			result.Bytes = size
			result.SHA256 = sha
			result.Transcript = append(result.Transcript, Event{Phase: "cache-hit", Message: cachePath, Bytes: size})
			return result, nil
		}
	}
	if sha, size, ok := verifiedFile(outputPath, expected, opts.ExpectedSize); ok {
		result.SourceURL = "output"
		result.FromCache = true
		result.Bytes = size
		result.SHA256 = sha
		result.Transcript = append(result.Transcript, Event{Phase: "output-hit", Bytes: size})
		return result, nil
	}
	partPath := outputPath + ".part"
	if opts.ExpectedSize > 0 {
		if info, err := os.Stat(partPath); err == nil && info.Size() == opts.ExpectedSize {
			sha, size, ok := verifiedFile(partPath, expected, opts.ExpectedSize)
			if ok {
				if err := promotePart(partPath, outputPath); err != nil {
					return Result{}, err
				}
				if cachePath := strings.TrimSpace(opts.CachePath); cachePath != "" && !sameCleanPath(cachePath, outputPath) {
					_ = copyVerifiedFile(outputPath, cachePath)
				}
				result.SourceURL = "partial"
				result.Resumed = true
				result.Bytes = size
				result.SHA256 = sha
				result.Transcript = append(result.Transcript, Event{Phase: "partial-hit", Bytes: size})
				return result, nil
			}
			_ = os.Remove(partPath)
		}
	}
	mirrors := normalizedMirrors(opts.Mirrors)
	if len(mirrors) == 0 {
		return Result{}, fmt.Errorf("at least one mirror is required")
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var lastErr error
	for _, mirror := range mirrors {
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				if err := sleepBackoff(ctx, attempt); err != nil {
					return Result{}, err
				}
			}
			resumed, err := downloadOnce(ctx, opts.Transport, mirror.URL, outputPath, opts.ExpectedSize)
			if err != nil {
				lastErr = err
				result.Transcript = append(result.Transcript, Event{Phase: "download-error", URL: mirror.URL, Message: err.Error()})
				if !retryable(err) {
					return Result{}, err
				}
				continue
			}
			sha, size, verifyErr := fileSHA256(partPath)
			if verifyErr != nil {
				return Result{}, verifyErr
			}
			if opts.ExpectedSize > 0 && size != opts.ExpectedSize {
				_ = os.Remove(partPath)
				return Result{}, fmt.Errorf("downloaded size does not match expected size: got %d bytes, expected %d", size, opts.ExpectedSize)
			}
			if normalizeSHA256(sha) != expected {
				_ = os.Remove(outputPath + ".part")
				return Result{}, fmt.Errorf("checksum mismatch for %s", mirror.URL)
			}
			if err := promotePart(partPath, outputPath); err != nil {
				return Result{}, err
			}
			if cachePath := strings.TrimSpace(opts.CachePath); cachePath != "" && !sameCleanPath(cachePath, outputPath) {
				_ = copyVerifiedFile(outputPath, cachePath)
			}
			result.SourceURL = mirror.URL
			result.Resumed = resumed
			result.Bytes = size
			result.SHA256 = sha
			result.Transcript = append(result.Transcript, Event{Phase: "download-verified", URL: mirror.URL, Bytes: size})
			return result, nil
		}
	}
	if lastErr != nil {
		return Result{}, lastErr
	}
	return Result{}, fmt.Errorf("download failed")
}

func normalizedMirrors(values []Mirror) []Mirror {
	out := make([]Mirror, 0, len(values))
	seen := map[string]bool{}
	for _, mirror := range values {
		mirror.URL = strings.TrimSpace(mirror.URL)
		if mirror.URL == "" || seen[mirror.URL] {
			continue
		}
		seen[mirror.URL] = true
		out = append(out, mirror)
	}
	return out
}

func downloadOnce(ctx context.Context, transport Transport, rawURL, outputPath string, expectedSize int64) (_ bool, resultErr error) {
	partPath := outputPath + ".part"
	var offset int64
	if info, err := os.Stat(partPath); err == nil {
		offset = info.Size()
	}
	if expectedSize > 0 && offset > expectedSize {
		_ = os.Remove(partPath)
		return false, fmt.Errorf("partial download exceeds expected size: got %d bytes, expected %d", offset, expectedSize)
	}
	resp, err := transport.Fetch(ctx, TransportRequest{
		URL:      rawURL,
		Offset:   offset,
		MaxBytes: expectedSize,
	})
	if err != nil {
		if resp.Body != nil {
			err = errors.Join(err, wrapResponseBodyCloseError(resp.Body.Close()))
		}
		return false, err
	}
	if resp.Body == nil {
		return false, fmt.Errorf("download response body is required")
	}
	defer func() {
		resultErr = errors.Join(resultErr, wrapResponseBodyCloseError(resp.Body.Close()))
	}()
	appendMode := offset > 0 && resp.StatusCode == downloadStatusPartialContent
	if offset > 0 && resp.StatusCode == downloadStatusOK {
		offset = 0
	}
	if resp.StatusCode != downloadStatusOK && resp.StatusCode != downloadStatusPartialContent {
		err := fmt.Errorf("download failed: status %d", resp.StatusCode)
		if retryableStatus(resp.StatusCode) {
			return false, retryableError{err: err}
		}
		return false, err
	}
	if expectedSize > 0 && resp.ContentLength >= 0 && offset+resp.ContentLength > expectedSize {
		_ = os.Remove(partPath)
		return false, fmt.Errorf("download exceeds expected size: got at least %d bytes, expected %d", offset+resp.ContentLength, expectedSize)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(partPath, flags, 0o600)
	if err != nil {
		return false, err
	}
	reader := io.Reader(resp.Body)
	remaining := int64(0)
	if expectedSize > 0 {
		remaining = expectedSize - offset
		reader = io.LimitReader(resp.Body, remaining+1)
	}
	written, err := io.Copy(file, reader)
	if err != nil {
		_ = file.Close()
		return appendMode, err
	}
	if expectedSize > 0 && written > remaining {
		_ = file.Close()
		_ = os.Remove(partPath)
		return appendMode, fmt.Errorf("download exceeds expected size: expected %d bytes", expectedSize)
	}
	if err := file.Close(); err != nil {
		return appendMode, err
	}
	return appendMode, nil
}

func wrapResponseBodyCloseError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close download response body: %w", err)
}

func promotePart(partPath, outputPath string) error {
	return atomicReplace(partPath, outputPath)
}

func copyVerifiedFile(src, dst string) error {
	if sameCleanPath(src, dst) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := atomicReplace(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func verifiedFile(path, expected string, expectedSize int64) (string, int64, bool) {
	sha, size, err := fileSHA256(path)
	if err != nil {
		return "", 0, false
	}
	if normalizeSHA256(sha) != expected || expectedSize > 0 && size != expectedSize {
		return sha, size, false
	}
	return sha, size, true
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}

func normalizeSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	if value == "" {
		return ""
	}
	return "sha256:" + value
}

func sleepBackoff(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt*attempt) * 100 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type retryableError struct {
	err error
}

func (e retryableError) Error() string {
	return e.err.Error()
}

func (e retryableError) Unwrap() error {
	return e.err
}

func retryable(err error) bool {
	var wrapped retryableError
	if errors.As(err, &wrapped) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection")
}

func retryableStatus(status int) bool {
	return status == downloadStatusRequestTimeout ||
		status == downloadStatusTooManyRequests ||
		status >= 500
}

func sameCleanPath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
