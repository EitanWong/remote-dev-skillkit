package cli

import (
	"bufio"
	"bytes"
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
const providerProcessWaitDelay = 2 * time.Second
const providerProcessCleanupTimeout = 3 * time.Second

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
		return runningTunnel{}, fmt.Errorf("ssh executable not found")
	}
	sshPath, err = filepath.Abs(sshPath)
	if err != nil {
		return runningTunnel{}, fmt.Errorf("resolve ssh executable path failed")
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
		idx := indexASCIIFold(remaining, "https://")
		if idx < 0 {
			return ""
		}
		rest := remaining[idx:]
		end := strings.IndexAny(rest, " \t\n\r|")
		if end < 0 {
			end = len(rest)
		}
		candidate := strings.Trim(strings.TrimRight(rest[:end], "/"), "\"'()[]{}<>,;")
		if canonical, ok := canonicalProviderURL(providerID, candidate); ok {
			return canonical
		}
		remaining = rest[end:]
	}
}

func indexASCIIFold(value, needle string) int {
	if needle == "" {
		return 0
	}
	for start := 0; start+len(needle) <= len(value); start++ {
		matched := true
		for index := range needle {
			if asciiLower(value[start+index]) != asciiLower(needle[index]) {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}

func asciiLower(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}

func canonicalProviderURL(providerID, candidate string) (string, bool) {
	u, err := url.Parse(candidate)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil || u.Port() != "" ||
		u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", false
	}
	if !strings.EqualFold(u.Host, u.Hostname()) {
		return "", false
	}
	if escapedPath := u.EscapedPath(); escapedPath != "" && escapedPath != "/" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if !validDNSHostname(host) {
		return "", false
	}
	allowed := false
	switch providerID {
	case tunnel.ProviderCloudflareQuick:
		allowed = strictSubdomain(host, "trycloudflare.com")
	case tunnel.ProviderLocalhostRun:
		allowed = strictSubdomain(host, "lhr.life") || (strictSubdomain(host, "localhost.run") && host != "admin.localhost.run")
	case tunnel.ProviderPinggy:
		allowed = strictSubdomain(host, "pinggy.link") || strictSubdomain(host, "pinggy-free.link")
	}
	if !allowed {
		return "", false
	}
	return "https://" + host, true
}

func validDNSHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || !isDNSLabelBoundary(label[0]) || !isDNSLabelBoundary(label[len(label)-1]) {
			return false
		}
		for index := 1; index+1 < len(label); index++ {
			character := label[index]
			if !isDNSLabelBoundary(character) && character != '-' {
				return false
			}
		}
	}
	return true
}

