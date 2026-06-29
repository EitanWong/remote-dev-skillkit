package mcpstdio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
)

const protocolVersion = "2025-11-25"

type Server struct {
	Gateway *gateway.MemoryGateway
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return Server{Gateway: gw}
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
	case "rdev.tickets.create":
		data, err = s.createTicket(params.Arguments)
	case "rdev.tickets.revoke":
		data, err = s.revokeTicket(params.Arguments)
	case "rdev.hosts.list":
		data, err = s.listHosts(params.Arguments)
	case "rdev.hosts.capabilities":
		data, err = s.hostCapabilities(params.Arguments)
	case "rdev.hosts.approve":
		data, err = s.approveHost(params.Arguments)
	case "rdev.hosts.revoke":
		data, err = s.revokeHost(params.Arguments)
	case "rdev.jobs.create":
		data, err = s.createJob(params.Arguments)
	case "rdev.jobs.status":
		data, err = s.jobStatus(params.Arguments)
	case "rdev.jobs.cancel":
		data, err = s.cancelJob(params.Arguments)
	case "rdev.jobs.approve":
		data, err = s.approveJob(params.Arguments)
	case "rdev.artifacts.list":
		data, err = s.listArtifacts(params.Arguments)
	case "rdev.artifacts.read":
		data, err = s.readArtifact(params.Arguments)
	case "rdev.audit.query":
		data, err = s.queryAudit(params.Arguments)
	case "rdev.policy.explain":
		data, err = s.explainPolicy(params.Arguments)
	case "rdev.policy.explain_shell":
		data, err = s.explainShellPolicy(params.Arguments)
	default:
		err = fmt.Errorf("unknown tool %q", params.Name)
	}
	if err != nil {
		return nil, err
	}
	return toolResult(data)
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
		"ticket":  ticket,
		"joinUrl": "https://agent.lunflux.com/join/" + ticket.Code,
	}, nil
}

func (s Server) revokeTicket(args map[string]any) (any, error) {
	return s.Gateway.RevokeTicket(requiredString(args, "ticket_id"), stringArg(args, "reason", ""))
}

func (s Server) listHosts(args map[string]any) (any, error) {
	return map[string]any{"hosts": s.Gateway.Hosts(stringArg(args, "status", ""))}, nil
}

func (s Server) hostCapabilities(args map[string]any) (any, error) {
	host, err := s.Gateway.Host(requiredString(args, "host_id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"host_id":      host.ID,
		"status":       host.Status,
		"capabilities": host.Capabilities,
	}, nil
}

func (s Server) approveHost(args map[string]any) (any, error) {
	return s.Gateway.ApproveHost(requiredString(args, "host_id"), stringSliceArg(args, "capabilities"))
}

func (s Server) revokeHost(args map[string]any) (any, error) {
	return s.Gateway.RevokeHost(requiredString(args, "host_id"), stringArg(args, "reason", ""))
}

func (s Server) createJob(args map[string]any) (any, error) {
	return s.Gateway.CreateJob(
		requiredString(args, "host_id"),
		requiredString(args, "adapter"),
		requiredString(args, "intent"),
		objectArg(args, "policy"),
	)
}

func (s Server) jobStatus(args map[string]any) (any, error) {
	return s.Gateway.Job(requiredString(args, "job_id"))
}

func (s Server) cancelJob(args map[string]any) (any, error) {
	return s.Gateway.CancelJob(requiredString(args, "job_id"), stringArg(args, "reason", ""))
}

func (s Server) approveJob(args map[string]any) (any, error) {
	return s.Gateway.ApproveJob(
		requiredString(args, "job_id"),
		requiredString(args, "approval_id"),
		requiredString(args, "decision"),
		stringArg(args, "reason", ""),
	)
}

func (s Server) listArtifacts(args map[string]any) (any, error) {
	return map[string]any{"artifacts": s.Gateway.Artifacts(requiredString(args, "job_id"))}, nil
}

func (s Server) readArtifact(args map[string]any) (any, error) {
	return s.Gateway.Artifact(requiredString(args, "artifact_id"))
}

func (s Server) queryAudit(args map[string]any) (any, error) {
	targetID := stringArg(args, "target_id", "")
	limit := intArg(args, "limit", 100)
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
	return policy.ExplainShellJob(
		model.HostMode(requiredString(args, "mode")),
		objectArg(args, "policy"),
	), nil
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
