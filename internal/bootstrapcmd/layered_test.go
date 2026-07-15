package bootstrapcmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

func TestRunLayeredTemporaryUsesVerifiedDigestCache(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("verified Windows core runtime\n"))
	manifestHits := 0
	assetHits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest-start":
			http.Redirect(w, r, "/release/layered-assets.json", http.StatusFound)
		case "/release/layered-assets.json":
			manifestHits++
			_, _ = w.Write(fixture.manifestJSON(t))
		case "/release/assets/rdev-host-windows-amd64.exe":
			assetHits++
			_, _ = w.Write(fixture.payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cacheDir := filepath.Join(t.TempDir(), "layered-cache")
	opts := fixture.options(server.URL+"/manifest-start", cacheDir, server.Client())
	opts.Now = time.Time{}
	opts.Args = []string{"host", "serve", "--ticket-code", "must-not-be-reported"}
	first, executablePath, err := RunLayeredTemporary(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.SchemaVersion != LayeredRunReportSchemaVersion || first.AssetID != "rdev-host-windows-amd64" ||
		first.FromCache || first.Resumed || first.Bytes != int64(len(fixture.payload)) {
		t.Fatalf("unexpected cold layered report: %#v", first)
	}
	assertLayeredStages(t, first.Stages)
	assertFileContent(t, executablePath, fixture.payload)
	digest := strings.TrimPrefix(fixture.manifest.Assets[0].SHA256, "sha256:")
	cachePath := filepath.Join(cacheDir, "content", digest)
	assertFileContent(t, cachePath, fixture.payload)
	assertLayeredPermissions(t, cacheDir, executablePath, cachePath)
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{server.URL, cacheDir, executablePath, "must-not-be-reported", fixture.manifest.Signature} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("layered report leaked %q: %s", forbidden, encoded)
		}
	}

	if err := os.Remove(executablePath); err != nil {
		t.Fatal(err)
	}
	second, secondPath, err := RunLayeredTemporary(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !second.FromCache || second.Resumed || second.Bytes != int64(len(fixture.payload)) || secondPath != executablePath {
		t.Fatalf("unexpected warm layered report/path: %#v %q", second, secondPath)
	}
	if manifestHits != 2 || assetHits != 1 {
		t.Fatalf("warm cache should avoid runtime refetch, manifest=%d asset=%d", manifestHits, assetHits)
	}
	assertFileContent(t, secondPath, fixture.payload)
}

func TestRunLayeredTemporaryResumesInterruptedTransferWithRange(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("0123456789abcdef-verified-runtime"))
	cacheDir := filepath.Join(t.TempDir(), "layered-cache")
	digest := strings.TrimPrefix(fixture.manifest.Assets[0].SHA256, "sha256:")
	outputPath := filepath.Join(cacheDir, "runtime", digest, "rdev-host-windows-amd64.exe")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		t.Fatal(err)
	}
	const partialBytes = 9
	if err := os.WriteFile(outputPath+".part", fixture.payload[:partialBytes], 0o600); err != nil {
		t.Fatal(err)
	}
	assetHits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/layered-assets.json":
			_, _ = w.Write(fixture.manifestJSON(t))
		case "/assets/rdev-host-windows-amd64.exe":
			assetHits++
			if got := r.Header.Get("Range"); got != "bytes=9-" {
				t.Errorf("Range = %q, want bytes=9-", got)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 9-%d/%d", len(fixture.payload)-1, len(fixture.payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(fixture.payload[partialBytes:])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	report, gotPath, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json", cacheDir, server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Resumed || report.FromCache || assetHits != 1 || gotPath != outputPath {
		t.Fatalf("unexpected resumed report/path: %#v %q hits=%d", report, gotPath, assetHits)
	}
	assertFileContent(t, gotPath, fixture.payload)
}

