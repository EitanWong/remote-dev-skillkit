package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/acceptance"
	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/connectionentry"
	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/evidenceplan"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/skillkit"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
	"github.com/EitanWong/remote-dev-skillkit/internal/update"
	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
)

const protocolVersion = "2025-11-25"

const configuredGatewayProviderID = "configured-gateway"

const mcpProviderPolicyRestrictedKey = "\x00restricted"

type mcpTunnelProviderPolicyFile struct {
	AllowedProviderIDs    *[]string         `json:"allowed_provider_ids"`
	DisabledProviderIDs   []string          `json:"disabled_provider_ids"`
	RegionalEvidencePaths []string          `json:"regional_evidence_paths"`
	SSHKnownHostsPaths    map[string]string `json:"ssh_known_hosts_paths"`
}

type mcpRegionalEvidenceSummary struct {
	ProviderID string                `json:"provider_id"`
	Region     tunnel.RegionProfile  `json:"region"`
	Status     tunnel.EvidenceStatus `json:"status"`
	ObservedAt time.Time             `json:"observed_at"`
	ExpiresAt  time.Time             `json:"expires_at"`
	Fresh      bool                  `json:"fresh"`
}

type Server struct {
	Gateway *gateway.MemoryGateway
	// RemoteGateway, when non-empty, causes session/task/artifact/audit MCP tool
	// calls to be proxied to a running hosted gateway over HTTP rather than
	// operating on the local in-memory gateway.  This lets `rdev mcp serve`
	// see sessions and tasks that were registered through a foreground support-session
	// process or a separately-started `rdev gateway serve`.
	//
	// Set automatically from RDEV_HOSTED_GATEWAY_URL (or any RDEV_*_GATEWAY_URL)
	// by the CLI when those environment variables are present.
	RemoteGateway string
	httpClient    *http.Client
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return Server{Gateway: gw}
}

// NewServerWithRemoteGateway returns a Server that proxies session/task/artifact/audit
// operations to remoteURL while using the local gw for ticket creation and trust ops.
// The HTTP client uses a retrying transport so that transient TLS EOF errors
// from Cloudflare Quick Tunnels or similar reverse proxies are handled silently.
func NewServerWithRemoteGateway(gw *gateway.MemoryGateway, remoteURL string) Server {
	return Server{
		Gateway:       gw,
		RemoteGateway: strings.TrimRight(strings.TrimSpace(remoteURL), "/"),
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: retryingMCPTransport{Base: http.DefaultTransport, MaxRetries: 3},
		},
	}
}

// retryingMCPTransport wraps http.DefaultTransport and retries GET/HEAD and
// Idempotency-Key POST requests on transient connection-level errors (EOF, TLS
// truncation) that commonly occur behind Cloudflare Quick Tunnels and similar
// reverse proxies.
type retryingMCPTransport struct {
	Base       http.RoundTripper
	MaxRetries int
}

func (r retryingMCPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := r.Base
	if base == nil {
		base = http.DefaultTransport
	}
	maxRetries := r.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if !requestCanBeRetried(req) {
		return base.RoundTrip(req)
	}
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(time.Duration(attempt*attempt) * 100 * time.Millisecond):
			}
		}
		attemptReq, err := requestForAttempt(req, attempt)
		if err != nil {
			return nil, err
		}
		resp, err := base.RoundTrip(attemptReq)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		msg := strings.ToLower(err.Error())
		isTransient := strings.Contains(msg, "eof") ||
			strings.Contains(msg, "connection reset") ||
			strings.Contains(msg, "broken pipe") ||
			strings.Contains(msg, "use of closed network connection")
		if !isTransient {
			return nil, err
		}
	}
	return nil, lastErr
}

func requestCanBeRetried(req *http.Request) bool {
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		return true
	}
	return req.Method == http.MethodPost &&
		strings.TrimSpace(req.Header.Get("Idempotency-Key")) != "" &&
		req.GetBody != nil
}

func requestForAttempt(req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 0 || req.GetBody == nil {
		return req, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	next := req.Clone(req.Context())
	next.Body = body
	return next, nil
}

// --- remote-gateway proxy helpers ---

func (s Server) remoteClient() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return http.DefaultClient
}

// effectiveGatewayURL returns the gateway base URL to use for a given tool
// call.  The lookup order is:
//  1. "gateway_url" key in the per-call args (lets the agent override on
//     every call without restarting the MCP server).
//  2. s.RemoteGateway set at server construction time (from --gateway-url
//     flag or RDEV_*_GATEWAY_URL environment variables).
//
// Returns "" when no gateway is configured; callers should fall back to the
// local in-memory gateway.
func (s Server) effectiveGatewayURL(args map[string]any) string {
	if v := stringArg(args, "gateway_url", ""); v != "" {
		return strings.TrimRight(strings.TrimSpace(v), "/")
	}
	return s.RemoteGateway
}

// proxyGETTo sends a GET to baseURL+path and decodes the response.
func (s Server) proxyGETTo(baseURL, path string) (any, error) {
	resp, err := s.remoteClient().Get(baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("remote gateway GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	value, err := s.decodeRemoteResponse(resp)
	if err != nil {
		return nil, err
	}
	return value, nil
}

// proxyPOSTTo sends a POST to baseURL+path and decodes the response.
func (s Server) proxyPOSTTo(baseURL, path string, payload any) (any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if body, ok := payload.(map[string]any); ok {
		if key, _ := body["idempotency_key"].(string); strings.TrimSpace(key) != "" {
			req.Header.Set("Idempotency-Key", strings.TrimSpace(key))
		}
	}
	resp, err := s.remoteClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote gateway POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	value, err := s.decodeRemoteResponse(resp)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func newIdempotencyKey(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	const alphabet = "0123456789abcdef"
	out := make([]byte, len(raw)*2)
	for i, value := range raw {
		out[i*2] = alphabet[value>>4]
		out[i*2+1] = alphabet[value&0x0f]
	}
	return prefix + "-" + string(out)
}

// proxyGET is a convenience wrapper using s.RemoteGateway as the base URL.
func (s Server) proxyGET(path string) (any, error) {
	return s.proxyGETTo(s.RemoteGateway, path)
}

// proxyPOST is a convenience wrapper using s.RemoteGateway as the base URL.
func (s Server) proxyPOST(path string, payload any) (any, error) {
	return s.proxyPOSTTo(s.RemoteGateway, path, payload)
}

func (s Server) decodeRemoteResponse(resp *http.Response) (any, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return nil, fmt.Errorf("read remote gateway response: %w", err)
	}
	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode remote gateway response: %w", err)
	}
	if resp.StatusCode >= 400 {
		if m, ok := result.(map[string]any); ok {
			if errMsg, ok := m["error"].(string); ok && errMsg != "" {
				return nil, fmt.Errorf("%s", errMsg)
			}
		}
		return nil, fmt.Errorf("remote gateway returned HTTP %d", resp.StatusCode)
	}
	return result, nil
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := encoder.Encode(errorResponse(nil, -32700, "parse error")); err != nil {
				return err
			}
			continue
		}
		resp := s.handle(req)
		if req.ID == nil {
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s Server) handle(req request) response {
	switch req.Method {
	case "initialize":
		return success(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo": map[string]any{
				"name":    "rdev-mcp",
				"version": buildinfo.Version,
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		})
	case "notifications/initialized":
		return success(req.ID, map[string]any{})
	case "tools/list":
		return success(req.ID, map[string]any{"tools": contracts.Tools()})
	case "tools/call":
		result, err := s.callTool(req.Params)
		if err != nil {
			return errorResponse(req.ID, -32000, err.Error())
		}
		return success(req.ID, result)
	default:
		return errorResponse(req.ID, -32601, "method not found")
	}
}

func (s Server) callTool(raw json.RawMessage) (result map[string]any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("invalid tool arguments: %v", recovered)
		}
	}()
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}
	var data any
	switch params.Name {
	case "rdev.sessions.create":
		data, err = s.createSession(params.Arguments)
	case "rdev.sessions.status":
		data, err = s.sessionStatus(params.Arguments)
	case "rdev.sessions.events":
		data, err = s.sessionEvents(params.Arguments)
	case "rdev.sessions.task":
		data, err = s.sessionTask(params.Arguments)
	case "rdev.sessions.interrupt":
		data, err = s.sessionInterrupt(params.Arguments)
	case "rdev.sessions.artifacts":
		data, err = s.sessionArtifacts(params.Arguments)
	case "rdev.sessions.close":
		data, err = s.sessionClose(params.Arguments)
	case "rdev.invites.create":
		data, err = s.createInvite(params.Arguments)
	case "rdev.support_session.connect":
		data, err = s.supportSessionConnect(params.Arguments)
	case "rdev.support_session.handoff":
		data, err = s.supportSessionHandoff(params.Arguments)
	case "rdev.support_session.prepare":
		data, err = s.supportSessionPrepare(params.Arguments)
	case "rdev.support_session.create":
		data, err = s.supportSessionCreate(params.Arguments)
	case "rdev.support_session.plan":
		data, err = s.supportSessionPlan(params.Arguments)
	case "rdev.support_session.status":
		data, err = s.supportSessionStatus(params.Arguments)
	case "rdev.support_session.report":
		data, err = s.supportSessionReport(params.Arguments)
	case "rdev.support_session.smoke_test":
		data, err = s.supportSessionSmokeTest(params.Arguments)
	case "rdev.support_session.live_e2e_plan":
		data, err = s.supportSessionLiveE2EPlan(params.Arguments)
	case "rdev.connection_entry.plan":
		data, err = s.connectionEntryPlan(params.Arguments)
	case "rdev.acceptance.scaffold_evidence":
		data, err = s.scaffoldAcceptanceEvidence(params.Arguments)
	case "rdev.acceptance.evidence_status":
		data, err = s.acceptanceEvidenceStatus(params.Arguments)
	case "rdev.acceptance.scaffold_post_release_download":
		data, err = s.scaffoldPostReleaseDownloadEvidence(params.Arguments)
	case "rdev.acceptance.post_release_evidence_status":
		data, err = s.postReleaseDownloadEvidenceStatus(params.Arguments)
	case "rdev.acceptance.release_evidence_index":
		data, err = s.releaseEvidenceIndex(params.Arguments)
	case "rdev.relay_adapter.package":
		data, err = s.relayAdapterPackage(params.Arguments)
	case "rdev.relay_adapter.verify":
		data, err = s.relayAdapterVerify(params.Arguments)
	case "rdev.tickets.create":
		data, err = s.createTicket(params.Arguments)
	case "rdev.tickets.revoke":
		data, err = s.revokeTicket(params.Arguments)
	case "rdev.audit.query":
		data, err = s.queryAudit(params.Arguments)
	case "rdev.policy.explain":
		data, err = s.explainPolicy(params.Arguments)
	case "rdev.policy.explain_shell":
		data, err = s.explainShellPolicy(params.Arguments)
	case "rdev.enrollment.verify_certificate":
		data, err = s.verifyEnrollmentCertificate(params.Arguments)
	case "rdev.update.check":
		data, err = s.updateCheck(params.Arguments)
	case "rdev.update.plan":
		data, err = s.updatePlan(params.Arguments)
	case "rdev.adapter.verify_result":
		data, err = s.verifyAdapterResult(params.Arguments)
	case "rdev.adapter.verify_lifecycle":
		data, err = s.verifyAdapterLifecycle(params.Arguments)
	case "rdev.adapter.verify_cancellation":
		data, err = s.verifyAdapterCancellation(params.Arguments)
	case "rdev.adapter.verify_runtime":
		data, err = s.verifyAdapterRuntime(params.Arguments)
	default:
		err = fmt.Errorf("unknown tool %q", params.Name)
	}
	if err != nil {
		return nil, err
	}
	return toolResult(data)
}

