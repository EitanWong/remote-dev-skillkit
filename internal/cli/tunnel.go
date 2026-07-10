package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

const sshTunnelStartupTimeout = 20 * time.Second

type sshTunnelSpec struct {
	Destination   string
	Port          int
	RemoteForward string
}

func sshTunnelArgs(sshPath string, spec sshTunnelSpec, knownHostsFile string) ([]string, error) {
	if strings.TrimSpace(knownHostsFile) == "" {
		return nil, fmt.Errorf("reviewed known-hosts file is required")
	}
	for name, value := range map[string]string{
		"ssh path":         sshPath,
		"destination":      spec.Destination,
		"remote forward":   spec.RemoteForward,
		"known-hosts file": knownHostsFile,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("%s contains an unsafe control character", name)
		}
	}
	if strings.HasPrefix(spec.Destination, "-") || strings.ContainsAny(spec.Destination, " \t") {
		return nil, fmt.Errorf("invalid SSH destination")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return nil, fmt.Errorf("SSH port must be between 1 and 65535")
	}

	args := []string{sshPath}
	if spec.Port != 22 {
		args = append(args, "-p", strconv.Itoa(spec.Port))
	}
	return append(args,
		"-T", "-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile="+knownHostsFile,
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-R", spec.RemoteForward,
		spec.Destination,
	), nil
}

func localhostRunTunnelSpec(localPort string) (sshTunnelSpec, error) {
	port, err := validatedLocalPort(localPort)
	if err != nil {
		return sshTunnelSpec{}, err
	}
	return sshTunnelSpec{
		Destination:   "nokey@localhost.run",
		Port:          22,
		RemoteForward: "80:localhost:" + strconv.Itoa(port),
	}, nil
}

func pinggyTunnelSpec(localPort string) (sshTunnelSpec, error) {
	port, err := validatedLocalPort(localPort)
	if err != nil {
		return sshTunnelSpec{}, err
	}
	return sshTunnelSpec{
		Destination:   "free.pinggy.io",
		Port:          443,
		RemoteForward: "0:localhost:" + strconv.Itoa(port),
	}, nil
}

func validatedLocalPort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("local port must be an integer between 1 and 65535")
	}
	return port, nil
}

func startLocalhostRunTunnel(ctx context.Context, stderr io.Writer, localPort, knownHostsFile string) (string, context.CancelFunc, error) {
	spec, err := localhostRunTunnelSpec(localPort)
	if err != nil {
		return "", func() {}, err
	}
	return startSSHTunnel(ctx, stderr, "localhost-run", spec, knownHostsFile)
}

func startPinggyTunnel(ctx context.Context, stderr io.Writer, localPort, knownHostsFile string) (string, context.CancelFunc, error) {
	spec, err := pinggyTunnelSpec(localPort)
	if err != nil {
		return "", func() {}, err
	}
	return startSSHTunnel(ctx, stderr, "pinggy", spec, knownHostsFile)
}

func startSSHTunnel(ctx context.Context, stderr io.Writer, providerID string, spec sshTunnelSpec, knownHostsFile string) (string, context.CancelFunc, error) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return "", func() {}, fmt.Errorf("ssh not found in PATH: %w", err)
	}
	argv, err := sshTunnelArgs(sshPath, spec, knownHostsFile)
	if err != nil {
		return "", func() {}, fmt.Errorf("%s SSH configuration: %w", providerID, err)
	}

	tunnelCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(tunnelCtx, argv[0], argv[1:]...)
	pr, pw := io.Pipe()
	combined := io.MultiWriter(pw, stderr)
	cmd.Stdout = combined
	cmd.Stderr = combined
	if err := cmd.Start(); err != nil {
		cancel()
		_ = pw.Close()
		return "", func() {}, fmt.Errorf("%s SSH start failed: %w", providerID, err)
	}
	urlCh := make(chan string, 1)
	go func() {
		defer pw.Close()
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			if candidate := providerURLFromLine(providerID, scanner.Text()); candidate != "" {
				select {
				case urlCh <- candidate:
				default:
				}
			}
		}
	}()
	select {
	case tunnelURL := <-urlCh:
		return tunnelURL, cancel, nil
	case <-time.After(sshTunnelStartupTimeout):
		cancel()
		_ = pr.Close()
		return "", func() {}, fmt.Errorf("%s did not print a tunnel URL within %v", providerID, sshTunnelStartupTimeout)
	}
}

