package agentinvite

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const SchemaVersion = "rdev.agent-invite.v1"

type Options struct {
	GatewayURL          string
	JoinURL             string
	ManifestURL         string
	Ticket              model.Ticket
	Transport           string
	Once                bool
	RequireHostApproval bool
	RdevCommand         string
	CreatedAt           time.Time
}

type Invite struct {
	SchemaVersion       string            `json:"schema_version"`
	GatewayURL          string            `json:"gateway_url"`
	JoinURL             string            `json:"join_url"`
	ManifestURL         string            `json:"manifest_url"`
	Ticket              model.Ticket      `json:"ticket"`
	Transport           string            `json:"transport"`
	TransportPlan       TransportPlan     `json:"transport_plan"`
	HostCommand         string            `json:"host_command"`
	FallbackCommands    []string          `json:"fallback_commands"`
	HumanNextActions    []string          `json:"human_next_actions"`
	AgentNextActions    []string          `json:"agent_next_actions"`
	ConnectivityChecks  []string          `json:"connectivity_checks"`
	MCPTools            map[string]string `json:"mcp_tools"`
	RequiresHumanAction []string          `json:"requires_human_action"`
	CreatedAt           time.Time         `json:"created_at"`
}

type TransportPlan struct {
	SchemaVersion string               `json:"schema_version"`
	Mode          string               `json:"mode"`
	Candidates    []TransportCandidate `json:"candidates"`
	Notes         []string             `json:"notes"`
}

type TransportCandidate struct {
	Transport    string   `json:"transport"`
	Priority     int      `json:"priority"`
	Reason       string   `json:"reason"`
	HostCommand  string   `json:"host_command"`
	FailureHints []string `json:"failure_hints,omitempty"`
}

func New(opts Options) (Invite, error) {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		return Invite{}, fmt.Errorf("gateway URL is required")
	}
	if _, err := url.ParseRequestURI(gatewayURL); err != nil {
		return Invite{}, fmt.Errorf("invalid gateway URL: %w", err)
	}
	manifestURL := strings.TrimSpace(opts.ManifestURL)
	if manifestURL == "" {
		manifestURL = defaultManifestURL(gatewayURL, opts.Ticket.Code)
	}
	if _, err := url.ParseRequestURI(manifestURL); err != nil {
		return Invite{}, fmt.Errorf("invalid manifest URL: %w", err)
	}
	joinURL := strings.TrimSpace(opts.JoinURL)
	if joinURL == "" {
		joinURL = gatewayURL + "/join/" + opts.Ticket.Code
	}
	if _, err := url.ParseRequestURI(joinURL); err != nil {
		return Invite{}, fmt.Errorf("invalid join URL: %w", err)
	}
	transport := strings.TrimSpace(opts.Transport)
	if transport == "" {
		transport = "auto"
	}
	if !validTransport(transport) {
		return Invite{}, fmt.Errorf("unsupported transport %q", transport)
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	createdAt := opts.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	hostCommand := hostServeCommand(rdevCommand, manifestURL, transport, opts.Once)
	transportPlan := newTransportPlan(rdevCommand, manifestURL, transport, opts.Once)
	fallbackCommands := fallbackCommandsFromPlan(transportPlan, transport)

	agentActions := []string{
		"Probe the local runtime with rdev doctor and inspect configured gateway reachability before asking the human to run the host command.",
		"Poll rdev.hosts.list with status=pending or active until the target host appears.",
		"Ask the human operator to approve the host if it is pending; then call rdev.hosts.approve with ticket-scoped capabilities.",
		"Create a job with rdev.jobs.create only after the host is active and the requested policy is explained.",
		"Read rdev.jobs.status and export evidence before reporting completion.",
	}
	if !opts.RequireHostApproval {
		agentActions[1] = "If the host is pending, request approval; if it is already active, continue to job policy explanation."
	}

	return Invite{
		SchemaVersion:    SchemaVersion,
		GatewayURL:       gatewayURL,
		JoinURL:          joinURL,
		ManifestURL:      manifestURL,
		Ticket:           opts.Ticket,
		Transport:        transport,
		TransportPlan:    transportPlan,
		HostCommand:      hostCommand,
		FallbackCommands: fallbackCommands,
		HumanNextActions: []string{
			"Run host_command on the target machine that needs help.",
			"If the target network blocks WebSocket traffic, use the first fallback command that the Agent recommends.",
		},
		AgentNextActions: agentActions,
		ConnectivityChecks: []string{
			"Confirm the gateway URL is reachable from the Agent environment before creating jobs.",
			"Ask the target-side human to run host_command; the host connects outbound, so no inbound port should be opened on the target machine.",
			"If the host does not appear, ask for target-side network symptoms: proxy requirement, TLS interception, blocked outbound 443, DNS failure, captive portal, or VPN requirement.",
			"Prefer auto transport. It tries WSS first and falls back to HTTPS long-poll, then short polling for restrictive networks.",
			"Use mTLS inputs only when the operator provides CA and client certificate material; never invent certificate paths.",
		},
		MCPTools: map[string]string{
			"list_hosts":    "rdev.hosts.list",
			"approve_host":  "rdev.hosts.approve",
			"create_job":    "rdev.jobs.create",
			"job_status":    "rdev.jobs.status",
			"export_bundle": "rdev.artifacts.export_bundle",
		},
		RequiresHumanAction: []string{"target-host-consent", "operator-approval-when-required"},
		CreatedAt:           createdAt,
	}, nil
}