func (s Server) createSession(args map[string]any) (any, error) {
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/sessions", sessionSpecFromArgs(args))
	}
	session, err := s.Gateway.CreateSession(sessionSpecFromArgs(args))
	if err != nil {
		return nil, err
	}
	status := session.DeriveStatus()
	return withSessionStatus(map[string]any{
		"session": session,
		"status":  status,
	}, status), nil
}

func (s Server) sessionStatus(args map[string]any) (any, error) {
	sessionID := requiredString(args, "session_id")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyGETTo(gwURL, "/v1/sessions/"+url.PathEscape(sessionID))
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		return nil, err
	}
	status := session.DeriveStatus()
	return withSessionStatus(map[string]any{
		"snapshot": session.Snapshot(),
		"status":   status,
	}, status), nil
}

func (s Server) sessionEvents(args map[string]any) (any, error) {
	sessionID := requiredString(args, "session_id")
	afterSeq := uint64(intArg(args, "after_seq", 0))
	limit := intArg(args, "limit", 100)
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		query := url.Values{}
		query.Set("after_seq", fmt.Sprintf("%d", afterSeq))
		query.Set("limit", fmt.Sprintf("%d", limit))
		if endpointID := stringArg(args, "endpoint_id", ""); endpointID != "" {
			query.Set("endpoint_id", endpointID)
		}
		if received := intArg(args, "received_seq", 0); received > 0 {
			query.Set("received_seq", fmt.Sprintf("%d", received))
		}
		if processed := intArg(args, "processed_seq", 0); processed > 0 {
			query.Set("processed_seq", fmt.Sprintf("%d", processed))
		}
		return s.proxyGETTo(gwURL, "/v1/sessions/"+url.PathEscape(sessionID)+"/events?"+query.Encode())
	}
	events, replay, err := s.Gateway.SessionEventsAfterForAgent(sessionID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		return nil, err
	}
	status := session.DeriveStatus()
	return withSessionStatus(map[string]any{
		"events":            events,
		"snapshot_required": replay.SnapshotRequired,
		"snapshot_seq":      replay.SnapshotSeq,
		"last_seq":          replay.LastSeq,
		"retry_after_ms":    replay.RetryAfterMS,
		"reconnecting":      replay.Reconnecting,
		"status":            status,
	}, status), nil
}

func (s Server) sessionTask(args map[string]any) (any, error) {
	sessionID := requiredString(args, "session_id")
	spec := controlplane.TaskSpec{
		TargetEndpointID: stringArg(args, "target_endpoint_id", ""),
		Adapter:          requiredString(args, "adapter"),
		Intent:           stringArg(args, "intent", ""),
		Capabilities:     stringSliceArg(args, "capabilities"),
		Payload:          objectArg(args, "payload"),
		Limits:           objectArg(args, "limits"),
		IdempotencyKey:   requiredString(args, "idempotency_key"),
	}
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/sessions/"+url.PathEscape(sessionID)+"/tasks", spec)
	}
	task, event, err := s.Gateway.SubmitSessionTask(sessionID, spec)
	if err != nil {
		return nil, err
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		return nil, err
	}
	status := session.DeriveStatus()
	return withSessionStatus(map[string]any{
		"task":   task,
		"event":  event,
		"status": status,
	}, status), nil
}

func (s Server) sessionInterrupt(args map[string]any) (any, error) {
	sessionID := requiredString(args, "session_id")
	payload := objectArg(args, "payload")
	if payload == nil {
		payload = map[string]any{}
	}
	reason := requiredString(args, "reason")
	payload["reason"] = reason
	event := controlplane.Event{
		Type:           controlplane.EventTypeInterrupt,
		FromEndpointID: "agent",
		ToEndpointID:   stringArg(args, "to_endpoint_id", ""),
		TaskID:         stringArg(args, "task_id", ""),
		IdempotencyKey: requiredString(args, "idempotency_key"),
		Payload:        payload,
	}
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/sessions/"+url.PathEscape(sessionID)+"/events", event)
	}
	appended, err := s.Gateway.AppendSessionEvent(sessionID, event)
	if err != nil {
		return nil, err
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		return nil, err
	}
	status := session.DeriveStatus()
	return withSessionStatus(map[string]any{
		"event":  appended,
		"status": status,
	}, status), nil
}

func (s Server) sessionArtifacts(args map[string]any) (any, error) {
	sessionID := requiredString(args, "session_id")
	if stringArg(args, "id", "") == "" && stringArg(args, "task_id", "") == "" {
		session, err := s.Gateway.Session(sessionID)
		if err != nil {
			return nil, err
		}
		status := session.DeriveStatus()
		return withSessionStatus(map[string]any{
			"artifacts": session.Artifacts,
			"status":    status,
		}, status), nil
	}
	ref := controlplane.ArtifactRef{
		ID:           stringArg(args, "id", ""),
		TaskID:       stringArg(args, "task_id", ""),
		Kind:         stringArg(args, "kind", ""),
		Name:         stringArg(args, "name", ""),
		SizeBytes:    int64(intArg(args, "size_bytes", 0)),
		SHA256:       stringArg(args, "sha256", ""),
		ContentType:  stringArg(args, "content_type", ""),
		UploadOffset: int64(intArg(args, "upload_offset", 0)),
		Complete:     boolArg(args, "complete", false),
	}
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/sessions/"+url.PathEscape(sessionID)+"/artifacts", ref)
	}
	artifact, event, err := s.Gateway.UpsertSessionArtifact(sessionID, ref)
	if err != nil {
		return nil, err
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		return nil, err
	}
	status := session.DeriveStatus()
	return withSessionStatus(map[string]any{
		"artifact": artifact,
		"event":    event,
		"status":   status,
	}, status), nil
}

func (s Server) sessionClose(args map[string]any) (any, error) {
	sessionID := requiredString(args, "session_id")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/sessions/"+url.PathEscape(sessionID)+"/close", map[string]any{
			"reason":          stringArg(args, "reason", ""),
			"idempotency_key": stringArg(args, "idempotency_key", ""),
		})
	}
	session, event, err := s.Gateway.CloseSession(sessionID)
	if err != nil {
		return nil, err
	}
	status := session.DeriveStatus()
	return withSessionStatus(map[string]any{
		"session": session,
		"event":   event,
		"status":  status,
	}, status), nil
}

func sessionSpecFromArgs(args map[string]any) controlplane.SessionSpec {
	spec := controlplane.SessionSpec{
		Profile:            stringArg(args, "profile", "attended-temporary"),
		Reason:             requiredString(args, "reason"),
		Capabilities:       stringSliceArg(args, "capabilities"),
		JoinPolicy:         stringArg(args, "join_policy", "single-target"),
		AuthorityID:        stringArg(args, "authority_id", ""),
		SelectedGatewayURL: stringArg(args, "selected_gateway_url", ""),
		ReconnectGraceMS:   intArg(args, "reconnect_grace_ms", 120000),
		RetryAfterMS:       intArg(args, "retry_after_ms", 500),
	}
	if raw := stringArg(args, "expires_at", ""); raw != "" {
		if expiresAt, err := time.Parse(time.RFC3339, raw); err == nil {
			spec.ExpiresAt = expiresAt
		}
	}
	return spec
}

func withSessionStatus(payload map[string]any, status controlplane.StatusSummary) map[string]any {
	payload["user_summary"] = status.UserSummary
	payload["agent_next_action"] = status.AgentNextAction
	payload["recoverable"] = status.Recoverable
	payload["retry_after_ms"] = status.RetryAfterMS
	return payload
}

func (s Server) supportSessionHandoff(args map[string]any) (any, error) {
	ttl := intArg(args, "ttl_seconds", 7200)
	if ttl < 60 || ttl > 86400 {
		return nil, fmt.Errorf("ttl_seconds must be between 60 and 86400")
	}
	rdevCommand := agentRdevCommand(stringArg(args, "rdev_command", ""))
	return supportsession.BuildHandoff(supportsession.HandoffOptions{
		RepoRoot:     stringArg(args, "repo_root", "."),
		WorkDir:      stringArg(args, "work_dir", ""),
		Addr:         stringArg(args, "addr", "0.0.0.0:8787"),
		GatewayURL:   stringArg(args, "gateway_url", ""),
		Target:       stringArg(args, "target", "auto"),
		Reason:       stringArg(args, "reason", "visible temporary remote support"),
		TTLSeconds:   ttl,
		AutoActivate: boolArg(args, "auto_activate", true),
		Locale:       stringArg(args, "locale", "auto"),
		RdevCommand:  rdevCommand,
	}), nil
}

func (s Server) supportSessionPrepare(args map[string]any) (any, error) {
	return supportsession.Prepare(context.Background(), supportsession.PrepareOptions{
		RepoRoot:    stringArg(args, "repo_root", "."),
		WorkDir:     stringArg(args, "work_dir", ""),
		GatewayURL:  stringArg(args, "gateway_url", ""),
		Addr:        stringArg(args, "addr", "0.0.0.0:8787"),
		Target:      stringArg(args, "target", "auto"),
		BuildAssets: boolArg(args, "build_assets", false),
		RdevCommand: agentRdevCommand(stringArg(args, "rdev_command", "")),
	})
}

func (s Server) supportSessionConnect(args map[string]any) (any, error) {
	ttl := intArg(args, "ttl_seconds", 7200)
	if ttl < 60 || ttl > 86400 {
		return nil, fmt.Errorf("ttl_seconds must be between 60 and 86400")
	}
	region, err := tunnelRegionArg(args)
	if err != nil {
		return nil, err
	}
	providerPolicy := strings.TrimSpace(stringArg(args, "provider_policy", ""))
	allowDegraded := boolArg(args, "allow_degraded_direct_handoff", false)
	gatewayURL := strings.TrimRight(strings.TrimSpace(stringArg(args, "gateway_url", "")), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	regionalEvidence, err := loadMCPRegionalEvidence(providerPolicy, region, time.Now().UTC(), gatewayURL != "")
	if err != nil {
		return nil, err
	}
	foregroundPolicyPath := providerPolicy
	if gatewayURL != "" {
		foregroundPolicyPath = ""
	}
	rdevCommand := agentRdevCommand(stringArg(args, "rdev_command", ""))
	handoff := supportsession.BuildHandoff(supportsession.HandoffOptions{
		RepoRoot:                   stringArg(args, "repo_root", "."),
		WorkDir:                    stringArg(args, "work_dir", ""),
		Addr:                       stringArg(args, "addr", "0.0.0.0:8787"),
		GatewayURL:                 gatewayURL,
		Target:                     stringArg(args, "target", "auto"),
		Reason:                     stringArg(args, "reason", "visible temporary remote support"),
		TTLSeconds:                 ttl,
		AutoActivate:               boolArg(args, "auto_activate", true),
		Locale:                     stringArg(args, "locale", "auto"),
		RdevCommand:                rdevCommand,
		Region:                     string(region),
		ProviderPolicyPath:         foregroundPolicyPath,
		AllowDegradedDirectHandoff: allowDegraded,
		RequireForeground:          gatewayURL != "",
	})
	if nextArgs, ok := handoff["mcp_next_arguments"].(map[string]any); ok {
		nextArgs["region"] = string(region)
		if foregroundPolicyPath != "" {
			nextArgs["provider_policy"] = foregroundPolicyPath
		}
		nextArgs["allow_degraded_direct_handoff"] = allowDegraded
	}
	readiness := supportsession.DirectAvailability(tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        region,
	}, false)
	payload := addTunnelReadinessAliases(supportsession.BuildConnectFromHandoff(handoff), readiness, regionalEvidence, providerPolicy != "")
	if gatewayURL != "" {
		payload["reason"] = "remote_ticket_and_probe_required"
	}
	return payload, nil
}

