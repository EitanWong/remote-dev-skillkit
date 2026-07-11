package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestCanonicalProviderURLTunn3l(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		want      string
	}{
		{name: "valid", candidate: "https://cedar-orion.tunn3l.sh", want: "https://cedar-orion.tunn3l.sh"},
		{name: "canonical case", candidate: "HTTPS://CEDAR-ORION.TUNN3L.SH", want: "https://cedar-orion.tunn3l.sh"},
		{name: "apex", candidate: "https://tunn3l.sh"},
		{name: "explicit root slash", candidate: "https://cedar-orion.tunn3l.sh/"},
		{name: "explicit default port", candidate: "https://cedar-orion.tunn3l.sh:443"},
		{name: "empty port", candidate: "https://cedar-orion.tunn3l.sh:"},
		{name: "path", candidate: "https://cedar-orion.tunn3l.sh/private"},
		{name: "query", candidate: "https://cedar-orion.tunn3l.sh/?token=secret"},
		{name: "empty query", candidate: "https://cedar-orion.tunn3l.sh?"},
		{name: "fragment", candidate: "https://cedar-orion.tunn3l.sh/#secret"},
		{name: "empty fragment", candidate: "https://cedar-orion.tunn3l.sh#"},
		{name: "userinfo", candidate: "https://user@cedar-orion.tunn3l.sh"},
		{name: "password", candidate: "https://user:secret@cedar-orion.tunn3l.sh"},
		{name: "wildcard", candidate: "https://*.tunn3l.sh"},
		{name: "underscore", candidate: "https://cedar_orion.tunn3l.sh"},
		{name: "leading hyphen", candidate: "https://-cedar.tunn3l.sh"},
		{name: "trailing hyphen", candidate: "https://cedar-.tunn3l.sh"},
		{name: "empty label", candidate: "https://cedar..tunn3l.sh"},
		{name: "multi label", candidate: "https://a.b.tunn3l.sh"},
		{name: "percent encoded label", candidate: "https://cedar%2dorion.tunn3l.sh"},
		{name: "unicode label", candidate: "https://雪.tunn3l.sh"},
		{name: "punycode label", candidate: "https://xn--cedar-orion.tunn3l.sh"},
		{name: "suffix attack", candidate: "https://cedar.tunn3l.sh.attacker.example"},
		{name: "trailing dot", candidate: "https://cedar.tunn3l.sh."},
		{name: "plaintext", candidate: "http://cedar.tunn3l.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := canonicalProviderURL(tunnel.ProviderTunn3l, tt.candidate)
			if got != tt.want || ok != (tt.want != "") {
				t.Fatalf("canonicalProviderURL(%q) = %q, %v; want %q, %v", tt.candidate, got, ok, tt.want, tt.want != "")
			}
		})
	}

	line := `{"url":"https://CEDAR-ORION.TUNN3L.SH","subdomain":"cedar-orion"}`
	if got := tunn3lURLFromJSONLine(line); got != "https://cedar-orion.tunn3l.sh" {
		t.Fatalf("JSON candidate = %q", got)
	}
	if got := providerURLFromLine(tunnel.ProviderTunn3l, "ready https://cedar-orion.tunn3l.sh"); got != "" {
		t.Fatalf("tunn3l text parser accepted %q", got)
	}
}

