package bootstrapcmd

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

const LayeredRunReportSchemaVersion = "rdev.layered-run-report.v1"

const maxLayeredManifestBytes = 1 << 20

type App struct {
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
	Client         *http.Client
	CommandContext func(context.Context, string, ...string) *exec.Cmd
}

type LayeredRunOptions struct {
	ManifestURL            string
	Root                   model.TrustBundle
	ExpectedReleaseVersion string
	Platform               string
	CacheDir               string
	Mode                   string
	Args                   []string
	Client                 *http.Client
	Now                    time.Time
}

type LayeredRunReport struct {
	SchemaVersion string            `json:"schema_version"`
	AssetID       string            `json:"asset_id"`
	FromCache     bool              `json:"from_cache"`
	Resumed       bool              `json:"resumed"`
	Bytes         int64             `json:"bytes"`
	Stages        []LayeredRunStage `json:"stages"`
}

type LayeredRunStage struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
}

type layeredRuntime struct {
	path   string
	digest [sha256.Size]byte
	size   int64
}

func (a App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing rdev-bootstrap subcommand")
	}
	switch args[0] {
	case "layered-run":
		return a.layeredRun(ctx, args[1:])
	case "help", "-h", "--help":
		_, _ = fmt.Fprintln(a.stdout(), "usage: rdev-bootstrap layered-run --manifest-url URL --root-public-key KEY --expected-release-version VERSION --platform OS/ARCH --cache-dir PATH --mode temporary [-- core-args...]")
		return nil
	default:
		return fmt.Errorf("unknown rdev-bootstrap subcommand %q", args[0])
	}
}

func (a App) layeredRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rdev-bootstrap layered-run", flag.ContinueOnError)
	fs.SetOutput(a.stderr())
	manifestURL := fs.String("manifest-url", "", "signed layered asset manifest URL")
	runnerManifest := fs.String("runner-manifest", "", "connection entry runner manifest path")
	rootPublicKey := fs.String("root-public-key", "", "pinned release root, formatted key_id:base64url_public_key")
	expectedReleaseVersion := fs.String("expected-release-version", "", "required signed layered asset release version")
	platform := fs.String("platform", "", "layered runtime platform")
	cacheDir := fs.String("cache-dir", "", "user-scoped verified runtime cache directory")
	mode := fs.String("mode", "", "bootstrap mode; must be temporary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	coreArgs := append([]string(nil), fs.Args()...)
	if strings.TrimSpace(*runnerManifest) != "" {
		runner, err := readBootstrapRunnerManifest(*runnerManifest)
		if err != nil {
			return err
		}
		if strings.TrimSpace(*manifestURL) == "" {
			*manifestURL = runner.LayeredAssetsManifestURL
		}
		if strings.TrimSpace(*platform) == "" {
			*platform = runner.TargetOS + "/" + runner.TargetArch
		}
		if len(coreArgs) == 0 {
			coreArgs = runner.coreArgs()
		}
	}
	if strings.TrimSpace(*mode) != "temporary" {
		return fmt.Errorf("layered-run requires --mode temporary")
	}
	if !supportedLayeredPlatform(strings.TrimSpace(*platform)) {
		return fmt.Errorf("layered-run requires a supported --platform")
	}
	if strings.TrimSpace(*expectedReleaseVersion) == "" {
		return fmt.Errorf("layered-run requires expected release version via --expected-release-version")
	}
	root, err := trustref.Parse(*rootPublicKey)
	if err != nil {
		return fmt.Errorf("root public key: %w", err)
	}
	report, runtime, err := runLayeredTemporary(ctx, LayeredRunOptions{
		ManifestURL:            strings.TrimSpace(*manifestURL),
		Root:                   root,
		ExpectedReleaseVersion: strings.TrimSpace(*expectedReleaseVersion),
		Platform:               strings.TrimSpace(*platform),
		CacheDir:               strings.TrimSpace(*cacheDir),
		Mode:                   strings.TrimSpace(*mode),
		Args:                   append([]string(nil), coreArgs...),
		Client:                 a.client(),
	})
	if err != nil {
		return err
	}
	executable, err := openVerifiedLayeredRuntime(runtime)
	if err != nil {
		return fmt.Errorf("lock verified layered runtime: %w", err)
	}
	defer executable.Close()
	cmd := a.commandContext(ctx, runtime.path, coreArgs...)
	if cmd == nil {
		return fmt.Errorf("layered runtime command is nil")
	}
	if err := recheckVerifiedLayeredRuntime(executable, runtime); err != nil {
		return fmt.Errorf("layered runtime changed before execution: %w", err)
	}
	if err := json.NewEncoder(a.stdout()).Encode(report); err != nil {
		return err
	}
	cmd.Stdout = a.stdout()
	cmd.Stderr = a.stderr()
	cmd.Stdin = a.stdin()
	return cmd.Run()
}

