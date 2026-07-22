//go:build !rdev_bootstrap_focused

package windowsentry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

func TestWindowsEntryRejectsEveryNonLayeredSubcommand(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"upgrade"},
		{"help"},
		{"doctor"},
		{"--help"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			app := App{Transport: &recordingTransport{}}
			if err := app.Run(t.Context(), args); err == nil {
				t.Fatalf("App.Run(%q) accepted a command other than layered-run", args)
			}
		})
	}
}

func TestWindowsEntryAttemptCheckInitializesAndReusesPreCoreState(t *testing.T) {
	directory := privateAttemptDirForTest(t)
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	app := App{Now: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)}
	args := []string{"layered-run", "attempt-check", "--attempt-dir", directory, "--launcher", "powershell", "--create"}
	if err := app.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	state, err := readAttemptState(filepath.Join(directory, attemptStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if state.AttemptID != filepath.Base(directory) || state.Stage != attemptStagePreCore || state.Launcher != launcherPowerShell {
		t.Fatalf("unexpected initialized attempt state: %#v", state)
	}
	if _, err := os.Stat(filepath.Join(directory, attemptLockFilename)); !os.IsNotExist(err) {
		t.Fatalf("attempt-check left its lock behind: %v", err)
	}
	if err := app.Run(t.Context(), []string{"layered-run", "attempt-check", "--attempt-dir", directory, "--launcher", "cmd"}); err != nil {
		t.Fatalf("canonical pre_core state was not reusable: %v", err)
	}
	if err := app.Run(t.Context(), args); err == nil {
		t.Fatal("attempt-check reused an existing directory with --create")
	}
}

func TestWindowsEntryAttemptCheckRejectsInvalidGrammarAndClosedState(t *testing.T) {
	directory := privateAttemptDirForTest(t)
	app := App{Now: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)}
	for _, args := range [][]string{
		{"layered-run", "attempt-check"},
		{"layered-run", "attempt-check", "--attempt-dir", directory},
		{"layered-run", "attempt-check", "--attempt-dir", directory, "--launcher", "cmd", "extra"},
		{"layered-run", "attempt-check", "--attempt-dir", directory, "--attempt-dir", directory, "--launcher", "cmd"},
		{"layered-run", "attempt-check", "--attempt-dir", directory, "--launcher", "invalid"},
		{"layered-run", "attempt-check", "--unknown", "value", "--attempt-dir", directory, "--launcher", "cmd"},
		{"layered-run", "attempt-check", "--path", directory, "--kind", "directory"},
	} {
		if err := app.Run(t.Context(), args); err == nil {
			t.Fatalf("attempt-check accepted invalid arguments %q", args)
		}
	}
	if err := writeAttemptState(directory, attemptState{
		SchemaVersion: AttemptStateSchemaVersion,
		AttemptID:     filepath.Base(directory),
		Stage:         attemptStageCoreExited,
		Launcher:      launcherPowerShell,
		UpdatedAt:     "2026-07-17T08:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(t.Context(), []string{"layered-run", "attempt-check", "--attempt-dir", directory, "--launcher", "cmd"}); err == nil {
		t.Fatal("attempt-check accepted core_exited state")
	}
}

func TestWindowsEntryAttemptCheckWithoutCreateRejectsMissingDirectory(t *testing.T) {
	directory := privateAttemptDirForTest(t)
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	app := App{Now: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)}
	if err := app.Run(t.Context(), []string{"layered-run", "attempt-check", "--attempt-dir", directory, "--launcher", "cmd"}); err == nil {
		t.Fatal("attempt-check without --create accepted a missing directory")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("attempt-check without --create mutated the missing directory: %v", err)
	}
}

