package tunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"
)

const maxProbeBody = 256 << 10

func ProbeGatewayHealth(ctx context.Context, client *http.Client, candidate Candidate, expectedInstance string) (ProbeEvidence, error) {
	started := time.Now()
	base, err := validatePublicCandidate(candidate)
	if err != nil {
		return ProbeEvidence{}, err
	}
	probeClient := safeProbeClient(client)
	healthURL := *base
	healthURL.Path = path.Join(base.Path, "healthz")
	healthURL.RawQuery = ""
	healthURL.Fragment = ""
	body, marker, err := probeResponse(ctx, probeClient, healthURL.String(), expectedInstance, false)
	evidence := ProbeEvidence{Latency: time.Since(started)}
	if err != nil {
		return evidence, err
	}
	_ = body
	evidence.DNSOK = true
	evidence.TCPConnectOK = true
	evidence.TLSOK = true
	evidence.HealthOK = true
	evidence.InstanceMarker = marker
	return evidence, nil
}

func ProbeBootstrapAsset(ctx context.Context, client *http.Client, candidate Candidate, ticketCode, expectedInstance string) (ProbeEvidence, error) {
	if strings.TrimSpace(ticketCode) == "" {
		return ProbeEvidence{}, errors.New("ticket code is required")
	}
	evidence, err := ProbeGatewayHealth(ctx, client, candidate, expectedInstance)
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
	if _, _, err := probeResponse(ctx, safeProbeClient(client), assetURL.String(), expectedInstance, true); err != nil {
		return evidence, err
	}
	evidence.BootstrapOK = true
	evidence.SmallAssetOK = true
	return evidence, nil
}

func safeProbeClient(client *http.Client) *http.Client {
	copyClient := &http.Client{Timeout: 10 * time.Second}
	if client != nil {
		*copyClient = *client
		if copyClient.Timeout <= 0 || copyClient.Timeout > 10*time.Second {
			copyClient.Timeout = 10 * time.Second
		}
	}
	if copyClient.Transport == nil {
		copyClient.Transport = &http.Transport{
			Proxy:               nil,
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 5 * time.Second,
		}
	}
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("redirect rejected")
	}
	return copyClient
}

func validatePublicCandidate(candidate Candidate) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(candidate.URL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("public candidate must be an HTTPS URL without userinfo")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return nil, errors.New("public candidate must not use a private or local address")
	}
	if ip, err := netip.ParseAddr(parsed.Hostname()); err == nil {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return nil, errors.New("public candidate must not use a private or local address")
		}
	}
	return parsed, nil
}

func probeResponse(ctx context.Context, client *http.Client, rawURL, expectedInstance string, bootstrap bool) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build probe request: %w", err)
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
	reader := io.LimitReader(response.Body, maxProbeBody+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", fmt.Errorf("read probe response: %w", err)
	}
	if len(body) > maxProbeBody {
		return nil, "", errors.New("probe response exceeds 256 KiB")
	}
	if bootstrap && looksLikeHTMLInterstitial(response.Header.Get("Content-Type"), body) {
		return nil, "", errors.New("bootstrap response looks like an HTML interstitial")
	}
	return body, marker, nil
}

func looksLikeHTMLInterstitial(contentType string, body []byte) bool {
	prefix := bytes.ToLower(bytes.TrimSpace(body))
	return strings.Contains(strings.ToLower(contentType), "text/html") || bytes.HasPrefix(prefix, []byte("<!doctype html")) || bytes.HasPrefix(prefix, []byte("<html"))
}
