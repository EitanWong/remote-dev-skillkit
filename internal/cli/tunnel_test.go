package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestValidateKnownHostsFileAcceptsExactDefaultAndNonDefaultPorts(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("test host key"))
	tests := []struct {
		name        string
		destination string
		port        int
		hostField   string
	}{
		{name: "default port strips user", destination: "nokey@localhost.run", port: 22, hostField: "localhost.run"},
		{name: "non-default port", destination: "free.pinggy.io", port: 443, hostField: "[free.pinggy.io]:443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeKnownHostsTestFile(t, tt.hostField+" ssh-ed25519 "+key+"\n", 0o600)
			if err := validateKnownHostsFile(path, tt.destination, tt.port); err != nil {
				t.Fatalf("valid known_hosts rejected: %v", err)
			}
		})
	}
}

func TestKnownHostsPathSelectionRejectsEnvironmentFallbackOnWindows(t *testing.T) {
	tests := []struct {
		name        string
		configured  string
		environment string
		goos        string
		want        string
	}{
		{name: "POSIX environment fallback", environment: " /tmp/reviewed-known-hosts ", goos: "linux", want: "/tmp/reviewed-known-hosts"},
		{name: "Windows environment fallback", environment: `C:\\Users\\me\\known_hosts`, goos: "windows"},
		{name: "Windows explicit policy path", configured: ` C:\\Users\\me\\known_hosts `, environment: `C:\\unsafe`, goos: "windows", want: `C:\\Users\\me\\known_hosts`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectKnownHostsPath(tt.configured, tt.environment, tt.goos); got != tt.want {
				t.Fatalf("selectKnownHostsPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSameKnownHostsPathUsesWindowsCaseFoldingOnly(t *testing.T) {
	left := `c:\Users\Administrator\Pins\known_hosts`
	right := `C:\USERS\ADMINISTRATOR\PINS\KNOWN_HOSTS`
	if !sameKnownHostsPath(left, right, "windows") {
		t.Fatal("Windows canonical path comparison rejected case-only differences")
	}
	if sameKnownHostsPath(left, right, "linux") {
		t.Fatal("POSIX canonical path comparison accepted case-only differences")
	}
}

func TestValidateKnownHostsFileRejectsUnsafeContent(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("test host key"))
	tests := []struct {
		name        string
		destination string
		port        int
		content     string
	}{
		{name: "empty", destination: "localhost.run", port: 22},
		{name: "wrong host", destination: "localhost.run", port: 22, content: "other.example ssh-ed25519 " + key + "\n"},
		{name: "default port bracket form", destination: "localhost.run", port: 22, content: "[localhost.run]:22 ssh-ed25519 " + key + "\n"},
		{name: "non-default bare host", destination: "free.pinggy.io", port: 443, content: "free.pinggy.io ssh-ed25519 " + key + "\n"},
		{name: "hashed host", destination: "localhost.run", port: 22, content: "|1|c2FsdA==|aGFzaA== ssh-ed25519 " + key + "\n"},
		{name: "wildcard host", destination: "localhost.run", port: 22, content: "*.localhost.run ssh-ed25519 " + key + "\n"},
		{name: "negated host", destination: "localhost.run", port: 22, content: "!localhost.run ssh-ed25519 " + key + "\n"},
		{name: "host list", destination: "localhost.run", port: 22, content: "localhost.run,other.example ssh-ed25519 " + key + "\n"},
		{name: "cert authority marker", destination: "localhost.run", port: 22, content: "@cert-authority localhost.run ssh-ed25519 " + key + "\n"},
		{name: "revoked marker", destination: "localhost.run", port: 22, content: "@revoked localhost.run ssh-ed25519 " + key + "\n"},
		{name: "malformed", destination: "localhost.run", port: 22, content: "localhost.run ssh-ed25519\n"},
		{name: "unsupported key", destination: "localhost.run", port: 22, content: "localhost.run ssh-dss " + key + "\n"},
		{name: "non-base64", destination: "localhost.run", port: 22, content: "localhost.run ssh-ed25519 not-base64!\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeKnownHostsTestFile(t, tt.content, 0o600)
			if err := validateKnownHostsFile(path, tt.destination, tt.port); err == nil {
				t.Fatal("unsafe known_hosts content accepted")
			}
		})
	}
}

func TestValidateKnownHostsFileRejectsUnsafeDestination(t *testing.T) {
	path := writeKnownHostsTestFile(t, "localhost.run ssh-ed25519 dGVzdA==\n", 0o600)
	for _, destination := range []string{
		"", "user@", "user@@localhost.run", "https://localhost.run", "localhost.run/path", "localhost.run:22",
		"-localhost.run", "-oProxyCommand=x@localhost.run", "user name@localhost.run", "user\tname@localhost.run",
	} {
		t.Run(destination, func(t *testing.T) {
			if err := validateKnownHostsFile(path, destination, 22); err == nil {
				t.Fatalf("unsafe destination %q accepted", destination)
			}
		})
	}
}

func TestValidateKnownHostsFileRejectsUnsafePath(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	if err := validateKnownHostsFile(missing, "localhost.run", 22); err == nil {
		t.Fatal("missing known_hosts accepted")
	}
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateKnownHostsFile(directory, "localhost.run", 22); err == nil {
		t.Fatal("directory known_hosts accepted")
	}
	if runtime.GOOS != "windows" {
		for _, mode := range []os.FileMode{0o620, 0o644} {
			t.Run(fmt.Sprintf("mode %04o", mode), func(t *testing.T) {
				loose := writeKnownHostsTestFile(t, "localhost.run ssh-ed25519 dGVzdA==\n", mode)
				if err := validateKnownHostsFile(loose, "localhost.run", 22); err == nil {
					t.Fatalf("known_hosts mode %04o accepted", mode)
				}
			})
		}
		if err := validateKnownHostsFile(os.DevNull, "localhost.run", 22); err == nil {
			t.Fatal("device known_hosts accepted")
		}
		fifo := filepath.Join(root, "fifo")
		if err := exec.Command("mkfifo", fifo).Run(); err != nil {
			t.Skipf("mkfifo unavailable: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- validateKnownHostsFile(fifo, "localhost.run", 22) }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("FIFO known_hosts accepted")
			}
		case <-time.After(time.Second):
			t.Fatal("FIFO validation blocked")
		}
	}
	target := writeKnownHostsTestFile(t, "localhost.run ssh-ed25519 dGVzdA==\n", 0o600)
	link := filepath.Join(root, "known_hosts-link")
	if err := os.Symlink(target, link); err == nil {
		if err := validateKnownHostsFile(link, "localhost.run", 22); err == nil {
			t.Fatal("symlink known_hosts accepted")
		}
	}
	ancestor := filepath.Join(root, "ancestor-link")
	if err := os.Symlink(filepath.Dir(target), ancestor); err == nil {
		if err := validateKnownHostsFile(filepath.Join(ancestor, filepath.Base(target)), "localhost.run", 22); err == nil {
			t.Fatal("ancestor symlink known_hosts accepted")
		}
	}
}

func TestValidateKnownHostsFileRejectsOversizeInput(t *testing.T) {
	path := writeKnownHostsTestFile(t, strings.Repeat("#", (1<<20)+1), 0o600)
	if err := validateKnownHostsFile(path, "localhost.run", 22); err == nil {
		t.Fatal("oversize known_hosts accepted")
	}
}

func TestValidateKnownHostsFileRejectsSSHPathExpansionSyntax(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, path := range []string{
		"known hosts",
		"known\u2003hosts",
		"known%h",
		"known${HOME}",
		"~known_hosts",
		"none",
	} {
		t.Run(path, func(t *testing.T) {
			if err := os.WriteFile(path, []byte("localhost.run ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			protectKnownHostsTestFile(t, path, 0o600)
			if err := validateKnownHostsFile(path, "localhost.run", 22); err == nil {
				t.Fatalf("known_hosts path %q with SSH expansion syntax accepted", path)
			}
		})
	}
}

func TestStartSSHTunnelValidatesKnownHostsBeforeSSHLookup(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := startSSHTunnel(context.Background(), io.Discard, tunnel.ProviderLocalhostRun, sshTunnelSpec{
		Destination:   "nokey@localhost.run",
		Port:          22,
		RemoteForward: "80:localhost:8787",
	}, filepath.Join(t.TempDir(), "missing-known-hosts"))
	if err == nil || !strings.Contains(err.Error(), "SSH known-hosts validation") {
		t.Fatalf("startSSHTunnel error = %v, want known-hosts validation before SSH lookup", err)
	}
}

func writeKnownHostsTestFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "known_hosts")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	protectKnownHostsTestFile(t, path, mode)
	return path
}

func TestTunnelHelperProcess(t *testing.T) {
	mode := os.Getenv("RDEV_TEST_TUNNEL_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "exit":
		_, _ = fmt.Fprintln(os.Stderr, "ready https://abc.trycloudflare.com")
		os.Exit(23)
	case "block":
		_, _ = fmt.Fprintln(os.Stderr, "ready https://abc.trycloudflare.com")
		time.Sleep(time.Hour)
	case "secret-block":
		_, _ = fmt.Fprintln(os.Stdout, "token=cf-secret ticket=ABCD-1234 peer=203.0.113.9")
		_, _ = fmt.Fprintln(os.Stderr, "peer6=2001:db8::9 path=/Users/example/private/creds.json rejected=https://abc.trycloudflare.com/?token=query-secret")
		time.Sleep(50 * time.Millisecond)
		_, _ = fmt.Fprintln(os.Stdout, "assigned=https://abc.trycloudflare.com")
		time.Sleep(time.Hour)
	case "oversized-block":
		_, _ = fmt.Fprintln(os.Stdout, strings.Repeat("x", 70<<10)+" assigned=https://oversized.trycloudflare.com")
		time.Sleep(time.Hour)
	case "no-url-block":
		time.Sleep(time.Hour)
	default:
		os.Exit(24)
	}
}

func TestTunnelProviderOutputIsPrivate(t *testing.T) {
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "secret-block")
	var stderr synchronizedBuffer
	started, err := startTunnelCommand(context.Background(), &stderr, tunnel.ProviderCloudflareQuick, []string{
		os.Args[0], "-test.run=TestTunnelHelperProcess",
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stopTunnelTestProcess(t, started)
	if started.URL != "https://abc.trycloudflare.com" {
		t.Fatalf("assigned URL = %q", started.URL)
	}
	logged := stderr.String()
	expectedCandidateID := tunnel.CandidateID(tunnel.ProviderCloudflareQuick, started.URL)
	if !strings.Contains(logged, `"candidate_id":"`+expectedCandidateID+`"`) ||
		expectedCandidateID == tunnel.CandidateID(tunnel.ProviderCloudflareQuick, "https://different.trycloudflare.com") {
		t.Fatalf("provider lifecycle log did not use stable candidate correlation IDs: %q", logged)
	}
	for _, forbidden := range []string{
		"cf-secret", "ABCD-1234", "203.0.113.9", "2001:db8::9", "/Users/example/private/creds.json",
		"query-secret", "https://abc.trycloudflare.com",
	} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("provider output sentinel %q leaked to stderr: %q", forbidden, logged)
		}
	}
}

func TestTunnelProviderOversizedLineStillDrainsAndDiscoversURL(t *testing.T) {
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "oversized-block")
	started, err := startTunnelCommand(context.Background(), io.Discard, tunnel.ProviderCloudflareQuick, []string{
		os.Args[0], "-test.run=TestTunnelHelperProcess",
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stopTunnelTestProcess(t, started)
	if started.URL != "https://oversized.trycloudflare.com" {
		t.Fatalf("assigned URL = %q", started.URL)
	}
}

func TestProviderOutputSinkDiscoversSplitCanonicalURL(t *testing.T) {
	if got := providerURLFromLine(tunnel.ProviderCloudflareQuick, "noise https://ABC.TRYCLOUDFLARE.COM/\rprogress"); got != "https://abc.trycloudflare.com" {
		t.Fatalf("direct split-window URL = %q", got)
	}
	var candidates []string
	sink := &providerOutputSink{
		providerID: tunnel.ProviderCloudflareQuick,
		onCandidate: func(candidate string) {
			candidates = append(candidates, candidate)
		},
	}
	for _, chunk := range [][]byte{
		{0xff, 0xfe, 'n', 'o', 'i', 's', 'e', ' '},
		[]byte("htt"),
		[]byte("ps://ABC.TRYCLOUDFLARE.COM/\rprogress"),
		[]byte(strings.Repeat("x", 70<<10) + " https://later.trycloudflare.com"),
	} {
		if written, err := sink.Write(chunk); err != nil || written != len(chunk) {
			t.Fatalf("providerOutputSink.Write() = %d, %v", written, err)
		}
	}
	if !slices.Equal(candidates, []string{"https://abc.trycloudflare.com"}) {
		t.Fatalf("split output candidates = %#v", candidates)
	}
}

func TestProviderOutputSinkWaitsForDelimiterOrEOF(t *testing.T) {
	var candidates []string
	sink := &providerOutputSink{
		providerID: tunnel.ProviderCloudflareQuick,
		onCandidate: func(candidate string) {
			candidates = append(candidates, candidate)
		},
	}
	for _, chunk := range []string{
		"assigned=https://prefix.trycloudflare.com",
		"/?token=secret\nassigned=https://suffix.trycloudflare.com",
		".evil.example\n",
		"assigned=https://final.trycloudflare.com",
	} {
		if _, err := sink.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
		if len(candidates) != 0 {
			t.Fatalf("accepted a URL before a real delimiter or EOF: %#v", candidates)
		}
	}
	sink.Finalize()
	if !slices.Equal(candidates, []string{"https://final.trycloudflare.com"}) {
		t.Fatalf("EOF-finalized candidates = %#v", candidates)
	}
}

type blockAfterFirstProviderEventWriter struct {
	writes  atomic.Int32
	release <-chan struct{}
}

func (w *blockAfterFirstProviderEventWriter) Write(data []byte) (int, error) {
	if w.writes.Add(1) > 1 {
		<-w.release
	}
	return len(data), nil
}

func TestProviderOutputDrainDoesNotBlockOnLifecycleLogWriter(t *testing.T) {
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "block")
	release := make(chan struct{})
	log := &blockAfterFirstProviderEventWriter{release: release}
	process, err := startProviderProcess(context.Background(), log, tunnel.ProviderCloudflareQuick, []string{
		os.Args[0], "-test.run=TestTunnelHelperProcess",
	}, "", tunnel.ProviderCloudflareQuick)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer func() {
		close(release)
		cancelAndWaitProviderProcess(process, providerProcessCleanupTimeout)
	}()
	select {
	case candidate := <-process.candidates:
		if candidate != "https://abc.trycloudflare.com" {
			t.Fatalf("candidate = %q", candidate)
		}
	case <-time.After(time.Second):
		t.Fatal("provider output drain blocked on lifecycle log writer")
	}
}

func TestProviderProcessCleanupDeadlineExceedsWaitDelay(t *testing.T) {
	if providerProcessCleanupTimeout <= providerProcessWaitDelay {
		t.Fatalf("cleanup timeout %s must exceed WaitDelay %s", providerProcessCleanupTimeout, providerProcessWaitDelay)
	}
	closed := make(chan struct{})
	close(closed)
	if !cancelAndWaitProviderProcess(providerProcess{
		cancel: func() {}, lifecycle: &processLifecycle{reaped: closed},
	}, time.Millisecond) {
		t.Fatal("already-reaped process was reported as unreaped")
	}
	if cancelAndWaitProviderProcess(providerProcess{
		cancel: func() {}, lifecycle: &processLifecycle{reaped: make(chan struct{})},
	}, time.Millisecond) {
		t.Fatal("unreaped process was reported as reaped")
	}
	if cancelAndWaitProviderProcess(providerProcess{}, 0) {
		t.Fatal("process without lifecycle was reported as reaped")
	}
	if !cancelAndWaitProviderProcess(providerProcess{
		cancel: func() {}, lifecycle: &processLifecycle{reaped: closed},
	}, 0) {
		t.Fatal("zero-timeout check missed an already-reaped process")
	}
	if cancelAndWaitProviderProcess(providerProcess{
		cancel: func() {}, lifecycle: &processLifecycle{reaped: make(chan struct{})},
	}, 0) {
		t.Fatal("zero-timeout check reported a live process as reaped")
	}
}

func TestTunnelProviderManagerStartupCeilingIncludesManagedDownloadBudget(t *testing.T) {
	deps, err := defaultTunnelRuntimeDeps(io.Discard, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if deps.Manager.StartTimeout != 120*time.Second {
		t.Fatalf("manager startup ceiling = %s, want 120s", deps.Manager.StartTimeout)
	}
}

func TestTunnelProviderStartupTimeoutReapsWithoutRawOutput(t *testing.T) {
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "no-url-block")
	var stderr synchronizedBuffer
	_, err := startTunnelCommand(context.Background(), &stderr, tunnel.ProviderCloudflareQuick, []string{
		os.Args[0], "-test.run=TestTunnelHelperProcess",
	}, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "startup timed out") {
		t.Fatalf("startup timeout error = %v", err)
	}
	if !strings.Contains(stderr.String(), `"error_class":"timeout"`) {
		t.Fatalf("startup timeout log = %q", stderr.String())
	}
}

func TestProviderProcessStartFailureDoesNotExposeExecutablePath(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "alice-private-secret-binary")
	var stderr synchronizedBuffer
	_, err := startTunnelCommand(context.Background(), &stderr, tunnel.ProviderCloudflareQuick, []string{secretPath}, time.Second)
	if err == nil {
		t.Fatal("expected provider start failure")
	}
	for surface, value := range map[string]string{"error": err.Error(), "stderr": stderr.String()} {
		if strings.Contains(value, secretPath) || strings.Contains(value, "alice-private-secret-binary") {
			t.Fatalf("%s leaked executable path: %q", surface, value)
		}
	}
	if !strings.Contains(stderr.String(), `"error_class":"start-failed"`) {
		t.Fatalf("start failure did not emit fixed error class: %q", stderr.String())
	}
}