func TestRunLayeredTemporaryRejectsBadSignatureBeforeAssetRequest(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("runtime must not download"))
	fixture.manifest.Signature = strings.Repeat("A", len(fixture.manifest.Signature))
	assetHits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/layered-assets.json" {
			_, _ = w.Write(fixture.manifestJSON(t))
			return
		}
		assetHits++
		_, _ = w.Write(fixture.payload)
	}))
	defer server.Close()

	report, executablePath, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json", t.TempDir(), server.Client()))
	if err == nil {
		t.Fatalf("expected invalid signature rejection, got report=%#v path=%q", report, executablePath)
	}
	if assetHits != 0 || executablePath != "" {
		t.Fatalf("bad signature reached asset/download preparation, hits=%d path=%q", assetHits, executablePath)
	}
}

func TestRunLayeredTemporaryRejectsUnexpectedReleaseVersionBeforeAssetRequest(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("runtime must not download for wrong release"))
	assetHits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/layered-assets.json" {
			_, _ = w.Write(fixture.manifestJSON(t))
			return
		}
		assetHits++
		_, _ = w.Write(fixture.payload)
	}))
	defer server.Close()

	opts := fixture.options(server.URL+"/layered-assets.json", t.TempDir(), server.Client())
	opts.ExpectedReleaseVersion = "v0.1.0"
	report, executablePath, err := RunLayeredTemporary(t.Context(), opts)
	if err == nil || !strings.Contains(err.Error(), "release version") {
		t.Fatalf("expected release version mismatch rejection, report=%#v path=%q err=%v", report, executablePath, err)
	}
	if assetHits != 0 || executablePath != "" {
		t.Fatalf("release version mismatch reached asset download, hits=%d path=%q", assetHits, executablePath)
	}
}

func TestRunLayeredTemporaryRejectsSignedSizeMismatch(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("runtime with signed size contract"))
	fixture.manifest.Assets[0].SizeBytes++
	manifest, err := release.SignLayeredAssetManifest(fixture.manifest, fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	fixture.manifest = manifest
	assetHits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/layered-assets.json" {
			_, _ = w.Write(fixture.manifestJSON(t))
			return
		}
		assetHits++
		_, _ = w.Write(fixture.payload)
	}))
	defer server.Close()
	report, executablePath, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json", t.TempDir(), server.Client()))
	if err == nil || !strings.Contains(err.Error(), "size") || executablePath != "" || assetHits != 1 {
		t.Fatalf("expected signed size mismatch rejection, report=%#v path=%q err=%v hits=%d", report, executablePath, err, assetHits)
	}
}

func TestRunLayeredTemporaryRejectsHTTPAndInvalidManifestResponses(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("runtime"))
	t.Run("http scheme before request", func(t *testing.T) {
		hits := 0
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
		defer server.Close()
		_, _, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json", t.TempDir(), server.Client()))
		if err == nil || hits != 0 {
			t.Fatalf("expected HTTP manifest rejection before request, err=%v hits=%d", err, hits)
		}
	})

	for _, tt := range []struct {
		name   string
		status int
		body   []byte
	}{
		{name: "non success status", status: http.StatusServiceUnavailable, body: []byte("unavailable")},
		{name: "invalid JSON", status: http.StatusOK, body: []byte(`{"schema_version":`)},
		{name: "trailing JSON", status: http.StatusOK, body: append(fixture.manifestJSON(t), []byte("\n{}")...)},
		{name: "oversized manifest", status: http.StatusOK, body: bytes.Repeat([]byte(" "), maxLayeredManifestBytes+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write(tt.body)
			}))
			defer server.Close()
			_, _, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json", t.TempDir(), server.Client()))
			if err == nil {
				t.Fatal("expected manifest response rejection")
			}
		})
	}
}

func TestRunLayeredTemporaryRejectsManifestQueryBeforeRequest(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("runtime"))
	hits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write(fixture.manifestJSON(t))
	}))
	defer server.Close()
	for _, suffix := range []string{"?token=must-not-leak", "?"} {
		_, _, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json"+suffix, t.TempDir(), server.Client()))
		if err == nil || hits != 0 {
			t.Fatalf("expected manifest query rejection before request, suffix=%q err=%v hits=%d", suffix, err, hits)
		}
	}
}

