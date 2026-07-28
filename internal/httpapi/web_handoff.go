package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
)

const (
	defaultWebHandoffTTL        = 30 * time.Minute
	minimumWebHandoffTTL        = time.Minute
	maximumWebHandoffTTL        = 24 * time.Hour
	defaultArtifactTicketTTL    = 15 * time.Minute
	maxWebHandoffRequestBytes   = 4 << 10
	webHandoffArtifactTicketKey = "X-Rdev-Handoff-Ticket"
)

// WebHandoffAsset is an immutable host executable that a claimed browser
// handoff may deliver. It is loaded at gateway startup rather than embedded in
// source or fetched from another provider.
type WebHandoffAsset struct {
	Filename string
	Content  []byte
	SHA256   string
}

// WebHandoffOptions enables browser handoffs for a public HTTPS gateway.
// An empty WindowsAMD64 asset keeps the landing routes available but refuses
// creation so operators cannot issue a link that has no host executable.
type WebHandoffOptions struct {
	PublicBaseURL string
	WindowsAMD64  WebHandoffAsset
}

type webHandoffConfig struct {
	publicBaseURL string
	windowsAMD64  WebHandoffAsset
}

func NewWindowsAMD64WebHandoffAsset(filename string, content []byte) (WebHandoffAsset, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" || len(content) == 0 {
		return WebHandoffAsset{}, fmt.Errorf("windows-amd64 host binary is required")
	}
	copied := append([]byte(nil), content...)
	sum := sha256.Sum256(copied)
	return WebHandoffAsset{
		Filename: filename,
		Content:  copied,
		SHA256:   hex.EncodeToString(sum[:]),
	}, nil
}

func LoadWindowsAMD64WebHandoffAsset(path string) (WebHandoffAsset, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return WebHandoffAsset{}, fmt.Errorf("windows-amd64 host binary path is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return WebHandoffAsset{}, fmt.Errorf("read windows-amd64 host binary: %w", err)
	}
	return NewWindowsAMD64WebHandoffAsset(filepath.Base(path), content)
}

// WithWebHandoff returns a copy of the server that can issue short-lived web
// handoffs through the configured public HTTPS gateway.
func (s Server) WithWebHandoff(options WebHandoffOptions) (Server, error) {
	baseURL, err := normalizeWebHandoffBaseURL(options.PublicBaseURL)
	if err != nil {
		return Server{}, err
	}
	asset := options.WindowsAMD64
	if len(asset.Content) > 0 {
		asset, err = NewWindowsAMD64WebHandoffAsset(asset.Filename, asset.Content)
		if err != nil {
			return Server{}, err
		}
	}
	s.webHandoff = webHandoffConfig{publicBaseURL: baseURL, windowsAMD64: asset}
	return s, nil
}

func normalizeWebHandoffBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("web handoff public base URL must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("web handoff public base URL must not include a path")
	}
	parsed.Path = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (s Server) createWebHandoff(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "operator role is required", false))
		return
	}
	if len(s.webHandoff.windowsAMD64.Content) == 0 {
		writeError(w, http.StatusServiceUnavailable, "windows-amd64 web handoff asset is not configured")
		return
	}

	var request struct {
		Platform    string `json:"platform"`
		ExpiresInMS int    `json:"expires_in_ms"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebHandoffRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid web handoff request")
		return
	}
	if strings.TrimSpace(request.Platform) == "" {
		request.Platform = gateway.WebHandoffPlatformWindowsAMD64
	}
	if request.Platform != gateway.WebHandoffPlatformWindowsAMD64 {
		writeError(w, http.StatusBadRequest, "unsupported web handoff platform")
		return
	}
	ttl, err := webHandoffTTL(request.ExpiresInMS)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	selectedGateway, err := normalizeWebHandoffBaseURL(session.SelectedGatewayURL)
	if err != nil || selectedGateway != s.webHandoff.publicBaseURL {
		writeError(w, http.StatusBadRequest, "session selected gateway must match the configured web handoff gateway")
		return
	}

	handoff, proof, err := s.Gateway.CreateWebHandoff(gateway.WebHandoffSpec{
		SessionID: session.ID,
		Platform:  request.Platform,
		ExpiresAt: s.Gateway.Now().Add(ttl),
	})
	if err != nil {
		writeWebHandoffError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	link := s.webHandoff.publicBaseURL + "/connect/" + url.PathEscape(handoff.ID) + "#" + proof
	writeJSON(w, http.StatusCreated, map[string]any{
		"handoff": map[string]any{
			"schema_version":      handoff.SchemaVersion,
			"id":                  handoff.ID,
			"session_id":          handoff.SessionID,
			"platform":            handoff.Platform,
			"url":                 link,
			"expires_at":          handoff.ExpiresAt,
			"artifact_filename":   s.webHandoff.windowsAMD64.Filename,
			"artifact_sha256":     s.webHandoff.windowsAMD64.SHA256,
			"artifact_size_bytes": len(s.webHandoff.windowsAMD64.Content),
		},
	})
}

func webHandoffTTL(expiresInMS int) (time.Duration, error) {
	if expiresInMS == 0 {
		return defaultWebHandoffTTL, nil
	}
	if expiresInMS < int(minimumWebHandoffTTL/time.Millisecond) || expiresInMS > int(maximumWebHandoffTTL/time.Millisecond) {
		return 0, fmt.Errorf("expires_in_ms must be between %d and %d", minimumWebHandoffTTL/time.Millisecond, maximumWebHandoffTTL/time.Millisecond)
	}
	return time.Duration(expiresInMS) * time.Millisecond, nil
}

func (s Server) webHandoffRoute(w http.ResponseWriter, r *http.Request) {
	id, action, ok := splitWebHandoffPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown web handoff endpoint")
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "":
		s.renderWebHandoffPage(w, id)
	case r.Method == http.MethodPost && action == "claim":
		s.claimWebHandoff(w, r, id)
	case r.Method == http.MethodGet && action == "rdev-host.exe":
		s.serveWebHandoffWindowsHost(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "unknown web handoff endpoint")
	}
}

func splitWebHandoffPath(path string) (id string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/connect/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		return parts[0], "", true
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		return parts[0], parts[1], true
	default:
		return "", "", false
	}
}

func (s Server) renderWebHandoffPage(w http.ResponseWriter, id string) {
	writeWebHandoffSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	claimPath, _ := json.Marshal("/connect/" + url.PathEscape(id) + "/claim")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Remote Dev Host Connection</title>
<style>body{font-family:system-ui,sans-serif;max-width:42rem;margin:4rem auto;padding:0 1.25rem;color:#172033}button{padding:.7rem 1rem;font:inherit}#status{min-height:1.5rem}</style></head>
<body><main><h1>Connect this Windows host</h1><p>This page prepares a one-time PowerShell bootstrap. Downloading it does not run it; review and start it in a visible PowerShell window.</p><button id="download" type="button">Download connection script</button><p id="status" role="status"></p></main>
<script>
(() => {
  const claimPath = %s;
  const proof = window.location.hash.slice(1);
  const button = document.getElementById('download');
  const status = document.getElementById('status');
  history.replaceState(null, '', window.location.pathname);
  if (!proof) { button.disabled = true; status.textContent = 'This handoff link is missing its confirmation fragment.'; return; }
  button.addEventListener('click', async () => {
    button.disabled = true;
    status.textContent = 'Preparing the connection script…';
    try {
      const response = await fetch(claimPath, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({proof})});
      if (!response.ok) { throw new Error('The handoff is expired, already used, or unavailable.'); }
      const payload = await response.json();
      const blob = new Blob([payload.bootstrap], {type:'text/plain;charset=utf-8'});
      const link = document.createElement('a');
      link.href = URL.createObjectURL(blob);
      link.download = 'Connect-Rdev.ps1';
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(link.href);
      status.textContent = 'Downloaded. Run Connect-Rdev.ps1 from the extracted location.';
    } catch (_) {
      status.textContent = 'The handoff could not be claimed. Ask the operator for a fresh link.';
      button.disabled = false;
    }
  });
})();
</script></body></html>`, string(claimPath))
}

