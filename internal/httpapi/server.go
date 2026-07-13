package httpapi

import (
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

type Server struct {
	Gateway         *gateway.MemoryGateway
	StatePath       string
	StateStore      gateway.StateStore
	OperatorAuth    *operatorauth.Authorizer
	Assets          AssetConfig
	stateMu         *sync.Mutex
	gatewayInstance string
}

var gatewayInstanceFallbackCounter atomic.Uint64

const permanentHostFailureExitCode = 78

type AssetConfig struct {
	RdevWindowsAMD64Path string
	RdevDarwinARM64Path  string
	RdevDarwinAMD64Path  string
	RdevLinuxAMD64Path   string
	RdevLinuxARM64Path   string
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return newServer(gw, nil, nil)
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
	return newServer(gw, store, nil)
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
	return newServer(gw, store, auth)
}

func newServer(gw *gateway.MemoryGateway, store gateway.StateStore, auth *operatorauth.Authorizer) Server {
	return Server{
		Gateway:         gw,
		StateStore:      store,
		OperatorAuth:    auth,
		stateMu:         &sync.Mutex{},
		gatewayInstance: newGatewayInstance(),
	}
}

func newGatewayInstance() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err == nil {
		return hex.EncodeToString(id[:])
	}
	fallback := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", time.Now().UnixNano(), gatewayInstanceFallbackCounter.Add(1))))
	return hex.EncodeToString(fallback[:len(id)])
}

func (s *Server) GatewayInstance() string {
	if s == nil {
		return ""
	}
	return s.gatewayInstance
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/support-session/bootstrap-probe.ps1", s.bootstrapProbeTemplate)
	mux.HandleFunc("GET /v1/trust", s.trust)
	mux.HandleFunc("GET /v1/trust-bundle", s.getTrustBundle)
	mux.HandleFunc("GET /v1/enrollment/revocations", s.getEnrollmentRevocations)
	mux.HandleFunc("POST /v1/enrollment/certificates", s.issueEnrollmentCertificate)
	mux.HandleFunc("POST /v1/enrollment/certificates/renew", s.renewEnrollmentCertificate)
	mux.HandleFunc("POST /v1/trust-bundle", s.updateTrustBundle)
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("POST /v1/session-joins", s.joinSessionByCode)
	mux.HandleFunc("GET /v1/sessions/", s.sessionRoute)
	mux.HandleFunc("POST /v1/sessions/", s.sessionRoute)
	mux.HandleFunc("POST /v1/tickets", s.createTicket)
	mux.HandleFunc("GET /v1/tickets/", s.ticketSubresource)
	mux.HandleFunc("GET /join/", s.join)
	mux.HandleFunc("GET /assets/", s.asset)
	mux.HandleFunc("POST /v1/support-session/preconnect", s.supportSessionPreconnect)
	mux.HandleFunc("GET /v1/support-session/status", s.supportSessionStatus)
	mux.HandleFunc("GET /v1/audit", s.listAudit)
	return mux
}

func (s Server) bootstrapProbeTemplate(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "bootstrap probe query parameters are not allowed")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 {
		writeError(w, http.StatusBadRequest, "bootstrap probe request body is not allowed")
		return
	}
	w.Header().Set("X-Rdev-Gateway-Instance", s.gatewayInstance)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(tunnel.BootstrapProbePowerShell)))
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, tunnel.BootstrapProbePowerShell)
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Rdev-Gateway-Instance", s.gatewayInstance)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s Server) createSession(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "operator role is required", false))
		return
	}
	var spec controlplane.SessionSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	session, err := s.Gateway.CreateSession(spec)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"session": session,
		"status":  session.DeriveStatus(),
	})
}

