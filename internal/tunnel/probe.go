package tunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	maxProbeBody             = 256 << 10
	bootstrapScriptMarker    = "$ErrorActionPreference = 'Stop'"
	BootstrapProbeMarker     = "rdev-bootstrap-probe-v1"
	BootstrapProbePowerShell = `$ErrorActionPreference = 'Stop'
# rdev-bootstrap-probe-v1
Write-Output 'rdev-bootstrap-probe-v1'
`
)

type probeResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type probeOptions struct {
	Resolver    probeResolver
	DialContext func(context.Context, string, string) (net.Conn, error)
	TLSConfig   *tls.Config
	Timeout     time.Duration
	stages      *probeStages
}

type probeStages struct {
	mu            sync.Mutex
	dns, tcp, tls bool
}

func ProbeGatewayHealth(ctx context.Context, client *http.Client, candidate Candidate, expectedInstance string) (ProbeEvidence, error) {
	return probeGatewayHealthWithOptions(ctx, candidate, expectedInstance, probeOptionsFromClient(client))
}

func ProbeBootstrapAsset(ctx context.Context, client *http.Client, candidate Candidate, ticketCode, expectedInstance string) (ProbeEvidence, error) {
	return probeBootstrapAssetWithOptions(ctx, candidate, ticketCode, expectedInstance, probeOptionsFromClient(client))
}

func ProbeBootstrapTemplate(ctx context.Context, client *http.Client, candidate Candidate, expectedInstance string) (ProbeEvidence, error) {
	return probeBootstrapTemplateWithOptions(ctx, candidate, expectedInstance, probeOptionsFromClient(client))
}

func probeGatewayHealthWithOptions(ctx context.Context, candidate Candidate, expectedInstance string, options probeOptions) (ProbeEvidence, error) {
	started := time.Now()
	base, err := validatePublicCandidate(candidate)
	if err != nil {
		return ProbeEvidence{}, err
	}
	healthURL := *base
	healthURL.Path = path.Join(base.Path, "healthz")
	healthURL.RawPath = ""
	healthURL.RawQuery = ""
	healthURL.Fragment = ""
	stages := &probeStages{}
	body, marker, err := probeResponse(ctx, newProbeHTTPClient(nil, optionsWithStages(options, stages)), healthURL.String(), expectedInstance, false, stages)
	evidence := stages.evidence(time.Since(started))
	if err != nil {
		return evidence, err
	}
	_ = body
	evidence.HealthOK = true
	evidence.InstanceMarker = marker
	return evidence, nil
}

func probeBootstrapAssetWithOptions(ctx context.Context, candidate Candidate, ticketCode, expectedInstance string, options probeOptions) (ProbeEvidence, error) {
	if strings.TrimSpace(ticketCode) == "" {
		return ProbeEvidence{}, errors.New("ticket code is required")
	}
	evidence, err := probeGatewayHealthWithOptions(ctx, candidate, expectedInstance, options)
	if err != nil {
		return evidence, err
	}
	base, err := validatePublicCandidate(candidate)
	if err != nil {
		return evidence, err
	}
	assetURL := *base
	assetURL.Path = strings.TrimRight(base.Path, "/") + "/join/" + ticketCode + "/bootstrap.ps1"
	assetURL.RawPath = strings.TrimRight(base.EscapedPath(), "/") + "/join/" + url.PathEscape(ticketCode) + "/bootstrap.ps1"
	assetURL.RawQuery = ""
	assetURL.Fragment = ""
	body, _, err := probeResponse(ctx, newProbeHTTPClient(nil, options), assetURL.String(), expectedInstance, true, nil)
	if err != nil {
		return evidence, err
	}
	if !bytes.Contains(body, []byte(bootstrapScriptMarker)) {
		return evidence, errors.New("bootstrap script marker missing")
	}
	evidence.BootstrapOK = true
	evidence.SmallAssetOK = true
	return evidence, nil
}

