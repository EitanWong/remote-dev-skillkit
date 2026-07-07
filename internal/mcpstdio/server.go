package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
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
	"github.com/EitanWong/remote-dev-skillkit/internal/evidenceplan"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/jobtemplate"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/skillkit"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
	"github.com/EitanWong/remote-dev-skillkit/internal/update"
	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
)

const protocolVersion = "2025-11-25"

type Server struct {
	Gateway *gateway.MemoryGateway
	// RemoteGateway, when non-empty, causes host/job/artifact/audit MCP tool
	// calls to be proxied to a running hosted gateway over HTTP rather than
	// operating on the local in-memory gateway.  This lets `rdev mcp serve`
	// see hosts and jobs that were registered through a foreground support-session
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

// NewServerWithRemoteGateway returns a Server that proxies host/job/artifact/audit
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

// retryingMCPTransport wraps http.DefaultTransport and retries GET/HEAD
// requests on transient connection-level errors (EOF, TLS truncation) that
// commonly occur behind Cloudflare Quick Tunnels and similar reverse proxies.
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
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
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
		resp, err := base.RoundTrip(req)
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
	return s.decodeRemoteResponse(resp)
}

// proxyPOSTTo sends a POST to baseURL+path and decodes the response.
func (s Server) proxyPOSTTo(baseURL, path string, payload any) (any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := s.remoteClient().Post(
		baseURL+path, "application/json", bytes.NewReader(data),
	)
	if err != nil {
		return nil, fmt.Errorf("remote gateway POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	return s.decodeRemoteResponse(resp)
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
	case "rdev.jobs.policy_template":
		data, err = s.jobPolicyTemplate(params.Arguments)
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

func (s Server) supportSessionHandoff(args map[string]any) (any, error) {
	ttl := intArg(args, "ttl_seconds", 7200)
	if ttl < 60 || ttl > 86400 {
		return nil, fmt.Errorf("ttl_seconds must be between 60 and 86400")
	}
	rdevCommand := agentRdevCommand(stringArg(args, "rdev_command", ""))
	return supportsession.BuildHandoff(supportsession.HandoffOptions{
		RepoRoot:    stringArg(args, "repo_root", "."),
		WorkDir:     stringArg(args, "work_dir", ""),
		Addr:        stringArg(args, "addr", "0.0.0.0:8787"),
		GatewayURL:  stringArg(args, "gateway_url", ""),
		Target:      stringArg(args, "target", "auto"),
		Reason:      stringArg(args, "reason", "visible temporary remote support"),
		TTLSeconds:  ttl,
		AutoApprove: boolArg(args, "auto_approve", true),
		Locale:      stringArg(args, "locale", "auto"),
		RdevCommand: rdevCommand,
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
	gatewayURL := strings.TrimRight(strings.TrimSpace(stringArg(args, "gateway_url", "")), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	if gatewayURL == "" {
		rdevCommand := agentRdevCommand(stringArg(args, "rdev_command", ""))
		handoff := supportsession.BuildHandoff(supportsession.HandoffOptions{
			RepoRoot:    stringArg(args, "repo_root", "."),
			WorkDir:     stringArg(args, "work_dir", ""),
			Addr:        stringArg(args, "addr", "0.0.0.0:8787"),
			Target:      stringArg(args, "target", "auto"),
			Reason:      stringArg(args, "reason", "visible temporary remote support"),
			TTLSeconds:  ttl,
			AutoApprove: boolArg(args, "auto_approve", true),
			Locale:      stringArg(args, "locale", "auto"),
			RdevCommand: rdevCommand,
		})
		return supportsession.BuildConnectFromHandoff(handoff), nil
	}
	created, err := s.createSupportSessionPayload(args, gatewayURL, ttl)
	if err != nil {
		return nil, err
	}
	return supportsession.BuildConnectFromCreated(created), nil
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
	autoApprove := boolArg(args, "auto_approve", false)
	if autoApprove && mode != model.HostModeAttendedTemporary {
		return nil, fmt.Errorf("auto_approve is only supported for attended-temporary Connection Entries")
	}
	metadata := map[string]string{}
	if autoApprove {
		metadata["auto_approve"] = "attended-temporary"
		metadata["connection_entry"] = "standard-visible"
		metadata["approval_contract"] = "target-consent-scoped-ticket"
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
		RequireHostApproval:   boolArg(args, "require_host_approval", !autoApprove),
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
	autoApprove := boolArg(args, "auto_approve", true)
	gatewayCandidates := supportsession.GatewayURLCandidatesFromIPs("0.0.0.0:8787", gatewayURL, nil)
	metadata := map[string]string{
		"connection_entry":  "standard-visible",
		"approval_contract": "target-consent-scoped-ticket",
	}
	if autoApprove {
		metadata["auto_approve"] = "attended-temporary"
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
		AutoApprove:           autoApprove,
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
		RepoRoot:    stringArg(args, "repo_root", "."),
		WorkDir:     stringArg(args, "work_dir", ""),
		GatewayURL:  stringArg(args, "gateway_url", ""),
		Addr:        stringArg(args, "addr", "0.0.0.0:8787"),
		Target:      stringArg(args, "target", "auto"),
		Reason:      stringArg(args, "reason", "visible temporary remote support"),
		TTLSeconds:  intArg(args, "ttl_seconds", 7200),
		AutoApprove: boolArg(args, "auto_approve", true),
		Locale:      stringArg(args, "locale", "auto"),
		RdevCommand: agentRdevCommand(stringArg(args, "rdev_command", "")),
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
			TicketCode: ticketCode,
			Hosts:      s.Gateway.HostsForTicketCode(ticketCode, ""),
			Locale:     stringArg(args, "locale", "auto"),
			GatewayURL: s.effectiveGatewayURL(args),
		})
		if !wait || status["connected"] == true || status["status"] == "pending-approval" {
			return status, nil
		}
		if timeoutSeconds > 0 && time.Now().After(deadline) {
			status["ok"] = false
			status["timed_out"] = true
			status["next_action"] = "Keep waiting, or check gateway reachability, network path, and target command output."
			statusText, _ := status["status"].(string)
			status["connection_recovery"] = supportsession.BuildConnectionRecovery(supportsession.ConnectionRecoveryOptions{
				Status:     statusText,
				TicketCode: ticketCode,
				Locale:     stringArg(args, "locale", "auto"),
				TimedOut:   true,
			})
			return status, nil
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
		if !wait || status["connected"] == true || status["status"] == "pending-approval" {
			return status, nil
		}
		if timeoutSeconds > 0 && time.Now().After(deadline) {
			status["ok"] = false
			status["timed_out"] = true
			status["next_action"] = "Keep waiting, or check gateway reachability, network path, and target command output."
			statusText, _ := status["status"].(string)
			status["connection_recovery"] = supportsession.BuildConnectionRecovery(supportsession.ConnectionRecoveryOptions{
				Status:     statusText,
				TicketCode: ticketCode,
				Locale:     stringArg(args, "locale", "auto"),
				TimedOut:   true,
			})
			return status, nil
		}
		time.Sleep(time.Duration(intervalMillis) * time.Millisecond)
	}
}

func (s Server) supportSessionReport(args map[string]any) (any, error) {
	gatewayURL := s.effectiveGatewayURL(args)
	hostID := strings.TrimSpace(stringArg(args, "host_id", ""))
	ticketCode := strings.TrimSpace(stringArg(args, "ticket_code", ""))
	var status map[string]any
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
		activeHosts := mapSliceFromAny(status["active_hosts"])
		if hostID == "" {
			if len(activeHosts) == 1 {
				hostID = stringMapValue(activeHosts[0], "id")
			} else {
				nextAction := "No active host is job-ready for this ticket. Wait with rdev.support_session.status or run rdev support-session recover if stale hosts are present."
				if len(activeHosts) > 1 {
					nextAction = "Multiple active hosts are registered for this ticket; choose the intended host explicitly with host_id before creating jobs."
				}
				return map[string]any{
					"schema_version":          "rdev.support-session-report.v1",
					"ok":                      false,
					"gateway_url":             gatewayURL,
					"connection_continuity":   supportSessionConnectionContinuityMap(gatewayURL),
					"disconnect_policy":       "do not disconnect automatically after task completion; keep the session alive until the operator explicitly requests disconnect/revoke/stop",
					"remote_control_entry":    supportSessionRemoteControlEntryMap(gatewayURL, ticketCode, status, nil),
					"managed_upgrade":         supportSessionManagedUpgradeRecommendationMap(nil),
					"ticket_code":             ticketCode,
					"host_id":                 "",
					"recommended_job_host_id": "",
					"status":                  status,
					"active_hosts":            status["active_hosts"],
					"stale_hosts":             status["stale_hosts"],
					"pending_hosts":           status["pending_hosts"],
					"host_count":              status["host_count"],
					"next_action":             nextAction,
					"stale_host_rule":         "Do not create jobs for stale_hosts; stale means the runner is not job-ready.",
				}, nil
			}
		}
	}
	if hostID == "" {
		return nil, fmt.Errorf("support_session.report requires host_id or ticket_code")
	}
	var host map[string]any
	var jobs []map[string]any
	if gatewayURL != "" {
		hostPayload, err := s.proxyGETTo(gatewayURL, "/v1/hosts/"+url.PathEscape(hostID))
		if err != nil {
			return nil, err
		}
		host = nestedMapOrSelfAny(hostPayload, "host")
		jobsPayload, err := s.proxyGETTo(gatewayURL, "/v1/jobs?host_id="+url.QueryEscape(hostID))
		if err != nil {
			return nil, err
		}
		jobs = mapSliceFromAny(nestedAny(jobsPayload, "jobs"))
	} else {
		hostModel, err := s.Gateway.Host(hostID)
		if err != nil {
			return nil, err
		}
		host = structToMap(hostModel)
		for _, job := range s.Gateway.Jobs() {
			if job.HostID == hostID {
				jobs = append(jobs, structToMap(job))
			}
		}
	}
	jobReports := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		jobID := stringMapValue(job, "id")
		artifactSummary := ""
		artifactCount := 0
		if jobID != "" {
			artifacts := map[string]any{}
			if gatewayURL != "" {
				if payload, err := s.proxyGETTo(gatewayURL, "/v1/jobs/"+url.PathEscape(jobID)+"/artifacts"); err == nil {
					if m, ok := payload.(map[string]any); ok {
						artifacts = m
					}
				}
			} else {
				values := s.Gateway.Artifacts(jobID)
				raw := make([]any, 0, len(values))
				for _, artifact := range values {
					raw = append(raw, structToMap(artifact))
				}
				artifacts["artifacts"] = raw
			}
			if values, _ := artifacts["artifacts"].([]any); values != nil {
				artifactCount = len(values)
			}
			artifactSummary = summarizeFirstArtifactMap(artifacts)
		}
		jobReports = append(jobReports, map[string]any{
			"job_id":           jobID,
			"status":           stringMapValue(job, "status"),
			"adapter":          stringMapValue(job, "adapter"),
			"intent":           stringMapValue(job, "intent"),
			"artifact_count":   artifactCount,
			"artifact_summary": artifactSummary,
		})
	}
	report := map[string]any{
		"schema_version":              "rdev.support-session-report.v1",
		"ok":                          true,
		"gateway_url":                 gatewayURL,
		"connection_continuity":       supportSessionConnectionContinuityMap(gatewayURL),
		"disconnect_policy":           "do not disconnect automatically after task completion; keep the session alive until the operator explicitly requests disconnect/revoke/stop",
		"remote_control_entry":        supportSessionRemoteControlEntryMap(gatewayURL, ticketCode, status, host),
		"managed_upgrade":             supportSessionManagedUpgradeRecommendationMap(host),
		"ticket_code":                 ticketCode,
		"host_id":                     hostID,
		"recommended_job_host_id":     hostID,
		"recommended_job_host_source": "explicit_host_id",
		"host":                        host,
		"jobs":                        jobReports,
		"human_report":                supportSessionHumanReportMap(host, jobReports),
		"next_action":                 "Use this report instead of hand-written curl summaries; create jobs only for recommended_job_host_id; keep the connection alive until the operator explicitly requests disconnect or revocation.",
		"stale_host_rule":             "Do not create jobs for stale_hosts; run rdev support-session recover if stale hosts or queued jobs accumulated.",
	}
	if status != nil {
		report["status"] = status
		report["active_hosts"] = status["active_hosts"]
		report["stale_hosts"] = status["stale_hosts"]
		report["pending_hosts"] = status["pending_hosts"]
		report["host_count"] = status["host_count"]
		report["recommended_job_host_source"] = "ticket_single_active_host"
	}
	return report, nil
}

func (s Server) supportSessionSmokeTest(args map[string]any) (any, error) {
	gatewayURL := s.effectiveGatewayURL(args)
	ticketCode := strings.TrimSpace(stringArg(args, "ticket_code", ""))
	hostID := strings.TrimSpace(stringArg(args, "host_id", ""))
	timeoutSeconds := intArg(args, "timeout_seconds", 120)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	if timeoutSeconds > 3600 {
		return nil, fmt.Errorf("timeout_seconds must be between 1 and 3600")
	}
	var status map[string]any
	if hostID == "" && ticketCode != "" {
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
		activeHosts := mapSliceFromAny(status["active_hosts"])
		if len(activeHosts) != 1 {
			return map[string]any{
				"schema_version":          "rdev.support-session-smoke-test.v1",
				"ok":                      false,
				"gateway_url":             gatewayURL,
				"connection_continuity":   supportSessionConnectionContinuityMap(gatewayURL),
				"disconnect_policy":       "do not disconnect automatically after task completion; keep the session alive until the operator explicitly requests disconnect/revoke/stop",
				"remote_control_entry":    supportSessionRemoteControlEntryMap(gatewayURL, ticketCode, status, nil),
				"managed_upgrade":         supportSessionManagedUpgradeRecommendationMap(nil),
				"ticket_code":             ticketCode,
				"host_id":                 "",
				"recommended_job_host_id": "",
				"status":                  status,
				"next_action":             "Smoke-test requires exactly one active host; wait with rdev.support_session.status or pass host_id explicitly.",
			}, nil
		}
		hostID = stringMapValue(activeHosts[0], "id")
	}
	if hostID == "" {
		return nil, fmt.Errorf("support_session.smoke_test requires host_id or ticket_code with exactly one active host")
	}
	audit, err := s.runSupportSessionCapabilityAudit(args, gatewayURL, hostID, time.Duration(timeoutSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	report := map[string]any{
		"schema_version":        "rdev.support-session-smoke-test.v1",
		"ok":                    audit["ok"],
		"gateway_url":           gatewayURL,
		"host_id":               hostID,
		"ticket_code":           ticketCode,
		"connection_continuity": supportSessionConnectionContinuityMap(gatewayURL),
		"disconnect_policy":     "do not disconnect automatically after task completion; keep the session alive until the operator explicitly requests disconnect/revoke/stop",
		"remote_control_entry":  supportSessionRemoteControlEntryMap(gatewayURL, ticketCode, status, nestedMapOrSelfAny(audit["host"], "")),
		"managed_upgrade":       supportSessionManagedUpgradeRecommendationMap(nestedMapOrSelfAny(audit["host"], "")),
		"capability_audit":      audit,
		"human_report":          supportSessionSmokeHumanReportMap(audit),
		"next_action":           "Use this single smoke-test report instead of hand-written job probes; keep the host connected for follow-up work unless the operator explicitly asks to disconnect.",
	}
	if status != nil {
		report["status"] = status
	}
	return report, nil
}

func (s Server) runSupportSessionCapabilityAudit(args map[string]any, gatewayURL, hostID string, timeout time.Duration) (map[string]any, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	deadline := time.Now().Add(timeout)
	host, err := s.fetchHostMap(gatewayURL, hostID)
	if err != nil {
		return nil, err
	}
	probes := mcpAuditCapabilityProbes(stringMapValue(host, "os"))
	results := make([]map[string]any, 0, len(probes))
	ok := true
	for _, probe := range probes {
		created, err := s.createJob(map[string]any{
			"gateway_url": gatewayURL,
			"host_id":     hostID,
			"adapter":     probe.Adapter,
			"intent":      probe.Intent,
			"policy":      probe.Policy,
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
		job := nestedMapOrSelfAny(created, "job")
		jobID := stringMapValue(job, "id")
		if jobID == "" {
			ok = false
			results = append(results, map[string]any{
				"name":       probe.Name,
				"capability": probe.Capability,
				"status":     "create_failed",
				"ok":         false,
				"error":      "gateway response missing job.id",
			})
			continue
		}
		jobStatus, err := s.waitForJobTerminal(gatewayURL, jobID, deadline)
		if err != nil {
			ok = false
			results = append(results, map[string]any{
				"name":       probe.Name,
				"capability": probe.Capability,
				"job_id":     jobID,
				"status":     "wait_failed",
				"ok":         false,
				"error":      err.Error(),
			})
			continue
		}
		status := stringMapValue(jobStatus, "status")
		resultOK := status == string(model.JobStatusSucceeded) || (probe.FailureIsOK && (status == string(model.JobStatusFailed) || status == string(model.JobStatusCanceled)))
		if !resultOK {
			ok = false
		}
		artifactSummary := ""
		artifactCount := 0
		if artifacts, err := s.listArtifacts(map[string]any{"gateway_url": gatewayURL, "job_id": jobID}); err == nil {
			artifactMap := nestedMapOrSelfAny(artifacts, "")
			if values, _ := artifactMap["artifacts"].([]any); values != nil {
				artifactCount = len(values)
			}
			artifactSummary = summarizeFirstArtifactMap(artifactMap)
		}
		results = append(results, map[string]any{
			"name":             probe.Name,
			"capability":       probe.Capability,
			"adapter":          probe.Adapter,
			"job_id":           jobID,
			"job_status":       status,
			"ok":               resultOK,
			"artifact_count":   artifactCount,
			"artifact_summary": artifactSummary,
		})
	}
	return map[string]any{
		"schema_version":        "rdev.support-session-capability-audit.v1",
		"ok":                    ok,
		"gateway_url":           gatewayURL,
		"connection_continuity": supportSessionConnectionContinuityMap(gatewayURL),
		"host":                  host,
		"results":               results,
		"next_action":           "Use scoped jobs only after this audit is ok; do not disconnect the host unless the operator asks.",
	}, nil
}

func (s Server) fetchHostMap(gatewayURL, hostID string) (map[string]any, error) {
	if strings.TrimSpace(gatewayURL) != "" {
		payload, err := s.proxyGETTo(gatewayURL, "/v1/hosts/"+url.PathEscape(hostID))
		if err != nil {
			return nil, err
		}
		return nestedMapOrSelfAny(payload, "host"), nil
	}
	host, err := s.Gateway.Host(hostID)
	if err != nil {
		return nil, err
	}
	return structToMap(host), nil
}

func (s Server) waitForJobTerminal(gatewayURL, jobID string, deadline time.Time) (map[string]any, error) {
	for {
		payload, err := s.jobStatus(map[string]any{"gateway_url": gatewayURL, "job_id": jobID})
		if err != nil {
			return nil, err
		}
		job := nestedMapOrSelfAny(payload, "job")
		status := stringMapValue(job, "status")
		if isTerminalJobStatus(status) {
			return job, nil
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return job, fmt.Errorf("job %s did not reach a terminal state before timeout; last status=%s", jobID, status)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case string(model.JobStatusSucceeded), "completed", string(model.JobStatusFailed), string(model.JobStatusCanceled):
		return true
	default:
		return false
	}
}

type mcpSupportSessionAuditProbe struct {
	Name        string
	Capability  string
	Adapter     string
	Intent      string
	Policy      map[string]any
	FailureIsOK bool
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
		"workspace_root":       ".",
		"capabilities":         []string{"powershell.user"},
		"command":              command,
		"allow_commands":       []string{"powershell.exe", "powershell", "pwsh"},
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	}
}

func mcpShellAuditPolicyWithWriteScope(capabilities, argv, allowCommands, writeScope []string) map[string]any {
	policy := map[string]any{
		"workspace_root":       ".",
		"capabilities":         capabilities,
		"argv":                 argv,
		"allow_commands":       allowCommands,
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	}
	if len(writeScope) > 0 {
		policy["write_scope"] = writeScope
	}
	return policy
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

func (s Server) listHosts(args map[string]any) (any, error) {
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		status := stringArg(args, "status", "")
		path := "/v1/hosts"
		if status != "" {
			path += "?status=" + url.QueryEscape(status)
		}
		return s.proxyGETTo(gwURL, path)
	}
	return map[string]any{"hosts": s.Gateway.Hosts(stringArg(args, "status", ""))}, nil
}

func (s Server) hostCapabilities(args map[string]any) (any, error) {
	hostID := requiredString(args, "host_id")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyGETTo(gwURL, "/v1/hosts/"+url.PathEscape(hostID))
	}
	host, err := s.Gateway.Host(hostID)
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
	hostID := requiredString(args, "host_id")
	caps := stringSliceArg(args, "capabilities")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/hosts/"+url.PathEscape(hostID)+"/approve", map[string]any{
			"capabilities": caps,
		})
	}
	return s.Gateway.ApproveHost(hostID, caps)
}

func (s Server) revokeHost(args map[string]any) (any, error) {
	hostID := requiredString(args, "host_id")
	reason := stringArg(args, "reason", "")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/hosts/"+url.PathEscape(hostID)+"/revoke", map[string]any{
			"reason": reason,
		})
	}
	return s.Gateway.RevokeHost(hostID, reason)
}

func (s Server) createJob(args map[string]any) (any, error) {
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/jobs", map[string]any{
			"host_id": requiredString(args, "host_id"),
			"adapter": requiredString(args, "adapter"),
			"intent":  requiredString(args, "intent"),
			"policy":  objectArg(args, "policy"),
		})
	}
	return s.Gateway.CreateJob(
		requiredString(args, "host_id"),
		requiredString(args, "adapter"),
		requiredString(args, "intent"),
		objectArg(args, "policy"),
	)
}