func TestRunLayeredTemporaryRejectsOversizedManagedFilesBeforeAssetRequest(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("signed runtime size"))
	assetHits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/layered-assets.json" {
			_, _ = w.Write(fixture.manifestJSON(t))
			return
		}
		assetHits++
		_, _ = w.Write(fixture.payload)
	}))
	defer server.Close()

	for _, fileName := range []string{"output", "content", "output.tmp", "content.tmp"} {
		t.Run(fileName, func(t *testing.T) {
			assetHits = 0
			cacheDir := t.TempDir()
			paths := fixture.cachePaths(cacheDir)
			managedPaths := map[string]string{
				"output":      paths.output,
				"content":     paths.content,
				"output.part": paths.output + ".part",
				"output.tmp":  paths.output + ".tmp",
				"content.tmp": paths.content + ".tmp",
			}
			path := managedPaths[fileName]
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, bytes.Repeat([]byte("x"), len(fixture.payload)+1), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json", cacheDir, server.Client()))
			if err == nil || assetHits != 0 {
				t.Fatalf("expected oversized %s rejection before asset request, err=%v hits=%d", fileName, err, assetHits)
			}
		})
	}

	t.Run("oversized output partial is discarded and downloaded again", func(t *testing.T) {
		assetHits = 0
		cacheDir := t.TempDir()
		paths := fixture.cachePaths(cacheDir)
		if err := os.MkdirAll(filepath.Dir(paths.output), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths.output+".part", bytes.Repeat([]byte("x"), len(fixture.payload)+1), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json", cacheDir, server.Client())); err != nil {
			t.Fatal(err)
		}
		if assetHits != 1 {
			t.Fatalf("expected one clean asset download after discarding oversized partial, got %d", assetHits)
		}
		if _, statErr := os.Stat(paths.output + ".part"); !os.IsNotExist(statErr) {
			t.Fatalf("oversized partial was not removed, stat err=%v", statErr)
		}
	})
}

func TestRunLayeredTemporaryAcceptsVerifiedOutputWithoutContentCache(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("verified output hit"))
	cacheDir := t.TempDir()
	paths := fixture.cachePaths(cacheDir)
	if err := os.MkdirAll(filepath.Dir(paths.output), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.output, fixture.payload, 0o600); err != nil {
		t.Fatal(err)
	}
	assetHits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/layered-assets.json" {
			_, _ = w.Write(fixture.manifestJSON(t))
			return
		}
		assetHits++
		_, _ = w.Write(fixture.payload)
	}))
	defer server.Close()
	report, executablePath, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/layered-assets.json", cacheDir, server.Client()))
	if err != nil || !report.FromCache || executablePath != paths.output || assetHits != 0 {
		t.Fatalf("expected verified output hit without content cache, report=%#v path=%q err=%v hits=%d", report, executablePath, err, assetHits)
	}
}

func TestSecureLayeredResultFilesRechecksSignedDigestAndSize(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("verified output"))
	paths := fixture.cachePaths(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.output), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		content []byte
		asset   release.LayeredAsset
	}{
		{name: "digest", content: []byte("tampered output"), asset: fixture.manifest.Assets[0]},
		{name: "size", content: fixture.payload, asset: func() release.LayeredAsset { asset := fixture.manifest.Assets[0]; asset.SizeBytes++; return asset }()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(paths.output, tt.content, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := secureLayeredResultFiles(paths, tt.asset); err == nil {
				t.Fatalf("expected final %s mismatch rejection", tt.name)
			}
		})
	}
}

