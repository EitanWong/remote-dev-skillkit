package supportsession

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/cdnopt"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
)

const PlanSchemaVersion = "rdev.support-session-plan.v1"
const PrepareSchemaVersion = "rdev.support-session-prepare.v1"
const HandoffSchemaVersion = "rdev.support-session-handoff.v1"
const ConnectSchemaVersion = "rdev.support-session-connect.v1"
const CreatedSchemaVersion = "rdev.support-session-created.v1"
const StartedSchemaVersion = "rdev.support-session-started.v1"
const StatusSchemaVersion = "rdev.support-session-status.v1"
const StatusFileSchemaVersion = "rdev.support-session-status-file.v1"
const BootstrapConnectorSchemaVersion = "rdev.support-session-bootstrap-connector.v1"
const HandoffTextFileSchemaVersion = "rdev.support-session-handoff-text-file.v1"
const ConnectedReportFileSchemaVersion = "rdev.support-session-connected-report-file.v1"
const ConnectionAttemptPolicySchemaVersion = "rdev.connection-attempt-policy.v1"
const UserHandoffSchemaVersion = "rdev.support-session-user-handoff.v1"
const ConnectionRecoverySchemaVersion = "rdev.support-session-connection-recovery.v1"
const ConnectedNextStepsSchemaVersion = "rdev.support-session-connected-next-steps.v1"
const ContinuityPolicySchemaVersion = "rdev.support-session-continuity-policy.v1"
const ConnectionSupervisionSchemaVersion = "rdev.support-session-connection-supervision.v1"
const supportSessionEndpointFreshAfter = 90 * time.Second
const GatewayCandidatePreflightSchemaVersion = "rdev.support-session-gateway-candidate-preflight.v1"
const ConnectivityHelperPreflightSchemaVersion = "rdev.support-session-connectivity-helper-preflight.v1"
const ConnectionEntryRunnerRecommendationSchemaVersion = "rdev.support-session-connection-entry-runner-recommendation.v1"
const AgentConnectionRunbookSchemaVersion = "rdev.support-session-agent-runbook.v1"
const FreshAgentFailurePreventionSchemaVersion = "rdev.support-session-fresh-agent-failure-prevention.v1"
const FreshAgentConnectContractSchemaVersion = "rdev.support-session-fresh-agent-connect-contract.v1"
const TargetHandoffEnvelopeSchemaVersion = "rdev.support-session-target-handoff-envelope.v1"
const RemoteControlEntrySchemaVersion = "rdev.support-session-remote-control-entry.v1"

const (
	targetHTTPConnectTimeoutSeconds = 2
	targetHTTPMaxTimeSeconds        = 10
	targetHTTPRetries               = 1
	targetHTTPRetryDelaySeconds     = 1

	supportSessionHelperGzipBudgetBytes = int64(8 * 1024 * 1024)
	supportSessionBootstrapTargetBytes  = int64(1 * 1024 * 1024)
)

type Options struct {
	RepoRoot     string
	WorkDir      string
	GatewayURL   string
	Addr         string
	Target       string
	Reason       string
	TTLSeconds   int
	AutoActivate bool
	Locale       string
	RdevCommand  string
}

type PrepareOptions struct {
	RepoRoot    string
	WorkDir     string
	Addr        string
	GatewayURL  string
	Target      string
	BuildAssets bool
	RdevCommand string
}

type HandoffOptions struct {
	RepoRoot                   string
	WorkDir                    string
	Addr                       string
	GatewayURL                 string
	Target                     string
	Reason                     string
	TTLSeconds                 int
	AutoActivate               bool
	Capabilities               []string
	Locale                     string
	RdevCommand                string
	Region                     string
	ProviderPolicyPath         string
	AllowDegradedDirectHandoff bool
	RequireForeground          bool
}

type RemoteControlEntryOptions struct {
	GatewayURL       string
	TicketCode       string
	Ticket           *model.Ticket
	Hosts            []model.Host
	Locale           string
	SessionID        string
	TargetEndpointID string
}

type GatewayURLCandidate struct {
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	Source      string `json:"source"`
	Recommended bool   `json:"recommended"`
	Reason      string `json:"reason"`
}

type GatewayEnvCandidate struct {
	EnvVar string
	URL    string
	Kind   string
	Scope  string
	Reason string
}

type connectivityHelperDefinition struct {
	ID                    string
	Kind                  string
	GatewayEnv            string
	StartArgvEnv          string
	InstallActionEnv      string
	AllowedTools          []string
	AuthorizationRequired []string
}

func BuildHandoff(opts HandoffOptions) map[string]any {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	gatewayURL, gatewayCandidates := ResolveGatewayURL(addr, opts.GatewayURL)
	target := normalizeTarget(opts.Target)
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	reason := strings.TrimSpace(opts.Reason)
	if reason == "" {
		reason = "visible temporary remote support"
	}
	ttl := opts.TTLSeconds
	if ttl == 0 {
		ttl = 7200
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	repoRoot := strings.TrimSpace(opts.RepoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	workDir := strings.TrimSpace(opts.WorkDir)
	selectedPath := "start-foreground-gateway"
	agentNextStep := "run cli_start_now_command in a visible terminal, then prefer handoff_text_file.path when present; otherwise send the returned target_handoff_envelope.full_text"
	mcpNextTool := ""
	resolvedCreateGatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if resolvedCreateGatewayURL == "" {
		resolvedCreateGatewayURL, _ = ConfiguredGatewayURLCandidate()
	}
	if resolvedCreateGatewayURL != "" && !opts.RequireForeground {
		gatewayURL = resolvedCreateGatewayURL
		selectedPath = "create-with-reachable-gateway"
		agentNextStep = "call rdev.support_session.create with mcp_next_arguments, then send the returned target_handoff_envelope.full_text"
		mcpNextTool = "rdev.support_session.create"
	}
	// Only explicit or configured stable gateways are safe to thread into
	// generated foreground-start commands. Opportunistic LAN/loopback candidates
	// are diagnostic previews; passing them as --gateway-url would turn them into
	// explicit operator-provided gateways and can suppress managed tunnel
	// selection in connect --start.
	startGatewayURL := resolvedCreateGatewayURL
	createArgs := map[string]any{
		"gateway_url":   resolvedCreateGatewayURL,
		"target":        target,
		"reason":        reason,
		"ttl_seconds":   ttl,
		"auto_activate": opts.AutoActivate,
		"locale":        locale,
		"rdev_command":  rdevCommand,
	}
	if len(opts.Capabilities) > 0 {
		createArgs["capabilities"] = append([]string(nil), opts.Capabilities...)
	}
	startCommand := []string{
		rdevCommand, "support-session", "start",
		"--addr", addr,
		"--target", target,
		"--reason", reason,
		"--ttl-seconds", strconv.Itoa(ttl),
		"--locale", locale,
		"--rdev-command", rdevCommand,
	}
	if workDir != "" {
		startCommand = append(startCommand, "--work-dir", workDir)
	}
	if startGatewayURL != "" {
		startCommand = append(startCommand, "--gateway-url", startGatewayURL)
	}
	if opts.AutoActivate {
		startCommand = append(startCommand, "--auto-activate")
	} else {
		startCommand = append(startCommand, "--auto-activate=false")
	}
	if len(opts.Capabilities) > 0 {
		startCommand = append(startCommand, "--capabilities", strings.Join(opts.Capabilities, ","))
	}
	startCommand = appendTunnelPolicyFlags(startCommand, opts.Region, opts.ProviderPolicyPath, opts.AllowDegradedDirectHandoff)
	connectStartCommand := []string{
		rdevCommand, "support-session", "connect",
		"--start",
		"--addr", addr,
		"--target", target,
		"--reason", reason,
		"--ttl-seconds", strconv.Itoa(ttl),
		"--locale", locale,
		"--rdev-command", rdevCommand,
	}
	if workDir != "" {
		connectStartCommand = append(connectStartCommand, "--work-dir", workDir)
	}
	if startGatewayURL != "" {
		connectStartCommand = append(connectStartCommand, "--gateway-url", startGatewayURL)
	}
	if opts.AutoActivate {
		connectStartCommand = append(connectStartCommand, "--auto-activate")
	} else {
		connectStartCommand = append(connectStartCommand, "--auto-activate=false")
	}
	if len(opts.Capabilities) > 0 {
		connectStartCommand = append(connectStartCommand, "--capabilities", strings.Join(opts.Capabilities, ","))
	}
	connectStartCommand = appendTunnelPolicyFlags(connectStartCommand, opts.Region, opts.ProviderPolicyPath, opts.AllowDegradedDirectHandoff)
	return map[string]any{
		"schema_version":           HandoffSchemaVersion,
		"ok":                       true,
		"intent":                   "single-agent-decision-for-one-command-visible-support-session",
		"selected_path":            selectedPath,
		"agent_next_step":          agentNextStep,
		"mcp_next_tool":            mcpNextTool,
		"mcp_next_arguments":       createArgs,
		"cli_start_now_command":    connectStartCommand,
		"foreground_start_command": startCommand,
		"prepare_command":          supportSessionPrepareCommand(rdevCommand, repoRoot, addr, startGatewayURL, target),
		"status_watch_rule":        "after sending the returned user_handoff to the human, call rdev.support_session.status with wait=true; when connected=true, proactively report the connection is established",
		"recovery_rule":            "if create/start/status fails or times out, read connection_recovery or rerun rdev.support_session.prepare; do not write custom recovery scripts",
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:        selectedPath,
			Status:       "not-created",
			Target:       target,
			Locale:       locale,
			GatewayURL:   gatewayURL,
			Candidates:   gatewayCandidates,
			AutoActivate: opts.AutoActivate,
			RdevCommand:  rdevCommand,
			NeedStartNow: selectedPath != "create-with-reachable-gateway",
		}),
		"gateway_url":            gatewayURL,
		"gateway_url_candidates": gatewayCandidates,
		"target":                 target,
		"locale":                 locale,
		"auto_activate": map[string]any{
			"enabled": opts.AutoActivate,
			"scope":   "attended-temporary first host only for this standard visible session",
		},
		"human_surface_rule": "humans receive only target_handoff_envelope.full_text from the next tool output; user_handoff remains a compatibility fallback",
		"agent_rule":         "use this handoff result as the first routing decision; do not choose support-session plan or write shell/PowerShell/bootstrap/relay glue unless this contract explicitly asks for a standard rdev command",
		"forbidden": []string{
			"manual ticket/root/gateway/transport assembly for target humans",
			"Agent-authored PowerShell or shell bootstrap/recovery scripts",
			"ExecutionPolicy Bypass",
			"hidden install",
			"UAC or sudo bypass",
			"service, firewall, DNS, route, credential, paid relay, or cloud changes without explicit authorization",
		},
	}
}

func appendTunnelPolicyFlags(command []string, region, policyPath string, allowDegraded bool) []string {
	region = strings.TrimSpace(region)
	if region != "" {
		command = append(command, "--region", region)
	}
	policyPath = strings.TrimSpace(policyPath)
	if policyPath != "" {
		command = append(command, "--provider-policy", policyPath)
	}
	if allowDegraded {
		command = append(command, "--allow-degraded-direct-handoff")
	}
	return command
}

func BuildConnectFromHandoff(handoff map[string]any) map[string]any {
	selectedPath, _ := handoff["selected_path"].(string)
	payload := map[string]any{
		"schema_version": ConnectSchemaVersion,
		"ok":             true,
		"intent":         "single-call-agent-entry-for-one-command-visible-support-session",
		"selected_path":  selectedPath,
		"handoff":        handoff,
		"agent_rule":     "Agents should call rdev.support_session.connect first when a human asks to connect a computer; follow this payload instead of choosing handoff/create/start/status manually.",
		"forbidden": []string{
			"manual ticket/root/gateway/transport assembly for target humans",
			"Agent-authored PowerShell or shell bootstrap/recovery scripts",
			"ExecutionPolicy Bypass",
			"hidden install",
		},
	}
	payload["fresh_agent_connect_contract"] = freshAgentConnectContract(freshAgentConnectContractOptions{
		Phase:        selectedPath,
		RdevCommand:  rdevCommandFromHandoff(handoff),
		AutoActivate: autoActivateFromHandoff(handoff),
	})
	if selectedPath == "create-with-reachable-gateway" {
		payload["ready_to_send_to_human"] = false
		payload["mcp_next_tool"] = handoff["mcp_next_tool"]
		payload["mcp_next_arguments"] = handoff["mcp_next_arguments"]
		payload["agent_connection_runbook"] = handoff["agent_connection_runbook"]
		payload["agent_next_step"] = "call mcp_next_tool with mcp_next_arguments; then send only the returned target_handoff_envelope.full_text and wait for connected=true"
		return payload
	}
	payload["ready_to_send_to_human"] = false
	payload["cli_start_now_command"] = handoff["cli_start_now_command"]
	payload["foreground_start_command"] = handoff["foreground_start_command"]
	payload["prepare_command"] = handoff["prepare_command"]
	payload["agent_connection_runbook"] = handoff["agent_connection_runbook"]
	payload["agent_next_step"] = "run cli_start_now_command in a visible terminal; it starts the gateway, builds verified helper assets, prints the target command, writes ready_file.path and status_file.path, and waits for the target; then send only the started payload's target_handoff_envelope.full_text and wait for connected=true"
	payload["human_surface_rule"] = "do not send this connect payload to the target human; run the returned cli_start_now_command first and then send the started payload's top-level target_handoff_envelope.full_text"
	return payload
}

func supportSessionPrepareCommand(rdevCommand, repoRoot, addr, gatewayURL, target string) []string {
	command := []string{
		rdevCommand, "support-session", "prepare",
		"--build-assets",
		"--repo-root", repoRoot,
		"--addr", addr,
	}
	if gatewayURL := strings.TrimRight(strings.TrimSpace(gatewayURL), "/"); gatewayURL != "" {
		command = append(command, "--gateway-url", gatewayURL)
	}
	return append(command, "--target", target)
}

func supportSessionConnectStartCommand(rdevCommand, addr, gatewayURL, target string) []string {
	command := []string{rdevCommand, "support-session", "connect", "--start", "--addr", addr}
	if gatewayURL := strings.TrimRight(strings.TrimSpace(gatewayURL), "/"); gatewayURL != "" {
		command = append(command, "--gateway-url", gatewayURL)
	}
	return append(command, "--target", target)
}

func supportSessionStartCommand(rdevCommand, addr, gatewayURL, target string) []string {
	command := []string{rdevCommand, "support-session", "start", "--addr", addr}
	if gatewayURL := strings.TrimRight(strings.TrimSpace(gatewayURL), "/"); gatewayURL != "" {
		command = append(command, "--gateway-url", gatewayURL)
	}
	return append(command, "--target", target)
}

func BuildConnectFromCreated(created map[string]any) map[string]any {
	readiness := availabilityReadinessFromMap(created)
	userHandoff := userHandoffWithReadiness(created["user_handoff"], readiness)
	envelope := targetHandoffEnvelopeWithReadiness(created["target_handoff_envelope"], readiness)
	agentNextStep := "send target_handoff_envelope.full_text to the target human, wait with rdev.support_session.status, then proactively report connected_next_steps.user_report when connected=true"
	humanSurfaceRule := "humans receive only target_handoff_envelope.full_text; user_handoff remains a compatibility fallback"
	if !readiness.ReadyToSend {
		agentNextStep = blockedHandoffInstruction(readiness.DegradedReason)
		humanSurfaceRule = blockedHandoffInstruction(readiness.DegradedReason)
	}
	return map[string]any{
		"schema_version":          ConnectSchemaVersion,
		"ok":                      true,
		"intent":                  "single-call-agent-entry-for-one-command-visible-support-session",
		"selected_path":           "created-with-reachable-gateway",
		"availability_readiness":  readiness,
		"ready_to_send":           readiness.ReadyToSend,
		"ready_to_activate":       readiness.ReadyToActivate,
		"ready_to_execute":        readiness.ReadyToExecute,
		"ready_to_send_to_human":  readiness.ReadyToSend,
		"created_session":         created,
		"user_handoff":            userHandoff,
		"target_handoff_envelope": envelope,
		"target_command":          created["target_command"],
		"join_url":                created["join_url"],
		"watch_connection_status": created["watch_connection_status"],
		"watch_connection_status_configured_gateway": created["watch_connection_status_configured_gateway"],
		"connection_supervision":                     created["connection_supervision"],
		"gateway_candidate_preflight":                created["gateway_candidate_preflight"],
		"connectivity_helper_preflight":              created["connectivity_helper_preflight"],
		"connection_entry_runner_recommendation":     created["connection_entry_runner_recommendation"],
		"agent_connection_runbook":                   created["agent_connection_runbook"],
		"rdev_bootstrap_connector":                   created["rdev_bootstrap_connector"],
		"fresh_agent_connect_contract": freshAgentConnectContract(freshAgentConnectContractOptions{
			Phase:                 "created-with-reachable-gateway",
			AvailabilityReadiness: readiness,
			TicketCode:            stringFromMap(created, "ticket_code"),
			RdevCommand:           rdevCommandFromRunbook(created["agent_connection_runbook"]),
			AutoActivate:          boolFromMap(created, "auto_activate"),
		}),
		"mcp_follow_up":      created["mcp_follow_up"],
		"agent_next_step":    agentNextStep,
		"human_surface_rule": humanSurfaceRule,
		"agent_rule":         "Agents should call rdev.support_session.connect first when a human asks to connect a computer; do not manually choose handoff/create/start/status when this payload is available.",
		"forbidden": []string{
			"manual ticket/root/gateway/transport assembly for target humans",
			"Agent-authored PowerShell or shell bootstrap/recovery scripts",
			"ExecutionPolicy Bypass",
			"hidden install",
		},
	}
}

type StatusOptions struct {
	TicketCode  string
	Hosts       []model.Host
	Session     *controlplane.Session
	Locale      string
	GatewayURL  string
	Preconnects []model.SupportSessionPreconnect
	// Ticket, when non-nil, adds ticket expiry information to the status
	// response so agents and users can see how much time remains.
	Ticket *model.Ticket
}

type CreatedOptions struct {
	GatewayURL               string
	GatewayURLCandidates     []GatewayURLCandidate
	JoinURL                  string
	ManifestURL              string
	ManifestRootPublicKey    string
	Ticket                   model.Ticket
	Target                   string
	Locale                   string
	RdevCommand              string
	AutoActivate             bool
	TargetBootstrapReadiness any
	AvailabilityReadiness    AvailabilityReadiness
}

type StartedOptions struct {
	Addr                      string
	GatewayURL                string
	WorkDir                   string
	ReadyFile                 string
	StatusFile                string
	HandoffTextFile           string
	ConnectedReportFile       string
	Created                   map[string]any
	AssetReport               any
	ConnectionReadiness       any
	ConnectivityStrategy      any
	GatewayCandidatePreflight any
	StandardRecoveryActions   []string
	AvailabilityReadiness     AvailabilityReadiness
}

