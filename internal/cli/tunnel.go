package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

const sshTunnelStartupTimeout = 20 * time.Second

type tunnelProviderPolicyFile struct {
	AllowedProviderIDs    []string          `json:"allowed_provider_ids"`
	DisabledProviderIDs   []string          `json:"disabled_provider_ids"`
	RegionalEvidencePaths []string          `json:"regional_evidence_paths"`
	SSHKnownHostsPaths    map[string]string `json:"ssh_known_hosts_paths"`
}

type tunnelRuntimeConfig struct {
	Region             tunnel.RegionProfile
	AllowedProviderIDs []string
	Evidence           []tunnel.RegionalEvidence
	SSHKnownHostsPaths map[string]string
}

func defaultTunnelRuntimeDeps(stderr io.Writer, knownHostsPaths map[string]string) (supportSessionStartDeps, error) {
	knownHosts := func(providerID string) string {
		if path := strings.TrimSpace(knownHostsPaths[providerID]); path != "" {
			return path
		}
		return strings.TrimSpace(os.Getenv("RDEV_SSH_KNOWN_HOSTS_FILE"))
	}
	registry, err := tunnel.NewRegistry(
		newCloudflareQuickProvider(stderr),
		newLocalhostRunProvider(stderr, knownHosts(tunnel.ProviderLocalhostRun)),
		newPinggyProvider(stderr, knownHosts(tunnel.ProviderPinggy)),
	)
	if err != nil {
		return supportSessionStartDeps{}, err
	}
	return supportSessionStartDeps{
		Registry: registry,
		Manager:  tunnel.Manager{MaxActive: 2, StartTimeout: 25 * time.Second, ProbeTimeout: 15 * time.Second},
		FinalProbe: func(ctx context.Context, candidate tunnel.Candidate, ticketCode, instance string) error {
			_, err := tunnel.ProbeBootstrapAsset(ctx, nil, candidate, ticketCode, instance)
			return err
		},
	}, nil
}

func loadTunnelRuntimeConfig(regionValue, policyPath string, registry tunnel.Registry) (tunnelRuntimeConfig, error) {
	region := tunnel.RegionProfile(strings.TrimSpace(regionValue))
	if region == "" {
		region = tunnel.RegionGlobal
	}
	if region != tunnel.RegionGlobal && region != tunnel.RegionCNMainland {
		return tunnelRuntimeConfig{}, fmt.Errorf("unsupported tunnel region %q; use global or cn-mainland", regionValue)
	}
	config := tunnelRuntimeConfig{Region: region, SSHKnownHostsPaths: map[string]string{}}
	if strings.TrimSpace(policyPath) == "" {
		return config, nil
	}
	info, err := os.Stat(policyPath)
	if err != nil {
		return tunnelRuntimeConfig{}, fmt.Errorf("read provider policy: %w", err)
	}
	if !info.Mode().IsRegular() {
		return tunnelRuntimeConfig{}, fmt.Errorf("provider policy must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&^os.FileMode(0o600) != 0 {
		return tunnelRuntimeConfig{}, fmt.Errorf("provider policy permissions must be 0600 or narrower")
	}
	file, err := os.Open(policyPath)
	if err != nil {
		return tunnelRuntimeConfig{}, fmt.Errorf("open provider policy: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var policy tunnelProviderPolicyFile
	if err := decoder.Decode(&policy); err != nil {
		return tunnelRuntimeConfig{}, fmt.Errorf("decode provider policy: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return tunnelRuntimeConfig{}, fmt.Errorf("decode provider policy: trailing JSON value is not allowed")
		}
		return tunnelRuntimeConfig{}, fmt.Errorf("decode provider policy trailing data: %w", err)
	}
	known := make(map[string]bool)
	for _, metadata := range registry.Providers() {
		known[metadata.ID] = true
	}
	disabled := make(map[string]bool)
	for _, id := range policy.DisabledProviderIDs {
		id = strings.TrimSpace(id)
		if !known[id] {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy references unknown provider %q", id)
		}
		disabled[id] = true
	}
	for _, id := range policy.AllowedProviderIDs {
		id = strings.TrimSpace(id)
		if !known[id] {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy references unknown provider %q", id)
		}
		if !disabled[id] {
			config.AllowedProviderIDs = append(config.AllowedProviderIDs, id)
		}
	}
	if len(policy.AllowedProviderIDs) == 0 && len(disabled) > 0 {
		for id := range known {
			if !disabled[id] {
				config.AllowedProviderIDs = append(config.AllowedProviderIDs, id)
			}
		}
		slices.Sort(config.AllowedProviderIDs)
	}
	for id, path := range policy.SSHKnownHostsPaths {
		if !known[id] {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy references unknown provider %q", id)
		}
		if strings.TrimSpace(path) == "" {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider %q known-hosts path is empty", id)
		}
		config.SSHKnownHostsPaths[id] = path
	}
	for _, evidencePath := range policy.RegionalEvidencePaths {
		evidence, err := loadRegionalEvidenceFile(evidencePath)
		if err != nil {
			return tunnelRuntimeConfig{}, err
		}
		for _, item := range evidence {
			if !known[item.ProviderID] {
				return tunnelRuntimeConfig{}, fmt.Errorf("regional evidence references unknown provider %q", item.ProviderID)
			}
		}
		config.Evidence = append(config.Evidence, evidence...)
	}
	return config, nil
}

func loadRegionalEvidenceFile(path string) ([]tunnel.RegionalEvidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read regional evidence %q: %w", path, err)
	}
	var values []tunnel.RegionalEvidence
	if err := json.Unmarshal(data, &values); err != nil {
		var single tunnel.RegionalEvidence
		if singleErr := json.Unmarshal(data, &single); singleErr != nil {
			return nil, fmt.Errorf("decode regional evidence %q: %w", path, err)
		}
		values = []tunnel.RegionalEvidence{single}
	}
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return nil, fmt.Errorf("validate regional evidence %q: %w", path, err)
		}
	}
	return values, nil
}

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

