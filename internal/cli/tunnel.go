package cli

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

const sshTunnelStartupTimeout = 20 * time.Second
const maxKnownHostsBytes = 1 << 20

type tunnelProviderPolicyFile struct {
	AllowedProviderIDs    *[]string         `json:"allowed_provider_ids"`
	DisabledProviderIDs   []string          `json:"disabled_provider_ids"`
	RegionalEvidencePaths []string          `json:"regional_evidence_paths"`
	SSHKnownHostsPaths    map[string]string `json:"ssh_known_hosts_paths"`
}

type tunnelRuntimeConfig struct {
	Region             tunnel.RegionProfile
	AllowedProviderIDs []string
	RestrictProviders  bool
	Evidence           []tunnel.RegionalEvidence
	SSHKnownHostsPaths map[string]string
}

func defaultTunnelRuntimeDeps(stderr io.Writer, knownHostsPaths map[string]string) (supportSessionStartDeps, error) {
	knownHosts := func(providerID string) string {
		return selectKnownHostsPath(knownHostsPaths[providerID], os.Getenv("RDEV_SSH_KNOWN_HOSTS_FILE"), runtime.GOOS)
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
		BootstrapProbe: func(ctx context.Context, candidate tunnel.Candidate, instance string) error {
			_, err := tunnel.ProbeBootstrapTemplate(ctx, nil, candidate, instance)
			return err
		},
		FinalProbe: func(ctx context.Context, candidate tunnel.Candidate, ticketCode, instance string) error {
			_, err := tunnel.ProbeBootstrapAsset(ctx, nil, candidate, ticketCode, instance)
			return err
		},
	}, nil
}

func selectKnownHostsPath(configuredPath, environmentPath, goos string) string {
	if path := strings.TrimSpace(configuredPath); path != "" {
		return path
	}
	if goos == "windows" {
		return ""
	}
	return strings.TrimSpace(environmentPath)
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
	var policy tunnelProviderPolicyFile
	if err := tunnel.ReadProtectedJSONFile(policyPath, &policy); err != nil {
		return tunnelRuntimeConfig{}, fmt.Errorf("decode provider policy: %w", err)
	}
	known := make(map[string]bool)
	for _, metadata := range registry.Providers() {
		known[metadata.ID] = true
	}
	disabled := make(map[string]bool)
	for _, value := range policy.DisabledProviderIDs {
		id := strings.TrimSpace(value)
		if id != value {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy disabled_provider_ids contains non-canonical provider %q", value)
		}
		if !known[id] {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy references unknown provider %q", id)
		}
		if disabled[id] {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy disabled_provider_ids contains duplicate provider %q", id)
		}
		disabled[id] = true
	}
	allowed := []string(nil)
	if policy.AllowedProviderIDs != nil {
		config.RestrictProviders = true
		allowed = *policy.AllowedProviderIDs
	}
	allowedSeen := make(map[string]bool, len(allowed))
	for _, value := range allowed {
		id := strings.TrimSpace(value)
		if id != value {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy allowed_provider_ids contains non-canonical provider %q", value)
		}
		if !known[id] {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy references unknown provider %q", id)
		}
		if allowedSeen[id] {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy allowed_provider_ids contains duplicate provider %q", id)
		}
		if disabled[id] {
			return tunnelRuntimeConfig{}, fmt.Errorf("provider policy lists provider %q as both allowed and disabled", id)
		}
		allowedSeen[id] = true
		config.AllowedProviderIDs = append(config.AllowedProviderIDs, id)
	}
	if policy.AllowedProviderIDs == nil && len(disabled) > 0 {
		config.RestrictProviders = true
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
	var data json.RawMessage
	if err := tunnel.ReadProtectedJSONFile(path, &data); err != nil {
		return nil, fmt.Errorf("read regional evidence %q: %w", path, err)
	}
	var values []tunnel.RegionalEvidence
	if err := decodeStrictTunnelJSON(data, &values); err != nil {
		var single tunnel.RegionalEvidence
		if singleErr := decodeStrictTunnelJSON(data, &single); singleErr != nil {
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

func decodeStrictTunnelJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value is not allowed")
		}
		return err
	}
	return nil
}

type sshTunnelSpec struct {
	Destination   string
	Port          int
	RemoteForward string
}

func sshTunnelArgs(sshPath string, spec sshTunnelSpec, knownHostsName string) ([]string, error) {
	if strings.TrimSpace(knownHostsName) == "" {
		return nil, fmt.Errorf("reviewed known-hosts file is required")
	}
	if err := validateSSHKnownHostsName(knownHostsName); err != nil {
		return nil, err
	}
	for name, value := range map[string]string{
		"ssh path":         sshPath,
		"destination":      spec.Destination,
		"remote forward":   spec.RemoteForward,
		"known-hosts file": knownHostsName,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("%s contains an unsafe control character", name)
		}
	}
	host, err := canonicalSSHDestinationHost(spec.Destination)
	if err != nil {
		return nil, err
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return nil, fmt.Errorf("SSH port must be between 1 and 65535")
	}

	args := []string{sshPath, "-F", "none", "-S", "none", "-p", strconv.Itoa(spec.Port)}
	return append(args,
		"-T", "-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile="+knownHostsName,
		"-o", "GlobalKnownHostsFile=none",
		"-o", "VerifyHostKeyDNS=no",
		"-o", "CheckHostIP=no",
		"-o", "CanonicalizeHostname=no",
		"-o", "UpdateHostKeys=no",
		"-o", "Hostname="+host,
		"-o", "ProxyCommand=none",
		"-o", "ProxyJump=none",
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-R", spec.RemoteForward,
		spec.Destination,
	), nil
}