func localhostRunTunnelURLFromLine(line string) string {
	return providerURLFromLine("localhost-run", line)
}

func providerURLFromLine(providerID, line string) string {
	for remaining := line; ; {
		idx := strings.Index(strings.ToLower(remaining), "https://")
		if idx < 0 {
			return ""
		}
		rest := remaining[idx:]
		end := strings.IndexAny(rest, " \t\n\r|")
		if end < 0 {
			end = len(rest)
		}
		candidate := strings.Trim(strings.TrimRight(rest[:end], "/"), "\"'()[]{}<>,;")
		if validProviderURL(providerID, candidate) {
			return candidate
		}
		remaining = rest[end:]
	}
}

func validProviderURL(providerID, candidate string) bool {
	u, err := url.Parse(candidate)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil || u.Port() != "" {
		return false
	}
	if !strings.EqualFold(u.Host, u.Hostname()) {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch providerID {
	case "cloudflare-quick":
		return strictSubdomain(host, "trycloudflare.com")
	case "localhost-run":
		return strictSubdomain(host, "lhr.life") || (strictSubdomain(host, "localhost.run") && host != "admin.localhost.run")
	case "pinggy":
		return strictSubdomain(host, "pinggy.link") || strictSubdomain(host, "pinggy-free.link")
	default:
		return false
	}
}

func strictSubdomain(host, suffix string) bool {
	return host != suffix && strings.HasSuffix(host, "."+suffix)
}

func startCloudflaredQuickTunnel(ctx context.Context, stderr io.Writer, localURL string) (string, context.CancelFunc, error) {
	cfPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return "", func() {}, fmt.Errorf("cloudflared not found in PATH: %w", err)
	}

	tunnelURL, cancel, err := startCloudflaredWithProtocol(ctx, cfPath, stderr, localURL, "http2", 25*time.Second)
	if err == nil {
		return tunnelURL, cancel, nil
	}
	_, _ = fmt.Fprintf(stderr, "[rdev] cloudflared http2 attempt failed (%v); retrying without protocol flag\n", err)
	return startCloudflaredWithProtocol(ctx, cfPath, stderr, localURL, "", 20*time.Second)
}

func startCloudflaredWithProtocol(ctx context.Context, cfPath string, stderr io.Writer, localURL, protocol string, timeout time.Duration) (string, context.CancelFunc, error) {
	tunnelCtx, cancel := context.WithCancel(ctx)
	args := []string{"tunnel"}
	if protocol != "" {
		args = append(args, "--protocol", protocol)
	}
	args = append(args, "--url", localURL)

	cmd := exec.CommandContext(tunnelCtx, cfPath, args...)
	pr, pw := io.Pipe()
	cmd.Stderr = io.MultiWriter(pw, stderr)
	if err := cmd.Start(); err != nil {
		cancel()
		_ = pw.Close()
		return "", func() {}, fmt.Errorf("cloudflared start failed (protocol=%q): %w", protocol, err)
	}
	urlCh := make(chan string, 1)
	go func() {
		defer pw.Close()
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			if candidate := providerURLFromLine("cloudflare-quick", scanner.Text()); candidate != "" {
				select {
				case urlCh <- candidate:
				default:
				}
			}
		}
	}()

	select {
	case tunnelURL := <-urlCh:
		return tunnelURL, cancel, nil
	case <-time.After(timeout):
		cancel()
		_ = pr.Close()
		return "", func() {}, fmt.Errorf("cloudflared (protocol=%q) did not print a tunnel URL within %v", protocol, timeout)
	}
}