func isDNSLabelBoundary(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func strictSubdomain(host, suffix string) bool {
	return host != suffix && strings.HasSuffix(host, "."+suffix)
}

func startCloudflaredQuickTunnel(ctx context.Context, stderr io.Writer, localURL string) (runningTunnel, error) {
	cfPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return runningTunnel{}, fmt.Errorf("cloudflared executable not found")
	}

	started, err := startCloudflaredWithProtocol(ctx, cfPath, stderr, localURL, "http2", 25*time.Second)
	if err == nil {
		return started, nil
	}
	writeTunnelProviderEvent(stderr, tunnel.ProviderCloudflareQuick, "retry", "starting", "", "start-failed")
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

type providerProcess struct {
	cancel     context.CancelFunc
	lifecycle  *processLifecycle
	candidates <-chan string
}

type tunnelProviderEvent struct {
	SchemaVersion string `json:"schema_version"`
	ProviderID    string `json:"provider_id"`
	CandidateID   string `json:"candidate_id,omitempty"`
	Phase         string `json:"phase"`
	Status        string `json:"status"`
	ErrorClass    string `json:"error_class,omitempty"`
}

func startTunnelCommand(ctx context.Context, stderr io.Writer, providerID string, argv []string, timeout time.Duration) (runningTunnel, error) {
	return startTunnelCommandInDirectory(ctx, stderr, providerID, argv, timeout, "")
}

func startTunnelCommandInDirectory(ctx context.Context, stderr io.Writer, providerID string, argv []string, timeout time.Duration, workingDirectory string) (runningTunnel, error) {
	process, err := startProviderProcess(ctx, stderr, providerID, argv, workingDirectory, providerID)
	if err != nil {
		return runningTunnel{}, err
	}
	safeProviderID := safeTunnelProviderID(providerID)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case tunnelURL := <-process.candidates:
		writeTunnelProviderEvent(stderr, safeProviderID, "candidate-assigned", "ready", tunnelURL, "")
		return runningTunnel{URL: tunnelURL, cancel: process.cancel, lifecycle: process.lifecycle}, nil
	case <-process.lifecycle.reaped:
		select {
		case tunnelURL := <-process.candidates:
			writeTunnelProviderEvent(stderr, safeProviderID, "candidate-assigned", "ready", tunnelURL, "")
			return runningTunnel{URL: tunnelURL, cancel: process.cancel, lifecycle: process.lifecycle}, nil
		default:
		}
		process.cancel()
		writeTunnelProviderEvent(stderr, safeProviderID, "startup", "failed", "", "process-exited")
		return runningTunnel{}, fmt.Errorf("%s provider process exited during startup", safeProviderID)
	case <-timer.C:
		errorClass := "timeout"
		if !cancelAndWaitProviderProcess(process, providerProcessCleanupTimeout) {
			errorClass = "reap-timeout"
		}
		writeTunnelProviderEvent(stderr, safeProviderID, "startup", "failed", "", errorClass)
		return runningTunnel{}, fmt.Errorf("%s provider startup timed out", safeProviderID)
	case <-ctx.Done():
		if cancelAndWaitProviderProcess(process, providerProcessCleanupTimeout) {
			writeTunnelProviderEvent(stderr, safeProviderID, "startup", "stopped", "", "canceled")
		} else {
			writeTunnelProviderEvent(stderr, safeProviderID, "startup", "failed", "", "reap-timeout")
		}
		return runningTunnel{}, ctx.Err()
	}
}

func startProviderProcess(ctx context.Context, log io.Writer, providerID string, argv []string, workingDirectory, discoverProviderID string) (providerProcess, error) {
	safeProviderID := safeTunnelProviderID(providerID)
	if len(argv) == 0 {
		writeTunnelProviderEvent(log, safeProviderID, "start", "failed", "", "invalid-argv")
		return providerProcess{}, fmt.Errorf("%s provider command argv is empty", safeProviderID)
	}
	if log == nil {
		log = io.Discard
	}
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, argv[0], argv[1:]...)
	cmd.Dir = workingDirectory
	candidates := make(chan string, 1)
	var candidateOnce sync.Once
	onCandidate := func(candidate string) {
		candidateOnce.Do(func() {
			candidates <- candidate
		})
	}
	stdoutSink := &providerOutputSink{providerID: discoverProviderID, onCandidate: onCandidate}
	stderrSink := &providerOutputSink{providerID: discoverProviderID, onCandidate: onCandidate}
	cmd.Stdout = stdoutSink
	cmd.Stderr = stderrSink
	cmd.WaitDelay = providerProcessWaitDelay
	writeTunnelProviderEvent(log, safeProviderID, "start", "starting", "", "")
	if err := cmd.Start(); err != nil {
		cancel()
		writeTunnelProviderEvent(log, safeProviderID, "start", "failed", "", "start-failed")
		return providerProcess{}, fmt.Errorf("%s provider process failed to start", safeProviderID)
	}
	lifecycle := newProcessLifecycle(func() error {
		err := cmd.Wait()
		stdoutSink.Finalize()
		stderrSink.Finalize()
		return err
	})
	return providerProcess{cancel: cancel, lifecycle: lifecycle, candidates: candidates}, nil
}