func (s Server) joinSessionByCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JoinCode string                    `json:"join_code"`
		Endpoint controlplane.EndpointSpec `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	session, endpoint, lease, events, err := s.Gateway.JoinSessionByCode(req.JoinCode, req.Endpoint)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":  session,
		"endpoint": endpoint,
		"lease":    lease,
		"events":   events,
	})
}

func (s Server) sessionRoute(w http.ResponseWriter, r *http.Request) {
	sessionID, resource, taskID, action, ok := splitSessionPath(r.URL.Path)
	if !ok {
		writeProtocolError(w, http.StatusNotFound, protocolHTTPError(controlplane.ErrSessionClosed, "unknown session endpoint", false))
		return
	}
	switch {
	case r.Method == http.MethodGet && resource == "":
		s.getSessionSnapshot(w, r, sessionID)
	case r.Method == http.MethodPost && resource == "join":
		s.joinSession(w, r, sessionID)
	case r.Method == http.MethodGet && resource == "events":
		if strings.TrimSpace(r.URL.Query().Get("endpoint_id")) != "" {
			s.sessionEventsAfter(w, r, sessionID)
		} else {
			s.sessionAgentEventsAfter(w, r, sessionID)
		}
	case r.Method == http.MethodPost && resource == "events":
		s.appendSessionEvent(w, r, sessionID)
	case r.Method == http.MethodPost && resource == "tasks" && taskID == "" && action == "":
		s.submitSessionTask(w, r, sessionID)
	case r.Method == http.MethodPost && resource == "tasks" && taskID != "" && action == "result":
		s.completeSessionTask(w, r, sessionID, taskID)
	case r.Method == http.MethodPost && resource == "artifacts":
		s.upsertSessionArtifact(w, r, sessionID)
	case r.Method == http.MethodGet && resource == "artifacts":
		s.listSessionArtifacts(w, r, sessionID)
	case r.Method == http.MethodPost && resource == "close":
		s.closeSession(w, r, sessionID)
	default:
		writeProtocolError(w, http.StatusNotFound, protocolHTTPError(controlplane.ErrSessionClosed, "unknown session endpoint", false))
	}
}

func (s Server) sessionAgentEventsAfter(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "auditor role is required", false))
		return
	}
	afterSeq, err := parseOptionalUint(r.URL.Query().Get("after_seq"), "after_seq")
	if err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrStaleCursor, err.Error(), true))
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"), "limit")
	if err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrTooManyEvents, err.Error(), true))
		return
	}
	events, replay, err := s.Gateway.SessionEventsAfterForAgent(sessionID, afterSeq, limit)
	if err != nil {
		writeControlPlaneErrorWithReplay(w, err, replay)
		return
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": events, "snapshot_required": replay.SnapshotRequired, "snapshot_seq": replay.SnapshotSeq,
		"last_seq": replay.LastSeq, "retry_after_ms": replay.RetryAfterMS, "reconnecting": replay.Reconnecting,
		"status": session.DeriveStatus(),
	})
}

func (s Server) getSessionSnapshot(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "auditor role is required", false))
		return
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": session.Snapshot()})
}

func (s Server) joinSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	var spec controlplane.EndpointSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	session, endpoint, lease, err := s.Gateway.JoinSession(sessionID, spec)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":  session,
		"endpoint": endpoint,
		"lease":    lease,
	})
}

func (s Server) sessionEventsAfter(w http.ResponseWriter, r *http.Request, sessionID string) {
	afterSeq, err := parseOptionalUint(r.URL.Query().Get("after_seq"), "after_seq")
	if err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrStaleCursor, err.Error(), true))
		return
	}
	receivedSeq, err := parseOptionalUint(r.URL.Query().Get("received_seq"), "received_seq")
	if err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrStaleCursor, err.Error(), true))
		return
	}
	processedSeq, err := parseOptionalUint(r.URL.Query().Get("processed_seq"), "processed_seq")
	if err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrStaleCursor, err.Error(), true))
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"), "limit")
	if err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrTooManyEvents, err.Error(), true))
		return
	}
	events, lease, replay, err := s.Gateway.SessionEventsAfter(sessionID, controlplane.EventCursor{
		EndpointID:   r.URL.Query().Get("endpoint_id"),
		LeaseSecret:  extractBearerToken(r),
		AfterSeq:     afterSeq,
		ReceivedSeq:  receivedSeq,
		ProcessedSeq: processedSeq,
	}, limit)
	if err != nil {
		writeControlPlaneErrorWithReplay(w, err, replay)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events":            events,
		"lease":             lease,
		"snapshot_required": replay.SnapshotRequired,
		"snapshot_seq":      replay.SnapshotSeq,
		"last_seq":          replay.LastSeq,
		"retry_after_ms":    replay.RetryAfterMS,
		"reconnecting":      replay.Reconnecting,
	})
}

func (s Server) appendSessionEvent(w http.ResponseWriter, r *http.Request, sessionID string) {
	var event controlplane.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	event.FromEndpointID = strings.TrimSpace(event.FromEndpointID)
	if event.FromEndpointID == "" {
		writeProtocolError(w, http.StatusUnauthorized, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "event source endpoint is required", false))
		return
	}
	if event.FromEndpointID == "agent" || event.FromEndpointID == "gateway" || strings.HasPrefix(event.FromEndpointID, "gateway.") {
		if !s.authorizeOperator(r, operatorauth.RoleOperator) {
			writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "operator role is required for reserved event sources", false))
			return
		}
	} else {
		if err := s.Gateway.ValidateSessionLease(sessionID, event.FromEndpointID, extractBearerToken(r)); err != nil {
			writeControlPlaneError(w, err)
			return
		}
	}
	appended, err := s.Gateway.AppendSessionEvent(sessionID, event)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"event": appended})
}

func (s Server) submitSessionTask(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "operator role is required", false))
		return
	}
	var spec controlplane.TaskSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	task, event, err := s.Gateway.SubmitSessionTask(sessionID, spec)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "event": event})
}

func (s Server) completeSessionTask(w http.ResponseWriter, r *http.Request, sessionID, taskID string) {
	var result map[string]any
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	task, event, err := s.Gateway.CompleteSessionTask(sessionID, taskID, result)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task, "event": event})
}

func (s Server) upsertSessionArtifact(w http.ResponseWriter, r *http.Request, sessionID string) {
	var ref controlplane.ArtifactRef
	if err := json.NewDecoder(r.Body).Decode(&ref); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	artifact, event, err := s.Gateway.UpsertSessionArtifact(sessionID, ref)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"artifact": artifact, "event": event})
}

func (s Server) listSessionArtifacts(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "auditor role is required", false))
		return
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": session.Artifacts, "status": session.DeriveStatus()})
}

func (s Server) closeSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "operator role is required", false))
		return
	}
	session, event, err := s.Gateway.CloseSession(sessionID)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session": session, "event": event})
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
		AutoActivate bool              `json:"auto_activate"`
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
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && (value != "" || key == gateway.TicketMetadataGatewayCandidates) {
			metadata[key] = value
		}
	}
	if req.AutoActivate {
		metadata["auto_activate"] = "attended-temporary"
	}
	if req.Mode == model.HostModeAttendedTemporary {
		req.Capabilities = policy.MergeTemporaryCapabilities(req.Capabilities)
	}
	authority, err := ticketGatewayAuthorityFromMetadata(r, metadata)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(authority.Candidates) > 0 {
		content, marshalErr := json.Marshal(authority.Candidates)
		if marshalErr != nil {
			writeError(w, http.StatusBadRequest, "ticket gateway candidate metadata is invalid")
			return
		}
		metadata[gateway.TicketMetadataGatewayCandidates] = string(content)
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
		"joinUrl":               authority.BaseURL + "/join/" + ticket.Code,
		"manifestUrl":           manifestURLForTicketBase(authority.BaseURL, ticket.Code),
		"manifestRootPublicKey": manifestRoot,
	})
}

func (s Server) ticketSubresource(w http.ResponseWriter, r *http.Request) {
	code, resource, ok := splitTicketSubresource(r.URL.Path)
	if !ok || resource != "manifest" {
		writeError(w, http.StatusNotFound, "unknown ticket endpoint")
		return
	}
	authority, err := s.storedTicketGatewayAuthority(r, code)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	baseURL := authority.BaseURL
	joinURL := baseURL + "/join/" + code
	catalog := s.connectionEntryPackageCatalog(joinURL, baseURL)
	manifest, err := s.Gateway.JoinManifestWithPackageCatalog(code, baseURL, joinURL, authority.Candidates, catalog)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	timeProof, err := s.Gateway.JoinManifestTimeProof(manifest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"manifest":              manifest,
		"gateway_time_proof":    timeProof,
		"manifestRootPublicKey": manifestRootPublicKey(s.Gateway.ManifestRoot()),
	})
}

func (s Server) join(w http.ResponseWriter, r *http.Request) {
	code, resource, ok := splitJoinPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown join endpoint")
		return
	}
	authority, err := s.storedTicketGatewayAuthority(r, code)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	manifestURL := manifestURLForTicketBase(authority.BaseURL, code)
	manifestRoot := manifestRootPublicKey(s.Gateway.ManifestRoot())
	if resource == "bootstrap.ps1" {
		if err := s.Gateway.ValidatePowerShellBootstrapTicket(code); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.writePowerShellBootstrap(w, code, authority.BaseURL, manifestURL, manifestRoot)
		return
	}
	if _, err := s.Gateway.JoinManifestWithGatewayCandidates(code, authority.BaseURL, authority.BaseURL+"/join/"+code, authority.Candidates); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch resource {
	case "":
		s.joinPage(w, r, code, authority.BaseURL, manifestURL)
	case "bootstrap.sh":
		s.writeShellBootstrap(w, code, authority.BaseURL, manifestURL, manifestRoot)
	default:
		writeError(w, http.StatusNotFound, "unknown join resource")
	}
}

func (s Server) connectionEntryPackageCatalog(joinURL, gatewayURL string) model.ConnectionEntryPackageCatalog {
	catalog := model.NewConnectionEntryPackageCatalog(joinURL)
	assetContracts := s.helperAssetContracts(gatewayURL)
	for i := range catalog.Candidates {
		if contract, ok := assetContracts[catalog.Candidates[i].ID]; ok {
			catalog.Candidates[i].HelperAsset = contract
		}
	}
	return catalog
}

func (s Server) helperAssetContracts(gatewayURL string) map[string]model.HelperAssetContract {
	contracts := map[string]model.HelperAssetContract{}
	for _, asset := range []struct {
		id   string
		name string
		path string
	}{
		{id: "windows-amd64", name: "rdev-windows-amd64.exe", path: s.Assets.RdevWindowsAMD64Path},
		{id: "darwin-arm64", name: "rdev-darwin-arm64", path: s.Assets.RdevDarwinARM64Path},
		{id: "darwin-amd64", name: "rdev-darwin-amd64", path: s.Assets.RdevDarwinAMD64Path},
		{id: "linux-amd64", name: "rdev-linux-amd64", path: s.Assets.RdevLinuxAMD64Path},
		{id: "linux-arm64", name: "rdev-linux-arm64", path: s.Assets.RdevLinuxARM64Path},
	} {
		contracts[asset.id] = helperAssetContract(gatewayURL, asset.name, asset.path)
	}
	return contracts
}

func helperAssetContract(gatewayURL, assetName, path string) model.HelperAssetContract {
	assetURL := strings.TrimRight(strings.TrimSpace(gatewayURL), "/") + "/assets/" + assetName
	contract := model.HelperAssetContract{
		Name:      assetName,
		SHA256URL: assetURL + ".sha256",
		Mirrors: []model.HelperAssetMirror{
			{URL: assetURL + ".gz", Kind: "gateway-asset", Compression: "gzip", Recommended: true},
			{URL: assetURL, Kind: "gateway-asset"},
		},
		BootstrapCanRunSessionTasks:            false,
		RequiresFullRunnerBeforeSessionTaskRun: true,
	}
	if strings.TrimSpace(path) != "" {
		if sum, err := fileSHA256(path); err == nil {
			contract.ExpectedSHA256 = "sha256:" + sum
		}
	}
	return contract
}

type ticketGatewayAuthority struct {
	BaseURL    string
	Candidates []model.JoinManifestGatewayCandidate
}

func (s Server) storedTicketGatewayAuthority(r *http.Request, code string) (ticketGatewayAuthority, error) {
	ticket, ok := s.Gateway.TicketForCode(code)
	if !ok {
		return ticketGatewayAuthority{}, errors.New("ticket gateway authority is unavailable")
	}
	return ticketGatewayAuthorityFromMetadata(r, ticket.Metadata)
}

func ticketGatewayAuthorityFromMetadata(r *http.Request, metadata map[string]string) (ticketGatewayAuthority, error) {
	raw, present := metadata[gateway.TicketMetadataGatewayCandidates]
	if !present {
		return ticketGatewayAuthority{BaseURL: strings.TrimRight(requestBaseURL(r), "/")}, nil
	}
	var candidates []model.JoinManifestGatewayCandidate
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &candidates) != nil {
		return ticketGatewayAuthority{}, errors.New("ticket gateway candidate metadata is invalid")
	}
	validated := make([]model.JoinManifestGatewayCandidate, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		normalized, ok := normalizeGatewayBase(candidate.URL)
		if !ok || seen[normalized] {
			continue
		}
		seen[normalized] = true
		candidate.URL = normalized
		validated = append(validated, candidate)
	}
	if len(validated) == 0 {
		return ticketGatewayAuthority{}, errors.New("ticket gateway candidate metadata has no valid public HTTPS or private LAN candidate")
	}
	selected := validated[0]
	matchedRequest := false
	for _, candidate := range validated {
		if requestAuthorityMatchesGatewayCandidate(r, candidate.URL) {
			selected = candidate
			matchedRequest = true
			break
		}
	}
	if !matchedRequest {
		for _, candidate := range validated {
			if candidate.Recommended {
				selected = candidate
				break
			}
		}
	}
	return ticketGatewayAuthority{BaseURL: selected.URL, Candidates: validated}, nil
}

func normalizePublicHTTPSGatewayBase(value string) (string, bool) {
	return normalizeGatewayBase(value)
}

func normalizeGatewayBase(value string) (string, bool) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.User != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(value, "#") || parsed.RawPath != "" {
		return "", false
	}
	privateLAN := false
	if address, err := netip.ParseAddr(parsed.Hostname()); err == nil {
		privateLAN = address.IsPrivate()
	}
	if strings.HasSuffix(parsed.Hostname(), ".") || (!privateLAN && tunnel.ValidatePublicCandidate(tunnel.Candidate{ProviderID: "ticket-metadata", URL: value}) != nil) ||
		(privateLAN && !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) ||
		(!privateLAN && (!strings.EqualFold(parsed.Scheme, "https") || parsed.Port() != "")) {
		return "", false
	}
	pathValue := strings.TrimRight(parsed.Path, "/")
	if pathValue != "" {
		if !strings.HasPrefix(pathValue, "/") || strings.Contains(pathValue, `\`) || strings.Contains(pathValue, "//") {
			return "", false
		}
		for _, segment := range strings.Split(strings.TrimPrefix(pathValue, "/"), "/") {
			if segment == "" || segment == "." || segment == ".." {
				return "", false
			}
		}
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = pathValue
	parsed.RawPath = ""
	return parsed.String(), true
}

func requestAuthorityMatchesGatewayCandidate(r *http.Request, candidateURL string) bool {
	host := strings.TrimSpace(r.Host)
	if host == "" || host != r.Host {
		return false
	}
	requestAuthority, err := url.Parse("https://" + host)
	if err != nil || requestAuthority.User != nil || requestAuthority.Hostname() == "" || requestAuthority.Path != "" || requestAuthority.RawQuery != "" || requestAuthority.Fragment != "" {
		return false
	}
	if port := requestAuthority.Port(); port != "" && port != "443" {
		return false
	}
	candidate, err := url.Parse(candidateURL)
	return err == nil && strings.EqualFold(requestAuthority.Hostname(), candidate.Hostname())
}

func manifestURLForTicketBase(baseURL, code string) string {
	return strings.TrimRight(baseURL, "/") + "/v1/tickets/" + code + "/manifest"
}

func (s Server) joinPage(w http.ResponseWriter, r *http.Request, code, baseURL, manifestURL string) {
	joinBase := strings.TrimRight(baseURL, "/") + "/join/" + code
	shellCommand := "curl -fsSL " + shellQuote(joinBase+"/bootstrap.sh") + " | sh"
	powerShellCommand := "powershell -NoProfile -Command \"irm '" + powerShellSingleQuoteValue(joinBase+"/bootstrap.ps1") + "' -UseBasicParsing | iex\""
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
			StepAgent:   "Agent host का इंतजार करता है, policy की जरूरत पर authorize करता है, और scoped session tasks चलाता है।",
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
			StepAgent:   "Agent дождется host, выполнит authorization при необходимости и запустит ограниченные session tasks.",
		})
	default:
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Connect This Machine",
			Note:        "Run one command on the computer that needs help. The connection is visible, outbound-only, revocable, and scoped to this support ticket.",
			NextHeading: "What Happens Next",
			StepCheck:   `The bootstrap checks for <code>rdev</code>.`,
			StepStart:   `It starts a visible, stable attended host session with <code>--transport long-poll</code>.`,
			StepAgent:   "The Agent waits for the host, authorizes it when policy requires, and runs scoped session tasks.",
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

func (s Server) writeShellBootstrap(w http.ResponseWriter, ticketCode, baseURL, manifestURL, manifestRootPublicKey string) {
	s.setTicketBootstrapHeaders(w, ticketCode)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	rootArg := ""
	if strings.TrimSpace(manifestRootPublicKey) != "" {
		rootArg = " --manifest-root-public-key " + shellQuote(manifestRootPublicKey)
	}
	assetBase := shellQuote(strings.TrimRight(baseURL, "/") + "/assets")
	preconnectURL := shellQuote(strings.TrimRight(baseURL, "/") + "/v1/support-session/preconnect")
	_, _ = fmt.Fprintf(w, `#!/bin/sh