func (s Server) jobPolicyTemplate(args map[string]any) (any, error) {
	return jobtemplate.PolicyTemplate(
		stringArg(args, "capability", "shell.user"),
		stringArg(args, "target_os", "auto"),
	), nil
}

func (s Server) jobStatus(args map[string]any) (any, error) {
	jobID := requiredString(args, "job_id")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyGETTo(gwURL, "/v1/jobs/"+url.PathEscape(jobID))
	}
	return s.Gateway.Job(jobID)
}

func (s Server) cancelJob(args map[string]any) (any, error) {
	jobID := requiredString(args, "job_id")
	reason := stringArg(args, "reason", "")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/jobs/"+url.PathEscape(jobID)+"/cancel", map[string]any{
			"reason": reason,
		})
	}
	return s.Gateway.CancelJob(jobID, reason)
}

func (s Server) approveJob(args map[string]any) (any, error) {
	jobID := requiredString(args, "job_id")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyPOSTTo(gwURL, "/v1/jobs/"+url.PathEscape(jobID)+"/approve", map[string]any{
			"approval_id": requiredString(args, "approval_id"),
			"decision":    requiredString(args, "decision"),
			"reason":      stringArg(args, "reason", ""),
		})
	}
	return s.Gateway.ApproveJob(
		jobID,
		requiredString(args, "approval_id"),
		requiredString(args, "decision"),
		stringArg(args, "reason", ""),
	)
}

