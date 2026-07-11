package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestLocalhostRunTrustAnchorProvenanceAndFingerprint(t *testing.T) {
	anchor := localhostRunTrustAnchor
	if anchor.ProviderID != tunnel.ProviderLocalhostRun || anchor.Host != "localhost.run" || anchor.Port != 22 {
		t.Fatalf("unexpected localhost.run identity: %#v", anchor)
	}
	if anchor.Fingerprint != "SHA256:FV8IMJ4IYjYUTnd6on7PqbRjaZf4c1EhhEBgeUdE94I" {
		t.Fatalf("fingerprint = %q", anchor.Fingerprint)
	}
	if anchor.SourceCommit != "9f499be7ece07d59ed927edbcfa6860ee7bcb853" {
		t.Fatalf("source commit = %q", anchor.SourceCommit)
	}
	if anchor.SourceURL != "https://github.com/localhost-run/client-service/blob/9f499be7ece07d59ed927edbcfa6860ee7bcb853/linux/systemd/localhost.run.service" {
		t.Fatalf("source URL = %q", anchor.SourceURL)
	}
	if anchor.ReviewedAt != "2026-07-11" {
		t.Fatalf("review date = %q", anchor.ReviewedAt)
	}

	fields := strings.Fields(anchor.KeyLine)
	if len(fields) != 3 || fields[0] != anchor.Host || fields[1] != "ssh-rsa" {
		t.Fatalf("unexpected official known-hosts line shape: %q", anchor.KeyLine)
	}
	keyBlob, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil || len(keyBlob) == 0 {
		t.Fatalf("decode official key: %v", err)
	}
	digest := sha256.Sum256(keyBlob)
	got := "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
	if got != anchor.Fingerprint {
		t.Fatalf("computed fingerprint = %q, want %q", got, anchor.Fingerprint)
	}
	if err := validateProviderTrustAnchor(anchor); err != nil {
		t.Fatalf("reviewed anchor rejected: %v", err)
	}
}

func TestLocalhostRunTrustAnchorRejectsInvalidConstants(t *testing.T) {
	fields := strings.Fields(localhostRunTrustAnchor.KeyLine)
	invalidKeyLine := func(host, keyType, encoded string) string {
		return strings.Join([]string{host, keyType, encoded}, " ")
	}
	tests := []struct {
		name   string
		mutate func(*providerTrustAnchor)
	}{
		{name: "provider ID", mutate: func(anchor *providerTrustAnchor) { anchor.ProviderID = "../localhost-run" }},
		{name: "host", mutate: func(anchor *providerTrustAnchor) { anchor.Host = "LOCALHOST.RUN" }},
		{name: "port", mutate: func(anchor *providerTrustAnchor) { anchor.Port = 443 }},
		{name: "key host", mutate: func(anchor *providerTrustAnchor) {
			anchor.KeyLine = invalidKeyLine("other.example", fields[1], fields[2])
		}},
		{name: "key type", mutate: func(anchor *providerTrustAnchor) { anchor.KeyLine = invalidKeyLine(fields[0], "ssh-dss", fields[2]) }},
		{name: "key encoding", mutate: func(anchor *providerTrustAnchor) {
			anchor.KeyLine = invalidKeyLine(fields[0], fields[1], "not-base64!")
		}},
		{name: "key trailing field", mutate: func(anchor *providerTrustAnchor) { anchor.KeyLine += " comment" }},
		{name: "fingerprint", mutate: func(anchor *providerTrustAnchor) { anchor.Fingerprint = "SHA256:wrong" }},
		{name: "source commit", mutate: func(anchor *providerTrustAnchor) { anchor.SourceCommit = strings.Repeat("a", 40) }},
		{name: "mutable source URL", mutate: func(anchor *providerTrustAnchor) {
			anchor.SourceURL = "https://github.com/localhost-run/client-service/blob/main/linux/systemd/localhost.run.service"
		}},
		{name: "source URL query", mutate: func(anchor *providerTrustAnchor) { anchor.SourceURL += "?download=1" }},
		{name: "review date", mutate: func(anchor *providerTrustAnchor) { anchor.ReviewedAt = "2026-7-11" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anchor := localhostRunTrustAnchor
			tt.mutate(&anchor)
			if err := validateProviderTrustAnchor(anchor); err == nil {
				t.Fatal("invalid trust-anchor constant accepted")
			}
		})
	}
}