func BuildStarted(opts StartedOptions) map[string]any {
	addr := strings.TrimSpace(opts.Addr)
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	workDir := strings.TrimSpace(opts.WorkDir)
	readyFile := strings.TrimSpace(opts.ReadyFile)
	statusFile := strings.TrimSpace(opts.StatusFile)
	handoffTextFile := strings.TrimSpace(opts.HandoffTextFile)
	connectedReportFile := strings.TrimSpace(opts.ConnectedReportFile)
	session := opts.Created
	assetsReady := assetReportAllReady(opts.AssetReport)
	readiness := normalizeAvailabilityReadiness(opts.AvailabilityReadiness)
	readiness.ReadyToSend = readiness.ReadyToSend && assetsReady
	blockedReason := readiness.DegradedReason
	if !assetsReady {
		blockedReason = "helper assets are not ready; do not send target_handoff_envelope.full_text until rdev support-session connect --start or prepare --build-assets reports asset_report.all_ready=true"
	}
	userHandoff := userHandoffWithReadiness(session["user_handoff"], readinessWithReason(readiness, blockedReason))
	envelope := targetHandoffEnvelopeWithReadiness(session["target_handoff_envelope"], readinessWithReason(readiness, blockedReason))
	agentFlow := []string{
		"keep this process running while the target host connects",
		"give the target-side human only target_handoff_envelope.full_text",
		"watch connection status with status_file.path, foreground_feedback, watch_connection_status, or rdev.support_session.status",
		"when connected=true, proactively report that the connection is established",
		"if connection_readiness.ready is false, follow standard_recovery_actions instead of writing ad hoc bootstrap or relay code",
	}
	humanSurfaceRule := "humans receive only target_handoff_envelope.full_text; user_handoff remains a compatibility fallback"
	if !readiness.ReadyToSend {
		agentFlow[1] = blockedHandoffInstruction(blockedReason)
		humanSurfaceRule = blockedHandoffInstruction(blockedReason)
	}
	payload := map[string]any{
		"schema_version": StartedSchemaVersion,
		"ok":             true,
		"intent":         "foreground-local-gateway-plus-one-command-support-session",
		"gateway": map[string]any{
			"addr":        addr,
			"gateway_url": gatewayURL,
			"work_dir":    workDir,
			"lifecycle":   "foreground-visible-process",
			"stop":        "interrupt this rdev support-session start process",
		},
		"availability_readiness":                     readiness,
		"ready_to_send":                              readiness.ReadyToSend,
		"ready_to_activate":                          readiness.ReadyToActivate,
		"ready_to_execute":                           readiness.ReadyToExecute,
		"ready_to_send_to_human":                     readiness.ReadyToSend,
		"user_handoff":                               userHandoff,
		"target_handoff_envelope":                    envelope,
		"target_command":                             session["target_command"],
		"join_url":                                   session["join_url"],
		"watch_connection_status":                    session["watch_connection_status"],
		"watch_connection_status_configured_gateway": session["watch_connection_status_configured_gateway"],
		"connection_supervision":                     session["connection_supervision"],
		"foreground_feedback": map[string]any{
			"schema_version":                  "rdev.support-session-foreground-feedback.v1",
			"stream":                          "stderr",
			"event_prefix":                    "rdev support session event: ",
			"log_event_schema_version":        "rdev.support-session-foreground-log-event.v1",
			"protected_status_schema_version": "rdev.support-session-foreground-event.v1",
			"log_fields":                      []string{"event", "status_class", "connected", "action_class"},
			"events":                          []string{"waiting", "pending-activation", "connected"},
			"connected_rule":                  "when event=connected, immediately tell the user the connection has been established before submitting session tasks",
			"security_rule":                   "stderr contains only the minimal shareable event; read status_file.path for protected ticket, gateway, host, and next-step details",
			"agent_rule":                      "parse foreground feedback events from stderr when this command is kept open; read status_file.path or use the status watcher as fallback sources of truth",
		},
		"mcp_follow_up":         session["mcp_follow_up"],
		"session":               session,
		"asset_report":          opts.AssetReport,
		"connection_readiness":  opts.ConnectionReadiness,
		"connectivity_strategy": opts.ConnectivityStrategy,
		"gateway_candidate_preflight": firstNonNil(
			opts.GatewayCandidatePreflight,
			session["gateway_candidate_preflight"],
		),
		"connectivity_helper_preflight":          session["connectivity_helper_preflight"],
		"connection_entry_runner_recommendation": session["connection_entry_runner_recommendation"],
		"rdev_bootstrap_connector":               session["rdev_bootstrap_connector"],
		"fresh_agent_connect_contract": freshAgentConnectContract(freshAgentConnectContractOptions{
			Phase:                 "foreground-started",
			AvailabilityReadiness: readiness,
			TicketCode:            stringFromMap(session, "ticket_code"),
			RdevCommand:           rdevCommandFromRunbook(session["agent_connection_runbook"]),
			AutoActivate:          boolFromMap(session, "auto_activate"),
			ReadyFile:             readyFile,
			StatusFile:            statusFile,
			HandoffTextFile:       handoffTextFile,
			ConnectedReportFile:   connectedReportFile,
		}),
		"agent_connection_runbook": firstNonNil(
			session["agent_connection_runbook"],
			agentConnectionRunbook(agentConnectionRunbookOptions{
				Phase:       "foreground-started",
				Status:      "waiting",
				GatewayURL:  gatewayURL,
				RdevCommand: "rdev",
			}),
		),
		"agent_flow":                agentFlow,
		"standard_recovery_actions": standardRecoveryActions(opts.StandardRecoveryActions),
		"human_surface_rule":        humanSurfaceRule,
		"forbidden": []string{
			"background hidden gateway",
			"ExecutionPolicy Bypass",
			"manual ticket/root/gateway/transport assembly for target user",
			"ad hoc bootstrap script generated by the Agent",
		},
	}
	if !readiness.ReadyToSend {
		payload["handoff_blocked_reason"] = blockedReason
	}
	if readyFile != "" {
		payload["ready_file"] = map[string]any{
			"schema_version": "rdev.support-session-ready-file.v1",
			"path":           readyFile,
			"contains":       StartedSchemaVersion,
			"agent_rule":     handoffFileInstruction(readiness.ReadyToSend, blockedReason, "read this file after starting the foreground gateway when terminal stdout is hard to parse; send target_handoff_envelope.full_text to the human"),
		}
	}
	if statusFile != "" {
		payload["status_file"] = map[string]any{
			"schema_version":        StatusFileSchemaVersion,
			"path":                  statusFile,
			"contains":              "rdev.support-session-foreground-event.v1",
			"status_schema_version": StatusSchemaVersion,
			"agent_rule":            "read this file after starting the foreground gateway when terminal output is unavailable; when event=connected or status.connected=true, immediately report connected_next_steps.user_report",
		}
	}
	if handoffTextFile != "" {
		payload["handoff_text_file"] = map[string]any{
			"schema_version": HandoffTextFileSchemaVersion,
			"path":           handoffTextFile,
			"contains":       "target_handoff_envelope.full_text",
			"agent_rule":     handoffFileInstruction(readiness.ReadyToSend, blockedReason, "read and forward this plain-text file verbatim to the target-side human; do not rewrite commands or extract ticket/root/gateway fields"),
		}
	}
	if connectedReportFile != "" {
		payload["connected_report_file"] = map[string]any{
			"schema_version": ConnectedReportFileSchemaVersion,
			"path":           connectedReportFile,
			"contains":       "connected_next_steps.user_report",
			"agent_rule":     "when this file exists or status_file.path reports connected=true, send this plain text to the user before submitting session tasks",
		}
	}
	return payload
}

func assetReportAllReady(report any) bool {
	if report == nil {
		return false
	}
	if typed, ok := report.(map[string]any); ok {
		ready, ok := typed["all_ready"].(bool)
		return ok && ready
	}
	return false
}

func Prepare(ctx context.Context, opts PrepareOptions) (map[string]any, error) {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	gatewayURL, gatewayCandidates := ResolveGatewayURL(addr, opts.GatewayURL)
	repoRootInput := strings.TrimSpace(opts.RepoRoot)
	if repoRootInput == "" {
		repoRootInput = "."
	}
	repoRoot, err := filepath.Abs(repoRootInput)
	if err != nil {
		repoRoot = repoRootInput
	}
	if found := findRepoRoot(repoRoot); found != "" {
		repoRoot = found
	} else if wd, err := os.Getwd(); err == nil {
		if found := findRepoRoot(wd); found != "" {
			repoRoot = found
		}
	}
	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		workDir = filepath.Join(repoRoot, "work", "rdev-support-session")
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	binDir := filepath.Join(workDir, "bin")
	goPath, _ := exec.LookPath("go")
	gitPath, _ := exec.LookPath("git")
	rdevPath, _ := exec.LookPath("rdev")
	currentExecutable, _ := os.Executable()
	repoValid := pathExists(filepath.Join(repoRoot, "go.mod")) && pathExists(filepath.Join(repoRoot, "cmd", "rdev-host", "main.go"))
	assets := supportSessionAssetSpecs(binDir)
	missingInputs := []string{}
	if strings.TrimSpace(goPath) == "" {
		missingInputs = append(missingInputs, "go binary is required to build missing helper assets from source")
	}
	if !repoValid {
		missingInputs = append(missingInputs, "valid remote-dev-skillkit checkout with go.mod and cmd/rdev-host/main.go")
	}
	assetReports := make([]map[string]any, 0, len(assets))
	allAssetsReady := true
	for _, asset := range assets {
		assetReady := false
		report := map[string]any{
			"id":                            asset.ID,
			"goos":                          asset.GOOS,
			"goarch":                        asset.GOARCH,
			"path":                          asset.Path,
			"asset_url":                     supportSessionAssetURL(gatewayURL, asset.Name, ""),
			"gzip_asset_url":                supportSessionAssetURL(gatewayURL, asset.Name, ".gz"),
			"sha256_url":                    supportSessionAssetURL(gatewayURL, asset.Name, ".sha256"),
			"build_status":                  "not-requested",
			"asset_role":                    "full-helper",
			"used_by_default_first_connect": true,
			"native_bootstrap_asset":        false,
		}
		if fileExists(asset.Path) {
			sum, err := fileSHA256Hex(asset.Path)
			if err != nil {
				report["present"] = false
				report["error"] = err.Error()
			} else {
				report["present"] = true
				report["sha256"] = sum
				applySupportSessionAssetDownloadEvidence(report, asset.Path)
				if opts.BuildAssets && repoValid && strings.TrimSpace(goPath) != "" {
					report["previous_sha256"] = sum
					report["build_status"] = "rebuild-requested"
				} else {
					assetReady = true
				}
			}
			if !assetReady && report["build_status"] != "rebuild-requested" {
				allAssetsReady = false
				assetReports = append(assetReports, report)
				continue
			}
			if assetReady {
				assetReports = append(assetReports, report)
				continue
			}
		}
		if !fileExists(asset.Path) {
			report["present"] = false
		}
		if opts.BuildAssets && repoValid && strings.TrimSpace(goPath) != "" {
			if err := os.MkdirAll(filepath.Dir(asset.Path), 0o700); err != nil {
				report["build_status"] = "failed"
				report["error"] = err.Error()
				// do not continue; fall through to prebuilt fallback below
			} else {
				cmd := exec.CommandContext(ctx, goPath, supportSessionRdevBuildArgs(asset.Path)...)
				cmd.Dir = repoRoot
				cmd.Env = append(os.Environ(), "GOOS="+asset.GOOS, "GOARCH="+asset.GOARCH, "CGO_ENABLED=0")
				output, err := cmd.CombinedOutput()
				if err != nil {
					report["build_status"] = "failed"
					report["error"] = err.Error()
					if len(output) > 0 {
						report["build_output_tail"] = tailString(string(output), 800)
					}
					// do not continue; fall through to prebuilt fallback below
				} else {
					sum, err := fileSHA256Hex(asset.Path)
					if err != nil {
						report["build_status"] = "failed"
						report["error"] = err.Error()
						// fall through to prebuilt fallback
					} else {
						report["present"] = true
						report["build_status"] = "built"
						report["sha256"] = sum
						applySupportSessionAssetDownloadEvidence(report, asset.Path)
						assetReady = true
					}
				}
			}
		}
		// Fallback: if Go build wasn't run or failed, check for a pre-built
		// binary shipped in the repository's work/rdev-support-session/bin/.
		// This lets `rdev support-session prepare` succeed even without a Go
		// toolchain installed on the operator machine.
		if !assetReady {
			prebuiltPath := filepath.Join(repoRoot, "work", "rdev-support-session", "bin", asset.Name)
			if fileExists(prebuiltPath) {
				if err := copyFile(prebuiltPath, asset.Path); err == nil {
					sum, sumErr := fileSHA256Hex(asset.Path)
					if sumErr == nil {
						report["present"] = true
						report["build_status"] = "copied-from-prebuilt"
						report["sha256"] = sum
						applySupportSessionAssetDownloadEvidence(report, asset.Path)
						assetReady = true
					}
				}
			}
		}
		if !assetReady {
			allAssetsReady = false
		}
		assetReports = append(assetReports, report)
	}
	assetDownloadSummary := supportSessionAssetDownloadSummary(assetReports)
	localRdevUsable := strings.TrimSpace(rdevPath) != ""
	if !localRdevUsable && strings.TrimSpace(currentExecutable) != "" && strings.Contains(filepath.Base(currentExecutable), "rdev") {
		localRdevUsable = true
		rdevPath = currentExecutable
	}
	target := normalizeTarget(opts.Target)
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	helperPreflight := connectivityHelperPreflight()
	connectionReadiness := map[string]any{
		"ready":                         localRdevUsable || allAssetsReady,
		"local_rdev_usable":             localRdevUsable,
		"target_bootstrap_self_repair":  allAssetsReady,
		"gateway_url":                   gatewayURL,
		"gateway_url_candidates":        gatewayCandidates,
		"gateway_candidate_preflight":   gatewayCandidatePreflight(gatewayURL, target, gatewayCandidates),
		"connectivity_helper_preflight": helperPreflight,
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:       "prepare",
			Status:      "not-created",
			Target:      target,
			GatewayURL:  gatewayURL,
			Candidates:  gatewayCandidates,
			RdevCommand: rdevCommand,
		}),
		"target":                        target,
		"human_gets_one_command":        true,
		"auto_activation_contract":      "attended-temporary first host only when created by support-session start/create",
		"requires_human_decision_first": []string{"company or owner authorization when the target is not clearly operator-owned"},
	}
	recoveryActions := []string{
		"prefer rdev support-session connect --start when no gateway is running; it creates the ticket, prints one target command, prepares helper assets, and watches through support-session status",
		"if local_rdev_usable is false, run go install ./cmd/rdev from a valid checkout or use go run ./cmd/rdev bootstrap agent-plan --repo-root . as a temporary planner",
		"if target_bootstrap_self_repair is false, rerun rdev support-session prepare --build-assets from a valid checkout before giving commands to targets that may not have rdev installed",
		"do not write custom PowerShell, relay, activation polling, ticket substitution, or bootstrap glue",
	}
	return map[string]any{
		"schema_version":         PrepareSchemaVersion,
		"ok":                     true,
		"repo_root":              repoRoot,
		"repo_root_valid":        repoValid,
		"work_dir":               workDir,
		"bin_dir":                binDir,
		"addr":                   addr,
		"gateway_url":            gatewayURL,
		"gateway_url_candidates": gatewayCandidates,
		"detected": map[string]any{
			"os":                 runtime.GOOS,
			"arch":               runtime.GOARCH,
			"go_path":            goPath,
			"git_path":           gitPath,
			"rdev_path":          rdevPath,
			"current_executable": currentExecutable,
			"host_capabilities":  hostcap.Detect(ctx),
		},
		"asset_report": map[string]any{
			"schema_version":                                "rdev.support-session-assets.v1",
			"build_assets":                                  opts.BuildAssets,
			"all_ready":                                     allAssetsReady,
			"download_budget_bytes":                         supportSessionHelperGzipBudgetBytes,
			"all_gzip_within_budget":                        assetDownloadSummary["all_gzip_within_budget"],
			"bootstrap_target_bytes":                        supportSessionBootstrapTargetBytes,
			"bootstrap_connector_recommended":               assetDownloadSummary["bootstrap_connector_recommended"],
			"first_connect_size_strategy":                   assetDownloadSummary["first_connect_size_strategy"],
			"default_first_connect_surface":                 assetDownloadSummary["default_first_connect_surface"],
			"default_runner_download_kind":                  assetDownloadSummary["default_runner_download_kind"],
			"first_task_requires_full_helper":               assetDownloadSummary["first_task_requires_full_helper"],
			"publishes_native_first_connect_asset":          assetDownloadSummary["publishes_native_first_connect_asset"],
			"default_full_helper_gzip_estimated_max_bytes":  assetDownloadSummary["default_full_helper_gzip_estimated_max_bytes"],
			"default_full_helper_meets_bootstrap_target":    assetDownloadSummary["default_full_helper_meets_bootstrap_target"],
			"native_first_connect_asset":                    assetDownloadSummary["native_first_connect_asset"],
			"default_first_connect_agent_interpretation":    assetDownloadSummary["default_first_connect_agent_interpretation"],
			"native_first_connect_asset_publication_policy": assetDownloadSummary["native_first_connect_asset_publication_policy"],
			"assets": assetReports,
		},
		"connectivity_strategy":         connectivityStrategy(gatewayURL, target, gatewayCandidates),
		"gateway_candidate_preflight":   gatewayCandidatePreflight(gatewayURL, target, gatewayCandidates),
		"connectivity_helper_preflight": helperPreflight,
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:       "prepare",
			Status:      "not-created",
			Target:      target,
			GatewayURL:  gatewayURL,
			Candidates:  gatewayCandidates,
			RdevCommand: rdevCommand,
		}),
		"connection_readiness":     connectionReadiness,
		"missing_inputs":           missingInputs,
		"standard_recovery":        recoveryActions,
		"target_handoff_policy":    "give the target-side human only target_handoff_envelope.full_text when present; use generated target_command or join_url only as compatibility fallback fields",
		"forbidden":                []string{"ExecutionPolicy Bypass", "hidden install", "manual ticket/root/gateway/transport assembly", "ad hoc bootstrap code"},
		"recommended_next_step":    recommendedSupportSessionNextStep(localRdevUsable, allAssetsReady),
		"command_to_connect_start": supportSessionConnectStartCommand(rdevCommand, addr, opts.GatewayURL, target),
		"command_to_start":         supportSessionStartCommand(rdevCommand, addr, opts.GatewayURL, target),
		"command_to_prepare_all":   supportSessionPrepareCommand(rdevCommand, repoRoot, addr, opts.GatewayURL, target),
	}, nil
}

func supportSessionRdevBuildArgs(outputPath string) []string {
	return []string{"build", "-trimpath", "-ldflags=-s -w", "-o", outputPath, "./cmd/rdev-host"}
}

func connectivityStrategy(gatewayURL, target string, gatewayCandidates []GatewayURLCandidate) map[string]any {
	return map[string]any{
		"schema_version":         "rdev.support-session-connectivity-strategy.v1",
		"intent":                 "adaptive-connectivity-with-automatic-native-fallback-and-configured-helper-escalation",
		"gateway_url":            gatewayURL,
		"gateway_url_candidates": gatewayCandidates,
		"target":                 target,
		"selection_order": []string{
			"same-machine-local-mcp",
			"native-direct-gateway",
			"native-lan-gateway",
			"proxy-aware-https",
			"wss-then-long-poll-then-poll",
			"existing-ssh-tunnel",
			"existing-frp-or-chisel-relay",
			"existing-headscale-tailscale-mesh",
			"existing-wireguard-vpn",
			"operator-provided-hosted-gateway",
		},
		"automatic_downgrade": []string{
			"if WSS is blocked, rdev host serve --transport auto falls back to HTTPS long-poll",
			"if long-poll is blocked or unstable, rdev host serve --transport auto falls back to short polling",
			"after registration, rdev host serve --transport auto can switch to another signed join-manifest gateway candidate if the current gateway fails before processing session tasks",
			"if a direct gateway health check fails, try LAN/private gateway candidates and configured proxy variables before helper paths",
			"if a configured helper path fails, report manual_action_required instead of guessing credentials or mutating network policy",
		},
		"automatic_upgrade": []string{
			"prefer LAN/private gateway when probes show both sides are routed locally",
			"prefer WSS/mTLS-capable gateway when available for lower latency",
			"for owned recurring machines, use a reviewed managed Connection Entry only after explicit persistence authorization",
			"after reconnect evidence is available, update runtime memory with the best known stable path for this host",
		},
		"read_only_probes": []string{
			"detect OS and architecture",
			"detect rdev availability",
			"detect proxy environment variable names without logging secret values",
			"probe gateway /healthz and signed manifest reachability",
			"detect ssh, frpc, chisel, tailscale, headscale, and wg/wireguard tools",
			"inspect route/LAN hints only within the operator-authorized scope",
		},
		"auto_use_when_configured": []string{
			"HTTP(S) proxy environment variables",
			"existing SSH tunnel start argv in RDEV_SSH_TUNNEL_START_ARGV_JSON",
			"existing relay start argv in RDEV_RELAY_START_ARGV_JSON",
			"existing mesh gateway URL in RDEV_MESH_GATEWAY_URL",
			"existing VPN gateway URL in RDEV_VPN_GATEWAY_URL",
		},
		"authorization_required_for": []string{
			"installing tunnel, mesh, VPN, driver, service, or firewall components",
			"creating or editing SSH keys/config",
			"opening ports or changing router/NAT/firewall/DNS/routes",
			"using paid hosted relay or cloud resources",
			"turning a temporary third-party session into managed persistence",
		},
		"agent_rule": "use Connection Entry runner metadata for relay/mesh/VPN/SSH execution; never ask the target-side human to assemble low-level flags",
	}
}

func gatewayCandidatePreflight(gatewayURL, target string, gatewayCandidates []GatewayURLCandidate) map[string]any {
	candidates := make([]map[string]any, 0, len(gatewayCandidates))
	for i, candidate := range gatewayCandidates {
		candidates = append(candidates, gatewayCandidatePreflightReport(i+1, candidate))
	}
	return map[string]any{
		"schema_version":  GatewayCandidatePreflightSchemaVersion,
		"intent":          "agent-readable-gateway-candidate-decision-table",
		"gateway_url":     strings.TrimRight(strings.TrimSpace(gatewayURL), "/"),
		"target":          normalizeTarget(target),
		"preflight_mode":  "local-classification-no-network-scan",
		"candidate_count": len(candidates),
		"candidates":      candidates,
		"standard_sequence": []string{
			"call rdev.support_session.connect or run rdev support-session connect first",
			"send only returned target_handoff_envelope.full_text",
			"wait with returned connection_supervision or foreground_feedback",
			"if all candidates fail, rerun rdev.support_session.prepare or rdev support-session prepare --build-assets before asking the human",
		},
		"ask_human_only_for": []string{
			"authorization or company policy",
			"persistent managed-host activation",
			"privileged firewall, DNS, route, service, driver, or credential changes",
			"real hosted/relay/mesh/VPN/SSH endpoint credentials when none are configured",
		},
		"agent_rule":         "use this candidate table before asking humans or writing probes; the target command owns ordered URL fallback and status/recovery owns waiting",
		"human_surface_rule": "do not expose this table to target users; humans receive only target_handoff_envelope.full_text",
		"forbidden": []string{
			"Agent-authored PowerShell or shell network probes",
			"custom relay, mesh, VPN, SSH, or polling scripts",
			"manual ticket/root/gateway/transport/checksum assembly",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
		},
	}
}

