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
	HostCommand         string            `json:"host_command"`
	HumanNextActions    []string          `json:"human_next_actions"`
	AgentNextActions    []string          `json:"agent_next_actions"`
	MCPTools            map[string]string `json:"mcp_tools"`
	RequiresHumanAction []string          `json:"requires_human_action"`
	CreatedAt           time.Time         `json:"created_at"`
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
		transport = "wss"
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	createdAt := opts.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	hostCommand := fmt.Sprintf("%s host serve --manifest-url %s --transport %s", rdevCommand, shellQuote(manifestURL), shellQuote(transport))
	if opts.Once {
		hostCommand += " --once"
	}

	agentActions := []string{
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
		HostCommand:      hostCommand,
		HumanNextActions: []string{"Run host_command on the target machine that needs help."},
		AgentNextActions: agentActions,
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>*?()[]{}!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