func TestMaterializeProviderKnownHostsCreatesPrivateExactSnapshot(t *testing.T) {
	root := protectedTrustTestRoot(t)
	path, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(root, "provider-trust", tunnel.ProviderLocalhostRun, "known_hosts")
	if path != wantPath {
		t.Fatalf("known-hosts path = %q, want %q", path, wantPath)
	}
	content, err := tunnel.ReadProtectedRegularFile(path, maxKnownHostsBytes)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != localhostRunTrustAnchor.KeyLine+"\n" {
		t.Fatalf("known-hosts snapshot content = %q", content)
	}
	if err := validateKnownHostsFile(path, localhostRunTrustAnchor.Host, localhostRunTrustAnchor.Port); err != nil {
		t.Fatalf("materialized known-hosts rejected: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("known-hosts mode = %04o", info.Mode().Perm())
		}
		for _, directory := range []string{filepath.Dir(path), filepath.Dir(filepath.Dir(path))} {
			info, err := os.Stat(directory)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("trust directory %q mode = %04o", directory, info.Mode().Perm())
			}
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "known_hosts" {
		t.Fatalf("unexpected trust-directory entries: %#v", entries)
	}
}

func TestMaterializeProviderKnownHostsReusesIdenticalSnapshot(t *testing.T) {
	root := protectedTrustTestRoot(t)
	first, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(second)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !os.SameFile(before, after) {
		t.Fatalf("idempotent materialization replaced snapshot: %q, %q", first, second)
	}
}

func TestMaterializeProviderKnownHostsPublishesConcurrentlyWithoutReplacement(t *testing.T) {
	root := protectedTrustTestRoot(t)
	const workers = 12
	paths := make(chan string, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			path, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor)
			paths <- path
			errs <- err
		}()
	}
	group.Wait()
	close(paths)
	close(errs)
	wantPath := filepath.Join(root, "provider-trust", tunnel.ProviderLocalhostRun, "known_hosts")
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for path := range paths {
		if path != wantPath {
			t.Fatalf("concurrent materialization path = %q, want %q", path, wantPath)
		}
	}
	if err := validateKnownHostsFile(wantPath, localhostRunTrustAnchor.Host, localhostRunTrustAnchor.Port); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeProviderKnownHostsRejectsTamperedExistingSnapshot(t *testing.T) {
	root := protectedTrustTestRoot(t)
	path, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor)
	if err != nil {
		t.Fatal(err)
	}
	tampered := []byte("localhost.run ssh-rsa dGFtcGVyZWQ=\n")
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor); err == nil {
		t.Fatal("tampered existing snapshot was silently replaced")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(tampered) {
		t.Fatalf("tampered snapshot was overwritten: %q", got)
	}
}

func TestMaterializeProviderKnownHostsRejectsUnsafeExistingSnapshot(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := protectedTrustTestRoot(t)
		path, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(filepath.Dir(path), "target")
		if err := os.WriteFile(target, []byte(localhostRunTrustAnchor.KeyLine+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		protectKnownHostsTestFile(t, target, 0o600)
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor); err == nil {
			t.Fatal("symlink snapshot was accepted or replaced")
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("symlink snapshot was changed: info=%v err=%v", info, err)
		}
	})
	if runtime.GOOS != "windows" {
		t.Run("wide permissions", func(t *testing.T) {
			root := protectedTrustTestRoot(t)
			path, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor); err == nil {
				t.Fatal("wide-permission snapshot was accepted or replaced")
			}
		})
	}
}

func TestMaterializeProviderKnownHostsRejectsUnsafeRoot(t *testing.T) {
	if _, err := materializeProviderKnownHosts("relative-root", localhostRunTrustAnchor); err == nil {
		t.Fatal("relative provider root accepted")
	}
	if runtime.GOOS != "windows" {
		root := protectedTrustTestRoot(t)
		if err := os.Chmod(root, 0o777); err != nil {
			t.Fatal(err)
		}
		if _, err := materializeProviderKnownHosts(root, localhostRunTrustAnchor); err == nil {
			t.Fatal("wide-permission provider root accepted")
		}
	}
}

