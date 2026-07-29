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
)

const protocolVersion = "2025-11-25"

type Server struct {
	Gateway *gateway.MemoryGateway
	// RemoteGateway, when non-empty, proxies session operations to a configured
	// Control Plane gateway rather than the local in-memory gateway.
	RemoteGateway string
	// remoteOperatorToken authenticates only requests sent to RemoteGateway.
	// Per-call gateway_url overrides intentionally never receive this token.
	remoteOperatorToken string
	httpClient          *http.Client
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return Server{Gateway: gw}
}

// NewServerWithRemoteGateway returns a Server that proxies session operations to remoteURL.
func NewServerWithRemoteGateway(gw *gateway.MemoryGateway, remoteURL string) Server {
	return NewServerWithRemoteGatewayAndOperatorToken(gw, remoteURL, "")
}

// NewServerWithRemoteGatewayAndOperatorToken returns a remote gateway proxy
// that sends the supplied operator bearer token only to the configured gateway.
// The token is deliberately withheld from per-call gateway_url overrides.
func NewServerWithRemoteGatewayAndOperatorToken(gw *gateway.MemoryGateway, remoteURL, operatorToken string) Server {
	operatorToken = strings.TrimSpace(operatorToken)
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: retryingMCPTransport{Base: http.DefaultTransport, MaxRetries: 3},
	}
	if operatorToken != "" {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return Server{
		Gateway:             gw,
		RemoteGateway:       normalizeGatewayBaseURL(remoteURL),
		remoteOperatorToken: operatorToken,
		httpClient:          client,
	}
}

func normalizeGatewayBaseURL(raw string) string {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "/v1" {
		parsed.Path = ""
		parsed.RawPath = ""
	} else if strings.HasSuffix(path, "/v1") {
		parsed.Path = strings.TrimSuffix(path, "/v1")
		parsed.RawPath = ""
	}
	return strings.TrimRight(parsed.String(), "/")
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

type gatewayTarget struct {
	URL              string
	useOperatorToken bool
}

// effectiveGatewayTarget returns the gateway base URL and whether it came from
// the configured server-level gateway. Per-call overrides must not inherit the
// configured gateway's bearer token, even when both URLs are identical.
func (s Server) effectiveGatewayTarget(args map[string]any) gatewayTarget {
	if v := stringArg(args, "gateway_url", ""); v != "" {
		return gatewayTarget{URL: normalizeGatewayBaseURL(v)}
	}
	return gatewayTarget{URL: s.RemoteGateway, useOperatorToken: true}
}

func (s Server) remoteGatewayAuthorization(baseURL string, useOperatorToken bool) string {
	if !useOperatorToken {
		return ""
	}
	if strings.TrimSpace(s.remoteOperatorToken) == "" {
		return ""
	}
	if normalizeGatewayBaseURL(baseURL) != s.RemoteGateway {
		return ""
	}
	return "Bearer " + s.remoteOperatorToken
}

// proxyGETTo sends a GET to baseURL+path and decodes the response.
func (s Server) proxyGETTo(baseURL, path string) (any, error) {
	return s.proxyGETToTarget(baseURL, path, true)
}

func (s Server) proxyGETToTarget(baseURL, path string, useOperatorToken bool) (any, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if authorization := s.remoteGatewayAuthorization(baseURL, useOperatorToken); authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := s.remoteClient().Do(req)
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
	return s.proxyPOSTToTarget(baseURL, path, payload, true)
}

func (s Server) proxyPOSTToTarget(baseURL, path string, payload any, useOperatorToken bool) (any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if authorization := s.remoteGatewayAuthorization(baseURL, useOperatorToken); authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
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
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		return nil, fmt.Errorf("remote gateway returned redirect HTTP %d", resp.StatusCode)
	}
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
	case "rdev.sessions.handoff":
		data, err = s.sessionHandoff(params.Arguments)
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

	default:
		err = unknownToolError{Name: params.Name}
	}
	if err != nil {
		return nil, err
	}
	return toolResult(data)
}

