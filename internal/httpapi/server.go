package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/evidence"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type Server struct {
	Gateway               *gateway.MemoryGateway
	StatePath             string
	EnrollmentIssuerToken string
	stateMu               *sync.Mutex
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return Server{Gateway: gw, stateMu: &sync.Mutex{}}
}

func NewServerWithState(gw *gateway.MemoryGateway, statePath string) Server {
	return Server{Gateway: gw, StatePath: statePath, stateMu: &sync.Mutex{}}
}

func NewServerWithOptions(gw *gateway.MemoryGateway, statePath, enrollmentIssuerToken string) Server {
	return Server{Gateway: gw, StatePath: statePath, EnrollmentIssuerToken: enrollmentIssuerToken, stateMu: &sync.Mutex{}}
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/trust", s.trust)
	mux.HandleFunc("GET /v1/trust-bundle", s.getTrustBundle)
	mux.HandleFunc("GET /v1/enrollment/revocations", s.getEnrollmentRevocations)
	mux.HandleFunc("POST /v1/enrollment/certificates", s.issueEnrollmentCertificate)
	mux.HandleFunc("POST /v1/trust-bundle", s.updateTrustBundle)
	mux.HandleFunc("POST /v1/tickets", s.createTicket)
	mux.HandleFunc("GET /v1/tickets/", s.ticketSubresource)
	mux.HandleFunc("GET /v1/hosts", s.listHosts)
	mux.HandleFunc("POST /v1/hosts/register", s.registerHost)
	mux.HandleFunc("GET /v1/hosts/", s.hostSubresource)
	mux.HandleFunc("POST /v1/hosts/", s.hostAction)
	mux.HandleFunc("POST /v1/jobs", s.createJob)
	mux.HandleFunc("GET /v1/jobs/", s.getJob)
	mux.HandleFunc("POST /v1/jobs/", s.jobAction)
	mux.HandleFunc("GET /v1/artifacts/", s.getArtifact)
	mux.HandleFunc("GET /v1/audit", s.listAudit)
	return mux
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
	revocations, ok := s.Gateway.EnrollmentRevocations()
	if !ok {
		writeError(w, http.StatusNotFound, "enrollment revocations not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revocations": revocations})
}

func (s Server) issueEnrollmentCertificate(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeEnrollmentIssuer(r) {
		writeError(w, http.StatusUnauthorized, "enrollment issuer token is required")
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

func (s Server) authorizeEnrollmentIssuer(r *http.Request) bool {
	token := strings.TrimSpace(s.EnrollmentIssuerToken)
	if token == "" {
		return true
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func (s Server) updateTrustBundle(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Mode         model.HostMode `json:"mode"`
		TTLSeconds   int            `json:"ttl_seconds"`
		Capabilities []string       `json:"capabilities"`
		Reason       string         `json:"reason"`
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
	ticket, err := s.Gateway.CreateTicket(req.Mode, req.TTLSeconds, req.Capabilities, req.Reason)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"ticket":      ticket,
		"joinUrl":     "https://agent.lunflux.com/join/" + ticket.Code,
		"manifestUrl": requestBaseURL(r) + "/v1/tickets/" + ticket.Code + "/manifest",
	})
}

func (s Server) ticketSubresource(w http.ResponseWriter, r *http.Request) {
	code, resource, ok := splitTicketSubresource(r.URL.Path)
	if !ok || resource != "manifest" {
		writeError(w, http.StatusNotFound, "unknown ticket endpoint")
		return
	}
	manifest, err := s.Gateway.JoinManifest(code, requestBaseURL(r), requestBaseURL(r)+"/join/"+code)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": manifest})
}

func (s Server) listHosts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"hosts": s.Gateway.Hosts(r.URL.Query().Get("status")),
	})
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
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"host": host})
}

func (s Server) hostAction(w http.ResponseWriter, r *http.Request) {
	hostID, action, ok := splitHostAction(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown host endpoint")
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

func (s Server) getJob(w http.ResponseWriter, r *http.Request) {
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
		job, artifact, err := s.Gateway.AppendJobArtifactForHost(req.HostID, jobID, req.ArtifactContent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job, "artifact": artifact})
	default:
		writeError(w, http.StatusNotFound, "unknown job action")
	}
}

func (s Server) listAudit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"events": s.Gateway.AuditEvents(),
	})
}

func (s Server) persistState(w http.ResponseWriter) bool {
	if strings.TrimSpace(s.StatePath) == "" {
		return true
	}
	if s.stateMu != nil {
		s.stateMu.Lock()
		defer s.stateMu.Unlock()
	}
	if _, err := s.Gateway.SaveSnapshot(s.StatePath); err != nil {
		writeError(w, http.StatusInternalServerError, "persist gateway state: "+err.Error())
		return false
	}
	return true
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