func tunnelRegionArg(args map[string]any) (tunnel.RegionProfile, error) {
	region := tunnel.RegionProfile(strings.TrimSpace(stringArg(args, "region", string(tunnel.RegionGlobal))))
	if region == "" {
		region = tunnel.RegionGlobal
	}
	if region != tunnel.RegionGlobal && region != tunnel.RegionCNMainland {
		return "", fmt.Errorf("unsupported tunnel region %q; use global or cn-mainland", region)
	}
	return region, nil
}

func addTunnelReadinessAliases(payload map[string]any, readiness supportsession.AvailabilityReadiness, evidence []mcpRegionalEvidenceSummary, providerPolicyApplied bool) map[string]any {
	payload["availability_readiness"] = readiness
	payload["availability_set"] = readiness.AvailabilitySet
	payload["regional_evidence"] = evidence
	payload["provider_policy_applied"] = providerPolicyApplied
	payload["ready_to_send"] = readiness.ReadyToSend
	payload["ready_to_send_human"] = readiness.ReadyToSend
	payload["ready_to_send_to_human"] = readiness.ReadyToSend
	payload["ready_to_activate"] = readiness.ReadyToActivate
	payload["ready_to_execute"] = readiness.ReadyToExecute
	payload["degraded_single_entry"] = readiness.DegradedSingleEntry
	return payload
}

func loadMCPRegionalEvidence(policyPath string, region tunnel.RegionProfile, now time.Time, allowConfiguredGateway bool) ([]mcpRegionalEvidenceSummary, error) {
	policyPath = strings.TrimSpace(policyPath)
	if policyPath == "" {
		return []mcpRegionalEvidenceSummary{}, nil
	}
	var policy mcpTunnelProviderPolicyFile
	if err := tunnel.ReadProtectedJSONFile(policyPath, &policy); err != nil {
		return nil, fmt.Errorf("decode provider policy: %w", err)
	}
	knownProviders := knownMCPProviderIDs(allowConfiguredGateway)
	allowed, disabled, err := validateMCPProviderPolicy(policy, knownProviders)
	if err != nil {
		return nil, err
	}
	summaries := make([]mcpRegionalEvidenceSummary, 0)
	for _, evidencePath := range policy.RegionalEvidencePaths {
		evidencePath = strings.TrimSpace(evidencePath)
		if evidencePath == "" {
			return nil, fmt.Errorf("provider policy contains an empty regional evidence path")
		}
		var evidenceData json.RawMessage
		if err := tunnel.ReadProtectedJSONFile(evidencePath, &evidenceData); err != nil {
			return nil, fmt.Errorf("read regional evidence: %w", err)
		}
		values, err := decodeMCPRegionalEvidence(evidenceData)
		if err != nil {
			return nil, fmt.Errorf("decode regional evidence: %w", err)
		}
		for _, item := range values {
			if !knownProviders[item.ProviderID] {
				return nil, fmt.Errorf("regional evidence references unknown provider %q", item.ProviderID)
			}
			if err := item.Validate(); err != nil {
				return nil, fmt.Errorf("validate regional evidence: %w", err)
			}
			if item.Region != region || !mcpPolicyAllowsProvider(item.ProviderID, allowed, disabled) {
				continue
			}
			summaries = append(summaries, mcpRegionalEvidenceSummary{
				ProviderID: item.ProviderID,
				Region:     item.Region,
				Status:     item.Status,
				ObservedAt: item.ObservedAt,
				ExpiresAt:  item.ExpiresAt,
				Fresh:      !item.ObservedAt.After(now) && item.ExpiresAt.After(now),
			})
		}
	}
	return summaries, nil
}

func knownMCPProviderIDs(allowConfiguredGateway bool) map[string]bool {
	known := make(map[string]bool, len(tunnel.CanonicalProviderIDs())+1)
	for _, id := range tunnel.CanonicalProviderIDs() {
		known[id] = true
	}
	if allowConfiguredGateway {
		known[configuredGatewayProviderID] = true
	}
	return known
}

func validateMCPProviderPolicy(policy mcpTunnelProviderPolicyFile, known map[string]bool) (map[string]bool, map[string]bool, error) {
	allowedCount := 0
	if policy.AllowedProviderIDs != nil {
		allowedCount = len(*policy.AllowedProviderIDs)
	}
	allowed := make(map[string]bool, allowedCount+1)
	disabled := make(map[string]bool, len(policy.DisabledProviderIDs))
	allowedValues := []string(nil)
	if policy.AllowedProviderIDs != nil {
		allowed[mcpProviderPolicyRestrictedKey] = true
		allowedValues = *policy.AllowedProviderIDs
	}
	for label, values := range map[string][]string{
		"allowed_provider_ids":  allowedValues,
		"disabled_provider_ids": policy.DisabledProviderIDs,
	} {
		target := allowed
		if label == "disabled_provider_ids" {
			target = disabled
		}
		for _, value := range values {
			id := strings.TrimSpace(value)
			if id != value {
				return nil, nil, fmt.Errorf("provider policy %s contains non-canonical provider %q", label, value)
			}
			if id == "" {
				return nil, nil, fmt.Errorf("provider policy %s contains an empty provider ID", label)
			}
			if target[id] {
				return nil, nil, fmt.Errorf("provider policy %s contains duplicate provider %q", label, id)
			}
			if !known[id] {
				return nil, nil, fmt.Errorf("provider policy %s references unknown provider %q", label, id)
			}
			target[id] = true
		}
	}
	for id := range allowed {
		if disabled[id] {
			return nil, nil, fmt.Errorf("provider policy lists provider %q as both allowed and disabled", id)
		}
	}
	for id, path := range policy.SSHKnownHostsPaths {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(path) == "" {
			return nil, nil, fmt.Errorf("provider policy ssh_known_hosts_paths requires non-empty provider IDs and paths")
		}
		if !known[strings.TrimSpace(id)] {
			return nil, nil, fmt.Errorf("provider policy ssh_known_hosts_paths references unknown provider %q", id)
		}
	}
	return allowed, disabled, nil
}

func mcpPolicyAllowsProvider(providerID string, allowed, disabled map[string]bool) bool {
	if disabled[providerID] {
		return false
	}
	if allowed[mcpProviderPolicyRestrictedKey] {
		return allowed[providerID]
	}
	return len(allowed) == 0 || allowed[providerID]
}

func decodeMCPRegionalEvidence(data []byte) ([]tunnel.RegionalEvidence, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("regional evidence is empty")
	}
	if bytes.TrimSpace(data)[0] == '[' {
		var values []tunnel.RegionalEvidence
		if err := decodeStrictMCPJSON(data, &values); err != nil {
			return nil, err
		}
		return values, nil
	}
	var value tunnel.RegionalEvidence
	if err := decodeStrictMCPJSON(data, &value); err != nil {
		return nil, err
	}
	return []tunnel.RegionalEvidence{value}, nil
}

func decodeStrictMCPJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
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

func availabilityReadinessFromCreated(created map[string]any) supportsession.AvailabilityReadiness {
	encoded, err := json.Marshal(created["availability_readiness"])
	if err != nil {
		return supportsession.AvailabilityReadiness{}
	}
	var readiness supportsession.AvailabilityReadiness
	if err := json.Unmarshal(encoded, &readiness); err != nil {
		return supportsession.AvailabilityReadiness{}
	}
	return readiness
}

func (s Server) updateCheck(args map[string]any) (any, error) {
	return update.CheckLatest(context.Background(), http.DefaultClient, update.Options{
		Repo:           stringArg(args, "repo", update.DefaultRepo),
		APIBaseURL:     stringArg(args, "api_base_url", update.DefaultAPIBaseURL),
		CurrentVersion: stringArg(args, "current_version", buildinfo.Version),
	})
}

func (s Server) updatePlan(args map[string]any) (any, error) {
	opts := update.Options{
		Repo:           stringArg(args, "repo", update.DefaultRepo),
		APIBaseURL:     stringArg(args, "api_base_url", update.DefaultAPIBaseURL),
		CurrentVersion: stringArg(args, "current_version", buildinfo.Version),
		Platform:       stringArg(args, "platform", ""),
	}
	check, err := update.CheckLatest(context.Background(), http.DefaultClient, opts)
	if err != nil {
		return check, err
	}
	return update.PlanFromCheck(check, opts), nil
}

func (s Server) createInvite(args map[string]any) (any, error) {
	mode := model.HostMode(stringArg(args, "mode", string(model.HostModeAttendedTemporary)))
	ttl := intArg(args, "ttl_seconds", 7200)
	reason := stringArg(args, "reason", "remote support")
	capabilities := stringSliceArg(args, "capabilities")
	autoActivate := boolArg(args, "auto_activate", false)
	if autoActivate && mode != model.HostModeAttendedTemporary {
		return nil, fmt.Errorf("auto_activate is only supported for attended-temporary Connection Entries")
	}
	metadata := map[string]string{}
	if autoActivate {
		metadata["auto_activate"] = "attended-temporary"
		metadata["connection_entry"] = "standard-visible"
		metadata["activation_contract"] = "target-consent-scoped-ticket"
	}
	ticket, err := s.Gateway.CreateTicketWithMetadata(mode, ttl, capabilities, reason, metadata)
	if err != nil {
		return nil, err
	}
	gatewayURL := requiredString(args, "gateway_url")
	manifestRoot := s.Gateway.ManifestRoot()
	invite, err := agentinvite.New(agentinvite.Options{
		GatewayURL:            gatewayURL,
		ManifestRootPublicKey: manifestRootPublicKey(manifestRoot),
		Ticket:                ticket,
		Transport:             stringArg(args, "transport", "auto"),
		NetworkScope:          stringArg(args, "network_scope", "auto"),
		AuthorityProfile:      stringArg(args, "authority_profile", "max-control"),
		Once:                  boolArg(args, "once", false),
		RequireHostActivation: boolArg(args, "require_host_activation", !autoActivate),
		RdevCommand:           stringArg(args, "rdev_command", "rdev"),
	})
	if err != nil {
		return nil, err
	}
	return invite, nil
}