func (s Server) listArtifacts(args map[string]any) (any, error) {
	jobID := requiredString(args, "job_id")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyGETTo(gwURL, "/v1/jobs/"+url.PathEscape(jobID)+"/artifacts")
	}
	return map[string]any{"artifacts": s.Gateway.Artifacts(jobID)}, nil
}

func (s Server) readArtifact(args map[string]any) (any, error) {
	artifactID := requiredString(args, "artifact_id")
	if gwURL := s.effectiveGatewayURL(args); gwURL != "" {
		return s.proxyGETTo(gwURL, "/v1/artifacts/"+url.PathEscape(artifactID))
	}
	return s.Gateway.Artifact(artifactID)
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
	return policy.ExplainShellJob(
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
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		certificateJSON = artifact.Content
	}
	if certificateJSON == "" {
		return nil, fmt.Errorf("certificate_json or artifact_id is required")
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
	if revocationsJSON := stringArg(args, "revocations_json", ""); revocationsJSON != "" || stringArg(args, "revocations_artifact_id", "") != "" {
		if artifactID := stringArg(args, "revocations_artifact_id", ""); artifactID != "" {
			artifact, err := s.Gateway.Artifact(artifactID)
			if err != nil {
				return nil, err
			}
			revocationsJSON = artifact.Content
		}
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
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		artifactJSON = artifact.Content
	}
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json or artifact_id is required")
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
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		artifactJSON = artifact.Content
	}
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json or artifact_id is required")
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
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		artifactJSON = artifact.Content
	}
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json or artifact_id is required")
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
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		artifactJSON = artifact.Content
	}
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json or artifact_id is required")
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
	case []model.Job:
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