func validateKnownHostsFile(path, destination string, port int) error {
	_, err := loadValidatedKnownHostsFile(path, destination, port)
	return err
}

type validatedKnownHostsFile struct {
	directory string
	name      string
	path      string
}

func loadValidatedKnownHostsFile(path, destination string, port int) (validatedKnownHostsFile, error) {
	prepared, err := prepareSSHKnownHostsFile(path)
	if err != nil {
		return validatedKnownHostsFile{}, err
	}
	host, err := canonicalSSHDestinationHost(destination)
	if err != nil {
		return validatedKnownHostsFile{}, err
	}
	if port < 1 || port > 65535 {
		return validatedKnownHostsFile{}, fmt.Errorf("SSH port must be between 1 and 65535")
	}
	content, err := tunnel.ReadProtectedRegularFile(prepared.path, maxKnownHostsBytes)
	if err != nil {
		return validatedKnownHostsFile{}, fmt.Errorf("validate known-hosts file: %w", err)
	}
	expectedHost := host
	if port != 22 {
		expectedHost = "[" + host + "]:" + strconv.Itoa(port)
	}
	allowedKeyTypes := map[string]bool{
		"ssh-ed25519": true, "ssh-rsa": true,
		"ecdsa-sha2-nistp256": true, "ecdsa-sha2-nistp384": true, "ecdsa-sha2-nistp521": true,
		"sk-ssh-ed25519@openssh.com": true, "sk-ecdsa-sha2-nistp256@openssh.com": true,
	}
	found := false
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	scanner.Buffer(make([]byte, 4096), maxKnownHostsBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return validatedKnownHostsFile{}, fmt.Errorf("known-hosts line %d is malformed", lineNumber)
		}
		hostField, keyType, encodedKey := fields[0], fields[1], fields[2]
		if strings.HasPrefix(hostField, "@") || strings.HasPrefix(hostField, "|") ||
			strings.ContainsAny(hostField, "*,!?") {
			return validatedKnownHostsFile{}, fmt.Errorf("known-hosts line %d uses an unsupported host pattern or marker", lineNumber)
		}
		if !allowedKeyTypes[keyType] {
			return validatedKnownHostsFile{}, fmt.Errorf("known-hosts line %d uses unsupported key type %q", lineNumber, keyType)
		}
		decoded, decodeErr := base64.StdEncoding.DecodeString(encodedKey)
		if decodeErr != nil || len(decoded) == 0 {
			return validatedKnownHostsFile{}, fmt.Errorf("known-hosts line %d contains an invalid base64 key", lineNumber)
		}
		if hostField == expectedHost {
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		return validatedKnownHostsFile{}, fmt.Errorf("scan known-hosts file: %w", err)
	}
	if !found {
		return validatedKnownHostsFile{}, fmt.Errorf("known-hosts file has no exact entry for %q", expectedHost)
	}
	return prepared, nil
}