func gatewayCandidatePreflightReport(order int, candidate GatewayURLCandidate) map[string]any {
	kind := strings.TrimSpace(candidate.Kind)
	scope := strings.TrimSpace(candidate.Scope)
	sameMachineOnly := kind == "loopback" || scope == "same-machine-only" || isLoopbackHost(candidate.Host)
	stableFallback := isStableGatewayCandidateKind(kind)
	status := "candidate-unverified"
	nextAction := "use the returned target command and status watcher; if it times out, follow connection_recovery"
	switch {
	case sameMachineOnly:
		status = "same-machine-only"
		nextAction = "use only for same-machine demos; for a remote target, prefer a LAN/private or configured hosted/relay/mesh/VPN/SSH candidate"
	case kind == "lan-private":
		status = "lan-candidate-unverified"
		nextAction = "try this as the first opportunistic LAN path; if status wait times out, configure a stable RDEV_*_GATEWAY_URL and create a fresh Connection Entry"
	case stableFallback:
		status = "configured-stable-fallback-unverified"
		nextAction = "use this configured fallback in the generated target command, then wait with connection_supervision"
	case kind == "explicit":
		status = "operator-provided-unverified"
		nextAction = "use when the operator or Agent runtime supplied this gateway; verify by standard status feedback, not custom scripts"
	}
	report := map[string]any{
		"order":                           order,
		"url":                             strings.TrimRight(strings.TrimSpace(candidate.URL), "/"),
		"kind":                            kind,
		"scope":                           scope,
		"source":                          candidate.Source,
		"recommended":                     candidate.Recommended,
		"reason":                          candidate.Reason,
		"host":                            candidate.Host,
		"port":                            candidate.Port,
		"health_url":                      strings.TrimRight(strings.TrimSpace(candidate.URL), "/") + "/healthz",
		"status":                          status,
		"probeable_from_agent":            !sameMachineOnly,
		"same_machine_only":               sameMachineOnly,
		"stable_fallback":                 stableFallback,
		"requires_operator_configuration": false,
		"standard_next_action":            nextAction,
	}
	if stableFallback {
		report["durability"] = "configured-fallback-for-lan-changes"
	} else if sameMachineOnly {
		report["durability"] = "same-machine-only"
	} else {
		report["durability"] = "opportunistic"
	}
	return report
}

func isStableGatewayCandidateKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "hosted", "relay", "mesh", "vpn", "ssh", "cloudflared", "cloudflared-named":
		return true
	default:
		return false
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

type agentConnectionRunbookOptions struct {
	Phase        string
	Status       string
	TicketCode   string
	Target       string
	Locale       string
	GatewayURL   string
	Candidates   []GatewayURLCandidate
	AutoActivate bool
	RdevCommand  string
	NeedStartNow bool
	TimedOut     bool
}

func agentConnectionRunbook(opts agentConnectionRunbookOptions) map[string]any {
	phase := strings.TrimSpace(opts.Phase)
	if phase == "" {
		phase = "support-session"
	}
	status := strings.TrimSpace(opts.Status)
	if status == "" {
		status = "waiting"
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	ticketCode := strings.TrimSpace(opts.TicketCode)
	sequence := []string{
		"start with rdev.support_session.connect or rdev support-session connect",
		"if ready_to_send_to_human=false, run cli_start_now_command exactly in a visible foreground terminal",
		"send only target_handoff_envelope.full_text to the target-side human",
		"keep the target-side command/page and the local foreground gateway open while waiting",
		"watch with foreground_feedback, connection_supervision.mcp_watch_call, or rdev.support_session.status wait=true",
		"when connected=true, immediately report connected_next_steps.user_report before submitting session tasks",
		"inspect capabilities, submit the smallest scoped session task, then review evidence before declaring work complete",
	}
	if opts.NeedStartNow {
		sequence = append([]string{"run cli_start_now_command before sending anything to the target-side human"}, sequence...)
	}
	watch := map[string]any{
		"foreground_feedback_event": "connected",
		"mcp_tool":                  "rdev.support_session.status",
		"mcp_arguments": map[string]any{
			"ticket_code": ticketCode,
			"locale":      opts.Locale,
			"wait":        true,
		},
		"cli_command": []string{rdevCommand, "support-session", "status", "--ticket-code", ticketCode, "--wait", "--locale", opts.Locale},
		"rule":        "use returned watcher fields when present; do not write a polling loop",
	}
	if gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/"); gatewayURL != "" {
		watch["mcp_arguments"].(map[string]any)["gateway_url"] = gatewayURL
		watch["cli_command"] = []string{rdevCommand, "support-session", "status", "--gateway-url", gatewayURL, "--ticket-code", ticketCode, "--wait", "--locale", opts.Locale}
	}
	if ticketCode == "" {
		watch["mcp_arguments"] = map[string]any{"wait": true}
		watch["cli_command"] = []string{rdevCommand, "support-session", "status", "--ticket-code", "<ticket-code>", "--wait"}
	}
	return map[string]any{
		"schema_version": AgentConnectionRunbookSchemaVersion,
		"intent":         "fresh-agent-one-command-connect-operate-recover-runbook",
		"phase":          phase,
		"status":         status,
		"timed_out":      opts.TimedOut,
		"ticket_code":    ticketCode,
		"target":         normalizeTarget(opts.Target),
		"locale":         opts.Locale,
		"gateway_url":    strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/"),
		"standard_entry_tool": map[string]any{
			"mcp_tool":    "rdev.support_session.connect",
			"cli_command": []string{rdevCommand, "support-session", "connect"},
			"rule":        "for a human request like connect this computer, call this first; it chooses create-via-reachable-gateway or foreground start and returns the only human handoff to send",
		},
		"fallback_entry_tool": map[string]any{
			"mcp_tool":    "rdev.support_session.prepare",
			"cli_command": []string{rdevCommand, "support-session", "prepare", "--build-assets"},
			"rule":        "use only to repair missing local helper assets or inspect gateway candidates before creating a fresh support-session entry",
		},
		"fresh_agent_failure_prevention": freshAgentFailurePrevention(),
		"auto_activate": map[string]any{
			"enabled": opts.AutoActivate,
			"scope":   "standard attended-temporary first host only",
		},
		"gateway_candidate_summary": gatewayCandidateRunbookSummary(opts.Candidates),
		"sequence":                  sequence,
		"watch":                     watch,
		"on_connected": []string{
			"tell the user the connection has been established",
			"read rdev.sessions.status for the active Control Plane session",
			"ask for task intent only if it is still unclear",
			"create the smallest scoped rdev.sessions.task allowed by capabilities and policy",
		},
		"on_timeout_or_failure": []string{
			"read connection_recovery and gateway_candidate_preflight from the returned payload",
			"check gateway_candidate_summary.needs_public_tunnel: if true, run the standard foreground `rdev support-session connect --start` flow; rdev owns cloudflared/localhost.run selection and tunnel lifetime",
			"if tunnel helpers are missing or blocked, report the standard manual_action_required output instead of writing cloudflared, SSH, or relay scripts",
			"rerun rdev.support_session.prepare or rdev support-session prepare --build-assets",
			"create a fresh Connection Entry with configured hosted/relay/mesh/VPN/SSH/cloudflared fallback when LAN-only paths are insufficient",
			"ask one short question only for authorization, persistence authorization, privileged network changes, paid/cloud resources, credentials, or unclear ownership",
		},
		"human_surface_rule": "target-side humans receive only target_handoff_envelope.full_text; user_handoff remains a compatibility fallback",
		"agent_rule":         "follow this runbook before choosing lower-level support-session tools; never make humans assemble low-level connection values",
		"low_level_entry_rule": map[string]any{
			"do_not_start_with": []string{
				"rdev.invites.create",
				"rdev invite create",
				"rdev.connection_entry.plan",
				"rdev connection-entry plan",
			},
			"reason": "low-level invite and package materialization surfaces are for reviewed packaging or advanced workflows; fresh Agents should use support_session.connect so helper assets, auto-activation, foreground feedback, and status watching are generated together",
			"allowed_when": []string{
				"an operator explicitly asks for package materialization",
				"managed owned-host service planning has been explicitly authorized",
				"support_session.connect or support_session.prepare returns a standard recovery instruction that names Connection Entry planning",
			},
		},
		"forbidden": []string{
			"manual ticket/root/gateway/transport/checksum assembly",
			"Agent-authored PowerShell or shell bootstrap, relay, authorization, or polling scripts",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
			"UAC or sudo bypass",
			"firewall, DNS, route, service, driver, credential, paid relay, or cloud changes without explicit authorization",
		},
	}
}

type freshAgentConnectContractOptions struct {
	Phase                 string
	TicketCode            string
	GatewayURL            string
	RdevCommand           string
	AutoActivate          bool
	ReadyFile             string
	StatusFile            string
	HandoffTextFile       string
	ConnectedReportFile   string
	AvailabilityReadiness AvailabilityReadiness
}

func freshAgentConnectContract(opts freshAgentConnectContractOptions) map[string]any {
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	ticketCode := strings.TrimSpace(opts.TicketCode)
	readiness := normalizeAvailabilityReadiness(opts.AvailabilityReadiness)
	humanSurface := "send handoff_text_file.path verbatim when present; otherwise send only target_handoff_envelope.full_text verbatim; use user_handoff.message plus user_handoff.copy_paste only for older payloads"
	statusRule := "after sending handoff_text_file.path or target_handoff_envelope.full_text to the human, wait with returned connection_supervision, foreground_feedback, status_file.path, connected_report_file.path, or rdev.support_session.status wait=true; when connected=true, report connected_report_file.path or connected_next_steps.user_report immediately"
	if !readiness.ReadyToSend {
		humanSurface = blockedHandoffInstruction(readiness.DegradedReason)
		statusRule = blockedHandoffInstruction(readiness.DegradedReason) + "; wait for a new payload whose ready_to_send=true before beginning status supervision"
	}
	contract := map[string]any{
		"schema_version":         FreshAgentConnectContractSchemaVersion,
		"intent":                 "model-independent-standard-path-for-a-fresh-agent-to-connect-one-target-machine",
		"phase":                  strings.TrimSpace(opts.Phase),
		"availability_readiness": readiness,
		"ready_to_send":          readiness.ReadyToSend,
		"ready_to_activate":      readiness.ReadyToActivate,
		"ready_to_execute":       readiness.ReadyToExecute,
		"ready_to_send_human":    readiness.ReadyToSend,
		"human_surface":          humanSurface,
		"first_tool":             "rdev.support_session.connect",
		"first_cli":              []string{rdevCommand, "support-session", "connect"},
		"recovery_if_rdev_missing": []string{
			"do not stop at rdev not found",
			"if inside a remote-dev-skillkit checkout, run go install ./cmd/rdev or go run ./cmd/rdev bootstrap agent-plan --repo-root .",
			"if no checkout is present, clone the public repository, build rdev from ./cmd/rdev, then rerun rdev support-session connect",
		},
		"ask_human_only_for": []string{
			"company or device-owner authorization when not already confirmed",
			"one of: target OS family only when a remote runner package must be materialized and target_os is auto",
			"authorization for privileged persistence, firewall/DNS/route/service/driver changes, credentials, or paid/cloud resources",
		},
		"do_not_ask_human_for": []string{
			"ticket code",
			"manifest root public key",
			"gateway URL",
			"transport",
			"release checksum",
			"relay, mesh, VPN, or SSH flags",
			"activation polling loops",
		},
		"agent_must_not_generate": []string{
			"PowerShell bootstrap code",
			"shell bootstrap code",
			"ticket/root/gateway substitution scripts",
			"relay, mesh, VPN, SSH, or polling scripts",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
		},
		"status_rule": statusRule,
		"auto_activate": map[string]any{
			"enabled": opts.AutoActivate,
			"scope":   "first attended-temporary host for the standard visible support-session ticket only",
		},
	}
	if ticketCode != "" {
		contract["ticket_code"] = ticketCode
		statusCLI := []string{rdevCommand, "support-session", "status", "--ticket-code", ticketCode, "--wait"}
		statusArgs := map[string]any{
			"ticket_code": ticketCode,
			"wait":        true,
		}
		if gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/"); gatewayURL != "" {
			statusCLI = []string{rdevCommand, "support-session", "status", "--gateway-url", gatewayURL, "--ticket-code", ticketCode, "--wait"}
			statusArgs["gateway_url"] = gatewayURL
		}
		contract["status_cli"] = statusCLI
		contract["status_mcp"] = map[string]any{
			"tool":      "rdev.support_session.status",
			"arguments": statusArgs,
		}
	}
	if strings.TrimSpace(opts.ReadyFile) != "" {
		contract["ready_file_path"] = opts.ReadyFile
	}
	if strings.TrimSpace(opts.StatusFile) != "" {
		contract["status_file_path"] = opts.StatusFile
	}
	if strings.TrimSpace(opts.HandoffTextFile) != "" {
		contract["handoff_text_file_path"] = opts.HandoffTextFile
	}
	if strings.TrimSpace(opts.ConnectedReportFile) != "" {
		contract["connected_report_file_path"] = opts.ConnectedReportFile
	}
	return contract
}

func rdevCommandFromHandoff(handoff map[string]any) string {
	if values, ok := handoff["cli_start_now_command"].([]string); ok && len(values) > 0 {
		return values[0]
	}
	if values, ok := handoff["foreground_start_command"].([]string); ok && len(values) > 0 {
		return values[0]
	}
	return "rdev"
}

func autoActivateFromHandoff(handoff map[string]any) bool {
	value, ok := handoff["auto_activate"].(map[string]any)
	if !ok {
		return false
	}
	return boolFromMap(value, "enabled")
}

func rdevCommandFromRunbook(value any) string {
	runbook, ok := value.(map[string]any)
	if !ok {
		return "rdev"
	}
	standardEntry, ok := runbook["standard_entry_tool"].(map[string]any)
	if !ok {
		return "rdev"
	}
	if values, ok := standardEntry["cli_command"].([]string); ok && len(values) > 0 {
		return values[0]
	}
	return "rdev"
}

func freshAgentFailurePrevention() map[string]any {
	return map[string]any{
		"schema_version": FreshAgentFailurePreventionSchemaVersion,
		"purpose":        "keep fresh Agents on the standardized connect/start/watch/recover path instead of recreating fragile gateway, invite, bootstrap, and authorization glue",
		"known_failure_pattern": []string{
			"manual rdev gateway serve plus rdev invite create can omit verified helper assets and produce target bootstraps that fail with rdev is required",
			"background or ad hoc gateway process management can disappear before the target joins and leaves the Agent without a ready/status file",
			"hand-written Windows PowerShell or shell bootstrap code causes model-dependent behavior and can weaken the security contract",
			"asking humans to choose ticket, root key, gateway URL, transport, release root, checksum, or platform command creates avoidable multi-turn setup failures",
			"running cloudflared tunnel as a separate background terminal then passing its URL via --gateway-url creates an untracked tunnel process that disappears silently; use rdev support-session connect --start which manages cloudflared internally with automatic HTTP/2 protocol selection and provider fallback",
			"using powershell -NoProfile -Command \"$var=...\" style commands with variables causes expansion failures when pasted into an existing PowerShell session; the generated irm 'url' | iex form avoids this entirely",
			"sending a multi-step sequence (prepare, then create cloudflared, then connect separately) instead of a single rdev support-session connect --start command adds unnecessary round-trips and failure surfaces",
		},
		"required_standard_path": []string{
			"start ordinary connect-this-computer requests with rdev.support_session.connect or rdev support-session connect",
			"when ready_to_send_to_human=false, run cli_start_now_command exactly in a visible foreground terminal",
			"send handoff_text_file.path when present; otherwise send only target_handoff_envelope.full_text to the human",
			"watch foreground_feedback, status_file.path, connected_report_file.path, connection_supervision, or rdev.support_session.status wait=true",
			"report connected_report_file.path when present; otherwise report connected_next_steps.user_report immediately when connected=true",
			"when cloudflared QUIC fails, rdev support-session connect --start automatically retries with --protocol http2 and then falls back to localhost.run SSH tunnel; do not manually restart cloudflared",
		},
		"standard_recovery": []string{
			"if rdev is missing, recover from the checkout with go install ./cmd/rdev or go run ./cmd/rdev bootstrap agent-plan --repo-root .",
			"if helper assets are missing, run rdev support-session connect --start or rdev support-session prepare --build-assets from a valid checkout",
			"if a LAN-only path times out or will not survive network changes, configure a standard hosted/relay/mesh/VPN/SSH gateway candidate and create a fresh Connection Entry",
			"ask one short question only for authorization, persistence authorization, privileged network changes, paid/cloud resources, credentials, or unclear ownership",
			"if the target reports a PowerShell variable error on the bootstrap command, they are running from an existing PowerShell session; tell them to use: irm 'URL' -UseBasicParsing | iex (the join page always shows this simpler form)",
		},
		"forbidden_agent_generated_workarounds": []string{
			"manual ticket/root/gateway/transport/checksum substitution for humans",
			"PowerShell or shell bootstrap/download scripts written by the Agent",
			"nohup/background gateway lifecycle glue written by the Agent",
			"manually starting cloudflared tunnel in a background terminal and passing --gateway-url separately",
			"using removed --public-tunnel flags or manually disabling managed tunnel selection",
			"custom relay, mesh, SSH, VPN, or polling scripts outside standard rdev tools",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
			"manual activation polling loops",
		},
		"agent_rule": "treat this as a regression guard: if tempted to write setup code, stop and use the standard rdev support-session connect/start/prepare/status contracts instead",
	}
}

// GatewayCandidateSummary returns a summary map for the given candidates,
// suitable for the agent connection runbook and for callers that need to
// decide whether to use the managed foreground public-tunnel path.
func GatewayCandidateSummary(candidates []GatewayURLCandidate) map[string]any {
	return gatewayCandidateRunbookSummary(candidates)
}

func gatewayCandidateRunbookSummary(candidates []GatewayURLCandidate) map[string]any {
	kinds := []string{}
	hasStable := false
	hasLAN := false
	hasSameMachineOnly := len(candidates) == 0
	cloudflaredInPATH := cloudflaredAvailable()
	for _, candidate := range candidates {
		kind := strings.TrimSpace(candidate.Kind)
		if kind == "" {
			kind = "unknown"
		}
		kinds = append(kinds, kind)
		if kind == "lan-private" {
			hasLAN = true
			hasSameMachineOnly = false
		}
		if isStableGatewayCandidateKind(kind) {
			hasStable = true
			hasSameMachineOnly = false
		}
		if kind != "loopback" && candidate.Scope != "same-machine-only" {
			hasSameMachineOnly = false
		}
	}
	// needsPublicTunnel is true when the only reachable candidates are LAN or
	// loopback — both of which fail for a target on a different network.
	needsPublicTunnel := !hasStable && (hasLAN || hasSameMachineOnly)
	publicTunnelHint := ""
	if needsPublicTunnel {
		if cloudflaredInPATH {
			publicTunnelHint = "cloudflared is in PATH; use `rdev support-session connect --start` and let rdev start/manage the public tunnel, validate the URL, and keep the foreground gateway alive; Quick Tunnel is an ephemeral fallback, so configure a stable gateway URL or named tunnel for repeated sessions"
		} else {
			publicTunnelHint = "no stable public gateway configured; use `rdev support-session connect --start` so rdev can try configured helpers and report manual_action_required if no public tunnel provider is available; configure RDEV_HOSTED_GATEWAY_URL or RDEV_CLOUDFLARED_NAMED_TUNNEL_URL for repeated sessions"
		}
	}
	summary := map[string]any{
		"candidate_count":                 len(candidates),
		"candidate_kinds":                 dedupeStrings(kinds),
		"has_lan_candidate":               hasLAN,
		"has_stable_configured_fallback":  hasStable,
		"has_only_same_machine_candidate": hasSameMachineOnly,
		"needs_public_tunnel":             needsPublicTunnel,
		"cloudflared_in_path":             cloudflaredInPATH,
		"rule":                            "if stable fallback is false, do not promise durable connectivity beyond the current direct/LAN path",
		"quick_tunnel_ephemeral":          needsPublicTunnel && !hasStable,
		"stable_gateway_advice": map[string]any{
			"preferred_for_repeated_sessions": true,
			"default_fallback":                "use rdev support-session connect --start first so Quick Tunnel can establish a working session when no stable gateway is configured",
			"cloud_or_vps":                    "if this Agent runs on a cloud server or a machine with a public DNS/IP, configure RDEV_HOSTED_GATEWAY_URL=https://your-domain-or-public-gateway",
			"cloudflare_named_tunnel":         "for a reusable Cloudflare address, configure RDEV_CLOUDFLARED_NAMED_TUNNEL_URL=https://your-subdomain.example.com plus a reviewed named-tunnel start command or token",
			"do_not_persist_quick_tunnel":     "do not treat https://*.trycloudflare.com Quick Tunnel URLs as durable; they are session fallback URLs",
		},
	}
	if publicTunnelHint != "" {
		summary["public_tunnel_hint"] = publicTunnelHint
	}
	return summary
}

func standardRecoveryActions(actions []string) []string {
	if len(actions) > 0 {
		return actions
	}
	return []string{
		"run rdev support-session prepare to inspect local rdev, Go, Git, repository, helper assets, gateway URL, and target command readiness",
		"if rdev is missing, build it from the checkout with go install ./cmd/rdev or go run ./cmd/rdev bootstrap agent-plan --repo-root .",
		"if helper assets are missing, rerun rdev support-session connect --start from a valid checkout so target bootstraps can download verified helpers",
		"ask one concise question only when authorization, persistence authorization, privileged network changes, or a real gateway/relay credential is required",
	}
}

func ResolveGatewayURL(addr, explicitGatewayURL string) (string, []GatewayURLCandidate) {
	candidates := GatewayURLCandidatesFromIPs(addr, explicitGatewayURL, localInterfaceIPs())
	for _, candidate := range candidates {
		if candidate.Recommended {
			return candidate.URL, candidates
		}
	}
	if len(candidates) > 0 {
		return candidates[0].URL, candidates
	}
	return strings.TrimRight(strings.TrimSpace(explicitGatewayURL), "/"), candidates
}

func GatewayURLCandidatesFromIPs(addr, explicitGatewayURL string, ips []net.IP) []GatewayURLCandidate {
	return gatewayURLCandidatesFromIPsAndEnv(addr, explicitGatewayURL, ips, gatewayEnvCandidatesFromEnv())
}

func ConfiguredGatewayURLCandidate() (string, []GatewayURLCandidate) {
	candidates := appendConfiguredGatewayCandidates(nil, gatewayEnvCandidatesFromEnv())
	for _, candidate := range candidates {
		if candidate.Recommended {
			return candidate.URL, candidates
		}
	}
	for _, candidate := range candidates {
		return candidate.URL, candidates
	}
	return "", candidates
}