func TestRunLayeredTemporaryRejectsCrossOriginRedirects(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("runtime"))
	t.Run("manifest", func(t *testing.T) {
		targetHits := 0
		target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			targetHits++
			_, _ = w.Write(fixture.manifestJSON(t))
		}))
		defer target.Close()
		origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/layered-assets.json", http.StatusFound)
		}))
		defer origin.Close()
		_, _, err := RunLayeredTemporary(t.Context(), fixture.options(origin.URL+"/layered-assets.json", t.TempDir(), origin.Client()))
		if err == nil || targetHits != 0 {
			t.Fatalf("expected cross-origin manifest redirect rejection, err=%v target_hits=%d", err, targetHits)
		}
	})

	t.Run("asset", func(t *testing.T) {
		targetHits := 0
		target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			targetHits++
			_, _ = w.Write(fixture.payload)
		}))
		defer target.Close()
		origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/layered-assets.json":
				_, _ = w.Write(fixture.manifestJSON(t))
			case "/assets/rdev-host-windows-amd64.exe":
				http.Redirect(w, r, target.URL+"/runtime.exe", http.StatusFound)
			default:
				http.NotFound(w, r)
			}
		}))
		defer origin.Close()
		_, _, err := RunLayeredTemporary(t.Context(), fixture.options(origin.URL+"/layered-assets.json", t.TempDir(), origin.Client()))
		if err == nil || targetHits != 0 {
			t.Fatalf("expected cross-origin asset redirect rejection, err=%v target_hits=%d", err, targetHits)
		}
	})
}

func TestRunLayeredTemporaryPreservesCallerRedirectPolicy(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("runtime"))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/layered-assets.json", http.StatusFound)
			return
		}
		_, _ = w.Write(fixture.manifestJSON(t))
	}))
	defer server.Close()
	sentinel := errors.New("caller redirect policy")
	policyCalls := 0
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		policyCalls++
		return sentinel
	}
	_, _, err := RunLayeredTemporary(t.Context(), fixture.options(server.URL+"/start", t.TempDir(), client))
	if !errors.Is(err, sentinel) || policyCalls != 1 {
		t.Fatalf("caller redirect policy was not preserved: err=%v calls=%d", err, policyCalls)
	}
}