func TestTunn3lJSONCandidateParserRejectsMalformedOrAmbiguousOutput(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "official shape", line: `{"url":"https://cedar-orion.tunn3l.sh","subdomain":"cedar-orion"}`, want: "https://cedar-orion.tunn3l.sh"},
		{name: "official fields reversed", line: ` { "subdomain":"cedar-orion", "url":"https://CEDAR-ORION.TUNN3L.SH" } `, want: "https://cedar-orion.tunn3l.sh"},
		{name: "malformed", line: `{"url":`},
		{name: "not object", line: `[{"url":"https://cedar-orion.tunn3l.sh","subdomain":"cedar-orion"}]`},
		{name: "missing url", line: `{"subdomain":"cedar-orion"}`},
		{name: "missing subdomain", line: `{"url":"https://cedar-orion.tunn3l.sh"}`},
		{name: "unknown field", line: `{"url":"https://cedar-orion.tunn3l.sh","subdomain":"cedar-orion","token":"secret"}`},
		{name: "duplicate url", line: `{"url":"https://cedar-orion.tunn3l.sh","url":"https://other.tunn3l.sh","subdomain":"cedar-orion"}`},
		{name: "duplicate subdomain", line: `{"url":"https://cedar-orion.tunn3l.sh","subdomain":"cedar-orion","subdomain":"other"}`},
		{name: "non-string", line: `{"url":"https://cedar-orion.tunn3l.sh","subdomain":123}`},
		{name: "trailing object", line: `{"url":"https://cedar-orion.tunn3l.sh","subdomain":"cedar-orion"}{}`},
		{name: "trailing text", line: `{"url":"https://cedar-orion.tunn3l.sh","subdomain":"cedar-orion"} secret`},
		{name: "subdomain mismatch", line: `{"url":"https://cedar-orion.tunn3l.sh","subdomain":"other"}`},
		{name: "uppercase subdomain", line: `{"url":"https://cedar-orion.tunn3l.sh","subdomain":"CEDAR-ORION"}`},
		{name: "punycode subdomain", line: `{"url":"https://xn--cedar-orion.tunn3l.sh","subdomain":"xn--cedar-orion"}`},
		{name: "nested subdomain", line: `{"url":"https://a.b.tunn3l.sh","subdomain":"a.b"}`},
		{name: "oversized subdomain", line: `{"url":"https://` + strings.Repeat("a", 64) + `.tunn3l.sh","subdomain":"` + strings.Repeat("a", 64) + `"}`},
		{name: "suffix attack", line: `{"url":"https://cedar.tunn3l.sh.attacker.example","subdomain":"cedar"}`},
		{name: "path", line: `{"url":"https://cedar-orion.tunn3l.sh/private","subdomain":"cedar-orion"}`},
		{name: "explicit root slash", line: `{"url":"https://cedar-orion.tunn3l.sh/","subdomain":"cedar-orion"}`},
		{name: "empty fragment", line: `{"url":"https://cedar-orion.tunn3l.sh#","subdomain":"cedar-orion"}`},
		{name: "percent encoded", line: `{"url":"https://cedar%2dorion.tunn3l.sh","subdomain":"cedar-orion"}`},
		{name: "unicode", line: `{"url":"https://雪.tunn3l.sh","subdomain":"雪"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tunn3lURLFromJSONLine(tt.line); got != tt.want {
				t.Fatalf("tunn3lURLFromJSONLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTunn3lJSONOutputSinkAcceptsOnlyFirstBoundedStrictLine(t *testing.T) {
	valid := `{"url":"https://cedar-orion.tunn3l.sh","subdomain":"cedar-orion"}`
	tests := []struct {
		name   string
		chunks [][]byte
		want   []string
	}{
		{name: "split official line", chunks: [][]byte{[]byte(`{"url":"https://cedar-`), []byte(`orion.tunn3l.sh","subdomain":"cedar-orion"}` + "\n")}, want: []string{"https://cedar-orion.tunn3l.sh"}},
		{name: "plain text URL", chunks: [][]byte{[]byte("ready https://cedar-orion.tunn3l.sh\n")}},
		{name: "malformed first line then valid", chunks: [][]byte{[]byte(`{"url":` + "\n" + valid + "\n")}},
		{name: "overlong first line then valid", chunks: [][]byte{bytes.Repeat([]byte("x"), (4<<10)+1), []byte("\n" + valid + "\n")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			sink := &tunn3lJSONOutputSink{onCandidate: func(candidate string) { got = append(got, candidate) }}
			for _, chunk := range tt.chunks {
				if written, err := sink.Write(chunk); err != nil || written != len(chunk) {
					t.Fatalf("Write() = %d, %v", written, err)
				}
			}
			sink.Finalize()
			if !slices.Equal(got, tt.want) {
				t.Fatalf("candidates = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestTunn3lCandidateIsAcceptedOnlyFromStrictStdoutJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is POSIX-only")
	}
	root := protectedTunn3lTestRoot(t)
	executable := filepath.Join(root, "stderr-only-helper")
	script := "#!/bin/sh\nprintf '%s\\n' 'token=secret path=/private/config https://cedar-orion.tunn3l.sh' >&2\nprintf '%s\\n' '{\"url\":\"https://cedar-orion.tunn3l.sh\",\"subdomain\":\"cedar-orion\"}' >&2\nexec sleep 3600\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var log synchronizedBuffer
	_, err := startTunnelCommandWithOptions(context.Background(), &log, tunnel.ProviderTunn3l,
		[]string{executable}, 50*time.Millisecond, providerProcessOptions{WorkingDirectory: root, Env: os.Environ()})
	if err == nil || !strings.Contains(err.Error(), "startup timed out") {
		t.Fatalf("stderr-only candidate error = %v", err)
	}
	for _, forbidden := range []string{"secret", "/private/config", "https://cedar-orion.tunn3l.sh"} {
		if strings.Contains(log.String(), forbidden) {
			t.Fatalf("stderr leaked %q: %q", forbidden, log.String())
		}
	}
}

func TestTunn3lProviderUsesVerifiedArgvSanitizedEnvironmentAndForegroundLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pinned tunn3l release has no Windows Agent asset")
	}
	root := protectedTunn3lTestRoot(t)
	toolRoot := filepath.Join(root, "tools", "tunn3l", tunn3lManagedVersion)
	if err := os.MkdirAll(toolRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := writeTunn3lTestExecutable(t, toolRoot)
	legacyHome := filepath.Join(root, "provider-state", "tunn3l", "home")
	if err := os.MkdirAll(filepath.Join(legacyHome, ".tunn3l"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyHome, ".tunn3l", "config.json"), []byte(`{"api_key":"old-secret","subdomain":"reserved","password":"old-password"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	capturePath := filepath.Join(root, "capture.txt")
	t.Setenv("RDEV_TEST_TUNN3L_CAPTURE", capturePath)
	t.Setenv("TUNN3L_TOKEN", "secret-token")
	t.Setenv("TUNN3L_SUBDOMAIN", "reserved-name")
	t.Setenv("TUNN3L_PASSWORD", "old-password")
	t.Setenv("TUNN3L_FUTURE_OVERRIDE", "unsafe")
	t.Setenv("TUNN3L_RELAY", "wss://attacker.example/ws")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "old-xdg"))
	t.Setenv("BUN_OPTIONS", "--preload="+filepath.Join(root, "attacker-bun.js"))
	t.Setenv("BUN_FUTURE_OVERRIDE", "unsafe")
	t.Setenv("NODE_OPTIONS", "--require=attacker.js")
	t.Setenv("NODE_PATH", filepath.Join(root, "attacker-modules"))
	t.Setenv("NODE_FUTURE_OVERRIDE", "unsafe")
	t.Setenv("NODE_TLS_REJECT_UNAUTHORIZED", "0")
	t.Setenv("NODE_EXTRA_CA_CERTS", filepath.Join(root, "attacker-ca.pem"))
	t.Setenv("SSL_CERT_FILE", filepath.Join(root, "attacker-cert.pem"))
	t.Setenv("SSL_CERT_DIR", filepath.Join(root, "attacker-certs"))
	t.Setenv("LD_PRELOAD", filepath.Join(root, "attacker.so"))
	t.Setenv("LD_LIBRARY_PATH", filepath.Join(root, "attacker-libs"))
	t.Setenv("LD_AUDIT", filepath.Join(root, "attacker-audit.so"))
	t.Setenv("LD_FUTURE_OVERRIDE", "unsafe")
	t.Setenv("DYLD_INSERT_LIBRARIES", filepath.Join(root, "attacker.dylib"))
	t.Setenv("GODEBUG", "x509sha1=1")
	t.Setenv("SSLKEYLOGFILE", filepath.Join(root, "tls.keys"))
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:18080")

	var installCalls atomic.Int32
	install := func(ctx context.Context, gotRoot string, asset managedToolAsset) (string, error) {
		installCalls.Add(1)
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if gotRoot != toolRoot {
			return "", fmt.Errorf("installer root = %q, want %q", gotRoot, toolRoot)
		}
		wantAsset, ok := tunn3lManagedAsset(runtime.GOOS, runtime.GOARCH)
		if !ok || asset != wantAsset {
			return "", fmt.Errorf("asset = %#v, want %#v", asset, wantAsset)
		}
		return executable, nil
	}
	provider := newTunn3lProviderWithRuntime(io.Discard, runtime.GOOS, runtime.GOARCH, install)
	metadata := provider.Metadata()
	if metadata.ID != tunnel.ProviderTunn3l || metadata.DisplayName != "tunn3l.sh" ||
		metadata.Executable != "rdev-managed:tunn3l-v0.5.1" || !metadata.Anonymous || !metadata.DefaultAutomatic ||
		metadata.AutomaticPriority != 20 || metadata.RequiresSSHPin ||
		!slices.Equal(metadata.Protocols, []string{"https", "wss"}) {
		t.Fatalf("metadata = %#v", metadata)
	}
	if metadata.FailureDomains.AuthoritativeDNS == "" || metadata.FailureDomains.EdgeNetwork == "" ||
		metadata.FailureDomains.OriginNetwork == "" || metadata.FailureDomains.ControlPlane == "" ||
		metadata.FailureDomains.CertificateDependency == "" {
		t.Fatalf("failure domains = %#v", metadata.FailureDomains)
	}
	wantDomains := tunnel.FailureDomains{
		AuthoritativeDNS:      "tunn3l.sh-dns",
		EdgeNetwork:           "tunn3l.sh-public-edge",
		OriginNetwork:         "rdev-local-gateway",
		ControlPlane:          "tunn3l.sh-wss-relay",
		CertificateDependency: "public-web-pki",
	}
	if metadata.FailureDomains != wantDomains {
		t.Fatalf("failure domains = %#v, want %#v", metadata.FailureDomains, wantDomains)
	}

	handle, err := provider.Start(context.Background(), tunnel.StartRequest{LocalPort: "8787", ProviderRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if installCalls.Load() != 1 {
		t.Fatalf("installer calls = %d", installCalls.Load())
	}
	candidate := handle.Candidate()
	if candidate.ProviderID != tunnel.ProviderTunn3l || candidate.URL != "https://cedar-orion.tunn3l.sh" || candidate.FailureDomains != metadata.FailureDomains {
		t.Fatalf("candidate = %#v", candidate)
	}

	captured := waitForTunn3lCapture(t, capturePath)
	wantLines := []string{
		"argc=3",
		"arg1=http",
		"arg2=8787",
		"arg3=--json",
		"TUNN3L_RELAY=" + tunn3lRelayURL,
		"TUNN3L_TOKEN=unset",
		"TUNN3L_SUBDOMAIN=unset",
		"TUNN3L_PASSWORD=unset",
		"TUNN3L_FUTURE_OVERRIDE=unset",
		"BUN_OPTIONS=unset",
		"BUN_FUTURE_OVERRIDE=unset",
		"NODE_OPTIONS=unset",
		"NODE_PATH=unset",
		"NODE_FUTURE_OVERRIDE=unset",
		"NODE_TLS_REJECT_UNAUTHORIZED=unset",
		"NODE_EXTRA_CA_CERTS=unset",
		"SSL_CERT_FILE=unset",
		"SSL_CERT_DIR=unset",
		"LD_PRELOAD=unset",
		"LD_LIBRARY_PATH=unset",
		"LD_AUDIT=unset",
		"LD_FUTURE_OVERRIDE=unset",
		"DYLD_INSERT_LIBRARIES=unset",
		"GODEBUG=unset",
		"SSLKEYLOGFILE=unset",
		"HTTPS_PROXY=http://127.0.0.1:18080",
		"config_present=no",
		"home_empty=yes",
	}
	for _, line := range wantLines {
		if !slices.Contains(captured, line) {
			t.Fatalf("capture missing %q: %#v", line, captured)
		}
	}
	values := tunn3lCaptureValues(captured)
	home := values["HOME"]
	if home == legacyHome || !strings.HasPrefix(home, legacyHome+string(filepath.Separator)+"session-") {
		t.Fatalf("session HOME = %q, legacy home = %q", home, legacyHome)
	}
	if values["USERPROFILE"] != home || values["XDG_CONFIG_HOME"] != home || values["pwd"] != home {
		t.Fatalf("session paths = %#v", values)
	}
	if mode := mustStat(t, home).Mode().Perm(); mode != 0o700 {
		t.Fatalf("home mode = %#o", mode)
	}
	select {
	case waitErr := <-handle.Wait():
		t.Fatalf("provider exited after assigning candidate: %v", waitErr)
	case <-time.After(50 * time.Millisecond):
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), providerProcessCleanupTimeout)
	defer cancel()
	if stopErr := handle.Stop(stopCtx); errors.Is(stopErr, context.DeadlineExceeded) {
		t.Fatalf("Stop did not reap the provider: %v", stopErr)
	}
	select {
	case <-handle.Wait():
	case <-time.After(time.Second):
		t.Fatal("Wait did not close after Stop")
	}
}

func TestTunn3lProviderEnvironmentPreservesProxyAndDetachesSanitizedValues(t *testing.T) {
	inherited := []string{
		"PATH=/usr/bin", "https_proxy=http://proxy.example:8080", "NO_PROXY=127.0.0.1",
		"home=/old", "UserProfile=/old-user", "xdg_config_home=/old-xdg",
		"tunn3l_token=secret", "TUNN3L_RELAY=wss://attacker.example", "TuNn3L_Future=value",
		"bUn_OpTiOnS=--preload=attacker", "BuN_Future=unsafe",
		"node_options=--require=attacker", "NODE_PATH=/attacker", "NoDe_Future=unsafe", "node_tls_reject_unauthorized=0", "node_extra_ca_certs=/attacker.pem",
		"ssl_cert_file=/attacker.pem", "SSL_CERT_DIR=/attacker", "ld_preload=/attacker.so", "LD_LIBRARY_PATH=/attacker",
		"Ld_AuDiT=/attacker-audit.so", "lD_Future=unsafe", "dyld_insert_libraries=/attacker.dylib", "DYLD_FUTURE_OVERRIDE=unsafe",
		"GoDeBuG=x509sha1=1", "sslkeylogfile=/private/tls.keys",
	}
	original := append([]string(nil), inherited...)
	home := "/session/home"
	got := tunn3lProviderEnvironment(home, inherited)
	if !slices.Equal(inherited, original) {
		t.Fatalf("inherited environment mutated: %#v", inherited)
	}
	values := tunn3lCaptureValues(got)
	for name, want := range map[string]string{
		"PATH": "/usr/bin", "https_proxy": "http://proxy.example:8080", "NO_PROXY": "127.0.0.1",
		"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": home, "TUNN3L_RELAY": tunn3lRelayURL,
	} {
		if values[name] != want {
			t.Fatalf("environment %s = %q, want %q: %#v", name, values[name], want, got)
		}
	}
	for _, item := range got {
		name, _, ok := strings.Cut(item, "=")
		if !ok {
			t.Fatalf("malformed environment item %q", item)
		}
		canonical := asciiUpperString(name)
		if strings.HasPrefix(canonical, "BUN_") || strings.HasPrefix(canonical, "NODE_") ||
			strings.HasPrefix(canonical, "LD_") || strings.HasPrefix(canonical, "DYLD_") ||
			(strings.HasPrefix(canonical, "TUNN3L_") && canonical != "TUNN3L_RELAY") ||
			slices.Contains([]string{"SSL_CERT_FILE", "SSL_CERT_DIR", "SSLKEYLOGFILE", "GODEBUG"}, canonical) {
			t.Fatalf("unsafe environment survived: %q", item)
		}
	}
}

func TestTunn3lSessionHomeIsUniqueEmptyAndDoesNotReuseConfig(t *testing.T) {
	root := protectedTunn3lTestRoot(t)
	legacyHome := filepath.Join(root, "provider-state", "tunn3l", "home")
	if err := os.MkdirAll(filepath.Join(legacyHome, ".tunn3l"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyConfig := filepath.Join(legacyHome, ".tunn3l", "config.json")
	if err := os.WriteFile(legacyConfig, []byte(`{"api_key":"old-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := newTunn3lSessionHome(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newTunn3lSessionHome(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == legacyHome || second == legacyHome {
		t.Fatalf("session homes are not unique: first=%q second=%q legacy=%q", first, second, legacyHome)
	}
	for _, home := range []string{first, second} {
		entries, readErr := os.ReadDir(home)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("session home %q entries=%#v err=%v", home, entries, readErr)
		}
		if mode := mustStat(t, home).Mode().Perm(); runtime.GOOS != "windows" && mode != 0o700 {
			t.Fatalf("session home %q mode=%#o", home, mode)
		}
	}
	if _, err := os.Stat(legacyConfig); err != nil {
		t.Fatalf("legacy config unexpectedly changed: %v", err)
	}
}

func TestTunn3lStartupDeadlineDoesNotOwnHealthyProcessLifetime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pinned tunn3l release has no Windows Agent asset")
	}
	root := protectedTunn3lTestRoot(t)
	toolRoot := filepath.Join(root, "tools", "tunn3l", tunn3lManagedVersion)
	if err := os.MkdirAll(toolRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := writeTunn3lTestExecutable(t, toolRoot)
	capturePath := filepath.Join(root, "lifetime-capture.txt")
	t.Setenv("RDEV_TEST_TUNN3L_CAPTURE", capturePath)
	install := func(context.Context, string, managedToolAsset) (string, error) { return executable, nil }
	startupBudget := 500 * time.Millisecond
	startedAt := time.Now()
	started, err := startTunn3lTunnelWithTimeout(context.Background(), io.Discard, tunnel.StartRequest{
		LocalPort: "8787", ProviderRoot: root,
	}, runtime.GOOS, runtime.GOARCH, install, startupBudget)
	if err != nil {
		t.Fatal(err)
	}
	defer stopTunnelTestProcess(t, started)
	waitForTunn3lCapture(t, capturePath)
	if delay := time.Until(startedAt.Add(startupBudget + 100*time.Millisecond)); delay > 0 {
		time.Sleep(delay)
	}
	select {
	case waitErr := <-started.lifecycle.wait:
		t.Fatalf("startup deadline canceled healthy provider: %v", waitErr)
	default:
	}
}

func TestTunn3lProviderRejectsInvalidRequestAndUnsupportedPlatformBeforeInstall(t *testing.T) {
	root := protectedTunn3lTestRoot(t)
	var installCalls atomic.Int32
	install := func(context.Context, string, managedToolAsset) (string, error) {
		installCalls.Add(1)
		return "", errors.New("install should not run")
	}
	provider := newTunn3lProviderWithRuntime(io.Discard, runtime.GOOS, runtime.GOARCH, install)
	for _, request := range []tunnel.StartRequest{
		{ProviderRoot: root},
		{LocalPort: "0", ProviderRoot: root},
		{LocalPort: "65536", ProviderRoot: root},
		{LocalPort: "8787"},
		{LocalPort: "8787", ProviderRoot: "relative-provider-root"},
		{LocalPort: "8787", ProviderRoot: root + string(filepath.Separator) + ".." + string(filepath.Separator) + "escape"},
	} {
		if _, err := provider.Start(context.Background(), request); err == nil {
			t.Fatalf("invalid request accepted: %#v", request)
		}
	}
	unsupportedRoot := filepath.Join(root, "unsupported-provider-root")
	unsupported := newTunn3lProviderWithRuntime(io.Discard, "windows", "amd64", install)
	if _, err := unsupported.Start(context.Background(), tunnel.StartRequest{LocalPort: "8787", ProviderRoot: unsupportedRoot}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported platform error = %v", err)
	}
	if _, err := os.Lstat(unsupportedRoot); !os.IsNotExist(err) {
		t.Fatalf("unsupported platform created provider root: %v", err)
	}
	if installCalls.Load() != 0 {
		t.Fatalf("invalid requests called installer %d times", installCalls.Load())
	}
}

func TestTunn3lProviderRejectsUnverifiedInstallerResultWithoutLeakingPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pinned tunn3l release has no Windows Agent asset")
	}
	root := protectedTunn3lTestRoot(t)
	toolRoot := filepath.Join(root, "tools", "tunn3l", tunn3lManagedVersion)
	if err := os.MkdirAll(toolRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	valid := writeTunn3lTestExecutable(t, toolRoot)
	if got, err := validateTunn3lExecutablePath(valid, toolRoot); err != nil || got != valid {
		t.Fatalf("valid executable = %q, %v", got, err)
	}
	content, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	badName := filepath.Join(toolRoot, "tunn3l-"+strings.Repeat("0", sha256.Size*2))
	if err := os.WriteFile(badName, content, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := validateTunn3lExecutablePath(badName, toolRoot); err == nil {
		t.Fatal("digest-mismatched executable accepted")
	}
	outsideRoot := filepath.Join(root, "outside")
	if err := os.Mkdir(outsideRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := writeTunn3lTestExecutable(t, outsideRoot)
	install := func(context.Context, string, managedToolAsset) (string, error) { return outside, nil }
	provider := newTunn3lProviderWithRuntime(io.Discard, runtime.GOOS, runtime.GOARCH, install)
	_, startErr := provider.Start(context.Background(), tunnel.StartRequest{LocalPort: "8787", ProviderRoot: root})
	if startErr == nil || !strings.Contains(startErr.Error(), "installation failed") {
		t.Fatalf("outside installer result error = %v", startErr)
	}
	for _, secret := range []string{root, outside, filepath.Base(outside)} {
		if strings.Contains(startErr.Error(), secret) {
			t.Fatalf("installer error leaked %q: %v", secret, startErr)
		}
	}
}

func protectedTunn3lTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeTunn3lTestExecutable(t *testing.T, root string) string {
	t.Helper()
	script := `#!/bin/sh
{
  printf 'argc=%s\n' "$#"
  printf 'arg1=%s\n' "${1-unset}"
  printf 'arg2=%s\n' "${2-unset}"
  printf 'arg3=%s\n' "${3-unset}"
  printf 'pwd=%s\n' "$PWD"
  printf 'HOME=%s\n' "${HOME-unset}"
  printf 'USERPROFILE=%s\n' "${USERPROFILE-unset}"
  printf 'XDG_CONFIG_HOME=%s\n' "${XDG_CONFIG_HOME-unset}"
  printf 'TUNN3L_RELAY=%s\n' "${TUNN3L_RELAY-unset}"
  printf 'TUNN3L_TOKEN=%s\n' "${TUNN3L_TOKEN-unset}"
  printf 'TUNN3L_SUBDOMAIN=%s\n' "${TUNN3L_SUBDOMAIN-unset}"
  printf 'TUNN3L_PASSWORD=%s\n' "${TUNN3L_PASSWORD-unset}"
  printf 'TUNN3L_FUTURE_OVERRIDE=%s\n' "${TUNN3L_FUTURE_OVERRIDE-unset}"
  printf 'BUN_OPTIONS=%s\n' "${BUN_OPTIONS-unset}"
  printf 'BUN_FUTURE_OVERRIDE=%s\n' "${BUN_FUTURE_OVERRIDE-unset}"
  printf 'NODE_OPTIONS=%s\n' "${NODE_OPTIONS-unset}"
  printf 'NODE_PATH=%s\n' "${NODE_PATH-unset}"
  printf 'NODE_FUTURE_OVERRIDE=%s\n' "${NODE_FUTURE_OVERRIDE-unset}"
  printf 'NODE_TLS_REJECT_UNAUTHORIZED=%s\n' "${NODE_TLS_REJECT_UNAUTHORIZED-unset}"
  printf 'NODE_EXTRA_CA_CERTS=%s\n' "${NODE_EXTRA_CA_CERTS-unset}"
  printf 'SSL_CERT_FILE=%s\n' "${SSL_CERT_FILE-unset}"
  printf 'SSL_CERT_DIR=%s\n' "${SSL_CERT_DIR-unset}"
  printf 'LD_PRELOAD=%s\n' "${LD_PRELOAD-unset}"
  printf 'LD_LIBRARY_PATH=%s\n' "${LD_LIBRARY_PATH-unset}"
  printf 'LD_AUDIT=%s\n' "${LD_AUDIT-unset}"
  printf 'LD_FUTURE_OVERRIDE=%s\n' "${LD_FUTURE_OVERRIDE-unset}"
  printf 'DYLD_INSERT_LIBRARIES=%s\n' "${DYLD_INSERT_LIBRARIES-unset}"
  printf 'GODEBUG=%s\n' "${GODEBUG-unset}"
  printf 'SSLKEYLOGFILE=%s\n' "${SSLKEYLOGFILE-unset}"
  printf 'HTTPS_PROXY=%s\n' "${HTTPS_PROXY-unset}"
  if [ -e "$HOME/.tunn3l/config.json" ]; then printf 'config_present=yes\n'; else printf 'config_present=no\n'; fi
  if [ -n "$(ls -A "$HOME")" ]; then printf 'home_empty=no\n'; else printf 'home_empty=yes\n'; fi
} > "$RDEV_TEST_TUNN3L_CAPTURE"
printf '%s\n' '{"url":"https://CEDAR-ORION.TUNN3L.SH","subdomain":"cedar-orion"}'
exec sleep 3600
`
	digest := sha256.Sum256([]byte(script))
	path := filepath.Join(root, fmt.Sprintf("tunn3l-%x", digest))
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func tunn3lCaptureValues(lines []string) map[string]string {
	values := make(map[string]string, len(lines))
	for _, line := range lines {
		name, value, ok := strings.Cut(line, "=")
		if ok {
			values[name] = value
		}
	}
	return values
}

func waitForTunn3lCapture(t *testing.T, path string) []string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		content, err := os.ReadFile(path)
		if err == nil && len(content) > 0 {
			return strings.Split(strings.TrimSpace(string(content)), "\n")
		}
		if time.Now().After(deadline) {
			t.Fatalf("capture was not written: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