type bootstrapRunnerManifest struct {
	SchemaVersion            string `json:"schema_version"`
	TargetOS                 string `json:"target_os"`
	TargetArch               string `json:"target_arch"`
	Mode                     string `json:"mode"`
	HostName                 string `json:"host_name"`
	ManifestURL              string `json:"manifest_url"`
	ManifestRootPublicKey    string `json:"manifest_root_public_key"`
	LayeredAssetsManifestURL string `json:"layered_assets_manifest_url"`
	GatewayURL               string `json:"gateway_url"`
}

func readBootstrapRunnerManifest(path string) (bootstrapRunnerManifest, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return bootstrapRunnerManifest{}, fmt.Errorf("runner manifest path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return bootstrapRunnerManifest{}, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxLayeredManifestBytes+1))
	if err != nil {
		return bootstrapRunnerManifest{}, err
	}
	if len(content) > maxLayeredManifestBytes {
		return bootstrapRunnerManifest{}, fmt.Errorf("runner manifest exceeds %d bytes", maxLayeredManifestBytes)
	}
	var manifest bootstrapRunnerManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return bootstrapRunnerManifest{}, fmt.Errorf("decode runner manifest: %w", err)
	}
	if manifest.SchemaVersion != "rdev.connection-entry.runner.v1" ||
		!supportedLayeredPlatform(manifest.TargetOS+"/"+manifest.TargetArch) ||
		strings.TrimSpace(manifest.LayeredAssetsManifestURL) == "" ||
		strings.TrimSpace(manifest.ManifestURL) == "" ||
		strings.TrimSpace(manifest.ManifestRootPublicKey) == "" ||
		strings.TrimSpace(manifest.GatewayURL) == "" {
		return bootstrapRunnerManifest{}, fmt.Errorf("runner manifest is incomplete")
	}
	return manifest, nil
}

func (manifest bootstrapRunnerManifest) coreArgs() []string {
	mode := strings.TrimSpace(manifest.Mode)
	if mode == "" || mode == "attended-temporary" {
		mode = "temporary"
	}
	args := []string{
		"--mode", mode,
		"--gateway", manifest.GatewayURL,
		"--manifest-url", manifest.ManifestURL,
		"--manifest-root-public-key", manifest.ManifestRootPublicKey,
		"--transport", "auto",
		"--once=false",
	}
	if strings.TrimSpace(manifest.HostName) != "" {
		args = append(args, "--name", manifest.HostName)
	}
	return args
}

func RunLayeredTemporary(ctx context.Context, opts LayeredRunOptions) (LayeredRunReport, string, error) {
	report, runtime, err := runLayeredTemporary(ctx, opts)
	return report, runtime.path, err
}

func runLayeredTemporary(ctx context.Context, opts LayeredRunOptions) (LayeredRunReport, layeredRuntime, error) {
	manifestURL, origin, client, now, err := prepareLayeredRequest(opts)
	if err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	report := LayeredRunReport{SchemaVersion: LayeredRunReportSchemaVersion}

	started := time.Now()
	manifest, finalManifestURL, err := fetchLayeredManifest(ctx, client, manifestURL)
	if err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	report.Stages = append(report.Stages, layeredStage("manifest-fetch", started))
	if now.IsZero() {
		now = time.Now()
	}

	started = time.Now()
	if err := release.VerifyLayeredAssetManifest(manifest, opts.Root, now); err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	if manifest.Version != opts.ExpectedReleaseVersion {
		return LayeredRunReport{}, layeredRuntime{}, fmt.Errorf("layered manifest release version %q does not match expected release version %q", manifest.Version, opts.ExpectedReleaseVersion)
	}
	asset, err := release.SelectLayeredAsset(manifest, opts.Platform, "core-runtime", nil)
	if err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	assetURL, err := resolveLayeredAssetURL(finalManifestURL, asset.RelativePath, origin)
	if err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	report.Stages = append(report.Stages, layeredStage("signature-verification", started))

	started = time.Now()
	paths, err := prepareLayeredCachePaths(opts.CacheDir, asset)
	if err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	result, err := assetdownload.Download(ctx, assetdownload.Options{
		Mirrors:        []assetdownload.Mirror{{URL: assetURL.String()}},
		ExpectedSHA256: asset.SHA256,
		ExpectedSize:   asset.SizeBytes,
		OutputPath:     paths.output,
		CachePath:      paths.content,
		Transport:      assetdownload.HTTPTransport{Client: client},
	})
	if err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	if result.Bytes != asset.SizeBytes {
		return LayeredRunReport{}, layeredRuntime{}, fmt.Errorf("downloaded runtime size does not match signed manifest")
	}
	report.Stages = append(report.Stages, layeredStage("runtime-download", started))

	started = time.Now()
	if err := secureLayeredResultFiles(paths, asset); err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	runtime, err := layeredRuntimeForAsset(paths.output, asset)
	if err != nil {
		return LayeredRunReport{}, layeredRuntime{}, err
	}
	report.Stages = append(report.Stages, layeredStage("runtime-launch-preparation", started))
	report.AssetID = asset.ID
	report.FromCache = result.FromCache
	report.Resumed = result.Resumed
	report.Bytes = result.Bytes
	return report, runtime, nil
}