func (s Server) supportSessionCreate(args map[string]any) (any, error) {
	ttl := intArg(args, "ttl_seconds", 7200)
	if ttl < 60 || ttl > 86400 {
		return nil, fmt.Errorf("ttl_seconds must be between 60 and 86400")
	}
	gatewayURL := strings.TrimRight(strings.TrimSpace(stringArg(args, "gateway_url", "")), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	if gatewayURL == "" {
		return nil, fmt.Errorf("support_session.create requires gateway_url or a configured RDEV_*_GATEWAY_URL")
	}
	return s.createSupportSessionPayload(args, gatewayURL, ttl)
}

func (s Server) createSupportSessionPayload(args map[string]any, gatewayURL string, ttl int) (map[string]any, error) {
	autoActivate := boolArg(args, "auto_activate", true)
	region, err := tunnelRegionArg(args)
	if err != nil {
		return nil, err
	}
	gatewayCandidates := supportsession.GatewayURLCandidatesFromIPs("0.0.0.0:8787", gatewayURL, nil)
	metadata := map[string]string{
		"connection_entry":    "standard-visible",
		"activation_contract": "target-consent-scoped-ticket",
	}
	if autoActivate {
		metadata["auto_activate"] = "attended-temporary"
	}
	metadata = addGatewayCandidateTicketMetadata(metadata, gatewayCandidates)
	ticket, err := s.Gateway.CreateTicketWithMetadata(
		model.HostModeAttendedTemporary,
		ttl,
		policyCapabilitiesToStrings(policy.TemporaryDefaults()),
		stringArg(args, "reason", "visible temporary remote support"),
		metadata,
	)
	if err != nil {
		return nil, err
	}
	joinURL := strings.TrimRight(gatewayURL, "/") + "/join/" + ticket.Code
	manifestURL := strings.TrimRight(gatewayURL, "/") + "/v1/tickets/" + ticket.Code + "/manifest"
	availabilitySet := tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        region,
		Candidates: []tunnel.Candidate{{
			ProviderID: configuredGatewayProviderID,
			URL:        gatewayURL,
		}},
	}
	readiness := supportsession.DirectAvailability(availabilitySet, boolArg(args, "allow_degraded_direct_handoff", false))
	return supportsession.BuildCreated(supportsession.CreatedOptions{
		GatewayURL:            gatewayURL,
		GatewayURLCandidates:  gatewayCandidates,
		JoinURL:               joinURL,
		ManifestURL:           manifestURL,
		ManifestRootPublicKey: manifestRootPublicKey(s.Gateway.ManifestRoot()),
		Ticket:                ticket,
		Target:                stringArg(args, "target", "auto"),
		Locale:                stringArg(args, "locale", "auto"),
		RdevCommand:           agentRdevCommand(stringArg(args, "rdev_command", "")),
		AutoActivate:          autoActivate,
		AvailabilityReadiness: readiness,
	}), nil
}

func addGatewayCandidateTicketMetadata(metadata map[string]string, candidates []supportsession.GatewayURLCandidate) map[string]string {
	if len(candidates) == 0 {
		return metadata
	}
	values := make([]model.JoinManifestGatewayCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		url := strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if url == "" {
			continue
		}
		values = append(values, model.JoinManifestGatewayCandidate{
			URL:         url,
			Kind:        strings.TrimSpace(candidate.Kind),
			Scope:       strings.TrimSpace(candidate.Scope),
			Recommended: candidate.Recommended,
			Reason:      strings.TrimSpace(candidate.Reason),
		})
	}
	if len(values) == 0 {
		return metadata
	}
	content, err := json.Marshal(values)
	if err != nil {
		return metadata
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata[gateway.TicketMetadataGatewayCandidates] = string(content)
	return metadata
}

func (s Server) supportSessionPlan(args map[string]any) (any, error) {
	return supportsession.BuildPlan(context.Background(), supportsession.Options{
		RepoRoot:     stringArg(args, "repo_root", "."),
		WorkDir:      stringArg(args, "work_dir", ""),
		GatewayURL:   stringArg(args, "gateway_url", ""),
		Addr:         stringArg(args, "addr", "0.0.0.0:8787"),
		Target:       stringArg(args, "target", "auto"),
		Reason:       stringArg(args, "reason", "visible temporary remote support"),
		TTLSeconds:   intArg(args, "ttl_seconds", 7200),
		AutoActivate: boolArg(args, "auto_activate", true),
		Locale:       stringArg(args, "locale", "auto"),
		RdevCommand:  agentRdevCommand(stringArg(args, "rdev_command", "")),
	}), nil
}

func (s Server) supportSessionLiveE2EPlan(args map[string]any) (any, error) {
	return supportsession.BuildLiveE2EPlan(supportsession.LiveE2EPlanOptions{
		GatewayURL:       stringArg(args, "gateway_url", ""),
		TicketCode:       stringArg(args, "ticket_code", ""),
		HostID:           stringArg(args, "host_id", ""),
		SessionID:        stringArg(args, "session_id", ""),
		TargetEndpointID: stringArg(args, "target_endpoint_id", ""),
		RdevCommand:      agentRdevCommand(stringArg(args, "rdev_command", "")),
		TimeoutSeconds:   intArg(args, "timeout_seconds", 180),
	}), nil
}

func (s Server) supportSessionStatus(args map[string]any) (any, error) {
	ticketCode := requiredString(args, "ticket_code")
	wait := boolArg(args, "wait", false)
	timeoutSeconds := intArg(args, "timeout_seconds", 120)
	intervalMillis := intArg(args, "interval_millis", 1000)
	if timeoutSeconds < 0 || timeoutSeconds > 3600 {
		return nil, fmt.Errorf("timeout_seconds must be between 0 and 3600")
	}
	if intervalMillis < 100 || intervalMillis > 60000 {
		return nil, fmt.Errorf("interval_millis must be between 100 and 60000")
	}
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.remoteSupportSessionStatus(args, gwURL, ticketCode, wait, timeoutSeconds, intervalMillis)
	}
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for {
		status := supportsession.BuildStatus(supportsession.StatusOptions{
			TicketCode:  ticketCode,
			Hosts:       s.Gateway.HostsForTicketCode(ticketCode, ""),
			Locale:      stringArg(args, "locale", "auto"),
			GatewayURL:  s.effectiveGatewayURL(args),
			Preconnects: s.Gateway.SupportSessionPreconnects(ticketCode),
		})
		if !wait || status["connected"] == true || status["status"] == "pending-activation" {
			return status, nil
		}
		if timeoutSeconds > 0 && time.Now().After(deadline) {
			return supportsession.MarkStatusTimedOut(status, ticketCode, stringArg(args, "locale", "auto")), nil
		}
		time.Sleep(time.Duration(intervalMillis) * time.Millisecond)
	}
}

func (s Server) remoteSupportSessionStatus(args map[string]any, gwURL, ticketCode string, wait bool, timeoutSeconds, intervalMillis int) (any, error) {
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for {
		path := "/v1/support-session/status?ticket_code=" + url.QueryEscape(ticketCode)
		if locale := stringArg(args, "locale", ""); locale != "" {
			path += "&locale=" + url.QueryEscape(locale)
		}
		payload, err := s.proxyGETTo(gwURL, path)
		if err != nil {
			return nil, err
		}
		status, _ := payload.(map[string]any)
		if status == nil {
			return nil, fmt.Errorf("remote gateway status response was not an object")
		}
		if !wait || status["connected"] == true || status["status"] == "pending-activation" {
			return status, nil
		}
		if timeoutSeconds > 0 && time.Now().After(deadline) {
			return supportsession.MarkStatusTimedOut(status, ticketCode, stringArg(args, "locale", "auto")), nil
		}
		time.Sleep(time.Duration(intervalMillis) * time.Millisecond)
	}
}

func (s Server) supportSessionReport(args map[string]any) (any, error) {
	gatewayURL := s.effectiveGatewayURL(args)
	hostID := strings.TrimSpace(stringArg(args, "host_id", ""))
	sessionID := strings.TrimSpace(stringArg(args, "session_id", ""))
	ticketCode := strings.TrimSpace(stringArg(args, "ticket_code", ""))
	var status map[string]any
	activeHosts := []map[string]any{}
	selectedHost := map[string]any{}
	if ticketCode != "" {
		statusAny, err := s.supportSessionStatus(map[string]any{
			"gateway_url":     gatewayURL,
			"ticket_code":     ticketCode,
			"locale":          stringArg(args, "locale", "auto"),
			"wait":            false,
			"timeout_seconds": float64(0),
			"interval_millis": float64(1000),
		})
		if err != nil {
			return nil, err
		}
		status = nestedMapOrSelfAny(statusAny, "")
		activeHosts = mapSliceFromAny(status["active_hosts"])
		if hostID == "" {
			if len(activeHosts) == 1 {
				hostID = stringMapValue(activeHosts[0], "id")
				selectedHost = activeHosts[0]
			} else {
				nextAction := "No active target endpoint is ready for this ticket. Wait with rdev.support_session.status or run recovery if stale endpoints are present."
				if len(activeHosts) > 1 {
					nextAction = "Multiple active targets are registered for this ticket; choose the intended session_id or target_endpoint_id before sending tasks."
				}
				return map[string]any{
					"schema_version":        "rdev.support-session-report.v1",
					"ok":                    false,
					"gateway_url":           gatewayURL,
					"connection_continuity": supportSessionConnectionContinuityMap(gatewayURL),
					"disconnect_policy":     "do not disconnect automatically after task completion; keep the session alive until the operator explicitly requests disconnect/revoke/stop",
					"remote_control_entry":  supportSessionRemoteControlEntryMap(gatewayURL, ticketCode, status, nil),
					"managed_upgrade":       supportSessionManagedUpgradeRecommendationMap(nil),
					"ticket_code":           ticketCode,
					"host_id":               "",
					"session_id":            "",
					"status":                status,
					"active_hosts":          status["active_hosts"],
					"stale_hosts":           status["stale_hosts"],
					"pending_hosts":         status["pending_hosts"],
					"host_count":            status["host_count"],
					"next_action":           nextAction,
					"stale_host_rule":       "Do not send tasks to stale endpoints; stale means the runner is not task-ready.",
				}, nil
			}
		} else {
			for _, candidate := range activeHosts {
				if stringMapValue(candidate, "id") == hostID {
					selectedHost = candidate
					break
				}
			}
		}
	}
	if hostID == "" && sessionID == "" {
		return nil, fmt.Errorf("support_session.report requires session_id, host_id, or ticket_code")
	}
	host := selectedHost
	if hostID != "" {
		if len(host) == 0 {
			host = map[string]any{
				"id":     hostID,
				"source": "explicit-host-id",
			}
		}
	}
	var snapshot map[string]any
	tasks := []map[string]any{}
	if sessionID != "" {
		var err error
		snapshot, err = s.fetchSessionSnapshotMap(gatewayURL, sessionID)
		if err != nil {
			return nil, err
		}
		tasks = mcpTaskReportsFromSnapshot(snapshot)
		host = mcpEnrichHostMapFromEndpoint(host, mcpTargetEndpointFromSnapshot(snapshot, stringArg(args, "target_endpoint_id", "")))
	}
	report := map[string]any{
		"schema_version":        "rdev.support-session-report.v1",
		"ok":                    true,
		"gateway_url":           gatewayURL,
		"connection_continuity": supportSessionConnectionContinuityMap(gatewayURL),
		"disconnect_policy":     "do not disconnect automatically after task completion; keep the session alive until the operator explicitly requests disconnect/revoke/stop",
		"remote_control_entry":  supportSessionRemoteControlEntryMap(gatewayURL, ticketCode, status, host),
		"managed_upgrade":       supportSessionManagedUpgradeRecommendationMap(host),
		"live_remote_e2e_plan": supportsession.BuildLiveE2EPlan(supportsession.LiveE2EPlanOptions{
			GatewayURL:       gatewayURL,
			TicketCode:       ticketCode,
			HostID:           hostID,
			SessionID:        sessionID,
			TargetEndpointID: stringArg(args, "target_endpoint_id", ""),
		}),
		"ticket_code":     ticketCode,
		"host_id":         hostID,
		"session_id":      sessionID,
		"host":            host,
		"session":         snapshot,
		"tasks":           tasks,
		"human_report":    supportSessionHumanReportMap(host, tasks),
		"next_action":     "Use rdev.sessions.task/events/artifacts for scoped work; keep the connection alive until the operator explicitly requests disconnect or revocation.",
		"stale_host_rule": "Do not send new session tasks to stale endpoints; run recovery or create a fresh session if no target endpoint is online.",
	}
	if status != nil {
		report["status"] = status
		report["active_hosts"] = status["active_hosts"]
		report["stale_hosts"] = status["stale_hosts"]
		report["pending_hosts"] = status["pending_hosts"]
		report["host_count"] = status["host_count"]
	}
	return report, nil
}

