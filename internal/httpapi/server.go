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
	"path"
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
const layeredAssetManifestHTTPPath = "/layered-assets.json"
const layeredAssetManifestFileName = "layered-assets.json"

type AssetConfig struct {
	LayeredAssetManifestPath      string
	LayeredReleaseRootPublicKey   string
	LayeredReleaseVersion         string
	RdevHostWindowsAMD64Path      string
	RdevBootstrapWindowsAMD64Path string
	RdevBootstrapWindowsARM64Path string
	RdevBootstrapDarwinARM64Path  string
	RdevBootstrapDarwinAMD64Path  string
	RdevBootstrapLinuxAMD64Path   string
	RdevBootstrapLinuxARM64Path   string
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
	mux.HandleFunc("GET "+layeredAssetManifestHTTPPath, s.layeredAssetManifest)
	mux.HandleFunc("GET /assets/", s.asset)
	mux.HandleFunc("POST /v1/support-session/preconnect", s.supportSessionPreconnect)
	mux.HandleFunc("GET /v1/support-session/status", s.supportSessionStatus)
	mux.HandleFunc("GET /v1/audit", s.listAudit)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLayeredAssetTraversalAlias(r) {
			writeError(w, http.StatusNotFound, "unknown asset")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func isLayeredAssetTraversalAlias(r *http.Request) bool {
	cleanPath := path.Clean(r.URL.Path)
	for _, exactPath := range []string{
		layeredAssetManifestHTTPPath,
		"/assets/rdev-host-windows-amd64.exe",
		"/assets/rdev-host-windows-amd64.exe.sha256",
	} {
		if cleanPath == exactPath && r.URL.Path != exactPath {
			return true
		}
	}
	return false
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
	wait, err := parseLongPollWait(r)
	if err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrTooManyEvents, err.Error(), true))
		return
	}
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
	cursor := controlplane.EventCursor{
		EndpointID:   r.URL.Query().Get("endpoint_id"),
		LeaseSecret:  extractBearerToken(r),
		AfterSeq:     afterSeq,
		ReceivedSeq:  receivedSeq,
		ProcessedSeq: processedSeq,
	}
	if wait > 0 {
		preview, currentLease, peekReplay, peekErr := s.Gateway.PeekSessionEventsAfter(sessionID, cursor, limit)
		if peekErr != nil {
			writeControlPlaneErrorWithReplay(w, peekErr, peekReplay)
			return
		}
		deadline := time.Now().Add(wait)
		if !currentLease.RenewAfter.IsZero() {
			renewalDeadline := currentLease.RenewAfter.Add(-time.Second)
			if renewalDeadline.Before(deadline) {
				deadline = renewalDeadline
			}
		}
		if len(preview) == 0 && time.Now().Before(deadline) {
			timer := time.NewTimer(time.Until(deadline))
			ticker := time.NewTicker(100 * time.Millisecond)
		waitLoop:
			for {
				select {
				case <-r.Context().Done():
					timer.Stop()
					ticker.Stop()
					return
				case <-timer.C:
					break waitLoop
				case <-ticker.C:
					preview, _, peekReplay, peekErr = s.Gateway.PeekSessionEventsAfter(sessionID, cursor, limit)
					if peekErr != nil {
						timer.Stop()
						ticker.Stop()
						writeControlPlaneErrorWithReplay(w, peekErr, peekReplay)
						return
					}
					if len(preview) > 0 {
						timer.Stop()
						break waitLoop
					}
				}
			}
			ticker.Stop()
		}
	}
	events, lease, replay, err := s.Gateway.SessionEventsAfter(sessionID, cursor, limit)
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
		{id: "windows-amd64", name: "rdev-bootstrap-windows-amd64.exe", path: s.Assets.RdevBootstrapWindowsAMD64Path},
		{id: "windows-arm64", name: "rdev-bootstrap-windows-arm64.exe", path: s.Assets.RdevBootstrapWindowsARM64Path},
		{id: "darwin-arm64", name: "rdev-bootstrap-darwin-arm64", path: s.Assets.RdevBootstrapDarwinARM64Path},
		{id: "darwin-amd64", name: "rdev-bootstrap-darwin-amd64", path: s.Assets.RdevBootstrapDarwinAMD64Path},
		{id: "linux-amd64", name: "rdev-bootstrap-linux-amd64", path: s.Assets.RdevBootstrapLinuxAMD64Path},
		{id: "linux-arm64", name: "rdev-bootstrap-linux-arm64", path: s.Assets.RdevBootstrapLinuxARM64Path},
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
			StepCheck:             `启动脚本会下载并校验 <code>rdev-bootstrap</code>。`,
			StepStart:             `<code>rdev-bootstrap</code> 会校验签名 core，并只启动一个使用自动通道选择的可见协助式主机会话。`,
			StepAgent:             "Agent 会等待主机上线，在策略需要时完成批准，然后运行受限的修复任务。",
		})
	case "es":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Conectar Esta Maquina",
			Note:        "Ejecuta un comando en el equipo que necesita ayuda. La conexion es visible, solo saliente, revocable y limitada a este ticket.",
			NextHeading: "Que pasa despues",
			StepCheck:   `El script descarga y verifica <code>rdev-bootstrap</code>.`,
			StepStart:   `<code>rdev-bootstrap</code> verifica el core firmado e inicia una sola sesion visible con seleccion automatica de canal.`,
			StepAgent:   "El Agent espera el host, lo aprueba si la politica lo requiere y ejecuta trabajos de reparacion limitados.",
		})
	case "fr":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Connecter Cette Machine",
			Note:        "Executez une commande sur l'ordinateur a aider. La connexion est visible, sortante uniquement, revocable et limitee a ce ticket.",
			NextHeading: "Et ensuite",
			StepCheck:   `Le script telecharge et verifie <code>rdev-bootstrap</code>.`,
			StepStart:   `<code>rdev-bootstrap</code> verifie le core signe et demarre une seule session visible avec selection automatique du canal.`,
			StepAgent:   "L'Agent attend le host, l'approuve si la politique l'exige, puis execute des reparations limitees.",
		})
	case "de":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Diese Maschine Verbinden",
			Note:        "Fuhre einen Befehl auf dem Computer aus, der Hilfe braucht. Die Verbindung ist sichtbar, nur ausgehend, widerrufbar und auf dieses Ticket begrenzt.",
			NextHeading: "Was als Nachstes passiert",
			StepCheck:   `Das Skript ladt <code>rdev-bootstrap</code> herunter und pruft es.`,
			StepStart:   `<code>rdev-bootstrap</code> pruft den signierten Core und startet genau eine sichtbare Sitzung mit automatischer Kanalwahl.`,
			StepAgent:   "Der Agent wartet auf den Host, genehmigt ihn falls erforderlich und startet begrenzte Reparaturjobs.",
		})
	case "ja":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "このマシンを接続",
			Note:        "サポートが必要なコンピューターで 1 つのコマンドを実行します。接続は可視、アウトバウンドのみ、取り消し可能で、このサポートチケットに限定されます。",
			NextHeading: "次に行われること",
			StepCheck:   `スクリプトは <code>rdev-bootstrap</code> をダウンロードして検証します。`,
			StepStart:   `<code>rdev-bootstrap</code> は署名済み core を検証し、自動チャネル選択で可視セッションを 1 つだけ開始します。`,
			StepAgent:   "Agent はホストを待ち、ポリシーが必要とする場合に承認し、限定された修復ジョブを実行します。",
		})
	case "ko":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "이 머신 연결",
			Note:        "도움이 필요한 컴퓨터에서 명령 하나를 실행합니다. 연결은 보이는 방식이며, 아웃바운드 전용이고, 철회 가능하며, 이 지원 티켓 범위로 제한됩니다.",
			NextHeading: "다음 단계",
			StepCheck:   `스크립트가 <code>rdev-bootstrap</code> 을 다운로드하고 검증합니다.`,
			StepStart:   `<code>rdev-bootstrap</code> 이 서명된 core 를 검증하고 자동 채널 선택으로 보이는 세션 하나만 시작합니다.`,
			StepAgent:   "Agent 는 호스트를 기다리고, 정책상 필요하면 승인한 뒤 제한된 복구 작업을 실행합니다.",
		})
	case "pt-BR":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Conectar Esta Maquina",
			Note:        "Execute um comando no computador que precisa de ajuda. A conexao e visivel, somente de saida, revogavel e limitada a este ticket.",
			NextHeading: "O que acontece depois",
			StepCheck:   `O script baixa e verifica <code>rdev-bootstrap</code>.`,
			StepStart:   `<code>rdev-bootstrap</code> verifica o core assinado e inicia uma unica sessao visivel com selecao automatica de canal.`,
			StepAgent:   "O Agent aguarda o host, aprova quando a politica exige e executa tarefas de reparo limitadas.",
		})
	case "hi":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "इस मशीन को कनेक्ट करें",
			Note:        "जिस कंप्यूटर को मदद चाहिए उस पर एक कमांड चलाएं। कनेक्शन दिखने वाला, केवल outbound, revoke करने योग्य, और इस support ticket तक सीमित है।",
			NextHeading: "आगे क्या होगा",
			StepCheck:   `स्क्रिप्ट <code>rdev-bootstrap</code> डाउनलोड करके सत्यापित करती है।`,
			StepStart:   `<code>rdev-bootstrap</code> signed core सत्यापित करके automatic channel selection के साथ केवल एक visible session शुरू करता है।`,
			StepAgent:   "Agent host का इंतजार करता है, policy की जरूरत पर authorize करता है, और scoped session tasks चलाता है।",
		})
	case "ar":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "توصيل هذا الجهاز",
			Note:        "شغّل أمرا واحدا على الكمبيوتر الذي يحتاج إلى مساعدة. الاتصال ظاهر، صادر فقط، قابل للإلغاء، ومحدود بتذكرة الدعم هذه.",
			NextHeading: "ماذا يحدث بعد ذلك",
			StepCheck:   `يقوم البرنامج النصي بتنزيل <code>rdev-bootstrap</code> والتحقق منه.`,
			StepStart:   `يتحقق <code>rdev-bootstrap</code> من core الموقع ويبدأ جلسة مرئية واحدة مع اختيار تلقائي للقناة.`,
			StepAgent:   "ينتظر Agent ظهور host، ويوافق عليه عند الحاجة حسب السياسة، ثم يشغل مهام إصلاح محددة النطاق.",
		})
	case "ru":
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Подключить Эту Машину",
			Note:        "Выполните одну команду на компьютере, которому нужна помощь. Подключение видимое, только исходящее, отзывное и ограничено этим тикетом.",
			NextHeading: "Что будет дальше",
			StepCheck:   `Сценарий загружает и проверяет <code>rdev-bootstrap</code>.`,
			StepStart:   `<code>rdev-bootstrap</code> проверяет подписанный core и запускает один видимый сеанс с автоматическим выбором канала.`,
			StepAgent:   "Agent дождется host, выполнит authorization при необходимости и запустит ограниченные session tasks.",
		})
	default:
		return withDefaults(joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Connect This Machine",
			Note:        "Run one command on the computer that needs help. The connection is visible, outbound-only, revocable, and scoped to this support ticket.",
			NextHeading: "What Happens Next",
			StepCheck:   `The script downloads and verifies <code>rdev-bootstrap</code>.`,
			StepStart:   `<code>rdev-bootstrap</code> verifies the signed core and starts exactly one visible session with automatic channel selection.`,
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
	_, _ = fmt.Fprintf(w, `#!/bin/sh
set -eu
bootstrap_base=%s
layered_manifest_url=%s
release_root=%s
release_version=%s
join_manifest_url=%s
join_manifest_root=%s
gateway_url=%s

case "$bootstrap_base:$layered_manifest_url:$join_manifest_url:$gateway_url" in
  *http://*|*ftp://*) echo "Connection Entry requires HTTPS assets and manifests." >&2; exit 78 ;;
esac
if [ -z "$release_root" ] || [ -z "$release_version" ] || [ -z "$join_manifest_root" ]; then
  echo "Signed Connection Entry release metadata is unavailable." >&2
  exit 78
fi
command -v curl >/dev/null 2>&1 || { echo "curl is required to obtain rdev-bootstrap." >&2; exit 127; }

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
bootstrap_asset="rdev-bootstrap-$os-$arch"
bootstrap_url="$bootstrap_base/$bootstrap_asset"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/rdev-bootstrap.XXXXXX")"
chmod 700 "$work_dir"
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM
bootstrap_path="$work_dir/$bootstrap_asset"
expected="$(curl --proto '=https' --tlsv1.2 --fail --silent --show-error --connect-timeout 10 --retry 3 "$bootstrap_url.sha256" | awk 'NR == 1 { print $1 }')"
case "$expected" in
  [0-9a-fA-F][0-9a-fA-F]*) ;;
  *) echo "rdev-bootstrap checksum is invalid." >&2; exit 78 ;;
esac
curl --proto '=https' --tlsv1.2 --fail --silent --show-error --connect-timeout 10 --retry 3 "$bootstrap_url" -o "$bootstrap_path"
if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$bootstrap_path" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$bootstrap_path" | awk '{print $1}')"
else
  echo "SHA-256 verification tool is required." >&2
  exit 127
fi
if [ "$(printf '%%s' "$actual" | tr '[:upper:]' '[:lower:]')" != "$(printf '%%s' "$expected" | tr '[:upper:]' '[:lower:]')" ]; then
  echo "rdev-bootstrap SHA-256 mismatch." >&2
  exit 78
fi
chmod 700 "$bootstrap_path"
cache_base="${XDG_CACHE_HOME:-${HOME:?HOME is required}/.cache}"
cache_dir="$cache_base/RemoteDevSkillkit/cache"
state_base="${XDG_STATE_HOME:-${HOME}/.local/state}"
identity_dir="$state_base/RemoteDevSkillkit"
mkdir -p "$cache_dir" "$identity_dir"
chmod 700 "$cache_dir" "$identity_dir"
identity_store="$identity_dir/host-identity.json"

set +e
"$bootstrap_path" layered-run \
  --manifest-url "$layered_manifest_url" \
  --root-public-key "$release_root" \
  --expected-release-version "$release_version" \
  --platform "$os/$arch" \
  --cache-dir "$cache_dir" \
  --mode temporary \
  -- \
  --mode temporary \
  --gateway "$gateway_url" \
  --manifest-url "$join_manifest_url" \
  --manifest-root-public-key "$join_manifest_root" \
  --transport auto \
  --once=false \
  --max-tasks 0 \
  --identity-store "$identity_store"
status=$?
set -e
exit "$status"
`, shellQuote(strings.TrimRight(baseURL, "/")+"/assets"), shellQuote(strings.TrimRight(baseURL, "/")+layeredAssetManifestHTTPPath), shellQuote(s.Assets.LayeredReleaseRootPublicKey), shellQuote(s.Assets.LayeredReleaseVersion), shellQuote(manifestURL), shellQuote(manifestRootPublicKey), shellQuote(baseURL))
}