func startLocalhostRunTunnel(ctx context.Context, stderr io.Writer, localPort, knownHostsFile string) (runningTunnel, error) {
	spec, err := localhostRunTunnelSpec(localPort)
	if err != nil {
		return runningTunnel{}, err
	}
	return startSSHTunnel(ctx, stderr, tunnel.ProviderLocalhostRun, spec, knownHostsFile)
}

func startPinggyTunnel(ctx context.Context, stderr io.Writer, localPort, knownHostsFile string) (runningTunnel, error) {
	spec, err := pinggyTunnelSpec(localPort)
	if err != nil {
		return runningTunnel{}, err
	}
	return startSSHTunnel(ctx, stderr, tunnel.ProviderPinggy, spec, knownHostsFile)
}

func startSSHTunnel(ctx context.Context, stderr io.Writer, providerID string, spec sshTunnelSpec, knownHostsFile string) (runningTunnel, error) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return runningTunnel{}, fmt.Errorf("ssh not found in PATH: %w", err)
	}
	argv, err := sshTunnelArgs(sshPath, spec, knownHostsFile)
	if err != nil {
		return runningTunnel{}, fmt.Errorf("%s SSH configuration: %w", providerID, err)
	}
	return startTunnelCommand(ctx, stderr, providerID, argv, sshTunnelStartupTimeout)
}

func localhostRunTunnelURLFromLine(line string) string {
	return providerURLFromLine(tunnel.ProviderLocalhostRun, line)
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
	case tunnel.ProviderCloudflareQuick:
		return strictSubdomain(host, "trycloudflare.com")
	case tunnel.ProviderLocalhostRun:
		return strictSubdomain(host, "lhr.life") || (strictSubdomain(host, "localhost.run") && host != "admin.localhost.run")
	case tunnel.ProviderPinggy:
		return strictSubdomain(host, "pinggy.link") || strictSubdomain(host, "pinggy-free.link")
	default:
		return false
	}
}

func strictSubdomain(host, suffix string) bool {
	return host != suffix && strings.HasSuffix(host, "."+suffix)
}

func startCloudflaredQuickTunnel(ctx context.Context, stderr io.Writer, localURL string) (runningTunnel, error) {
	cfPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return runningTunnel{}, fmt.Errorf("cloudflared not found in PATH: %w", err)
	}

	started, err := startCloudflaredWithProtocol(ctx, cfPath, stderr, localURL, "http2", 25*time.Second)
	if err == nil {
		return started, nil
	}
	_, _ = fmt.Fprintf(stderr, "[rdev] cloudflared http2 attempt failed (%v); retrying without protocol flag\n", err)
	return startCloudflaredWithProtocol(ctx, cfPath, stderr, localURL, "", 20*time.Second)
}

func startCloudflaredWithProtocol(ctx context.Context, cfPath string, stderr io.Writer, localURL, protocol string, timeout time.Duration) (runningTunnel, error) {
	args := []string{"tunnel"}
	if protocol != "" {
		args = append(args, "--protocol", protocol)
	}
	args = append(args, "--url", localURL)
	return startTunnelCommand(ctx, stderr, tunnel.ProviderCloudflareQuick, append([]string{cfPath}, args...), timeout)
}

type runningTunnel struct {
	URL       string
	cancel    context.CancelFunc
	lifecycle *processLifecycle
}