func (s Server) supportSessionSmokeTest(args map[string]any) (any, error) {
	gatewayURL := s.effectiveGatewayURL(args)
	ticketCode := strings.TrimSpace(stringArg(args, "ticket_code", ""))
	sessionID := strings.TrimSpace(stringArg(args, "session_id", ""))
	timeoutSeconds := intArg(args, "timeout_seconds", 120)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	if timeoutSeconds > 3600 {
		return nil, fmt.Errorf("timeout_seconds must be between 1 and 3600")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("support_session.smoke_test requires session_id")
	}
	remoteControl := boolArg(args, "remote_control", false)
	audit, err := s.runSupportSessionCapabilityAudit(args, gatewayURL, sessionID, time.Duration(timeoutSeconds)*time.Second, remoteControl)
	if err != nil {
		return nil, err
	}
	report := map[string]any{
		"schema_version":           "rdev.support-session-smoke-test.v1",
		"ok":                       audit["ok"],
		"gateway_url":              gatewayURL,
		"session_id":               sessionID,
		"target_endpoint_id":       audit["target_endpoint_id"],
		"ticket_code":              ticketCode,
		"connection_continuity":    supportSessionConnectionContinuityMap(gatewayURL),
		"disconnect_policy":        "do not disconnect automatically after task completion; keep the session alive until the operator explicitly requests disconnect/revoke/stop",
		"remote_control_entry":     supportSessionRemoteControlEntryMap(gatewayURL, ticketCode, nil, nestedMapOrSelfAny(audit["host"], "")),
		"managed_upgrade":          supportSessionManagedUpgradeRecommendationMap(nestedMapOrSelfAny(audit["host"], "")),
		"capability_audit":         audit,
		"remote_control_requested": remoteControl,
		"human_report":             supportSessionSmokeHumanReportMap(audit),
		"next_action":              "Use this single smoke-test report instead of hand-written probes; keep the target endpoint connected for follow-up work unless the operator explicitly asks to disconnect.",
	}
	return report, nil
}

func (s Server) runSupportSessionCapabilityAudit(args map[string]any, gatewayURL, sessionID string, timeout time.Duration, remoteControl bool) (map[string]any, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	deadline := time.Now().Add(timeout)
	snapshot, err := s.fetchSessionSnapshotMap(gatewayURL, sessionID)
	if err != nil {
		return nil, err
	}
	targetEndpointID := strings.TrimSpace(stringArg(args, "target_endpoint_id", ""))
	endpoint := mcpTargetEndpointFromSnapshot(snapshot, targetEndpointID)
	selectedTargetEndpointID := stringMapValue(endpoint, "id")
	host := mcpHostMapFromEndpoint(endpoint)
	probes := mcpAuditCapabilityProbes(stringMapValue(host, "os"))
	if remoteControl {
		probes = append(probes, mcpRemoteControlAuditProbes(stringMapValue(host, "os"))...)
	}
	results := make([]map[string]any, 0, len(probes))
	ok := true
	remoteControlProbeCount := 0
	for _, probe := range probes {
		if probe.Category == "remote_control" {
			remoteControlProbeCount++
		}
		created, err := s.sessionTask(map[string]any{
			"gateway_url":        gatewayURL,
			"session_id":         sessionID,
			"target_endpoint_id": selectedTargetEndpointID,
			"adapter":            probe.Adapter,
			"intent":             probe.Intent,
			"capabilities":       stringSliceFromAnyMap(probe.Policy["capabilities"]),
			"payload":            probe.Policy,
			"limits":             mcpSessionLimitsFromPayload(probe.Policy),
			"idempotency_key":    newIdempotencyKey("support-smoke-" + probe.Name),
		})
		if err != nil {
			ok = false
			results = append(results, map[string]any{
				"name":       probe.Name,
				"capability": probe.Capability,
				"status":     "create_failed",
				"ok":         false,
				"error":      err.Error(),
			})
			continue
		}
		task := nestedMapOrSelfAny(created, "task")
		taskID := stringMapValue(task, "id")
		if taskID == "" {
			ok = false
			results = append(results, map[string]any{
				"name":       probe.Name,
				"capability": probe.Capability,
				"status":     "create_failed",
				"ok":         false,
				"error":      "gateway response missing task.id",
			})
			continue
		}
		taskStatus, err := s.waitForSessionTaskTerminal(gatewayURL, sessionID, taskID, deadline)
		if err != nil {
			ok = false
			results = append(results, map[string]any{
				"name":       probe.Name,
				"capability": probe.Capability,
				"task_id":    taskID,
				"status":     "wait_failed",
				"ok":         false,
				"error":      err.Error(),
			})
			continue
		}
		status := stringMapValue(taskStatus, "status")
		resultOK := status == string(controlplane.TaskStatusSucceeded) || (probe.FailureIsOK && (status == string(controlplane.TaskStatusFailed) || status == string(controlplane.TaskStatusCanceled)))
		if !resultOK {
			ok = false
		}
		category := probe.Category
		if category == "" {
			category = "baseline"
		}
		results = append(results, map[string]any{
			"name":             probe.Name,
			"category":         category,
			"capability":       probe.Capability,
			"adapter":          probe.Adapter,
			"task_id":          taskID,
			"task_status":      status,
			"ok":               resultOK,
			"expected_failure": probe.FailureIsOK,
		})
	}
	return map[string]any{
		"schema_version":             "rdev.support-session-capability-audit.v1",
		"ok":                         ok,
		"gateway_url":                gatewayURL,
		"session_id":                 sessionID,
		"target_endpoint_id":         selectedTargetEndpointID,
		"connection_continuity":      supportSessionConnectionContinuityMap(gatewayURL),
		"host":                       host,
		"session":                    snapshot,
		"results":                    results,
		"remote_control_requested":   remoteControl,
		"remote_control_probe_count": remoteControlProbeCount,
		"next_action":                "Use scoped session tasks only after this audit is ok; do not disconnect the target endpoint unless the operator asks.",
	}, nil
}

func (s Server) fetchSessionSnapshotMap(gatewayURL, sessionID string) (map[string]any, error) {
	if strings.TrimSpace(gatewayURL) != "" {
		payload, err := s.proxyGETTo(gatewayURL, "/v1/sessions/"+url.PathEscape(sessionID))
		if err != nil {
			return nil, err
		}
		snapshot := nestedMapOrSelfAny(payload, "snapshot")
		if len(snapshot) == 0 {
			return nil, fmt.Errorf("session response missing snapshot")
		}
		return snapshot, nil
	}
	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		return nil, err
	}
	return structToMap(session.Snapshot()), nil
}

func mcpTaskReportsFromSnapshot(snapshot map[string]any) []map[string]any {
	tasks := mapSliceFromAny(snapshot["tasks"])
	reports := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		reports = append(reports, map[string]any{
			"task_id":            stringMapValue(task, "id"),
			"target_endpoint_id": stringMapValue(task, "target_endpoint_id"),
			"status":             stringMapValue(task, "status"),
			"adapter":            stringMapValue(task, "adapter"),
			"intent":             stringMapValue(task, "intent"),
			"attempt_id":         stringMapValue(task, "attempt_id"),
		})
	}
	return reports
}

func mcpTargetEndpointFromSnapshot(snapshot map[string]any, targetEndpointID string) map[string]any {
	endpoints := mapSliceFromAny(snapshot["endpoints"])
	targetEndpointID = strings.TrimSpace(targetEndpointID)
	if targetEndpointID != "" {
		for _, endpoint := range endpoints {
			if stringMapValue(endpoint, "id") == targetEndpointID {
				return endpoint
			}
		}
	}
	for _, endpoint := range endpoints {
		if stringMapValue(endpoint, "role") == string(controlplane.EndpointRoleTarget) && stringMapValue(endpoint, "state") == string(controlplane.EndpointStateOnline) {
			return endpoint
		}
	}
	for _, endpoint := range endpoints {
		if stringMapValue(endpoint, "role") == string(controlplane.EndpointRoleTarget) {
			return endpoint
		}
	}
	return map[string]any{}
}

func mcpHostMapFromEndpoint(endpoint map[string]any) map[string]any {
	if len(endpoint) == 0 {
		return map[string]any{}
	}
	platform := stringMapValue(endpoint, "platform")
	return map[string]any{
		"id":                   stringMapValue(endpoint, "id"),
		"name":                 firstReportFieldMap(endpoint, "name", "id"),
		"os":                   mcpPlatformOS(platform),
		"arch":                 mcpPlatformArch(platform),
		"status":               stringMapValue(endpoint, "state"),
		"platform":             platform,
		"identity_fingerprint": stringMapValue(endpoint, "identity_fingerprint"),
		"capabilities":         stringSliceFromAnyMap(endpoint["capabilities"]),
	}
}

func mcpEnrichHostMapFromEndpoint(host, endpoint map[string]any) map[string]any {
	endpointHost := mcpHostMapFromEndpoint(endpoint)
	if len(host) == 0 {
		return endpointHost
	}
	enriched := map[string]any{}
	for key, value := range host {
		enriched[key] = value
	}
	for _, key := range []string{"name", "os", "arch", "status", "platform", "identity_fingerprint", "capabilities"} {
		if _, ok := enriched[key]; !ok {
			if value, exists := endpointHost[key]; exists {
				enriched[key] = value
			}
		}
	}
	return enriched
}

func mcpPlatformOS(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if before, _, ok := strings.Cut(platform, "/"); ok {
		return before
	}
	return platform
}