func TestLocalhostRunTrustAnchorProviderFallback(t *testing.T) {
	root := protectedTrustTestRoot(t)
	wantErr := errors.New("stop after pin capture")
	var captured string
	provider := cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{ID: tunnel.ProviderLocalhostRun},
		stderr:   io.Discard,
		start: func(_ context.Context, _ io.Writer, _ tunnel.StartRequest, knownHosts string) (runningTunnel, error) {
			captured = knownHosts
			return runningTunnel{}, wantErr
		},
	}
	if _, err := provider.Start(context.Background(), tunnel.StartRequest{ProviderRoot: root}); !errors.Is(err, wantErr) {
		t.Fatalf("provider start error = %v, want %v", err, wantErr)
	}
	wantPath := filepath.Join(root, "provider-trust", tunnel.ProviderLocalhostRun, "known_hosts")
	if captured != wantPath {
		t.Fatalf("fallback known-hosts path = %q, want %q", captured, wantPath)
	}
	if err := validateKnownHostsFile(captured, localhostRunTrustAnchor.Host, localhostRunTrustAnchor.Port); err != nil {
		t.Fatal(err)
	}
}

func TestLocalhostRunTrustAnchorOperatorOverrideTakesPriority(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		requested  string
		want       string
	}{
		{name: "configured operator path", configured: "configured_known_hosts", want: "configured_known_hosts"},
		{name: "request path", configured: "configured_known_hosts", requested: "request_known_hosts", want: "request_known_hosts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unusedRoot := filepath.Join(protectedTrustTestRoot(t), "must-not-be-created")
			wantErr := errors.New("stop after pin capture")
			var captured string
			provider := cliTunnelProvider{
				metadata:       tunnel.ProviderMetadata{ID: tunnel.ProviderLocalhostRun},
				stderr:         io.Discard,
				knownHostsFile: tt.configured,
				start: func(_ context.Context, _ io.Writer, _ tunnel.StartRequest, knownHosts string) (runningTunnel, error) {
					captured = knownHosts
					return runningTunnel{}, wantErr
				},
			}
			request := tunnel.StartRequest{KnownHostsFile: tt.requested, ProviderRoot: unusedRoot}
			if _, err := provider.Start(context.Background(), request); !errors.Is(err, wantErr) {
				t.Fatalf("provider start error = %v, want %v", err, wantErr)
			}
			if captured != tt.want {
				t.Fatalf("known-hosts path = %q, want %q", captured, tt.want)
			}
			if _, err := os.Lstat(unusedRoot); !os.IsNotExist(err) {
				t.Fatalf("built-in trust root was materialized despite operator override: %v", err)
			}
		})
	}
}

func TestLocalhostRunInvalidOperatorOverrideFailsClosed(t *testing.T) {
	invalid := writeKnownHostsTestFile(t, "other.example ssh-rsa dGVzdA==\n", 0o600)
	unusedRoot := filepath.Join(protectedTrustTestRoot(t), "must-not-be-created")
	t.Setenv("PATH", t.TempDir())
	provider := newLocalhostRunProvider(io.Discard, invalid)
	_, err := provider.Start(context.Background(), tunnel.StartRequest{LocalPort: "8787", ProviderRoot: unusedRoot})
	if err == nil || !strings.Contains(err.Error(), "SSH known-hosts validation") {
		t.Fatalf("invalid operator known-hosts error = %v", err)
	}
	if _, err := os.Lstat(unusedRoot); !os.IsNotExist(err) {
		t.Fatalf("invalid operator override fell back to built-in trust: %v", err)
	}
}

func TestLocalhostRunTrustAnchorProviderMetadata(t *testing.T) {
	localhost := newLocalhostRunProvider(io.Discard, "").Metadata()
	if !localhost.DefaultAutomatic || localhost.AutomaticPriority != 30 || localhost.RequiresSSHPin {
		t.Fatalf("localhost.run metadata = %#v", localhost)
	}
	pinggy := newPinggyProvider(io.Discard, "").Metadata()
	if pinggy.DefaultAutomatic || pinggy.AutomaticPriority != 40 || !pinggy.RequiresSSHPin {
		t.Fatalf("Pinggy metadata = %#v", pinggy)
	}
}

func protectedTrustTestRoot(t *testing.T) string {
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