type providerOutputSink struct {
	mu          sync.Mutex
	providerID  string
	onCandidate func(string)
	carry       []byte
	found       bool
}

func (s *providerOutputSink) Write(data []byte) (int, error) {
	const (
		chunkLimit = 32 << 10
	)
	originalLength := len(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.found || s.providerID == "" || s.onCandidate == nil {
		return originalLength, nil
	}
	for len(data) > 0 && !s.found {
		chunkLength := min(len(data), chunkLimit)
		s.consumeLocked(data[:chunkLength])
		data = data[chunkLength:]
	}
	return originalLength, nil
}

func (s *providerOutputSink) Finalize() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.found || s.providerID == "" || s.onCandidate == nil || len(s.carry) == 0 {
		return
	}
	candidate := providerURLFromLine(s.providerID, string(s.carry))
	s.carry = nil
	if candidate != "" {
		s.found = true
		s.onCandidate(candidate)
	}
}

func (s *providerOutputSink) consumeLocked(chunk []byte) {
	const carryLimit = 4 << 10
	window := make([]byte, 0, len(s.carry)+len(chunk))
	window = append(window, s.carry...)
	window = append(window, chunk...)
	lastDelimiter := bytes.LastIndexAny(window, " \t\n\r|")
	if lastDelimiter < 0 {
		s.carry = retainProviderOutputTail(s.carry, window, carryLimit)
		return
	}
	completed := window[:lastDelimiter+1]
	s.carry = retainProviderOutputTail(s.carry, window[lastDelimiter+1:], carryLimit)
	if candidate := providerURLFromLine(s.providerID, string(completed)); candidate != "" {
		s.found = true
		s.onCandidate(candidate)
	}
}

func retainProviderOutputTail(destination, value []byte, limit int) []byte {
	if len(value) > limit {
		value = value[len(value)-limit:]
	}
	return append(destination[:0], value...)
}

func writeTunnelProviderEvent(out io.Writer, providerID, phase, status, candidateURL, errorClass string) {
	if out == nil {
		return
	}
	providerID = safeTunnelProviderID(providerID)
	event := tunnelProviderEvent{
		SchemaVersion: "rdev.tunnel-provider-event.v1",
		ProviderID:    providerID,
		Phase:         safeTunnelProviderPhase(phase),
		Status:        safeTunnelProviderStatus(status),
		ErrorClass:    safeTunnelProviderErrorClass(errorClass),
	}
	if candidateURL != "" {
		event.CandidateID = tunnel.CandidateID(providerID, candidateURL)
	}
	content, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = io.WriteString(out, "rdev tunnel provider event: "+string(content)+"\n")
}

func cancelAndWaitProviderProcess(process providerProcess, timeout time.Duration) bool {
	if process.cancel != nil {
		process.cancel()
	}
	if process.lifecycle == nil || process.lifecycle.reaped == nil {
		return false
	}
	if timeout <= 0 {
		select {
		case <-process.lifecycle.reaped:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-process.lifecycle.reaped:
		return true
	case <-timer.C:
		return false
	}
}

func safeTunnelProviderID(value string) string {
	if value == "" {
		return "unknown"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return "unknown"
		}
	}
	return value
}

func safeTunnelProviderPhase(value string) string {
	switch value {
	case "start", "retry", "configuration", "candidate-assigned", "startup", "stop":
		return value
	default:
		return "unknown"
	}
}

func safeTunnelProviderStatus(value string) string {
	switch value {
	case "starting", "ready", "failed", "stopped":
		return value
	default:
		return "unknown"
	}
}

func safeTunnelProviderErrorClass(value string) string {
	switch value {
	case "", "invalid-argv", "start-failed", "process-exited", "timeout", "canceled",
		"reap-timeout", "executable-not-found", "invalid-config":
		return value
	default:
		return "failed"
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