func mcpPlatformArch(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if _, after, ok := strings.Cut(platform, "/"); ok {
		return after
	}
	return ""
}

func mcpSessionLimitsFromPayload(payload map[string]any) map[string]any {
	limits := map[string]any{}
	for _, key := range []string{"max_duration_seconds", "max_output_bytes", "network"} {
		if value, ok := payload[key]; ok {
			limits[key] = value
		}
	}
	return limits
}

func (s Server) waitForSessionTaskTerminal(gatewayURL, sessionID, taskID string, deadline time.Time) (map[string]any, error) {
	for {
		snapshot, err := s.fetchSessionSnapshotMap(gatewayURL, sessionID)
		if err != nil {
			return nil, err
		}
		for _, task := range mapSliceFromAny(snapshot["tasks"]) {
			if stringMapValue(task, "id") != taskID {
				continue
			}
			status := stringMapValue(task, "status")
			if isTerminalTaskStatus(status) {
				return task, nil
			}
			break
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil, fmt.Errorf("task %s did not reach a terminal state before timeout", taskID)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func isTerminalTaskStatus(status string) bool {
	switch status {
	case string(controlplane.TaskStatusSucceeded), string(controlplane.TaskStatusFailed), string(controlplane.TaskStatusCanceled):
		return true
	default:
		return false
	}
}

type mcpSupportSessionAuditProbe struct {
	Name        string
	Category    string
	Capability  string
	Adapter     string
	Intent      string
	Policy      map[string]any
	FailureIsOK bool
}

func mcpRemoteControlAuditProbes(hostOS string) []mcpSupportSessionAuditProbe {
	desktopFailureIsOK := !strings.EqualFold(hostOS, "windows")
	return []mcpSupportSessionAuditProbe{
		{
			Name:       "file_adapter_list",
			Category:   "remote_control",
			Capability: "file.transfer.read",
			Adapter:    "file",
			Intent:     "remote-control smoke file adapter list",
			Policy:     mcpFileListSmokePolicy(),
		},
		{
			Name:        "desktop_window_inspect",
			Category:    "remote_control",
			Capability:  "window.inspect",
			Adapter:     "desktop",
			Intent:      "remote-control smoke desktop window inspect",
			Policy:      mcpDesktopWindowInspectSmokePolicy(),
			FailureIsOK: desktopFailureIsOK,
		},
	}
}

func mcpFileListSmokePolicy() map[string]any {
	return map[string]any{
		"workspace_root":       policy.DefaultWorkspaceRoot,
		"capabilities":         []string{"file.transfer.read", "fs.read"},
		"action":               "list",
		"path":                 ".",
		"max_bytes":            1024 * 1024,
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	}
}

func mcpDesktopWindowInspectSmokePolicy() map[string]any {
	return map[string]any{
		"workspace_root":       policy.DefaultWorkspaceRoot,
		"capabilities":         []string{"window.inspect"},
		"action":               "window.inspect",
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	}
}

func mcpAuditCapabilityProbes(hostOS string) []mcpSupportSessionAuditProbe {
	if strings.EqualFold(hostOS, "windows") {
		return []mcpSupportSessionAuditProbe{
			{Name: "identity", Capability: "shell.user", Adapter: "shell", Intent: "capability audit identity", Policy: mcpShellAuditPolicy([]string{"shell.user"}, []string{"cmd", "/c", "hostname && whoami && cd"}, []string{"cmd"})},
			{Name: "powershell_identity", Capability: "powershell.user", Adapter: "powershell", Intent: "capability audit PowerShell identity", Policy: mcpPowerShellAuditPolicy("Write-Output $env:COMPUTERNAME; whoami; Get-Location")},
			{Name: "fs_read", Capability: "fs.read", Adapter: "shell", Intent: "capability audit scoped read", Policy: mcpShellAuditPolicy([]string{"shell.user", "fs.read"}, []string{"cmd", "/c", "dir /b ."}, []string{"cmd"})},
			{Name: "fs_write_scoped", Capability: "fs.write.scoped", Adapter: "shell", Intent: "capability audit scoped write", Policy: mcpShellAuditPolicyWithWriteScope([]string{"shell.user", "fs.write.scoped"}, []string{"cmd", "/c", "echo rdev-audit> rdev_capability_audit.txt && type rdev_capability_audit.txt && del rdev_capability_audit.txt && if not exist rdev_capability_audit.txt echo deleted"}, []string{"cmd"}, []string{"."})},
			{Name: "process_inspect", Capability: "process.inspect", Adapter: "shell", Intent: "capability audit process inspect", Policy: mcpShellAuditPolicy([]string{"shell.user", "process.inspect"}, []string{"tasklist"}, []string{"tasklist"})},
		}
	}
	return []mcpSupportSessionAuditProbe{
		{Name: "identity", Capability: "shell.user", Adapter: "shell", Intent: "capability audit identity", Policy: mcpShellAuditPolicy([]string{"shell.user"}, []string{"sh", "-c", "hostname && whoami && pwd"}, []string{"sh"})},
		{Name: "fs_read", Capability: "fs.read", Adapter: "shell", Intent: "capability audit scoped read", Policy: mcpShellAuditPolicy([]string{"shell.user", "fs.read"}, []string{"sh", "-c", "ls -la . | head -40"}, []string{"sh"})},
		{Name: "fs_write_scoped", Capability: "fs.write.scoped", Adapter: "shell", Intent: "capability audit scoped write", Policy: mcpShellAuditPolicyWithWriteScope([]string{"shell.user", "fs.write.scoped"}, []string{"sh", "-c", "printf rdev-audit > rdev_capability_audit.txt && cat rdev_capability_audit.txt && rm rdev_capability_audit.txt && test ! -e rdev_capability_audit.txt && echo deleted"}, []string{"sh"}, []string{"."})},
		{Name: "process_inspect", Capability: "process.inspect", Adapter: "shell", Intent: "capability audit process inspect", Policy: mcpShellAuditPolicy([]string{"shell.user", "process.inspect"}, []string{"sh", "-c", "ps -o pid,comm= -p $$"}, []string{"sh"})},
	}
}

func mcpShellAuditPolicy(capabilities, argv, allowCommands []string) map[string]any {
	return mcpShellAuditPolicyWithWriteScope(capabilities, argv, allowCommands, nil)
}

func mcpPowerShellAuditPolicy(command string) map[string]any {
	return map[string]any{
		"workspace_root":       policy.DefaultWorkspaceRoot,
		"capabilities":         []string{"powershell.user"},
		"command":              command,
		"allow_commands":       []string{"powershell.exe", "powershell", "pwsh"},
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	}
}

func mcpShellAuditPolicyWithWriteScope(capabilities, argv, allowCommands, writeScope []string) map[string]any {
	taskPolicy := map[string]any{
		"workspace_root":       policy.DefaultWorkspaceRoot,
		"capabilities":         capabilities,
		"argv":                 argv,
		"allow_commands":       allowCommands,
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	}
	if len(writeScope) > 0 {
		taskPolicy["write_scope"] = writeScope
	}
	return taskPolicy
}

func (s Server) connectionEntryPlan(args map[string]any) (any, error) {
	return connectionentry.FromInvite(connectionentry.Options{
		InviteJSON:                     requiredString(args, "invite_json"),
		OutDir:                         stringArg(args, "out_dir", ""),
		TargetOS:                       stringArg(args, "target_os", ""),
		TargetArch:                     stringArg(args, "target_arch", ""),
		Ownership:                      stringArg(args, "ownership", ""),
		SessionMode:                    stringArg(args, "session_mode", ""),
		ReleaseBundleURL:               stringArg(args, "release_bundle_url", ""),
		ReleaseBundleRequiredArtifacts: stringArg(args, "release_bundle_required_artifacts", ""),
		ReleaseBundlePath:              stringArg(args, "release_bundle_path", ""),
		ReleaseRootPublicKey:           stringArg(args, "release_root_public_key", ""),
		ManagedBinaryPath:              stringArg(args, "managed_binary_path", ""),
		ManagedServiceName:             stringArg(args, "managed_service_name", ""),
		ManagedServiceLabel:            stringArg(args, "managed_service_label", ""),
		ManagedUnitName:                stringArg(args, "managed_unit_name", ""),
		WindowsHostDownloadURL:         stringArg(args, "windows_host_download_url", ""),
		WindowsHostExpectedSHA256:      stringArg(args, "windows_host_sha256", ""),
		WindowsVerifierDownloadURL:     stringArg(args, "windows_verifier_download_url", ""),
		WindowsVerifierExpectedSHA256:  stringArg(args, "windows_verifier_sha256", ""),
		WindowsBootstrapScriptPath:     stringArg(args, "windows_bootstrap_script", ""),
		WindowsBootstrapScriptURL:      stringArg(args, "windows_bootstrap_script_url", ""),
		WindowsBootstrapScriptSHA256:   stringArg(args, "windows_bootstrap_script_sha256", ""),
		HostName:                       stringArg(args, "host_name", ""),
		RdevCommand:                    stringArg(args, "rdev_command", ""),
		Force:                          boolArg(args, "force", false),
	})
}

func (s Server) scaffoldAcceptanceEvidence(args map[string]any) (any, error) {
	return evidenceplan.Build(evidenceplan.Options{
		PlanPath:                  stringArg(args, "plan", ""),
		HostedProviderPackagePath: stringArg(args, "hosted_provider_package", ""),
		RelayAdapterPackagePath:   stringArg(args, "relay_adapter_package", ""),
		OutDir:                    requiredString(args, "out_dir"),
		PackageDir:                stringArg(args, "package_dir", ""),
		CreatePlaceholders:        boolArg(args, "create_placeholders", false),
		Force:                     boolArg(args, "force", false),
		GeneratedAt:               time.Now().UTC(),
	})
}

func (s Server) acceptanceEvidenceStatus(args map[string]any) (any, error) {
	return evidenceplan.StatusForScaffold(evidenceplan.StatusOptions{
		ScaffoldPath: requiredString(args, "scaffold"),
		GeneratedAt:  time.Now().UTC(),
	})
}

func (s Server) scaffoldPostReleaseDownloadEvidence(args map[string]any) (any, error) {
	return acceptance.ScaffoldPostReleaseDownloadEvidence(acceptance.PostReleaseDownloadScaffoldOptions{
		PostReleaseInstallDir: stringArg(args, "post_release_install_dir", ""),
		PlanPath:              stringArg(args, "plan", ""),
		PlanVerificationPath:  stringArg(args, "plan_verification", ""),
		OutDir:                requiredString(args, "out_dir"),
		CreatePlaceholders:    boolArg(args, "create_placeholders", false),
		Force:                 boolArg(args, "force", false),
		Now:                   time.Now().UTC(),
	})
}

func (s Server) postReleaseDownloadEvidenceStatus(args map[string]any) (any, error) {
	return acceptance.StatusPostReleaseDownloadEvidence(acceptance.PostReleaseDownloadStatusOptions{
		ScaffoldPath: requiredString(args, "scaffold"),
		Now:          time.Now().UTC(),
	})
}

func (s Server) releaseEvidenceIndex(args map[string]any) (any, error) {
	return acceptance.BuildReleaseEvidenceIndex(acceptance.ReleaseEvidenceIndexOptions{
		OutDir:                           requiredString(args, "out_dir"),
		HostedProviderRuntimePackagePath: stringArg(args, "hosted_provider_runtime_package", ""),
		RelayAdapterPackagePaths:         stringSliceArg(args, "relay_adapter_packages"),
		PostReleaseDownloadPackagePath:   stringArg(args, "post_release_download_package", ""),
		Now:                              time.Now().UTC(),
	})
}

func (s Server) relayAdapterPackage(args map[string]any) (any, error) {
	return relayadapter.Build(relayadapter.Options{
		OutDir:      requiredString(args, "out_dir"),
		Name:        stringArg(args, "name", ""),
		AdapterKind: stringArg(args, "adapter", "chisel"),
		GeneratedAt: time.Now().UTC(),
		Force:       boolArg(args, "force", false),
	})
}

func (s Server) relayAdapterVerify(args map[string]any) (any, error) {
	return relayadapter.Verify(requiredString(args, "package"))
}

func (s Server) createTicket(args map[string]any) (any, error) {
	mode := model.HostMode(stringArg(args, "mode", string(model.HostModeAttendedTemporary)))
	ttl := intArg(args, "ttl_seconds", 7200)
	reason := stringArg(args, "reason", "remote support")
	capabilities := stringSliceArg(args, "capabilities")
	ticket, err := s.Gateway.CreateTicket(mode, ttl, capabilities, reason)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ticket":                ticket,
		"joinUrl":               publicExampleJoinURL(ticket.Code),
		"manifestRootPublicKey": manifestRootPublicKey(s.Gateway.ManifestRoot()),
	}, nil
}

func publicExampleJoinURL(ticketCode string) string {
	return "https://agent.example.com/join/" + ticketCode
}

func manifestRootPublicKey(root model.TrustBundle) string {
	if root.SigningKeyID == "" || root.PublicKey == "" {
		return ""
	}
	return root.SigningKeyID + ":" + root.PublicKey
}

func (s Server) revokeTicket(args map[string]any) (any, error) {
	return s.Gateway.RevokeTicket(requiredString(args, "ticket_id"), stringArg(args, "reason", ""))
}

func (s Server) queryAudit(args map[string]any) (any, error) {
	targetID := stringArg(args, "target_id", "")
	limit := intArg(args, "limit", 100)
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		path := fmt.Sprintf("/v1/audit?limit=%d", limit)
		if targetID != "" {
			path += "&target_id=" + url.QueryEscape(targetID)
		}
		return s.proxyGETTo(gwURL, path)
	}
	events := s.Gateway.AuditEvents()
	filtered := make([]model.AuditEvent, 0, len(events))
	for _, event := range events {
		if targetID == "" || event.TargetID == targetID {
			filtered = append(filtered, event)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return map[string]any{"events": filtered}, nil
}

func (s Server) explainPolicy(args map[string]any) (any, error) {
	return policy.Explain(
		model.HostMode(requiredString(args, "mode")),
		policy.Capability(requiredString(args, "capability")),
	), nil
}

func (s Server) explainShellPolicy(args map[string]any) (any, error) {
	return policy.ExplainShellTask(
		model.HostMode(requiredString(args, "mode")),
		objectArg(args, "policy"),
	), nil
}

const EnrollmentCertificateVerificationSchemaVersion = "rdev.enrollment-certificate-verification.v1"

type enrollmentCertificateVerificationReport struct {
	SchemaVersion              string         `json:"schema_version"`
	OK                         bool           `json:"ok"`
	CertificateSchema          string         `json:"certificate_schema,omitempty"`
	CertificateFingerprint     string         `json:"certificate_fingerprint,omitempty"`
	RevocationListSchema       string         `json:"revocation_list_schema,omitempty"`
	RevokedCertificateCount    int            `json:"revoked_certificate_count,omitempty"`
	IssuerKeyID                string         `json:"issuer_key_id,omitempty"`
	RootKeyID                  string         `json:"root_key_id,omitempty"`
	SubjectIdentityFingerprint string         `json:"subject_identity_fingerprint,omitempty"`
	TicketCode                 string         `json:"ticket_code,omitempty"`
	Mode                       model.HostMode `json:"mode,omitempty"`
	NotBefore                  *time.Time     `json:"not_before,omitempty"`
	NotAfter                   *time.Time     `json:"not_after,omitempty"`
	VerifiedAt                 time.Time      `json:"verified_at"`
	Checks                     []string       `json:"checks"`
	Errors                     []string       `json:"errors,omitempty"`
	RecommendedActions         []string       `json:"recommended_actions,omitempty"`
}

func (s Server) verifyEnrollmentCertificate(args map[string]any) (any, error) {
	certificateJSON := stringArg(args, "certificate_json", "")
	if certificateJSON == "" {
		return nil, fmt.Errorf("certificate_json is required")
	}
	rootPublicKey := requiredString(args, "root_public_key")
	verifyAt := time.Now().UTC()
	if value := stringArg(args, "verify_at", ""); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, fmt.Errorf("verify_at must be RFC3339: %w", err)
		}
		verifyAt = parsed.UTC()
	}
	report := enrollmentCertificateVerificationReport{
		SchemaVersion: EnrollmentCertificateVerificationSchemaVersion,
		VerifiedAt:    verifyAt,
		Checks:        []string{},
	}
	certificate, err := decodeEnrollmentCertificateJSON([]byte(certificateJSON))
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		report.RecommendedActions = append(report.RecommendedActions, "provide a JSON object using schema rdev.host-enrollment-certificate.v1 or a wrapper with certificate/enrollment_certificate")
		return report, nil
	}
	report.CertificateSchema = certificate.SchemaVersion
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, nil
	}
	report.CertificateFingerprint = fingerprint
	report.IssuerKeyID = certificate.IssuerKeyID
	report.SubjectIdentityFingerprint = certificate.SubjectIdentityFingerprint
	report.TicketCode = certificate.TicketCode
	report.Mode = certificate.Mode
	notBefore := certificate.NotBefore.UTC()
	notAfter := certificate.NotAfter.UTC()
	report.NotBefore = &notBefore
	report.NotAfter = &notAfter
	report.Checks = append(report.Checks, "certificate_json_decoded")
	root, err := trustref.Parse(rootPublicKey)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		report.RecommendedActions = append(report.RecommendedActions, "pin the enrollment root as key_id:base64url_ed25519_public_key")
		return report, nil
	}
	report.RootKeyID = root.SigningKeyID
	report.Checks = append(report.Checks, "root_public_key_decoded")
	if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, verifyAt); err != nil {
		report.Errors = append(report.Errors, err.Error())
		report.RecommendedActions = append(report.RecommendedActions, "reject this host registration until a valid enrollment certificate is presented")
		return report, nil
	}
	report.Checks = append(report.Checks, "signature_valid", "validity_window_active", "issuer_matches_root")
	if revocationsJSON := stringArg(args, "revocations_json", ""); revocationsJSON != "" {
		revocations, err := decodeEnrollmentRevocationListJSON([]byte(revocationsJSON))
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			report.RecommendedActions = append(report.RecommendedActions, "provide a JSON object using schema rdev.host-enrollment-revocations.v1")
			return report, nil
		}
		report.RevocationListSchema = revocations.SchemaVersion
		report.RevokedCertificateCount = len(revocations.RevokedCertificates)
		report.Checks = append(report.Checks, "revocation_list_json_decoded")
		if err := model.VerifyHostEnrollmentRevocationListSignature(revocations, root, verifyAt); err != nil {
			report.Errors = append(report.Errors, err.Error())
			report.RecommendedActions = append(report.RecommendedActions, "refresh the enrollment revocation list from the trusted authority before trusting this registration")
			return report, nil
		}
		report.Checks = append(report.Checks, "revocation_list_signature_valid", "revocation_list_fresh")
		if err := model.VerifyHostEnrollmentCertificateNotRevoked(certificate, revocations); err != nil {
			report.Errors = append(report.Errors, err.Error())
			report.RecommendedActions = append(report.RecommendedActions, "reject this host registration because its enrollment certificate is revoked")
			return report, nil
		}
		report.Checks = append(report.Checks, "certificate_not_revoked")
	}
	report.OK = true
	return report, nil
}

