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
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
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
		RdevCommand: stringArg(args, "rdev_command", "rdev"),
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
		handoff := supportsession.BuildHandoff(supportsession.HandoffOptions{
			RepoRoot:    stringArg(args, "repo_root", "."),
			WorkDir:     stringArg(args, "work_dir", ""),
			Addr:        stringArg(args, "addr", "0.0.0.0:8787"),
			Target:      stringArg(args, "target", "auto"),
			Reason:      stringArg(args, "reason", "visible temporary remote support"),
			TTLSeconds:  ttl,
			AutoApprove: boolArg(args, "auto_approve", true),
			Locale:      stringArg(args, "locale", "auto"),
			RdevCommand: stringArg(args, "rdev_command", "rdev"),
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
	metadata := map[string]string{
		"connection_entry":  "standard-visible",
		"approval_contract": "target-consent-scoped-ticket",
	}
	if autoApprove {
		metadata["auto_approve"] = "attended-temporary"
	}
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
		GatewayURLCandidates:  supportsession.GatewayURLCandidatesFromIPs("0.0.0.0:8787", gatewayURL, nil),
		JoinURL:               joinURL,
		ManifestURL:           manifestURL,
		ManifestRootPublicKey: manifestRootPublicKey(s.Gateway.ManifestRoot()),
		Ticket:                ticket,
		Target:                stringArg(args, "target", "auto"),
		Locale:                stringArg(args, "locale", "auto"),
		RdevCommand:           stringArg(args, "rdev_command", "rdev"),
		AutoApprove:           autoApprove,
	}), nil
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
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for {
		status := supportsession.BuildStatus(supportsession.StatusOptions{
			TicketCode: ticketCode,
			Hosts:      s.Gateway.HostsForTicketCode(ticketCode, ""),
			Locale:     stringArg(args, "locale", "auto"),
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