type layeredCachePaths struct {
	output  string
	content string
}

func layeredRuntimeForAsset(path string, asset release.LayeredAsset) (layeredRuntime, error) {
	digestBytes, err := hex.DecodeString(strings.TrimPrefix(asset.SHA256, "sha256:"))
	if err != nil || len(digestBytes) != sha256.Size {
		return layeredRuntime{}, fmt.Errorf("signed layered runtime digest is invalid")
	}
	var digest [sha256.Size]byte
	copy(digest[:], digestBytes)
	return layeredRuntime{path: path, digest: digest, size: asset.SizeBytes}, nil
}

func recheckVerifiedLayeredRuntime(file *os.File, runtime layeredRuntime) error {
	if file == nil {
		return fmt.Errorf("protected layered runtime handle is required")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	pathInfo, err := os.Lstat(runtime.path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) || info.Size() != runtime.size {
		return fmt.Errorf("protected layered runtime path or size changed")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, runtime.size+1))
	if err != nil {
		return err
	}
	if written != runtime.size || subtle.ConstantTimeCompare(digest.Sum(nil), runtime.digest[:]) != 1 {
		return fmt.Errorf("protected layered runtime digest or size changed")
	}
	postInfo, err := file.Stat()
	if err != nil {
		return err
	}
	postPathInfo, err := os.Lstat(runtime.path)
	if err != nil {
		return err
	}
	if !postInfo.Mode().IsRegular() || postPathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(postInfo, postPathInfo) || postInfo.Size() != runtime.size {
		return fmt.Errorf("protected layered runtime changed during final verification")
	}
	_, err = file.Seek(0, io.SeekStart)
	return err
}

func prepareLayeredRequest(opts LayeredRunOptions) (*url.URL, *url.URL, *http.Client, time.Time, error) {
	if opts.Mode != "temporary" {
		return nil, nil, nil, time.Time{}, fmt.Errorf("layered run requires mode temporary")
	}
	if !supportedLayeredPlatform(opts.Platform) {
		return nil, nil, nil, time.Time{}, fmt.Errorf("layered run requires a supported platform")
	}
	if strings.TrimSpace(opts.ExpectedReleaseVersion) == "" {
		return nil, nil, nil, time.Time{}, fmt.Errorf("expected release version is required")
	}
	if strings.TrimSpace(opts.CacheDir) == "" {
		return nil, nil, nil, time.Time{}, fmt.Errorf("cache directory is required")
	}
	manifestURL, err := parseLayeredHTTPSURL(opts.ManifestURL)
	if err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	return manifestURL, manifestURL, cloneLayeredHTTPClient(opts.Client, manifestURL), opts.Now, nil
}

func supportedLayeredPlatform(platform string) bool {
	switch strings.TrimSpace(platform) {
	case "windows/amd64", "windows/arm64", "darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64":
		return true
	default:
		return false
	}
}

func parseLayeredHTTPSURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid layered URL")
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil || strings.Contains(rawURL, "#") || parsed.RawQuery != "" || parsed.ForceQuery {
		return nil, fmt.Errorf("layered URL must be an HTTPS URL without credentials, query, or fragment")
	}
	return parsed, nil
}

func cloneLayeredHTTPClient(base *http.Client, origin *url.URL) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	cloned := *base
	originalPolicy := base.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if !sameLayeredOrigin(req.URL, origin) {
			return fmt.Errorf("layered redirect must remain same-origin HTTPS")
		}
		if originalPolicy != nil {
			if err := originalPolicy(req, via); err != nil {
				return err
			}
		}
		if !sameLayeredOrigin(req.URL, origin) {
			return fmt.Errorf("layered redirect policy changed the request origin")
		}
		return nil
	}
	return &cloned
}

func sameLayeredOrigin(candidate, origin *url.URL) bool {
	return candidate != nil && origin != nil &&
		strings.EqualFold(candidate.Scheme, "https") &&
		strings.EqualFold(origin.Scheme, "https") &&
		strings.EqualFold(candidate.Host, origin.Host) &&
		candidate.User == nil && candidate.RawQuery == "" && !candidate.ForceQuery && candidate.Fragment == ""
}