func decodeEnrollmentCertificateJSON(content []byte) (model.HostEnrollmentCertificate, error) {
	var certificate model.HostEnrollmentCertificate
	if err := json.Unmarshal(content, &certificate); err == nil && certificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion {
		return certificate, nil
	}
	var wrapped struct {
		Certificate           model.HostEnrollmentCertificate `json:"certificate"`
		EnrollmentCertificate model.HostEnrollmentCertificate `json:"enrollment_certificate"`
	}
	if err := json.Unmarshal(content, &wrapped); err != nil {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("decode enrollment certificate JSON: %w", err)
	}
	switch {
	case wrapped.Certificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion:
		return wrapped.Certificate, nil
	case wrapped.EnrollmentCertificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion:
		return wrapped.EnrollmentCertificate, nil
	default:
		return model.HostEnrollmentCertificate{}, fmt.Errorf("unsupported enrollment certificate schema")
	}
}

func decodeEnrollmentRevocationListJSON(content []byte) (model.HostEnrollmentRevocationList, error) {
	var list model.HostEnrollmentRevocationList
	if err := json.Unmarshal(content, &list); err != nil {
		return model.HostEnrollmentRevocationList{}, fmt.Errorf("decode enrollment revocation list JSON: %w", err)
	}
	if list.SchemaVersion != model.HostEnrollmentRevocationListSchemaVersion {
		return model.HostEnrollmentRevocationList{}, fmt.Errorf("unsupported enrollment revocation list schema %q", list.SchemaVersion)
	}
	return list, nil
}

func (s Server) verifyAdapterResult(args map[string]any) (any, error) {
	artifactJSON := stringArg(args, "artifact_json", "")
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json is required")
	}
	requiredFields := stringSliceArg(args, "required_string_fields")
	if len(requiredFields) == 0 {
		requiredFields = []string{"workspace_root"}
	}
	return adapterkit.VerifyResultArtifactJSON([]byte(artifactJSON), adapterkit.ResultArtifactContract{
		Adapter:                 requiredString(args, "adapter"),
		SchemaVersion:           requiredString(args, "schema"),
		CommandFields:           stringSliceArg(args, "command_fields"),
		RequiredStringFields:    requiredFields,
		RequireTiming:           boolArg(args, "require_timing", true),
		RequireRedaction:        boolArg(args, "require_redaction", true),
		RejectUnredactedSecrets: boolArg(args, "reject_secret_patterns", true),
	}), nil
}

func (s Server) verifyAdapterLifecycle(args map[string]any) (any, error) {
	artifactJSON := stringArg(args, "artifact_json", "")
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json is required")
	}
	schema := stringArg(args, "schema", adapterkit.LifecycleManifestSchemaVersion)
	return adapterkit.VerifyLifecycleManifestJSON([]byte(artifactJSON), adapterkit.LifecycleContract{
		Adapter:                 requiredString(args, "adapter"),
		SchemaVersion:           schema,
		RequiredPhases:          stringSliceArg(args, "required_phases"),
		RequireSafety:           boolArg(args, "require_safety", true),
		RequireCancellation:     boolArg(args, "require_cancellation", true),
		RequireResultSchema:     boolArg(args, "require_result_schema", true),
		RejectUnredactedSecrets: boolArg(args, "reject_secret_patterns", true),
	}), nil
}