func (s Server) claimWebHandoff(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Proof string `json:"proof"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebHandoffRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid web handoff claim")
		return
	}
	handoff, ticket, err := s.Gateway.ClaimWebHandoff(id, request.Proof, defaultArtifactTicketTTL)
	if err != nil {
		writeWebHandoffError(w, err)
		return
	}
	session, err := s.Gateway.Session(handoff.SessionID)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	selectedGateway, normalizeErr := normalizeWebHandoffBaseURL(session.SelectedGatewayURL)
	if normalizeErr != nil || selectedGateway != s.webHandoff.publicBaseURL {
		writeError(w, http.StatusBadRequest, "session selected gateway must match the configured web handoff gateway")
		return
	}
	if !s.persistState(w) {
		return
	}
	writeWebHandoffSecurityHeaders(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"bootstrap":  s.webHandoffBootstrap(handoff, session.JoinCode, ticket),
		"expires_at": handoff.ArtifactTicketExpiresAt,
	})
}

func (s Server) webHandoffBootstrap(handoff gateway.WebHandoff, joinCode, ticket string) string {
	assetURL := s.webHandoff.publicBaseURL + "/connect/" + url.PathEscape(handoff.ID) + "/rdev-host.exe"
	return fmt.Sprintf(`# Remote Dev Skillkit managed-host bootstrap.
# This script runs visibly in the current PowerShell window. It does not create
# a service, scheduled task, firewall rule, or execution-policy bypass.
$ErrorActionPreference = 'Stop'
$gateway = %s
$joinCode = %s
$artifactUri = %s
$artifactTicket = %s
$expectedSHA256 = %s
$stateRoot = Join-Path $env:LOCALAPPDATA 'RemoteDevSkillkit\managed-host'
$hostBinary = Join-Path $stateRoot 'rdev-host.exe'
$tempBinary = Join-Path $stateRoot 'rdev-host.download.exe'
$identityStore = Join-Path $stateRoot 'identity.json'
$trustStore = Join-Path $stateRoot 'trust.json'
$lockStore = Join-Path $stateRoot 'workspace-locks'

New-Item -ItemType Directory -Force -Path $stateRoot, $lockStore | Out-Null
Invoke-WebRequest -Uri $artifactUri -Headers @{ %s = $artifactTicket } -OutFile $tempBinary
$actualSHA256 = (Get-FileHash -LiteralPath $tempBinary -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSHA256 -ne $expectedSHA256) { throw 'rdev-host.exe SHA-256 verification failed.' }
Move-Item -LiteralPath $tempBinary -Destination $hostBinary -Force

Write-Host 'Starting managed Remote Dev Skillkit connector in this visible PowerShell window.'
& $hostBinary serve --mode managed --gateway $gateway --join-code $joinCode --once=false --max-tasks 0 --transport long-poll --identity-store $identityStore --trust-store $trustStore --workspace-lock-store $lockStore
exit $LASTEXITCODE
`, powershellLiteral(s.webHandoff.publicBaseURL), powershellLiteral(joinCode), powershellLiteral(assetURL), powershellLiteral(ticket), powershellLiteral(s.webHandoff.windowsAMD64.SHA256), powershellLiteral(webHandoffArtifactTicketKey))
}

func powershellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (s Server) serveWebHandoffWindowsHost(w http.ResponseWriter, r *http.Request, id string) {
	if len(s.webHandoff.windowsAMD64.Content) == 0 {
		writeError(w, http.StatusServiceUnavailable, "windows-amd64 web handoff asset is not configured")
		return
	}
	if _, err := s.Gateway.ValidateWebHandoffArtifactTicket(id, r.Header.Get(webHandoffArtifactTicketKey)); err != nil {
		writeWebHandoffError(w, err)
		return
	}
	writeWebHandoffSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/vnd.microsoft.portable-executable")
	w.Header().Set("Content-Disposition", "attachment; filename=\"rdev-host.exe\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(s.webHandoff.windowsAMD64.Content)))
	_, _ = w.Write(s.webHandoff.windowsAMD64.Content)
}

func writeWebHandoffSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; connect-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

func writeWebHandoffError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gateway.ErrWebHandoffNotFound):
		writeError(w, http.StatusNotFound, "web handoff not found")
	case errors.Is(err, gateway.ErrWebHandoffExpired), errors.Is(err, gateway.ErrWebHandoffClaimed):
		writeError(w, http.StatusGone, "web handoff has expired or was already claimed")
	case errors.Is(err, gateway.ErrWebHandoffInvalidProof), errors.Is(err, gateway.ErrWebHandoffInvalidTicket):
		writeError(w, http.StatusForbidden, "web handoff authorization failed")
	case errors.Is(err, gateway.ErrWebHandoffSessionInvalid):
		writeError(w, http.StatusConflict, "session is not eligible for a web handoff")
	default:
		writeError(w, http.StatusBadRequest, "web handoff request failed")
	}
}