func startTunnelCommand(ctx context.Context, stderr io.Writer, providerID string, argv []string, timeout time.Duration) (runningTunnel, error) {
	if len(argv) == 0 {
		return runningTunnel{}, fmt.Errorf("%s command argv is empty", providerID)
	}
	if stderr == nil {
		stderr = io.Discard
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
		_ = pr.Close()
		return runningTunnel{}, fmt.Errorf("%s start failed: %w", providerID, err)
	}
	lifecycle := newProcessLifecycle(func() error {
		err := cmd.Wait()
		_ = pw.Close()
		return err
	})
	urlCh := make(chan string, 1)
	go func() {
		defer pr.Close()
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

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case tunnelURL := <-urlCh:
		return runningTunnel{URL: tunnelURL, cancel: cancel, lifecycle: lifecycle}, nil
	case <-lifecycle.reaped:
		select {
		case tunnelURL := <-urlCh:
			return runningTunnel{URL: tunnelURL, cancel: cancel, lifecycle: lifecycle}, nil
		default:
		}
		cancel()
		if err := lifecycle.err(); err != nil {
			return runningTunnel{}, fmt.Errorf("%s exited during startup: %w", providerID, err)
		}
		return runningTunnel{}, fmt.Errorf("%s exited during startup", providerID)
	case <-timer.C:
		cancel()
		_ = pr.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		select {
		case <-lifecycle.reaped:
		case <-cleanupCtx.Done():
		}
		return runningTunnel{}, fmt.Errorf("%s did not print a tunnel URL within %v", providerID, timeout)
	case <-ctx.Done():
		cancel()
		return runningTunnel{}, ctx.Err()
	}
}

type cliTunnelProvider struct {
	metadata       tunnel.ProviderMetadata
	stderr         io.Writer
	knownHostsFile string
	start          func(context.Context, io.Writer, tunnel.StartRequest, string) (runningTunnel, error)
}

func newCloudflareQuickProvider(stderr io.Writer) tunnel.Provider {
	return cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{
			ID: tunnel.ProviderCloudflareQuick, DisplayName: "Cloudflare Quick Tunnel", Protocols: []string{"https"},
			Anonymous: true, Executable: "cloudflared", DocumentationURL: "https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/", DefaultAutomatic: true,
		},
		stderr: stderr,
		start: func(ctx context.Context, stderr io.Writer, request tunnel.StartRequest, _ string) (runningTunnel, error) {
			return startCloudflaredQuickTunnel(ctx, stderr, request.LocalURL)
		},
	}
}

func newLocalhostRunProvider(stderr io.Writer, knownHostsFile string) tunnel.Provider {
	return cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{
			ID: tunnel.ProviderLocalhostRun, DisplayName: "localhost.run", Protocols: []string{"https", "ssh"},
			Anonymous: true, Executable: "ssh", DocumentationURL: "https://localhost.run/docs/", RequiresSSHPin: true,
		},
		stderr: stderr, knownHostsFile: knownHostsFile,
		start: func(ctx context.Context, stderr io.Writer, request tunnel.StartRequest, pin string) (runningTunnel, error) {
			return startLocalhostRunTunnel(ctx, stderr, request.LocalPort, pin)
		},
	}
}

func newPinggyProvider(stderr io.Writer, knownHostsFile string) tunnel.Provider {
	return cliTunnelProvider{
		metadata: tunnel.ProviderMetadata{
			ID: tunnel.ProviderPinggy, DisplayName: "Pinggy", Protocols: []string{"https", "ssh"},
			Anonymous: true, Executable: "ssh", DocumentationURL: "https://pinggy.io/docs/", RequiresSSHPin: true,
		},
		stderr: stderr, knownHostsFile: knownHostsFile,
		start: func(ctx context.Context, stderr io.Writer, request tunnel.StartRequest, pin string) (runningTunnel, error) {
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
	started, err := p.start(ctx, p.stderr, request, knownHostsFile)
	if err != nil {
		return nil, err
	}
	return newCLITunnelHandle(tunnel.Candidate{
		ProviderID:     p.metadata.ID,
		URL:            started.URL,
		FailureDomains: p.metadata.FailureDomains,
	}, started.cancel, started.lifecycle), nil
}

type cliTunnelHandle struct {
	candidate tunnel.Candidate
	cancel    context.CancelFunc
	lifecycle *processLifecycle
	stopOnce  sync.Once
}

type processLifecycle struct {
	wait   chan error
	reaped chan struct{}
	mu     sync.RWMutex
	result error
}

func newProcessLifecycle(wait func() error) *processLifecycle {
	lifecycle := &processLifecycle{wait: make(chan error, 1), reaped: make(chan struct{})}
	go func() {
		err := wait()
		lifecycle.mu.Lock()
		lifecycle.result = err
		lifecycle.mu.Unlock()
		lifecycle.wait <- err
		close(lifecycle.wait)
		close(lifecycle.reaped)
	}()
	return lifecycle
}

func (l *processLifecycle) err() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.result
}

func newCLITunnelHandle(candidate tunnel.Candidate, cancel context.CancelFunc, lifecycle *processLifecycle) *cliTunnelHandle {
	return &cliTunnelHandle{candidate: candidate, cancel: cancel, lifecycle: lifecycle}
}

func (h *cliTunnelHandle) Candidate() tunnel.Candidate { return h.candidate }

func (h *cliTunnelHandle) Wait() <-chan error { return h.lifecycle.wait }

func (h *cliTunnelHandle) Stop(ctx context.Context) error {
	h.stopOnce.Do(h.cancel)
	select {
	case <-h.lifecycle.reaped:
		return h.lifecycle.err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