func TestLayeredRunCLIExecutesVerifiedRuntimeInForeground(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("downloaded Windows runtime"))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/layered-assets.json":
			_, _ = w.Write(fixture.manifestJSON(t))
		case "/assets/rdev-host-windows-amd64.exe":
			_, _ = w.Write(fixture.payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var invokedPath string
	var invokedArgs []string
	cacheDir := filepath.Join(t.TempDir(), "cache")
	app := App{
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  strings.NewReader("inherited-input"),
		Client: server.Client(),
		CommandContext: func(ctx context.Context, path string, args ...string) *exec.Cmd {
			invokedPath = path
			invokedArgs = append([]string(nil), args...)
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLayeredRunCLIHelperProcess$")
			cmd.Env = append(os.Environ(), "RDEV_LAYERED_RUN_HELPER_PROCESS=1")
			return cmd
		},
	}
	err := app.Run(t.Context(), []string{
		"layered-run",
		"--manifest-url", server.URL + "/layered-assets.json",
		"--root-public-key", trustref.Encode(fixture.key.ID, fixture.key.PublicKey),
		"--expected-release-version", fixture.manifest.Version,
		"--platform", "windows/amd64",
		"--cache-dir", cacheDir,
		"--mode", "temporary",
		"--", "host", "serve", "--ticket-code", "ticket-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(invokedPath) != "rdev-host-windows-amd64.exe" || strings.Join(invokedArgs, "\x00") != "host\x00serve\x00--ticket-code\x00ticket-secret" {
		t.Fatalf("unexpected foreground invocation: %q %#v", invokedPath, invokedArgs)
	}
	if !strings.Contains(stdout.String(), `"schema_version":"rdev.layered-run-report.v1"`) ||
		!strings.Contains(stdout.String(), "helper-stdin=inherited-input") ||
		!strings.Contains(stderr.String(), "helper-stderr") {
		t.Fatalf("foreground stdio/report contract failed, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	for _, forbidden := range []string{server.URL, cacheDir, invokedPath, "ticket-secret"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("layered CLI report leaked %q: %s", forbidden, stdout.String())
		}
	}
}

func TestLayeredRunCLIRejectsInjectedPreExecRuntimeSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows denies the injected rename while the executable handle is locked")
	}
	fixture := newLayeredTestFixture(t, []byte("runtime verified before injected replacement"))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/layered-assets.json":
			_, _ = w.Write(fixture.manifestJSON(t))
		case "/assets/rdev-host-windows-amd64.exe":
			_, _ = w.Write(fixture.payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	markerPath := filepath.Join(t.TempDir(), "runtime-executed")
	commandContextCalls := 0
	app := App{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Stdin:  strings.NewReader(""),
		Client: server.Client(),
		CommandContext: func(ctx context.Context, path string, args ...string) *exec.Cmd {
			commandContextCalls++
			replacementPath := path + ".injected-swap"
			replacement := bytes.Repeat([]byte("x"), len(fixture.payload))
			if err := os.WriteFile(replacementPath, replacement, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacementPath, path); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLayeredRunCLIHelperProcess$")
			cmd.Env = append(os.Environ(),
				"RDEV_LAYERED_RUN_HELPER_PROCESS=1",
				"RDEV_LAYERED_RUN_HELPER_MARKER="+markerPath,
			)
			return cmd
		},
	}
	err := app.Run(t.Context(), []string{
		"layered-run",
		"--manifest-url", server.URL + "/layered-assets.json",
		"--root-public-key", trustref.Encode(fixture.key.ID, fixture.key.PublicKey),
		"--expected-release-version", fixture.manifest.Version,
		"--platform", "windows/amd64",
		"--cache-dir", filepath.Join(t.TempDir(), "cache"),
		"--mode", "temporary",
		"--", "host", "serve",
	})
	if err == nil || !strings.Contains(err.Error(), "changed before execution") {
		t.Fatalf("pre-exec runtime swap error = %v, want fail-closed rejection", err)
	}
	if commandContextCalls != 1 {
		t.Fatalf("CommandContext calls = %d, want 1 injected swap attempt", commandContextCalls)
	}
	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("swapped runtime command executed, marker stat error = %v", statErr)
	}
}