func probeBootstrapTemplateWithOptions(ctx context.Context, candidate Candidate, expectedInstance string, options probeOptions) (ProbeEvidence, error) {
	started := time.Now()
	base, err := validatePublicCandidate(candidate)
	if err != nil {
		return ProbeEvidence{}, err
	}
	probeURL := *base
	probeURL.Path = strings.TrimRight(base.Path, "/") + "/v1/support-session/bootstrap-probe.ps1"
	probeURL.RawPath = ""
	probeURL.RawQuery = ""
	probeURL.Fragment = ""
	stages := &probeStages{}
	body, marker, err := probeResponse(ctx, newProbeHTTPClient(nil, optionsWithStages(options, stages)), probeURL.String(), expectedInstance, true, stages)
	evidence := stages.evidence(time.Since(started))
	if err != nil {
		return evidence, err
	}
	if expectedInstance == "" || marker != expectedInstance {
		return evidence, errors.New("gateway instance marker mismatch")
	}
	if !bytes.Equal(body, []byte(BootstrapProbePowerShell)) {
		return evidence, errors.New("bootstrap probe template mismatch")
	}
	evidence.HealthOK = true
	evidence.InstanceMarker = marker
	evidence.BootstrapOK = true
	evidence.SmallAssetOK = true
	return evidence, nil
}

func probeOptionsFromClient(client *http.Client) probeOptions {
	options := probeOptions{}
	if client != nil {
		options.Timeout = client.Timeout
	}
	return options
}

func optionsWithStages(options probeOptions, stages *probeStages) probeOptions {
	options.stages = stages
	return options
}

func newProbeHTTPClient(_ *http.Client, options probeOptions) *http.Client {
	timeout := options.Timeout
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if options.TLSConfig != nil {
		tlsConfig = options.TLSConfig.Clone()
		if tlsConfig.MinVersion < tls.VersionTLS12 {
			tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	dialContext := secureProbeDial(options, options.stages)
	return &http.Client{
		Timeout: timeout,
		Jar:     nil,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         dialContext,
			TLSClientConfig:     tlsConfig,
			TLSHandshakeTimeout: 5 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect rejected")
		},
	}
}

func secureProbeDial(options probeOptions, stages *probeStages) func(context.Context, string, string) (net.Conn, error) {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	baseDial := options.DialContext
	if baseDial == nil {
		baseDial = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("parse probe address: %w", err)
		}
		ips, err := resolvePublicIPs(ctx, resolver, host)
		if err != nil {
			return nil, err
		}
		if stages != nil {
			stages.setDNS()
		}
		var dialErrs []error
		for _, ip := range ips {
			conn, dialErr := baseDial(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				if stages != nil {
					stages.setTCP()
				}
				return conn, nil
			}
			dialErrs = append(dialErrs, dialErr)
		}
		return nil, fmt.Errorf("connect public candidate: %w", errors.Join(dialErrs...))
	}
}

func resolvePublicIPs(ctx context.Context, resolver probeResolver, hostname string) ([]netip.Addr, error) {
	hostname = strings.TrimSuffix(strings.TrimSpace(hostname), ".")
	if literal, err := netip.ParseAddr(hostname); err == nil {
		literal = literal.Unmap()
		if err := validatePublicIP(literal); err != nil {
			return nil, err
		}
		return []netip.Addr{literal}, nil
	}
	ips, err := resolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve public candidate: %w", err)
	}
	if len(ips) == 0 {
		return nil, errors.New("public candidate resolved no addresses")
	}
	validated := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		ip = ip.Unmap()
		if err := validatePublicIP(ip); err != nil {
			return nil, err
		}
		validated = append(validated, ip)
	}
	return validated, nil
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"), netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("ff00::/8"),
}

func validatePublicIP(ip netip.Addr) error {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
		return errors.New("public candidate resolved a non-public address")
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(ip) {
			return errors.New("public candidate resolved a non-public address")
		}
	}
	return nil
}