func (s Server) createSession(args map[string]any) (any, error) {
	if target := s.effectiveGatewayTarget(args); target.URL != "" {
		spec := sessionSpecFromArgs(args)
		// A remote-proxied create must bind the session handoff to the same
		// configured gateway that received the operator-authenticated request.
		spec.SelectedGatewayURL = target.URL
		return s.proxyPOSTToTarget(target.URL, "/v1/sessions", spec, target.useOperatorToken)
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

func (s Server) sessionHandoff(args map[string]any) (any, error) {
	sessionID := requiredString(args, "session_id")
	platform := strings.TrimSpace(stringArg(args, "platform", gateway.WebHandoffPlatformWindowsAMD64))
	if platform != gateway.WebHandoffPlatformWindowsAMD64 {
		return nil, fmt.Errorf("unsupported web handoff platform %q", platform)
	}
	expiresInMS := intArg(args, "expires_in_ms", 0)
	if expiresInMS < 0 {
		return nil, fmt.Errorf("expires_in_ms must be non-negative")
	}
	target := s.effectiveGatewayTarget(args)
	if target.URL == "" {
		return nil, fmt.Errorf("session handoff requires a configured remote HTTPS gateway")
	}
	return s.proxyPOSTToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID)+"/host-handoffs", map[string]any{
		"platform":      platform,
		"expires_in_ms": expiresInMS,
	}, target.useOperatorToken)
}

func (s Server) sessionStatus(args map[string]any) (any, error) {
	sessionID := requiredString(args, "session_id")
	if target := s.effectiveGatewayTarget(args); target.URL != "" {
		return s.proxyGETToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID), target.useOperatorToken)
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
	if target := s.effectiveGatewayTarget(args); target.URL != "" {
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
		return s.proxyGETToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID)+"/events?"+query.Encode(), target.useOperatorToken)
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
	action := strings.TrimSpace(stringArg(args, "action", "submit"))
	if action == "" {
		action = "submit"
	}
	if action == "resume" {
		taskID := requiredString(args, "task_id")
		request := map[string]any{
			"checkpoint_id":   requiredString(args, "checkpoint_id"),
			"idempotency_key": requiredString(args, "idempotency_key"),
		}
		if target := s.effectiveGatewayTarget(args); target.URL != "" {
			return s.proxyPOSTToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID)+"/tasks/"+url.PathEscape(taskID)+"/resume", request, target.useOperatorToken)
		}
		task, event, err := s.Gateway.ResumeSessionTask(sessionID, taskID, request["checkpoint_id"].(string), request["idempotency_key"].(string))
		if err != nil {
			return nil, err
		}
		session, err := s.Gateway.Session(sessionID)
		if err != nil {
			return nil, err
		}
		status := session.DeriveStatus()
		return withSessionStatus(map[string]any{"task": task, "event": event, "status": status}, status), nil
	}
	if action != "submit" {
		return nil, fmt.Errorf("unsupported task action %q", action)
	}
	spec, err := sessionTaskSpecFromArgs(args)
	if err != nil {
		return nil, err
	}
	if target := s.effectiveGatewayTarget(args); target.URL != "" {
		return s.proxyPOSTToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID)+"/tasks", spec, target.useOperatorToken)
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