func TestConfiguredStableTunnelStartRejectsInvalidURLAndEarlyExit(t *testing.T) {
	if _, _, err := startConfiguredCloudflaredStableTunnelWithGrace(context.Background(), io.Discard, cloudflaredStableTunnelConfig{}, 10*time.Millisecond); err == nil {
		t.Fatal("empty stable argv accepted")
	}
	if _, _, err := startConfiguredCloudflaredStableTunnelWithGrace(context.Background(), io.Discard, cloudflaredStableTunnelConfig{
		GatewayURL: "https://user:password@stable.example.test",
		Argv:       []string{os.Args[0], "-test.run=TestTunnelHelperProcess"},
	}, 10*time.Millisecond); err == nil {
		t.Fatal("credential-bearing stable URL accepted")
	}
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "exit")
	_, _, err := startConfiguredCloudflaredStableTunnelWithGrace(context.Background(), io.Discard, cloudflaredStableTunnelConfig{
		GatewayURL: "https://stable.example.test",
		Argv:       []string{os.Args[0], "-test.run=TestTunnelHelperProcess"},
	}, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "exited during startup") {
		t.Fatalf("early stable process exit error = %v", err)
	}
}

func TestTunnelProviderEventRejectsArbitraryFields(t *testing.T) {
	var out strings.Builder
	writeTunnelProviderEvent(&out, "unsafe provider secret", "secret-phase", "secret-status", "https://secret.example.test/?token=query", "secret-error")
	logged := out.String()
	for _, forbidden := range []string{"unsafe provider secret", "secret-phase", "secret-status", "secret.example.test", "query", "secret-error"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("provider event leaked %q: %q", forbidden, logged)
		}
	}
	if !strings.Contains(logged, `"provider_id":"unknown"`) || !strings.Contains(logged, `"phase":"unknown"`) ||
		!strings.Contains(logged, `"status":"unknown"`) || !strings.Contains(logged, `"error_class":"failed"`) {
		t.Fatalf("provider event did not fail closed: %q", logged)
	}
}

