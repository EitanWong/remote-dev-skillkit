package bootstrapcmd

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
)

type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Client *http.Client
}

func (a App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing rdev-bootstrap subcommand")
	}
	switch args[0] {
	case "upgrade":
		return a.upgrade(ctx, args[1:])
	case "help", "-h", "--help":
		_, _ = fmt.Fprintln(a.stdout(), "usage: rdev-bootstrap upgrade --gateway-url URL --ticket-code CODE --asset NAME --out PATH [--mirror URL] [--cache PATH] [--no-exec] [-- full-helper-args...]")
		return nil
	default:
		return fmt.Errorf("unknown rdev-bootstrap subcommand %q", args[0])
	}
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
	cmd := exec.CommandContext(ctx, opts.Out, opts.ExecArgs...)
	cmd.Stdout = a.stdout()
	cmd.Stderr = a.stderr()
	cmd.Stdin = os.Stdin
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
