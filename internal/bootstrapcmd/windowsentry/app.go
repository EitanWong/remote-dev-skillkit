package windowsentry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
)

const (
	RunReportSchemaVersion = "rdev.windows-entry-run-report.v1"
	maxManifestBytes       = int64(1 << 20)
)

type App struct {
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
	Transport      assetdownload.Transport
	Now            time.Time
	Clock          func() time.Time
	CommandContext func(context.Context, string, ...string) *exec.Cmd
	download       func(context.Context, assetdownload.Options) (assetdownload.Result, error)
}

type RunReport struct {
	SchemaVersion string `json:"schema_version"`
	AssetID       string `json:"asset_id"`
	FromCache     bool   `json:"from_cache"`
	Resumed       bool   `json:"resumed"`
	Bytes         int64  `json:"bytes"`
}

func (a App) Run(ctx context.Context, args []string) (resultErr error) {
	if len(args) == 0 || args[0] != "layered-run" {
		return newPreCoreError("invalid_input")
	}
	if len(args) > 1 && (args[1] == "attempt-check" || args[1] == "private-path-check") {
		return a.runCheck(args[1] == "attempt-check", args[2:])
	}
	opts, err := parseRunArgs(args[1:])
	if err != nil {
		return newPreCoreError("invalid_input")
	}
	if opts.mode != "temporary" {
		return newPreCoreError("invalid_input")
	}
	if opts.platform != "windows/amd64" && opts.platform != "windows/arm64" {
		return newPreCoreError("invalid_input")
	}
	if strings.TrimSpace(opts.expectedVersion) == "" {
		return newPreCoreError("invalid_input")
	}
	if strings.TrimSpace(opts.cacheDir) == "" {
		return newPreCoreError("invalid_input")
	}
	if strings.TrimSpace(opts.attemptDir) == "" || !opts.launcher.valid() {
		return newPreCoreError("invalid_input")
	}
	if !validCoreTransport(opts.args) {
		return newPreCoreError("invalid_input")
	}
	attempt, err := acquireAttempt(opts.attemptDir, opts.launcher, a.now())
	if err != nil {
		if errors.Is(err, errAttemptClosed) {
			return newPreCoreError("attempt_closed")
		}
		if errors.Is(err, errAttemptBusy) {
			return newPreCoreError("attempt_busy")
		}
		return newPreCoreError("attempt_invalid")
	}
	defer func() {
		if closeErr := attempt.close(); closeErr != nil && resultErr == nil {
			resultErr = errors.New("layered attempt lock release failed")
		}
	}()

	parsedManifestURL, err := strictHTTPSURL(opts.manifestURL)
	if err != nil {
		return newPreCoreError("invalid_input")
	}
	root, err := release.ParseLayeredTrustRoot(opts.rootPublicKey)
	if err != nil {
		return newPreCoreError("invalid_input")
	}
	transport := a.Transport
	if transport == nil {
		transport, err = defaultTransport()
		if err != nil {
			return newPreCoreError("transport_unavailable")
		}
	}
	manifest, err := fetchManifest(ctx, transport, parsedManifestURL.String())
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return newPreCoreError("manifest_fetch")
	}
	if err := release.VerifyLayeredAssetManifestRoot(manifest, root, a.now()); err != nil {
		return newPreCoreError("manifest_verify")
	}
	if manifest.Version != strings.TrimSpace(opts.expectedVersion) {
		return newPreCoreError("manifest_verify")
	}
	asset, err := release.SelectLayeredAsset(manifest, opts.platform, "core-runtime", nil)
	if err != nil {
		return newPreCoreError("manifest_verify")
	}
	assetURL, err := resolveAssetURL(parsedManifestURL, asset.RelativePath)
	if err != nil {
		return newPreCoreError("manifest_verify")
	}
	outputPath, contentPath, err := cachePaths(opts.cacheDir, asset)
	if err != nil {
		return newPreCoreError("runtime_prepare")
	}

	download := a.download
	if download == nil {
		download = assetdownload.Download
	}
	result, err := download(ctx, assetdownload.Options{
		Mirrors:        []assetdownload.Mirror{{URL: assetURL.String()}},
		OutputPath:     outputPath,
		CachePath:      contentPath,
		ExpectedSHA256: asset.SHA256,
		ExpectedSize:   asset.SizeBytes,
		Transport:      transport,
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return newPreCoreError("runtime_prepare")
	}
	if result.Bytes != asset.SizeBytes {
		return newPreCoreError("runtime_prepare")
	}
	if err := os.Chmod(result.OutputPath, 0o600); err != nil {
		return newPreCoreError("runtime_prepare")
	}
	if err := validatePrivateCacheFile(result.OutputPath, asset.SizeBytes); err != nil {
		return newPreCoreError("runtime_prepare")
	}
	if err := validateOptionalPrivateCacheFile(contentPath, asset.SizeBytes); err != nil {
		return newPreCoreError("runtime_prepare")
	}
	runtimeFile, err := openVerifiedRuntime(result.OutputPath, asset.SizeBytes, asset.SHA256)
	if err != nil {
		return newPreCoreError("runtime_prepare")
	}
	defer runtimeFile.Close()

	report := RunReport{
		SchemaVersion: RunReportSchemaVersion,
		AssetID:       asset.ID,
		FromCache:     result.FromCache,
		Resumed:       result.Resumed,
		Bytes:         result.Bytes,
	}
	cmd := a.commandContext(ctx, result.OutputPath, opts.args...)
	if cmd == nil {
		return newPreCoreError("runtime_prepare")
	}
	cmd.Stdin = a.stdin()
	cmd.Stdout = a.stdout()
	cmd.Stderr = a.stderr()
	lifecycle, err := newCoreLifecycle(cmd)
	if err != nil {
		return newPreCoreError("runtime_prepare")
	}
	defer lifecycle.close()
	if err := recheckVerifiedRuntime(runtimeFile, result.OutputPath, asset.SizeBytes, asset.SHA256); err != nil {
		return newPreCoreError("runtime_prepare")
	}
	if err := writeRunReport(a.stdout(), report); err != nil {
		return newPreCoreError("report_output")
	}
	if err := attempt.transition(attemptStageCoreStarted, a.now()); err != nil {
		return newPreCoreError("attempt_state")
	}
	cleaned, runErr := lifecycle.run(ctx, cmd)
	if !cleaned {
		return errors.New("layered core containment failed")
	}
	if err := attempt.transition(attemptStageCoreExited, a.now()); err != nil {
		return errors.New("layered core exit state failed")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runErr != nil {
		return errors.New("layered core lifecycle failed")
	}
	return nil
}

func (a App) runCheck(attemptCommand bool, args []string) error {
	if !attemptCommand {
		if len(args) != 4 || args[0] != "--path" || args[2] != "--kind" {
			return errInvalidAttemptState
		}
		directory := args[3] == "directory"
		if args[1] == "" || args[3] != "directory" && args[3] != "file" {
			return errInvalidAttemptState
		}
		return validatePrivateLauncherPath(args[1], directory)
	}
	create := len(args) == 5 && args[4] == "--create"
	if len(args) != 4 && !create || args[0] != "--attempt-dir" || args[2] != "--launcher" {
		return errInvalidAttemptState
	}
	directory := args[1]
	launcher := attemptLauncher(args[3])
	if directory == "" || !launcher.valid() {
		return errInvalidAttemptState
	}
	if create {
		if err := preparePrivateAttemptDirectory(directory); err != nil {
			return err
		}
	} else if _, err := validatePrivateAttemptDirectory(directory); err != nil {
		return err
	}
	attempt, err := acquireAttempt(directory, launcher, a.now())
	if err != nil {
		return err
	}
	return attempt.close()
}

func writeRunReport(destination io.Writer, report RunReport) error {
	encoded := make([]byte, 0, 160)
	encoded = append(encoded, `{"schema_version":`...)
	encoded = appendReportString(encoded, report.SchemaVersion)
	encoded = append(encoded, `,"asset_id":`...)
	encoded = appendReportString(encoded, report.AssetID)
	encoded = append(encoded, `,"from_cache":`...)
	encoded = strconv.AppendBool(encoded, report.FromCache)
	encoded = append(encoded, `,"resumed":`...)
	encoded = strconv.AppendBool(encoded, report.Resumed)
	encoded = append(encoded, `,"bytes":`...)
	encoded = strconv.AppendInt(encoded, report.Bytes, 10)
	encoded = append(encoded, '}', '\n')
	written, err := destination.Write(encoded)
	if err == nil && written != len(encoded) {
		return io.ErrShortWrite
	}
	return err
}

func appendReportString(destination []byte, value string) []byte {
	const hex = "0123456789abcdef"
	destination = append(destination, '"')
	for _, character := range value {
		switch character {
		case '\\', '"':
			destination = append(destination, '\\', byte(character))
		case '\b':
			destination = append(destination, `\b`...)
		case '\f':
			destination = append(destination, `\f`...)
		case '\n':
			destination = append(destination, `\n`...)
		case '\r':
			destination = append(destination, `\r`...)
		case '\t':
			destination = append(destination, `\t`...)
		default:
			if character < 0x20 {
				destination = append(destination, `\u00`...)
				destination = append(destination, hex[(character>>4)&15], hex[character&15])
			} else {
				destination = utf8.AppendRune(destination, character)
			}
		}
	}
	return append(destination, '"')
}

func fetchManifest(ctx context.Context, transport assetdownload.Transport, rawURL string) (_ release.LayeredAssetManifest, resultErr error) {
	response, err := transport.Fetch(ctx, assetdownload.TransportRequest{URL: rawURL, MaxBytes: maxManifestBytes})
	if err != nil {
		if response.Body != nil {
			err = errors.Join(err, wrapManifestBodyCloseError(response.Body.Close()))
		}
		return release.LayeredAssetManifest{}, err
	}
	if response.Body == nil {
		return release.LayeredAssetManifest{}, fmt.Errorf("manifest response body is required")
	}
	defer func() {
		resultErr = errors.Join(resultErr, wrapManifestBodyCloseError(response.Body.Close()))
	}()
	if response.StatusCode != 200 {
		return release.LayeredAssetManifest{}, fmt.Errorf("layered manifest request failed: status %d", response.StatusCode)
	}
	if response.ContentLength > maxManifestBytes {
		return release.LayeredAssetManifest{}, fmt.Errorf("layered manifest exceeds %d bytes", maxManifestBytes)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxManifestBytes+1))
	if err != nil {
		return release.LayeredAssetManifest{}, err
	}
	if int64(len(content)) > maxManifestBytes {
		return release.LayeredAssetManifest{}, fmt.Errorf("layered manifest exceeds %d bytes", maxManifestBytes)
	}
	return release.DecodeLayeredAssetManifest(content)
}