func gatewayURLCandidatesFromIPsAndEnv(addr, explicitGatewayURL string, ips []net.IP, envCandidates []GatewayEnvCandidate) []GatewayURLCandidate {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	host, port := splitListenAddress(addr)
	if port == "" {
		port = "8787"
	}
	explicit := strings.TrimRight(strings.TrimSpace(explicitGatewayURL), "/")
	candidates := make([]GatewayURLCandidate, 0, 2+len(ips))
	if explicit != "" {
		candidates = append(candidates, candidateFromURL(explicit, "explicit", "operator-provided", "argument:gateway_url", true, "explicit gateway_url supplied by the Agent/operator"))
	}
	if isWildcardHost(host) {
		for _, ip := range sortedPrivateIPs(ips) {
			url := "http://" + net.JoinHostPort(ip.String(), port)
			candidates = append(candidates, GatewayURLCandidate{
				URL:         url,
				Kind:        "lan-private",
				Scope:       "LAN/VPN/routed-private-network",
				Host:        ip.String(),
				Port:        port,
				Source:      "local-interface",
				Recommended: explicit == "" && !hasRecommendedGatewayCandidate(candidates),
				Reason:      "listen address is wildcard; this private interface address is more useful for target machines than 0.0.0.0",
			})
		}
		candidates = appendConfiguredGatewayCandidates(candidates, envCandidates)
		loopbackURL := "http://" + net.JoinHostPort("127.0.0.1", port)
		candidates = append(candidates, GatewayURLCandidate{
			URL:         loopbackURL,
			Kind:        "loopback",
			Scope:       "same-machine-only",
			Host:        "127.0.0.1",
			Port:        port,
			Source:      "listen-address",
			Recommended: explicit == "" && !hasRecommendedGatewayCandidate(candidates),
			Reason:      "fallback for same-machine testing only; remote target machines cannot use this URL",
		})
		return dedupeGatewayCandidates(candidates)
	}
	urlHost := host
	if urlHost == "" {
		urlHost = "127.0.0.1"
	}
	scope := "host-specific"
	kind := "host"
	reason := "listen address names a concrete host"
	if isLoopbackHost(urlHost) {
		scope = "same-machine-only"
		kind = "loopback"
		reason = "loopback is only suitable when the target is this same machine"
	}
	if kind == "loopback" {
		candidates = appendConfiguredGatewayCandidates(candidates, envCandidates)
	}
	candidates = append(candidates, GatewayURLCandidate{
		URL:         "http://" + net.JoinHostPort(urlHost, port),
		Kind:        kind,
		Scope:       scope,
		Host:        urlHost,
		Port:        port,
		Source:      "listen-address",
		Recommended: explicit == "",
		Reason:      reason,
	})
	return appendConfiguredGatewayCandidates(candidates, envCandidates)
}

func splitListenAddress(addr string) (string, string) {
	host, port, err := net.SplitHostPort(addr)
	if err == nil {
		return strings.Trim(host, "[]"), port
	}
	if strings.HasPrefix(addr, ":") {
		return "", strings.TrimPrefix(addr, ":")
	}
	if strings.Count(addr, ":") == 0 {
		return addr, ""
	}
	return strings.Trim(addr, "[]"), ""
}

func candidateFromURL(rawURL, kind, scope, source string, recommended bool, reason string) GatewayURLCandidate {
	host := ""
	port := ""
	withoutScheme := strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://")
	if parsedURL, err := neturl.Parse(rawURL); err == nil && parsedURL.Host != "" {
		host = strings.Trim(parsedURL.Hostname(), "[]")
		port = parsedURL.Port()
	} else if parsedHost, parsedPort, err := net.SplitHostPort(withoutScheme); err == nil {
		host = strings.Trim(parsedHost, "[]")
		port = parsedPort
	}
	return GatewayURLCandidate{
		URL:         rawURL,
		Kind:        kind,
		Scope:       scope,
		Host:        host,
		Port:        port,
		Source:      source,
		Recommended: recommended,
		Reason:      reason,
	}
}

func gatewayEnvCandidatesFromEnv() []GatewayEnvCandidate {
	definitions := []GatewayEnvCandidate{
		{EnvVar: "RDEV_HOSTED_GATEWAY_URL", Kind: "hosted", Scope: "operator-provided-hosted-gateway", Reason: "configured hosted gateway URL"},
		{EnvVar: "RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", Kind: "cloudflared-named", Scope: "configured-cloudflared-named-tunnel", Reason: "configured reusable Cloudflare named tunnel URL"},
		{EnvVar: "RDEV_RELAY_GATEWAY_URL", Kind: "relay", Scope: "configured-relay", Reason: "configured relay gateway URL"},
		{EnvVar: "RDEV_MESH_GATEWAY_URL", Kind: "mesh", Scope: "configured-mesh", Reason: "configured mesh gateway URL"},
		{EnvVar: "RDEV_VPN_GATEWAY_URL", Kind: "vpn", Scope: "configured-vpn", Reason: "configured VPN gateway URL"},
		{EnvVar: "RDEV_SSH_GATEWAY_URL", Kind: "ssh", Scope: "configured-ssh-tunnel", Reason: "configured SSH tunnel gateway URL"},
		{EnvVar: "RDEV_CLOUDFLARED_GATEWAY_URL", Kind: "cloudflared", Scope: "configured-cloudflared-gateway", Reason: "configured or active Cloudflare gateway URL"},
	}
	out := make([]GatewayEnvCandidate, 0, len(definitions))
	for _, definition := range definitions {
		raw := strings.TrimRight(strings.TrimSpace(os.Getenv(definition.EnvVar)), "/")
		if raw == "" {
			continue
		}
		definition.URL = raw
		out = append(out, definition)
	}
	return out
}

func connectivityHelperPreflight() map[string]any {
	definitions := connectivityHelperDefinitions()
	helpers := make([]map[string]any, 0, len(definitions))
	configured := []string{}
	readyCount := 0
	for _, definition := range definitions {
		gatewayURL := strings.TrimRight(strings.TrimSpace(os.Getenv(definition.GatewayEnv)), "/")
		startArgvRaw := strings.TrimSpace(os.Getenv(definition.StartArgvEnv))
		installActionRaw := strings.TrimSpace(os.Getenv(definition.InstallActionEnv))
		readyIncremented := false
		report := map[string]any{
			"id":                     definition.ID,
			"kind":                   definition.Kind,
			"gateway_env":            definition.GatewayEnv,
			"start_argv_env":         definition.StartArgvEnv,
			"install_action_env":     definition.InstallActionEnv,
			"gateway_configured":     gatewayURL != "",
			"start_configured":       startArgvRaw != "",
			"install_configured":     installActionRaw != "",
			"allowed_tools":          definition.AllowedTools,
			"authorization_required": definition.AuthorizationRequired,
			"status":                 "not-configured",
		}
		if gatewayURL != "" {
			report["gateway_url"] = gatewayURL
			configured = append(configured, definition.ID)
		}
		if startArgvRaw != "" {
			argv, tool, err := parseHelperStartArgv(startArgvRaw, definition.StartArgvEnv, definition.AllowedTools)
			if err != nil {
				report["status"] = "invalid-start-argv"
				report["error"] = err.Error()
			} else {
				report["start_tool"] = tool
				report["start_argc"] = len(argv)
				report["start_argv_preview"] = safeArgvPreview(argv)
				if gatewayURL != "" {
					report["status"] = "ready-to-use-after-authorization-check"
					readyCount++
					readyIncremented = true
				} else {
					report["status"] = "start-argv-without-gateway"
				}
			}
		}
		if definition.ID == "cloudflared-named-tunnel" {
			tokenFileConfigured := strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN_FILE")) != ""
			tokenConfigured := strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN")) != ""
			tunnelNameConfigured := strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_TUNNEL_NAME")) != ""
			report["token_file_configured"] = tokenFileConfigured
			report["token_configured"] = tokenConfigured
			report["tunnel_name_configured"] = tunnelNameConfigured
			report["stable_url_env"] = "RDEV_CLOUDFLARED_NAMED_TUNNEL_URL"
			report["quick_tunnel_ephemeral"] = false
			if gatewayURL != "" && !readyIncremented && (tokenFileConfigured || tokenConfigured || tunnelNameConfigured) {
				report["status"] = "ready-to-use-after-authorization-check"
				readyCount++
				readyIncremented = true
			}
		}
		if installActionRaw != "" {
			actionReport := parseHelperInstallActionReport(installActionRaw, definition.InstallActionEnv, definition.AllowedTools)
			report["install_action"] = actionReport
			if actionReport["valid"] == false && report["status"] == "not-configured" {
				report["status"] = "invalid-install-action"
			}
		}
		helpers = append(helpers, report)
	}
	return map[string]any{
		"schema_version":        ConnectivityHelperPreflightSchemaVersion,
		"intent":                "standard-read-only-helper-configuration-preflight-for-adaptive-connectivity",
		"helpers":               helpers,
		"configured_helper_ids": configured,
		"ready_helper_count":    readyCount,
		"auto_execute":          false,
		"standard_next_step":    "use rdev.connection_entry.plan plus rdev connection-entry run --dry-run when helper execution is needed; do not write custom SSH, relay, mesh, VPN, shell, or PowerShell startup code",
		"agent_rule":            "read this before asking network questions or writing tunnel commands; if a helper is configured, use standard Connection Entry runner metadata and authorization boundaries",
		"forbidden": []string{
			"ExecutionPolicy Bypass",
			"encoded shell commands",
			"shell command-string wrappers",
			"printing credentials or private keys",
			"installing services, drivers, firewall, DNS, route, cloud, or paid resources without explicit authorization",
		},
	}
}

func connectionEntryRunnerRecommendation(opts CreatedOptions, gatewayURL, joinURL, manifestURL string, gatewayCandidates []GatewayURLCandidate, continuityPolicy, helperPreflight map[string]any) map[string]any {
	target := normalizeTarget(opts.Target)
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	targetRdevCommand := "rdev"
	transport := "auto"
	invite, err := agentinvite.New(agentinvite.Options{
		GatewayURL:            gatewayURL,
		JoinURL:               joinURL,
		ManifestURL:           manifestURL,
		ManifestRootPublicKey: opts.ManifestRootPublicKey,
		Ticket:                opts.Ticket,
		Transport:             transport,
		NetworkScope:          "auto",
		AuthorityProfile:      "standard",
		Once:                  false,
		RequireHostActivation: !opts.AutoActivate,
		RdevCommand:           targetRdevCommand,
	})
	targetOS := connectionEntryRunnerTargetOS(target)
	mcpPlanArguments := map[string]any{
		"invite_json":  "",
		"ownership":    "third-party",
		"session_mode": string(model.HostModeAttendedTemporary),
	}
	cliPlanArgv := []string{
		rdevCommand, "connection-entry", "plan",
		"--invite-json", "<invite_json>",
		"--ownership", "third-party",
		"--session-mode", string(model.HostModeAttendedTemporary),
	}
	if targetOS != "auto" {
		mcpPlanArguments["target_os"] = targetOS
		cliPlanArgv = append(cliPlanArgv, "--target-os", targetOS)
	}
	recommendation := map[string]any{
		"schema_version": ConnectionEntryRunnerRecommendationSchemaVersion,
		"intent":         "standard-upgrade-path-from-simple-support-session-to-self-contained-adaptive-connection-entry-runner",
		"recommended_when": []string{
			"the target may leave the LAN or current routed network",
			"the target is operator-owned and expected to support recurring Agent work",
			"gateway_candidate_preflight reports only LAN, loopback, or explicit candidates and durable connectivity is required",
			"connectivity_helper_preflight reports configured SSH, relay, mesh, or VPN helper metadata",
			"the Agent needs a package that probes direct, proxy, relay, mesh, VPN, or SSH-assisted paths before starting rdev host serve",
		},
		"default_human_surface":                 "keep using target_handoff_envelope.full_text for the simplest attended temporary session; user_handoff remains a compatibility fallback; use the runner package when durable or restrictive-network connectivity is needed",
		"standard_tool":                         "rdev.connection_entry.plan",
		"standard_cli":                          "rdev connection-entry plan",
		"runner_schema":                         "rdev.connection-entry.runner.v1",
		"runner_plan_schema":                    "rdev.connection-entry.runner-plan.v1",
		"target_os":                             targetOS,
		"target_os_rule":                        "if target_os is auto, probe or ask for only the OS family before materializing the runner; do not ask for ticket, root, gateway, transport, release, relay, mesh, VPN, or SSH flags",
		"target_os_choices":                     []string{"windows", "darwin", "linux"},
		"target_os_required_for_remote_package": targetOS == "auto",
		"stable_after_lan_change":               continuityPolicy["stable_after_lan_change"],
		"configured_helper_ids":                 helperPreflight["configured_helper_ids"],
		"ready_helper_count":                    helperPreflight["ready_helper_count"],
		"gateway_candidate_count":               len(gatewayCandidates),
		"auto_execute":                          false,
		"dry_run_first":                         true,
		"mcp_plan_call": map[string]any{
			"tool":          "rdev.connection_entry.plan",
			"arguments":     mcpPlanArguments,
			"argument_rule": "set invite_json to invite_json below; when target_os_required_for_remote_package is true, add exactly one target_os from target_os_choices after probing the target OS family",
		},
		"cli_plan_argv": cliPlanArgv,
		"cli_dry_run_argv_template": []string{
			rdevCommand, "connection-entry", "run",
			"--runner-manifest", "<runner_plan.manifest_path>",
			"--dry-run",
		},
		"agent_sequence": []string{
			"use target_handoff_envelope.full_text first for ordinary attended temporary support; user_handoff remains a compatibility fallback",
			"when durable or restrictive-network connectivity is required, materialize this invite with rdev.connection_entry.plan",
			"dry-run the generated runner before execution so direct/LAN/proxy/helper paths are selected by rdev, not by model-authored scripts",
			"give the target-side human only the generated visible launcher or package entry point",
			"after handoff, wait with connection_supervision and report connected_next_steps.user_report when connected=true",
		},
		"forbidden": []string{
			"manual ticket/root/gateway/transport/checksum assembly",
			"Agent-authored SSH, relay, mesh, VPN, PowerShell, shell, or polling scripts",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
			"firewall, DNS, route, service, driver, credential, cloud, or paid relay changes without explicit authorization",
		},
	}
	if err != nil {
		recommendation["ok"] = false
		recommendation["error"] = err.Error()
		return recommendation
	}
	inviteJSON, err := json.Marshal(invite)
	if err != nil {
		recommendation["ok"] = false
		recommendation["error"] = err.Error()
		return recommendation
	}
	recommendation["ok"] = true
	recommendation["invite_schema_version"] = invite.SchemaVersion
	recommendation["invite_json"] = string(inviteJSON)
	mcpCall := recommendation["mcp_plan_call"].(map[string]any)
	args := mcpCall["arguments"].(map[string]any)
	args["invite_json"] = string(inviteJSON)
	recommendation["mcp_plan_call"] = mcpCall
	recommendation["recommended_now"] = helperReadyCount(helperPreflight) > 0 || !boolFromMap(continuityPolicy, "stable_after_lan_change")
	return recommendation
}

func connectionEntryRunnerTargetOS(target string) string {
	switch normalizeTarget(target) {
	case "windows":
		return "windows"
	case "macos", "darwin":
		return "darwin"
	case "linux":
		return "linux"
	default:
		return "auto"
	}
}

func helperReadyCount(preflight map[string]any) int {
	switch value := preflight["ready_helper_count"].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func boolFromMap(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func connectivityHelperDefinitions() []connectivityHelperDefinition {
	return []connectivityHelperDefinition{
		// cloudflared quick-tunnel: free, zero-config, no account needed.
		// Detected automatically and started by the managed foreground
		// support-session flow; Agents must not run it as ad hoc glue.
		{
			ID:                    "cloudflared-quick-tunnel",
			Kind:                  "cloudflared",
			GatewayEnv:            "RDEV_CLOUDFLARED_GATEWAY_URL",
			StartArgvEnv:          "RDEV_CLOUDFLARED_START_ARGV_JSON",
			InstallActionEnv:      "RDEV_CLOUDFLARED_INSTALL_ACTION_JSON",
			AllowedTools:          []string{"cloudflared"},
			AuthorizationRequired: []string{"download/install if not already present"},
		},
		{
			ID:                    "cloudflared-named-tunnel",
			Kind:                  "cloudflared-named",
			GatewayEnv:            "RDEV_CLOUDFLARED_NAMED_TUNNEL_URL",
			StartArgvEnv:          "RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON",
			InstallActionEnv:      "RDEV_CLOUDFLARED_INSTALL_ACTION_JSON",
			AllowedTools:          []string{"cloudflared"},
			AuthorizationRequired: []string{"Cloudflare account/zone setup", "DNS route or hostname mapping", "tunnel token or credentials custody"},
		},
		{
			ID:                    "existing-ssh-tunnel",
			Kind:                  "ssh",
			GatewayEnv:            "RDEV_SSH_GATEWAY_URL",
			StartArgvEnv:          "RDEV_SSH_TUNNEL_START_ARGV_JSON",
			InstallActionEnv:      "RDEV_SSH_INSTALL_ACTION_JSON",
			AllowedTools:          []string{"ssh"},
			AuthorizationRequired: []string{"new keys", "config edits", "ambiguous identities", "privileged ports"},
		},
		{
			ID:                    "existing-frp-or-chisel-relay",
			Kind:                  "relay",
			GatewayEnv:            "RDEV_RELAY_GATEWAY_URL",
			StartArgvEnv:          "RDEV_RELAY_START_ARGV_JSON",
			InstallActionEnv:      "RDEV_RELAY_INSTALL_ACTION_JSON",
			AllowedTools:          []string{"frpc", "chisel"},
			AuthorizationRequired: []string{"download/install", "relay credential creation", "public port changes", "paid relay", "persistent service"},
		},
		{
			ID:                    "existing-headscale-tailscale-mesh",
			Kind:                  "mesh",
			GatewayEnv:            "RDEV_MESH_GATEWAY_URL",
			StartArgvEnv:          "RDEV_MESH_START_ARGV_JSON",
			InstallActionEnv:      "RDEV_MESH_INSTALL_ACTION_JSON",
			AllowedTools:          []string{"tailscale"},
			AuthorizationRequired: []string{"new enrollment", "auth key use", "ACL changes", "DNS changes", "service changes"},
		},
		{
			ID:                    "existing-wireguard-vpn",
			Kind:                  "vpn",
			GatewayEnv:            "RDEV_VPN_GATEWAY_URL",
			StartArgvEnv:          "RDEV_VPN_START_ARGV_JSON",
			InstallActionEnv:      "RDEV_VPN_INSTALL_ACTION_JSON",
			AllowedTools:          []string{"wg", "wg-quick"},
			AuthorizationRequired: []string{"key creation", "profile import", "route/DNS/firewall mutation", "persistent tunnel start"},
		},
	}
}

func parseHelperStartArgv(raw, envName string, allowedTools []string) ([]string, string, error) {
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		return nil, "", fmt.Errorf("%s must be a JSON argv array: %w", envName, err)
	}
	if len(argv) == 0 {
		return nil, "", fmt.Errorf("%s must contain at least one argv item", envName)
	}
	for i, arg := range argv {
		if strings.TrimSpace(arg) == "" {
			return nil, "", fmt.Errorf("%s item %d must not be empty", envName, i)
		}
	}
	if err := rejectUnsafeArgv(argv, envName); err != nil {
		return nil, "", err
	}
	tool := executableBaseName(argv[0])
	if !helperToolAllowed(tool, allowedTools) {
		return nil, "", fmt.Errorf("%s starts %q, but only allows: %s", envName, tool, strings.Join(allowedTools, ", "))
	}
	return argv, tool, nil
}

func parseHelperInstallActionReport(raw, envName string, allowedTools []string) map[string]any {
	var action struct {
		SchemaVersion     string   `json:"schema_version"`
		Tool              string   `json:"tool"`
		Argv              []string `json:"argv"`
		Scope             string   `json:"scope"`
		Reason            string   `json:"reason"`
		ExpectedSHA256    string   `json:"expected_sha256"`
		RequiresElevation bool     `json:"requires_elevation"`
	}
	report := map[string]any{"valid": false, "env": envName}
	if err := json.Unmarshal([]byte(raw), &action); err != nil {
		report["error"] = fmt.Sprintf("%s must be a JSON dependency install action: %v", envName, err)
		return report
	}
	tool := executableBaseName(action.Tool)
	report["tool"] = tool
	report["scope"] = action.Scope
	report["reason"] = action.Reason
	report["has_expected_sha256"] = strings.TrimSpace(action.ExpectedSHA256) != ""
	report["requires_elevation"] = action.RequiresElevation
	if tool == "" {
		report["error"] = envName + " must include a non-empty tool"
		return report
	}
	if !helperToolAllowed(tool, allowedTools) {
		report["error"] = fmt.Sprintf("%s installs %q, but only allows: %s", envName, tool, strings.Join(allowedTools, ", "))
		return report
	}
	if len(action.Argv) == 0 {
		report["error"] = envName + " must include a non-empty argv array"
		return report
	}
	if err := rejectUnsafeArgv(action.Argv, envName); err != nil {
		report["error"] = err.Error()
		return report
	}
	if action.RequiresElevation {
		report["error"] = envName + " requires elevation; use a reviewed managed package/service plan instead"
		return report
	}
	scope := strings.TrimSpace(action.Scope)
	if scope == "" {
		scope = "user"
	}
	switch scope {
	case "user", "workspace", "attended-visible", "managed-authorized":
	default:
		report["error"] = fmt.Sprintf("%s has unsupported install scope %q", envName, scope)
		return report
	}
	if strings.TrimSpace(action.ExpectedSHA256) != "" && !isHexSHA256(action.ExpectedSHA256) {
		report["error"] = envName + " expected_sha256 must be a hex SHA-256 value"
		return report
	}
	report["valid"] = true
	report["argc"] = len(action.Argv)
	report["argv_preview"] = safeArgvPreview(action.Argv)
	return report
}

func safeArgvPreview(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := make([]string, 0, len(argv))
	for i, arg := range argv {
		if i == 0 {
			out = append(out, executableBaseName(arg))
			continue
		}
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key") || strings.Contains(lower, "password") {
			out = append(out, "<redacted>")
			continue
		}
		out = append(out, arg)
	}
	if len(out) > 8 {
		return append(out[:8], "...")
	}
	return out
}