func validatePublicCandidate(candidate Candidate) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(candidate.URL))
	if err != nil || strings.TrimSpace(candidate.ProviderID) == "" || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Port() != "" {
		return nil, errors.New("public candidate must include a provider ID and an HTTPS URL without userinfo or an explicit port")
	}
	parsed.Scheme = "https"
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return nil, errors.New("public candidate must not use a private or local address")
	}
	if ip, err := netip.ParseAddr(hostname); err == nil {
		if err := validatePublicIP(ip.Unmap()); err != nil {
			return nil, err
		}
	} else if !validPublicHostname(hostname) {
		return nil, errors.New("public candidate must use a valid public hostname or address")
	}
	return parsed, nil
}

func ValidatePublicCandidate(candidate Candidate) error {
	_, err := validatePublicCandidate(candidate)
	return err
}

func ValidateAvailabilitySet(set AvailabilitySet) error {
	if set.SchemaVersion != AvailabilitySchemaVersion {
		return errors.New("unsupported tunnel availability schema")
	}
	if !supportedRegion(set.Region) {
		return errors.New("unsupported tunnel availability region")
	}
	for _, candidate := range set.Candidates {
		if err := ValidatePublicCandidate(candidate); err != nil {
			return err
		}
	}
	return nil
}

func validPublicHostname(hostname string) bool {
	if len(hostname) > 253 || !strings.Contains(hostname, ".") {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return false
			}
		}
	}
	return true
}

func probeResponse(ctx context.Context, client *http.Client, rawURL, expectedInstance string, bootstrap bool, stages *probeStages) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build probe request: %w", err)
	}
	if stages != nil {
		trace := &httptrace.ClientTrace{TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				stages.setTLS()
			}
		}}
		request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("probe request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("probe returned HTTP %d", response.StatusCode)
	}
	marker := response.Header.Get("X-Rdev-Gateway-Instance")
	if marker == "" {
		return nil, "", errors.New("gateway instance marker missing")
	}
	if expectedInstance != "" && marker != expectedInstance {
		return nil, "", errors.New("gateway instance marker mismatch")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProbeBody+1))
	if err != nil {
		return nil, "", fmt.Errorf("read probe response: %w", err)
	}
	if len(body) > maxProbeBody {
		return nil, "", errors.New("probe response exceeds 256 KiB")
	}
	if bootstrap {
		if len(bytes.TrimSpace(body)) == 0 {
			return nil, "", errors.New("bootstrap response is empty")
		}
		if !allowedBootstrapContentType(response.Header.Get("Content-Type")) {
			return nil, "", errors.New("bootstrap response has an unexpected content type")
		}
		if looksLikeHTMLInterstitial(response.Header.Get("Content-Type"), body) {
			return nil, "", errors.New("bootstrap response looks like an HTML interstitial")
		}
	}
	return body, marker, nil
}

func allowedBootstrapContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "text/plain", "text/x-powershell", "application/x-powershell", "application/octet-stream":
		return true
	default:
		return false
	}
}

func looksLikeHTMLInterstitial(contentType string, body []byte) bool {
	prefix := bytes.ToLower(bytes.TrimSpace(body))
	return strings.Contains(strings.ToLower(contentType), "text/html") || bytes.HasPrefix(prefix, []byte("<!doctype html")) || bytes.HasPrefix(prefix, []byte("<html"))
}

func (s *probeStages) setDNS() { s.mu.Lock(); s.dns = true; s.mu.Unlock() }
func (s *probeStages) setTCP() { s.mu.Lock(); s.tcp = true; s.mu.Unlock() }
func (s *probeStages) setTLS() { s.mu.Lock(); s.tls = true; s.mu.Unlock() }
func (s *probeStages) evidence(latency time.Duration) ProbeEvidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ProbeEvidence{DNSOK: s.dns, TCPConnectOK: s.tcp, TLSOK: s.tls, Latency: latency}
}