func TestLayeredRunCLIRequiresTemporaryWindowsAMD64Contract(t *testing.T) {
	fixture := newLayeredTestFixture(t, []byte("runtime"))
	root := trustref.Encode(fixture.key.ID, fixture.key.PublicKey)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing mode", args: []string{"--manifest-url", "https://gateway.test/layered-assets.json", "--root-public-key", root, "--platform", "windows/amd64", "--cache-dir", t.TempDir()}, want: "mode temporary"},
		{name: "managed mode", args: []string{"--manifest-url", "https://gateway.test/layered-assets.json", "--root-public-key", root, "--expected-release-version", fixture.manifest.Version, "--platform", "windows/amd64", "--cache-dir", t.TempDir(), "--mode", "managed"}, want: "mode temporary"},
		{name: "missing expected release version", args: []string{"--manifest-url", "https://gateway.test/layered-assets.json", "--root-public-key", root, "--platform", "windows/amd64", "--cache-dir", t.TempDir(), "--mode", "temporary"}, want: "expected release version"},
		{name: "wrong platform", args: []string{"--manifest-url", "https://gateway.test/layered-assets.json", "--root-public-key", root, "--expected-release-version", fixture.manifest.Version, "--platform", "windows-amd64", "--cache-dir", t.TempDir(), "--mode", "temporary"}, want: "platform windows/amd64"},
		{name: "invalid root", args: []string{"--manifest-url", "https://gateway.test/layered-assets.json", "--root-public-key", "invalid", "--expected-release-version", fixture.manifest.Version, "--platform", "windows/amd64", "--cache-dir", t.TempDir(), "--mode", "temporary"}, want: "root public key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := App{Stdout: io.Discard, Stderr: io.Discard, Client: testHTTPClient(func(r *http.Request) (*http.Response, error) {
				t.Fatalf("invalid layered CLI contract made request to %s", r.URL)
				return nil, nil
			})}
			err := app.Run(t.Context(), append([]string{"layered-run"}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLayeredRunCLIHelperProcess(t *testing.T) {
	if os.Getenv("RDEV_LAYERED_RUN_HELPER_PROCESS") != "1" {
		return
	}
	if markerPath := os.Getenv("RDEV_LAYERED_RUN_HELPER_MARKER"); markerPath != "" {
		if err := os.WriteFile(markerPath, []byte("executed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "helper-stdin=%s", content)
	_, _ = fmt.Fprint(os.Stderr, "helper-stderr")
}

type layeredTestFixture struct {
	payload  []byte
	manifest release.LayeredAssetManifest
	key      signing.Key
	now      time.Time
}

func newLayeredTestFixture(t *testing.T, payload []byte) layeredTestFixture {
	t.Helper()
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	sum := sha256.Sum256(payload)
	manifest, err := release.SignLayeredAssetManifest(release.LayeredAssetManifest{
		SchemaVersion: release.LayeredAssetManifestSchemaVersion,
		Version:       "v0.2.0",
		GeneratedAt:   now,
		ExpiresAt:     now.Add(24 * time.Hour),
		Assets: []release.LayeredAsset{{
			ID:           "rdev-host-windows-amd64",
			Platform:     "windows/amd64",
			Kind:         "core-runtime",
			RelativePath: "assets/rdev-host-windows-amd64.exe",
			SHA256:       "sha256:" + hex.EncodeToString(sum[:]),
			SizeBytes:    int64(len(payload)),
		}},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	return layeredTestFixture{payload: append([]byte(nil), payload...), manifest: manifest, key: key, now: now}
}

func (f layeredTestFixture) manifestJSON(t *testing.T) []byte {
	t.Helper()
	content, err := json.Marshal(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func (f layeredTestFixture) options(manifestURL, cacheDir string, client *http.Client) LayeredRunOptions {
	return LayeredRunOptions{
		ManifestURL:            manifestURL,
		Root:                   model.NewTrustBundle(f.key.ID, f.key.PublicKey),
		ExpectedReleaseVersion: f.manifest.Version,
		Platform:               "windows/amd64",
		CacheDir:               cacheDir,
		Mode:                   "temporary",
		Client:                 client,
		Now:                    f.now,
	}
}

func (f layeredTestFixture) cachePaths(cacheDir string) layeredCachePaths {
	digest := strings.TrimPrefix(f.manifest.Assets[0].SHA256, "sha256:")
	return layeredCachePaths{
		output:  filepath.Join(cacheDir, "runtime", digest, filepath.Base(f.manifest.Assets[0].RelativePath)),
		content: filepath.Join(cacheDir, "content", digest),
	}
}

func assertLayeredStages(t *testing.T, stages []LayeredRunStage) {
	t.Helper()
	want := []string{"manifest-fetch", "signature-verification", "runtime-download", "runtime-launch-preparation"}
	if len(stages) != len(want) {
		t.Fatalf("stages = %#v, want %v", stages, want)
	}
	for index, stage := range stages {
		if stage.Name != want[index] || stage.DurationMS < 0 {
			t.Fatalf("stage[%d] = %#v, want name %q with nonnegative duration", index, stage, want[index])
		}
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("file %s content = %q, want %q", filepath.Base(path), got, want)
	}
}

func assertLayeredPermissions(t *testing.T, cacheDir, executablePath, cachePath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	for _, dir := range []string{cacheDir, filepath.Join(cacheDir, "runtime"), filepath.Dir(executablePath), filepath.Join(cacheDir, "content")} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %s mode = %o, want 0700", filepath.Base(dir), info.Mode().Perm())
		}
	}
	for _, path := range []string{executablePath, cachePath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("file %s mode = %o, want 0600", filepath.Base(path), info.Mode().Perm())
		}
	}
}