func fetchLayeredManifest(ctx context.Context, client *http.Client, manifestURL *url.URL) (release.LayeredAssetManifest, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL.String(), nil)
	if err != nil {
		return release.LayeredAssetManifest{}, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return release.LayeredAssetManifest{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return release.LayeredAssetManifest{}, nil, fmt.Errorf("layered manifest request failed: %s", resp.Status)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxLayeredManifestBytes+1))
	if err != nil {
		return release.LayeredAssetManifest{}, nil, err
	}
	if len(content) > maxLayeredManifestBytes {
		return release.LayeredAssetManifest{}, nil, fmt.Errorf("layered manifest exceeds %d bytes", maxLayeredManifestBytes)
	}
	manifest, err := release.DecodeLayeredAssetManifest(content)
	if err != nil {
		return release.LayeredAssetManifest{}, nil, fmt.Errorf("decode layered manifest: %w", err)
	}
	finalURL := manifestURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL
	}
	return manifest, finalURL, nil
}

func resolveLayeredAssetURL(manifestURL *url.URL, relativePath string, origin *url.URL) (*url.URL, error) {
	reference, err := url.Parse(relativePath)
	if err != nil {
		return nil, err
	}
	resolved := manifestURL.ResolveReference(reference)
	if !sameLayeredOrigin(resolved, origin) {
		return nil, fmt.Errorf("layered asset URL must remain same-origin HTTPS")
	}
	return resolved, nil
}

func prepareLayeredCachePaths(cacheDir string, asset release.LayeredAsset) (layeredCachePaths, error) {
	cacheDir, err := filepath.Abs(strings.TrimSpace(cacheDir))
	if err != nil {
		return layeredCachePaths{}, err
	}
	digest := strings.TrimPrefix(asset.SHA256, "sha256:")
	paths := layeredCachePaths{
		output:  filepath.Join(cacheDir, "runtime", digest, filepath.Base(asset.RelativePath)),
		content: filepath.Join(cacheDir, "content", digest),
	}
	for _, dir := range []string{cacheDir, filepath.Join(cacheDir, "runtime"), filepath.Dir(paths.output), filepath.Join(cacheDir, "content")} {
		if err := ensureLayeredDirectory(dir); err != nil {
			return layeredCachePaths{}, err
		}
	}
	for _, path := range []string{paths.output, paths.output + ".tmp", paths.content, paths.content + ".tmp"} {
		if err := secureLayeredFileIfExists(path, asset.SizeBytes); err != nil {
			return layeredCachePaths{}, err
		}
	}
	if err := secureLayeredPartialFileIfExists(paths.output+".part", asset.SizeBytes); err != nil {
		return layeredCachePaths{}, err
	}
	return paths, nil
}

func ensureLayeredDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("layered cache path must be a directory without symlinks")
	}
	return os.Chmod(path, 0o700)
}

func secureLayeredFileIfExists(path string, maxBytes int64) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("layered cache file must be regular and not a symlink")
	}
	if info.Size() > maxBytes {
		return fmt.Errorf("layered cache file exceeds signed runtime size")
	}
	return os.Chmod(path, 0o600)
}

func secureLayeredPartialFileIfExists(path string, maxBytes int64) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("layered cache partial must be regular and not a symlink")
	}
	if info.Size() > maxBytes {
		return os.Remove(path)
	}
	return os.Chmod(path, 0o600)
}

func secureLayeredResultFiles(paths layeredCachePaths, asset release.LayeredAsset) error {
	info, err := os.Lstat(paths.output)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("layered result must be a regular file without symlinks")
	}
	sha, err := fileSHA256(paths.output)
	finalInfo, finalStatErr := os.Lstat(paths.output)
	if err != nil || finalStatErr != nil || finalInfo.Mode()&os.ModeSymlink != 0 || !finalInfo.Mode().IsRegular() || "sha256:"+sha != asset.SHA256 || finalInfo.Size() != asset.SizeBytes {
		return fmt.Errorf("layered result does not match signed digest and size")
	}
	if err := os.Chmod(paths.output, 0o600); err != nil {
		return err
	}
	contentInfo, err := os.Lstat(paths.content)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if contentInfo.Mode()&os.ModeSymlink != 0 || !contentInfo.Mode().IsRegular() {
		return fmt.Errorf("layered content cache must be a regular file without symlinks")
	}
	return os.Chmod(paths.content, 0o600)
}

func layeredStage(name string, started time.Time) LayeredRunStage {
	return LayeredRunStage{Name: name, DurationMS: time.Since(started).Milliseconds()}
}

func (a App) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
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

func (a App) commandContext(ctx context.Context, path string, args ...string) *exec.Cmd {
	if a.CommandContext != nil {
		return a.CommandContext(ctx, path, args...)
	}
	return exec.CommandContext(ctx, path, args...)
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