func stopTunnelTestProcess(t *testing.T, started runningTunnel) {
	t.Helper()
	started.cancel()
	select {
	case <-started.lifecycle.reaped:
	case <-time.After(2 * time.Second):
		t.Fatal("tunnel helper was not reaped")
	}
}

func TestTunnelProviderWaitReportsSpontaneousProcessExit(t *testing.T) {
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "exit")
	provider := cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{ID: "cloudflare-quick"},
		stderr:   io.Discard,
		start: func(ctx context.Context, stderr io.Writer, _ tunnel.StartRequest, _ string) (runningTunnel, error) {
			return startTunnelCommand(ctx, stderr, "cloudflare-quick", []string{os.Args[0], "-test.run=TestTunnelHelperProcess"}, time.Second)
		},
	}
	handle, err := provider.Start(context.Background(), tunnel.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case waitErr := <-handle.Wait():
		if waitErr == nil {
			t.Fatal("expected spontaneous process exit error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider Wait did not report spontaneous process exit")
	}
}

func TestManagerStartupTimeoutDoesNotCancelCLIProviderProcess(t *testing.T) {
	t.Setenv("RDEV_TEST_TUNNEL_HELPER", "block")
	provider := cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{ID: tunnel.ProviderCloudflareQuick, DefaultAutomatic: true},
		stderr:   io.Discard,
		start: func(ctx context.Context, stderr io.Writer, _ tunnel.StartRequest, _ string) (runningTunnel, error) {
			return startTunnelCommand(ctx, stderr, tunnel.ProviderCloudflareQuick, []string{
				os.Args[0], "-test.run=TestTunnelHelperProcess",
			}, time.Second)
		},
	}
	runtime, err := (tunnel.Manager{
		StartTimeout: 500 * time.Millisecond,
		Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TLSOK: true, HealthOK: true}, nil
		},
	}).Start(context.Background(), []tunnel.Selection{{Provider: provider, Metadata: provider.Metadata()}}, tunnel.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(650 * time.Millisecond)
	snapshot := runtime.Snapshot()
	if len(snapshot.Candidates) != 1 || snapshot.Attempts[0].Status != tunnel.AttemptHealthy {
		t.Fatalf("manager startup timeout canceled CLI provider process: %#v", snapshot)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = runtime.Stop(stopCtx)
	if got := runtime.Snapshot().Attempts[0].Status; got != tunnel.AttemptStopped {
		t.Fatalf("CLI provider process was not reaped on runtime stop: %q", got)
	}
}

func TestCLITunnelHandleStopCancelsAndReapsExactlyOnce(t *testing.T) {
	var cancelCalls atomic.Int32
	var waitCalls atomic.Int32
	released := make(chan struct{})
	waitErr := errors.New("process wait result")
	lifecycle := newProcessLifecycle(func() error {
		waitCalls.Add(1)
		<-released
		return waitErr
	})
	handle := newCLITunnelHandle(tunnel.Candidate{}, func() {
		if cancelCalls.Add(1) == 1 {
			close(released)
		}
	}, lifecycle)

	if err := handle.Stop(context.Background()); !errors.Is(err, waitErr) {
		t.Fatalf("first Stop error = %v, want %v", err, waitErr)
	}
	if err := handle.Stop(context.Background()); !errors.Is(err, waitErr) {
		t.Fatalf("second Stop error = %v, want %v", err, waitErr)
	}
	if cancelCalls.Load() != 1 || waitCalls.Load() != 1 {
		t.Fatalf("cancel calls = %d, wait calls = %d; want one each", cancelCalls.Load(), waitCalls.Load())
	}
	if got := <-handle.Wait(); !errors.Is(got, waitErr) {
		t.Fatalf("Wait error = %v, want %v", got, waitErr)
	}
}

func TestCLITunnelHandleStopHonorsContextWhileReaping(t *testing.T) {
	released := make(chan struct{})
	lifecycle := newProcessLifecycle(func() error {
		<-released
		return nil
	})
	handle := newCLITunnelHandle(tunnel.Candidate{}, func() {}, lifecycle)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := handle.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want deadline exceeded", err)
	}
	close(released)
	<-handle.Wait()
}