func (s Server) writePowerShellBootstrap(w http.ResponseWriter, ticketCode, baseURL, manifestURL, manifestRootPublicKey string) {
	s.setTicketBootstrapHeaders(w, ticketCode)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `$ErrorActionPreference = 'Stop'
$bootstrapBase = '%s'
$layeredManifestUrl = '%s'
$releaseRoot = '%s'
$releaseVersion = '%s'
$joinManifestUrl = '%s'
$joinManifestRoot = '%s'
$gatewayUrl = '%s'

foreach ($rawUrl in @($bootstrapBase, $layeredManifestUrl, $joinManifestUrl, $gatewayUrl)) {
  $uri = [Uri]$rawUrl
  if ($uri.Scheme -ne 'https' -or -not [string]::IsNullOrEmpty($uri.UserInfo) -or -not [string]::IsNullOrEmpty($uri.Query) -or -not [string]::IsNullOrEmpty($uri.Fragment)) {
    throw 'Connection Entry requires HTTPS URLs without credentials, query strings, or fragments.'
  }
}
if ([string]::IsNullOrWhiteSpace($releaseRoot) -or [string]::IsNullOrWhiteSpace($releaseVersion) -or [string]::IsNullOrWhiteSpace($joinManifestRoot)) {
  throw 'Signed Connection Entry release metadata is unavailable.'
}
if (-not [Environment]::Is64BitOperatingSystem) {
  throw 'Unsupported Windows architecture.'
}
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "rdev-bootstrap-windows-$arch.exe"
$bootstrapUrl = "$bootstrapBase/$asset"
$cacheDir = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'RemoteDevSkillkit\cache'
$downloadRoot = Join-Path $cacheDir 'bootstrap-download'
$downloadDir = Join-Path $downloadRoot ([Guid]::NewGuid().ToString('N'))
$bootstrapPath = Join-Path $downloadDir 'rdev-bootstrap.exe'
$attemptDir = Join-Path (Join-Path $cacheDir 'attempts') ([Guid]::NewGuid().ToString('N'))

function Protect-RdevPath([string]$Path) {
  $userSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
  $icacls = Join-Path $env:SystemRoot 'System32\icacls.exe'
  & $icacls $Path '/inheritance:r' '/grant:r' "*$userSid:(OI)(CI)F" '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "Failed to protect Connection Entry path: $Path" }
}

[IO.Directory]::CreateDirectory($cacheDir) | Out-Null
[IO.Directory]::CreateDirectory($downloadRoot) | Out-Null
[IO.Directory]::CreateDirectory($downloadDir) | Out-Null
Protect-RdevPath $cacheDir
Protect-RdevPath $downloadRoot
Protect-RdevPath $downloadDir

$handler = [System.Net.Http.HttpClientHandler]::new()
$handler.AllowAutoRedirect = $false
$handler.UseDefaultCredentials = $false
$client = [System.Net.Http.HttpClient]::new($handler)
$client.Timeout = [TimeSpan]::FromSeconds(30)
try {
  $checksumResponse = $client.GetAsync("$bootstrapUrl.sha256").GetAwaiter().GetResult()
  if (-not $checksumResponse.IsSuccessStatusCode) { throw 'rdev-bootstrap checksum download failed.' }
  $expected = ($checksumResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult() -split '\s+')[0].ToLowerInvariant()
  if ($expected -notmatch '^[0-9a-f]{64}$') { throw 'rdev-bootstrap checksum is invalid.' }

  $binaryResponse = $client.GetAsync($bootstrapUrl).GetAwaiter().GetResult()
  if (-not $binaryResponse.IsSuccessStatusCode) { throw 'rdev-bootstrap download failed.' }
  $bytes = $binaryResponse.Content.ReadAsByteArrayAsync().GetAwaiter().GetResult()
  [IO.File]::WriteAllBytes($bootstrapPath, $bytes)
  $actual = (Get-FileHash -LiteralPath $bootstrapPath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $expected) { throw 'rdev-bootstrap SHA-256 mismatch.' }
  Protect-RdevPath $bootstrapPath

  & $bootstrapPath layered-run attempt-check --attempt-dir $attemptDir --launcher powershell --create
  if ($LASTEXITCODE -ne 0) { throw 'rdev-bootstrap attempt initialization failed.' }
  $bootstrapArgs = @(
    'layered-run',
    '--manifest-url', $layeredManifestUrl,
    '--root-public-key', $releaseRoot,
    '--expected-release-version', $releaseVersion,
    '--platform', "windows/$arch",
    '--cache-dir', $cacheDir,
    '--attempt-dir', $attemptDir,
    '--launcher', 'powershell',
    '--mode', 'temporary',
    '--',
    '--mode', 'temporary',
    '--gateway', $gatewayUrl,
    '--manifest-url', $joinManifestUrl,
    '--manifest-root-public-key', $joinManifestRoot,
    '--transport', 'auto',
    '--once=false',
    '--max-tasks', '0'
  )
  & $bootstrapPath @bootstrapArgs
  exit $LASTEXITCODE
} finally {
  $client.Dispose()
  $handler.Dispose()
  Remove-Item -LiteralPath $downloadDir -Recurse -Force -ErrorAction SilentlyContinue
}
`, powerShellSingleQuoteValue(strings.TrimRight(baseURL, "/")+"/assets"), powerShellSingleQuoteValue(strings.TrimRight(baseURL, "/")+layeredAssetManifestHTTPPath), powerShellSingleQuoteValue(s.Assets.LayeredReleaseRootPublicKey), powerShellSingleQuoteValue(s.Assets.LayeredReleaseVersion), powerShellSingleQuoteValue(manifestURL), powerShellSingleQuoteValue(manifestRootPublicKey), powerShellSingleQuoteValue(baseURL))
}