type cliTunnelProvider struct {
	metadata       tunnel.ProviderMetadata
	stderr         io.Writer
	knownHostsFile string
	start          func(context.Context, io.Writer, tunnel.StartRequest, string) (string, context.CancelFunc, error)
}

func newCloudflareQuickProvider(stderr io.Writer) tunnel.Provider {
	return cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{
			ID: "cloudflare-quick", DisplayName: "Cloudflare Quick Tunnel", Protocols: []string{"https"},
			Anonymous: true, Executable: "cloudflared", DocumentationURL: "https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/", DefaultAutomatic: true,
		},
		stderr: stderr,
		start: func(ctx context.Context, stderr io.Writer, request tunnel.StartRequest, _ string) (string, context.CancelFunc, error) {
			return startCloudflaredQuickTunnel(ctx, stderr, request.LocalURL)
		},
	}
}

func newLocalhostRunProvider(stderr io.Writer, knownHostsFile string) tunnel.Provider {
	return cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{
			ID: "localhost-run", DisplayName: "localhost.run", Protocols: []string{"https", "ssh"},
			Anonymous: true, Executable: "ssh", DocumentationURL: "https://localhost.run/docs/", RequiresSSHPin: true,
		},
		stderr: stderr, knownHostsFile: knownHostsFile,
		start: func(ctx context.Context, stderr io.Writer, request tunnel.StartRequest, pin string) (string, context.CancelFunc, error) {
			return startLocalhostRunTunnel(ctx, stderr, request.LocalPort, pin)
		},
	}
}

func newPinggyProvider(stderr io.Writer, knownHostsFile string) tunnel.Provider {
	return cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{
			ID: "pinggy", DisplayName: "Pinggy", Protocols: []string{"https", "ssh"},
			Anonymous: true, Executable: "ssh", DocumentationURL: "https://pinggy.io/docs/", RequiresSSHPin: true,
		},
		stderr: stderr, knownHostsFile: knownHostsFile,
		start: func(ctx context.Context, stderr io.Writer, request tunnel.StartRequest, pin string) (string, context.CancelFunc, error) {
			return startPinggyTunnel(ctx, stderr, request.LocalPort, pin)
		},
	}
}

func (p cliTunnelProvider) ID() string { return p.metadata.ID }

func (p cliTunnelProvider) Metadata() tunnel.ProviderMetadata {
	metadata := p.metadata
	metadata.Protocols = append([]string(nil), p.metadata.Protocols...)
	return metadata
}

func (p cliTunnelProvider) Start(ctx context.Context, request tunnel.StartRequest) (tunnel.Handle, error) {
	knownHostsFile := strings.TrimSpace(request.KnownHostsFile)
	if knownHostsFile == "" {
		knownHostsFile = strings.TrimSpace(p.knownHostsFile)
	}
	if p.metadata.RequiresSSHPin && knownHostsFile == "" {
		return nil, fmt.Errorf("provider %q requires a reviewed known-hosts file", p.metadata.ID)
	}
	publicURL, cancel, err := p.start(ctx, p.stderr, request, knownHostsFile)
	if err != nil {
		return nil, err
	}
	return newCLITunnelHandle(tunnel.Candidate{
		ProviderID:     p.metadata.ID,
		URL:            publicURL,
		FailureDomains: p.metadata.FailureDomains,
	}, cancel), nil
}

type cliTunnelHandle struct {
	candidate tunnel.Candidate
	cancel    context.CancelFunc
	done      chan error
	stopOnce  sync.Once
}

func newCLITunnelHandle(candidate tunnel.Candidate, cancel context.CancelFunc) *cliTunnelHandle {
	return &cliTunnelHandle{candidate: candidate, cancel: cancel, done: make(chan error, 1)}
}

func (h *cliTunnelHandle) Candidate() tunnel.Candidate { return h.candidate }

func (h *cliTunnelHandle) Wait() <-chan error { return h.done }

func (h *cliTunnelHandle) Stop(context.Context) error {
	h.stopOnce.Do(func() {
		h.cancel()
		h.done <- nil
		close(h.done)
	})
	return nil
}