set -eu
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
asset=""
rdev_preconnect_url=%s
rdev_preconnect() {
  phase="$1"
  message="${2:-}"
  if command -v curl >/dev/null 2>&1; then
	    curl -fsS -m 3 -X POST "$rdev_preconnect_url" \
	      -H 'Content-Type: application/json' \
	      --data "{\"ticket_code\":\"%s\",\"phase\":\"$phase\",\"os\":\"$os\",\"arch\":\"$arch\",\"asset\":\"$asset\",\"source\":\"rdev-bootstrap-preconnect\",\"message\":\"$message\"}" >/dev/null 2>&1 || true
	  fi
	}
	rdev_curl_retry_flags="--retry 3 --retry-delay 2 --connect-timeout 10"
	if ! command -v rdev >/dev/null 2>&1; then
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
	  expected="$(curl $rdev_curl_retry_flags -fsSL %s"/${asset}.sha256")"
	  cache_base="${XDG_CACHE_HOME:-}"
	  if [ -z "$cache_base" ]; then
	    if [ -n "${HOME:-}" ]; then
	      cache_base="$HOME/.cache"
	    else
	      cache_base="${TMPDIR:-/tmp}"
	    fi
	  fi
	  cache_dir="$cache_base/remote-dev-skillkit/helpers"
	  mkdir -p "$cache_dir"
	  cache_path="$cache_dir/${asset}"
	  if [ -f "$cache_path" ]; then
	    rdev_preconnect "verifying-helper" "checking cached verified helper"
	    cache_actual="$(shasum -a 256 "$cache_path" | awk '{print $1}')"
	    if [ "$cache_actual" = "$expected" ]; then
	      rdev_preconnect "using-cached-helper" "using cached verified helper"
	      chmod 700 "$cache_path"
	      out="$cache_path"
	      rdev_cmd="$out"
	    else
	      rm -f "$cache_path"
	    fi
	  fi
	  if [ -z "${rdev_cmd:-}" ]; then
	    rdev_preconnect "downloading-helper" "downloading verified helper"
	    mkdir -p "${TMPDIR:-/tmp}/rdev-connection-entry"
	    out="${TMPDIR:-/tmp}/rdev-connection-entry/rdev"
		    echo "Downloading verified rdev helper ${asset}..."
		  gz_status="000"
		  if command -v gzip >/dev/null 2>&1; then
		    gz_status="$(curl $rdev_curl_retry_flags -fsS -o /dev/null -w "%%{http_code}" %s"/${asset}.gz" 2>/dev/null || true)"
		  fi
		  if [ "$gz_status" = "200" ]; then
		    tmp_gz="$out.gz"
		    curl $rdev_curl_retry_flags -fsSL %s"/${asset}.gz" -o "$tmp_gz"
		    gzip -dc "$tmp_gz" > "$out"
		    rm -f "$tmp_gz"
		  else
		    http_status="$(curl $rdev_curl_retry_flags -fsS -o /dev/null -w "%%{http_code}" %s"/${asset}" 2>/dev/null || true)"
		    if [ "$http_status" != "200" ]; then
		      echo "rdev helper binary not available at gateway (HTTP $http_status) — the gateway may still be starting. Wait a moment and retry." >&2
		      exit 127
		    fi
		    curl $rdev_curl_retry_flags -fsSL %s"/${asset}" -o "$out"
		  fi
	    rdev_preconnect "verifying-helper" "verifying downloaded helper"
		  actual="$(shasum -a 256 "$out" | awk '{print $1}')"
  if [ "$actual" != "$expected" ]; then
    echo "rdev helper SHA-256 mismatch" >&2
    rm -f "$out"
    exit 127
  fi
	    cp "$out" "$cache_path"
  chmod 700 "$out"
  rdev_cmd="$out"
	  fi
