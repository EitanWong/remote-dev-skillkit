package cli

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestSSHProviderArgsRequireKnownHosts(t *testing.T) {
	spec := sshTunnelSpec{Destination: "nokey@localhost.run", Port: 22, RemoteForward: "80:localhost:8787"}
	if _, err := sshTunnelArgs("ssh", spec, ""); err == nil {
		t.Fatal("expected missing pin error")
	}
	args, err := sshTunnelArgs("ssh", spec, "/tmp/known_hosts")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "StrictHostKeyChecking=no") || !strings.Contains(joined, "StrictHostKeyChecking=yes") {
		t.Fatalf("unsafe args: %v", args)
	}
	if args[0] != "ssh" || strings.Contains(joined, "sh -c") || !strings.Contains(joined, "UserKnownHostsFile=/tmp/known_hosts") {
		t.Fatalf("expected direct pinned ssh argv, got %v", args)
	}
}

func TestSSHProviderArgsRejectUnsafeInputs(t *testing.T) {
	tests := []struct {
		name       string
		sshPath    string
		spec       sshTunnelSpec
		knownHosts string
	}{
		{name: "ssh path control", sshPath: "ssh\n", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known_hosts"},
		{name: "destination control", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host\r", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known_hosts"},
		{name: "forward control", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787\x00"}, knownHosts: "/tmp/known_hosts"},
		{name: "known hosts control", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 22, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known_hosts\n"},
		{name: "invalid port", sshPath: "ssh", spec: sshTunnelSpec{Destination: "host", Port: 0, RemoteForward: "80:localhost:8787"}, knownHosts: "/tmp/known_hosts"},
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
		{"cloudflare bare", "cloudflare-quick", "https://trycloudflare.com", ""},
		{"cloudflare suffix", "cloudflare-quick", "https://abc.trycloudflare.com.attacker.test", ""},
		{"cloudflare port", "cloudflare-quick", "https://abc.trycloudflare.com:8443", ""},
		{"cloudflare empty port", "cloudflare-quick", "https://abc.trycloudflare.com:", ""},
		{"localhost admin", "localhost-run", "https://admin.localhost.run", ""},
		{"localhost valid", "localhost-run", "https://abc.lhr.life", "https://abc.lhr.life"},
		{"userinfo", "localhost-run", "https://user@abc.lhr.life", ""},
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

func TestSSHProvidersRefuseStartWithoutReviewedKnownHosts(t *testing.T) {
	providers := []tunnel.Provider{
		newLocalhostRunProvider(io.Discard, ""),
		newPinggyProvider(io.Discard, ""),
	}
	for _, provider := range providers {
		t.Run(provider.ID(), func(t *testing.T) {
			if _, err := provider.Start(context.Background(), tunnel.StartRequest{LocalPort: "8787"}); err == nil {
				t.Fatal("expected provider start to require reviewed known-hosts")
			}
		})
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
