package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/evidence"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/wsproto"
)

type Server struct {
	Gateway      *gateway.MemoryGateway
	StatePath    string
	StateStore   gateway.StateStore
	OperatorAuth *operatorauth.Authorizer
	Assets       AssetConfig
	stateMu      *sync.Mutex
}

type AssetConfig struct {
	RdevWindowsAMD64Path string
	RdevDarwinARM64Path  string
	RdevDarwinAMD64Path  string
	RdevLinuxAMD64Path   string
	RdevLinuxARM64Path   string
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return Server{Gateway: gw, stateMu: &sync.Mutex{}}
}

func NewServerWithState(gw *gateway.MemoryGateway, statePath string) Server {
	if strings.TrimSpace(statePath) == "" {
		return NewServer(gw)
	}
	store, _ := gateway.NewFileStateStore(statePath)
	server := NewServerWithStateStore(gw, store)
	server.StatePath = statePath
	return server
}

func NewServerWithStateStore(gw *gateway.MemoryGateway, store gateway.StateStore) Server {
	return Server{Gateway: gw, StateStore: store, stateMu: &sync.Mutex{}}
}

func NewServerWithOperatorAuth(gw *gateway.MemoryGateway, statePath string, auth *operatorauth.Authorizer) Server {
	if strings.TrimSpace(statePath) == "" {
		return NewServerWithOperatorAuthAndStateStore(gw, nil, auth)
	}
	store, _ := gateway.NewFileStateStore(statePath)
	server := NewServerWithOperatorAuthAndStateStore(gw, store, auth)
	server.StatePath = statePath
	return server
}

func NewServerWithOperatorAuthAndStateStore(gw *gateway.MemoryGateway, store gateway.StateStore, auth *operatorauth.Authorizer) Server {
	return Server{Gateway: gw, StateStore: store, OperatorAuth: auth, stateMu: &sync.Mutex{}}
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/trust", s.trust)
	mux.HandleFunc("GET /v1/trust-bundle", s.getTrustBundle)
	mux.HandleFunc("GET /v1/enrollment/revocations", s.getEnrollmentRevocations)
	mux.HandleFunc("POST /v1/enrollment/certificates", s.issueEnrollmentCertificate)
	mux.HandleFunc("POST /v1/enrollment/certificates/renew", s.renewEnrollmentCertificate)
	mux.HandleFunc("POST /v1/trust-bundle", s.updateTrustBundle)
	mux.HandleFunc("POST /v1/tickets", s.createTicket)
	mux.HandleFunc("GET /v1/tickets/", s.ticketSubresource)
	mux.HandleFunc("GET /join/", s.join)
	mux.HandleFunc("GET /assets/", s.asset)
	mux.HandleFunc("GET /v1/support-session/status", s.supportSessionStatus)
	mux.HandleFunc("GET /v1/hosts", s.listHosts)
	mux.HandleFunc("POST /v1/hosts/register", s.registerHost)
	mux.HandleFunc("GET /v1/hosts/", s.hostSubresource)
	mux.HandleFunc("GET /v1/ws/hosts/", s.hostWebSocket)
	mux.HandleFunc("POST /v1/hosts/", s.hostAction)
	mux.HandleFunc("GET /v1/jobs", s.listJobs)
	mux.HandleFunc("POST /v1/jobs", s.createJob)
	mux.HandleFunc("GET /v1/jobs/", s.getJob)
	mux.HandleFunc("POST /v1/jobs/", s.jobAction)
	mux.HandleFunc("GET /v1/artifacts/", s.getArtifact)
	mux.HandleFunc("GET /v1/audit", s.listAudit)
	return mux
}

func (s Server) hostWebSocket(w http.ResponseWriter, r *http.Request) {
	hostID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/ws/hosts/"), "/")
	if hostID == "" {
		writeError(w, http.StatusNotFound, "unknown websocket host endpoint")
		return
	}
	hostSecret := extractBearerToken(r)
	// Validate host secret before upgrading — the secret may be passed as the
	// "host_secret" query param since WebSocket upgrade headers are limited.
	if !s.Gateway.ValidateHostSecret(hostID, hostSecret) {
		writeError(w, http.StatusUnauthorized, "host authentication required: include Authorization: Bearer <host_secret> or ?host_secret=<token>")
		return
	}
	conn, err := wsproto.Upgrade(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer conn.Close()
	if err := s.Gateway.HeartbeatHost(hostID, hostSecret); err != nil {
		_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
		return
	}
	for {
		if err := s.Gateway.HeartbeatHost(hostID, hostSecret); err != nil {
			_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
			return
		}
		job, ok, err := s.nextJobForHost(r.Context(), hostID, 60*time.Second)
		if err != nil {
			_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
			return
		}
		if !ok {
			if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageNoop, HostID: hostID}); err != nil {
				return
			}
			continue
		}
		if !s.persistStateNoResponse() {
			_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "persist gateway state failed"})
			return
		}
		if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageJob, HostID: hostID, JobID: job.ID, Job: &job}); err != nil {
			return
		}
	responseLoop:
		for {
			if err := s.Gateway.HeartbeatHost(hostID, hostSecret); err != nil {
				_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
				return
			}
			var msg wsproto.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if err := s.Gateway.HeartbeatHost(hostID, hostSecret); err != nil {
				_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
				return
			}
			if msg.HostID == "" {
				msg.HostID = hostID
			}
			if msg.JobID == "" {
				msg.JobID = job.ID
			}
			if msg.HostID != hostID || msg.JobID != job.ID {
				_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "websocket job response host or job mismatch"})
				return
			}
			switch msg.Type {
			case wsproto.MessageComplete:
				if _, _, err := s.Gateway.CompleteJobForHost(hostID, job.ID, msg.ArtifactContent); err != nil {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
					return
				}
				if !s.persistStateNoResponse() {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "persist gateway state failed"})
					return
				}
				if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageComplete, HostID: hostID, JobID: job.ID}); err != nil {
					return
				}
				break responseLoop
			case wsproto.MessageFail:
				if _, _, err := s.Gateway.FailJobForHostWithArtifact(hostID, job.ID, msg.Reason, msg.ArtifactContent); err != nil {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
					return
				}
				if !s.persistStateNoResponse() {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "persist gateway state failed"})
					return
				}
				if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageFail, HostID: hostID, JobID: job.ID}); err != nil {
					return
				}
				break responseLoop
			case wsproto.MessageArtifact:
				if _, _, err := s.Gateway.AppendJobArtifactForHost(hostID, job.ID, msg.ArtifactContent); err != nil {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
					return
				}
				if !s.persistStateNoResponse() {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "persist gateway state failed"})
					return
				}
				if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageArtifact, HostID: hostID, JobID: job.ID}); err != nil {
					return
				}
			default:
				_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "unsupported websocket message type"})
				return
			}
		}
	}
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s Server) trust(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"trust": s.Gateway.TrustBundle()})
}