func defaultManifestURL(gatewayURL, ticketCode string) string {
	if strings.HasSuffix(gatewayURL, "/v1") {
		return gatewayURL + "/tickets/" + ticketCode + "/manifest"
	}
	return gatewayURL + "/v1/tickets/" + ticketCode + "/manifest"
}

func validTransport(transport string) bool {
	switch transport {
	case "auto", "wss", "long-poll", "poll":
		return true
	default:
		return false
	}
}

func newTransportPlan(rdevCommand, manifestURL, transport string, once bool) TransportPlan {
	transports := []string{transport}
	if transport == "auto" {
		transports = []string{"wss", "long-poll", "poll"}
	}
	candidates := make([]TransportCandidate, 0, len(transports))
	for i, candidate := range transports {
		candidates = append(candidates, TransportCandidate{
			Transport:    candidate,
			Priority:     i + 1,
			Reason:       transportReason(candidate),
			HostCommand:  hostServeCommand(rdevCommand, manifestURL, candidate, once),
			FailureHints: transportFailureHints(candidate),
		})
	}
	return TransportPlan{
		SchemaVersion: "rdev.transport-plan.v1",
		Mode:          transport,
		Candidates:    candidates,
		Notes: []string{
			"All candidates are outbound target-host connections; target networks do not need inbound firewall rules.",
			"Transport only carries signed work and evidence; host-side policy still authorizes every job.",
			"Use relay, VPN, or mesh only as an operator-approved connectivity assist for owned hosts, never as job authorization.",
		},
	}
}

func fallbackCommandsFromPlan(plan TransportPlan, selected string) []string {
	if selected != "auto" {
		return nil
	}
	values := make([]string, 0, len(plan.Candidates)-1)
	for _, candidate := range plan.Candidates {
		if candidate.Transport == "wss" {
			continue
		}
		values = append(values, candidate.HostCommand)
	}
	return values
}

func hostServeCommand(rdevCommand, manifestURL, transport string, once bool) string {
	command := fmt.Sprintf("%s host serve --manifest-url %s --transport %s", rdevCommand, shellQuote(manifestURL), shellQuote(transport))
	if once {
		command += " --once"
	}
	return command
}

func transportReason(transport string) string {
	switch transport {
	case "wss":
		return "Preferred low-latency outbound WebSocket channel over HTTPS/TLS."
	case "long-poll":
		return "HTTPS-compatible fallback for proxies and firewalls that block WebSocket upgrades."
	case "poll":
		return "Maximum-compatibility HTTPS fallback when long-held requests are interrupted."
	default:
		return "Automatically tries the best available outbound transport."
	}
}

func transportFailureHints(transport string) []string {
	switch transport {
	case "wss":
		return []string{"websocket upgrade blocked", "TLS interception", "proxy requires explicit configuration", "outbound 443 blocked"}
	case "long-poll":
		return []string{"long-held HTTPS requests interrupted", "proxy timeout too short", "captive portal or VPN required"}
	case "poll":
		return []string{"DNS failure", "gateway unreachable", "certificate trust failure", "operator auth or enrollment rejected"}
	default:
		return nil
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
