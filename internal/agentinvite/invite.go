package agentinvite

import (
	"fmt"
	"net"
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
	NetworkScope        string
	AuthorityProfile    string
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
	ConnectionPlan      ConnectionPlan    `json:"connection_plan"`
	AuthorityProfile    AuthorityProfile  `json:"authority_profile"`
	CustomerBootstrap   CustomerBootstrap `json:"customer_bootstrap"`
	HostContextPlan     HostContextPlan   `json:"host_context_plan"`
	ProvisioningPlan    ProvisioningPlan  `json:"agent_provisioning_plan"`
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

type ConnectionPlan struct {
	SchemaVersion       string               `json:"schema_version"`
	NetworkScope        string               `json:"network_scope"`
	GatewayReachability string               `json:"gateway_reachability"`
	Implemented         []ConnectionProtocol `json:"implemented"`
	AgentManaged        []ConnectionProtocol `json:"agent_managed"`
	DiscoveryPlan       DiscoveryPlan        `json:"discovery_plan"`
	SelectionOrder      []string             `json:"selection_order"`
	EnvironmentProbes   []string             `json:"environment_probes"`
	AgentRules          []string             `json:"agent_rules"`
}

type ConnectionProtocol struct {
	ID            string   `json:"id"`
	Status        string   `json:"status"`
	BestFor       string   `json:"best_for"`
	Requirements  []string `json:"requirements"`
	SecurityModel string   `json:"security_model"`
	AgentAction   string   `json:"agent_action"`
}

type DiscoveryPlan struct {
	SchemaVersion string   `json:"schema_version"`
	Allowed       []string `json:"allowed"`
	Boundaries    []string `json:"boundaries"`
	StopWhen      []string `json:"stop_when"`
}

type AuthorityProfile struct {
	SchemaVersion        string               `json:"schema_version"`
	Profile              string               `json:"profile"`
	RemoteHostRole       string               `json:"remote_host_role"`
	Discovery            AuthorityScope       `json:"discovery"`
	DownstreamControl    AuthorityScope       `json:"downstream_control"`
	AutonomousActions    []string             `json:"autonomous_actions"`
	RequiredCapabilities []string             `json:"required_capabilities"`
	EvidenceRequired     []string             `json:"evidence_required"`
	StopConditions       []string             `json:"stop_conditions"`
	ControlPaths         []ConnectionProtocol `json:"control_paths"`
}

type AuthorityScope struct {
	Allowed      bool     `json:"allowed"`
	Scope        string   `json:"scope"`
	Description  string   `json:"description"`
	Requirements []string `json:"requirements"`
}

type CustomerBootstrap struct {
	SchemaVersion          string            `json:"schema_version"`
	CustomerLink           string            `json:"customer_link"`
	ConsentMode            string            `json:"consent_mode"`
	AutomationLevel        string            `json:"automation_level"`
	OneLineCommands        map[string]string `json:"one_line_commands"`
	InstallPrerequisites   []string          `json:"install_prerequisites"`
	CustomerSteps          []string          `json:"customer_steps"`
	AgentAfterConnect      []string          `json:"agent_after_connect"`
	RevocationInstructions []string          `json:"revocation_instructions"`
}

type HostContextPlan struct {
	SchemaVersion         string   `json:"schema_version"`
	StorageLocation       string   `json:"storage_location"`
	ServerContextBudget   string   `json:"server_context_budget"`
	ProgressiveDisclosure []string `json:"progressive_disclosure"`
	HostLocalStores       []string `json:"host_local_stores"`
	GatewayIndexes        []string `json:"gateway_indexes"`
	AgentRules            []string `json:"agent_rules"`
	RedactionRules        []string `json:"redaction_rules"`
}

type ProvisioningPlan struct {
	SchemaVersion       string   `json:"schema_version"`
	Mode                string   `json:"mode"`
	DiscoveryTargets    []string `json:"discovery_targets"`
	AutoInstallAllowed  []string `json:"auto_install_allowed"`
	InstallScopes       []string `json:"install_scopes"`
	ApprovalRequiredFor []string `json:"approval_required_for"`
	PreferredSources    []string `json:"preferred_sources"`
	AgentRules          []string `json:"agent_rules"`
	EvidenceRequired    []string `json:"evidence_required"`
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
	networkScope := strings.TrimSpace(opts.NetworkScope)
	if networkScope == "" {
		networkScope = "auto"
	}
	if !validNetworkScope(networkScope) {
		return Invite{}, fmt.Errorf("unsupported network scope %q", networkScope)
	}
	authorityProfile := strings.TrimSpace(opts.AuthorityProfile)
	if authorityProfile == "" {
		authorityProfile = "max-control"
	}
	if !validAuthorityProfile(authorityProfile) {
		return Invite{}, fmt.Errorf("unsupported authority profile %q", authorityProfile)
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
	connectionPlan := newConnectionPlan(gatewayURL, networkScope)
	authority := newAuthorityProfile(authorityProfile)
	customerBootstrap := newCustomerBootstrap(joinURL)
	hostContextPlan := newHostContextPlan()
	provisioningPlan := newProvisioningPlan()
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
		SchemaVersion:     SchemaVersion,
		GatewayURL:        gatewayURL,
		JoinURL:           joinURL,
		ManifestURL:       manifestURL,
		Ticket:            opts.Ticket,
		Transport:         transport,
		TransportPlan:     transportPlan,
		ConnectionPlan:    connectionPlan,
		AuthorityProfile:  authority,
		CustomerBootstrap: customerBootstrap,
		HostContextPlan:   hostContextPlan,
		ProvisioningPlan:  provisioningPlan,
		HostCommand:       hostCommand,
		FallbackCommands:  fallbackCommands,
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
			"If the Agent and target are on the same LAN, probe scoped local/LAN reachability and prefer a LAN-reachable gateway URL before relay, mesh, or SSH.",
			"Use mTLS, SSH, mesh, relay, or proxy inputs automatically when they are already configured in the environment; ask only when required values cannot be discovered.",
			"When authority_profile permits downstream control, the connected host may act as the field workstation for discovering and controlling other reachable authorized devices.",
			"Keep remote environment, project structure, requirement notes, and large evidence on the host; load only indexed slices or artifact references into the Agent server context.",
			"Let the host detect missing skills, MCP tools, adapters, language runtimes, package managers, and project dependencies; install user-scoped missing pieces automatically when policy allows, and ask before elevation or external mutation.",
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

func newHostContextPlan() HostContextPlan {
	return HostContextPlan{
		SchemaVersion:       "rdev.host-context-plan.v1",
		StorageLocation:     "remote-host-first",
		ServerContextBudget: "index-and-on-demand-slices",
		ProgressiveDisclosure: []string{
			"start with host summary, workspace roots, adapter inventory, and active task intent",
			"request file tree slices only for the selected workspace and depth",
			"request environment/configuration details only when needed for the current job",
			"request file contents, logs, command output, and evidence artifacts by explicit path, query, or artifact id",
			"evict or summarize stale slices after the job step is complete",
		},
		HostLocalStores: []string{
			"environment probes",
			"workspace indexes",
			"project file tree snapshots",
			"requirement and task notes",
			"adapter transcripts",
			"large logs and evidence artifacts",
		},
		GatewayIndexes: []string{
			"host id and status",
			"workspace root handles",
			"artifact ids, checksums, sizes, and redaction metadata",
			"context slice ids and freshness timestamps",
		},
		AgentRules: []string{
			"do not dump whole repositories or full environment snapshots into server context",
			"prefer host-side search, indexing, and summarization before requesting raw content",
			"ask when the workspace, config source, credential boundary, or retention policy is unclear",
			"use signed jobs for context collection so host policy, redaction, audit, and approval gates apply",
		},
		RedactionRules: []string{
			"redact secrets, tokens, private keys, customer identifiers, and private hostnames before upload",
			"store sensitive raw context on the host when possible and upload only redacted summaries or references",
			"include checksums and freshness metadata so later slices can be verified without reloading everything",
		},
	}
}

func newProvisioningPlan() ProvisioningPlan {
	return ProvisioningPlan{
		SchemaVersion: "rdev.agent-provisioning-plan.v1",
		Mode:          "adaptive-host-local",
		DiscoveryTargets: []string{
			"installed rdev binary and version",
			"installed Skillkit skills and MCP tool contract",
			"available adapters: codex, claude-code, acpx, shell, powershell",
			"OS, shell, service manager, package managers, and PATH",
			"project language runtimes and dependency manifests",
			"workspace permissions, disk space, network/proxy settings, and trust roots",
		},
		AutoInstallAllowed: []string{
			"user-scoped Skillkit skills from a verified bundle",
			"user-scoped MCP tool metadata from a verified bundle",
			"project-local development dependencies declared by lockfiles or manifests",
			"adapter shims or helper binaries from verified release artifacts",
			"cache directories and host-local context indexes under the approved workspace or rdev data root",
		},
		InstallScopes: []string{
			"remote-host-local",
			"user-scoped-by-default",
			"workspace-scoped-when-project-specific",
			"managed-service-scoped-only-after-operator-approval",
		},
		ApprovalRequiredFor: []string{
			"administrator or root elevation",
			"system-wide package installation",
			"service, daemon, LaunchAgent, systemd, registry, or login-item changes",
			"credential, token, SSH key, certificate, or secret-manager mutation",
			"external account creation, paid API use, publishing, pushing, deploying, or firewall changes",
			"execution policy or security product configuration changes beyond process-scoped bootstrap flags",
		},
		PreferredSources: []string{
			"verified Remote Dev Skillkit bundle",
			"signed rdev release artifacts and release bundle",
			"project lockfiles and package manifests",
			"operator-provided internal mirrors or package registries",
			"already-installed host tools discovered at runtime",
		},
		AgentRules: []string{
			"probe before installing; reuse working host-local tools when versions satisfy the task",
			"prefer user-scoped or workspace-scoped installs over system-wide installs",
			"verify checksums, signatures, bundle manifests, and lockfiles before execution",
			"record every install, version, source, checksum, and command as evidence",
			"do not assume framework paths; detect Codex, Claude Code, Hermes, OpenClaw, OpenCode, and generic MCP locations or ask",
			"ask when install scope, package source, credential boundary, or approval policy is unclear",
		},
		EvidenceRequired: []string{
			"preflight detection result",
			"install plan with scope and source",
			"verification output for downloaded or copied artifacts",
			"commands run and exit codes",
			"post-install capability report",
			"redacted logs and artifact ids",
		},
	}
}

func newCustomerBootstrap(joinURL string) CustomerBootstrap {
	bootstrapBase := strings.TrimRight(joinURL, "/")
	return CustomerBootstrap{
		SchemaVersion:   "rdev.customer-bootstrap.v1",
		CustomerLink:    joinURL,
		ConsentMode:     "attended-visible-consent",
		AutomationLevel: "one-link-minimal-steps-after-consent",
		OneLineCommands: map[string]string{
			"macos_linux_sh":     "curl -fsSL " + shellQuote(bootstrapBase+"/bootstrap.sh") + " | sh",
			"windows_powershell": "powershell -NoProfile -ExecutionPolicy Bypass -Command \"irm '" + powershellSingleQuoteValue(bootstrapBase+"/bootstrap.ps1") + "' | iex\"",
		},
		InstallPrerequisites: []string{
			"rdev binary is installed or made available by the published bootstrap package",
			"target user runs the visible bootstrap command on the machine that needs help",
			"outbound HTTPS or configured fallback connectivity to the gateway",
		},
		CustomerSteps: []string{
			"Open customer_link on the target machine.",
			"Run the platform command shown on that page.",
			"Keep the visible host session running until the Agent reports completion.",
		},
		AgentAfterConnect: []string{
			"watch rdev.hosts.list until the host appears",
			"approve the expected host when policy requires approval",
			"run max-control discovery and repair jobs scoped to the support request",
			"export evidence and revoke the ticket when finished",
		},
		RevocationInstructions: []string{
			"revoke the ticket with rdev.tickets.revoke or the gateway API",
			"stop the visible host process on the target machine",
			"remove any temporary bootstrap files created by the published installer package",
		},
	}
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

func validNetworkScope(scope string) bool {
	switch scope {
	case "auto", "internet", "lan", "relay", "mesh", "ssh":
		return true
	default:
		return false
	}
}

func validAuthorityProfile(profile string) bool {
	switch profile {
	case "standard", "max-control":
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
			"Relay, VPN, mesh, and SSH are connectivity assists only; they never authorize jobs by themselves.",
		},
	}
}

func newAuthorityProfile(profile string) AuthorityProfile {
	if profile == "standard" {
		return AuthorityProfile{
			SchemaVersion:  "rdev.agent-authority.v1",
			Profile:        profile,
			RemoteHostRole: "approved-rdev-host",
			Discovery: AuthorityScope{
				Allowed:     true,
				Scope:       "host-local",
				Description: "Inspect the connected host and gateway reachability.",
				Requirements: []string{
					"host is registered and approved",
					"job policy includes read-only inspection capabilities",
				},
			},
			DownstreamControl: AuthorityScope{
				Allowed:     false,
				Scope:       "none",
				Description: "Downstream device control is not enabled by the standard profile.",
			},
			AutonomousActions: []string{
				"host-local diagnostics",
				"gateway reachability checks",
				"adapter and tool discovery",
			},
			RequiredCapabilities: []string{"process.inspect", "fs.read"},
			EvidenceRequired:     []string{"diagnostic commands", "host capabilities", "job artifact", "audit events"},
			StopConditions:       []string{"host approval missing", "job policy denies requested capability"},
		}
	}
	return AuthorityProfile{
		SchemaVersion:  "rdev.agent-authority.v1",
		Profile:        profile,
		RemoteHostRole: "field-workstation-and-control-pivot",
		Discovery: AuthorityScope{
			Allowed:     true,
			Scope:       "host-local, scoped LAN/private network, configured public target ranges, configured relay/mesh/SSH routes",
			Description: "The approved remote host may be used as the Agent's field vantage point for heuristic discovery.",
			Requirements: []string{
				"host is registered and approved",
				"job policy includes network.discovery.scoped or equivalent operator-granted capability",
				"discovery evidence is captured as job artifacts",
			},
		},
		DownstreamControl: AuthorityScope{
			Allowed:     true,
			Scope:       "reachable devices in discovered or configured authorized scopes",
			Description: "The Agent may control downstream hosts or devices through the approved remote host when credentials, routes, or management protocols are available.",
			Requirements: []string{
				"job policy includes downstream.control.scoped",
				"credentials or control channels are already configured or supplied through local secret storage",
				"actions remain tied to the stated task intent",
				"control evidence and affected device identifiers are captured",
			},
		},
		AutonomousActions: []string{
			"local interface and route discovery",
			"scoped LAN/private-network reachability probes",
			"mDNS/DNS/service discovery where available",
			"configured SSH tunnel use",
			"configured mesh/VPN route use",
			"configured relay use",
			"downstream device diagnostics and control within job policy",
		},
		RequiredCapabilities: []string{
			"network.discovery.scoped",
			"network.probe.lan",
			"relay.use",
			"mesh.use",
			"ssh.tunnel",
			"downstream.control.scoped",
			"shell.user",
		},
		EvidenceRequired: []string{
			"discovery scope",
			"probes executed",
			"reachable device inventory",
			"selected control path",
			"commands or API calls executed",
			"redacted outputs",
			"affected device identifiers",
			"audit events",
		},
		StopConditions: []string{
			"requested action no longer matches task intent",
			"credential or target identity is ambiguous",
			"job policy denies required capability",
			"action would mutate third-party accounts or credentials without explicit approval",
			"downstream device appears outside discovered or configured authorized scope",
		},
		ControlPaths: []ConnectionProtocol{
			{
				ID:            "downstream-ssh",
				Status:        "agent-managed-when-configured",
				BestFor:       "reachable servers, routers, appliances, and dev machines with existing SSH access",
				Requirements:  []string{"ssh config or agent entry exists", "job policy permits downstream.control.scoped", "commands are captured and redacted"},
				SecurityModel: "SSH provides reachability; rdev job policy and audit govern the Agent action",
				AgentAction:   "use existing SSH routes automatically when target identity and key choice are unambiguous",
			},
			{
				ID:            "downstream-http-api",
				Status:        "agent-managed-when-configured",
				BestFor:       "routers, NAS devices, services, and management APIs reachable from the host",
				Requirements:  []string{"API endpoint is in scope", "credentials are configured or supplied through secret storage", "state-changing calls match task intent"},
				SecurityModel: "API auth is a device credential; rdev records intent, evidence, and affected device identity",
				AgentAction:   "discover and use configured management APIs when they are needed for the task",
			},
			{
				ID:            "downstream-mesh-or-relay",
				Status:        "agent-managed-when-configured",
				BestFor:       "devices reachable only through existing mesh, VPN, or relay routes",
				Requirements:  []string{"route is installed or discoverable", "credential mutation is not required", "device remains in authorized scope"},
				SecurityModel: "mesh/relay assists routing only; rdev policy authorizes the work",
				AgentAction:   "use configured mesh, VPN, or relay routes as soon as native reachability fails",
			},
		},
	}
}

func newConnectionPlan(gatewayURL, networkScope string) ConnectionPlan {
	reachability := "internet-or-operator-configured"
	if gatewayLooksLocal(gatewayURL) {
		reachability = "local-machine"
	} else if gatewayLooksLAN(gatewayURL) {
		reachability = "lan"
	}
	implemented := []ConnectionProtocol{
		{
			ID:            "outbound-wss-mtls",
			Status:        "implemented",
			BestFor:       "public gateway, LAN gateway, low-latency job delivery, and mTLS deployments",
			Requirements:  []string{"target can reach gateway over outbound TCP 443 or configured WSS port", "WebSocket upgrade is allowed", "CA/client certificate material is provided when mTLS is required"},
			SecurityModel: "transport identity only; signed envelopes, host policy, approvals, and evidence remain authoritative",
			AgentAction:   "try first when network scope is auto and the gateway is reachable",
		},
		{
			ID:            "outbound-https-long-poll",
			Status:        "implemented",
			BestFor:       "corporate proxies, NAT, and firewalls that allow HTTPS but block WebSocket upgrades",
			Requirements:  []string{"target can reach gateway over outbound HTTP or HTTPS", "long-held requests are not aggressively terminated"},
			SecurityModel: "same signed job and host-local policy model as WSS",
			AgentAction:   "use automatically after WSS fails before any relay, mesh, or SSH escalation",
		},
		{
			ID:            "outbound-https-short-poll",
			Status:        "implemented",
			BestFor:       "maximum compatibility when long-held requests are interrupted",
			Requirements:  []string{"target can make repeated outbound HTTP or HTTPS requests to gateway"},
			SecurityModel: "same signed job and host-local policy model as WSS",
			AgentAction:   "use as the final native fallback before agent-managed relay, mesh, or SSH connectivity",
		},
		{
			ID:            "lan-gateway-http-https",
			Status:        lanStatus(gatewayURL),
			BestFor:       "Agent server and target host on the same LAN or VPN segment",
			Requirements:  []string{"gateway URL uses a LAN-reachable host/IP", "target can route to that address", "local firewall permits the gateway listener"},
			SecurityModel: "LAN reachability is not authorization; host still verifies manifests, trust pins, envelopes, and approvals",
			AgentAction:   "if gateway is local-only or private, probe scoped LAN reachability and use the routable LAN URL when available",
		},
	}
	agentManaged := []ConnectionProtocol{
		{
			ID:            "agent-managed-https-relay",
			Status:        "agent-managed-when-configured",
			BestFor:       "target cannot reach private Agent server directly but can reach a neutral HTTPS relay",
			Requirements:  []string{"relay URL is configured or discoverable", "relay forwards only rdev gateway HTTP/WSS traffic", "relay credentials stay in local config or secret storage"},
			SecurityModel: "relay is connectivity only and cannot authorize jobs",
			AgentAction:   "use a configured relay automatically after native outbound HTTPS paths fail; ask only if no relay is configured",
		},
		{
			ID:            "agent-managed-mesh-vpn",
			Status:        "agent-managed-when-configured",
			BestFor:       "owned or managed hosts where Tailscale/headscale/WireGuard-style mesh is installed or available",
			Requirements:  []string{"mesh CLI/config is present or configured", "gateway URL resolves inside the mesh", "mesh credentials are not emitted in invite output"},
			SecurityModel: "mesh identity assists routing only; rdev host approval and signed jobs still govern execution",
			AgentAction:   "detect installed mesh tooling and use it automatically when already configured; ask only before new enrollment or external account mutation",
		},
		{
			ID:            "agent-managed-ssh-tunnel",
			Status:        "agent-managed-when-configured",
			BestFor:       "reachable hosts where SSH access already exists and HTTPS egress is blocked",
			Requirements:  []string{"SSH endpoint is discovered or configured", "SSH key custody stays in the local SSH agent/config", "target consent and host approval still happen"},
			SecurityModel: "SSH is a tunnel only, not job authorization",
			AgentAction:   "use existing SSH config/agent entries automatically for tunnel setup; ask only when endpoint, username, or key choice is unclear",
		},
	}
	return ConnectionPlan{
		SchemaVersion:       "rdev.connection-plan.v1",
		NetworkScope:        networkScope,
		GatewayReachability: reachability,
		Implemented:         implemented,
		AgentManaged:        agentManaged,
		DiscoveryPlan: DiscoveryPlan{
			SchemaVersion: "rdev.discovery-plan.v1",
			Allowed: []string{
				"local interface and route inspection",
				"configured gateway, proxy, SSH, relay, and mesh config inspection",
				"scoped LAN reachability probes for private/link-local ranges related to local interfaces",
				"mDNS/Bonjour or local DNS lookup when available",
				"gateway health checks and protocol preflight checks",
			},
			Boundaries: []string{
				"scan local/private networks and any explicit public target ranges provided by the user or configuration",
				"do not print secrets, tokens, SSH keys, relay credentials, or private server addresses into public artifacts",
				"do not mutate third-party relay/mesh accounts or credentials without explicit approval",
				"do not treat a discovered transport path as host approval or job authorization",
			},
			StopWhen: []string{
				"a host registers and becomes pending or active",
				"a required credential, route, proxy, relay, mesh, or SSH value is ambiguous",
				"the probe would leave the local/private scope",
			},
		},
		SelectionOrder: []string{
			"lan-gateway-http-https when gateway URL is local/private and target is on the same LAN",
			"outbound-wss-mtls",
			"outbound-https-long-poll",
			"outbound-https-short-poll",
			"agent-managed-https-relay",
			"agent-managed-mesh-vpn",
			"agent-managed-ssh-tunnel",
		},
		EnvironmentProbes: []string{
			"rdev doctor on the Agent side",
			"gateway /healthz reachability from the Agent side",
			"target-side DNS resolution for the gateway host",
			"target-side outbound TCP 443 or configured gateway port",
			"proxy, captive portal, VPN, TLS interception, and firewall policy",
			"whether Agent and target share a LAN, VPN, relay, SSH route, or configured mesh",
		},
		AgentRules: []string{
			"Actively probe the local environment and scoped private network before asking the user.",
			"Use configured relay, mesh, SSH, proxy, certificate, or private gateway values automatically when unambiguous.",
			"Ask concise questions only when network scope, credentials, or target identity are unclear.",
			"Prefer outbound target-host connections; avoid inbound target firewall changes.",
			"Treat every non-rdev transport as connectivity assistance only.",
		},
	}
}

func lanStatus(gatewayURL string) string {
	if gatewayLooksLAN(gatewayURL) || gatewayLooksLocal(gatewayURL) {
		return "implemented-when-target-can-route-to-gateway"
	}
	return "supported-with-lan-reachable-gateway-url"
}

func gatewayLooksLocal(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || (ip != nil && ip.IsLoopback())
}

func gatewayLooksLAN(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast()
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

func powershellSingleQuoteValue(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