func (s Server) asset(w http.ResponseWriter, r *http.Request) {
	const windowsHostAssetPath = "/assets/rdev-host-windows-amd64.exe"
	if strings.HasPrefix(r.URL.Path, windowsHostAssetPath) {
		s.rdevHostWindowsAMD64Asset(w, r)
		return
	}
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
		writeError(w, http.StatusInternalServerError, "asset is unavailable")
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

func (s Server) layeredAssetManifest(w http.ResponseWriter, r *http.Request) {
	if !exactAssetRequest(r, layeredAssetManifestHTTPPath) {
		writeError(w, http.StatusNotFound, "unknown asset")
		return
	}
	path, ok := configuredAssetPath(s.Assets.LayeredAssetManifestPath)
	if !ok {
		writeError(w, http.StatusNotFound, "asset is not configured")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset is unavailable")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusInternalServerError, "asset is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	http.ServeContent(w, r, layeredAssetManifestFileName, info.ModTime(), file)
}

func exactAssetRequest(r *http.Request, path string) bool {
	return r.URL.EscapedPath() == path &&
		r.URL.RawQuery == "" &&
		!r.URL.ForceQuery &&
		r.URL.Fragment == ""
}

func (s Server) rdevHostWindowsAMD64Asset(w http.ResponseWriter, r *http.Request) {
	const assetPath = "/assets/rdev-host-windows-amd64.exe"
	shaOnly := false
	switch {
	case exactAssetRequest(r, assetPath):
	case exactAssetRequest(r, assetPath+".sha256"):
		shaOnly = true
	default:
		writeError(w, http.StatusNotFound, "unknown asset")
		return
	}
	path, ok := configuredAssetPath(s.Assets.RdevHostWindowsAMD64Path)
	if !ok {
		writeError(w, http.StatusNotFound, "asset is not configured")
		return
	}
	sum, err := fileSHA256(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset is unavailable")
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

func (s Server) serveGzipAsset(w http.ResponseWriter, path string) {
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset is unavailable")
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
	case "rdev-bootstrap-windows-amd64.exe":
		return configuredAssetPath(s.Assets.RdevBootstrapWindowsAMD64Path)
	case "rdev-bootstrap-windows-arm64.exe":
		return configuredAssetPath(s.Assets.RdevBootstrapWindowsARM64Path)
	case "rdev-bootstrap-darwin-arm64":
		return configuredAssetPath(s.Assets.RdevBootstrapDarwinARM64Path)
	case "rdev-bootstrap-darwin-amd64":
		return configuredAssetPath(s.Assets.RdevBootstrapDarwinAMD64Path)
	case "rdev-bootstrap-linux-amd64":
		return configuredAssetPath(s.Assets.RdevBootstrapLinuxAMD64Path)
	case "rdev-bootstrap-linux-arm64":
		return configuredAssetPath(s.Assets.RdevBootstrapLinuxARM64Path)
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