func executableBaseName(value string) string {
	normalized := strings.ReplaceAll(value, "\\", "/")
	parts := strings.Split(normalized, "/")
	base := strings.TrimSpace(parts[len(parts)-1])
	return strings.TrimSuffix(strings.ToLower(base), ".exe")
}

// cloudflaredAvailable returns true when the cloudflared binary is findable
// in the system PATH (or on Windows, also in common install locations).
// It intentionally avoids running cloudflared so there are no side effects.
func cloudflaredAvailable() bool {
	_, err := exec.LookPath("cloudflared")
	return err == nil
}

func helperToolAllowed(tool string, allowed []string) bool {
	tool = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(tool)), ".exe")
	for _, value := range allowed {
		if tool == strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".exe") {
			return true
		}
	}
	return false
}

func rejectUnsafeArgv(argv []string, label string) error {
	if len(argv) == 0 {
		return nil
	}
	tool := executableBaseName(argv[0])
	for _, arg := range argv[1:] {
		lower := strings.ToLower(strings.TrimSpace(arg))
		if strings.EqualFold(arg, "bypass") {
			return fmt.Errorf("%s must not use ExecutionPolicy Bypass", label)
		}
		if lower == "-encodedcommand" || lower == "-enc" {
			return fmt.Errorf("%s must not use encoded shell commands", label)
		}
		if !isShellTool(tool) {
			continue
		}
		if lower == "-c" || lower == "/c" || lower == "-command" {
			return fmt.Errorf("%s must use argv execution, not shell command-string execution", label)
		}
	}
	return nil
}

func isShellTool(tool string) bool {
	switch tool {
	case "sh", "bash", "zsh", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func isHexSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func appendConfiguredGatewayCandidates(candidates []GatewayURLCandidate, envCandidates []GatewayEnvCandidate) []GatewayURLCandidate {
	for _, envCandidate := range envCandidates {
		url := strings.TrimRight(strings.TrimSpace(envCandidate.URL), "/")
		if url == "" {
			continue
		}
		recommended := !hasRecommendedGatewayCandidate(candidates)
		candidates = append(candidates, candidateFromURL(
			url,
			envCandidate.Kind,
			envCandidate.Scope,
			"env:"+envCandidate.EnvVar,
			recommended,
			envCandidate.Reason+" from "+envCandidate.EnvVar,
		))
	}
	return dedupeGatewayCandidates(candidates)
}

func localInterfaceIPs() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			out = append(out, normalizeIP(ip))
		}
	}
	return out
}

func sortedPrivateIPs(ips []net.IP) []net.IP {
	values := make([]net.IP, 0, len(ips))
	seen := map[string]bool{}
	for _, ip := range ips {
		ip = normalizeIP(ip)
		if ip == nil || !ip.IsPrivate() {
			continue
		}
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, ip)
	}
	sort.Slice(values, func(i, j int) bool {
		left4 := values[i].To4() != nil
		right4 := values[j].To4() != nil
		if left4 != right4 {
			return left4
		}
		return values[i].String() < values[j].String()
	})
	return values
}

func normalizeIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

func isWildcardHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func hasRecommendedGatewayCandidate(candidates []GatewayURLCandidate) bool {
	for _, candidate := range candidates {
		if candidate.Recommended {
			return true
		}
	}
	return false
}

func dedupeGatewayCandidates(candidates []GatewayURLCandidate) []GatewayURLCandidate {
	out := make([]GatewayURLCandidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.URL) == "" || seen[candidate.URL] {
			continue
		}
		seen[candidate.URL] = true
		out = append(out, candidate)
	}
	return out
}

func dedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func BuildCreated(opts CreatedOptions) map[string]any {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	joinURL := strings.TrimSpace(opts.JoinURL)
	if joinURL == "" && gatewayURL != "" && opts.Ticket.Code != "" {
		joinURL = gatewayURL + "/join/" + opts.Ticket.Code
	}
	manifestURL := strings.TrimSpace(opts.ManifestURL)
	if manifestURL == "" && gatewayURL != "" && opts.Ticket.Code != "" {
		manifestURL = gatewayURL + "/v1/tickets/" + opts.Ticket.Code + "/manifest"
	}
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	target := normalizeTarget(opts.Target)
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	gatewayCandidates := normalizeCreatedGatewayCandidates(gatewayURL, opts.GatewayURLCandidates)
	joinURLs := joinURLsForGatewayCandidates(gatewayCandidates, opts.Ticket.Code)
	if len(joinURLs) == 0 && joinURL != "" {
		joinURLs = []string{joinURL}
	}
	attemptPolicy := connectionAttemptPolicy(gatewayCandidates, joinURLs)
	continuityPolicy := connectionContinuityPolicy(gatewayCandidates)
	helperPreflight := connectivityHelperPreflight()
	runnerRecommendation := connectionEntryRunnerRecommendation(opts, gatewayURL, joinURL, manifestURL, gatewayCandidates, continuityPolicy, helperPreflight)
	runtimeGatewayCandidates := runtimeGatewayCandidates(gatewayCandidates)
	windowsCommand := windowsBootstrapCommand(joinURLs, runtimeGatewayCandidates)
	macLinuxCommand := macLinuxBootstrapCommand(joinURLs, runtimeGatewayCandidates)
	bootstrapRequirements := bootstrapRequirements(target)
	targetCommands := map[string]string{
		"windows":     windowsCommand,
		"macos_linux": macLinuxCommand,
		"join_url":    joinURL,
	}
	recommended := autoTargetCopyPaste(joinURL, targetCommands)
	recommendedSurface := "multi_platform"
	switch target {
	case "windows":
		recommended = windowsCommand
		recommendedSurface = "windows"
	case "macos", "linux":
		recommended = macLinuxCommand
		recommendedSurface = "macos_linux"
	}
	statusCommand := []string{
		rdevCommand, "support-session", "status",
		"--gateway-url", gatewayURL,
		"--ticket-code", opts.Ticket.Code,
		"--wait",
		"--locale", locale,
	}
	configuredGatewayStatusCommand := []string{
		rdevCommand, "support-session", "status",
		"--ticket-code", opts.Ticket.Code,
		"--wait",
		"--locale", locale,
	}
	remoteControlEntry := BuildRemoteControlEntry(RemoteControlEntryOptions{
		GatewayURL: gatewayURL,
		TicketCode: opts.Ticket.Code,
		Ticket:     &opts.Ticket,
		Locale:     locale,
	})
	readiness := normalizeAvailabilityReadiness(opts.AvailabilityReadiness)
	agentFlow := []string{
		"give the target-side human only target_handoff_envelope.full_text when present; use target_command or join_url only as compatibility fallback fields",
		"target_command is the human-safe primary command; signed join-manifest gateway candidates are embedded for rdev runtime failover after bootstrap; do not write your own fallback script",
		"read connection_continuity_policy to decide whether this session survives LAN changes or needs a configured hosted/relay/mesh/VPN/SSH path",
		"if the gateway was not started by rdev support-session start, verify target_bootstrap_requirements before sending a Windows/macOS/Linux command",
		"watch connection status with watch_connection_status or rdev.support_session.status",
		"read rdev_bootstrap_connector before interpreting target_preconnects; preconnect means the target command started before the full helper finished downloading, not that session tasks can run yet",
		"when connected=true, proactively report that the connection is established",
		"do not ask the human to assemble ticket, gateway, manifest root, transport, or helper flags",
	}
	if !readiness.ReadyToSend {
		agentFlow[0] = blockedHandoffInstruction(readiness.DegradedReason)
		agentFlow[3] = blockedHandoffInstruction(readiness.DegradedReason)
	}
	return map[string]any{
		"schema_version":                         CreatedSchemaVersion,
		"ok":                                     true,
		"session_mode":                           string(model.HostModeAttendedTemporary),
		"intent":                                 "agent-created-one-command-visible-support-session",
		"gateway_url":                            gatewayURL,
		"gateway_url_candidates":                 gatewayCandidates,
		"ticket_code":                            opts.Ticket.Code,
		"ticket":                                 opts.Ticket,
		"join_url":                               joinURL,
		"manifest_url":                           manifestURL,
		"manifest_root_public_key":               opts.ManifestRootPublicKey,
		"target":                                 target,
		"locale":                                 locale,
		"auto_activate":                          opts.AutoActivate,
		"availability_readiness":                 readiness,
		"ready_to_send":                          readiness.ReadyToSend,
		"ready_to_activate":                      readiness.ReadyToActivate,
		"ready_to_execute":                       readiness.ReadyToExecute,
		"ready_to_send_to_human":                 readiness.ReadyToSend,
		"recommended_surface":                    recommendedSurface,
		"target_command":                         recommended,
		"target_commands":                        targetCommands,
		"user_handoff":                           userHandoff(locale, target, recommendedSurface, recommended, joinURL, targetCommands, readiness),
		"target_handoff_envelope":                targetHandoffEnvelope(locale, target, recommendedSurface, recommended, joinURL, targetCommands, remoteControlEntry, readiness),
		"remote_control_entry":                   remoteControlEntry,
		"connection_attempt_policy":              attemptPolicy,
		"connection_continuity_policy":           continuityPolicy,
		"connection_supervision":                 connectionSupervision(opts.Ticket.Code, locale, rdevCommand, gatewayURL, attemptPolicy, continuityPolicy),
		"gateway_candidate_preflight":            gatewayCandidatePreflight(gatewayURL, target, gatewayCandidates),
		"connectivity_helper_preflight":          helperPreflight,
		"connection_entry_runner_recommendation": runnerRecommendation,
		"rdev_bootstrap_connector":               rdevBootstrapConnectorContract(),
		"fresh_agent_connect_contract": freshAgentConnectContract(freshAgentConnectContractOptions{
			Phase:                 "created",
			AvailabilityReadiness: readiness,
			TicketCode:            opts.Ticket.Code,
			GatewayURL:            gatewayURL,
			RdevCommand:           rdevCommand,
			AutoActivate:          opts.AutoActivate,
		}),
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:        "created",
			Status:       "waiting",
			TicketCode:   opts.Ticket.Code,
			Target:       target,
			Locale:       locale,
			GatewayURL:   gatewayURL,
			Candidates:   gatewayCandidates,
			AutoActivate: opts.AutoActivate,
			RdevCommand:  rdevCommand,
		}),
		"target_bootstrap_requirements": bootstrapRequirements,
		"target_bootstrap_readiness":    opts.TargetBootstrapReadiness,
		"watch_connection_status":       statusCommand,
		"watch_connection_status_configured_gateway": map[string]any{
			"command": configuredGatewayStatusCommand,
			"requires_configured_gateway_env": []string{
				"RDEV_HOSTED_GATEWAY_URL",
				"RDEV_RELAY_GATEWAY_URL",
				"RDEV_MESH_GATEWAY_URL",
				"RDEV_VPN_GATEWAY_URL",
				"RDEV_SSH_GATEWAY_URL",
			},
			"agent_rule": "use this shorter watcher when one RDEV_*_GATEWAY_URL is configured; otherwise use watch_connection_status",
		},
		"mcp_follow_up": []map[string]any{
			{
				"tool": "rdev.support_session.status",
				"arguments": map[string]any{
					"ticket_code": opts.Ticket.Code,
					"locale":      locale,
					"wait":        true,
					"gateway_url": gatewayURL,
				},
			},
		},
		"human_message": localizedCreatedMessage(locale),
		"agent_flow":    agentFlow,
		"forbidden": []string{
			"ExecutionPolicy Bypass",
			"hidden install",
			"manual ticket/root/gateway/transport assembly for target user",
			"ad hoc bootstrap script generated by the Agent",
		},
	}
}

func rdevBootstrapConnectorContract() map[string]any {
	return map[string]any{
		"schema_version":                       BootstrapConnectorSchemaVersion,
		"first_connect_target_bytes":           supportSessionBootstrapTargetBytes,
		"bootstrap_surface":                    "script-preconnect",
		"default_first_connect_surface":        "script-preconnect",
		"publishes_native_first_connect_asset": false,
		"preconnect_endpoint":                  "/v1/support-session/preconnect",
		"preconnect_phase_before_full_helper":  "downloading-helper",
		"source":                               "rdev-bootstrap-preconnect",
		"native_connector": map[string]any{
			"schema_version":                      "rdev.bootstrap-native-connector.v1",
			"source":                              "rdev-bootstrap-native",
			"availability":                        "optional-if-rdev-bootstrap-is-already-installed-or-published",
			"published_by_support_session_assets": false,
			"default_first_connect_surface":       "script-preconnect",
			"command":                             []string{"rdev-bootstrap", "upgrade"},
			"capabilities":                        []string{"preconnect to gateway", "download verified full helper", "verify SHA-256", "exec verified full helper"},
			"can_run_session_tasks_before_full_runner":  false,
			"requires_full_runner_before_session_tasks": true,
			"standard_no_exec_probe":                    []string{"rdev-bootstrap", "upgrade", "--no-exec", "--gateway-url", "<gateway-url>", "--ticket-code", "<ticket-code>", "--asset", "<asset>", "--out", "<path>"},
			"asset_budget_rule":                         "do not publish native rdev-bootstrap as the default first-connect asset until its compressed release artifact is proven under first_connect_target_bytes",
			"agent_rule":                                "use rdev-bootstrap upgrade only when rdev-bootstrap is already installed or explicitly published by the release assets; otherwise use script-preconnect and do not treat rdev-bootstrap itself as a session task runner",
		},
		"cdn_download_optimizer":                 cdnDownloadOptimizerContract(),
		"grants_host_access":                     false,
		"can_run_session_tasks":                  false,
		"full_runner_phase":                      "download-verified-rdev-host",
		"upgrade_required_for":                   []string{"shell tasks", "filesystem tasks", "process tasks", "desktop operations", "coding tasks"},
		"status_fields":                          []string{"target_preconnects", "target_preconnect_count"},
		"agent_rule":                             "treat target_preconnects as evidence that the target-side bootstrap command started and reached the gateway, not as a connected executable host; wait for connected=true before submitting session tasks",
		"operator_explanation":                   "bootstrap preconnect is a low-byte first-contact signal that narrows download/network diagnosis before the full verified helper is available",
		"must_not_be_used_for_authorization":     true,
		"must_not_skip_full_helper_verification": true,
	}
}

func cdnDownloadOptimizerContract() map[string]any {
	plan := cdnopt.BuildPlan(cdnopt.Options{Provider: "cloudflare"})
	return map[string]any{
		"schema_version":           plan.SchemaVersion,
		"provider":                 plan.Provider,
		"status":                   plan.Status,
		"dry_run":                  plan.DryRun,
		"enabled_by_default":       plan.EnabledByDefault,
		"requires_explicit_enable": plan.RequiresExplicitEnable,
		"asset_downloads_only":     plan.AssetDownloadsOnly,
		"max_concurrency":          plan.MaxConcurrency,
		"sample_bytes":             plan.SampleBytes,
		"timeout_seconds":          plan.TimeoutSeconds,
		"candidate_sources":        plan.CandidateSources,
		"forbidden_side_effects":   plan.ForbiddenSideEffects,
		"safety_rules":             plan.SafetyRules,
		"agent_rule":               plan.AgentRule,
	}
}

func userHandoff(locale, target, surface, copyPaste, joinURL string, targetCommands map[string]string, readiness AvailabilityReadiness) map[string]any {
	agentNextStep := "send message plus copy_paste to the user, then call rdev.support_session.status with wait=true"
	agentRule := "do not rewrite copy_paste, do not ask the user to assemble ticket/root/gateway/transport, and do not add custom polling"
	autoTargetRule := autoTargetHandoffRule(target)
	if !readiness.ReadyToSend {
		agentNextStep = blockedHandoffInstruction(readiness.DegradedReason)
		agentRule = blockedHandoffInstruction(readiness.DegradedReason)
		autoTargetRule = blockedHandoffInstruction(readiness.DegradedReason)
	}
	return map[string]any{
		"schema_version":      UserHandoffSchemaVersion,
		"locale":              locale,
		"target":              target,
		"copy_paste_kind":     surface,
		"copy_paste":          copyPaste,
		"join_url":            joinURL,
		"message":             localizedUserHandoffMessage(locale, surface),
		"windows_command":     targetCommands["windows"],
		"macos_linux_command": targetCommands["macos_linux"],
		"auto_target_rule":    autoTargetRule,
		"ready_to_send":       readiness.ReadyToSend,
		"agent_next_step":     agentNextStep,
		"agent_rule":          agentRule,
	}
}

func targetHandoffEnvelope(locale, target, surface, copyPaste, joinURL string, targetCommands map[string]string, remoteControlEntry map[string]any, readiness AvailabilityReadiness) map[string]any {
	message := localizedUserHandoffMessage(locale, surface)
	fullText := message + "\n\n" + copyPaste
	if card := remoteControlEntryText(remoteControlEntry); card != "" {
		fullText += "\n\n" + card
	}
	agentRule := "send full_text verbatim to the target-side human; do not rewrite copy_paste, append ticket/root/gateway details, or generate custom setup scripts"
	afterSend := "wait with connection_supervision, status_file.path, foreground_feedback, or rdev.support_session.status wait=true and report connected_next_steps.user_report when connected=true"
	autoTargetRule := autoTargetHandoffRule(target)
	if !readiness.ReadyToSend {
		agentRule = blockedHandoffInstruction(readiness.DegradedReason)
		afterSend = blockedHandoffInstruction(readiness.DegradedReason)
		autoTargetRule = blockedHandoffInstruction(readiness.DegradedReason)
	}
	return map[string]any{
		"schema_version":       TargetHandoffEnvelopeSchemaVersion,
		"locale":               locale,
		"target":               target,
		"ready_to_forward":     readiness.ReadyToSend,
		"format":               "plain-text",
		"message":              message,
		"copy_paste":           copyPaste,
		"copy_paste_kind":      surface,
		"full_text":            fullText,
		"join_url":             joinURL,
		"remote_control_entry": remoteControlEntry,
		"fallbacks": map[string]string{
			"windows_command":     targetCommands["windows"],
			"macos_linux_command": targetCommands["macos_linux"],
			"join_url":            targetCommands["join_url"],
		},
		"auto_target_rule": autoTargetRule,
		"agent_rule":       agentRule,
		"after_send":       afterSend,
		"forbidden": []string{
			"rewriting copy_paste",
			"manual ticket/root/gateway/transport assembly",
			"custom PowerShell or shell bootstrap",
			"custom relay, mesh, VPN, SSH, authorization, or polling scripts",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
		},
	}
}

func blockedHandoffInstruction(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "availability readiness is not sendable"
	}
	return "do not send target_handoff_envelope.full_text, handoff_text_file.path, user_handoff.message, or user_handoff.copy_paste to the target human while ready_to_send=false: " + reason
}

func readinessWithReason(readiness AvailabilityReadiness, reason string) AvailabilityReadiness {
	readiness.DegradedReason = strings.TrimSpace(reason)
	return readiness
}

func userHandoffWithReadiness(value any, readiness AvailabilityReadiness) map[string]any {
	handoff := cloneAnyMap(value)
	handoff["ready_to_send"] = readiness.ReadyToSend
	if !readiness.ReadyToSend {
		handoff["agent_next_step"] = blockedHandoffInstruction(readiness.DegradedReason)
		handoff["agent_rule"] = blockedHandoffInstruction(readiness.DegradedReason)
		handoff["auto_target_rule"] = blockedHandoffInstruction(readiness.DegradedReason)
	}
	return handoff
}

func targetHandoffEnvelopeWithReadiness(value any, readiness AvailabilityReadiness) map[string]any {
	envelope := cloneAnyMap(value)
	envelope["ready_to_forward"] = readiness.ReadyToSend
	if !readiness.ReadyToSend {
		envelope["agent_rule"] = blockedHandoffInstruction(readiness.DegradedReason)
		envelope["after_send"] = blockedHandoffInstruction(readiness.DegradedReason)
		envelope["auto_target_rule"] = blockedHandoffInstruction(readiness.DegradedReason)
	}
	return envelope
}

func cloneAnyMap(value any) map[string]any {
	source, _ := value.(map[string]any)
	cloned := make(map[string]any, len(source))
	for key, item := range source {
		cloned[key] = item
	}
	return cloned
}

func handoffFileInstruction(ready bool, reason, allowed string) string {
	if ready {
		return allowed
	}
	return blockedHandoffInstruction(reason)
}

func remoteControlEntryText(entry map[string]any) string {
	if len(entry) == 0 {
		return ""
	}
	deviceID, _ := entry["support_device_id"].(string)
	passcode, _ := entry["session_passcode"].(string)
	if strings.TrimSpace(deviceID) == "" && strings.TrimSpace(passcode) == "" {
		return ""
	}
	lines := []string{"Remote control style entry:"}
	if strings.TrimSpace(deviceID) != "" {
		lines = append(lines, "Device ID: "+deviceID)
	}
	if strings.TrimSpace(passcode) != "" {
		lines = append(lines, "Session Password: "+passcode)
	}
	lines = append(lines, "Keep this visible connector open. The Agent will not disconnect it unless the operator explicitly asks.")
	return strings.Join(lines, "\n")
}