func TestSSHProviderArgsRequireKnownHosts(t *testing.T) {
	spec := sshTunnelSpec{Destination: "nokey@localhost.run", Port: 22, RemoteForward: "80:localhost:8787"}
	if _, err := sshTunnelArgs("ssh", spec, ""); err == nil {
		t.Fatal("expected missing pin error")
	}
	args, err := sshTunnelArgs("ssh", spec, "known_hosts")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "StrictHostKeyChecking=no") || !strings.Contains(joined, "StrictHostKeyChecking=yes") {
		t.Fatalf("unsafe args: %v", args)
	}
	if args[0] != "ssh" || strings.Contains(joined, "sh -c") || !strings.Contains(joined, "UserKnownHostsFile=known_hosts") {
		t.Fatalf("expected direct pinned ssh argv, got %v", args)
	}
}

func TestSSHProviderArgsDisableSecondaryHostTrustSources(t *testing.T) {
	tests := []struct {
		name string
		spec sshTunnelSpec
		host string
		port string
	}{
		{name: "default port", spec: sshTunnelSpec{Destination: "nokey@localhost.run", Port: 22, RemoteForward: "80:localhost:8787"}, host: "localhost.run", port: "22"},
		{name: "non-default port", spec: sshTunnelSpec{Destination: "free.pinggy.io", Port: 443, RemoteForward: "0:localhost:8787"}, host: "free.pinggy.io", port: "443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := sshTunnelArgs("ssh", tt.spec, "known_hosts")
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, "\x00")
			for _, required := range []string{
				"-F\x00none",
				"-S\x00none",
				"-p\x00" + tt.port,
				"GlobalKnownHostsFile=none",
				"VerifyHostKeyDNS=no",
				"CheckHostIP=no",
				"CanonicalizeHostname=no",
				"UpdateHostKeys=no",
				"Hostname=" + tt.host,
				"ProxyCommand=none",
				"ProxyJump=none",
			} {
				if !strings.Contains(joined, required) {
					t.Fatalf("SSH argv missing %q: %v", required, args)
				}
			}
			if strings.Contains(joined, "KnownHostsCommand=") {
				t.Fatalf("SSH argv must rely on -F none instead of an executable KnownHostsCommand value: %v", args)
			}
		})
	}
}