func (s Server) verifyAdapterCancellation(args map[string]any) (any, error) {
	artifactJSON := stringArg(args, "artifact_json", "")
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json is required")
	}
	requiredFields := stringSliceArg(args, "required_string_fields")
	if len(requiredFields) == 0 {
		requiredFields = []string{"workspace_root"}
	}
	return adapterkit.VerifyCancellationArtifactJSON([]byte(artifactJSON), adapterkit.CancellationContract{
		Adapter:                 requiredString(args, "adapter"),
		SchemaVersion:           requiredString(args, "schema"),
		CommandFields:           stringSliceArg(args, "command_fields"),
		RequiredStringFields:    requiredFields,
		RequireTiming:           boolArg(args, "require_timing", true),
		RequireRedaction:        boolArg(args, "require_redaction", true),
		RejectUnredactedSecrets: boolArg(args, "reject_secret_patterns", true),
	}), nil
}

func (s Server) verifyAdapterRuntime(args map[string]any) (any, error) {
	artifactJSON := stringArg(args, "artifact_json", "")
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json is required")
	}
	return adapterkit.VerifyRuntimeFixtureJSON([]byte(artifactJSON), adapterkit.RuntimeFixtureContract{
		Adapter:               requiredString(args, "adapter"),
		RequiredPhases:        stringSliceArg(args, "required_phases"),
		RequireSuccessful:     boolArg(args, "require_successful", true),
		RequireCleanup:        boolArg(args, "require_cleanup", true),
		RequireResultArtifact: boolArg(args, "require_result_artifact", false),
		RequireCancellation:   boolArg(args, "require_cancellation", false),
	}), nil
}

func success(id any, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id any, code int, message string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func toolResult(data any) (map[string]any, error) {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(bytes)},
		},
		"structuredContent": data,
	}, nil
}

func nestedAny(value any, key string) any {
	object, _ := value.(map[string]any)
	if object == nil {
		return nil
	}
	return object[key]
}

func nestedMapOrSelfAny(value any, objectKey string) map[string]any {
	object, _ := value.(map[string]any)
	if object == nil {
		return map[string]any{}
	}
	nested, _ := object[objectKey].(map[string]any)
	if nested != nil {
		return nested
	}
	return object
}

func mapSliceFromAny(value any) []map[string]any {
	if typed, ok := value.([]map[string]any); ok {
		return typed
	}
	raw, _ := value.([]any)
	if raw != nil {
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			} else if m := structToMap(item); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	switch typed := value.(type) {
	case []model.Host:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, structToMap(item))
		}
		return out
	}
	return nil
}

func structToMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func stringMapValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func summarizeFirstArtifactMap(payload map[string]any) string {
	values, _ := payload["artifacts"].([]any)
	if len(values) == 0 {
		return ""
	}
	first, _ := values[0].(map[string]any)
	content, _ := first["content"].(string)
	content = strings.TrimSpace(content)
	if len(content) > 300 {
		content = content[:300] + "..."
	}
	return content
}

func supportSessionHumanReportMap(host map[string]any, tasks []map[string]any) string {
	var b strings.Builder
	hostName := firstReportFieldMap(host, "name", "hostname", "id")
	hostOS := firstReportFieldMap(host, "os")
	hostArch := firstReportFieldMap(host, "arch")
	if hostName == "" {
		hostName = "unknown-host"
	}
	_, _ = fmt.Fprintf(&b, "Remote Dev Skillkit support-session report\n")
	_, _ = fmt.Fprintf(&b, "- Host: %s", hostName)
	if hostOS != "" || hostArch != "" {
		_, _ = fmt.Fprintf(&b, " (%s %s)", hostOS, hostArch)
	}
	_, _ = fmt.Fprintf(&b, "\n- Tasks reviewed: %d\n", len(tasks))
	for _, task := range tasks {
		_, _ = fmt.Fprintf(&b, "- %s: %s", task["task_id"], task["status"])
		if intent, _ := task["intent"].(string); intent != "" {
			_, _ = fmt.Fprintf(&b, " - %s", intent)
		}
		_, _ = fmt.Fprint(&b, "\n")
	}
	_, _ = fmt.Fprint(&b, "- Connection: keep alive until the operator explicitly asks to disconnect, revoke, or stop it.")
	return b.String()
}

func supportSessionSmokeHumanReportMap(audit map[string]any) string {
	var b strings.Builder
	_, _ = fmt.Fprintln(&b, "Remote Dev Skillkit smoke-test report")
	if host, _ := audit["host"].(map[string]any); host != nil {
		_, _ = fmt.Fprintf(&b, "- Host: %s (%s %s)\n", firstReportFieldMap(host, "name", "hostname", "id"), firstReportFieldMap(host, "os"), firstReportFieldMap(host, "arch"))
	}
	for _, result := range mapSliceFromAny(audit["results"]) {
		_, _ = fmt.Fprintf(&b, "- %s: ok=%v status=%s\n", stringMapValue(result, "name"), result["ok"], firstReportFieldMap(result, "task_status", "status"))
	}
	_, _ = fmt.Fprint(&b, "- Connection: keep alive until the operator explicitly asks to disconnect.")
	return b.String()
}

func supportSessionRemoteControlEntryMap(gatewayURL, ticketCode string, status, host map[string]any) map[string]any {
	if status != nil {
		if entry, ok := status["remote_control_entry"].(map[string]any); ok && len(entry) > 0 {
			return entry
		}
	}
	hosts := []model.Host{}
	if host != nil {
		hosts = append(hosts, model.Host{
			ID:                  firstReportFieldMap(host, "id", "host_id"),
			TicketID:            firstReportFieldMap(host, "ticket_id"),
			Mode:                model.HostMode(firstReportFieldMap(host, "mode")),
			Status:              model.HostStatus(firstReportFieldMap(host, "status")),
			Name:                firstReportFieldMap(host, "name", "hostname"),
			OS:                  firstReportFieldMap(host, "os"),
			Arch:                firstReportFieldMap(host, "arch"),
			Capabilities:        stringSliceFromAnyMap(host["capabilities"]),
			IdentityKeyID:       firstReportFieldMap(host, "identity_key_id"),
			IdentityPublicKey:   firstReportFieldMap(host, "identity_public_key"),
			IdentityFingerprint: firstReportFieldMap(host, "identity_fingerprint"),
		})
	}
	return supportsession.BuildRemoteControlEntry(supportsession.RemoteControlEntryOptions{
		GatewayURL: gatewayURL,
		TicketCode: ticketCode,
		Hosts:      hosts,
		Locale:     "auto",
	})
}

func stringSliceFromAnyMap(value any) []string {
	if typed, ok := value.([]string); ok {
		return typed
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && text != "" {
			values = append(values, text)
		}
	}
	return values
}

func supportSessionConnectionContinuityMap(gatewayURL string) map[string]any {
	gatewayURL = strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
	ephemeral := strings.Contains(strings.ToLower(gatewayURL), ".trycloudflare.com")
	stableConfigured := false
	stableKinds := []string{}
	if configured, candidates := supportsession.ConfiguredGatewayURLCandidate(); configured != "" {
		stableConfigured = true
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.URL) == "" {
				continue
			}
			stableKinds = append(stableKinds, strings.TrimSpace(candidate.Kind))
		}
	}
	stable := gatewayURL != "" && !ephemeral
	return map[string]any{
		"schema_version":                          "rdev.support-session-connection-continuity.v1",
		"gateway_url":                             gatewayURL,
		"stable_gateway":                          stable,
		"ephemeral_quick_tunnel":                  ephemeral,
		"stable_configured":                       stableConfigured,
		"stable_configured_kinds":                 stableKinds,
		"managed_reconnect_ready":                 stable && stableConfigured,
		"managed_service_requires_stable_gateway": true,
		"operator_action":                         supportSessionContinuityActionMap(ephemeral, stableConfigured),
		"do_not_expect_quick_reuse":               ephemeral,
		"recommended_stable_gateway":              []string{"RDEV_HOSTED_GATEWAY_URL", "RDEV_CLOUDFLARED_NAMED_TUNNEL_URL"},
		"disconnect_requires_request":             true,
	}
}

func supportSessionManagedUpgradeRecommendationMap(host map[string]any) map[string]any {
	targetOS := "auto"
	if host != nil {
		if value := firstReportFieldMap(host, "os"); value != "" {
			targetOS = strings.ToLower(value)
		}
	}
	return map[string]any{
		"schema_version":                           "rdev.support-session-managed-upgrade.v1",
		"for_owned_recurring_hosts":                true,
		"for_third_party_temporary":                false,
		"requires_explicit_operator_authorization": true,
		"requires_stable_gateway":                  true,
		"stable_gateway_env": []string{
			"RDEV_HOSTED_GATEWAY_URL",
			"RDEV_CLOUDFLARED_NAMED_TUNNEL_URL",
		},
		"target_os":        targetOS,
		"recommended_tool": "rdev.connection_entry.plan",
		"agent_rule":       "If this is the operator's own recurring machine, ask one short ownership/persistence authorization question, configure a stable gateway, then generate a reviewed managed-service Connection Entry plan. Do not install persistence for third-party temporary support.",
	}
}

func supportSessionContinuityActionMap(ephemeral, stableConfigured bool) string {
	switch {
	case ephemeral:
		return "current gateway is a Cloudflare Quick Tunnel and will not be reusable; configure RDEV_HOSTED_GATEWAY_URL or RDEV_CLOUDFLARED_NAMED_TUNNEL_URL before relying on managed reconnect"
	case stableConfigured:
		return "stable gateway is configured; keep it running for recurring hosts and managed service reconnect"
	default:
		return "gateway is not a Quick Tunnel, but no stable RDEV_* gateway env was detected; verify DNS/IP durability before promising managed reconnect"
	}
}

func firstReportFieldMap(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringMapValue(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func oneLine(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 240 {
		return value[:240] + "..."
	}
	return value
}

func requiredString(args map[string]any, key string) string {
	value := stringArg(args, key, "")
	if value == "" {
		panic(fmt.Sprintf("missing required argument %q", key))
	}
	return value
}

func stringArg(args map[string]any, key, fallback string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

func agentRdevCommand(command string) string {
	if command = strings.TrimSpace(command); command != "" {
		return command
	}
	return skillkit.RecommendedRdevCommand()
}

func intArg(args map[string]any, key string, fallback int) int {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return fallback
	}
}

func boolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	typed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return typed
}

func policyCapabilitiesToStrings(caps []policy.Capability) []string {
	values := make([]string, 0, len(caps))
	for _, cap := range caps {
		values = append(values, string(cap))
	}
	return values
}

func stringSliceArg(args map[string]any, key string) []string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && text != "" {
			values = append(values, text)
		}
	}
	return values
}

func objectArg(args map[string]any, key string) map[string]any {
	value, ok := args[key]
	if !ok || value == nil {
		return map[string]any{}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return object
}