func autoTargetHandoffRule(target string) string {
	if target != "auto" {
		return "target platform is selected; send copy_paste verbatim"
	}
	return "target platform is unknown; send the multi-platform full_text verbatim so the target-side human can choose Windows PowerShell, macOS/Linux terminal, or browser fallback without receiving a bare URL"
}

func autoTargetCopyPaste(joinURL string, targetCommands map[string]string) string {
	parts := []string{
		"Choose the matching option on the target computer and keep the window open while I connect.",
	}
	if windows := strings.TrimSpace(targetCommands["windows"]); windows != "" {
		parts = append(parts, "Windows PowerShell:\n"+windows)
	}
	if macLinux := strings.TrimSpace(targetCommands["macos_linux"]); macLinux != "" {
		parts = append(parts, "macOS/Linux terminal:\n"+macLinux)
	}
	if joinURL = strings.TrimSpace(joinURL); joinURL != "" {
		parts = append(parts, "Browser fallback:\n"+joinURL)
	}
	return strings.Join(parts, "\n\n")
}

func normalizeCreatedGatewayCandidates(gatewayURL string, candidates []GatewayURLCandidate) []GatewayURLCandidate {
	values := make([]GatewayURLCandidate, 0, len(candidates)+1)
	for _, candidate := range candidates {
		candidate.URL = strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if candidate.URL == "" {
			continue
		}
		values = append(values, candidate)
	}
	if gatewayURL != "" && len(values) == 0 {
		values = append(values, candidateFromURL(gatewayURL, "explicit", "operator-provided", "created:generation", true, "created support session gateway URL"))
	}
	if !hasRecommendedGatewayCandidate(values) && len(values) > 0 {
		values[0].Recommended = true
	}
	return dedupeGatewayCandidates(values)
}

func joinURLsForGatewayCandidates(candidates []GatewayURLCandidate, ticketCode string) []string {
	if strings.TrimSpace(ticketCode) == "" {
		return nil
	}
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		base := strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if base == "" {
			continue
		}
		values = append(values, base+"/join/"+ticketCode)
	}
	return dedupeStrings(values)
}

func BuildRemoteControlEntry(opts RemoteControlEntryOptions) map[string]any {
	ticketCode := strings.TrimSpace(opts.TicketCode)
	var ticket *model.Ticket
	if opts.Ticket != nil {
		ticket = opts.Ticket
		if ticketCode == "" {
			ticketCode = strings.TrimSpace(ticket.Code)
		}
	}
	hosts := append([]model.Host(nil), opts.Hosts...)
	deviceID, deviceIDSource := remoteControlDeviceID(ticketCode, hosts)
	activeHosts := hostsByStatus(hosts, model.HostStatusActive)
	staleHosts := hostsByStatus(hosts, model.HostStatusStale)
	pendingHosts := hostsByStatus(hosts, model.HostStatusPending)
	passcode := remoteControlSessionPasscode(ticketCode)
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	ephemeralGateway := strings.Contains(strings.ToLower(gatewayURL), ".trycloudflare.com")
	persistenceMode := "visible-attended-connector"
	if len(activeHosts) > 0 || len(staleHosts) > 0 || len(pendingHosts) > 0 {
		persistenceMode = "visible-attended-connector-with-persistent-host-identity"
	}
	entry := map[string]any{
		"schema_version":               RemoteControlEntrySchemaVersion,
		"product_model":                "remote-control-style support device entry for AI Agents",
		"entry_name":                   "Support Device Entry",
		"support_device_id":            deviceID,
		"support_device_id_source":     deviceIDSource,
		"session_passcode":             passcode,
		"session_passcode_kind":        "ticket-scoped session passcode",
		"session_passcode_rotates":     true,
		"gateway_url":                  gatewayURL,
		"ephemeral_gateway":            ephemeralGateway,
		"stable_gateway_required_for":  []string{"same address across Agent sessions", "managed service reconnect", "owned recurring host"},
		"connector_persistence":        persistenceMode,
		"explicit_disconnect_required": true,
		"agent_rule":                   "Treat this like a remote-control app entry: use support_device_id plus session_passcode/status/report fields, keep the connector online after work, and disconnect/revoke/stop only after an explicit operator request.",
		"human_rule":                   "The target-side person opens the visible connector and keeps it open; closing the connector or revoking the ticket ends access.",
		"temporary_support_policy":     "Temporary customer support remains visible and attended but is not auto-disconnected when the Agent finishes a task.",
		"managed_upgrade_policy":       "For an operator-owned recurring machine, ask for explicit managed-service authorization and require a stable gateway before installing service persistence.",
		"forbidden": []string{
			"Agent-initiated disconnect after task completion",
			"hidden install",
			"unauthorized service persistence",
			"long-lived shared host password",
		},
	}
	if ticketCode != "" {
		entry["ticket_code"] = ticketCode
	}
	if ticket != nil {
		entry["ticket_status"] = string(ticket.Status)
		entry["ticket_expires_at"] = ticket.ExpiresAt.UTC().Format(time.RFC3339)
		entry["ticket_expires_in_seconds"] = maxInt(0, int(ticket.ExpiresAt.Sub(time.Now().UTC()).Seconds()))
	}
	if len(activeHosts) == 1 {
		entry["recommended_task_endpoint_id"] = activeHosts[0].ID
	}
	if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
		entry["session_id"] = sessionID
	}
	if targetEndpointID := strings.TrimSpace(opts.TargetEndpointID); targetEndpointID != "" {
		entry["recommended_target_endpoint_id"] = targetEndpointID
		entry["recommended_task_endpoint_id"] = targetEndpointID
	}
	if len(hosts) > 0 {
		entry["host_count"] = map[string]int{
			"active":  len(activeHosts),
			"stale":   len(staleHosts),
			"pending": len(pendingHosts),
			"total":   len(hosts),
		}
	}
	return entry
}

func remoteControlDeviceID(ticketCode string, hosts []model.Host) (string, string) {
	for _, host := range hosts {
		if value := strings.TrimSpace(host.IdentityFingerprint); value != "" {
			return humanCode("RDEV", value), "host_identity_fingerprint"
		}
	}
	for _, host := range hosts {
		if value := strings.TrimSpace(host.ID); value != "" {
			return humanCode("RDEV", value), "host_id"
		}
	}
	if ticketCode = strings.TrimSpace(ticketCode); ticketCode != "" {
		return "RDEV-" + strings.ToUpper(strings.ReplaceAll(ticketCode, "_", "-")), "connection_entry_ticket"
	}
	return "RDEV-PENDING", "pending-target-connector"
}

func remoteControlSessionPasscode(ticketCode string) string {
	ticketCode = strings.TrimSpace(ticketCode)
	if ticketCode == "" {
		return ""
	}
	return strings.ToUpper(ticketCode)
}

func humanCode(prefix, seed string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(seed)))
	value := strings.ToUpper(hex.EncodeToString(sum[:]))[:12]
	return prefix + "-" + value[0:4] + "-" + value[4:8] + "-" + value[8:12]
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func connectionAttemptPolicy(candidates []GatewayURLCandidate, joinURLs []string) map[string]any {
	candidateReports := make([]map[string]any, 0, len(joinURLs))
	for i, joinURL := range joinURLs {
		report := map[string]any{
			"order":    i + 1,
			"join_url": joinURL,
		}
		if i < len(candidates) {
			report["gateway_url"] = candidates[i].URL
			report["kind"] = candidates[i].Kind
			report["scope"] = candidates[i].Scope
			report["recommended"] = candidates[i].Recommended
		}
		candidateReports = append(candidateReports, report)
	}
	return map[string]any{
		"schema_version":               ConnectionAttemptPolicySchemaVersion,
		"candidate_order":              candidateReports,
		"windows_download_timeout_sec": targetHTTPMaxTimeSeconds,
		"curl_connect_timeout_sec":     targetHTTPConnectTimeoutSeconds,
		"curl_max_time_sec":            targetHTTPMaxTimeSeconds,
		"retries_per_candidate":        targetHTTPRetries,
		"retry_delay_sec":              targetHTTPRetryDelaySeconds,
		"target_handoff":               "single visible command; Windows uses the first human-safe bootstrap URL and passes signed gateway candidates to the rdev runtime; macOS/Linux keeps bounded candidate download fallback",
		"agent_rule":                   "do not rewrite, wrap, or expand target_command; watch the returned status tool/command instead",
	}
}

func connectionContinuityPolicy(candidates []GatewayURLCandidate) map[string]any {
	kinds := make([]string, 0, len(candidates))
	stableKinds := []string{}
	hasLAN := false
	hasLoopbackOnly := len(candidates) == 0
	for _, candidate := range candidates {
		kind := strings.TrimSpace(candidate.Kind)
		if kind == "" {
			kind = "unknown"
		}
		kinds = append(kinds, kind)
		switch kind {
		case "lan-private":
			hasLAN = true
			hasLoopbackOnly = false
		case "hosted", "relay", "mesh", "vpn", "ssh":
			stableKinds = append(stableKinds, kind)
			hasLoopbackOnly = false
		case "explicit", "host":
			if candidate.Scope != "same-machine-only" {
				hasLoopbackOnly = false
			}
		case "loopback":
		default:
			hasLoopbackOnly = false
		}
	}
	stableAfterLANChange := len(stableKinds) > 0
	assessment := "lan-or-explicit-path"
	if stableAfterLANChange {
		assessment = "stable-fallback-configured"
	} else if hasLAN {
		assessment = "lan-dependent"
	} else if hasLoopbackOnly {
		assessment = "same-machine-only"
	}
	return map[string]any{
		"schema_version":                      ContinuityPolicySchemaVersion,
		"intent":                              "agent-readable-continuity-and-reconnect-guidance",
		"connection_model":                    "target-initiated-outbound-connection-entry-with-ordered-candidate-fallback",
		"candidate_kinds":                     dedupeStrings(kinds),
		"stable_fallback_kinds":               dedupeStrings(stableKinds),
		"has_lan_candidate":                   hasLAN,
		"has_stable_configured_fallback":      stableAfterLANChange,
		"stable_after_lan_change":             stableAfterLANChange,
		"assessment":                          assessment,
		"target_command_behavior":             "the returned target command is human-safe and includes signed gateway candidate metadata for rdev runtime failover after bootstrap",
		"agent_watch_behavior":                "after handing over target_handoff_envelope.full_text, use rdev.support_session.status wait=true or the returned watcher command and proactively report connected=true",
		"automatic_downgrade":                 []string{"target command tries the next Connection Entry URL when direct or LAN bootstrap fails", "host transport auto may downgrade WSS to HTTPS long-poll to short polling", "after registration, host transport auto may switch to another signed join-manifest gateway candidate when the current gateway fails before session tasks are processed", "status wait timeout returns connection_recovery instead of requiring custom polling"},
		"automatic_upgrade":                   []string{"when an RDEV_HOSTED_GATEWAY_URL, RDEV_CLOUDFLARED_NAMED_TUNNEL_URL, RDEV_RELAY_GATEWAY_URL, RDEV_MESH_GATEWAY_URL, RDEV_VPN_GATEWAY_URL, or RDEV_SSH_GATEWAY_URL becomes configured, create a fresh Connection Entry so future target commands include that stable path", "for operator-owned recurring machines, move from attended-temporary to a reviewed managed Connection Entry only after explicit persistence authorization", "persist the best verified stable path in scoped runtime memory after evidence is reviewed"},
		"if_lan_or_loopback_fails":            []string{"run rdev.support_session.prepare or rdev support-session prepare --build-assets to refresh gateway_url_candidates", "prefer configured hosted/relay/mesh/VPN/SSH gateway URLs before asking the human for network details", "ask only when privileged network changes, credentials, paid/cloud resources, or managed persistence are required"},
		"requires_operator_authorization_for": []string{"opening ports, router/NAT/firewall/DNS/route changes", "installing tunnel, mesh, VPN, service, driver, or persistent helper components", "creating or editing SSH credentials/config", "using paid hosted relay or cloud resources", "turning a temporary third-party session into managed persistence"},
		"forbidden":                           []string{"Agent-authored polling loops", "custom PowerShell or shell relay/bootstrap scripts", "asking humans to assemble ticket/root/gateway/transport/checksum values", "ExecutionPolicy Bypass", "hidden install or persistence"},
		"agent_rule":                          "treat LAN as an opportunistic first path, not the reliability plan; when stable_after_lan_change is false, prefer a configured hosted/relay/mesh/VPN/SSH gateway for durable work before claiming robust connectivity",
	}
}

func connectionSupervision(ticketCode, locale, rdevCommand, gatewayURL string, attemptPolicy, continuityPolicy map[string]any) map[string]any {
	ticketCode = strings.TrimSpace(ticketCode)
	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = "auto"
	}
	rdevCommand = strings.TrimSpace(rdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	gatewayURL = strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
	stableAfterLANChange, _ := continuityPolicy["stable_after_lan_change"].(bool)
	assessment, _ := continuityPolicy["assessment"].(string)
	candidateOrder, _ := attemptPolicy["candidate_order"]
	upgradeRecommended := !stableAfterLANChange
	upgradeReason := "stable hosted/relay/mesh/VPN/SSH fallback already configured"
	if upgradeRecommended {
		upgradeReason = "current Connection Entry has no configured stable fallback beyond direct/LAN/explicit candidates"
	}
	mcpWatchArgs := map[string]any{
		"ticket_code": ticketCode,
		"locale":      locale,
		"wait":        true,
	}
	cliWatchCommand := []string{rdevCommand, "support-session", "status", "--ticket-code", ticketCode, "--wait", "--locale", locale}
	statusRecoveryArgs := map[string]any{"ticket_code": ticketCode, "locale": locale, "wait": true}
	statusRecoveryCommand := []string{rdevCommand, "support-session", "status", "--ticket-code", ticketCode, "--wait", "--locale", locale}
	if gatewayURL != "" {
		mcpWatchArgs["gateway_url"] = gatewayURL
		cliWatchCommand = []string{rdevCommand, "support-session", "status", "--gateway-url", gatewayURL, "--ticket-code", ticketCode, "--wait", "--locale", locale}
		statusRecoveryArgs["gateway_url"] = gatewayURL
		statusRecoveryCommand = []string{rdevCommand, "support-session", "status", "--gateway-url", gatewayURL, "--ticket-code", ticketCode, "--wait", "--locale", locale}
	}
	return map[string]any{
		"schema_version": ConnectionSupervisionSchemaVersion,
		"intent":         "agent-side-watch-report-and-standard-upgrade-contract",
		"ticket_code":    ticketCode,
		"locale":         locale,
		"mcp_watch_call": map[string]any{
			"tool":      "rdev.support_session.status",
			"arguments": mcpWatchArgs,
		},
		"cli_watch_command":       cliWatchCommand,
		"connected_report_rule":   "when status.connected=true, immediately send connected_next_steps.user_report to the user before submitting session tasks",
		"pending_activation_rule": "if status=pending-activation, wait briefly for the standard attended-temporary auto-activation path or create a fresh Connection Entry; do not call retired host lifecycle tools and do not ask the target human for ticket/root/gateway/transport",
		"timeout_recovery_tools": []map[string]any{
			{"tool": "rdev.support_session.status", "arguments": statusRecoveryArgs},
			{"tool": "rdev.support_session.prepare", "arguments": map[string]any{"target": "auto", "build_assets": true}},
		},
		"timeout_recovery_commands": [][]string{
			statusRecoveryCommand,
			{rdevCommand, "support-session", "prepare", "--target", "auto", "--build-assets"},
		},
		"candidate_order":                     candidateOrder,
		"continuity_assessment":               assessment,
		"stable_after_lan_change":             stableAfterLANChange,
		"upgrade_recommended":                 upgradeRecommended,
		"upgrade_reason":                      upgradeReason,
		"standard_upgrade_paths":              []string{"configure RDEV_HOSTED_GATEWAY_URL, RDEV_CLOUDFLARED_NAMED_TUNNEL_URL, RDEV_RELAY_GATEWAY_URL, RDEV_MESH_GATEWAY_URL, RDEV_VPN_GATEWAY_URL, or RDEV_SSH_GATEWAY_URL, then create a fresh Connection Entry", "for operator-owned recurring machines, materialize a reviewed managed Connection Entry package after explicit persistence authorization", "use rdev.connection_entry.plan plus rdev connection-entry run --dry-run for package/runner-based path selection"},
		"automatic_downgrade_boundaries":      []string{"target command owns ordered URL fallback and bounded timeouts", "host transport auto may downgrade WSS to HTTPS long-poll to short polling", "host runtime may reuse signed join-manifest gateway candidates after registration if the current gateway fails before processing session tasks", "status timeout returns connection_recovery for standard recovery"},
		"requires_operator_authorization_for": continuityPolicy["requires_operator_authorization_for"],
		"agent_rule":                          "after sending target_handoff_envelope.full_text, use this supervision contract to wait, report connected=true, and choose standard upgrade/recovery tools; do not write polling, relay, bootstrap, or network mutation scripts",
		"human_surface_rule":                  "humans receive only target_handoff_envelope.full_text; supervision fields are for the Agent runtime",
		"forbidden":                           []string{"custom polling loops", "Agent-authored PowerShell or shell relay/bootstrap scripts", "manual ticket/root/gateway/transport assembly", "ExecutionPolicy Bypass", "hidden install or persistence"},
	}
}

func bootstrapRequirements(target string) map[string]any {
	requirements := map[string]any{
		"schema_version": "rdev.support-session-target-bootstrap-requirements.v1",
		"target":         target,
		"agent_rule":     "support-session connect --start prepares these helper assets automatically; if using an existing gateway, verify the relevant /assets endpoints before sending a platform command",
		"standard_fix":   []string{"rdev support-session prepare --build-assets", "rdev support-session connect --start"},
		"forbidden":      []string{"telling the target user to install rdev manually as the first recovery step", "writing an ad hoc bootstrap downloader", "using ExecutionPolicy Bypass"},
	}
	switch target {
	case "windows":
		requirements["required_assets"] = []string{"rdev-windows-amd64.exe", "rdev-windows-amd64.exe.sha256"}
		requirements["verification"] = []string{"GET /assets/rdev-windows-amd64.exe.sha256 must return 200", "GET /assets/rdev-windows-amd64.exe must return 200"}
	case "macos", "linux":
		requirements["required_assets"] = []string{"matching rdev-<os>-<arch> helper", "matching .sha256"}
		requirements["verification"] = []string{"GET /assets/<helper>.sha256 must return 200", "GET /assets/<helper> must return 200"}
	default:
		requirements["required_assets"] = []string{"matching Windows/macOS/Linux rdev helper", "matching .sha256"}
		requirements["verification"] = []string{"send target_handoff_envelope.full_text verbatim; it includes Windows/macOS/Linux commands plus browser fallback", "verify platform assets before sending a platform-specific terminal command from an existing gateway"}
	}
	return requirements
}

// windowsBootstrapCommand generates the human-facing Windows bootstrap command.
// Keep this intentionally short and readable: real fresh-Agent tests showed
// that long EncodedCommand/loop handoffs are easy for models to corrupt or
// over-explain. The URL still carries signed runtime gateway candidates, so
// rdev can fail over after the bootstrap script is fetched.
func windowsBootstrapCommand(joinURLs []string, runtimeCandidates []GatewayURLCandidate) string {
	urls := quotedPowerShellArrayValues(joinURLs, "/bootstrap.ps1", runtimeCandidates)
	if len(urls) == 0 {
		return ""
	}
	return "powershell -NoProfile -Command \"irm " + urls[0] + " -UseBasicParsing | iex\""
}

func macLinuxBootstrapCommand(joinURLs []string, runtimeCandidates []GatewayURLCandidate) string {
	urls := quotedShellArrayValues(joinURLs, "/bootstrap.sh", runtimeCandidates)
	return "sh -c 'set -eu; t=\"${TMPDIR:-/tmp}/rdev-bootstrap-$$.sh\"; for u in " + strings.Join(urls, " ") + "; do if curl --connect-timeout " + strconv.Itoa(targetHTTPConnectTimeoutSeconds) + " --max-time " + strconv.Itoa(targetHTTPMaxTimeSeconds) + " --retry " + strconv.Itoa(targetHTTPRetries) + " --retry-delay " + strconv.Itoa(targetHTTPRetryDelaySeconds) + " -fsSL \"$u\" -o \"$t\" && sh \"$t\"; then rm -f \"$t\"; exit 0; fi; rm -f \"$t\"; done; echo \"No reachable rdev Connection Entry URL\" >&2; exit 1'"
}

func quotedPowerShellArrayValues(values []string, suffix string, runtimeCandidates []GatewayURLCandidate) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		bootstrapURL := bootstrapURLWithRuntimeCandidates(value+suffix, runtimeCandidates)
		out = append(out, "'"+strings.ReplaceAll(bootstrapURL, "'", "''")+"'")
	}
	return out
}

func quotedShellArrayValues(values []string, suffix string, runtimeCandidates []GatewayURLCandidate) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		out = append(out, shellQuote(bootstrapURLWithRuntimeCandidates(value+suffix, runtimeCandidates)))
	}
	return out
}

func runtimeGatewayCandidates(candidates []GatewayURLCandidate) []GatewayURLCandidate {
	stable := make([]GatewayURLCandidate, 0, len(candidates))
	rest := make([]GatewayURLCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		switch candidate.Kind {
		case "hosted", "relay", "mesh", "vpn", "ssh":
			stable = append(stable, candidate)
		default:
			rest = append(rest, candidate)
		}
	}
	return append(stable, rest...)
}

func bootstrapURLWithRuntimeCandidates(rawURL string, candidates []GatewayURLCandidate) string {
	// Keep human-facing bootstrap URLs short. Gateway candidates are still
	// exposed in machine-readable session payloads and signed manifests, but the
	// copy-paste command must not leak LAN/loopback metadata or become fragile.
	return rawURL
}