else
  rdev_preconnect "using-installed-helper" "using installed rdev helper"
  rdev_cmd="$(command -v rdev)"
fi
rdev_identity_base="${XDG_STATE_HOME:-}"
if [ -z "$rdev_identity_base" ]; then
  if [ -n "${HOME:-}" ]; then
    rdev_identity_base="$HOME/.local/state"
  else
    rdev_identity_base="${TMPDIR:-/tmp}"
  fi
fi
rdev_identity_dir="$rdev_identity_base/remote-dev-skillkit"
mkdir -p "$rdev_identity_dir"
rdev_identity_store="$rdev_identity_dir/host-identity.json"
echo "Starting visible Remote Dev Skillkit host session..."
echo "[rdev] Persistent support identity: $rdev_identity_store"
rdev_preconnect "starting-full-helper" "starting verified full helper"
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
rdev_permanent_exit=`+strconv.Itoa(permanentHostFailureExitCode)+`
rdev_max_retries=5
	rdev_retry_delay=5
	rdev_attempt=0
	while true; do
	  if "$rdev_cmd" host serve --manifest-url %s%s --transport long-poll --once=false --max-tasks 0 --identity-store "$rdev_identity_store"; then
	    rdev_exit=0
	  else
	    rdev_exit=$?
	  fi
	  rdev_attempt=$((rdev_attempt + 1))
	  echo "[rdev] host process exited with code $rdev_exit"
  if [ "$rdev_exit" -eq 0 ] || [ "$rdev_exit" -eq "$rdev_permanent_exit" ] || [ "$rdev_attempt" -gt "$rdev_max_retries" ]; then
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
	`, preconnectURL, ticketCode, assetBase, assetBase, assetBase, assetBase, assetBase, shellQuote(manifestURL), rootArg)
}

func (s Server) writePowerShellBootstrap(w http.ResponseWriter, ticketCode, baseURL, manifestURL, manifestRootPublicKey string) {
	s.setTicketBootstrapHeaders(w, ticketCode)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	rootArg := ""
	if strings.TrimSpace(manifestRootPublicKey) != "" {
		rootArg = " --manifest-root-public-key '" + powerShellSingleQuoteValue(manifestRootPublicKey) + "'"
	}
	assetBase := powerShellSingleQuoteValue(strings.TrimRight(baseURL, "/") + "/assets")
	preconnectURL := powerShellSingleQuoteValue(strings.TrimRight(baseURL, "/") + "/v1/support-session/preconnect")
	_, _ = fmt.Fprintf(w, `$ErrorActionPreference = 'Stop'