func supportSessionHumanReportMap(host map[string]any, jobs []map[string]any) string {
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
	_, _ = fmt.Fprintf(&b, "\n- Jobs reviewed: %d\n", len(jobs))
	for _, job := range jobs {
		_, _ = fmt.Fprintf(&b, "- %s: %s", job["job_id"], job["status"])
		if intent, _ := job["intent"].(string); intent != "" {
			_, _ = fmt.Fprintf(&b, " - %s", intent)
		}
		if summary, _ := job["artifact_summary"].(string); summary != "" {
			_, _ = fmt.Fprintf(&b, " | evidence: %s", oneLine(summary))
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
		_, _ = fmt.Fprintf(&b, "- %s: ok=%v status=%s\n", stringMapValue(result, "name"), result["ok"], firstReportFieldMap(result, "job_status", "status"))
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
		"schema_version":                      "rdev.support-session-managed-upgrade.v1",
		"for_owned_recurring_hosts":           true,
		"for_third_party_temporary":           false,
		"requires_explicit_operator_approval": true,
		"requires_stable_gateway":             true,
		"stable_gateway_env": []string{
			"RDEV_HOSTED_GATEWAY_URL",
			"RDEV_CLOUDFLARED_NAMED_TUNNEL_URL",
		},
		"target_os":        targetOS,
		"recommended_tool": "rdev.connection_entry.plan",
		"agent_rule":       "If this is the operator's own recurring machine, ask one short ownership/persistence approval question, configure a stable gateway, then generate a reviewed managed-service Connection Entry plan. Do not install persistence for third-party temporary support.",
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