func sessionTaskSpecFromArgs(args map[string]any) (controlplane.TaskSpec, error) {
	adapter := strings.TrimSpace(stringArg(args, "adapter", ""))
	if adapter == "" {
		return controlplane.TaskSpec{}, fmt.Errorf("adapter is required")
	}
	payload := objectArg(args, "payload")
	if raw, ok := args["toolchain_request"]; ok && raw != nil {
		if adapter != "toolchain" {
			return controlplane.TaskSpec{}, fmt.Errorf("toolchain_request requires adapter %q", "toolchain")
		}
		if len(payload) != 0 {
			return controlplane.TaskSpec{}, fmt.Errorf("toolchain_request does not accept an additional payload object")
		}
		request, err := contracts.DecodeToolchainRequest(raw)
		if err != nil {
			return controlplane.TaskSpec{}, err
		}
		if !sameStringSet(stringSliceArg(args, "capabilities"), contracts.ToolchainRequiredCapabilities()) {
			return controlplane.TaskSpec{}, fmt.Errorf("toolchain_request capabilities must exactly match package-install authorization")
		}
		intent := strings.TrimSpace(stringArg(args, "intent", ""))
		if intent == "" {
			intent = "ensure " + request.Tool + " toolchain"
		}
		return controlplane.TaskSpec{
			TargetEndpointID: stringArg(args, "target_endpoint_id", ""),
			Adapter:          adapter,
			Intent:           intent,
			Capabilities:     contracts.ToolchainRequiredCapabilities(),
			Payload:          request.TaskPayload(),
			Limits:           objectArg(args, "limits"),
			IdempotencyKey:   requiredString(args, "idempotency_key"),
		}, nil
	}
	if raw, ok := args["engineering_task"]; ok && raw != nil {
		engineering, err := contracts.DecodeEngineeringTask(raw)
		if err != nil {
			return controlplane.TaskSpec{}, err
		}
		if err := engineering.ValidateForAdapter(adapter); err != nil {
			return controlplane.TaskSpec{}, err
		}
		outerCapabilities := stringSliceArg(args, "capabilities")
		if len(outerCapabilities) > 0 && !sameStringSet(outerCapabilities, engineering.RequiredCapabilities) {
			return controlplane.TaskSpec{}, fmt.Errorf("capabilities must exactly match engineering_task.required_capabilities")
		}
		outerIntent := strings.TrimSpace(stringArg(args, "intent", ""))
		if outerIntent != "" && outerIntent != engineering.Goal {
			return controlplane.TaskSpec{}, fmt.Errorf("intent must match engineering_task.goal when engineering_task is present")
		}
		outerKey := strings.TrimSpace(stringArg(args, "idempotency_key", ""))
		if outerKey != "" && outerKey != engineering.IdempotencyKey {
			return controlplane.TaskSpec{}, fmt.Errorf("idempotency_key must match engineering_task.idempotency_key")
		}
		return controlplane.TaskSpec{
			TargetEndpointID: stringArg(args, "target_endpoint_id", ""),
			Adapter:          adapter,
			Intent:           engineering.Goal,
			Capabilities:     append([]string(nil), engineering.RequiredCapabilities...),
			Payload:          engineering.TaskPayload(payload),
			Limits:           engineering.TaskLimits(),
			IdempotencyKey:   engineering.IdempotencyKey,
		}, nil
	}
	idempotencyKey := strings.TrimSpace(stringArg(args, "idempotency_key", ""))
	if idempotencyKey == "" {
		return controlplane.TaskSpec{}, fmt.Errorf("idempotency_key is required")
	}
	return controlplane.TaskSpec{
		TargetEndpointID: stringArg(args, "target_endpoint_id", ""),
		Adapter:          adapter,
		Intent:           stringArg(args, "intent", ""),
		Capabilities:     stringSliceArg(args, "capabilities"),
		Payload:          payload,
		Limits:           objectArg(args, "limits"),
		IdempotencyKey:   idempotencyKey,
	}, nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	values := make(map[string]bool, len(left))
	for _, value := range left {
		if value == "" || values[value] {
			return false
		}
		values[value] = true
	}
	for _, value := range right {
		if !values[value] {
			return false
		}
	}
	return true
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
	if target := s.effectiveGatewayTarget(args); target.URL != "" {
		return s.proxyPOSTToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID)+"/events", event, target.useOperatorToken)
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
		if target := s.effectiveGatewayTarget(args); target.URL != "" {
			return s.proxyGETToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID)+"/artifacts", target.useOperatorToken)
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
	if target := s.effectiveGatewayTarget(args); target.URL != "" {
		return s.proxyPOSTToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID)+"/artifacts", ref, target.useOperatorToken)
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
	action := strings.TrimSpace(stringArg(args, "action", "close"))
	if action == "" {
		action = "close"
	}
	if action != "close" && action != "revoke" {
		return nil, fmt.Errorf("unsupported session close action %q", action)
	}
	if target := s.effectiveGatewayTarget(args); target.URL != "" {
		return s.proxyPOSTToTarget(target.URL, "/v1/sessions/"+url.PathEscape(sessionID)+"/"+action, map[string]any{
			"reason":          stringArg(args, "reason", ""),
			"idempotency_key": stringArg(args, "idempotency_key", ""),
		}, target.useOperatorToken)
	}
	var (
		session controlplane.Session
		event   controlplane.Event
		err     error
	)
	if action == "revoke" {
		session, event, err = s.Gateway.RevokeSession(sessionID)
	} else {
		session, event, err = s.Gateway.CloseSession(sessionID)
	}
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
