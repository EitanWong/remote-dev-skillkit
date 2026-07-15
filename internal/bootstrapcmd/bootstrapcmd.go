package bootstrapcmd

import (
	"compress/gzip"
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
	"runtime"
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
	case "upgrade":
		return a.upgrade(ctx, args[1:])
	case "layered-run":
		return a.layeredRun(ctx, args[1:])
	case "help", "-h", "--help":
		_, _ = fmt.Fprintln(a.stdout(), "usage: rdev-bootstrap upgrade --gateway-url URL --ticket-code CODE --asset NAME --out PATH [--mirror URL] [--cache PATH] [--no-exec] [-- full-helper-args...]")
		_, _ = fmt.Fprintln(a.stdout(), "       rdev-bootstrap layered-run --manifest-url URL --root-public-key KEY --expected-release-version VERSION --platform windows/amd64 --cache-dir PATH --mode temporary [-- rdev-host-args...]")
		return nil
	default:
		return fmt.Errorf("unknown rdev-bootstrap subcommand %q", args[0])
	}
}

func (a App) layeredRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rdev-bootstrap layered-run", flag.ContinueOnError)
	fs.SetOutput(a.stderr())
	manifestURL := fs.String("manifest-url", "", "signed layered asset manifest URL")
	rootPublicKey := fs.String("root-public-key", "", "pinned release root, formatted key_id:base64url_public_key")
	expectedReleaseVersion := fs.String("expected-release-version", "", "required signed layered asset release version")
	platform := fs.String("platform", "", "layered runtime platform; must be windows/amd64")
	cacheDir := fs.String("cache-dir", "", "user-scoped verified runtime cache directory")
	mode := fs.String("mode", "", "bootstrap mode; must be temporary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*mode) != "temporary" {
		return fmt.Errorf("layered-run requires --mode temporary")
	}
	if strings.TrimSpace(*platform) != "windows/amd64" {
		return fmt.Errorf("layered-run requires --platform windows/amd64")
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
		Args:                   append([]string(nil), fs.Args()...),
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
	cmd := a.commandContext(ctx, runtime.path, fs.Args()...)
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
		Client:         client,
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
	if opts.Platform != "windows/amd64" {
		return nil, nil, nil, time.Time{}, fmt.Errorf("layered run requires platform windows/amd64")
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
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	return manifestURL, manifestURL, cloneLayeredHTTPClient(opts.Client, manifestURL), now, nil
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
	var manifest release.LayeredAssetManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
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

func (a App) upgrade(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rdev-bootstrap upgrade", flag.ContinueOnError)
	fs.SetOutput(a.stderr())
	gatewayURL := fs.String("gateway-url", "", "support-session gateway URL")
	ticketCode := fs.String("ticket-code", "", "support-session ticket code")
	asset := fs.String("asset", "", "full rdev helper asset name")
	out := fs.String("out", "", "downloaded full helper output path")
	cache := fs.String("cache", "", "optional verified helper cache path")
	noExec := fs.Bool("no-exec", false, "download and verify without executing the full helper")
	var mirrors stringListFlag
	fs.Var(&mirrors, "mirror", "additional raw helper mirror URL; may be repeated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts := upgradeOptions{
		GatewayURL: strings.TrimRight(strings.TrimSpace(*gatewayURL), "/"),
		TicketCode: strings.TrimSpace(*ticketCode),
		Asset:      strings.TrimSpace(*asset),
		Out:        strings.TrimSpace(*out),
		Cache:      strings.TrimSpace(*cache),
		Mirrors:    []string(mirrors),
		NoExec:     *noExec,
		ExecArgs:   fs.Args(),
	}
	return a.runUpgrade(ctx, opts)
}

type upgradeOptions struct {
	GatewayURL string
	TicketCode string
	Asset      string
	Out        string
	Cache      string
	Mirrors    []string
	NoExec     bool
	ExecArgs   []string
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value != "" {
		*f = append(*f, value)
	}
	return nil
}

func (a App) runUpgrade(ctx context.Context, opts upgradeOptions) error {
	if opts.GatewayURL == "" {
		return fmt.Errorf("gateway-url is required")
	}
	if opts.TicketCode == "" {
		return fmt.Errorf("ticket-code is required")
	}
	if opts.Asset == "" {
		return fmt.Errorf("asset is required")
	}
	if opts.Out == "" {
		return fmt.Errorf("out is required")
	}
	if strings.Contains(opts.Asset, "/") || strings.Contains(opts.Asset, `\`) {
		return fmt.Errorf("asset must be a file name")
	}
	absOut, err := filepath.Abs(opts.Out)
	if err != nil {
		return err
	}
	opts.Out = absOut
	if err := os.MkdirAll(filepath.Dir(opts.Out), 0o700); err != nil {
		return err
	}
	_ = a.postPreconnect(ctx, opts, "downloading-helper", "downloading verified helper")
	downloadResult, err := a.downloadVerifiedHelper(ctx, opts)
	if err != nil {
		_ = os.Remove(opts.Out)
		return err
	}
	if err := os.Chmod(opts.Out, 0o700); err != nil {
		return err
	}
	if opts.NoExec {
		return json.NewEncoder(a.stdout()).Encode(map[string]any{
			"schema_version": "rdev-bootstrap-upgrade-result.v1",
			"ok":             true,
			"verified":       true,
			"executed":       false,
			"helper":         opts.Out,
			"asset":          opts.Asset,
			"download":       downloadResult,
		})
	}
	_ = a.postPreconnect(ctx, opts, "starting-full-helper", "starting verified full helper")
	cmd := a.commandContext(ctx, opts.Out, opts.ExecArgs...)
	cmd.Stdout = a.stdout()
	cmd.Stderr = a.stderr()
	cmd.Stdin = a.stdin()
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func (a App) postPreconnect(ctx context.Context, opts upgradeOptions, phase, message string) error {
	body := map[string]any{
		"ticket_code": opts.TicketCode,
		"phase":       phase,
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"asset":       opts.Asset,
		"source":      "rdev-bootstrap-native",
		"message":     message,
	}
	content, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.GatewayURL+"/v1/support-session/preconnect", strings.NewReader(string(content)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("preconnect failed: %s", resp.Status)
	}
	return nil
}

func (a App) downloadVerifiedHelper(ctx context.Context, opts upgradeOptions) (assetdownload.Result, error) {
	assetsURL := opts.GatewayURL + "/assets/" + opts.Asset
	expected, err := a.downloadText(ctx, assetsURL+".sha256")
	if err != nil {
		return assetdownload.Result{}, err
	}
	cachePath := strings.TrimSpace(opts.Cache)
	if cachePath == "" {
		if path, ok := assetdownload.DefaultCachePath(opts.Asset); ok {
			cachePath = path
		}
	}
	mirrors := make([]assetdownload.Mirror, 0, len(opts.Mirrors)+1)
	for _, mirror := range opts.Mirrors {
		mirror = strings.TrimSpace(mirror)
		if mirror != "" {
			mirrors = append(mirrors, assetdownload.Mirror{URL: mirror, Kind: "operator-mirror"})
		}
	}
	mirrors = append(mirrors, assetdownload.Mirror{URL: assetsURL, Kind: "gateway-asset"})
	result, rawErr := assetdownload.Download(ctx, assetdownload.Options{
		Mirrors:        mirrors,
		OutputPath:     opts.Out,
		CachePath:      cachePath,
		ExpectedSHA256: strings.TrimSpace(expected),
		Client:         a.client(),
	})
	if rawErr == nil {
		return result, nil
	}
	if strings.Contains(strings.ToLower(rawErr.Error()), "checksum mismatch") {
		return assetdownload.Result{}, rawErr
	}
	if err := a.downloadGzip(ctx, assetsURL+".gz", opts.Out); err != nil {
		return assetdownload.Result{}, rawErr
	}
	actual, err := fileSHA256(opts.Out)
	if err != nil {
		return assetdownload.Result{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(expected), actual) {
		return assetdownload.Result{}, fmt.Errorf("rdev helper SHA-256 mismatch: expected %s got %s", strings.TrimSpace(expected), actual)
	}
	info, err := os.Stat(opts.Out)
	if err != nil {
		return assetdownload.Result{}, err
	}
	result = assetdownload.Result{
		OutputPath: opts.Out,
		SourceURL:  assetsURL + ".gz",
		Bytes:      info.Size(),
		SHA256:     "sha256:" + actual,
		Transcript: []assetdownload.Event{{Phase: "download-verified", URL: assetsURL + ".gz", Bytes: info.Size()}},
	}
	return result, nil
}

func (a App) downloadGzip(ctx context.Context, rawURL, out string) error {
	resp, err := a.getWithRetry(ctx, rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s failed: %s", rawURL, resp.Status)
	}
	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer reader.Close()
	return writeFileAtomically(out, reader)
}

func (a App) downloadFile(ctx context.Context, rawURL, out string) error {
	resp, err := a.getWithRetry(ctx, rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s failed: %s", rawURL, resp.Status)
	}
	return writeFileAtomically(out, resp.Body)
}

func (a App) downloadText(ctx context.Context, rawURL string) (string, error) {
	resp, err := a.getWithRetry(ctx, rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s failed: %s", rawURL, resp.Status)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (a App) getWithRetry(ctx context.Context, rawURL string) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := a.client().Do(req)
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}
		if resp != nil {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("GET %s returned %s", rawURL, resp.Status)
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
		}
	}
	return nil, lastErr
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

func writeFileAtomically(path string, reader io.Reader) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
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