func prepareSSHKnownHostsFile(path string) (validatedKnownHostsFile, error) {
	if strings.TrimSpace(path) == "" {
		return validatedKnownHostsFile{}, fmt.Errorf("reviewed known-hosts file is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return validatedKnownHostsFile{}, fmt.Errorf("resolve known-hosts file: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return validatedKnownHostsFile{}, fmt.Errorf("resolve known-hosts file: %w", err)
	}
	if !sameKnownHostsPath(abs, resolved, runtime.GOOS) {
		return validatedKnownHostsFile{}, fmt.Errorf("known-hosts file must not traverse symlinks")
	}
	name := filepath.Base(resolved)
	if err := validateSSHKnownHostsName(name); err != nil {
		return validatedKnownHostsFile{}, err
	}
	if err := tunnel.ValidateProtectedParentChain(resolved); err != nil {
		return validatedKnownHostsFile{}, err
	}
	return validatedKnownHostsFile{directory: filepath.Dir(resolved), name: name, path: resolved}, nil
}

func sameKnownHostsPath(left, right, goos string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if goos == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func validateSSHKnownHostsName(name string) error {
	if name == "" {
		return fmt.Errorf("reviewed known-hosts file is required")
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "-") || strings.EqualFold(name, "none") {
		return fmt.Errorf("known-hosts file name is unsafe for SSH")
	}
	for _, character := range name {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '.' && character != '_' && character != '-' {
			return fmt.Errorf("known-hosts file name is unsafe for SSH")
		}
	}
	return nil
}

func canonicalSSHDestinationHost(destination string) (string, error) {
	if destination == "" || strings.HasPrefix(destination, "-") || strings.ContainsAny(destination, "/\\:#?[]") ||
		strings.IndexFunc(destination, func(character rune) bool {
			return unicode.IsControl(character) || unicode.IsSpace(character)
		}) >= 0 {
		return "", fmt.Errorf("invalid SSH destination")
	}
	if strings.Count(destination, "@") > 1 {
		return "", fmt.Errorf("invalid SSH destination")
	}
	host := destination
	if user, value, ok := strings.Cut(destination, "@"); ok {
		if user == "" || value == "" {
			return "", fmt.Errorf("invalid SSH destination")
		}
		host = value
	}
	if host != strings.ToLower(host) || len(host) > 253 || strings.HasPrefix(host, "-") || strings.HasSuffix(host, "-") {
		return "", fmt.Errorf("SSH destination host is not canonical")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("SSH destination host is not canonical")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", fmt.Errorf("SSH destination host is not canonical")
			}
		}
	}
	return host, nil
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
	prepared, err := loadValidatedKnownHostsFile(knownHostsFile, spec.Destination, spec.Port)
	if err != nil {
		return runningTunnel{}, fmt.Errorf("%s SSH known-hosts validation: %w", providerID, err)
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return runningTunnel{}, fmt.Errorf("ssh not found in PATH: %w", err)
	}
	sshPath, err = filepath.Abs(sshPath)
	if err != nil {
		return runningTunnel{}, fmt.Errorf("resolve ssh executable path: %w", err)
	}
	argv, err := sshTunnelArgs(sshPath, spec, prepared.name)
	if err != nil {
		return runningTunnel{}, fmt.Errorf("%s SSH configuration: %w", providerID, err)
	}
	return startTunnelCommandInDirectory(ctx, stderr, providerID, argv, sshTunnelStartupTimeout, prepared.directory)
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
	return startTunnelCommandInDirectory(ctx, stderr, providerID, argv, timeout, "")
}

func startTunnelCommandInDirectory(ctx context.Context, stderr io.Writer, providerID string, argv []string, timeout time.Duration, workingDirectory string) (runningTunnel, error) {
	if len(argv) == 0 {
		return runningTunnel{}, fmt.Errorf("%s command argv is empty", providerID)
	}
	if stderr == nil {
		stderr = io.Discard
	}
	tunnelCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(tunnelCtx, argv[0], argv[1:]...)
	cmd.Dir = workingDirectory
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