func TestWindowsEntryPrivatePathCheckAcceptsOnlyExactGrammarAndPrivateObjects(t *testing.T) {
	directory := privateAttemptDirForTest(t)
	filePath := privateLauncherFileForTest(t, directory, "launcher.cmd")
	app := App{}
	for _, args := range [][]string{
		{"layered-run", "private-path-check", "--path", directory, "--kind", "directory"},
		{"layered-run", "private-path-check", "--path", filePath, "--kind", "file"},
	} {
		if err := app.Run(t.Context(), args); err != nil {
			t.Fatalf("private path check rejected %q: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"layered-run", "private-path-check"},
		{"layered-run", "private-path-check", "--path", directory, "--kind", "other"},
		{"layered-run", "private-path-check", "--path", directory, "--kind", "shape-directory"},
		{"layered-run", "private-path-check", "--kind", "directory", "--path", directory},
		{"layered-run", "private-path-check", "--path", directory, "--kind", "directory", "extra"},
		{"layered-run", "private-path-check", "--attempt-dir", directory, "--launcher", "cmd"},
	} {
		if err := app.Run(t.Context(), args); err == nil {
			t.Fatalf("private path check accepted invalid arguments %q", args)
		}
	}
}

func TestWindowsEntryVerifiesManifestBeforeCoreAndDownloadsOnlyCore(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	transport := &recordingTransport{responses: map[string]transportFixture{
		fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
		fixture.coreURL:     {status: 200, content: fixture.core},
		fixture.helperURL:   {status: 200, content: []byte("optional helper must not be requested")},
	}}
	downloadCalls := 0
	var stdout bytes.Buffer
	app := App{
		Stdout:         &stdout,
		Stderr:         io.Discard,
		Transport:      transport,
		Now:            fixture.now,
		CommandContext: successfulTestCommand,
		download: func(ctx context.Context, opts assetdownload.Options) (assetdownload.Result, error) {
			downloadCalls++
			if opts.Transport != transport {
				t.Fatal("focused app did not inject its command transport into assetdownload.Download")
			}
			if len(opts.Mirrors) != 1 || opts.Mirrors[0].URL != fixture.coreURL {
				t.Fatalf("download selected a non-core asset: %#v", opts.Mirrors)
			}
			return assetdownload.Download(ctx, opts)
		},
	}
	if err := app.Run(t.Context(), fixture.args(t, windowsEntryTestCacheDir(t))); err != nil {
		t.Fatal(err)
	}
	if downloadCalls != 1 {
		t.Fatalf("assetdownload.Download calls = %d, want exactly 1", downloadCalls)
	}
	wantRequests := []string{fixture.manifestURL, fixture.coreURL}
	if fmt.Sprint(transport.requestURLs()) != fmt.Sprint(wantRequests) {
		t.Fatalf("transport requests = %q, want manifest then core only %q", transport.requestURLs(), wantRequests)
	}
	if strings.Contains(stdout.String(), fixture.helperURL) {
		t.Fatalf("focused report exposed or requested optional helper URL: %s", stdout.String())
	}
	var report RunReport
	if err := json.NewDecoder(&stdout).Decode(&report); err != nil {
		t.Fatalf("decode focused report: %v\n%s", err, stdout.String())
	}
	if report.AssetID != "windows-core" || report.Bytes != int64(len(fixture.core)) {
		t.Fatalf("unexpected focused report: %#v", report)
	}
}

func TestWindowsEntryRejectsBadSignatureBeforeAnyCoreRequest(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	var manifest release.LayeredAssetManifest
	if err := json.Unmarshal(fixture.manifestJSON, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Signature = strings.Repeat("A", len(manifest.Signature))
	badManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	transport := &recordingTransport{responses: map[string]transportFixture{
		fixture.manifestURL: {status: 200, content: badManifest},
		fixture.coreURL:     {status: 200, content: fixture.core},
	}}
	downloadCalls := 0
	app := App{
		Transport: transport,
		Now:       fixture.now,
		download: func(context.Context, assetdownload.Options) (assetdownload.Result, error) {
			downloadCalls++
			return assetdownload.Result{}, nil
		},
	}
	if err := app.Run(t.Context(), fixture.args(t, windowsEntryTestCacheDir(t))); err == nil {
		t.Fatal("invalid signed manifest was accepted")
	}
	if downloadCalls != 0 {
		t.Fatalf("assetdownload.Download ran %d times before manifest verification", downloadCalls)
	}
	if got := transport.requestURLs(); len(got) != 1 || got[0] != fixture.manifestURL {
		t.Fatalf("bad signature caused post-manifest request(s): %q", got)
	}
}

func TestWindowsEntryRejectsCoreChangedAfterDownload(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	transport := &recordingTransport{responses: map[string]transportFixture{
		fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
		fixture.coreURL:     {status: 200, content: fixture.core},
	}}
	commandCalled := false
	app := App{
		Transport: transport,
		Now:       fixture.now,
		download: func(ctx context.Context, opts assetdownload.Options) (assetdownload.Result, error) {
			result, err := assetdownload.Download(ctx, opts)
			if err != nil {
				return result, err
			}
			if err := os.WriteFile(result.OutputPath, bytes.Repeat([]byte("x"), len(fixture.core)), 0o600); err != nil {
				return assetdownload.Result{}, err
			}
			return result, nil
		},
		CommandContext: func(ctx context.Context, path string, args ...string) *exec.Cmd {
			commandCalled = true
			return successfulTestCommand(ctx, path, args...)
		},
	}
	if err := app.Run(t.Context(), fixture.args(t, windowsEntryTestCacheDir(t))); err == nil {
		t.Fatal("same-size core replacement was accepted")
	}
	if commandCalled {
		t.Fatal("core command was created after final digest verification failed")
	}
}

func TestWindowsEntryRejectsCoreReplacedAfterCommandCreation(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	transport := &recordingTransport{responses: map[string]transportFixture{
		fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
		fixture.coreURL:     {status: 200, content: fixture.core},
	}}
	commandCreated := false
	replacementBlocked := false
	var stdout bytes.Buffer
	app := App{
		Stdout:    &stdout,
		Transport: transport,
		Now:       fixture.now,
		CommandContext: func(ctx context.Context, runtimePath string, args ...string) *exec.Cmd {
			commandCreated = true
			replacement := runtimePath + ".replacement"
			if err := os.WriteFile(replacement, bytes.Repeat([]byte("x"), len(fixture.core)), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, runtimePath); err != nil {
				if runtime.GOOS == "windows" {
					replacementBlocked = true
					_ = os.Remove(replacement)
					return successfulTestCommand(ctx, runtimePath, args...)
				}
				t.Fatal(err)
			}
			return successfulTestCommand(ctx, runtimePath, args...)
		},
	}
	err := app.Run(t.Context(), fixture.args(t, windowsEntryTestCacheDir(t)))
	if replacementBlocked {
		if err != nil {
			t.Fatalf("locked core failed after replacement was blocked: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("core replacement after command creation was accepted")
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed final runtime verification emitted a success report: %s", stdout.String())
	}
	if !commandCreated {
		t.Fatal("test did not reach the final pre-start verification boundary")
	}
}

func TestWindowsEntryRejectsUnsafeCacheBeforeCoreDownload(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	root := canonicalWindowsEntryTestTempDir(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "cache-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("create cache symlink: %v", err)
	}

	for _, cacheDir := range []string{
		`\\server\share\rdev-cache`,
		`//server/share/rdev-cache`,
		filepath.Join(link, "nested"),
	} {
		t.Run(cacheDir, func(t *testing.T) {
			transport := &recordingTransport{responses: map[string]transportFixture{
				fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
			}}
			downloadCalls := 0
			app := App{
				Transport: transport,
				Now:       fixture.now,
				download: func(context.Context, assetdownload.Options) (assetdownload.Result, error) {
					downloadCalls++
					return assetdownload.Result{}, fmt.Errorf("unexpected download")
				},
			}
			if err := app.Run(t.Context(), fixture.args(t, cacheDir)); err == nil {
				t.Fatalf("unsafe cache path %q was accepted", cacheDir)
			}
			if downloadCalls != 0 {
				t.Fatalf("unsafe cache path %q reached core download", cacheDir)
			}
		})
	}
}

func TestWindowsEntryRejectsUnsafeManifestURLsBeforeTransport(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	unsafeURLs := []string{
		"http://downloads.example.test/layered.json",
		"https://user:secret@downloads.example.test/layered.json",
		"https://downloads.example.test/layered.json?channel=test",
		"https://downloads.example.test/layered.json#latest",
		"//downloads.example.test/layered.json",
		"https://:443/layered.json",
		"https://downloads.example.test:bad/layered.json",
		"https://downloads.example.test:65536/layered.json",
		"https://[::1/layered.json",
	}
	for _, rawURL := range unsafeURLs {
		t.Run(rawURL, func(t *testing.T) {
			transport := &recordingTransport{}
			args := fixture.args(t, windowsEntryTestCacheDir(t))
			args[2] = rawURL
			app := App{Transport: transport, Now: fixture.now}
			if err := app.Run(t.Context(), args); err == nil {
				t.Fatalf("unsafe manifest URL %q was accepted", rawURL)
			}
			if len(transport.requests) != 0 {
				t.Fatalf("unsafe manifest URL reached transport: %#v", transport.requests)
			}
		})
	}
}

func TestWindowsEntryRejectsInvalidRequiredFlagsBeforeTransport(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	for _, testCase := range []struct {
		name  string
		flag  string
		value string
	}{
		{name: "mode", flag: "--mode", value: "persistent"},
		{name: "platform", flag: "--platform", value: "linux/amd64"},
		{name: "version", flag: "--expected-release-version", value: ""},
		{name: "cache", flag: "--cache-dir", value: ""},
		{name: "attempt", flag: "--attempt-dir", value: ""},
		{name: "launcher", flag: "--launcher", value: "pwsh"},
		{name: "root", flag: "--root-public-key", value: "invalid"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			transport := &recordingTransport{}
			args := replaceWindowsEntryFlag(t, fixture.args(t, windowsEntryTestCacheDir(t)), testCase.flag, testCase.value)
			if err := (App{Transport: transport, Now: fixture.now}).Run(t.Context(), args); err == nil {
				t.Fatalf("invalid %s was accepted", testCase.flag)
			}
			if len(transport.requests) != 0 {
				t.Fatalf("invalid %s reached transport", testCase.flag)
			}
		})
	}
}

func TestWindowsEntryRejectsAmbiguousLayeredArguments(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	duplicateMode := fixture.args(t, windowsEntryTestCacheDir(t))
	separator := slices.Index(duplicateMode, "--")
	duplicateMode = append(append(append([]string(nil), duplicateMode[:separator]...), "--mode", "temporary"), duplicateMode[separator:]...)
	for _, args := range [][]string{
		duplicateMode,
		{"layered-run", "positional-before-flags"},
	} {
		transport := &recordingTransport{}
		if err := (App{Transport: transport, Now: fixture.now}).Run(t.Context(), args); err == nil {
			t.Fatalf("ambiguous focused arguments were accepted: %q", args)
		}
		if len(transport.requests) != 0 {
			t.Fatalf("ambiguous focused arguments reached transport: %q", args)
		}
	}
}

func TestWindowsEntryManifestFetchIsBounded(t *testing.T) {
	validURL := "https://downloads.example.test/layered.json"
	for _, testCase := range []struct {
		name      string
		transport assetdownload.Transport
	}{
		{
			name: "transport error",
			transport: transportFunc(func(context.Context, assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
				return assetdownload.TransportResponse{}, fmt.Errorf("fetch failed")
			}),
		},
		{
			name: "missing body",
			transport: transportFunc(func(context.Context, assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
				return assetdownload.TransportResponse{StatusCode: 200}, nil
			}),
		},
		{
			name: "bad status",
			transport: transportFunc(func(context.Context, assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
				return assetdownload.TransportResponse{StatusCode: 503, Body: io.NopCloser(strings.NewReader("retry"))}, nil
			}),
		},
		{
			name: "declared oversize",
			transport: transportFunc(func(context.Context, assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
				return assetdownload.TransportResponse{StatusCode: 200, ContentLength: maxManifestBytes + 1, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			}),
		},
		{
			name: "streamed oversize",
			transport: transportFunc(func(context.Context, assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
				return assetdownload.TransportResponse{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(io.LimitReader(zeroReader{}, maxManifestBytes+1))}, nil
			}),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := fetchManifest(t.Context(), testCase.transport, validURL); err == nil {
				t.Fatal("invalid manifest response was accepted")
			}
		})
	}
}

func TestWindowsEntryManifestFetchPropagatesBodyCloseFailure(t *testing.T) {
	closeErr := fmt.Errorf("manifest temporary cleanup failed")
	transport := transportFunc(func(context.Context, assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
		return assetdownload.TransportResponse{
			StatusCode:    200,
			ContentLength: 2,
			Body:          &errorCloseBody{Reader: strings.NewReader("{}"), Err: closeErr},
		}, nil
	})
	if _, err := fetchManifest(t.Context(), transport, "https://downloads.example.test/layered.json"); err == nil || !errors.Is(err, closeErr) {
		t.Fatalf("manifest response cleanup failure was not propagated: %v", err)
	}
}

func TestWindowsEntryAssetURLAndRuntimeValidation(t *testing.T) {
	manifestURL, err := strictHTTPSURL("https://downloads.example.test/releases/layered.json")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveAssetURL(manifestURL, "rdev-core.exe")
	if err != nil || resolved.String() != "https://downloads.example.test/releases/rdev-core.exe" {
		t.Fatalf("resolve same-origin core: %s, %v", resolved, err)
	}
	if _, err := resolveAssetURL(manifestURL, "https://other.example.test/rdev-core.exe"); err == nil {
		t.Fatal("cross-origin core URL was accepted")
	}
	for _, rawURL := range []string{
		"https://downloads.example.test:443/releases/layered.json",
		"https://[::1]:443/releases/layered.json",
	} {
		if _, err := strictHTTPSURL(rawURL); err != nil {
			t.Fatalf("safe HTTPS authority %q rejected: %v", rawURL, err)
		}
	}

	runtimePath := filepath.Join(t.TempDir(), "rdev-core.exe")
	if err := os.WriteFile(runtimePath, []byte("core"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(runtimePath, 4); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(runtimePath, 5); err == nil {
		t.Fatal("runtime with the wrong signed size was accepted")
	}
	linkPath := runtimePath + ".link"
	if err := os.Symlink(runtimePath, linkPath); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(linkPath, 4); err == nil {
		t.Fatal("runtime symlink was accepted")
	}
}

func TestWindowsEntryRejectsUnsafeWindowsCacheBasenames(t *testing.T) {
	for _, name := range []string{"", "CON", "con.exe", "PRN.txt", "COM1.exe", "LPT9", "core. ", "core.", "core<1>.exe", "core name.exe", "c\u00f6re.exe"} {
		if validWindowsCacheBasename(name) {
			t.Errorf("unsafe Windows cache basename %q was accepted", name)
		}
	}
	for _, name := range []string{"rdev-host.exe", "rdev-core-windows-amd64.exe", "core_v2.1.exe"} {
		if !validWindowsCacheBasename(name) {
			t.Errorf("safe Windows cache basename %q was rejected", name)
		}
	}
}

func TestWindowsEntryHostDefaults(t *testing.T) {
	if runtime.GOOS != "windows" {
		if _, err := defaultTransport(); err == nil {
			t.Fatal("non-Windows package unexpectedly created the Windows transport")
		}
	}
	app := App{}
	if app.stdout() != os.Stdout || app.stderr() != os.Stderr || app.stdin() != os.Stdin {
		t.Fatal("focused app did not preserve process standard streams")
	}
	if app.commandContext(t.Context(), os.Args[0]) == nil {
		t.Fatal("focused app did not create the default foreground command")
	}
}

func TestWindowsEntryHelperProcess(t *testing.T) {
	if os.Getenv("RDEV_WINDOWS_ENTRY_HELPER") != "1" {
		return
	}
	os.Exit(0)
}

type windowsEntryFixture struct {
	now          time.Time
	root         string
	manifestURL  string
	coreURL      string
	helperURL    string
	core         []byte
	manifestJSON []byte
}

func newWindowsEntryFixture(t *testing.T) windowsEntryFixture {
	t.Helper()
	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	core := []byte("representative verified Windows core runtime\n")
	helper := []byte("optional helper\n")
	coreDigest := sha256.Sum256(core)
	helperDigest := sha256.Sum256(helper)
	manifest, err := release.SignLayeredAssetManifest(release.LayeredAssetManifest{
		SchemaVersion: release.LayeredAssetManifestSchemaVersion,
		Version:       "v2.0.0-test",
		GeneratedAt:   now.Add(-time.Hour),
		ExpiresAt:     now.Add(time.Hour),
		Assets: []release.LayeredAsset{
			{
				ID:           "windows-helper",
				Platform:     "windows/amd64",
				Kind:         "optional-helper",
				RelativePath: "optional-helper.exe",
				SHA256:       fmt.Sprintf("sha256:%x", helperDigest),
				SizeBytes:    int64(len(helper)),
			},
			{
				ID:           "windows-core",
				Platform:     "windows/amd64",
				Kind:         "core-runtime",
				RelativePath: "rdev-core.exe",
				SHA256:       fmt.Sprintf("sha256:%x", coreDigest),
				SizeBytes:    int64(len(core)),
			},
		},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return windowsEntryFixture{
		now:          now,
		root:         key.ID + ":" + base64.RawURLEncoding.EncodeToString(key.PublicKey),
		manifestURL:  "https://downloads.example.test/releases/layered.json",
		coreURL:      "https://downloads.example.test/releases/rdev-core.exe",
		helperURL:    "https://downloads.example.test/releases/optional-helper.exe",
		core:         core,
		manifestJSON: manifestJSON,
	}
}

func (fixture windowsEntryFixture) args(t *testing.T, cacheDir string) []string {
	t.Helper()
	return windowsEntryAttemptArgs(fixture, cacheDir, privateAttemptDirForTest(t), launcherPowerShell)
}

func (fixture windowsEntryFixture) baseArgs(cacheDir string) []string {
	return []string{
		"layered-run",
		"--manifest-url", fixture.manifestURL,
		"--root-public-key", fixture.root,
		"--expected-release-version", "v2.0.0-test",
		"--platform", "windows/amd64",
		"--cache-dir", cacheDir,
		"--mode", "temporary",
		"--", "serve", "--mode", "temporary", "--transport", "auto",
	}
}

type transportFixture struct {
	status  int
	content []byte
}

type transportFunc func(context.Context, assetdownload.TransportRequest) (assetdownload.TransportResponse, error)

func (fn transportFunc) Fetch(ctx context.Context, request assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
	return fn(ctx, request)
}

type zeroReader struct{}

func (zeroReader) Read(content []byte) (int, error) {
	for index := range content {
		content[index] = 0
	}
	return len(content), nil
}

type errorCloseBody struct {
	io.Reader
	Err error
}

func (body *errorCloseBody) Close() error {
	return body.Err
}

type recordingTransport struct {
	responses map[string]transportFixture
	requests  []assetdownload.TransportRequest
}

func (transport *recordingTransport) Fetch(_ context.Context, request assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
	transport.requests = append(append([]assetdownload.TransportRequest(nil), transport.requests...), request)
	fixture, ok := transport.responses[request.URL]
	if !ok {
		return assetdownload.TransportResponse{}, fmt.Errorf("unexpected request %q", request.URL)
	}
	content := fixture.content
	if request.Offset > 0 {
		if request.Offset > int64(len(content)) {
			return assetdownload.TransportResponse{}, fmt.Errorf("offset exceeds fixture")
		}
		content = content[request.Offset:]
	}
	status := fixture.status
	if request.Offset > 0 && status == 200 {
		status = 206
	}
	return assetdownload.TransportResponse{
		StatusCode:    status,
		ContentLength: int64(len(content)),
		Body:          io.NopCloser(bytes.NewReader(content)),
	}, nil
}

func (transport *recordingTransport) requestURLs() []string {
	urls := make([]string, len(transport.requests))
	for index, request := range transport.requests {
		urls[index] = request.URL
	}
	return urls
}

func successfulTestCommand(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsEntryHelperProcess$")
	cmd.Env = append(os.Environ(), "RDEV_WINDOWS_ENTRY_HELPER=1")
	return cmd
}

func replaceWindowsEntryFlag(t *testing.T, args []string, name, value string) []string {
	t.Helper()
	cloned := append([]string(nil), args...)
	for index := 0; index+1 < len(cloned); index++ {
		if cloned[index] == name {
			cloned[index+1] = value
			return cloned
		}
	}
	t.Fatalf("flag %s not found", name)
	return nil
}

func canonicalWindowsEntryTestTempDir(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func assertPrivateFileForWindowsEntryTest(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected private regular file at %s", path)
	}
}
