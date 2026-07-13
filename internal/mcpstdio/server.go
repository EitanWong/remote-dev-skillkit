package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/skillkit"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
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
			var unknown unknownToolError
			if errors.As(err, &unknown) {
				return errorResponse(req.ID, -32602, err.Error())
			}
			return errorResponse(req.ID, -32000, err.Error())
		}
		return success(req.ID, result)
	default:
		return errorResponse(req.ID, -32601, "method not found")
	}
}

type unknownToolError struct {
	Name string
}

func (e unknownToolError) Error() string {
	return fmt.Sprintf("unknown tool %q", e.Name)
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
	case "rdev.sessions.connect":
		data, err = s.sessionsConnect(params.Arguments)
	default:
		err = unknownToolError{Name: params.Name}
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
		if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
			return s.proxyGETTo(gwURL, "/v1/sessions/"+url.PathEscape(sessionID)+"/artifacts")
		}
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

func (s Server) sessionsConnect(args map[string]any) (any, error) {
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
		Capabilities:               stringSliceArg(args, "capabilities"),
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
