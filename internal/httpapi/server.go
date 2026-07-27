package httpapi

import (
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
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

type sessionTaskRequest struct {
	controlplane.TaskSpec
	EngineeringTask any `json:"engineering_task"`
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

	mux.HandleFunc("GET /v1/trust", s.trust)
	mux.HandleFunc("GET /v1/trust-bundle", s.getTrustBundle)

	mux.HandleFunc("POST /v1/trust-bundle", s.updateTrustBundle)
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("POST /v1/session-joins", s.joinSessionByCode)
	mux.HandleFunc("GET /v1/sessions/", s.sessionRoute)
	mux.HandleFunc("POST /v1/sessions/", s.sessionRoute)

	mux.HandleFunc("GET "+layeredAssetManifestHTTPPath, s.layeredAssetManifest)
	mux.HandleFunc("GET /assets/", s.asset)

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
	case r.Method == http.MethodPost && resource == "tasks" && taskID != "" && action == "resume":
		s.resumeSessionTask(w, r, sessionID, taskID)
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
	var request sessionTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	spec := request.TaskSpec
	if err := normalizeEngineeringTaskSpec(&spec, request.EngineeringTask); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrInvalidTask, err.Error(), false))
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

func normalizeEngineeringTaskSpec(spec *controlplane.TaskSpec, topLevel any) error {
	if spec == nil {
		return fmt.Errorf("task spec is required")
	}
	if topLevel != nil {
		if spec.Payload == nil {
			spec.Payload = map[string]any{}
		}
		if _, exists := spec.Payload["engineering_task"]; exists {
			return fmt.Errorf("engineering_task must be supplied either at the top level or inside payload, not both")
		}
		spec.Payload["engineering_task"] = topLevel
	}
	raw, ok := spec.Payload["engineering_task"]
	if !ok || raw == nil {
		return nil
	}
	engineering, err := contracts.DecodeEngineeringTask(raw)
	if err != nil {
		return err
	}
	if err := engineering.ValidateForAdapter(spec.Adapter); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Intent) != "" && strings.TrimSpace(spec.Intent) != engineering.Goal {
		return fmt.Errorf("intent must match engineering_task.goal when engineering_task is present")
	}
	if strings.TrimSpace(spec.IdempotencyKey) != "" && strings.TrimSpace(spec.IdempotencyKey) != engineering.IdempotencyKey {
		return fmt.Errorf("idempotency_key must match engineering_task.idempotency_key")
	}
	if len(spec.Capabilities) > 0 && !sameCapabilitySet(spec.Capabilities, engineering.RequiredCapabilities) {
		return fmt.Errorf("capabilities must exactly match engineering_task.required_capabilities")
	}
	if len(spec.Limits) > 0 {
		return fmt.Errorf("limits must be omitted when engineering_task is present")
	}
	spec.Intent = engineering.Goal
	spec.Capabilities = append([]string(nil), engineering.RequiredCapabilities...)
	spec.Payload = engineering.TaskPayload(spec.Payload)
	spec.Limits = engineering.TaskLimits()
	spec.IdempotencyKey = engineering.IdempotencyKey
	return nil
}

func sameCapabilitySet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]bool, len(left))
	for _, value := range left {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	for _, value := range right {
		if !seen[value] {
			return false
		}
	}
	return true
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

func (s Server) resumeSessionTask(w http.ResponseWriter, r *http.Request, sessionID, taskID string) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "operator role is required", false))
		return
	}
	var request struct {
		CheckpointID   string `json:"checkpoint_id"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeProtocolError(w, http.StatusBadRequest, protocolHTTPError(controlplane.ErrPayloadTooLarge, "invalid JSON body", false))
		return
	}
	task, event, err := s.Gateway.ResumeSessionTask(sessionID, taskID, request.CheckpointID, request.IdempotencyKey)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "event": event})
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