func BuildPlan(ctx context.Context, opts Options) map[string]any {
	repoRootInput := strings.TrimSpace(opts.RepoRoot)
	if repoRootInput == "" {
		repoRootInput = "."
	}
	repoRoot, _ := filepath.Abs(repoRootInput)
	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		workDir = filepath.Join(repoRoot, "work", "rdev-support-session")
	}
	workDir, _ = filepath.Abs(workDir)
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	gatewayURL, gatewayCandidates := ResolveGatewayURL(addr, opts.GatewayURL)
	target := strings.TrimSpace(opts.Target)
	if target == "" {
		target = "auto"
	}
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	ttl := opts.TTLSeconds
	if ttl == 0 {
		ttl = 7200
	}
	rdevPath := filepath.Join(workDir, "bin", exeName("rdev", runtime.GOOS))
	windowsRdevPath := filepath.Join(workDir, "bin", "rdev-windows-amd64.exe")
	linuxRdevPath := filepath.Join(workDir, "bin", "rdev-linux-amd64")
	linuxArmRdevPath := filepath.Join(workDir, "bin", "rdev-linux-arm64")
	darwinArmRdevPath := filepath.Join(workDir, "bin", "rdev-darwin-arm64")
	darwinAmdRdevPath := filepath.Join(workDir, "bin", "rdev-darwin-amd64")
	createInviteCommand := []string{
		rdevPath, "invite", "create",
		"--gateway", gatewayURL,
		"--mode", string(model.HostModeAttendedTemporary),
		"--ttl-seconds", strconv.Itoa(ttl),
		"--reason", opts.Reason,
		"--transport", "auto",
	}
	if opts.AutoActivate {
		createInviteCommand = append(createInviteCommand, "--auto-activate")
	}
	helperPreflight := connectivityHelperPreflight()
	inviteBody, _ := json.Marshal(map[string]any{
		"mode":          string(model.HostModeAttendedTemporary),
		"ttl_seconds":   ttl,
		"reason":        opts.Reason,
		"auto_activate": opts.AutoActivate,
		"metadata": map[string]string{
			"connection_entry":    "standard-visible",
			"activation_contract": "target-consent-scoped-ticket",
		},
	})
	return map[string]any{
		"schema_version":                PlanSchemaVersion,
		"ok":                            true,
		"intent":                        "one-command-visible-attended-temporary-connection-entry",
		"repo_root":                     repoRoot,
		"work_dir":                      workDir,
		"target":                        target,
		"locale":                        locale,
		"gateway_url":                   gatewayURL,
		"gateway_url_candidates":        gatewayCandidates,
		"connectivity_helper_preflight": helperPreflight,
		"auto_activate": map[string]any{
			"enabled":        opts.AutoActivate,
			"scope":          "attended-temporary tickets created by this standard plan only",
			"capabilities":   policyCapabilitiesToStrings(policy.TemporaryDefaults()),
			"security_model": "target consent plus signed manifest plus scoped ticket capabilities",
		},
		"commands": map[string]any{
			"prepare_dirs":           []string{"mkdir", "-p", filepath.Join(workDir, "bin"), filepath.Join(workDir, ".rdev", "keys"), filepath.Join(workDir, ".rdev", "gateway"), filepath.Join(workDir, ".rdev", "audit")},
			"build_local_rdev":       []string{"go", "build", "-o", rdevPath, "./cmd/rdev"},
			"build_windows_rdev":     []string{"env", "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", windowsRdevPath, "./cmd/rdev"},
			"build_linux_rdev":       []string{"env", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", linuxRdevPath, "./cmd/rdev"},
			"build_linux_arm64_rdev": []string{"env", "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "go", "build", "-o", linuxArmRdevPath, "./cmd/rdev"},
			"build_macos_arm64_rdev": []string{"env", "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0", "go", "build", "-o", darwinArmRdevPath, "./cmd/rdev"},
			"build_macos_amd64_rdev": []string{"env", "GOOS=darwin", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", darwinAmdRdevPath, "./cmd/rdev"},
			"start_gateway": []string{
				rdevPath, "gateway", "serve", "--dev",
				"--addr", addr,
				"--audit-log", filepath.Join(workDir, ".rdev", "audit", "events.jsonl"),
				"--state", filepath.Join(workDir, ".rdev", "gateway", "state.json"),
				"--signing-key", filepath.Join(workDir, ".rdev", "keys", "gateway-signing-key.json"),
				"--manifest-signing-key", filepath.Join(workDir, ".rdev", "keys", "manifest-root-key.json"),
				"--rdev-windows-amd64", windowsRdevPath,
				"--rdev-linux-amd64", linuxRdevPath,
				"--rdev-linux-arm64", linuxArmRdevPath,
				"--rdev-darwin-arm64", darwinArmRdevPath,
				"--rdev-darwin-amd64", darwinAmdRdevPath,
			},
			"create_invite_http": []string{
				"curl", "-fsS", "-X", "POST", gatewayURL + "/v1/tickets",
				"-H", "Content-Type: application/json",
				"-d", string(inviteBody),
			},
			"create_invite_cli": createInviteCommand,
			"watch_connection_status": []string{
				rdevPath, "support-session", "status",
				"--gateway-url", gatewayURL,
				"--ticket-code", "<ticket-code>",
				"--wait",
				"--locale", locale,
			},
		},
		"target_user_instructions": LocalizedTargetInstructions(gatewayURL, locale),
		"agent_flow": []string{
			"run prepare_dirs and build commands from repo_root",
			"start gateway with the exact start_gateway argv in a managed terminal/session",
			"create the invite through HTTP or CLI",
			"give target user only the localized join URL or one-line visible script",
			"watch connection status with rdev.support_session.status or rdev support-session status --wait",
			"when connected=true, proactively tell the user the connection is established before creating session tasks",
			"do not write ad hoc relay/nohup/bootstrap code",
			"after endpoint connects, continue through session status/events; do not call retired host-activation contracts",
		},
		"forbidden": []string{
			"ExecutionPolicy Bypass",
			"hidden install",
			"unverified binary download",
			"manual ticket/root/gateway/transport assembly for target user",
		},
		"detected_host_capabilities": hostcap.Detect(ctx),
	}
}

func normalizeTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "windows", "win":
		return "windows"
	case "macos", "mac", "darwin":
		return "macos"
	case "linux":
		return "linux"
	default:
		return "auto"
	}
}

func localizedCreatedMessage(locale string) string {
	switch locale {
	case "zh-CN", "zh":
		return "把 target_command 发给目标电脑上的用户执行；然后持续监听状态，连接建立后主动告诉用户。"
	case "ja":
		return "target_command を対象コンピューターのユーザーに渡して実行してもらい、接続状態を監視して、接続後に報告してください。"
	case "ko":
		return "대상 컴퓨터의 사용자에게 target_command를 실행하게 한 뒤 상태를 감시하고 연결되면 알려 주세요."
	case "es":
		return "Entrega target_command a la persona en el equipo de destino, vigila el estado y avisa cuando la conexion quede establecida."
	case "fr":
		return "Donne target_command a la personne sur l'ordinateur cible, surveille l'etat, puis annonce quand la connexion est etablie."
	case "de":
		return "Gib target_command an die Person am Zielcomputer, uberwache den Status und melde, sobald die Verbindung steht."
	case "pt-BR":
		return "Entregue target_command para a pessoa no computador de destino, acompanhe o status e avise quando a conexao estiver estabelecida."
	default:
		return "Give target_command to the target-side human, watch status, and proactively report when the connection is established."
	}
}

func localizedUserHandoffMessage(locale, surface string) string {
	copyHint := "Run this on the target computer and keep the window open while I connect."
	if surface == "join_url" {
		copyHint = "Open this on the target computer and keep the page or terminal open while I connect."
	}
	if surface == "multi_platform" {
		copyHint = "Choose the matching option on the target computer and keep the page or terminal open while I connect."
	}
	switch locale {
	case "zh-CN", "zh":
		if surface == "join_url" {
			return "请在目标电脑上打开下面这个连接。打开后保持页面或终端窗口不要关闭，我会自动等待连接并在连上后告诉你。"
		}
		if surface == "multi_platform" {
			return "请在目标电脑上选择匹配系统的选项执行。执行后保持页面或终端窗口不要关闭，我会自动等待连接并在连上后告诉你。"
		}
		return "请在目标电脑上运行下面这条命令。运行后保持窗口不要关闭，我会自动等待连接并在连上后告诉你。"
	case "ja":
		if surface == "join_url" {
			return "対象コンピューターで次のリンクを開いてください。接続中はページまたはターミナルを開いたままにしてください。接続後に報告します。"
		}
		if surface == "multi_platform" {
			return "対象コンピューターで OS に合う選択肢を実行してください。接続中はページまたはターミナルを開いたままにしてください。接続後に報告します。"
		}
		return "対象コンピューターで次のコマンドを実行してください。接続中はウィンドウを開いたままにしてください。接続後に報告します。"
	case "ko":
		if surface == "join_url" {
			return "대상 컴퓨터에서 아래 링크를 열어 주세요. 연결되는 동안 페이지나 터미널을 닫지 마세요. 연결되면 알려 드립니다."
		}
		if surface == "multi_platform" {
			return "대상 컴퓨터에서 운영체제에 맞는 옵션을 실행해 주세요. 연결되는 동안 페이지나 터미널을 닫지 마세요. 연결되면 알려 드립니다."
		}
		return "대상 컴퓨터에서 아래 명령을 실행해 주세요. 연결되는 동안 창을 닫지 마세요. 연결되면 알려 드립니다."
	case "es":
		if surface == "join_url" {
			return "Abre este enlace en el equipo de destino y deja la pagina o terminal abierta mientras conecto. Avisare cuando este conectado."
		}
		if surface == "multi_platform" {
			return "En el equipo de destino, elige la opcion que coincida con el sistema y deja la pagina o terminal abierta mientras conecto. Avisare cuando este conectado."
		}
		return "Ejecuta este comando en el equipo de destino y deja la ventana abierta mientras conecto. Avisare cuando este conectado."
	case "fr":
		if surface == "join_url" {
			return "Ouvre ce lien sur l'ordinateur cible et garde la page ou le terminal ouvert pendant la connexion. Je te dirai quand c'est connecte."
		}
		if surface == "multi_platform" {
			return "Sur l'ordinateur cible, choisis l'option correspondant au systeme et garde la page ou le terminal ouvert pendant la connexion. Je te dirai quand c'est connecte."
		}
		return "Execute cette commande sur l'ordinateur cible et garde la fenetre ouverte pendant la connexion. Je te dirai quand c'est connecte."
	case "de":
		if surface == "join_url" {
			return "Offne diesen Link auf dem Zielcomputer und lasse die Seite oder das Terminal wahrend der Verbindung offen. Ich melde mich, sobald es verbunden ist."
		}
		if surface == "multi_platform" {
			return "Wahle auf dem Zielcomputer die passende Option fur das Betriebssystem und lasse Seite oder Terminal wahrend der Verbindung offen. Ich melde mich, sobald es verbunden ist."
		}
		return "Fuhre diesen Befehl auf dem Zielcomputer aus und lasse das Fenster wahrend der Verbindung offen. Ich melde mich, sobald es verbunden ist."
	case "pt-BR":
		if surface == "join_url" {
			return "Abra este link no computador de destino e mantenha a pagina ou o terminal aberto enquanto eu conecto. Avisarei quando estiver conectado."
		}
		if surface == "multi_platform" {
			return "No computador de destino, escolha a opcao correspondente ao sistema e mantenha a pagina ou o terminal aberto enquanto eu conecto. Avisarei quando estiver conectado."
		}
		return "Execute este comando no computador de destino e mantenha a janela aberta enquanto eu conecto. Avisarei quando estiver conectado."
	default:
		return copyHint
	}
}

func BuildStatus(opts StatusOptions) map[string]any {
	ticketCode := strings.TrimSpace(opts.TicketCode)
	now := time.Now().UTC()
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	hosts := append([]model.Host(nil), opts.Hosts...)
	active := hostsByStatus(hosts, model.HostStatusActive)
	stale := hostsByStatus(hosts, model.HostStatusStale)
	pending := hostsByStatus(hosts, model.HostStatusPending)
	revoked := hostsByStatus(hosts, model.HostStatusRevoked)
	ticketUsable := opts.Ticket == nil || (opts.Ticket.Status == model.TicketStatusActive && now.Before(opts.Ticket.ExpiresAt))
	bindingValid := opts.Ticket == nil || opts.Session == nil || (opts.Ticket.SessionID == opts.Session.ID && opts.Session.SourceTicketID == opts.Ticket.ID && opts.Session.JoinCode == opts.Ticket.Code)
	targetEndpoints := onlineTargetEndpoints(opts.Session, now)
	if !ticketUsable || !bindingValid {
		targetEndpoints = nil
	}
	sessionID := ""
	if opts.Session != nil {
		sessionID = opts.Session.ID
	}
	recommendedTargetEndpointID := ""
	var recommendedTargetEndpoint *controlplane.Endpoint
	if len(targetEndpoints) > 0 {
		recommendedTargetEndpointID = targetEndpoints[0].ID
		recommendedTargetEndpoint = &targetEndpoints[0]
	}
	connected := ticketUsable && (len(active) > 0 || recommendedTargetEndpointID != "")
	preconnectSummary := targetPreconnectSummary(opts.Preconnects)
	preconnectStatus, _ := preconnectSummary["status"].(string)
	waiting := !connected && len(pending) == 0 && len(stale) == 0 && len(revoked) == 0 && preconnectStatus == ""
	status := "waiting"
	if connected {
		status = "connected"
	} else if len(pending) > 0 {
		status = "pending-activation"
	} else if len(stale) > 0 {
		status = "stale"
	} else if len(revoked) > 0 {
		status = "revoked"
	} else if preconnectStatus != "" {
		status = preconnectStatus
	}
	connectedSessionID := ""
	if recommendedTargetEndpointID != "" {
		connectedSessionID = sessionID
	}
	remoteControlEntry := BuildRemoteControlEntry(RemoteControlEntryOptions{
		GatewayURL:       opts.GatewayURL,
		TicketCode:       ticketCode,
		Ticket:           opts.Ticket,
		Hosts:            hosts,
		Locale:           locale,
		SessionID:        sessionID,
		TargetEndpointID: recommendedTargetEndpointID,
	})
	out := map[string]any{
		"schema_version":                 StatusSchemaVersion,
		"ok":                             connected || len(pending) > 0 || waiting || preconnectStatus != "",
		"ticket_code":                    ticketCode,
		"status":                         status,
		"connected":                      connected,
		"session_id":                     sessionID,
		"recommended_target_endpoint_id": recommendedTargetEndpointID,
		"waiting":                        waiting,
		"feedback":                       localizedStatusFeedback(status, locale),
		"next_action":                    localizedStatusNextAction(status, locale),
		"remote_control_entry":           remoteControlEntry,
		"connected_next_steps": BuildConnectedNextSteps(ConnectedNextStepsOptions{
			Status:           status,
			Hosts:            active,
			Locale:           locale,
			GatewayURL:       opts.GatewayURL,
			SessionID:        connectedSessionID,
			TargetEndpointID: recommendedTargetEndpointID,
			TargetEndpoint:   recommendedTargetEndpoint,
		}),
		"connection_recovery": BuildConnectionRecovery(ConnectionRecoveryOptions{
			Status:     status,
			TicketCode: ticketCode,
			Locale:     locale,
			GatewayURL: opts.GatewayURL,
			TimedOut:   false,
		}),
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:      "status",
			Status:     status,
			TicketCode: ticketCode,
			Locale:     locale,
			GatewayURL: opts.GatewayURL,
			TimedOut:   false,
		}),
		"active_hosts":       active,
		"stale_hosts":        stale,
		"pending_hosts":      pending,
		"revoked_hosts":      revoked,
		"target_preconnects": append([]model.SupportSessionPreconnect(nil), opts.Preconnects...),
		"target_preconnect_count": map[string]int{
			"total": len(opts.Preconnects),
		},
		"host_count": map[string]int{
			"active":  len(active),
			"stale":   len(stale),
			"pending": len(pending),
			"revoked": len(revoked),
			"total":   len(hosts),
		},
	}
	if preconnectSummary != nil {
		out["target_preconnect_summary"] = preconnectSummary
	}
	// Attach ticket expiry when the caller provides the ticket so agents and
	// users can see how much time remains without having to infer it.
	if opts.Ticket != nil {
		remainingSec := int(opts.Ticket.ExpiresAt.Sub(now).Seconds())
		if remainingSec < 0 {
			remainingSec = 0
		}
		out["ticket_expires_at"] = opts.Ticket.ExpiresAt.UTC().Format(time.RFC3339)
		out["ticket_expires_in_seconds"] = remainingSec
		out["ticket_status"] = string(opts.Ticket.Status)
	}
	return out
}