func wrapManifestBodyCloseError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close layered manifest response body: %w", err)
}

type httpsURL struct {
	raw       string
	origin    string
	directory string
}

func (parsed httpsURL) String() string {
	return parsed.raw
}

func strictHTTPSURL(rawURL string) (httpsURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if len(rawURL) <= len("https://") || !strings.EqualFold(rawURL[:len("https://")], "https://") || strings.ContainsAny(rawURL, "?#\\") {
		return httpsURL{}, fmt.Errorf("layered URL must be HTTPS without credentials, query, or fragment")
	}
	remainder := rawURL[len("https://"):]
	slash := strings.IndexByte(remainder, '/')
	authority := remainder
	pathValue := ""
	if slash >= 0 {
		authority = remainder[:slash]
		pathValue = remainder[slash:]
	}
	if !validHTTPSAuthority(authority) || strings.ContainsAny(authority, "@%") || containsURLControl(authority) || containsURLControl(pathValue) {
		return httpsURL{}, fmt.Errorf("layered URL must be HTTPS without credentials, query, or fragment")
	}
	origin := rawURL[:len("https://")] + authority
	directory := "/"
	if pathValue != "" {
		directory = pathValue[:strings.LastIndexByte(pathValue, '/')+1]
	}
	return httpsURL{raw: rawURL, origin: origin, directory: directory}, nil
}