$asset = ''
$preconnectUrl = '%s'
function Send-RdevPreconnect([string]$phase, [string]$message) {
  try {
    $body = @{
      ticket_code = '%s'
      phase = $phase
      os = 'windows'
      arch = 'amd64'
      asset = $asset
      source = 'rdev-bootstrap-preconnect'
      message = $message
    } | ConvertTo-Json -Compress
    Invoke-WebRequest -Uri $preconnectUrl -Method Post -Body $body -ContentType 'application/json' -UseBasicParsing -TimeoutSec 3 | Out-Null
  } catch {
    Write-Host "[rdev] preconnect status update skipped: $($_.Exception.Message)"
  }
}
function Invoke-RdevWebRequestWithRetry([string]$Uri, [string]$OutFile = '', [int]$MaxAttempts = 3, [int]$DelaySeconds = 2) {
  for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
    try {
      if ([string]::IsNullOrWhiteSpace($OutFile)) {
        return Invoke-WebRequest -Uri $Uri -UseBasicParsing -ErrorAction Stop -TimeoutSec 30
      }
      Invoke-WebRequest -Uri $Uri -OutFile $OutFile -UseBasicParsing -ErrorAction Stop -TimeoutSec 30
      return
    } catch {
      if ($attempt -ge $MaxAttempts) { throw }
      Write-Host "[rdev] download attempt $attempt failed: $($_.Exception.Message). Retrying..."
      Start-Sleep -Seconds $DelaySeconds
    }
  }
}
$rdevCmd = Get-Command rdev -ErrorAction SilentlyContinue
if ($rdevCmd) {
  Send-RdevPreconnect 'using-installed-helper' 'using installed rdev helper'
  $rdevPath = $rdevCmd.Source
} else {
  if (-not [Environment]::Is64BitOperatingSystem) {
    throw "unsupported Windows architecture: 32-bit"
  }
  $asset = "rdev-windows-amd64.exe"
  $expected = (Invoke-RdevWebRequestWithRetry -Uri ('%s/' + $asset + '.sha256')).Content.Trim()
  $cacheBase = [Environment]::GetFolderPath('LocalApplicationData')
  if ([string]::IsNullOrWhiteSpace($cacheBase)) { $cacheBase = $env:TEMP }
  $cacheDir = Join-Path $cacheBase "RemoteDevSkillkit\cache\helpers"
  New-Item -ItemType Directory -Force -Path $cacheDir | Out-Null
  $cachePath = Join-Path $cacheDir $asset
  if (Test-Path -LiteralPath $cachePath) {
    Send-RdevPreconnect 'verifying-helper' 'checking cached verified helper'
    $cacheActual = (Get-FileHash -Algorithm SHA256 -Path $cachePath).Hash.ToLowerInvariant()
    if ($cacheActual -eq $expected.ToLowerInvariant()) {
      Send-RdevPreconnect 'using-cached-helper' 'using cached verified helper'
      $rdevPath = $cachePath
    } else {
      Remove-Item -Force $cachePath -ErrorAction SilentlyContinue
    }
  }
  if ([string]::IsNullOrWhiteSpace($rdevPath)) {
    Send-RdevPreconnect 'downloading-helper' 'downloading verified helper'
    $dir = Join-Path $env:TEMP "rdev-connection-entry"
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
	    $rdevPath = Join-Path $dir "rdev.exe"
	    Write-Host "Downloading verified rdev helper $asset..."
			$compressedPath = $rdevPath + ".gz"
			$usedCompressed = $false
			try {
		    Invoke-RdevWebRequestWithRetry -Uri ('%s/' + $asset + '.gz') -OutFile $compressedPath
		    $inputStream = [System.IO.File]::OpenRead($compressedPath)
	    try {
	      $outputStream = [System.IO.File]::Create($rdevPath)
	      try {
	        $gzipStream = [System.IO.Compression.GzipStream]::new($inputStream, [System.IO.Compression.CompressionMode]::Decompress)
	        try {
	          $gzipStream.CopyTo($outputStream)
	        } finally {
	          $gzipStream.Dispose()
	        }
	      } finally {
	        $outputStream.Dispose()
	      }
	    } finally {
	      $inputStream.Dispose()
	    }
	    Remove-Item -Force $compressedPath -ErrorAction SilentlyContinue
	    $usedCompressed = $true
	  } catch {
	    Remove-Item -Force $compressedPath -ErrorAction SilentlyContinue
	    Write-Host "Compressed rdev helper unavailable; falling back to uncompressed download."
	  }
		  if (-not $usedCompressed) {
		    try {
		      Invoke-RdevWebRequestWithRetry -Uri ('%s/' + $asset) -OutFile $rdevPath
		    } catch {
		    $errMsg = $_.Exception.Message
		    throw ("Failed to download rdev helper from gateway. The asset binary may not be configured yet — ensure the gateway has finished starting, then run the command again. Detail: " + $errMsg)
			    }
			  }
    Send-RdevPreconnect 'verifying-helper' 'verifying downloaded helper'
	  $actual = (Get-FileHash -Algorithm SHA256 -Path $rdevPath).Hash.ToLowerInvariant()
	  if ($actual -ne $expected.ToLowerInvariant()) {
	    Remove-Item -Force $rdevPath -ErrorAction SilentlyContinue
	    throw "rdev helper SHA-256 mismatch"
	  }
    Copy-Item -Force -Path $rdevPath -Destination $cachePath
  }
}
Write-Host "Starting visible Remote Dev Skillkit host session..."
$identityBase = [Environment]::GetFolderPath('LocalApplicationData')
if ([string]::IsNullOrWhiteSpace($identityBase)) { $identityBase = $env:TEMP }
$identityDir = Join-Path $identityBase "RemoteDevSkillkit"
New-Item -ItemType Directory -Force -Path $identityDir | Out-Null
$identityStore = Join-Path $identityDir "host-identity.json"
Write-Host "[rdev] Persistent support identity: $identityStore"
Send-RdevPreconnect 'starting-full-helper' 'starting verified full helper'
# Prevent Windows idle sleep/display sleep while rdev is running. This does not
# bypass lock-screen policy or enterprise security controls.
# SetThreadExecutionState keeps the session awake so the runner can continue to
# poll for and execute session tasks.
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
$rdevPermanentExitCode = `+strconv.Itoa(permanentHostFailureExitCode)+`
$rdevMaxRetries = 5
$rdevRetryDelaySec = 5
$rdevAttempt = 0
do {
  if ($rdevAttempt -gt 0) {
    Write-Host ("[rdev] Retrying host registration (attempt $($rdevAttempt + 1) of $($rdevMaxRetries + 1)) after ${rdevRetryDelaySec}s...")
    Start-Sleep -Seconds $rdevRetryDelaySec
  }
  & $rdevPath host serve --manifest-url '%s'%s --transport long-poll --once=false --max-tasks 0 --identity-store $identityStore
  $rdevExitCode = $LASTEXITCODE
  $rdevAttempt++
  Write-Host "[rdev] host process exited with code $rdevExitCode"
} while ($rdevExitCode -ne 0 -and $rdevExitCode -ne $rdevPermanentExitCode -and $rdevAttempt -le $rdevMaxRetries)
# Restore normal sleep policy before exiting.
try { [void][RdevSleepPrevention]::SetThreadExecutionState([RdevSleepPrevention]::ES_CONTINUOUS) } catch { }
exit $rdevExitCode
	`, preconnectURL, powerShellSingleQuoteValue(ticketCode), assetBase, assetBase, assetBase, powerShellSingleQuoteValue(manifestURL), rootArg)
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
	gzipOnly := false
	if strings.HasSuffix(name, ".gz") {
		gzipOnly = true
		name = strings.TrimSuffix(name, ".gz")
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
	if gzipOnly {
		s.serveGzipAsset(w, path)
		return
	}
	http.ServeFile(w, r, path)
}

func (s Server) serveGzipAsset(w http.ResponseWriter, path string) {
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.WriteHeader(http.StatusOK)
	zw := gzip.NewWriter(w)
	if _, err := io.Copy(zw, file); err != nil {
		_ = zw.Close()
		return
	}
	_ = zw.Close()
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
	ticket, ok := s.Gateway.TicketForCode(ticketCode)
	if !ok {
		writeError(w, http.StatusNotFound, "support session not found")
		return
	}
	authority, err := ticketGatewayAuthorityFromMetadata(r, ticket.Metadata)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var boundSession *controlplane.Session
	if ticket.SessionID != "" {
		session, sessionErr := s.Gateway.Session(ticket.SessionID)
		if sessionErr != nil || session.SourceTicketID != ticket.ID || session.JoinCode != ticket.Code {
			writeError(w, http.StatusBadRequest, "support session binding is invalid")
			return
		}
		boundSession = &session
	}
	hosts := s.Gateway.HostsForTicketCode(ticketCode, "")
	opts := supportsession.StatusOptions{
		TicketCode:  ticketCode,
		Hosts:       hosts,
		Session:     boundSession,
		Locale:      r.URL.Query().Get("locale"),
		GatewayURL:  authority.BaseURL,
		Preconnects: s.Gateway.SupportSessionPreconnects(ticketCode),
		Ticket:      &ticket,
	}
	status := supportsession.BuildStatus(opts)
	writeJSON(w, http.StatusOK, status)
}

func (s Server) supportSessionPreconnect(w http.ResponseWriter, r *http.Request) {
	var req model.SupportSessionPreconnect
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	event, err := s.Gateway.RecordSupportSessionPreconnect(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":         true,
		"preconnect": event,
	})
}

func requestBodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
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

func parseOptionalUint(raw, name string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
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

func splitSessionPath(path string) (sessionID string, resource string, taskID string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/sessions/")
	if rest == path {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		return parts[0], "", "", "", true
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		return parts[0], parts[1], "", "", true
	case len(parts) == 4 && parts[0] != "" && parts[1] == "tasks" && parts[2] != "" && parts[3] != "":
		return parts[0], parts[1], parts[2], parts[3], true
	default:
		return "", "", "", "", false
	}
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeControlPlaneError(w http.ResponseWriter, err error) {
	var protocolErr controlplane.ProtocolError
	if errors.As(err, &protocolErr) {
		writeProtocolError(w, protocolHTTPStatus(protocolErr.Code), protocolErr)
		return
	}
	writeProtocolError(w, http.StatusInternalServerError, protocolHTTPError(controlplane.ErrSessionClosed, "internal control plane error", true))
}

func writeControlPlaneErrorWithReplay(w http.ResponseWriter, err error, replay controlplane.EventReplayState) {
	var protocolErr controlplane.ProtocolError
	if errors.As(err, &protocolErr) {
		writeJSON(w, protocolHTTPStatus(protocolErr.Code), map[string]any{
			"error":             protocolErr,
			"snapshot_required": replay.SnapshotRequired,
			"snapshot_seq":      replay.SnapshotSeq,
			"last_seq":          replay.LastSeq,
			"retry_after_ms":    replay.RetryAfterMS,
			"reconnecting":      replay.Reconnecting,
		})
		return
	}
	writeControlPlaneError(w, err)
}

func writeProtocolError(w http.ResponseWriter, status int, err controlplane.ProtocolError) {
	writeJSON(w, status, map[string]any{"error": err})
}

func protocolHTTPError(code controlplane.ErrorCode, message string, recoverable bool) controlplane.ProtocolError {
	return controlplane.ProtocolError{
		SchemaVersion:   controlplane.ErrorSchemaVersion,
		Code:            code,
		Message:         message,
		Recoverable:     recoverable,
		RetryAfterMS:    500,
		UserSummary:     message,
		AgentNextAction: protocolAgentNextAction(code),
	}
}

func protocolHTTPStatus(code controlplane.ErrorCode) int {
	switch code {
	case controlplane.ErrUnauthorizedEndpoint, controlplane.ErrLeaseExpired:
		return http.StatusUnauthorized
	case controlplane.ErrInvalidJoinCode, controlplane.ErrEndpointNotFound, controlplane.ErrTaskNotFound:
		return http.StatusNotFound
	case controlplane.ErrIdempotencyConflict, controlplane.ErrTerminalSession, controlplane.ErrSessionClosed, controlplane.ErrTaskAlreadyTerminal, controlplane.ErrJoinPolicyRejected:
		return http.StatusConflict
	case controlplane.ErrPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case controlplane.ErrTooManyEvents, controlplane.ErrStaleCursor, controlplane.ErrSnapshotRequired, controlplane.ErrArtifactOffsetMismatch, controlplane.ErrChecksumMismatch, controlplane.ErrCapabilityUnavailable, controlplane.ErrAuthorityMismatch, controlplane.ErrStaleReplica:
		return http.StatusBadRequest
	default:
		return http.StatusBadRequest
	}
}

func protocolAgentNextAction(code controlplane.ErrorCode) string {
	switch code {
	case controlplane.ErrSnapshotRequired:
		return "fetch the session snapshot and resume from snapshot_seq"
	case controlplane.ErrUnauthorizedEndpoint, controlplane.ErrLeaseExpired:
		return "join, resume, or renew the endpoint lease"
	case controlplane.ErrIdempotencyConflict:
		return "reuse the original idempotent payload or choose a new idempotency key"
	case controlplane.ErrTerminalSession, controlplane.ErrSessionClosed:
		return "do not send new work to this session"
	default:
		return "inspect the structured error and retry only if recoverable"
	}
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

func (s Server) setTicketBootstrapHeaders(w http.ResponseWriter, ticketCode string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set(tunnel.TicketCodeSHA256Header, tunnel.TicketCodeSHA256(ticketCode))
	w.Header().Set("X-Rdev-Gateway-Instance", s.gatewayInstance)
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