func onlineTargetEndpoints(session *controlplane.Session, now time.Time) []controlplane.Endpoint {
	if session == nil || session.Status == controlplane.SessionStatusClosed || session.Status == controlplane.SessionStatusFailed || session.Status == controlplane.SessionStatusRevoked {
		return nil
	}
	endpoints := make([]controlplane.Endpoint, 0, len(session.Endpoints))
	for _, endpoint := range session.Endpoints {
		if endpoint.Role != controlplane.EndpointRoleTarget {
			continue
		}
		if endpoint.LastSeenAt.IsZero() || endpoint.LastSeenAt.Before(now.Add(-supportSessionEndpointFreshAfter)) {
			continue
		}
		switch endpoint.State {
		case controlplane.EndpointStateOnline, controlplane.EndpointStateBusy, controlplane.EndpointStateDegraded:
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

func targetPreconnectSummary(preconnects []model.SupportSessionPreconnect) map[string]any {
	if len(preconnects) == 0 {
		return nil
	}
	latest := preconnects[0]
	countByPhase := map[string]int{}
	for _, preconnect := range preconnects {
		phase := strings.TrimSpace(preconnect.Phase)
		if phase == "" {
			phase = "started"
		}
		countByPhase[phase]++
		if preconnect.LastSeenAt.After(latest.LastSeenAt) ||
			(preconnect.LastSeenAt.Equal(latest.LastSeenAt) && preconnect.CreatedAt.After(latest.CreatedAt)) {
			latest = preconnect
		}
	}
	status := targetPreconnectStatus(latest.Phase)
	return map[string]any{
		"status":               status,
		"phase":                latest.Phase,
		"latest":               latest,
		"count_by_phase":       countByPhase,
		"agent_interpretation": targetPreconnectAgentInterpretation(status),
	}
}

func targetPreconnectStatus(phase string) string {
	normalized := strings.ToLower(strings.TrimSpace(phase))
	switch {
	case strings.Contains(normalized, "download"):
		return "target-downloading"
	case strings.Contains(normalized, "verify") || strings.Contains(normalized, "checksum"):
		return "target-verifying"
	case strings.Contains(normalized, "starting") ||
		strings.Contains(normalized, "launch") ||
		strings.Contains(normalized, "register"):
		return "target-starting"
	default:
		return "target-preconnect"
	}
}

func targetPreconnectAgentInterpretation(status string) string {
	switch status {
	case "target-downloading":
		return "The target command reached the gateway and is downloading the helper; this is not disconnected or user inaction."
	case "target-verifying":
		return "The target command reached the gateway and is verifying a downloaded helper; this is not disconnected or user inaction."
	case "target-starting":
		return "The target command reached the gateway and is starting or registering the helper; this is not disconnected or user inaction."
	default:
		return "The target command reached the gateway before host registration; this is not disconnected or user inaction."
	}
}

type ConnectedNextStepsOptions struct {
	Status           string
	Hosts            []model.Host
	Locale           string
	GatewayURL       string
	SessionID        string
	TargetEndpointID string
	TargetEndpoint   *controlplane.Endpoint
}

func BuildConnectedNextSteps(opts ConnectedNextStepsOptions) map[string]any {
	sessionID := strings.TrimSpace(opts.SessionID)
	targetEndpointID := strings.TrimSpace(opts.TargetEndpointID)
	hostID := ""
	hostName := ""
	capabilities := []string{}
	if len(opts.Hosts) > 0 {
		host := opts.Hosts[0]
		hostID = host.ID
		hostName = host.Name
		capabilities = append([]string(nil), host.Capabilities...)
	} else if opts.TargetEndpoint != nil {
		hostName = opts.TargetEndpoint.Name
		capabilities = append([]string(nil), opts.TargetEndpoint.Capabilities...)
	}
	connected := opts.Status == "connected" && (hostID != "" || targetEndpointID != "")
	report := "Connection established. I can see the target host and will keep the connector online until you explicitly ask me to disconnect, revoke, or stop it."
	switch opts.Locale {
	case "zh-CN", "zh":
		report = "连接已经建立。我已经看到目标主机，并会保持连接在线，直到你明确要求我断开、撤销或停止。"
	}
	actions := []string{}
	if connected {
		actions = []string{
			"send user_report to the user before creating session tasks",
			"run rdev support-session audit-capabilities against this host before ad-hoc work",
			"use rdev.sessions.task/events/artifacts for the smallest scoped task only after the user's task intent is clear",
			"export or review task evidence before declaring the remote work complete",
			"keep the connector online after the task unless the operator explicitly requests disconnect, revoke, or stop",
		}
	} else {
		report = ""
	}
	var mcpNextCalls any
	var cliNextCommands any
	if connected {
		mcpSessionID := sessionID
		if mcpSessionID == "" && hostID != "" {
			mcpSessionID = "<session-id>"
		}
		mcpNextCalls = connectedNextMCPCalls(mcpSessionID, opts.GatewayURL)
		cliNextCommands = connectedNextCLICommands(hostID, sessionID, targetEndpointID)
	}
	return map[string]any{
		"schema_version":     ConnectedNextStepsSchemaVersion,
		"connected":          connected,
		"host_id":            hostID,
		"host_name":          hostName,
		"session_id":         sessionID,
		"target_endpoint_id": targetEndpointID,
		"capabilities":       capabilities,
		"user_report":        report,
		"agent_next_actions": actions,
		"mcp_next_calls":     mcpNextCalls,
		"cli_next_commands":  cliNextCommands,
		"forbidden": []string{
			"creating a broad shell task before inspecting capabilities",
			"claiming work is complete before reviewing task evidence",
			"asking the user for ticket/root/gateway/transport after connected=true",
			"disconnecting or revoking the support device entry just because a task finished",
		},
	}
}

func connectedNextMCPCalls(sessionID, gatewayURL string) []map[string]any {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	args := map[string]any{
		"session_id": sessionID,
	}
	if gatewayURL = strings.TrimRight(strings.TrimSpace(gatewayURL), "/"); gatewayURL != "" {
		args["gateway_url"] = gatewayURL
	}
	return []map[string]any{
		{
			"tool":      "rdev.sessions.status",
			"arguments": args,
		},
	}
}

func connectedNextCLICommands(hostID, sessionID, targetEndpointID string) [][]string {
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" && strings.TrimSpace(targetEndpointID) != "" {
		return [][]string{
			{"rdev", "support-session", "audit-capabilities", "--gateway-url", "<active-gateway-url>", "--session-id", sessionID, "--target-endpoint-id", targetEndpointID},
			{"rdev", "mcp", "serve", "--gateway-url", "<active-gateway-url>"},
		}
	}
	if hostID = strings.TrimSpace(hostID); hostID == "" {
		return nil
	}
	return [][]string{
		{"rdev", "support-session", "audit-capabilities", "--gateway-url", "<active-gateway-url>", "--host-id", hostID},
		{"rdev", "mcp", "serve", "--gateway-url", "<active-gateway-url>"},
	}
}

type ConnectionRecoveryOptions struct {
	Status     string
	TicketCode string
	Locale     string
	GatewayURL string
	TimedOut   bool
}

func BuildConnectionRecovery(opts ConnectionRecoveryOptions) map[string]any {
	status := strings.TrimSpace(opts.Status)
	if status == "" {
		status = "waiting"
	}
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	ticketCode := strings.TrimSpace(opts.TicketCode)
	agentActions := []string{}
	humanChecks := []string{}
	switch status {
	case "connected":
		agentActions = []string{
			"tell the user the connection is established",
			"inspect host capabilities before creating the smallest scoped session task",
			"review audit and evidence after each task",
		}
	case "pending-activation":
		agentActions = []string{
			"wait briefly for standard attended-temporary auto-activation synchronization",
			"if the endpoint stays pending, create a fresh Connection Entry instead of calling retired host lifecycle tools",
			"do not ask the target-side human for ticket, root, gateway, or transport values",
		}
	case "target-downloading":
		agentActions = []string{
			"treat this as target-side helper download progress or a stalled weak-network transfer, not as user inaction",
			"inspect target_preconnect_summary phase, last_seen_at, asset, source, and seen_count before asking the target-side human for output",
			"prefer standard asset mirror, retry/backoff, or rdev-bootstrap follow-up paths instead of writing ad hoc download scripts",
		}
		humanChecks = []string{
			"keep the visible target-side command running while the helper download continues",
			"copy the visible download error only if the standard command reports a failure",
		}
	case "target-verifying":
		agentActions = []string{
			"treat this as target-side helper verification progress, not as user inaction",
			"inspect target_preconnect_summary and asset checksum evidence before asking the target-side human to retry",
			"prefer standard asset/checksum recovery instead of writing ad hoc verification scripts",
		}
		humanChecks = []string{
			"keep the visible target-side command running while helper verification continues",
			"copy the visible checksum or verification error only if the standard command reports a failure",
		}
	case "target-starting", "target-preconnect":
		agentActions = []string{
			"treat this as target-side bootstrap progress before host registration, not as user inaction",
			"inspect target_preconnect_summary before asking the target-side human to retry",
			"continue using rdev.support_session.status with wait=true instead of writing a polling loop",
		}
		humanChecks = []string{
			"keep the visible target-side command running while registration continues",
			"copy the visible error only if the standard command reports a failure",
		}
	case "revoked":
		agentActions = []string{
			"create a new Connection Entry with rdev.support_session.create or rdev support-session connect --start",
			"send only the new user_handoff message plus copy_paste value to the human",
			"do not reuse old ticket, manifest root, gateway, or transport metadata",
		}
	default:
		agentActions = []string{
			"keep using rdev.support_session.status with wait=true or rdev support-session status --wait",
			"if the original target_handoff_envelope is still valid, resend target_handoff_envelope.full_text verbatim",
			"if the target cannot reach the join URL or every gateway candidate times out, run rdev.support_session.prepare or rdev support-session prepare --build-assets, then create a fresh Connection Entry",
			"use gateway_candidate_preflight, connection_attempt_policy, runner_plan, or standard_recovery fields to choose the next standard rdev path instead of writing ad hoc network scripts",
		}
		humanChecks = []string{
			"keep the visible target-side command window open",
			"open the Connection Entry join URL on the target machine if the terminal command failed",
			"copy the visible error output only when the standard command reports a failure",
		}
	}
	if status != "waiting" && status != "" {
		humanChecks = append(humanChecks, "do not copy raw ticket, root key, gateway, transport, release, or checksum values")
	}
	reason := "standard status-specific recovery"
	if opts.TimedOut {
		reason = "standard wait timeout recovery"
		if status == "waiting" {
			agentActions = append([]string{
				"treat this as a connection-entry failure to reach or run, not as permission to improvise scripts",
			}, agentActions...)
		}
	}
	return map[string]any{
		"schema_version": ConnectionRecoverySchemaVersion,
		"status":         status,
		"ticket_code":    ticketCode,
		"timed_out":      opts.TimedOut,
		"reason":         reason,
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:      "recovery",
			Status:     status,
			TicketCode: ticketCode,
			Locale:     locale,
			GatewayURL: opts.GatewayURL,
			TimedOut:   opts.TimedOut,
		}),
		"agent_rule": localizedConnectionRecoveryAgentRule(locale),
		"agent_next_actions": append(agentActions, []string{
			"ask one short question only for authorization, persistence authorization, privileged network changes, paid/cloud relay use, credentials, or unclear ownership",
		}...),
		"human_checks": humanChecks,
		"standard_tools": []string{
			"rdev.support_session.status",
			"rdev support-session status --wait",
			"rdev.support_session.prepare",
			"rdev support-session prepare --build-assets",
			"rdev.support_session.create",
			"rdev support-session connect --start",
			"rdev support-session start",
			"rdev.connection_entry.plan",
			"rdev connection-entry run --dry-run",
			"rdev.sessions.status",
			"rdev.sessions.events",
			"rdev.sessions.task",
			"rdev.sessions.interrupt",
			"rdev task policy-template",
		},
		"forbidden": []string{
			"Agent-authored PowerShell or shell relay scripts",
			"manual ticket/root/gateway/transport assembly for target humans",
			"ExecutionPolicy Bypass",
			"hidden install",
			"UAC or sudo bypass",
			"firewall, DNS, route, service, driver, credential, paid relay, or cloud changes without explicit authorization",
		},
	}
}

func MarkStatusTimedOut(status map[string]any, ticketCode, locale string) map[string]any {
	if status == nil {
		status = map[string]any{}
	}
	status["ok"] = false
	status["timed_out"] = true
	statusText, _ := status["status"].(string)
	status["next_action"] = localizedTimedOutStatusNextAction(statusText, locale)
	gatewayURL := ""
	if entry, ok := status["remote_control_entry"].(map[string]any); ok {
		gatewayURL, _ = entry["gateway_url"].(string)
	}
	status["connection_recovery"] = BuildConnectionRecovery(ConnectionRecoveryOptions{
		Status:     statusText,
		TicketCode: ticketCode,
		Locale:     locale,
		GatewayURL: gatewayURL,
		TimedOut:   true,
	})
	return status
}

func localizedTimedOutStatusNextAction(status, locale string) string {
	switch locale {
	case "zh-CN", "zh":
		switch status {
		case "target-downloading":
			return "下载仍在进行或已在弱网中停滞；继续等待，检查 target_preconnect_summary 的时间、资产和镜像策略，不要误判为目标端未执行。"
		case "target-verifying":
			return "校验仍在进行或已停滞；检查 target_preconnect_summary、校验和和资产镜像，不要让用户重新拼接命令。"
		case "target-starting", "target-preconnect":
			return "目标端 bootstrap 已触达网关但尚未完成注册；继续等待并检查 target_preconnect_summary，不要编写临时轮询脚本。"
		}
	default:
		switch status {
		case "target-downloading":
			return "The helper download is still in progress or stalled on a weak network; keep waiting, inspect target_preconnect_summary timestamps, asset, and mirror strategy, and do not treat this as the target command not running."
		case "target-verifying":
			return "Helper verification is still in progress or stalled; inspect target_preconnect_summary, checksums, and asset mirrors instead of asking the user to reassemble commands."
		case "target-starting", "target-preconnect":
			return "The target bootstrap reached the gateway but has not completed registration; keep waiting and inspect target_preconnect_summary instead of writing ad hoc polling scripts."
		}
	}
	return "Keep waiting, or check gateway reachability, network path, and target command output."
}

func LocalizedTargetInstructions(gatewayURL, locale string) map[string]any {
	windows := "powershell -NoProfile -Command \"irm '" + gatewayURL + "/join/<ticket-code>/bootstrap.ps1' | iex\""
	macLinux := "curl -fsSL " + gatewayURL + "/join/<ticket-code>/bootstrap.sh | sh"
	labels := map[string]string{
		"auto":  "Open this visible support command on the target computer. Keep the terminal open while the Agent works.",
		"en":    "Open this visible support command on the target computer. Keep the terminal open while the Agent works.",
		"zh-CN": "在目标电脑上运行这条可见的支持命令。Agent 工作期间请保持终端窗口打开。",
		"ja":    "対象コンピューターでこの表示されるサポートコマンドを実行し、Agent の作業中はターミナルを開いたままにしてください。",
		"ko":    "대상 컴퓨터에서 이 표시되는 지원 명령을 실행하고 Agent가 작업하는 동안 터미널을 열어 두세요.",
		"es":    "Ejecuta este comando visible de soporte en el equipo de destino y deja la terminal abierta mientras trabaja el Agent.",
		"fr":    "Executez cette commande d'assistance visible sur l'ordinateur cible et gardez le terminal ouvert pendant que l'Agent travaille.",
		"de":    "Fuhre diesen sichtbaren Support-Befehl auf dem Zielcomputer aus und lasse das Terminal offen, wahrend der Agent arbeitet.",
		"pt-BR": "Execute este comando visivel de suporte no computador de destino e mantenha o terminal aberto enquanto o Agent trabalha.",
	}
	message, ok := labels[locale]
	if !ok {
		message = labels["en"]
	}
	return map[string]any{
		"message":             message,
		"windows":             windows,
		"macos_linux":         macLinux,
		"join_url_template":   gatewayURL + "/join/<ticket-code>",
		"human_receives_only": []string{"localized join URL", "visible one-line script", "or signed package when published"},
	}
}

func hostsByStatus(hosts []model.Host, status model.HostStatus) []model.Host {
	values := make([]model.Host, 0, len(hosts))
	for _, host := range hosts {
		if host.Status == status {
			values = append(values, host)
		}
	}
	return values
}

func localizedStatusFeedback(status, locale string) string {
	switch locale {
	case "zh-CN", "zh":
		switch status {
		case "connected":
			return "连接已经建立，目标主机已在线并可用于受控任务。"
		case "pending-activation":
			return "目标主机已经出现，正在等待审批或自动批准完成。"
		case "target-downloading":
			return "目标端命令已经触达网关，正在下载 rdev helper；这通常是弱网或大包体导致的等待。"
		case "target-verifying":
			return "目标端命令已经触达网关，正在校验下载的 rdev helper。"
		case "target-starting":
			return "目标端命令已经触达网关，正在启动或注册 rdev helper。"
		case "target-preconnect":
			return "目标端命令已经触达网关，但还没有完成主机注册。"
		case "revoked":
			return "连接票据或主机已经撤销。"
		default:
			return "还没有检测到目标主机连接，请确认目标机器上的可见命令仍在运行。"
		}
	default:
		switch status {
		case "connected":
			return "Connection established. The target host is online and ready for scoped work."
		case "pending-activation":
			return "The target host has appeared and is waiting for standard auto-activation to complete."
		case "target-downloading":
			return "The target command reached the gateway and is downloading the rdev helper; this is usually weak-network or large-asset wait time."
		case "target-verifying":
			return "The target command reached the gateway and is verifying the downloaded rdev helper."
		case "target-starting":
			return "The target command reached the gateway and is starting or registering the rdev helper."
		case "target-preconnect":
			return "The target command reached the gateway but has not completed host registration yet."
		case "revoked":
			return "The connection ticket or host has been revoked."
		default:
			return "No target host is connected yet. Keep the visible command running on the target machine."
		}
	}
}

func localizedStatusNextAction(status, locale string) string {
	switch locale {
	case "zh-CN", "zh":
		switch status {
		case "connected":
			return "向用户汇报连接已建立，然后检查主机能力并创建最小权限任务。"
		case "pending-activation":
			return "继续等待标准自动激活短暂同步；如果仍停留，创建新的 Connection Entry。"
		case "target-downloading":
			return "继续等待下载完成；不要误判为目标端未执行。必要时检查 target_preconnect_summary 和网络/镜像策略。"
		case "target-verifying":
			return "继续等待校验完成；如果长时间停留，检查 helper 校验和与资产镜像。"
		case "target-starting":
			return "继续等待主机注册完成；如果长时间停留，检查目标端可见窗口输出。"
		case "target-preconnect":
			return "继续等待下一阶段 preconnect 或主机注册；如果没有进展，再查看目标端输出。"
		case "revoked":
			return "创建新的 Connection Entry。"
		default:
			return "继续等待，或检查 gateway 地址、网络可达性和目标命令输出。"
		}
	default:
		switch status {
		case "connected":
			return "Tell the user the connection is established, then inspect capabilities and create the smallest scoped session task."
		case "pending-activation":
			return "Wait briefly for standard auto-activation; create a fresh Connection Entry if the endpoint stays pending."
		case "target-downloading":
			return "Keep waiting for the download to finish; do not misdiagnose this as the target command not running. Inspect target_preconnect_summary and network or mirror strategy if it stalls."
		case "target-verifying":
			return "Keep waiting for helper verification; if it stalls, inspect checksums and asset mirrors."
		case "target-starting":
			return "Keep waiting for host registration; if it stalls, inspect the visible target-side output."
		case "target-preconnect":
			return "Keep waiting for the next preconnect phase or host registration; inspect target output only if there is no progress."
		case "revoked":
			return "Create a new Connection Entry."
		default:
			return "Keep waiting, or check gateway reachability, network path, and target command output."
		}
	}
}

func localizedConnectionRecoveryAgentRule(locale string) string {
	switch locale {
	case "zh-CN", "zh":
		return "使用返回的 Connection Entry、user_handoff、status、prepare、runner 字段恢复连接；不要编写临时 PowerShell、shell、relay、审批轮询或 bootstrap 代码，也不要让人类拼 ticket/root/gateway/transport。"
	default:
		return "Recover connection setup through returned Connection Entry, user_handoff, status, prepare, and runner fields; do not write ad hoc PowerShell, shell, relay, activation-polling, or bootstrap code, and do not ask humans to assemble ticket/root/gateway/transport values."
	}
}

func exeName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func policyCapabilitiesToStrings(caps []policy.Capability) []string {
	values := make([]string, 0, len(caps))
	for _, cap := range caps {
		values = append(values, string(cap))
	}
	return values
}

type supportSessionAssetSpec struct {
	ID     string
	Name   string
	GOOS   string
	GOARCH string
	Path   string
}

func supportSessionAssetSpecs(binDir string) []supportSessionAssetSpec {
	return []supportSessionAssetSpec{
		{ID: "windows-amd64", Name: "rdev-windows-amd64.exe", GOOS: "windows", GOARCH: "amd64", Path: filepath.Join(binDir, "rdev-windows-amd64.exe")},
		{ID: "darwin-arm64", Name: "rdev-darwin-arm64", GOOS: "darwin", GOARCH: "arm64", Path: filepath.Join(binDir, "rdev-darwin-arm64")},
		{ID: "darwin-amd64", Name: "rdev-darwin-amd64", GOOS: "darwin", GOARCH: "amd64", Path: filepath.Join(binDir, "rdev-darwin-amd64")},
		{ID: "linux-amd64", Name: "rdev-linux-amd64", GOOS: "linux", GOARCH: "amd64", Path: filepath.Join(binDir, "rdev-linux-amd64")},
		{ID: "linux-arm64", Name: "rdev-linux-arm64", GOOS: "linux", GOARCH: "arm64", Path: filepath.Join(binDir, "rdev-linux-arm64")},
	}
}

func supportSessionAssetURL(gatewayURL, assetName, suffix string) string {
	base := strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
	return base + "/assets/" + assetName + suffix
}

func applySupportSessionAssetDownloadEvidence(report map[string]any, path string) {
	sizeBytes, err := fileSizeBytes(path)
	if err != nil {
		report["size_error"] = err.Error()
		return
	}
	report["size_bytes"] = sizeBytes

	gzipBytes, err := gzipFileSizeBytes(path)
	if err != nil {
		report["gzip_estimate_error"] = err.Error()
		return
	}
	report["gzip_estimated_bytes"] = gzipBytes
	report["gzip_budget_bytes"] = supportSessionHelperGzipBudgetBytes
	report["gzip_within_budget"] = gzipBytes <= supportSessionHelperGzipBudgetBytes
	report["bootstrap_target_bytes"] = supportSessionBootstrapTargetBytes
	report["bootstrap_target_met"] = gzipBytes <= supportSessionBootstrapTargetBytes
	if sizeBytes > 0 {
		report["compression_ratio"] = float64(gzipBytes) / float64(sizeBytes)
	}
}

func supportSessionAssetDownloadSummary(assetReports []map[string]any) map[string]any {
	allGzipWithinBudget := true
	bootstrapConnectorRecommended := false
	var maxGzipBytes int64
	hasGzipEvidence := false
	for _, report := range assetReports {
		withinBudget, ok := report["gzip_within_budget"].(bool)
		if !ok || !withinBudget {
			allGzipWithinBudget = false
		}
		if bootstrapTargetMet, ok := report["bootstrap_target_met"].(bool); ok && !bootstrapTargetMet {
			bootstrapConnectorRecommended = true
		}
		if gzipBytes, ok := report["gzip_estimated_bytes"].(int64); ok {
			hasGzipEvidence = true
			if gzipBytes > maxGzipBytes {
				maxGzipBytes = gzipBytes
			}
		}
	}
	return map[string]any{
		"all_gzip_within_budget":                        allGzipWithinBudget,
		"bootstrap_connector_recommended":               bootstrapConnectorRecommended,
		"first_connect_size_strategy":                   "serve gzip-compressed full helper assets now; use rdev-bootstrap as the next connector architecture when compressed helpers exceed the 1 MB first-connect target",
		"default_first_connect_surface":                 "script-preconnect",
		"default_runner_download_kind":                  "gzip-full-helper",
		"first_task_requires_full_helper":               true,
		"publishes_native_first_connect_asset":          false,
		"default_full_helper_gzip_estimated_max_bytes":  maxGzipBytes,
		"default_full_helper_meets_bootstrap_target":    hasGzipEvidence && maxGzipBytes <= supportSessionBootstrapTargetBytes,
		"native_first_connect_asset":                    nativeFirstConnectAssetReport(),
		"default_first_connect_agent_interpretation":    "the generated bootstrap script reaches preconnect first, then downloads a gzip-compressed full helper before any session tasks can run",
		"native_first_connect_asset_publication_policy": "publish rdev-bootstrap as a default first-connect asset only after its compressed release artifact is measured under first_connect_target_bytes",
	}
}

func nativeFirstConnectAssetReport() map[string]any {
	return map[string]any{
		"schema_version": "rdev.support-session-native-first-connect-asset.v1",
		"name":           "rdev-bootstrap",
		"published":      false,
		"measured":       false,
		"target_bytes":   supportSessionBootstrapTargetBytes,
		"target_met":     false,
		"reason":         "rdev-bootstrap is not published by support-session assets, so the default first-connect path must be treated as script preconnect plus full-helper download",
	}
}

func recommendedSupportSessionNextStep(localRdevUsable, allAssetsReady bool) string {
	switch {
	case localRdevUsable && allAssetsReady:
		return "run rdev support-session connect --start and give the generated target_command to the target-side human"
	case allAssetsReady:
		return "use the current rdev executable or checkout command to run support-session connect --start; target-side self-repair assets are ready"
	default:
		return "prepare helper assets from a valid checkout before relying on one-command targets without preinstalled rdev"
	}
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func findRepoRoot(start string) string {
	if strings.TrimSpace(start) == "" {
		return ""
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	info, err := os.Stat(dir)
	if err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if pathExists(filepath.Join(dir, "go.mod")) && pathExists(filepath.Join(dir, "cmd", "rdev", "main.go")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func fileSizeBytes(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory", path)
	}
	return info.Size(), nil
}

type countingWriter struct {
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

func gzipFileSizeBytes(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	counter := &countingWriter{}
	zw := gzip.NewWriter(counter)
	_, copyErr := io.Copy(zw, file)
	closeErr := zw.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	return counter.n, nil
}

// copyFile copies src to dst, creating parent directories as needed.
// It sets the executable bit on non-Windows platforms.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func fileSHA256Hex(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func tailString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