func (s Server) getTrustBundle(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"trust_bundle": s.Gateway.SignedTrustBundle()})
}

func (s Server) getEnrollmentRevocations(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeEnrollmentIssuer(r) {
		writeError(w, http.StatusUnauthorized, "operator issuer role is required")
		return
	}
	revocations, ok := s.Gateway.EnrollmentRevocations()
	if !ok {
		writeError(w, http.StatusNotFound, "enrollment revocations not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revocations": revocations})
}

func (s Server) issueEnrollmentCertificate(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeEnrollmentIssuer(r) {
		writeError(w, http.StatusUnauthorized, "operator issuer role is required")
		return
	}
	var req struct {
		TicketCode          string   `json:"ticket_code"`
		Name                string   `json:"name"`
		OS                  string   `json:"os"`
		Arch                string   `json:"arch"`
		Capabilities        []string `json:"capabilities"`
		IdentityKeyID       string   `json:"identity_key_id"`
		IdentityPublicKey   string   `json:"identity_public_key"`
		IdentityFingerprint string   `json:"identity_fingerprint"`
		ValidMinutes        int      `json:"valid_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ValidMinutes == 0 {
		req.ValidMinutes = 60
	}
	certificate, err := s.Gateway.IssueEnrollmentCertificate(gateway.EnrollmentCertificateRequest{
		TicketCode:          req.TicketCode,
		Name:                req.Name,
		OS:                  req.OS,
		Arch:                req.Arch,
		Capabilities:        req.Capabilities,
		IdentityKeyID:       req.IdentityKeyID,
		IdentityPublicKey:   req.IdentityPublicKey,
		IdentityFingerprint: req.IdentityFingerprint,
		ValidMinutes:        req.ValidMinutes,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	root, ok := s.Gateway.EnrollmentRoot()
	if !ok {
		writeError(w, http.StatusInternalServerError, "enrollment root not configured")
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"certificate":             certificate,
		"certificate_fingerprint": fingerprint,
		"enrollment_root":         root,
	})
}

func (s Server) renewEnrollmentCertificate(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeEnrollmentIssuer(r) {
		writeError(w, http.StatusUnauthorized, "operator issuer role is required")
		return
	}
	var req struct {
		Certificate  model.HostEnrollmentCertificate `json:"certificate"`
		ValidMinutes int                             `json:"valid_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ValidMinutes == 0 {
		req.ValidMinutes = 60
	}
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(req.Certificate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	certificate, err := s.Gateway.RenewEnrollmentCertificate(gateway.EnrollmentCertificateRenewalRequest{
		Certificate:  req.Certificate,
		ValidMinutes: req.ValidMinutes,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	root, ok := s.Gateway.EnrollmentRoot()
	if !ok {
		writeError(w, http.StatusInternalServerError, "enrollment root not configured")
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"certificate":                      certificate,
		"certificate_fingerprint":          fingerprint,
		"previous_certificate_fingerprint": previousFingerprint,
		"enrollment_root":                  root,
	})
}

func (s Server) authorizeEnrollmentIssuer(r *http.Request) bool {
	return s.authorizeOperator(r, operatorauth.RoleIssuer)
}

func (s Server) updateTrustBundle(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
	var req struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	bundle, err := s.Gateway.UpdateSignedTrustBundle(req.TrustBundle)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trust_bundle": bundle})
}

func (s Server) createTicket(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
	var req struct {
		Mode         model.HostMode    `json:"mode"`
		TTLSeconds   int               `json:"ttl_seconds"`
		Capabilities []string          `json:"capabilities"`
		Reason       string            `json:"reason"`
		AutoApprove  bool              `json:"auto_approve"`
		Metadata     map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Mode == "" {
		req.Mode = model.HostModeAttendedTemporary
	}
	if req.TTLSeconds == 0 {
		req.TTLSeconds = 7200
	}
	if req.Reason == "" {
		req.Reason = "remote support"
	}
	metadata := map[string]string{}
	for key, value := range req.Metadata {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			metadata[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if req.AutoApprove {
		metadata["auto_approve"] = "attended-temporary"
	}
	ticket, err := s.Gateway.CreateTicketWithMetadata(req.Mode, req.TTLSeconds, req.Capabilities, req.Reason, metadata)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistState(w) {
		return
	}
	manifestRoot := manifestRootPublicKey(s.Gateway.ManifestRoot())
	writeJSON(w, http.StatusCreated, map[string]any{
		"ticket":                ticket,
		"joinUrl":               requestBaseURL(r) + "/join/" + ticket.Code,
		"manifestUrl":           requestBaseURL(r) + "/v1/tickets/" + ticket.Code + "/manifest",
		"manifestRootPublicKey": manifestRoot,
	})
}

func (s Server) ticketSubresource(w http.ResponseWriter, r *http.Request) {
	code, resource, ok := splitTicketSubresource(r.URL.Path)
	if !ok || resource != "manifest" {
		writeError(w, http.StatusNotFound, "unknown ticket endpoint")
		return
	}
	baseURL := requestBaseURL(r)
	manifest, err := s.Gateway.JoinManifestWithGatewayCandidates(code, baseURL, baseURL+"/join/"+code, joinManifestGatewayCandidatesFromRequest(r, baseURL))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"manifest":              manifest,
		"manifestRootPublicKey": manifestRootPublicKey(s.Gateway.ManifestRoot()),
	})
}

func (s Server) join(w http.ResponseWriter, r *http.Request) {
	code, resource, ok := splitJoinPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown join endpoint")
		return
	}
	manifestURL := manifestURLForJoinRequest(r, code)
	manifestRoot := manifestRootPublicKey(s.Gateway.ManifestRoot())
	if _, err := s.Gateway.JoinManifest(code, requestBaseURL(r), requestBaseURL(r)+"/join/"+code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch resource {
	case "":
		s.joinPage(w, r, code, manifestURL)
	case "bootstrap.sh":
		s.writeShellBootstrap(w, r, manifestURL, manifestRoot)
	case "bootstrap.ps1":
		s.writePowerShellBootstrap(w, r, manifestURL, manifestRoot)
	default:
		writeError(w, http.StatusNotFound, "unknown join resource")
	}
}

func manifestURLForJoinRequest(r *http.Request, code string) string {
	manifestURL := requestBaseURL(r) + "/v1/tickets/" + code + "/manifest"
	if raw := strings.TrimSpace(r.URL.Query().Get("gateway_url_candidates")); raw != "" {
		values := url.Values{}
		values.Set("gateway_url_candidates", raw)
		manifestURL += "?" + values.Encode()
	}
	return manifestURL
}

func joinManifestGatewayCandidatesFromRequest(r *http.Request, fallbackGatewayURL string) []model.JoinManifestGatewayCandidate {
	raw := strings.TrimSpace(r.URL.Query().Get("gateway_url_candidates"))
	if raw == "" {
		return nil
	}
	var candidates []model.JoinManifestGatewayCandidate
	if err := json.Unmarshal([]byte(raw), &candidates); err != nil {
		return nil
	}
	for i := range candidates {
		candidates[i].URL = strings.TrimRight(strings.TrimSpace(candidates[i].URL), "/")
	}
	if len(candidates) == 0 && strings.TrimSpace(fallbackGatewayURL) != "" {
		return []model.JoinManifestGatewayCandidate{{URL: strings.TrimRight(fallbackGatewayURL, "/"), Kind: "manifest-gateway", Scope: "signed-manifest", Recommended: true}}
	}
	return candidates
}

func (s Server) joinPage(w http.ResponseWriter, r *http.Request, code, manifestURL string) {
	joinBase := strings.TrimRight(requestBaseURL(r), "/") + "/join/" + code
	shellCommand := "curl -fsSL " + shellQuote(joinBase+"/bootstrap.sh") + " | sh"
	powerShellCommand := "powershell -NoProfile -Command \"irm '" + powerShellSingleQuoteValue(joinBase+"/bootstrap.ps1") + "' | iex\""
	packageCatalog := model.NewConnectionEntryPackageCatalog(joinBase)
	packageCatalogJSON, _ := json.Marshal(packageCatalog)
	packageCatalogScript := strings.ReplaceAll(string(packageCatalogJSON), "</", "<\\/")
	locale := joinLocale(r)
	copy := joinCopy(locale)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="%s">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 2rem; line-height: 1.45; max-width: 860px; }
	    code, pre { background: #f4f4f5; border-radius: 6px; padding: .2rem .35rem; }
	    pre { padding: 1rem; overflow-x: auto; }
	    .note { border-left: 4px solid #2563eb; padding-left: 1rem; color: #1f2937; }
	    .entry { border: 1px solid #d4d4d8; border-radius: 8px; padding: 1rem; margin: 1rem 0; }
	    .status { color: #52525b; }
	  </style>
	</head>
	<body>
	  <h1>%s</h1>
	  <p class="note">%s</p>
	  <section class="entry" id="selected-entry">
	    <h2>%s</h2>
	    <p class="status" id="selected-status">%s</p>
	    <pre><code id="selected-command">%s</code></pre>
	  </section>
	  <h2>macOS / Linux</h2>
	  <pre><code>%s</code></pre>
	  <h2>Windows PowerShell</h2>
	  <pre><code>%s</code></pre>
	  <h2>%s</h2>
	  <pre><code id="package-catalog-json">%s</code></pre>
	  <h2>%s</h2>
	  <ol>
	    <li>%s</li>
    <li>%s</li>
    <li>%s</li>
	  </ol>
	  <p>Manifest: <code>%s</code></p>
	  <script type="application/json" id="package-catalog">%s</script>
	  <script>
	    (() => {
	      const catalog = JSON.parse(document.getElementById("package-catalog").textContent);
	      const ua = navigator.userAgent || "";
	      const platform = navigator.platform || "";
	      const haystack = (ua + " " + platform).toLowerCase();
	      const candidate = (catalog.candidates || []).find((item) =>
	        item.selection_hints && item.selection_hints.some((hint) =>
	          haystack.includes(String(hint).toLowerCase())
	        )
	      );
	      const status = document.getElementById("selected-status");
	      const command = document.getElementById("selected-command");
	      if (!candidate) {
	        status.textContent = "Could not detect this OS. Use the macOS/Linux or Windows command below.";
	        return;
	      }
	      status.textContent = candidate.label + ": package " + candidate.package_status + "; using visible " + candidate.fallback_script_status + " script fallback.";
	      if (candidate.target_os === "windows") {
	        command.textContent = %q;
	      } else {
	        command.textContent = %q;
	      }
	    })();
	  </script>
	</body>
	</html>`,
		html.EscapeString(locale),
		html.EscapeString(copy.Title),
		html.EscapeString(copy.Heading),
		html.EscapeString(copy.Note),
		html.EscapeString(copy.SelectedHeading),
		html.EscapeString(copy.SelectedStatus),
		html.EscapeString(copy.SelectedCommand),
		html.EscapeString(shellCommand),
		html.EscapeString(powerShellCommand),
		html.EscapeString(copy.PackageCatalogHeading),
		html.EscapeString(string(packageCatalogJSON)),
		html.EscapeString(copy.NextHeading),
		copy.StepCheck,
		copy.StepStart,
		copy.StepAgent,
		html.EscapeString(manifestURL),
		packageCatalogScript,
		powerShellCommand,
		shellCommand,
	)
}

type joinPageCopy struct {
	Title                 string
	Heading               string
	Note                  string
	SelectedHeading       string
	SelectedStatus        string
	SelectedCommand       string
	PackageCatalogHeading string
	NextHeading           string
	StepCheck             string
	StepStart             string
	StepAgent             string
}

func joinCopy(locale string) joinPageCopy {
	withDefaults := func(copy joinPageCopy) joinPageCopy {
		if copy.SelectedHeading == "" {
			copy.SelectedHeading = "Recommended Entry"
		}
		if copy.SelectedStatus == "" {
			copy.SelectedStatus = "Detecting this machine and selecting the best available entry."
		}
		if copy.SelectedCommand == "" {
			copy.SelectedCommand = "If detection is unavailable, choose the macOS/Linux or Windows command below."
		}
		if copy.PackageCatalogHeading == "" {
			copy.PackageCatalogHeading = "Agent Package Catalog"
		}
		return copy
	}
	switch locale {
	case "zh-CN":
		return withDefaults(joinPageCopy{
			Title:                 "Remote Dev Skillkit 连接",
			Heading:               "连接这台机器",
			Note:                  "在需要帮助的电脑上运行一条命令。连接是可见、仅出站、可撤销，并且限定在此支持工单内。",
			SelectedHeading:       "推荐入口",
			SelectedStatus:        "正在识别这台机器，并选择最合适的连接入口。",
			SelectedCommand:       "如果无法自动识别，请使用下面的 macOS/Linux 或 Windows 命令。",
			PackageCatalogHeading: "Agent 包目录",
			NextHeading:           "接下来会发生什么",
			StepCheck:             `启动脚本会检查 <code>rdev</code>。`,
			StepStart:             `它会用 <code>--transport long-poll</code> 启动一个可见、稳定的协助式主机会话。`,
			StepAgent:             "Agent 会等待主机上线，在策略需要时完成批准，然后运行受限的修复任务。",
		})
	case "es":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Conectar Esta Maquina",
			Note:        "Ejecuta un comando en el equipo que necesita ayuda. La conexion es visible, solo saliente, revocable y limitada a este ticket.",
			NextHeading: "Que pasa despues",
			StepCheck:   `El bootstrap comprueba <code>rdev</code>.`,
			StepStart:   `Inicia una sesion visible y estable con <code>--transport long-poll</code>.`,
			StepAgent:   "El Agent espera el host, lo aprueba si la politica lo requiere y ejecuta trabajos de reparacion limitados.",
		})
	case "fr":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Connecter Cette Machine",
			Note:        "Executez une commande sur l'ordinateur a aider. La connexion est visible, sortante uniquement, revocable et limitee a ce ticket.",
			NextHeading: "Et ensuite",
			StepCheck:   `Le bootstrap verifie <code>rdev</code>.`,
			StepStart:   `Il demarre une session visible et stable avec <code>--transport long-poll</code>.`,
			StepAgent:   "L'Agent attend le host, l'approuve si la politique l'exige, puis execute des reparations limitees.",
		})
	case "de":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Diese Maschine Verbinden",
			Note:        "Fuhre einen Befehl auf dem Computer aus, der Hilfe braucht. Die Verbindung ist sichtbar, nur ausgehend, widerrufbar und auf dieses Ticket begrenzt.",
			NextHeading: "Was als Nachstes passiert",
			StepCheck:   `Der Bootstrap pruft <code>rdev</code>.`,
			StepStart:   `Er startet eine sichtbare, stabile Sitzung mit <code>--transport long-poll</code>.`,
			StepAgent:   "Der Agent wartet auf den Host, genehmigt ihn falls erforderlich und startet begrenzte Reparaturjobs.",
		})
	case "ja":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "このマシンを接続",
			Note:        "サポートが必要なコンピューターで 1 つのコマンドを実行します。接続は可視、アウトバウンドのみ、取り消し可能で、このサポートチケットに限定されます。",
			NextHeading: "次に行われること",
			StepCheck:   `bootstrap は <code>rdev</code> を確認します。`,
			StepStart:   `<code>--transport long-poll</code> で可視で安定したホストセッションを開始します。`,
			StepAgent:   "Agent はホストを待ち、ポリシーが必要とする場合に承認し、限定された修復ジョブを実行します。",
		})
	case "ko":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "이 머신 연결",
			Note:        "도움이 필요한 컴퓨터에서 명령 하나를 실행합니다. 연결은 보이는 방식이며, 아웃바운드 전용이고, 철회 가능하며, 이 지원 티켓 범위로 제한됩니다.",
			NextHeading: "다음 단계",
			StepCheck:   `bootstrap 이 <code>rdev</code> 를 확인합니다.`,
			StepStart:   `<code>--transport long-poll</code> 로 보이고 안정적인 호스트 세션을 시작합니다.`,
			StepAgent:   "Agent 는 호스트를 기다리고, 정책상 필요하면 승인한 뒤 제한된 복구 작업을 실행합니다.",
		})
	case "pt-BR":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Conectar Esta Maquina",
			Note:        "Execute um comando no computador que precisa de ajuda. A conexao e visivel, somente de saida, revogavel e limitada a este ticket.",
			NextHeading: "O que acontece depois",
			StepCheck:   `O bootstrap verifica <code>rdev</code>.`,
			StepStart:   `Ele inicia uma sessao visivel e estavel com <code>--transport long-poll</code>.`,
			StepAgent:   "O Agent aguarda o host, aprova quando a politica exige e executa tarefas de reparo limitadas.",
		})
	case "hi":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "इस मशीन को कनेक्ट करें",
			Note:        "जिस कंप्यूटर को मदद चाहिए उस पर एक कमांड चलाएं। कनेक्शन दिखने वाला, केवल outbound, revoke करने योग्य, और इस support ticket तक सीमित है।",
			NextHeading: "आगे क्या होगा",
			StepCheck:   `bootstrap <code>rdev</code> जांचता है।`,
			StepStart:   `यह <code>--transport long-poll</code> के साथ visible और stable host session शुरू करता है।`,
			StepAgent:   "Agent host का इंतजार करता है, policy की जरूरत पर approve करता है, और scoped repair jobs चलाता है।",
		})
	case "ar":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "توصيل هذا الجهاز",
			Note:        "شغّل أمرا واحدا على الكمبيوتر الذي يحتاج إلى مساعدة. الاتصال ظاهر، صادر فقط، قابل للإلغاء، ومحدود بتذكرة الدعم هذه.",
			NextHeading: "ماذا يحدث بعد ذلك",
			StepCheck:   `يتحقق bootstrap من <code>rdev</code>.`,
			StepStart:   `يبدأ جلسة host مرئية ومستقرة باستخدام <code>--transport long-poll</code>.`,
			StepAgent:   "ينتظر Agent ظهور host، ويوافق عليه عند الحاجة حسب السياسة، ثم يشغل مهام إصلاح محددة النطاق.",
		})
	case "ru":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Подключить Эту Машину",
			Note:        "Выполните одну команду на компьютере, которому нужна помощь. Подключение видимое, только исходящее, отзывное и ограничено этим тикетом.",
			NextHeading: "Что будет дальше",
			StepCheck:   `bootstrap проверит <code>rdev</code>.`,
			StepStart:   `Он запустит видимую и стабильную сессию host с <code>--transport long-poll</code>.`,
			StepAgent:   "Agent дождется host, выполнит approval при необходимости и запустит ограниченные repair jobs.",
		})
	default:
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Connect This Machine",
			Note:        "Run one command on the computer that needs help. The connection is visible, outbound-only, revocable, and scoped to this support ticket.",
			NextHeading: "What Happens Next",
			StepCheck:   `The bootstrap checks for <code>rdev</code>.`,
			StepStart:   `It starts a visible, stable attended host session with <code>--transport long-poll</code>.`,
			StepAgent:   "The Agent waits for the host, approves it when policy requires, and runs scoped repair jobs.",
		})
	}
}

func joinLocale(r *http.Request) string {
	if lang := supportedJoinLocale(r.URL.Query().Get("lang")); lang != "" {
		return lang
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if lang := supportedJoinLocale(tag); lang != "" {
			return lang
		}
	}
	return "en"
}

func supportedJoinLocale(tag string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(tag), "_", "-")
	if normalized == "" {
		return ""
	}
	lower := strings.ToLower(normalized)
	switch lower {
	case "en":
		return "en"
	case "zh-cn", "zh-hans", "zh":
		return "zh-CN"
	case "es":
		return "es"
	case "fr":
		return "fr"
	case "de":
		return "de"
	case "ja":
		return "ja"
	case "ko":
		return "ko"
	case "pt-br", "pt":
		return "pt-BR"
	case "hi":
		return "hi"
	case "ar":
		return "ar"
	case "ru":
		return "ru"
	default:
		if base, _, ok := strings.Cut(lower, "-"); ok {
			return supportedJoinLocale(base)
		}
		return ""
	}
}

func (s Server) writeShellBootstrap(w http.ResponseWriter, r *http.Request, manifestURL, manifestRootPublicKey string) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	rootArg := ""
	if strings.TrimSpace(manifestRootPublicKey) != "" {
		rootArg = " --manifest-root-public-key " + shellQuote(manifestRootPublicKey)
	}
	assetBase := shellQuote(strings.TrimRight(requestBaseURL(r), "/") + "/assets")
	_, _ = fmt.Fprintf(w, `#!/bin/sh
set -eu
if ! command -v rdev >/dev/null 2>&1; then
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "unsupported architecture: $arch" >&2; exit 127 ;;
  esac
  case "$os" in
    darwin|linux) ;;
    *) echo "unsupported operating system: $os" >&2; exit 127 ;;
  esac
  asset="rdev-${os}-${arch}"
  mkdir -p "${TMPDIR:-/tmp}/rdev-connection-entry"
  out="${TMPDIR:-/tmp}/rdev-connection-entry/rdev"
  echo "Downloading verified rdev helper ${asset}..."
  http_status="$(curl -fsS -o /dev/null -w "%%{http_code}" %s"/${asset}" 2>/dev/null || true)"
  if [ "$http_status" != "200" ]; then
    echo "rdev helper binary not available at gateway (HTTP $http_status) — the gateway may still be starting. Wait a moment and retry." >&2
    exit 127
  fi
  curl -fsSL %s"/${asset}" -o "$out"
  expected="$(curl -fsSL %s"/${asset}.sha256")"
  actual="$(shasum -a 256 "$out" | awk '{print $1}')"
  if [ "$actual" != "$expected" ]; then
    echo "rdev helper SHA-256 mismatch" >&2
    rm -f "$out"
    exit 127
  fi
  chmod 700 "$out"
  rdev_cmd="$out"
else
  rdev_cmd="$(command -v rdev)"
fi
echo "Starting visible Remote Dev Skillkit host session..."
# Prevent idle/display/system sleep while the rdev session is active when the
# platform exposes a standard inhibitor. This does not bypass lock-screen
# policy or enterprise security controls. Kill the inhibitor when the runner
# exits.
rdev_caffeinate_pid=""
rdev_inhibit_pid=""
if [ "$os" = "darwin" ] && command -v caffeinate >/dev/null 2>&1; then
  caffeinate -dimsu &
  rdev_caffeinate_pid=$!
  echo "[rdev] Sleep prevention enabled via caffeinate (pid $rdev_caffeinate_pid)"
elif [ "$os" = "linux" ] && command -v systemd-inhibit >/dev/null 2>&1; then
  systemd-inhibit --what=sleep:idle --why="Remote Dev Skillkit host session is active" --mode=block sleep infinity &
  rdev_inhibit_pid=$!
  echo "[rdev] Sleep prevention enabled via systemd-inhibit (pid $rdev_inhibit_pid)"
else
  echo "[rdev] Sleep prevention unavailable — keep this visible session active to avoid disconnection"
fi
rdev_max_retries=5
rdev_retry_delay=5
rdev_attempt=0
while true; do
  "$rdev_cmd" host serve --manifest-url %s%s --transport long-poll --once=false --max-jobs 0
  rdev_exit=$?
  rdev_attempt=$((rdev_attempt + 1))
  echo "[rdev] host process exited with code $rdev_exit"
  if [ $rdev_exit -eq 0 ] || [ $rdev_attempt -gt $rdev_max_retries ]; then
    break
  fi
  echo "[rdev] Retrying (attempt $rdev_attempt of $rdev_max_retries) after ${rdev_retry_delay}s..."
  sleep $rdev_retry_delay
done
if [ -n "$rdev_caffeinate_pid" ]; then
  kill "$rdev_caffeinate_pid" 2>/dev/null || true
fi
if [ -n "$rdev_inhibit_pid" ]; then
  kill "$rdev_inhibit_pid" 2>/dev/null || true
fi
exit $rdev_exit
`, assetBase, assetBase, assetBase, shellQuote(manifestURL), rootArg)
}

func (s Server) writePowerShellBootstrap(w http.ResponseWriter, r *http.Request, manifestURL, manifestRootPublicKey string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	rootArg := ""
	if strings.TrimSpace(manifestRootPublicKey) != "" {
		rootArg = " --manifest-root-public-key '" + powerShellSingleQuoteValue(manifestRootPublicKey) + "'"
	}
	assetBase := powerShellSingleQuoteValue(strings.TrimRight(requestBaseURL(r), "/") + "/assets")
	_, _ = fmt.Fprintf(w, `$ErrorActionPreference = 'Stop'
$rdevCmd = Get-Command rdev -ErrorAction SilentlyContinue
if ($rdevCmd) {
  $rdevPath = $rdevCmd.Source
} else {
  if (-not [Environment]::Is64BitOperatingSystem) {
    throw "unsupported Windows architecture: 32-bit"
  }
  $asset = "rdev-windows-amd64.exe"
  $dir = Join-Path $env:TEMP "rdev-connection-entry"
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $rdevPath = Join-Path $dir "rdev.exe"
  Write-Host "Downloading verified rdev helper $asset..."
  try {
    Invoke-WebRequest -Uri ('%s/' + $asset) -OutFile $rdevPath -UseBasicParsing -ErrorAction Stop
  } catch {
    $errMsg = $_.Exception.Message
    throw ("Failed to download rdev helper from gateway. The asset binary may not be configured yet — ensure the gateway has finished starting, then run the command again. Detail: " + $errMsg)
  }
  $expected = (Invoke-WebRequest -Uri ('%s/' + $asset + '.sha256') -UseBasicParsing).Content.Trim()
  $actual = (Get-FileHash -Algorithm SHA256 -Path $rdevPath).Hash.ToLowerInvariant()
  if ($actual -ne $expected.ToLowerInvariant()) {
    Remove-Item -Force $rdevPath -ErrorAction SilentlyContinue
    throw "rdev helper SHA-256 mismatch"
  }
}
Write-Host "Starting visible Remote Dev Skillkit host session..."
# Prevent Windows idle sleep/display sleep while rdev is running. This does not
# bypass lock-screen policy or enterprise security controls.
# SetThreadExecutionState keeps the session awake so the runner can continue to
# poll for and execute jobs.
try {
  Add-Type -TypeDefinition @'
using System.Runtime.InteropServices;
public static class RdevSleepPrevention {
  [DllImport("kernel32.dll")] public static extern uint SetThreadExecutionState(uint f);
  public const uint ES_CONTINUOUS      = 0x80000000u;
  public const uint ES_SYSTEM_REQUIRED = 0x00000001u;
  public const uint ES_DISPLAY_REQUIRED = 0x00000002u;
}
'@ -ErrorAction SilentlyContinue
  [void][RdevSleepPrevention]::SetThreadExecutionState([RdevSleepPrevention]::ES_CONTINUOUS -bor [RdevSleepPrevention]::ES_SYSTEM_REQUIRED -bor [RdevSleepPrevention]::ES_DISPLAY_REQUIRED)
  Write-Host "[rdev] Sleep prevention enabled (SetThreadExecutionState)"
} catch {
  Write-Host "[rdev] Sleep prevention unavailable — keep this window active to avoid disconnection"
}
$rdevMaxRetries = 5
$rdevRetryDelaySec = 5
$rdevAttempt = 0
do {
  if ($rdevAttempt -gt 0) {
    Write-Host ("[rdev] Retrying host registration (attempt $($rdevAttempt + 1) of $($rdevMaxRetries + 1)) after ${rdevRetryDelaySec}s...")
    Start-Sleep -Seconds $rdevRetryDelaySec
  }
  & $rdevPath host serve --manifest-url '%s'%s --transport long-poll --once=false --max-jobs 0
  $rdevExitCode = $LASTEXITCODE
  $rdevAttempt++
  Write-Host "[rdev] host process exited with code $rdevExitCode"
} while ($rdevExitCode -ne 0 -and $rdevAttempt -le $rdevMaxRetries)
# Restore normal sleep policy before exiting.
try { [void][RdevSleepPrevention]::SetThreadExecutionState([RdevSleepPrevention]::ES_CONTINUOUS) } catch { }
exit $rdevExitCode
`, assetBase, assetBase, powerShellSingleQuoteValue(manifestURL), rootArg)
}

func (s Server) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/assets/"), "/")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, `\`) {
		writeError(w, http.StatusNotFound, "unknown asset")
		return
	}
	shaOnly := false
	if strings.HasSuffix(name, ".sha256") {
		shaOnly = true
		name = strings.TrimSuffix(name, ".sha256")
	}
	path, ok := s.assetPath(name)
	if !ok {
		writeError(w, http.StatusNotFound, "asset is not configured")
		return
	}
	sum, err := fileSHA256(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if shaOnly {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, sum)
		return
	}
	http.ServeFile(w, r, path)
}

func (s Server) assetPath(name string) (string, bool) {
	switch name {
	case "rdev-windows-amd64.exe":
		return configuredAssetPath(s.Assets.RdevWindowsAMD64Path)
	case "rdev-darwin-arm64":
		return configuredAssetPath(s.Assets.RdevDarwinARM64Path)
	case "rdev-darwin-amd64":
		return configuredAssetPath(s.Assets.RdevDarwinAMD64Path)
	case "rdev-linux-amd64":
		return configuredAssetPath(s.Assets.RdevLinuxAMD64Path)
	case "rdev-linux-arm64":
		return configuredAssetPath(s.Assets.RdevLinuxARM64Path)
	default:
		return "", false
	}
}

func configuredAssetPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	clean, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(clean)
	if err != nil || info.IsDir() {
		return "", false
	}
	return clean, true
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func manifestRootPublicKey(root model.TrustBundle) string {
	if root.SigningKeyID == "" || root.PublicKey == "" {
		return ""
	}
	return root.SigningKeyID + ":" + root.PublicKey
}

func (s Server) listHosts(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hosts": s.Gateway.Hosts(r.URL.Query().Get("status")),
	})
}

func (s Server) supportSessionStatus(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	ticketCode := strings.TrimSpace(r.URL.Query().Get("ticket_code"))
	if ticketCode == "" {
		writeError(w, http.StatusBadRequest, "ticket_code is required")
		return
	}
	hosts := s.Gateway.HostsForTicketCode(ticketCode, "")
	opts := supportsession.StatusOptions{
		TicketCode: ticketCode,
		Hosts:      hosts,
		Locale:     r.URL.Query().Get("locale"),
		GatewayURL: requestBaseURL(r),
	}
	// Attach ticket expiry when the ticket is found so the status response
	// includes ticket_expires_at and ticket_expires_in_seconds.
	if ticket, ok := s.Gateway.TicketForCode(ticketCode); ok {
		opts.Ticket = &ticket
	}
	status := supportsession.BuildStatus(opts)
	writeJSON(w, http.StatusOK, status)
}

func (s Server) registerHost(w http.ResponseWriter, r *http.Request) {
	var req model.HostRegistration
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	host, err := s.Gateway.RegisterHost(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Generate a per-host secret that the host process must present on all
	// subsequent host-side requests (jobs/next, heartbeat, complete, fail).
	secret, err := s.Gateway.GenerateHostSecret(host.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate host secret: "+err.Error())
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"host":        host,
		"host_secret": secret,
	})
}

func (s Server) hostAction(w http.ResponseWriter, r *http.Request) {
	hostID, action, ok := splitHostAction(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown host endpoint")
		return
	}
	// heartbeat is host-authenticated (not operator-authenticated)
	if action == "heartbeat" {
		secret := extractBearerToken(r)
		if err := s.Gateway.HeartbeatHost(hostID, secret); err != nil {
			if strings.Contains(err.Error(), "policy denied") {
				writeError(w, http.StatusUnauthorized, "host authentication required: provide host_secret as Bearer token")
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "host_id": hostID})
		return
	}
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
	switch action {
	case "approve":
		var req struct {
			Capabilities []string `json:"capabilities"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
		host, err := s.Gateway.ApproveHost(hostID, req.Capabilities)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"host": host})
	case "revoke":
		var req struct {
			Reason string `json:"reason"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
		host, err := s.Gateway.RevokeHost(hostID, req.Reason)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"host": host})
	default:
		writeError(w, http.StatusNotFound, "unknown host action")
	}
}

func (s Server) hostSubresource(w http.ResponseWriter, r *http.Request) {
	if hostID, ok := splitHostID(r.URL.Path); ok {
		if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
			writeError(w, http.StatusForbidden, "auditor role is required")
			return
		}
		host, err := s.Gateway.Host(hostID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"host": host})
		return
	}
	hostID, resource, action, ok := splitHostSubresource(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown host endpoint")
		return
	}
	switch {
	case resource == "jobs" && action == "next":
		// Validate host secret: prevents unauthenticated callers from claiming
		// jobs and impersonating the registered host process.
		if !s.Gateway.ValidateHostSecret(hostID, extractBearerToken(r)) {
			writeError(w, http.StatusUnauthorized, "host authentication required: include Authorization: Bearer <host_secret>")
			return
		}
		wait, err := parseLongPollWait(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job, ok, err := s.nextJobForHost(r.Context(), hostID, wait)
		if err != nil {
			if err == context.Canceled {
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"job": nil})
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
	case resource == "trust-bundle" && action == "update":
		currentSequence, err := parseOptionalInt(r.URL.Query().Get("current_sequence"), "current_sequence")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		update, err := s.Gateway.TrustBundleUpdateForHost(hostID, currentSequence, r.URL.Query().Get("current_hash"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"trust_bundle_update": update})
	default:
		writeError(w, http.StatusNotFound, "unknown host subresource")
	}
}

func (s Server) nextJobForHost(ctx context.Context, hostID string, wait time.Duration) (model.Job, bool, error) {
	if wait <= 0 {
		return s.Gateway.NextJobForHost(hostID)
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, ok, err := s.Gateway.NextJobForHost(hostID)
		if err != nil || ok {
			return job, ok, err
		}
		select {
		case <-ctx.Done():
			return model.Job{}, false, ctx.Err()
		case <-deadline.C:
			return model.Job{}, false, nil
		case <-ticker.C:
		}
	}
}

func (s Server) createJob(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
	if r.Method == http.MethodGet {
		s.listJobs(w, r)
		return
	}
	var req struct {
		HostID  string         `json:"host_id"`
		Adapter string         `json:"adapter"`
		Intent  string         `json:"intent"`
		Policy  map[string]any `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// Policy pre-check: validate before persisting the job so that policy
	// explain and job creation are consistent — a job that policy.ExplainShellJob
	// would deny is rejected at the gateway level instead of being left queued
	// forever waiting for a host that will never execute it.
	adapter := strings.ToLower(strings.TrimSpace(req.Adapter))
	if adapter == "shell" || adapter == "powershell" {
		if host, hostErr := s.Gateway.Host(req.HostID); hostErr == nil {
			explanation := policy.ExplainShellJob(host.Mode, req.Policy)
			if !explanation.Allowed {
				summary := strings.Join(explanation.Denials, "; ")
				if summary == "" {
					summary = "policy denied"
				}
				writeError(w, http.StatusUnprocessableEntity, "policy_violation: "+summary)
				return
			}
		}
	}
	job, err := s.Gateway.CreateJob(req.HostID, req.Adapter, req.Intent, req.Policy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": job})
}

func (s Server) listJobs(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	hostID := strings.TrimSpace(r.URL.Query().Get("host_id"))
	jobs := s.Gateway.Jobs()
	if hostID != "" {
		filtered := jobs[:0]
		for _, job := range jobs {
			if job.HostID == hostID {
				filtered = append(filtered, job)
			}
		}
		jobs = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s Server) getJob(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	if jobID, resource, ok := splitJobSubresource(r.URL.Path); ok {
		switch resource {
		case "artifacts":
			writeJSON(w, http.StatusOK, map[string]any{"artifacts": s.Gateway.Artifacts(jobID)})
		case "evidence-bundle":
			s.exportJobEvidenceBundle(w, r, jobID)
		default:
			writeError(w, http.StatusNotFound, "unknown job subresource")
		}
		return
	}
	jobID, ok := splitJobID(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown job endpoint")
		return
	}
	job, err := s.Gateway.Job(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	artifactID, ok := splitArtifactID(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown artifact endpoint")
		return
	}
	artifact, err := s.Gateway.Artifact(artifactID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact})
}

func (s Server) exportJobEvidenceBundle(w http.ResponseWriter, r *http.Request, jobID string) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
	out := r.URL.Query().Get("out")
	if out == "" {
		writeError(w, http.StatusBadRequest, "out query parameter is required")
		return
	}
	job, err := s.Gateway.Job(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	manifest, err := evidence.ExportDirectory(out, evidence.Input{
		Job:         job,
		Artifacts:   s.Gateway.Artifacts(jobID),
		AuditEvents: s.Gateway.AuditEvents(),
		GeneratedAt: time.Now(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"out":               out,
		"job_id":            manifest.JobID,
		"file_count":        len(manifest.Files) + 1,
		"audit_event_count": manifest.AuditEventCount,
		"audit_root_hash":   manifest.AuditRootHash,
		"manifest":          manifest,
	})
}

func (s Server) jobAction(w http.ResponseWriter, r *http.Request) {
	jobID, action, ok := splitJobAction(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown job endpoint")
		return
	}
	switch action {
	case "complete":
		var req struct {
			HostID          string `json:"host_id"`
			ArtifactContent string `json:"artifact_content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.HostID == "" {
			writeError(w, http.StatusBadRequest, "host_id is required")
			return
		}
		if !s.Gateway.ValidateHostSecret(req.HostID, extractBearerToken(r)) {
			writeError(w, http.StatusUnauthorized, "host authentication required: include Authorization: Bearer <host_secret>")
			return
		}
		job, artifact, err := s.Gateway.CompleteJobForHost(req.HostID, jobID, req.ArtifactContent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job, "artifact": artifact})
	case "fail":
		var req struct {
			HostID          string `json:"host_id"`
			Reason          string `json:"reason"`
			ArtifactContent string `json:"artifact_content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.HostID == "" {
			writeError(w, http.StatusBadRequest, "host_id is required")
			return
		}
		if !s.Gateway.ValidateHostSecret(req.HostID, extractBearerToken(r)) {
			writeError(w, http.StatusUnauthorized, "host authentication required: include Authorization: Bearer <host_secret>")
			return
		}
		job, artifact, err := s.Gateway.FailJobForHostWithArtifact(req.HostID, jobID, req.Reason, req.ArtifactContent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		payload := map[string]any{"job": job}
		if artifact != nil {
			payload["artifact"] = artifact
		}
		writeJSON(w, http.StatusOK, payload)
	case "artifact":
		var req struct {
			HostID          string `json:"host_id"`
			ArtifactContent string `json:"artifact_content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.HostID == "" {
			writeError(w, http.StatusBadRequest, "host_id is required")
			return
		}
		if !s.Gateway.ValidateHostSecret(req.HostID, extractBearerToken(r)) {
			writeError(w, http.StatusUnauthorized, "host authentication required: include Authorization: Bearer <host_secret>")
			return
		}
		job, artifact, err := s.Gateway.AppendJobArtifactForHost(req.HostID, jobID, req.ArtifactContent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job, "artifact": artifact})
	case "cancel":
		// Operator-side job cancellation. The agent calls this when a job is stuck
		// or should no longer run. The gateway marks the job as canceled and the
		// host will see its status transition to "canceled" on the next poll/push.
		if !s.authorizeOperator(r, operatorauth.RoleOperator) {
			writeError(w, http.StatusForbidden, "operator role is required")
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		// Reason is optional; ignore decode errors so an empty body also works.
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Reason == "" {
			req.Reason = "canceled by operator"
		}
		job, err := s.Gateway.CancelJob(jobID, req.Reason)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
	default:
		writeError(w, http.StatusNotFound, "unknown job action")
	}
}

func (s Server) listAudit(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": s.Gateway.AuditEvents(),
	})
}

func (s Server) authorizeOperator(r *http.Request, roles ...string) bool {
	if !s.OperatorAuth.Enabled() {
		return true
	}
	return s.OperatorAuth.AuthorizeBearer(r.Header.Get("Authorization"), roles...)
}

func (s Server) persistState(w http.ResponseWriter) bool {
	if err := s.persistStateInternal(); err != nil {
		writeError(w, http.StatusInternalServerError, "persist gateway state: "+err.Error())
		return false
	}
	return true
}

func (s Server) persistStateNoResponse() bool {
	return s.persistStateInternal() == nil
}

func (s Server) persistStateInternal() error {
	if s.StateStore == nil {
		if strings.TrimSpace(s.StatePath) == "" {
			return nil
		}
		store, err := gateway.NewFileStateStore(s.StatePath)
		if err != nil {
			return fmt.Errorf("configure gateway state store: %w", err)
		}
		s.StateStore = store
	}
	if s.StateStore == nil {
		return nil
	}
	if s.stateMu != nil {
		s.stateMu.Lock()
		defer s.stateMu.Unlock()
	}
	if _, err := s.StateStore.SaveFrom(s.Gateway); err != nil {
		return err
	}
	return nil
}

func parseLongPollWait(r *http.Request) (time.Duration, error) {
	query := r.URL.Query()
	if raw := query.Get("wait_ms"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 60000 {
			return 0, fmt.Errorf("wait_ms must be between 0 and 60000")
		}
		return time.Duration(value) * time.Millisecond, nil
	}
	if raw := query.Get("wait_seconds"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 60 {
			return 0, fmt.Errorf("wait_seconds must be between 0 and 60")
		}
		return time.Duration(value) * time.Second, nil
	}
	return 0, nil
}

func parseOptionalInt(raw, name string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

func splitHostAction(path string) (hostID string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/hosts/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func splitTicketSubresource(path string) (code string, resource string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/tickets/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func splitJoinPath(path string) (code string, resource string, ok bool) {
	rest := strings.TrimPrefix(path, "/join/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], "", true
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func splitHostID(path string) (hostID string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/hosts/")
	if rest == path {
		return "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 1 || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>*?()[]{}!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func powerShellSingleQuoteValue(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func splitHostSubresource(path string) (hostID string, resource string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/hosts/")
	if rest == path {
		return "", "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func splitJobID(path string) (jobID string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/jobs/")
	if rest == path {
		return "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 1 || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func splitJobAction(path string) (jobID string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/jobs/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func splitJobSubresource(path string) (jobID string, resource string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/jobs/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func splitArtifactID(path string) (artifactID string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/artifacts/")
	if rest == path {
		return "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 1 || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

// extractBearerToken returns the token from "Authorization: Bearer <token>"
// or from the "host_secret" query parameter (useful for WebSocket upgrades
// which cannot easily set request headers).
func extractBearerToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return r.URL.Query().Get("host_secret")
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}
