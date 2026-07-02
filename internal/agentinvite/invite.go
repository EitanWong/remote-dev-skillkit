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
	GatewayURL            string
	JoinURL               string
	ManifestURL           string
	ManifestRootPublicKey string
	Ticket                model.Ticket
	Transport             string
	NetworkScope          string
	AuthorityProfile      string
	Once                  bool
	RequireHostApproval   bool
	RdevCommand           string
	CreatedAt             time.Time
}

type Invite struct {
	SchemaVersion         string              `json:"schema_version"`
	GatewayURL            string              `json:"gateway_url"`
	JoinURL               string              `json:"join_url"`
	ManifestURL           string              `json:"manifest_url"`
	ManifestRootPublicKey string              `json:"manifest_root_public_key,omitempty"`
	Ticket                model.Ticket        `json:"ticket"`
	Transport             string              `json:"transport"`
	TransportPlan         TransportPlan       `json:"transport_plan"`
	ConnectionPlan        ConnectionPlan      `json:"connection_plan"`
	AuthorityProfile      AuthorityProfile    `json:"authority_profile"`
	ConnectionEntry       ConnectionEntry     `json:"connection_entry"`
	ConnectionEntryPlan   ConnectionEntryPlan `json:"connection_entry_plan"`
	HostContextPlan       HostContextPlan     `json:"host_context_plan"`
	ProvisioningPlan      ProvisioningPlan    `json:"agent_provisioning_plan"`
	CollaborationPlan     CollaborationPlan   `json:"agent_collaboration_plan"`
	LocalizationPlan      LocalizationPlan    `json:"localization_plan"`
	ManagedDevPlan        ManagedDevPlan      `json:"managed_development_plan"`
	HostCommand           string              `json:"host_command"`
	FallbackCommands      []string            `json:"fallback_commands"`
	HumanNextActions      []string            `json:"human_next_actions"`
	AgentNextActions      []string            `json:"agent_next_actions"`
	ConnectivityChecks    []string            `json:"connectivity_checks"`
	MCPTools              map[string]string   `json:"mcp_tools"`
	RequiresHumanAction   []string            `json:"requires_human_action"`
	CreatedAt             time.Time           `json:"created_at"`
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

type ConnectionEntry struct {
	SchemaVersion          string                              `json:"schema_version"`
	HandoffName            string                              `json:"handoff_name"`
	HandoffContract        []string                            `json:"handoff_contract"`
	EntryURL               string                              `json:"entry_url"`
	ConsentMode            string                              `json:"consent_mode"`
	AutomationLevel        string                              `json:"automation_level"`
	PackageCatalog         model.ConnectionEntryPackageCatalog `json:"package_catalog"`
	OneLineCommands        map[string]string                   `json:"one_line_commands"`
	InstallPrerequisites   []string                            `json:"install_prerequisites"`
	HumanSteps             []string                            `json:"human_steps"`
	AgentAfterConnect      []string                            `json:"agent_after_connect"`
	RevocationInstructions []string                            `json:"revocation_instructions"`
}

type ConnectionEntryPlan struct {
	SchemaVersion         string                `json:"schema_version"`
	Mode                  string                `json:"mode"`
	PackagePlanSchema     string                `json:"package_plan_schema"`
	BestFor               []string              `json:"best_for"`
	EntryModes            []string              `json:"entry_modes"`
	TargetSelectionPolicy TargetSelectionPolicy `json:"target_selection_policy"`
	ModeSelection         []string              `json:"mode_selection"`
	RequiredAgentFlow     []string              `json:"required_agent_flow"`
	PackageFormats        []string              `json:"package_formats"`
	RequiredContents      []string              `json:"required_contents"`
	RuntimeFlow           []string              `json:"runtime_flow"`
	NetworkStrategy       []string              `json:"network_strategy"`
	PrivilegeStrategy     []string              `json:"privilege_strategy"`
	AgentRules            []string              `json:"agent_rules"`
	EvidenceRequired      []string              `json:"evidence_required"`
	ImplementationGaps    []string              `json:"implementation_gaps"`
}

type TargetSelectionPolicy struct {
	SchemaVersion         string   `json:"schema_version"`
	DecisionOwner         string   `json:"decision_owner"`
	DefaultOwnedMode      string   `json:"default_owned_mode"`
	DefaultThirdPartyMode string   `json:"default_third_party_mode"`
	OwnedSignals          []string `json:"owned_signals"`
	ThirdPartySignals     []string `json:"third_party_signals"`
	AskWhen               []string `json:"ask_when"`
	AgentRules            []string `json:"agent_rules"`
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

type CollaborationPlan struct {
	SchemaVersion       string   `json:"schema_version"`
	Mode                string   `json:"mode"`
	Protocols           []string `json:"protocols"`
	DiscoveryTargets    []string `json:"discovery_targets"`
	CollaborationUses   []string `json:"collaboration_uses"`
	DelegationRules     []string `json:"delegation_rules"`
	ApprovalRequiredFor []string `json:"approval_required_for"`
	EvidenceRequired    []string `json:"evidence_required"`
}

type LocalizationPlan struct {
	SchemaVersion      string   `json:"schema_version"`
	Mode               string   `json:"mode"`
	SupportedLanguages []string `json:"supported_languages"`
	DetectionSources   []string `json:"detection_sources"`
	LocalizedSurfaces  []string `json:"localized_surfaces"`
	AgentRules         []string `json:"agent_rules"`
	FallbackOrder      []string `json:"fallback_order"`
	EvidenceRequired   []string `json:"evidence_required"`
}

type ManagedDevPlan struct {
	SchemaVersion       string   `json:"schema_version"`
	Mode                string   `json:"mode"`
	BestFor             []string `json:"best_for"`
	HostModes           []string `json:"host_modes"`
	ServiceSurfaces     []string `json:"service_surfaces"`
	ReliabilityControls []string `json:"reliability_controls"`
	WorkspaceControls   []string `json:"workspace_controls"`
	MaintenanceControls []string `json:"maintenance_controls"`
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

	manifestRootPublicKey := strings.TrimSpace(opts.ManifestRootPublicKey)
	hostCommand := hostServeCommand(rdevCommand, manifestURL, manifestRootPublicKey, transport, opts.Once)
	transportPlan := newTransportPlan(rdevCommand, manifestURL, manifestRootPublicKey, transport, opts.Once)
	connectionPlan := newConnectionPlan(gatewayURL, networkScope)
	authority := newAuthorityProfile(authorityProfile)
	connectionEntry := newConnectionEntry(joinURL)
	connectionEntryPlan := newConnectionEntryPlan(gatewayURL, manifestURL, manifestRootPublicKey, transport)
	hostContextPlan := newHostContextPlan()
	provisioningPlan := newProvisioningPlan()
	collaborationPlan := newCollaborationPlan()
	localizationPlan := newLocalizationPlan()
	managedDevPlan := newManagedDevPlan()
	fallbackCommands := fallbackCommandsFromPlan(transportPlan, transport)

	agentActions := []string{
		"Probe the local runtime with rdev doctor and inspect configured gateway reachability before giving the human a connection entry link, script, or package.",
		"Poll rdev.hosts.list with status=pending or active until the target host appears.",
		"Ask the human operator to approve the host if it is pending; then call rdev.hosts.approve with ticket-scoped capabilities.",
		"Create a job with rdev.jobs.create only after the host is active and the requested policy is explained.",
		"Read rdev.jobs.status and export evidence before reporting completion.",
	}
	if !opts.RequireHostApproval {
		agentActions[1] = "If the host is pending, request approval; if it is already active, continue to job policy explanation."
	}

	return Invite{
		SchemaVersion:         SchemaVersion,
		GatewayURL:            gatewayURL,
		JoinURL:               joinURL,
		ManifestURL:           manifestURL,
		ManifestRootPublicKey: manifestRootPublicKey,
		Ticket:                opts.Ticket,
		Transport:             transport,
		TransportPlan:         transportPlan,
		ConnectionPlan:        connectionPlan,
		AuthorityProfile:      authority,
		ConnectionEntry:       connectionEntry,
		ConnectionEntryPlan:   connectionEntryPlan,
		HostContextPlan:       hostContextPlan,
		ProvisioningPlan:      provisioningPlan,
		CollaborationPlan:     collaborationPlan,
		LocalizationPlan:      localizationPlan,
		ManagedDevPlan:        managedDevPlan,
		HostCommand:           hostCommand,
		FallbackCommands:      fallbackCommands,
		HumanNextActions: []string{
			"Open connection_entry.entry_url or run the generated connection entry package on the target machine that needs help.",
			"Keep the visible connection session open until the Agent reports completion.",
		},
		AgentNextActions: agentActions,
		ConnectivityChecks: []string{
			"Confirm the gateway URL is reachable from the Agent environment before creating jobs.",
			"Ask the target-side human to open connection_entry.entry_url or run the generated connection entry package; the host connects outbound, so no inbound port should be opened on the target machine.",
			"If the host does not appear, ask for target-side network symptoms: proxy requirement, TLS interception, blocked outbound 443, DNS failure, captive portal, or VPN requirement.",
			"Prefer auto transport. It tries WSS first and falls back to HTTPS long-poll, then short polling for restrictive networks.",
			"If the Agent and target are on the same LAN, probe scoped local/LAN reachability and prefer a LAN-reachable gateway URL before relay, mesh, or SSH.",
			"Use mTLS, SSH, mesh, relay, or proxy inputs automatically when they are already configured in the environment; ask only when required values cannot be discovered.",
			"When authority_profile permits downstream control, the connected host may act as the field workstation for discovering and controlling other reachable authorized devices.",
			"Keep remote environment, project structure, requirement notes, and large evidence on the host; load only indexed slices or artifact references into the Agent server context.",
			"Let the host detect missing skills, MCP tools, adapters, language runtimes, package managers, and project dependencies; install user-scoped missing pieces automatically when policy allows, and ask before elevation or external mutation.",
			"Discover local or configured Agent peers, including A2A-compatible agents, when collaboration can help; delegate only through signed jobs, scoped policy, approvals, redaction, and evidence.",
			"Detect the target host language and localize connection entries, skills, MCP responses, job summaries, approval prompts, and evidence summaries; fall back predictably when a locale is unavailable.",
			"For owned long-running development machines, prefer managed mode with service-backed restart, release gates, enrollment renewal, revocation refresh, workspace locks, host-local context, and reconnect evidence.",
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

func newManagedDevPlan() ManagedDevPlan {
	return ManagedDevPlan{
		SchemaVersion: "rdev.managed-development-plan.v1",
		Mode:          "owned-long-running-developer-workstation",
		BestFor: []string{
			"operator-owned Macs, Windows PCs, and Linux workstations",
			"long-running coding, debugging, testing, refactoring, and repository maintenance",
			"recurring Agent work on stable local projects",
			"host-local context caches, dependency caches, and durable evidence stores",
		},
		HostModes: []string{
			"managed",
			"attended-temporary for one-off third-party support",
		},
		ServiceSurfaces: []string{
			"macOS LaunchAgent plan with explicit launchctl start/inspect/stop",
			"Linux systemd user service plan with explicit systemctl --user lifecycle",
			"Windows Service reviewed sc.exe plan with explicit start/stop/delete",
			"foreground managed process for development smoke tests",
		},
		ReliabilityControls: []string{
			"--once=false for continuous host loops",
			"--transport auto for WSS, HTTPS long-poll, then short-poll fallback",
			"service restart or KeepAlive where supported by the reviewed service surface",
			"signed release-bundle startup gate before registration",
			"host enrollment certificate renewal before expiry",
			"signed revocation refresh before registration",
			"trust-bundle sequence and rollback checks",
			"reconnect proof after service restart, logout, or reboot acceptance",
		},
		WorkspaceControls: []string{
			"workspace locks for one-writer protection",
			"Git worktree preparation for coding jobs",
			"host-local project indexes and context slices",
			"adapter-specific evidence for Codex, Claude Code, acpx, shell, and PowerShell",
			"approval gates before push, merge, deploy, publish, credential, service, or external-resource changes",
		},
		MaintenanceControls: []string{
			"periodic rdev doctor and host capability probes",
			"release-bundle verification before host upgrades",
			"Skillkit and MCP tool contract verification after updates",
			"artifact, audit, context-cache, and dependency-cache retention policy",
			"safe uninstall or service stop plan retained with the managed host",
		},
		AgentRules: []string{
			"treat owned long-running developer machines as managed hosts, not third-party temporary hosts",
			"keep the host visible and controllable through normal OS service controls",
			"store project context and large evidence on the host; keep server context indexed and on demand",
			"reuse verified host-local tools and caches before reinstalling dependencies",
			"collect evidence for reconnects, service lifecycle, workspace locks, jobs, approvals, and upgrades",
			"ask before enabling persistence on a third-party machine",
		},
		EvidenceRequired: []string{
			"managed host registration and approval",
			"service plan or foreground managed command",
			"release-gate verification output",
			"workspace lock acquire and release evidence",
			"adapter result artifacts and test evidence",
			"audit slice and evidence bundle",
			"reconnect transcript for service-backed claims",
			"safe stop or uninstall instructions",
		},
	}
}

func newLocalizationPlan() LocalizationPlan {
	return LocalizationPlan{
		SchemaVersion: "rdev.localization-plan.v1",
		Mode:          "target-host-language-auto",
		SupportedLanguages: []string{
			"en",
			"zh-CN",
			"es",
			"fr",
			"de",
			"ja",
			"ko",
			"pt-BR",
			"hi",
			"ar",
			"ru",
		},
		DetectionSources: []string{
			"join page lang query parameter",
			"HTTP Accept-Language",
			"target host locale environment variables such as LANG, LC_ALL, LC_MESSAGES, and LANGUAGE",
			"Windows UI culture and user culture",
			"macOS AppleLanguages and locale",
			"Linux locale and desktop language settings",
			"operator or target-side explicit language preference",
		},
		LocalizedSurfaces: []string{
			"join page and bootstrap instructions",
			"Skillkit README and install notes",
			"Agent skills and workflow prompts",
			"MCP tool summaries and user-facing errors",
			"approval prompts and risk explanations",
			"job status, artifact summaries, and evidence bundle summaries",
			"release, verification, and acceptance instructions",
		},
		AgentRules: []string{
			"use BCP 47 language tags and normalize regional variants before matching",
			"prefer the target-side language for target-side instructions",
			"prefer the operator's configured language for operator-only reports when known",
			"keep protocol keys, schema versions, capability ids, command names, paths, and code blocks unchanged",
			"do not translate secrets, tokens, file paths, shell syntax, JSON field names, or evidence checksums",
			"include the selected language in evidence when language affects target-side instructions or approvals",
			"ask only when detected languages conflict and the next action is target-facing or high risk",
		},
		FallbackOrder: []string{
			"exact BCP 47 match",
			"base language match",
			"English",
		},
		EvidenceRequired: []string{
			"detected locale sources",
			"selected BCP 47 language",
			"fallback reason when exact match is unavailable",
			"localized target-facing text version or artifact id",
		},
	}
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
			"redact secrets, tokens, private keys, target-side identifiers, and private hostnames before upload",
			"store sensitive raw context on the host when possible and upload only redacted summaries or references",
			"include checksums and freshness metadata so later slices can be verified without reloading everything",
		},
	}
}

func newCollaborationPlan() CollaborationPlan {
	return CollaborationPlan{
		SchemaVersion: "rdev.agent-collaboration-plan.v1",
		Mode:          "host-local-peer-collaboration",
		Protocols: []string{
			"a2a-agent-card",
			"a2a-jsonrpc-http",
			"a2a-sse-streaming",
			"mcp-stdio",
			"local-agent-cli",
		},
		DiscoveryTargets: []string{
			"A2A agent cards from configured URLs or well-known local endpoints",
			"local MCP servers and tool contracts",
			"installed agent CLIs such as codex, claude, acpx, openclaw, opencode, or hermes",
			"workspace-local agent configuration files",
			"operator-provided peer endpoints and trust roots",
		},
		CollaborationUses: []string{
			"ask a specialized local Agent to inspect or summarize host-local context",
			"delegate a bounded coding, debugging, documentation, or test subtask",
			"coordinate with an existing project Agent that already knows the workspace",
			"request tool-specific diagnostics without copying full project context to the central Agent server",
			"collect peer artifacts, messages, task status, and summaries as rdev evidence",
		},
		DelegationRules: []string{
			"treat peer agents as adapters or downstream collaborators, not as authorization roots",
			"discover peer capabilities before delegation and choose the narrowest useful task",
			"use host-local context slices and artifact references instead of dumping full repositories into peer prompts",
			"wrap peer work in signed rdev jobs so host policy, workspace locks, redaction, audit, cancellation, and approval gates apply",
			"do not expose rdev operator tokens, host private keys, release keys, target-side secrets, or unrestricted shell access to peers",
			"prefer A2A task, message, and artifact boundaries when a peer advertises an A2A Agent Card",
		},
		ApprovalRequiredFor: []string{
			"delegating to an untrusted, unauthenticated, or internet-reachable peer",
			"sharing sensitive target-side data, secrets, credentials, private hostnames, or regulated data",
			"letting a peer install packages, mutate services, push, deploy, publish, spend money, or change external resources",
			"granting a peer downstream control over additional hosts or devices",
		},
		EvidenceRequired: []string{
			"peer discovery result and advertised capabilities",
			"selected protocol and endpoint trust basis",
			"delegated task intent, scope, and policy",
			"peer messages, task status, artifacts, checksums, and redaction metadata",
			"approval tokens used for high-risk collaboration",
			"final peer summary linked to rdev job and audit ids",
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

func newConnectionEntry(joinURL string) ConnectionEntry {
	bootstrapBase := strings.TrimRight(joinURL, "/")
	return ConnectionEntry{
		SchemaVersion:   "rdev.connection-entry.v1",
		HandoffName:     "Connection Entry",
		HandoffContract: connectionEntryHandoffContract(),
		EntryURL:        joinURL,
		ConsentMode:     "attended-visible-consent",
		AutomationLevel: "one-entry-minimal-steps-after-consent",
		PackageCatalog:  model.NewConnectionEntryPackageCatalog(joinURL),
		OneLineCommands: map[string]string{
			"macos_linux_sh":     "curl -fsSL " + shellQuote(bootstrapBase+"/bootstrap.sh") + " | sh",
			"windows_powershell": "powershell -NoProfile -Command \"irm '" + powershellSingleQuoteValue(bootstrapBase+"/bootstrap.ps1") + "' | iex\"",
		},
		InstallPrerequisites: []string{
			"preferred: a signed self-contained Remote Dev Skillkit connection entry package is available for the target OS",
			"fallback: rdev binary is already installed or made available by the published connection entry package",
			"target-side user runs one visible connection entry on the machine that needs help",
			"outbound HTTPS or configured fallback connectivity to the gateway",
		},
		HumanSteps: []string{
			"Open entry_url on the target machine.",
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

func newConnectionEntryPlan(gatewayURL, manifestURL, manifestRootPublicKey, transport string) ConnectionEntryPlan {
	return ConnectionEntryPlan{
		SchemaVersion:     "rdev.connection-entry-plan.v1",
		Mode:              "universal-agent-selected-entry",
		PackagePlanSchema: "rdev.connection-entry.package-plan.v1",
		BestFor: []string{
			"any new target host connection where humans should not assemble tickets, roots, gateway URLs, or transport flags",
			"third-party temporary repair where the target host should not install Go, Git, Node, Python, or other prerequisites first",
			"operator-owned workstations that should become stable long-running managed development hosts",
			"cross-subnet LAN, VPN, NAT, firewall, proxy, and CGNAT environments where the entry must choose the best outbound path",
			"owned workstations that need a quick attended bootstrap before an explicit managed-service plan is approved",
			"package-aware join pages where the Agent or browser chooses the target OS package or visible script fallback",
		},
		EntryModes: []string{
			"attended-temporary for third-party, one-off, or time-boxed repair",
			"managed for operator-owned personal or fleet machines that need durable development access",
			"break-glass only when operator policy explicitly permits a short-lived emergency session",
		},
		TargetSelectionPolicy: newTargetSelectionPolicy(),
		ModeSelection: []string{
			"if the machine is operator-owned or expected to support recurring Agent development work, select managed mode and plan an explicit visible service lifecycle",
			"if the machine belongs to someone else or the session is one-off support, select attended-temporary mode with no persistence by default",
			"if ownership, persistence approval, or service policy is unclear, ask one concise question before creating a managed entry",
			"do not expose raw ticket codes, manifest roots, gateway URLs, or transport flags to the human when a connection_entry can carry them",
		},
		RequiredAgentFlow: []string{
			"create an invite first with rdev.invites.create or rdev invite create",
			"materialize the invite with rdev.connection_entry.plan or rdev connection-entry plan before giving target-side instructions",
			"give the target side only the selected connection_entry.entry_url, visible launcher script, or signed package",
			"keep ticket, gateway, manifest root, transport, release, and checksum values inside Agent/tool metadata",
			"choose managed for owned recurring machines and attended-temporary for third-party or one-off machines",
		},
		PackageFormats: []string{
			"windows-amd64 signed zip or exe containing rdev.exe, release-bundle.json, checksums, and a visible connection launcher",
			"darwin-arm64/darwin-amd64 tar.gz or pkg containing rdev, release-bundle.json, checksums, and a visible connection launcher",
			"linux-amd64/linux-arm64 tar.gz or AppImage-style package containing rdev, release-bundle.json, checksums, and a visible connection launcher",
		},
		RequiredContents: []string{
			"target-platform rdev/rdev-host binary",
			"signed release-bundle.json and required artifact manifests",
			"pinned release root public key",
			"pinned manifest root public key: " + placeholderIfEmpty(manifestRootPublicKey, "<manifest-root-public-key>"),
			"join manifest URL: " + placeholderIfEmpty(manifestURL, "<manifest-url>"),
			"gateway URL: " + placeholderIfEmpty(gatewayURL, "<gateway-url>"),
			"transport preference: " + placeholderIfEmpty(transport, "auto"),
			"visible stop/revoke instructions and local cleanup notes",
			"managed-mode service plan only when the operator explicitly approves persistent owned-host access",
			"package catalog with per-OS package candidates and visible script fallbacks",
		},
		RuntimeFlow: []string{
			"show the target-side user the session purpose, gateway, ticket expiry, requested capabilities, and stop instructions before connecting",
			"detect target OS and architecture from Agent probes, browser hints, inventory, or a single operator answer before selecting a package candidate",
			"verify the package checksum and signed release bundle before executing the host binary",
			"fetch and verify the join manifest with the pinned manifest root before trusting ticket, gateway, or job-signing metadata",
			"select attended-temporary or managed mode from ownership, recurrence, and persistence policy before registering",
			"register the host, wait for operator approval when required, then start job transport with --transport auto",
			"for attended temporary mode, keep the session visible; closing the launcher stops the host process",
			"for managed owned-host mode, use a reviewed service plan, release gate, renewal/revocation refresh, reconnect proof, and explicit stop/uninstall instructions",
		},
		NetworkStrategy: []string{
			"try outbound WSS first for low-latency work",
			"fall back to HTTPS long-poll when WebSocket upgrades are blocked",
			"fall back to short polling when long-held requests are unstable",
			"prefer LAN/private gateway URLs when the Agent and target can route to each other across subnets",
			"use already configured proxy, relay, mesh, VPN, or SSH paths when direct outbound gateway access fails",
			"ask before installing persistent, privileged, paid, firewall, DNS, cloud, or security-policy-changing connectivity components",
		},
		PrivilegeStrategy: []string{
			"default to normal user permissions for attended temporary support",
			"request administrator/root privileges only when a signed job explicitly requires them for the repair",
			"use normal OS authorization surfaces such as UAC or sudo prompts; do not hide elevation or weaken execution policy permanently",
			"record elevation requests, decisions, commands, exit codes, and post-change verification in evidence",
		},
		AgentRules: []string{
			"prefer sending a connection entry link, script, or package over asking for raw shell access",
			"do not ask the human to copy ticket codes, root keys, gateway URLs, or transport flags when the invite already contains them",
			"if the connection entry cannot reach the gateway, collect probe evidence and switch to the next configured connection mode before asking the human",
			"keep target-side human steps to open/run/approve/keep-window-open whenever possible",
			"choose managed mode only for owned hosts or explicitly approved durable access; keep third-party hosts temporary by default",
		},
		EvidenceRequired: []string{
			"package verification result",
			"join manifest verification result",
			"selected host mode and ownership/persistence decision basis",
			"selected transport and fallback attempts",
			"host registration and approval status",
			"revocation or stop instructions delivered to the target-side user",
		},
		ImplementationGaps: []string{
			"publish per-platform connection entry packages as release assets",
			"add end-to-end Windows clean-machine acceptance evidence for one-run connection entry startup",
			"add relay/mesh adapter execution paths for environments where direct outbound gateway access fails",
		},
	}
}

func newTargetSelectionPolicy() TargetSelectionPolicy {
	return TargetSelectionPolicy{
		SchemaVersion:         "rdev.target-selection-policy.v1",
		DecisionOwner:         "agent-after-probing-and-when-needed-operator-confirmation",
		DefaultOwnedMode:      "managed",
		DefaultThirdPartyMode: "attended-temporary",
		OwnedSignals: []string{
			"the operator says this is my computer, personal workstation, company workstation, or managed fleet host",
			"the task requires recurring Agent development, long-running maintenance, reconnect after restart, or durable workspace context",
			"an existing owned-host inventory, enrollment certificate, managed service record, or operator policy identifies the machine as managed",
		},
		ThirdPartySignals: []string{
			"the machine belongs to a customer, friend, vendor, or other third party",
			"the task is one-off repair, debugging, setup, or short-lived support",
			"the operator has not explicitly approved persistent service lifecycle or owned-host enrollment",
		},
		AskWhen: []string{
			"ownership is ambiguous and the selected mode would create persistence",
			"the user requests long-term access on a third-party machine",
			"the repair appears to require administrator/root privileges, service installation, firewall, DNS, cloud, paid relay, or security-policy changes",
		},
		AgentRules: []string{
			"decide the target mode from ownership, duration, and persistence approval before materializing the Connection Entry",
			"use managed only for owned or formally managed machines with explicit durable-access approval",
			"use attended-temporary for third-party or one-off machines by default",
			"ask exactly one short question when the mode cannot be decided safely from probes, inventory, or the operator's request",
			"never make target-side humans choose between ticket, root, gateway, transport, release, or checksum values",
		},
	}
}

func connectionEntryHandoffContract() []string {
	return []string{
		"Connection Entry is the universal target-side handoff for every new remote host, whether the result is a link, visible script, or signed package.",
		"Target-side humans must not assemble ticket codes, gateway URLs, manifest roots, transports, release roots, or checksums by hand.",
		"Agent/tool metadata owns low-level connection parameters and records missing_inputs when package materialization needs more release data.",
		"Owned personal or fleet machines default to managed planning for durable Agent work; third-party or one-off machines default to attended-temporary planning.",
	}
}

func placeholderIfEmpty(value, placeholder string) string {
	if strings.TrimSpace(value) == "" {
		return placeholder
	}
	return value
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

func newTransportPlan(rdevCommand, manifestURL, manifestRootPublicKey, transport string, once bool) TransportPlan {
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
			HostCommand:  hostServeCommand(rdevCommand, manifestURL, manifestRootPublicKey, candidate, once),
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

func hostServeCommand(rdevCommand, manifestURL, manifestRootPublicKey, transport string, once bool) string {
	command := fmt.Sprintf("%s host serve --manifest-url %s --transport %s", rdevCommand, shellQuote(manifestURL), shellQuote(transport))
	if strings.TrimSpace(manifestRootPublicKey) != "" {
		command += " --manifest-root-public-key " + shellQuote(manifestRootPublicKey)
	}
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