func validHTTPSAuthority(authority string) bool {
	if authority == "" {
		return false
	}
	if authority[0] == '[' {
		closing := strings.IndexByte(authority, ']')
		if closing <= 1 || !validIPv6Literal(authority[1:closing]) {
			return false
		}
		remainder := authority[closing+1:]
		return remainder == "" || strings.HasPrefix(remainder, ":") && validHTTPSPort(remainder[1:])
	}
	if strings.ContainsAny(authority, "[]") || strings.Count(authority, ":") > 1 {
		return false
	}
	host := authority
	port := ""
	if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		host, port = authority[:colon], authority[colon+1:]
		if !validHTTPSPort(port) {
			return false
		}
	}
	if host == "" || strings.Contains(host, "..") {
		return false
	}
	for index := 0; index < len(host); index++ {
		character := host[index]
		if !isASCIIAlphaNumeric(character) && character != '.' && character != '-' {
			return false
		}
	}
	return true
}

func validIPv6Literal(host string) bool {
	if !strings.Contains(host, ":") {
		return false
	}
	for index := 0; index < len(host); index++ {
		character := host[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F') || character == ':' || character == '.') {
			return false
		}
	}
	return true
}

func validHTTPSPort(port string) bool {
	if port == "" || len(port) > 5 {
		return false
	}
	value := 0
	for index := 0; index < len(port); index++ {
		if port[index] < '0' || port[index] > '9' {
			return false
		}
		value = value*10 + int(port[index]-'0')
	}
	return value > 0 && value <= 65535
}

func isASCIIAlphaNumeric(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}