func TestSSHProviderArgsRejectUnsafeInputs(t *testing.T) {
	tests := []struct {
		name       string
		sshPath    string
		spec       sshTunnelSpec
		knownHosts string
	}{
		{name: "ssh path control", sshPath: "ssh\n", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "known_hosts"},
		{name: "destination control", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host\r", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "known_hosts"},
		{name: "forward control", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787\x00"}, knownHosts: "known_hosts"},
		{name: "known hosts control", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known_hosts\n"},
		{name: "known hosts list whitespace", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known hosts"},
		{name: "known hosts Unicode whitespace", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known\u2003hosts"},
		{name: "known hosts token", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known_%h"},
		{name: "known hosts environment", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "${HOME}/known_hosts"},
		{name: "known hosts tilde", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "~/.ssh/known_hosts"},
		{name: "known hosts none", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "none"},
		{name: "known hosts absolute path", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known_hosts"},
		{name: "known hosts parent traversal", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "../known_hosts"},
		{name: "invalid port", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 0, RemoteForward: "80:localhost:8787"}, knownHosts: "known_hosts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := sshTunnelArgs(tt.sshPath, tt.spec, tt.knownHosts); err == nil {
				t.Fatal("expected unsafe ssh input to be rejected")
			}
		})
	}
}

func TestProviderURLParsersRejectMisleadingURLs(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		line     string
		want     string
	}{
		{"cloudflare valid", "cloudflare-quick", "ready https://abc.trycloudflare.com", "https://abc.trycloudflare.com"},
		{"cloudflare canonical", "cloudflare-quick", "ready HTTPS://ABC.TRYCLOUDFLARE.COM/", "https://abc.trycloudflare.com"},
		{"cloudflare bare", "cloudflare-quick", "https://trycloudflare.com", ""},
		{"cloudflare suffix", "cloudflare-quick", "https://abc.trycloudflare.com.attacker.test", ""},
		{"cloudflare port", "cloudflare-quick", "https://abc.trycloudflare.com:8443", ""},
		{"cloudflare empty port", "cloudflare-quick", "https://abc.trycloudflare.com:", ""},
		{"cloudflare query", "cloudflare-quick", "https://abc.trycloudflare.com/?token=query-secret", ""},
		{"cloudflare fragment", "cloudflare-quick", "https://abc.trycloudflare.com/#secret", ""},
		{"cloudflare path", "cloudflare-quick", "https://abc.trycloudflare.com/private", ""},
		{"cloudflare encoded path delimiter", "cloudflare-quick", "https://abc.trycloudflare.com/%3Ftoken", ""},
		{"cloudflare wildcard label", "cloudflare-quick", "https://*.trycloudflare.com", ""},
		{"cloudflare empty label", "cloudflare-quick", "https://a..trycloudflare.com", ""},
		{"cloudflare oversized label", "cloudflare-quick", "https://" + strings.Repeat("a", 64) + ".trycloudflare.com", ""},
		{"cloudflare skips invalid label", "cloudflare-quick", "https://*.trycloudflare.com https://valid.trycloudflare.com", "https://valid.trycloudflare.com"},
		{"localhost admin", "localhost-run", "https://admin.localhost.run", ""},
		{"localhost valid", "localhost-run", "https://abc.lhr.life", "https://abc.lhr.life"},
		{"userinfo", "localhost-run", "https://user@abc.lhr.life", ""},
		{"userinfo password", "localhost-run", "https://user:password@abc.lhr.life", ""},
		{"localhost port", "localhost-run", "https://abc.lhr.life:443", ""},
		{"localhost empty port", "localhost-run", "https://abc.lhr.life:", ""},
		{"pinggy valid", "pinggy", "tunnel https://abc.pinggy.link", "https://abc.pinggy.link"},
		{"pinggy free valid", "pinggy", "tunnel https://abc.pinggy-free.link", "https://abc.pinggy-free.link"},
		{"pinggy bare", "pinggy", "https://pinggy.link", ""},
		{"pinggy suffix", "pinggy", "https://abc.pinggy.link.attacker.test", ""},
		{"pinggy userinfo", "pinggy", "https://user@abc.pinggy.link", ""},
		{"pinggy port", "pinggy", "https://abc.pinggy.link:443", ""},
		{"pinggy empty port", "pinggy", "https://abc.pinggy.link:", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerURLFromLine(tt.provider, tt.line); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestPinggyProviderRefusesStartWithoutReviewedKnownHosts(t *testing.T) {
	provider := newPinggyProvider(io.Discard, "")
	if _, err := provider.Start(context.Background(), tunnel.StartRequest{LocalPort: "8787"}); err == nil {
		t.Fatal("expected Pinggy start to require reviewed known-hosts")
	}
}

func TestSSHTunnelSpecsValidateLocalPort(t *testing.T) {
	for _, port := range []string{"", "0", "65536", "8787\n", "not-a-port"} {
		t.Run(port, func(t *testing.T) {
			if _, err := localhostRunTunnelSpec(port); err == nil {
				t.Fatalf("expected localhost.run port %q to be rejected", port)
			}
			if _, err := pinggyTunnelSpec(port); err == nil {
				t.Fatalf("expected pinggy port %q to be rejected", port)
			}
		})
	}
}