func resolveAssetURL(manifestURL httpsURL, relativePath string) (httpsURL, error) {
	if relativePath == "" || path.Clean(relativePath) != relativePath || strings.HasPrefix(relativePath, "/") || strings.ContainsAny(relativePath, "\\?#:%") {
		return httpsURL{}, fmt.Errorf("invalid layered asset URL")
	}
	return httpsURL{raw: manifestURL.origin + manifestURL.directory + relativePath, origin: manifestURL.origin}, nil
}

func containsURLControl(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] >= 0x7f {
			return true
		}
	}
	return false
}

type runOptions struct {
	manifestURL     string
	rootPublicKey   string
	expectedVersion string
	platform        string
	cacheDir        string
	attemptDir      string
	launcher        attemptLauncher
	mode            string
	args            []string
}

func parseRunArgs(args []string) (runOptions, error) {
	var opts runOptions
	seen := make(map[string]bool, 6)
	for index := 0; index < len(args); index++ {
		name := args[index]
		if name == "--" {
			opts.args = append([]string(nil), args[index+1:]...)
			return opts, nil
		}
		if !strings.HasPrefix(name, "--") || index+1 >= len(args) || seen[name] {
			return runOptions{}, fmt.Errorf("invalid or duplicate layered-run argument %q", name)
		}
		seen[name] = true
		index++
		value := args[index]
		switch name {
		case "--manifest-url":
			opts.manifestURL = value
		case "--root-public-key":
			opts.rootPublicKey = value
		case "--expected-release-version":
			opts.expectedVersion = value
		case "--platform":
			opts.platform = value
		case "--cache-dir":
			opts.cacheDir = value
		case "--attempt-dir":
			opts.attemptDir = value
		case "--launcher":
			opts.launcher = attemptLauncher(value)
		case "--mode":
			opts.mode = value
		default:
			return runOptions{}, fmt.Errorf("unknown layered-run argument %q", name)
		}
	}
	return opts, nil
}

func validCoreTransport(args []string) bool {
	found := false
	for index := 0; index < len(args); index++ {
		if args[index] == "--transport" {
			if found || index+1 >= len(args) || args[index+1] != "auto" {
				return false
			}
			found = true
			index++
			continue
		}
		if strings.HasPrefix(args[index], "--transport=") {
			return false
		}
	}
	return found
}

func validateRuntime(path string, expectedSize int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != expectedSize {
		return fmt.Errorf("downloaded runtime is not a regular file with the signed size")
	}
	return nil
}

func openVerifiedRuntime(path string, expectedSize int64, expectedSHA256 string) (*os.File, error) {
	if err := validateRuntime(path, expectedSize); err != nil {
		return nil, err
	}
	file, err := openPrivateRuntime(path)
	if err != nil {
		return nil, err
	}
	valid := false
	defer func() {
		if !valid {
			_ = file.Close()
		}
	}()
	if err := recheckVerifiedRuntime(file, path, expectedSize, expectedSHA256); err != nil {
		return nil, err
	}
	valid = true
	return file, nil
}

func recheckVerifiedRuntime(file *os.File, path string, expectedSize int64, expectedSHA256 string) error {
	if file == nil {
		return fmt.Errorf("downloaded runtime verification handle is required")
	}
	if err := validatePrivateCacheFile(path, expectedSize); err != nil {
		return err
	}
	info, err := file.Stat()
	pathInfo, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || !info.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) || info.Size() != expectedSize {
		return fmt.Errorf("downloaded runtime identity changed before verification")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, expectedSize+1))
	if err != nil {
		return err
	}
	actualSHA256 := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if written != expectedSize || actualSHA256 != expectedSHA256 {
		return fmt.Errorf("downloaded runtime does not match the signed digest and size")
	}
	postInfo, err := file.Stat()
	postPathInfo, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || !os.SameFile(postInfo, postPathInfo) || postInfo.Size() != expectedSize {
		return fmt.Errorf("downloaded runtime changed during final verification")
	}
	return nil
}

func (a App) stdout() io.Writer {
	if a.Stdout != nil {
		return a.Stdout
	}
	return os.Stdout
}

func (a App) stderr() io.Writer {
	if a.Stderr != nil {
		return a.Stderr
	}
	return os.Stderr
}

func (a App) stdin() io.Reader {
	if a.Stdin != nil {
		return a.Stdin
	}
	return os.Stdin
}

func (a App) now() time.Time {
	if a.Clock != nil {
		if now := a.Clock(); !now.IsZero() {
			return now
		}
	}
	if !a.Now.IsZero() {
		return a.Now
	}
	return time.Now()
}

func (a App) commandContext(ctx context.Context, path string, args ...string) *exec.Cmd {
	if a.CommandContext != nil {
		return a.CommandContext(ctx, path, args...)
	}
	return exec.CommandContext(ctx, path, args...)
}
