package acceptance

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

const FreshAgentSupportSessionReportSchemaVersion = "rdev.acceptance.fresh-agent-support-session.v1"

type FreshAgentSupportSessionOptions struct {
	OutDir      string
	GatewayURL  string
	RdevCommand string
	Locale      string
	Now         time.Time
}

type FreshAgentSupportSessionReport struct {
	SchemaVersion           string           `json:"schema_version"`
	GeneratedAt             time.Time        `json:"generated_at"`
	OutDir                  string           `json:"out_dir"`
	GatewayURL              string           `json:"gateway_url"`
	ConnectNoGateway        map[string]any   `json:"connect_no_gateway"`
	ConnectReachableGateway map[string]any   `json:"connect_reachable_gateway"`
	HandoffNoGateway        map[string]any   `json:"handoff_no_gateway"`
	HandoffReachableGateway map[string]any   `json:"handoff_reachable_gateway"`
	CreatedSession          map[string]any   `json:"created_session"`
	StartedSession          map[string]any   `json:"started_session"`
	StableFallbackSession   map[string]any   `json:"stable_fallback_session"`
	DegradedOverrideSession map[string]any   `json:"degraded_override_session"`
	MainlandEvidence        map[string]any   `json:"mainland_evidence"`
	ShareableAttempts       []map[string]any `json:"shareable_attempts"`
	LifecycleTransitions    map[string]any   `json:"lifecycle_transitions"`
	ConnectedStatus         map[string]any   `json:"connected_status"`
	WaitingRecovery         map[string]any   `json:"waiting_recovery"`
	BootstrapSelfRepair     map[string]any   `json:"bootstrap_self_repair"`
	LiveRemoteE2EGates      []map[string]any `json:"live_remote_e2e_gates"`
	Checks                  []Check          `json:"checks"`
	RecommendedNextSteps    []string         `json:"recommended_next_steps"`
	RealEnvironmentRequired []string         `json:"real_environment_required"`
}

func RunFreshAgentSupportSession(opts FreshAgentSupportSessionOptions) (FreshAgentSupportSessionReport, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return FreshAgentSupportSessionReport{}, fmt.Errorf("out directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return FreshAgentSupportSessionReport{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return FreshAgentSupportSessionReport{}, err
	}
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL = "http://127.0.0.1:8787"
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "en"
	}

	handoffNoGateway := withFreshAgentGatewayEnvCleared(func() map[string]any {
		return supportsession.BuildHandoff(supportsession.HandoffOptions{
			Addr:         "0.0.0.0:8787",
			Target:       "auto",
			Reason:       "fresh Agent support-session acceptance",
			TTLSeconds:   7200,
			AutoActivate: true,
			Locale:       locale,
			RdevCommand:  rdevCommand,
		})
	})
	handoffReachableGateway := supportsession.BuildHandoff(supportsession.HandoffOptions{
		Addr:         "0.0.0.0:8787",
		GatewayURL:   gatewayURL,
		Target:       "auto",
		Reason:       "fresh Agent support-session acceptance",
		TTLSeconds:   7200,
		AutoActivate: true,
		Locale:       locale,
		RdevCommand:  rdevCommand,
	})
	connectNoGateway := supportsession.BuildConnectFromHandoff(handoffNoGateway)
	managedDirectSet := tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates: []tunnel.Candidate{{
			ProviderID: "managed-direct",
			URL:        "https://managed-direct.example.test",
		}},
	}
	managedDirectReadiness := supportsession.DirectAvailability(managedDirectSet, false)
	degradedOverrideReadiness := supportsession.DirectAvailability(managedDirectSet, true)
	mainlandEligibility := tunnel.EvaluateEligibility(
		tunnel.ProviderMetadata{ID: "managed-direct", DefaultAutomatic: true},
		tunnel.Policy{Region: tunnel.RegionCNMainland, Now: now},
		nil,
	)
	mainlandEvidence := map[string]any{
		"region":             tunnel.RegionCNMainland,
		"verified":           mainlandEligibility.Evidence != nil && mainlandEligibility.Evidence.Status == tunnel.EvidenceVerified,
		"eligible":           mainlandEligibility.Eligible,
		"eligibility_reason": mainlandEligibility.Reason,
	}
	rawAttempt := map[string]any{
		"provider_id": "managed-direct",
		"status":      "degraded",
		"error_class": "provider-health-check-failed",
		"failure_domains": map[string]bool{
			"authoritative_dns": true,
			"edge_network":      true,
			"control_plane":     true,
		},
		"known_hosts":  "AAAAC3NzaKnownHostsSecret",
		"token":        "super-secret-token",
		"target_ip":    "198.51.100.77",
		"provider_url": "https://raw-provider.example.test/session/secret",
	}
	shareableAttempts := []map[string]any{redactShareableTunnelAttempt(rawAttempt)}
	lifecycleTransitions := map[string]any{
		"readiness": map[string]any{
			"state":       managedDirectReadiness.State,
			"transitions": []string{"unavailable", "degraded-single-entry"},
		},
		"cleanup": map[string]any{
			"state":       "pending-explicit-stop",
			"transitions": []string{"not-requested", "stop-requested", "cleaned"},
		},
	}

	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicketWithMetadata(
		model.HostModeAttendedTemporary,
		7200,
		policyCapabilitiesToStringsForFreshAgent(policy.TemporaryDefaults()),
		"fresh Agent support-session acceptance",
		map[string]string{
			"connection_entry":    "standard-visible",
			"activation_contract": "target-consent-scoped-ticket",
			"auto_activate":       "attended-temporary",
		},
	)
	if err != nil {
		return FreshAgentSupportSessionReport{}, err
	}
	created := supportsession.BuildCreated(supportsession.CreatedOptions{
		GatewayURL:            gatewayURL,
		GatewayURLCandidates:  supportsession.GatewayURLCandidatesFromIPs("0.0.0.0:8787", gatewayURL, nil),
		ManifestRootPublicKey: manifestRootPublicKeyForFreshAgent(gw.ManifestRoot()),
		Ticket:                ticket,
		Target:                "auto",
		Locale:                locale,
		RdevCommand:           rdevCommand,
		AutoActivate:          true,
		AvailabilityReadiness: managedDirectReadiness,
	})
	connectReachableGateway := supportsession.BuildConnectFromCreated(created)
	started := supportsession.BuildStarted(supportsession.StartedOptions{
		Addr:                  "0.0.0.0:8787",
		GatewayURL:            gatewayURL,
		WorkDir:               filepath.Join(outDir, "support-session"),
		ReadyFile:             filepath.Join(outDir, "support-session", "support-session-ready.json"),
		StatusFile:            filepath.Join(outDir, "support-session", "support-session-status.json"),
		HandoffTextFile:       filepath.Join(outDir, "support-session", "target-handoff.txt"),
		ConnectedReportFile:   filepath.Join(outDir, "support-session", "connected-report.txt"),
		Created:               created,
		AvailabilityReadiness: managedDirectReadiness,
	})
	stableFallback := withFreshAgentGatewayEnv("RDEV_RELAY_GATEWAY_URL", "https://relay.example.test/rdev", func() map[string]any {
		stableURL, stableCandidates := supportsession.ConfiguredGatewayURLCandidate()
		stableTicket, err := gw.CreateTicketWithMetadata(
			model.HostModeAttendedTemporary,
			7200,
			policyCapabilitiesToStringsForFreshAgent(policy.TemporaryDefaults()),
			"fresh Agent stable fallback acceptance",
			map[string]string{
				"connection_entry":    "standard-visible",
				"activation_contract": "target-consent-scoped-ticket",
				"auto_activate":       "attended-temporary",
			},
		)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		handoff := supportsession.BuildHandoff(supportsession.HandoffOptions{
			Addr:         "0.0.0.0:8787",
			Target:       "auto",
			Reason:       "fresh Agent stable fallback acceptance",
			TTLSeconds:   7200,
			AutoActivate: true,
			Locale:       locale,
			RdevCommand:  rdevCommand,
		})
		created := supportsession.BuildCreated(supportsession.CreatedOptions{
			GatewayURL:            stableURL,
			GatewayURLCandidates:  stableCandidates,
			ManifestRootPublicKey: manifestRootPublicKeyForFreshAgent(gw.ManifestRoot()),
			Ticket:                stableTicket,
			Target:                "windows",
			Locale:                locale,
			RdevCommand:           rdevCommand,
			AutoActivate:          true,
			AvailabilityReadiness: supportsession.DirectAvailability(tunnel.AvailabilitySet{
				SchemaVersion: tunnel.AvailabilitySchemaVersion,
				Region:        tunnel.RegionGlobal,
				Candidates: []tunnel.Candidate{{
					ProviderID: "configured-relay",
					URL:        stableURL,
				}},
			}, false),
		})
		return map[string]any{
			"schema_version": "rdev.acceptance.stable-fallback-session.v1",
			"env_var":        "RDEV_RELAY_GATEWAY_URL",
			"gateway_url":    stableURL,
			"handoff":        handoff,
			"created":        created,
		}
	})
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "fresh-agent-acceptance-host",
		OS:           "linux",
		Arch:         "amd64",
		Capabilities: ticket.Capabilities,
	})
	if err != nil {
		return FreshAgentSupportSessionReport{}, err
	}
	connectedStatus := supportsession.BuildStatus(supportsession.StatusOptions{
		TicketCode: ticket.Code,
		Hosts:      gw.HostsForTicketCode(ticket.Code, ""),
		Locale:     locale,
	})
	waitingRecovery := supportsession.BuildConnectionRecovery(supportsession.ConnectionRecoveryOptions{
		Status:     "waiting",
		TicketCode: ticket.Code,
		Locale:     locale,
		TimedOut:   true,
	})
	bootstrapSelfRepair, bootstrapChecks, err := buildBootstrapSelfRepairContract(outDir, now, mapFromAny(created["rdev_bootstrap_connector"]))
	if err != nil {
		return FreshAgentSupportSessionReport{}, err
	}

	report := FreshAgentSupportSessionReport{
		SchemaVersion:           FreshAgentSupportSessionReportSchemaVersion,
		GeneratedAt:             now.UTC(),
		OutDir:                  outDir,
		GatewayURL:              gatewayURL,
		ConnectNoGateway:        connectNoGateway,
		ConnectReachableGateway: connectReachableGateway,
		HandoffNoGateway:        handoffNoGateway,
		HandoffReachableGateway: handoffReachableGateway,
		CreatedSession:          created,
		StartedSession:          started,
		StableFallbackSession:   stableFallback,
		DegradedOverrideSession: map[string]any{
			"schema_version":         "rdev.acceptance.degraded-override-session.v1",
			"availability_readiness": degradedOverrideReadiness,
			"prebootstrap_failover":  false,
		},
		MainlandEvidence:     mainlandEvidence,
		ShareableAttempts:    shareableAttempts,
		LifecycleTransitions: lifecycleTransitions,
		ConnectedStatus:      connectedStatus,
		WaitingRecovery:      waitingRecovery,
		BootstrapSelfRepair:  bootstrapSelfRepair,
		LiveRemoteE2EGates:   liveRemoteE2EGates(gatewayURL, rdevCommand),
		Checks: freshAgentSupportSessionChecks(freshAgentSupportSessionCheckInput{
			HandoffNoGateway:        handoffNoGateway,
			HandoffReachableGateway: handoffReachableGateway,
			ConnectNoGateway:        connectNoGateway,
			ConnectReachableGateway: connectReachableGateway,
			CreatedSession:          created,
			StartedSession:          started,
			StableFallbackSession:   stableFallback,
			ConnectedStatus:         connectedStatus,
			WaitingRecovery:         waitingRecovery,
			Host:                    host,
			Ticket:                  ticket,
		}),
		RecommendedNextSteps: []string{
			"Use this contract gate before fresh-Agent multi-harness acceptance to catch regressions in the standard connect/handoff/create/start/status flow.",
			"Run real Codex, Claude Code, Hermes, and OpenClaw/OpenCode acceptance next; this local report does not prove model behavior in those runtimes.",
			"Run clean Windows/macOS/Linux target acceptance and restrictive-network relay/mesh/VPN/SSH evidence before claiming production-grade connectivity.",
		},
		RealEnvironmentRequired: []string{
			"fresh-agent Codex/Claude Code/Hermes/OpenClaw/OpenCode runs",
			"clean Windows/macOS/Linux target machines",
			"live Windows attended-temporary support session with support-session smoke-test --remote-control evidence",
			"live Windows file upload/download byte_compare=match evidence",
			"live Windows session interrupt/replay evidence through rdev.sessions.interrupt",
			"real LAN departure or restrictive-network relay/mesh/VPN/SSH paths",
		},
	}
	report.Checks = append(report.Checks, bootstrapChecks...)
	report.Checks = append(report.Checks, regionalTunnelAcceptanceChecks(report)...)
	if err := writeFreshAgentSupportSessionReport(filepath.Join(outDir, "report.json"), report); err != nil {
		return FreshAgentSupportSessionReport{}, err
	}
	return report, nil
}

func liveRemoteE2EGates(gatewayURL, rdevCommand string) []map[string]any {
	plan := supportsession.BuildLiveE2EPlan(supportsession.LiveE2EPlanOptions{
		GatewayURL:     gatewayURL,
		RdevCommand:    rdevCommand,
		TimeoutSeconds: 180,
	})
	gates, _ := plan["gates"].([]map[string]any)
	return gates
}

func regionalTunnelAcceptanceChecks(report FreshAgentSupportSessionReport) []Check {
	stableCreated := mapFromAny(report.StableFallbackSession["created"])
	stable := availabilityReadinessForAcceptance(stableCreated["availability_readiness"])
	managedDirect := availabilityReadinessForAcceptance(report.StartedSession["availability_readiness"])
	override := availabilityReadinessForAcceptance(report.DegradedOverrideSession["availability_readiness"])
	mainlandVerified := boolFromAny(report.MainlandEvidence["verified"])
	mainlandReason := stringFromAny(report.MainlandEvidence["eligibility_reason"])
	shareable, _ := json.Marshal(report.ShareableAttempts)
	shareableText := string(shareable)
	redacted := true
	for _, forbidden := range []string{
		"AAAAC3NzaKnownHostsSecret",
		"super-secret-token",
		"198.51.100.77",
		"https://raw-provider.example.test/session/secret",
	} {
		redacted = redacted && !strings.Contains(shareableText, forbidden)
	}
	readinessTransition := mapFromAny(report.LifecycleTransitions["readiness"])
	cleanupTransition := mapFromAny(report.LifecycleTransitions["cleanup"])
	return []Check{
		{
			Name:   "stable_gateway_is_degraded_without_override",
			Passed: stable.State == "degraded-single-entry" && !stable.ReadyToSend,
			Detail: stable.State,
		},
		{
			Name:   "managed_direct_tunnel_is_degraded_without_override",
			Passed: managedDirect.State == "degraded-single-entry" && !managedDirect.ReadyToSend,
			Detail: managedDirect.State,
		},
		{
			Name:   "explicit_override_is_sendable_but_degraded",
			Passed: override.State == "degraded-single-entry" && override.ReadyToSend && !override.ReadyToActivate && !override.ReadyToExecute,
			Detail: override.State,
		},
		{
			Name:   "cn_mainland_missing_evidence_is_not_verified",
			Passed: !mainlandVerified && mainlandReason == "regional-evidence-missing",
			Detail: mainlandReason,
		},
		{
			Name:   "direct_mode_cannot_claim_prebootstrap_failover",
			Passed: report.DegradedOverrideSession["prebootstrap_failover"] == false && len(override.AvailabilitySet.Candidates) == 1,
			Detail: fmt.Sprintf("candidates=%d", len(override.AvailabilitySet.Candidates)),
		},
		{
			Name:   "shareable_attempts_redact_protected_material",
			Passed: redacted,
			Detail: shareableText,
		},
		{
			Name:   "cleanup_and_readiness_transitions_are_independent",
			Passed: stringFromAny(readinessTransition["state"]) == "degraded-single-entry" && stringFromAny(cleanupTransition["state"]) == "pending-explicit-stop",
			Detail: fmt.Sprintf("readiness=%s cleanup=%s", stringFromAny(readinessTransition["state"]), stringFromAny(cleanupTransition["state"])),
		},
	}
}

func redactShareableTunnelAttempt(attempt map[string]any) map[string]any {
	failureDomains := map[string]bool{}
	switch domains := attempt["failure_domains"].(type) {
	case map[string]bool:
		for name, configured := range domains {
			failureDomains[name] = configured
		}
	case map[string]any:
		for name, configured := range domains {
			failureDomains[name] = boolFromAny(configured)
		}
	}
	return map[string]any{
		"provider_id":     stringFromAny(attempt["provider_id"]),
		"status":          stringFromAny(attempt["status"]),
		"error_class":     stringFromAny(attempt["error_class"]),
		"failure_domains": failureDomains,
		"credentials":     "redacted",
		"target":          "redacted",
	}
}

func availabilityReadinessForAcceptance(value any) supportsession.AvailabilityReadiness {
	if readiness, ok := value.(supportsession.AvailabilityReadiness); ok {
		return readiness
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return supportsession.AvailabilityReadiness{}
	}
	var readiness supportsession.AvailabilityReadiness
	if err := json.Unmarshal(encoded, &readiness); err != nil {
		return supportsession.AvailabilityReadiness{}
	}
	return readiness
}

type freshAgentSupportSessionCheckInput struct {
	HandoffNoGateway        map[string]any
	HandoffReachableGateway map[string]any
	ConnectNoGateway        map[string]any
	ConnectReachableGateway map[string]any
	CreatedSession          map[string]any
	StartedSession          map[string]any
	StableFallbackSession   map[string]any
	ConnectedStatus         map[string]any
	WaitingRecovery         map[string]any
	Host                    model.Host
	Ticket                  model.Ticket
}

func freshAgentSupportSessionChecks(input freshAgentSupportSessionCheckInput) []Check {
	noGatewayCommand := stringSliceFromAny(input.HandoffNoGateway["foreground_start_command"])
	noGatewayStartNowCommand := stringSliceFromAny(input.HandoffNoGateway["cli_start_now_command"])
	reachableArgs := mapFromAny(input.HandoffReachableGateway["mcp_next_arguments"])
	connectStartCommand := stringSliceFromAny(input.ConnectNoGateway["foreground_start_command"])
	connectStartNowCommand := stringSliceFromAny(input.ConnectNoGateway["cli_start_now_command"])
	connectUserHandoff := mapFromAny(input.ConnectReachableGateway["user_handoff"])
	connectEnvelope := mapFromAny(input.ConnectReachableGateway["target_handoff_envelope"])
	connectHelperPreflight := mapFromAny(input.ConnectReachableGateway["connectivity_helper_preflight"])
	connectRunnerRecommendation := mapFromAny(input.ConnectReachableGateway["connection_entry_runner_recommendation"])
	connectContract := mapFromAny(input.ConnectReachableGateway["fresh_agent_connect_contract"])
	handoff := mapFromAny(input.CreatedSession["user_handoff"])
	envelope := mapFromAny(input.CreatedSession["target_handoff_envelope"])
	mcpFollowUp := mapSliceFromAny(input.CreatedSession["mcp_follow_up"])
	configuredWatcher := mapFromAny(input.CreatedSession["watch_connection_status_configured_gateway"])
	supervision := mapFromAny(input.CreatedSession["connection_supervision"])
	preflight := mapFromAny(input.CreatedSession["gateway_candidate_preflight"])
	helperPreflight := mapFromAny(input.CreatedSession["connectivity_helper_preflight"])
	runnerRecommendation := mapFromAny(input.CreatedSession["connection_entry_runner_recommendation"])
	createdContract := mapFromAny(input.CreatedSession["fresh_agent_connect_contract"])
	runbook := mapFromAny(input.CreatedSession["agent_connection_runbook"])
	runbookStandardEntry := mapFromAny(runbook["standard_entry_tool"])
	runbookLowLevelRule := mapFromAny(runbook["low_level_entry_rule"])
	runbookFailurePrevention := mapFromAny(runbook["fresh_agent_failure_prevention"])
	readyFile := mapFromAny(input.StartedSession["ready_file"])
	statusFile := mapFromAny(input.StartedSession["status_file"])
	handoffTextFile := mapFromAny(input.StartedSession["handoff_text_file"])
	connectedReportFile := mapFromAny(input.StartedSession["connected_report_file"])
	foregroundFeedback := mapFromAny(input.StartedSession["foreground_feedback"])
	session := mapFromAny(input.StartedSession["session"])
	startedHandoff := mapFromAny(input.StartedSession["user_handoff"])
	startedEnvelope := mapFromAny(input.StartedSession["target_handoff_envelope"])
	startedSupervision := mapFromAny(input.StartedSession["connection_supervision"])
	startedPreflight := mapFromAny(input.StartedSession["gateway_candidate_preflight"])
	startedHelperPreflight := mapFromAny(input.StartedSession["connectivity_helper_preflight"])
	startedRunnerRecommendation := mapFromAny(input.StartedSession["connection_entry_runner_recommendation"])
	startedContract := mapFromAny(input.StartedSession["fresh_agent_connect_contract"])
	startedRunbook := mapFromAny(input.StartedSession["agent_connection_runbook"])
	stableFallbackCreated := mapFromAny(input.StableFallbackSession["created"])
	stableFallbackHandoff := mapFromAny(input.StableFallbackSession["handoff"])
	stableFallbackHandoffArgs := mapFromAny(stableFallbackHandoff["mcp_next_arguments"])
	stableFallbackContinuity := mapFromAny(stableFallbackCreated["connection_continuity_policy"])
	stableFallbackSupervision := mapFromAny(stableFallbackCreated["connection_supervision"])
	stableFallbackRunbook := mapFromAny(stableFallbackCreated["agent_connection_runbook"])
	stableFallbackRunbookSummary := mapFromAny(stableFallbackRunbook["gateway_candidate_summary"])
	connectedNext := mapFromAny(input.ConnectedStatus["connected_next_steps"])
	statusRunbook := mapFromAny(input.ConnectedStatus["agent_connection_runbook"])
	recoveryRunbook := mapFromAny(input.WaitingRecovery["agent_connection_runbook"])
	recoveryForbidden := strings.Join(stringSliceFromAny(input.WaitingRecovery["forbidden"]), "\n")
	copyPaste := stringFromAny(handoff["copy_paste"])
	targetCommand := stringFromAny(input.CreatedSession["target_command"])
	forbiddenText := strings.Join(stringSliceFromAny(input.CreatedSession["forbidden"]), "\n") + "\n" + targetCommand + "\n" + copyPaste
	checks := []Check{
		{Name: "connect_without_gateway_returns_start_now_command", Passed: input.ConnectNoGateway["schema_version"] == supportsession.ConnectSchemaVersion && input.ConnectNoGateway["selected_path"] == "start-foreground-gateway" && input.ConnectNoGateway["ready_to_send_to_human"] == false && containsAllStrings(connectStartNowCommand, "support-session", "connect", "--start") && containsAllStrings(connectStartCommand, "support-session", "start"), Detail: strings.Join(connectStartNowCommand, " ")},
		{Name: "connect_with_gateway_returns_ready_handoff", Passed: input.ConnectReachableGateway["schema_version"] == supportsession.ConnectSchemaVersion && input.ConnectReachableGateway["selected_path"] == "created-with-reachable-gateway" && input.ConnectReachableGateway["ready_to_send_to_human"] == false && input.ConnectReachableGateway["ready_to_send"] == false && input.ConnectReachableGateway["ready_to_activate"] == false && input.ConnectReachableGateway["ready_to_execute"] == false && stringFromAny(connectUserHandoff["schema_version"]) == supportsession.UserHandoffSchemaVersion, Detail: stringFromAny(connectUserHandoff["copy_paste_kind"])},
		{Name: "connect_with_gateway_returns_forwardable_envelope", Passed: stringFromAny(connectEnvelope["schema_version"]) == supportsession.TargetHandoffEnvelopeSchemaVersion && !boolFromAny(connectEnvelope["ready_to_forward"]) && strings.Contains(stringFromAny(connectEnvelope["full_text"]), stringFromAny(connectEnvelope["copy_paste"])) && strings.Contains(strings.ToLower(stringFromAny(connectEnvelope["agent_rule"])), "do not send"), Detail: stringFromAny(connectEnvelope["copy_paste_kind"])},
		{Name: "connect_with_gateway_has_top_level_helper_preflight", Passed: stringFromAny(connectHelperPreflight["schema_version"]) == supportsession.ConnectivityHelperPreflightSchemaVersion && strings.Contains(strings.Join(stringSliceFromAny(connectHelperPreflight["forbidden"]), "\n"), "ExecutionPolicy Bypass"), Detail: fmt.Sprintf("%v", connectHelperPreflight["configured_helper_ids"])},
		{Name: "connect_with_gateway_has_runner_recommendation", Passed: stringFromAny(connectRunnerRecommendation["schema_version"]) == supportsession.ConnectionEntryRunnerRecommendationSchemaVersion && stringFromAny(connectRunnerRecommendation["standard_tool"]) == "rdev.connection_entry.plan" && strings.TrimSpace(stringFromAny(connectRunnerRecommendation["invite_json"])) != "", Detail: stringFromAny(connectRunnerRecommendation["target_os"])},
		{Name: "connect_with_gateway_has_fresh_agent_contract", Passed: stringFromAny(connectContract["schema_version"]) == supportsession.FreshAgentConnectContractSchemaVersion && !boolFromAny(connectContract["ready_to_send_human"]) && !boolFromAny(connectContract["ready_to_send"]) && !boolFromAny(connectContract["ready_to_activate"]) && !boolFromAny(connectContract["ready_to_execute"]) && strings.Contains(stringFromAny(connectContract["human_surface"]), "target_handoff_envelope.full_text") && strings.Contains(strings.Join(stringSliceFromAny(connectContract["do_not_ask_human_for"]), "\n"), "gateway URL") && strings.Contains(strings.Join(stringSliceFromAny(connectContract["agent_must_not_generate"]), "\n"), "PowerShell bootstrap code"), Detail: fmt.Sprintf("%v", connectContract)},
		{Name: "handoff_without_gateway_selects_foreground_start", Passed: input.HandoffNoGateway["selected_path"] == "start-foreground-gateway", Detail: stringFromAny(input.HandoffNoGateway["selected_path"])},
		{Name: "handoff_without_gateway_prefers_connect_start", Passed: containsAllStrings(noGatewayStartNowCommand, "support-session", "connect", "--start"), Detail: strings.Join(noGatewayStartNowCommand, " ")},
		{Name: "foreground_start_command_is_standard_tool", Passed: containsAllStrings(noGatewayCommand, "support-session", "start"), Detail: strings.Join(noGatewayCommand, " ")},
		{Name: "handoff_with_gateway_selects_create_tool", Passed: input.HandoffReachableGateway["selected_path"] == "create-with-reachable-gateway" && input.HandoffReachableGateway["mcp_next_tool"] == "rdev.support_session.create", Detail: stringFromAny(input.HandoffReachableGateway["selected_path"])},
		{Name: "create_arguments_include_gateway_and_waitable_target", Passed: stringFromAny(reachableArgs["gateway_url"]) != "" && stringFromAny(reachableArgs["target"]) == "auto", Detail: fmt.Sprintf("%v", reachableArgs)},
		{Name: "created_session_has_one_user_handoff", Passed: stringFromAny(handoff["schema_version"]) == supportsession.UserHandoffSchemaVersion && copyPaste != "", Detail: stringFromAny(handoff["copy_paste_kind"])},
		{Name: "created_session_has_forwardable_envelope", Passed: stringFromAny(envelope["schema_version"]) == supportsession.TargetHandoffEnvelopeSchemaVersion && stringFromAny(envelope["copy_paste"]) == copyPaste && strings.Contains(stringFromAny(envelope["full_text"]), copyPaste) && strings.Contains(strings.Join(stringSliceFromAny(envelope["forbidden"]), "\n"), "manual ticket/root/gateway/transport"), Detail: stringFromAny(envelope["copy_paste_kind"])},
		{Name: "created_session_copy_paste_is_not_rewritten_placeholder", Passed: copyPaste == targetCommand && !strings.Contains(copyPaste, "<ticket-code>") && !strings.Contains(copyPaste, "ExecutionPolicy Bypass"), Detail: copyPaste},
		{Name: "created_session_has_waiting_mcp_followup", Passed: len(mcpFollowUp) > 0 && stringFromAny(mcpFollowUp[0]["tool"]) == "rdev.support_session.status" && boolFromAny(mapFromAny(mcpFollowUp[0]["arguments"])["wait"]), Detail: fmt.Sprintf("%v", mcpFollowUp)},
		{Name: "configured_gateway_watcher_omits_gateway_url", Passed: !strings.Contains(strings.Join(stringSliceFromAny(configuredWatcher["command"]), " "), "--gateway-url"), Detail: strings.Join(stringSliceFromAny(configuredWatcher["command"]), " ")},
		{Name: "created_session_has_connection_supervision", Passed: stringFromAny(supervision["schema_version"]) == supportsession.ConnectionSupervisionSchemaVersion && stringFromAny(mapFromAny(supervision["mcp_watch_call"])["tool"]) == "rdev.support_session.status" && boolFromAny(mapFromAny(mapFromAny(supervision["mcp_watch_call"])["arguments"])["wait"]) && strings.Contains(stringFromAny(supervision["connected_report_rule"]), "connected_next_steps.user_report"), Detail: stringFromAny(supervision["upgrade_reason"])},
		{Name: "connection_supervision_covers_signed_candidate_runtime_failover", Passed: strings.Contains(strings.Join(stringSliceFromAny(supervision["automatic_downgrade_boundaries"]), "\n"), "signed join-manifest gateway candidates"), Detail: strings.Join(stringSliceFromAny(supervision["automatic_downgrade_boundaries"]), " | ")},
		{Name: "created_session_has_gateway_candidate_preflight", Passed: stringFromAny(preflight["schema_version"]) == supportsession.GatewayCandidatePreflightSchemaVersion && intFromAny(preflight["candidate_count"]) > 0 && strings.Contains(stringFromAny(preflight["agent_rule"]), "target command owns ordered URL fallback"), Detail: stringFromAny(preflight["preflight_mode"])},
		{Name: "created_session_has_connectivity_helper_preflight", Passed: stringFromAny(helperPreflight["schema_version"]) == supportsession.ConnectivityHelperPreflightSchemaVersion && stringFromAny(helperPreflight["agent_rule"]) != "" && strings.Contains(strings.Join(stringSliceFromAny(helperPreflight["forbidden"]), "\n"), "ExecutionPolicy Bypass"), Detail: fmt.Sprintf("%v", helperPreflight["configured_helper_ids"])},
		{Name: "created_session_has_connection_entry_runner_recommendation", Passed: stringFromAny(runnerRecommendation["schema_version"]) == supportsession.ConnectionEntryRunnerRecommendationSchemaVersion && stringFromAny(mapFromAny(runnerRecommendation["mcp_plan_call"])["tool"]) == "rdev.connection_entry.plan" && strings.Contains(strings.Join(stringSliceFromAny(runnerRecommendation["agent_sequence"]), "\n"), "dry-run the generated runner") && strings.Contains(strings.Join(stringSliceFromAny(runnerRecommendation["forbidden"]), "\n"), "Agent-authored SSH"), Detail: stringFromAny(runnerRecommendation["target_os"])},
		{Name: "created_session_has_fresh_agent_contract", Passed: stringFromAny(createdContract["schema_version"]) == supportsession.FreshAgentConnectContractSchemaVersion && !boolFromAny(createdContract["ready_to_send_human"]) && !boolFromAny(createdContract["ready_to_send"]) && !boolFromAny(createdContract["ready_to_activate"]) && !boolFromAny(createdContract["ready_to_execute"]) && strings.Contains(strings.Join(stringSliceFromAny(createdContract["do_not_ask_human_for"]), "\n"), "ticket code") && strings.Contains(strings.Join(stringSliceFromAny(createdContract["agent_must_not_generate"]), "\n"), "ticket/root/gateway substitution scripts"), Detail: fmt.Sprintf("%v", createdContract)},
		{Name: "created_session_has_agent_connection_runbook", Passed: stringFromAny(runbook["schema_version"]) == supportsession.AgentConnectionRunbookSchemaVersion && strings.Contains(strings.Join(stringSliceFromAny(runbook["sequence"]), "\n"), "target_handoff_envelope.full_text") && strings.Contains(strings.Join(stringSliceFromAny(runbook["forbidden"]), "\n"), "Agent-authored PowerShell"), Detail: stringFromAny(runbook["phase"])},
		{Name: "agent_runbook_starts_with_support_session_connect", Passed: stringFromAny(runbookStandardEntry["mcp_tool"]) == "rdev.support_session.connect" && strings.Contains(strings.Join(stringSliceFromAny(runbookStandardEntry["cli_command"]), " "), "support-session connect"), Detail: fmt.Sprintf("%v", runbookStandardEntry)},
		{Name: "agent_runbook_forbids_low_level_invite_first", Passed: strings.Contains(strings.Join(stringSliceFromAny(runbookLowLevelRule["do_not_start_with"]), "\n"), "rdev.invites.create") && strings.Contains(strings.Join(stringSliceFromAny(runbookLowLevelRule["do_not_start_with"]), "\n"), "rdev.connection_entry.plan"), Detail: fmt.Sprintf("%v", runbookLowLevelRule)},
		{Name: "agent_runbook_contains_real_failure_prevention", Passed: stringFromAny(runbookFailurePrevention["schema_version"]) == supportsession.FreshAgentFailurePreventionSchemaVersion && strings.Contains(strings.Join(stringSliceFromAny(runbookFailurePrevention["known_failure_pattern"]), "\n"), "rdev is required") && strings.Contains(strings.Join(stringSliceFromAny(runbookFailurePrevention["required_standard_path"]), "\n"), "cli_start_now_command") && strings.Contains(strings.Join(stringSliceFromAny(runbookFailurePrevention["forbidden_agent_generated_workarounds"]), "\n"), "ExecutionPolicy Bypass"), Detail: fmt.Sprintf("%v", runbookFailurePrevention)},
		{Name: "started_payload_has_top_level_handoff", Passed: input.StartedSession["ready_to_send_to_human"] == false && input.StartedSession["ready_to_send"] == false && input.StartedSession["ready_to_activate"] == false && input.StartedSession["ready_to_execute"] == false && stringFromAny(startedHandoff["schema_version"]) == supportsession.UserHandoffSchemaVersion && stringFromAny(startedHandoff["copy_paste"]) == stringFromAny(input.StartedSession["target_command"]), Detail: stringFromAny(startedHandoff["copy_paste_kind"])},
		{Name: "started_payload_has_top_level_forwardable_envelope", Passed: stringFromAny(startedEnvelope["schema_version"]) == supportsession.TargetHandoffEnvelopeSchemaVersion && !boolFromAny(startedEnvelope["ready_to_forward"]) && stringFromAny(startedEnvelope["copy_paste"]) == stringFromAny(input.StartedSession["target_command"]) && strings.Contains(strings.ToLower(stringFromAny(startedEnvelope["after_send"])), "do not send"), Detail: stringFromAny(startedEnvelope["copy_paste_kind"])},
		{Name: "started_payload_has_top_level_supervision", Passed: stringFromAny(startedSupervision["schema_version"]) == supportsession.ConnectionSupervisionSchemaVersion && stringFromAny(startedSupervision["ticket_code"]) == input.Ticket.Code, Detail: stringFromAny(startedSupervision["continuity_assessment"])},
		{Name: "started_payload_has_top_level_gateway_preflight", Passed: stringFromAny(startedPreflight["schema_version"]) == supportsession.GatewayCandidatePreflightSchemaVersion && intFromAny(startedPreflight["candidate_count"]) > 0, Detail: stringFromAny(startedPreflight["preflight_mode"])},
		{Name: "started_payload_has_top_level_helper_preflight", Passed: stringFromAny(startedHelperPreflight["schema_version"]) == supportsession.ConnectivityHelperPreflightSchemaVersion && strings.Contains(stringFromAny(startedHelperPreflight["agent_rule"]), "Connection Entry runner"), Detail: fmt.Sprintf("%v", startedHelperPreflight["configured_helper_ids"])},
		{Name: "started_payload_has_top_level_runner_recommendation", Passed: stringFromAny(startedRunnerRecommendation["schema_version"]) == supportsession.ConnectionEntryRunnerRecommendationSchemaVersion && strings.TrimSpace(stringFromAny(startedRunnerRecommendation["invite_json"])) != "", Detail: stringFromAny(startedRunnerRecommendation["target_os"])},
		{Name: "started_payload_has_fresh_agent_contract", Passed: stringFromAny(startedContract["schema_version"]) == supportsession.FreshAgentConnectContractSchemaVersion && !boolFromAny(startedContract["ready_to_send_human"]) && !boolFromAny(startedContract["ready_to_send"]) && !boolFromAny(startedContract["ready_to_activate"]) && !boolFromAny(startedContract["ready_to_execute"]) && strings.Contains(stringFromAny(startedContract["status_file_path"]), "support-session-status.json") && strings.Contains(strings.Join(stringSliceFromAny(startedContract["recovery_if_rdev_missing"]), "\n"), "go install ./cmd/rdev"), Detail: fmt.Sprintf("%v", startedContract)},
		{Name: "started_payload_has_top_level_agent_runbook", Passed: stringFromAny(startedRunbook["schema_version"]) == supportsession.AgentConnectionRunbookSchemaVersion && strings.Contains(fmt.Sprintf("%v", startedRunbook["watch"]), "rdev.support_session.status"), Detail: stringFromAny(startedRunbook["phase"])},
		{Name: "started_payload_has_foreground_feedback", Passed: stringFromAny(foregroundFeedback["schema_version"]) == "rdev.support-session-foreground-feedback.v1" && stringFromAny(foregroundFeedback["event_prefix"]) == "rdev support session event: " && strings.Contains(stringFromAny(foregroundFeedback["connected_rule"]), "connection has been established"), Detail: stringFromAny(foregroundFeedback["event_prefix"])},
		{Name: "started_payload_exposes_ready_file", Passed: stringFromAny(readyFile["schema_version"]) == "rdev.support-session-ready-file.v1" && strings.Contains(stringFromAny(readyFile["path"]), "support-session-ready.json"), Detail: stringFromAny(readyFile["path"])},
		{Name: "started_payload_exposes_status_file", Passed: stringFromAny(statusFile["schema_version"]) == supportsession.StatusFileSchemaVersion && strings.Contains(stringFromAny(statusFile["path"]), "support-session-status.json") && strings.Contains(stringFromAny(statusFile["agent_rule"]), "connected_next_steps.user_report"), Detail: stringFromAny(statusFile["path"])},
		{Name: "started_payload_exposes_handoff_text_file", Passed: stringFromAny(handoffTextFile["schema_version"]) == supportsession.HandoffTextFileSchemaVersion && strings.Contains(stringFromAny(handoffTextFile["path"]), "target-handoff.txt") && strings.Contains(strings.ToLower(stringFromAny(handoffTextFile["agent_rule"])), "do not send"), Detail: stringFromAny(handoffTextFile["path"])},
		{Name: "started_payload_exposes_connected_report_file", Passed: stringFromAny(connectedReportFile["schema_version"]) == supportsession.ConnectedReportFileSchemaVersion && strings.Contains(stringFromAny(connectedReportFile["path"]), "connected-report.txt") && strings.Contains(stringFromAny(connectedReportFile["agent_rule"]), "plain text"), Detail: stringFromAny(connectedReportFile["path"])},
		{Name: "started_payload_embeds_created_session", Passed: stringFromAny(session["schema_version"]) == supportsession.CreatedSchemaVersion && stringFromAny(session["ticket_code"]) == input.Ticket.Code, Detail: stringFromAny(session["ticket_code"])},
		{Name: "stable_fallback_handoff_uses_configured_gateway", Passed: stringFromAny(stableFallbackHandoff["selected_path"]) == "create-with-reachable-gateway" && stringFromAny(stableFallbackHandoffArgs["gateway_url"]) == "https://relay.example.test/rdev", Detail: fmt.Sprintf("%v", stableFallbackHandoffArgs)},
		{Name: "stable_fallback_created_uses_relay_candidate", Passed: strings.Contains(stringFromAny(stableFallbackCreated["target_command"]), "https://relay.example.test/rdev/join/") && !strings.Contains(stringFromAny(stableFallbackCreated["target_command"]), "gateway_url_candidates=") && strings.Contains(stringFromAny(mapFromAny(stableFallbackCreated["user_handoff"])["copy_paste"]), "https://relay.example.test/rdev/join/"), Detail: stringFromAny(stableFallbackCreated["target_command"])},
		{Name: "stable_fallback_continuity_is_durable", Passed: boolFromAny(stableFallbackContinuity["stable_after_lan_change"]) && strings.Contains(strings.Join(stringSliceFromAny(stableFallbackContinuity["stable_fallback_kinds"]), "\n"), "relay"), Detail: fmt.Sprintf("%v", stableFallbackContinuity)},
		{Name: "stable_fallback_supervision_does_not_request_upgrade", Passed: stableFallbackSupervision["upgrade_recommended"] == false && strings.Contains(stringFromAny(stableFallbackSupervision["upgrade_reason"]), "stable hosted/relay/mesh/VPN/SSH fallback already configured"), Detail: stringFromAny(stableFallbackSupervision["upgrade_reason"])},
		{Name: "stable_fallback_runbook_reports_stable_candidate", Passed: boolFromAny(stableFallbackRunbookSummary["has_stable_configured_fallback"]) && strings.Contains(strings.Join(stringSliceFromAny(stableFallbackRunbookSummary["candidate_kinds"]), "\n"), "relay"), Detail: fmt.Sprintf("%v", stableFallbackRunbookSummary)},
		{Name: "auto_activation_connects_first_attended_host", Passed: input.Host.Status == model.HostStatusActive && input.ConnectedStatus["connected"] == true, Detail: string(input.Host.Status)},
		{Name: "connected_status_has_user_report", Passed: stringFromAny(connectedNext["schema_version"]) == supportsession.ConnectedNextStepsSchemaVersion && strings.TrimSpace(stringFromAny(connectedNext["user_report"])) != "", Detail: stringFromAny(connectedNext["user_report"])},
		{Name: "connected_status_points_to_session_probe", Passed: strings.Contains(fmt.Sprintf("%v", connectedNext["mcp_next_calls"]), "rdev.sessions.status"), Detail: fmt.Sprintf("%v", connectedNext["mcp_next_calls"])},
		{Name: "connected_status_has_agent_runbook", Passed: stringFromAny(statusRunbook["schema_version"]) == supportsession.AgentConnectionRunbookSchemaVersion && stringFromAny(statusRunbook["status"]) == "connected", Detail: stringFromAny(statusRunbook["phase"])},
		{Name: "waiting_recovery_has_agent_runbook", Passed: stringFromAny(recoveryRunbook["schema_version"]) == supportsession.AgentConnectionRunbookSchemaVersion && strings.Contains(strings.Join(stringSliceFromAny(recoveryRunbook["on_timeout_or_failure"]), "\n"), "gateway_candidate_preflight"), Detail: stringFromAny(recoveryRunbook["phase"])},
		{Name: "waiting_recovery_forbids_custom_scripts", Passed: strings.Contains(recoveryForbidden, "Agent-authored PowerShell") && strings.Contains(recoveryForbidden, "manual ticket/root/gateway/transport"), Detail: recoveryForbidden},
		{Name: "fresh_agent_surface_forbids_unsafe_shortcuts", Passed: strings.Contains(forbiddenText, "hidden install") && strings.Contains(forbiddenText, "ExecutionPolicy Bypass"), Detail: forbiddenText},
	}
	return checks
}

func buildBootstrapSelfRepairContract(outDir string, now time.Time, bootstrapConnector map[string]any) (map[string]any, []Check, error) {
	assetDir := filepath.Join(outDir, "bootstrap-self-repair-assets")
	if err := os.MkdirAll(assetDir, 0o700); err != nil {
		return nil, nil, err
	}
	assets := map[string]string{
		"rdev-windows-amd64.exe": "fake windows rdev helper\n",
		"rdev-darwin-arm64":      "fake darwin arm64 rdev helper\n",
		"rdev-darwin-amd64":      "fake darwin amd64 rdev helper\n",
		"rdev-linux-amd64":       "fake linux amd64 rdev helper\n",
		"rdev-linux-arm64":       "fake linux arm64 rdev helper\n",
	}
	assetPaths := map[string]string{}
	assetSHA256 := map[string]string{}
	for name, content := range assets {
		path := filepath.Join(assetDir, name)
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			return nil, nil, err
		}
		assetPaths[name] = path
		sum := sha256.Sum256([]byte(content))
		assetSHA256[name] = fmt.Sprintf("%x", sum[:])
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicketWithMetadata(
		model.HostModeAttendedTemporary,
		7200,
		policyCapabilitiesToStringsForFreshAgent(policy.TemporaryDefaults()),
		"fresh Agent bootstrap self-repair acceptance",
		map[string]string{
			"connection_entry":    "standard-visible",
			"activation_contract": "target-consent-scoped-ticket",
			"auto_activate":       "attended-temporary",
		},
	)
	if err != nil {
		return nil, nil, err
	}
	server := httpapi.NewServer(gw)
	server.Assets = httpapi.AssetConfig{
		RdevWindowsAMD64Path: assetPaths["rdev-windows-amd64.exe"],
		RdevDarwinARM64Path:  assetPaths["rdev-darwin-arm64"],
		RdevDarwinAMD64Path:  assetPaths["rdev-darwin-amd64"],
		RdevLinuxAMD64Path:   assetPaths["rdev-linux-amd64"],
		RdevLinuxARM64Path:   assetPaths["rdev-linux-arm64"],
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	joinBase := httpServer.URL + "/join/" + ticket.Code
	joinPage, err := fetchAcceptanceText(httpServer.URL + "/join/" + ticket.Code)
	if err != nil {
		return nil, nil, err
	}
	windowsBootstrap, err := fetchAcceptanceText(joinBase + "/bootstrap.ps1")
	if err != nil {
		return nil, nil, err
	}
	shellBootstrap, err := fetchAcceptanceText(joinBase + "/bootstrap.sh")
	if err != nil {
		return nil, nil, err
	}
	assetResults := make([]map[string]any, 0, len(assetSHA256))
	allAssetsOK := true
	assetNames := make([]string, 0, len(assetSHA256))
	for name := range assetSHA256 {
		assetNames = append(assetNames, name)
	}
	sort.Strings(assetNames)
	for _, name := range assetNames {
		expected := assetSHA256[name]
		actual, err := fetchAcceptanceText(httpServer.URL + "/assets/" + name + ".sha256")
		ok := err == nil && strings.TrimSpace(actual) == expected
		if !ok {
			allAssetsOK = false
		}
		result := map[string]any{
			"asset":           name,
			"sha256_endpoint": httpServer.URL + "/assets/" + name + ".sha256",
			"expected_sha256": expected,
			"ok":              ok,
		}
		if err != nil {
			result["error"] = err.Error()
		}
		assetResults = append(assetResults, result)
	}
	bootstrapTargetBytes := intFromAny(bootstrapConnector["first_connect_target_bytes"])
	nativeBootstrapConnector := mapFromAny(bootstrapConnector["native_connector"])
	bootstrapFirstConnect := map[string]any{
		"schema_version":                           "rdev.acceptance.bootstrap-first-connect.v1",
		"bootstrap_connector_schema_version":       bootstrapConnector["schema_version"],
		"native_connector":                         nativeBootstrapConnector,
		"first_connect_target_bytes":               bootstrapTargetBytes,
		"default_first_connect_surface":            bootstrapConnector["default_first_connect_surface"],
		"publishes_native_first_connect_asset":     bootstrapConnector["publishes_native_first_connect_asset"],
		"windows_script_bytes":                     len(windowsBootstrap),
		"shell_script_bytes":                       len(shellBootstrap),
		"windows_within_budget":                    bootstrapTargetBytes > 0 && len(windowsBootstrap) < bootstrapTargetBytes,
		"shell_within_budget":                      bootstrapTargetBytes > 0 && len(shellBootstrap) < bootstrapTargetBytes,
		"preconnect_endpoint":                      bootstrapConnector["preconnect_endpoint"],
		"preconnect_source":                        bootstrapConnector["source"],
		"preconnect_grants_host_access":            boolFromAny(bootstrapConnector["grants_host_access"]),
		"can_run_session_tasks_before_full_runner": boolFromAny(bootstrapConnector["can_run_session_tasks"]),
		"full_runner_phase":                        bootstrapConnector["full_runner_phase"],
		"must_not_skip_full_helper_verification": boolFromAny(
			bootstrapConnector["must_not_skip_full_helper_verification"],
		),
		"staged_upgrade_rule": "preconnect is a sub-1MB first-contact signal only; session task execution requires downloading the full helper with retry/backoff, verifying SHA-256, then starting host serve",
	}
	report := map[string]any{
		"schema_version":          "rdev.acceptance.bootstrap-self-repair.v1",
		"join_url":                joinBase,
		"ticket_code":             ticket.Code,
		"windows_script_bytes":    len(windowsBootstrap),
		"shell_script_bytes":      len(shellBootstrap),
		"asset_sha256":            assetResults,
		"bootstrap_first_connect": bootstrapFirstConnect,
		"agent_rule":              "fresh Agents should rely on support-session join bootstrap self-repair instead of asking target users to install rdev manually",
	}
	forbidden := joinPage + "\n" + windowsBootstrap + "\n" + shellBootstrap
	checks := []Check{
		{Name: "bootstrap_self_repair_join_page_available", Passed: strings.Contains(joinPage, "bootstrap.ps1") && strings.Contains(joinPage, "bootstrap.sh") && strings.Contains(joinPage, "rdev.connection-entry.package-catalog.v1"), Detail: joinBase},
		{Name: "bootstrap_self_repair_windows_downloads_verified_helper", Passed: strings.Contains(windowsBootstrap, "Downloading verified rdev helper") && strings.Contains(windowsBootstrap, "Invoke-RdevWebRequestWithRetry") && strings.Contains(windowsBootstrap, "Get-FileHash") && strings.Contains(windowsBootstrap, ".sha256"), Detail: "PowerShell downloads with retry/backoff and verifies rdev-windows-amd64.exe when rdev is absent"},
		{Name: "bootstrap_self_repair_shell_downloads_verified_helper", Passed: strings.Contains(shellBootstrap, "Downloading verified rdev helper") && strings.Contains(shellBootstrap, "rdev_curl_retry_flags") && strings.Contains(shellBootstrap, "curl $rdev_curl_retry_flags -fsSL") && strings.Contains(shellBootstrap, "shasum -a 256") && strings.Contains(shellBootstrap, ".sha256"), Detail: "shell downloads with retry/backoff and verifies target OS/arch helper when rdev is absent"},
		{Name: "bootstrap_self_repair_pins_manifest_root", Passed: strings.Contains(windowsBootstrap, "--manifest-root-public-key") && strings.Contains(shellBootstrap, "--manifest-root-public-key"), Detail: "bootstrap scripts pin the join manifest trust root"},
		{Name: "bootstrap_self_repair_starts_visible_host", Passed: strings.Contains(windowsBootstrap, "host serve") && strings.Contains(shellBootstrap, "host serve") && strings.Contains(windowsBootstrap, "--transport long-poll") && strings.Contains(shellBootstrap, "--transport long-poll") && strings.Contains(windowsBootstrap, "--once=false") && strings.Contains(shellBootstrap, "--once=false"), Detail: "bootstrap scripts start attended host serve with stable long-poll transport"},
		{Name: "bootstrap_self_repair_assets_have_hashes", Passed: allAssetsOK, Detail: fmt.Sprintf("%v", assetResults)},
		{Name: "bootstrap_self_repair_no_manual_rdev_requirement", Passed: !strings.Contains(forbidden, "rdev is required") && !strings.Contains(forbidden, "Install the verified rdev release package") && !strings.Contains(forbidden, "ExecutionPolicy Bypass"), Detail: "join/bootstrap surface must not ask the target user to manually install rdev or bypass execution policy"},
		{Name: "bootstrap_first_connect_scripts_under_budget", Passed: boolFromAny(bootstrapFirstConnect["windows_within_budget"]) && boolFromAny(bootstrapFirstConnect["shell_within_budget"]), Detail: fmt.Sprintf("windows=%d shell=%d target=%d", len(windowsBootstrap), len(shellBootstrap), bootstrapTargetBytes)},
		{Name: "bootstrap_first_connect_preconnect_does_not_grant_host_access", Passed: stringFromAny(bootstrapConnector["schema_version"]) == supportsession.BootstrapConnectorSchemaVersion && stringFromAny(bootstrapConnector["preconnect_endpoint"]) == "/v1/support-session/preconnect" && !boolFromAny(bootstrapConnector["grants_host_access"]) && !boolFromAny(bootstrapConnector["can_run_session_tasks"]), Detail: fmt.Sprintf("%v", bootstrapConnector)},
		{Name: "bootstrap_first_connect_requires_verified_full_helper_upgrade", Passed: stringFromAny(bootstrapConnector["full_runner_phase"]) == "download-verified-rdev-host" && boolFromAny(bootstrapConnector["must_not_skip_full_helper_verification"]) && strings.Contains(windowsBootstrap, "Get-FileHash") && strings.Contains(shellBootstrap, "shasum -a 256") && strings.Contains(windowsBootstrap, "Invoke-RdevWebRequestWithRetry") && strings.Contains(shellBootstrap, "curl $rdev_curl_retry_flags -fsSL"), Detail: "full helper upgrade must use retry/backoff plus SHA-256 verification before host serve can run session tasks"},
	}
	return report, checks, nil
}

func fetchAcceptanceText(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET %s returned %s: %s", url, resp.Status, string(content))
	}
	return string(content), nil
}

func writeFreshAgentSupportSessionReport(path string, report FreshAgentSupportSessionReport) error {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func withFreshAgentGatewayEnvCleared(fn func() map[string]any) map[string]any {
	envNames := []string{
		"RDEV_HOSTED_GATEWAY_URL",
		"RDEV_RELAY_GATEWAY_URL",
		"RDEV_MESH_GATEWAY_URL",
		"RDEV_VPN_GATEWAY_URL",
		"RDEV_SSH_GATEWAY_URL",
	}
	type previousEnv struct {
		value string
		ok    bool
	}
	previous := map[string]previousEnv{}
	for _, name := range envNames {
		value, ok := os.LookupEnv(name)
		previous[name] = previousEnv{value: value, ok: ok}
		_ = os.Unsetenv(name)
	}
	defer func() {
		for _, name := range envNames {
			if previous[name].ok {
				_ = os.Setenv(name, previous[name].value)
			} else {
				_ = os.Unsetenv(name)
			}
		}
	}()
	return fn()
}

func withFreshAgentGatewayEnv(name, value string, fn func() map[string]any) map[string]any {
	envNames := []string{
		"RDEV_HOSTED_GATEWAY_URL",
		"RDEV_RELAY_GATEWAY_URL",
		"RDEV_MESH_GATEWAY_URL",
		"RDEV_VPN_GATEWAY_URL",
		"RDEV_SSH_GATEWAY_URL",
	}
	type previousEnv struct {
		value string
		ok    bool
	}
	previous := map[string]previousEnv{}
	for _, envName := range envNames {
		envValue, ok := os.LookupEnv(envName)
		previous[envName] = previousEnv{value: envValue, ok: ok}
		_ = os.Unsetenv(envName)
	}
	if strings.TrimSpace(name) != "" {
		_ = os.Setenv(name, value)
	}
	defer func() {
		for _, envName := range envNames {
			if previous[envName].ok {
				_ = os.Setenv(envName, previous[envName].value)
			} else {
				_ = os.Unsetenv(envName)
			}
		}
	}()
	return fn()
}

func policyCapabilitiesToStringsForFreshAgent(capabilities []policy.Capability) []string {
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		out = append(out, string(capability))
	}
	return out
}

func manifestRootPublicKeyForFreshAgent(root model.TrustBundle) string {
	if root.SigningKeyID == "" || root.PublicKey == "" {
		return ""
	}
	return root.SigningKeyID + ":" + root.PublicKey
}

func containsAllStrings(values []string, needles ...string) bool {
	joined := strings.Join(values, "\x00")
	for _, needle := range needles {
		if !strings.Contains(joined, needle) {
			return false
		}
	}
	return true
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return text
}

func boolFromAny(value any) bool {
	b, _ := value.(bool)
	return b
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil
	}
	return result
}

func stringSliceFromAny(value any) []string {
	if value == nil {
		return nil
	}
	if typed, ok := value.([]string); ok {
		return typed
	}
	if values, ok := value.([]any); ok {
		out := make([]string, 0, len(values))
		for _, item := range values {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

func mapSliceFromAny(value any) []map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.([]map[string]any); ok {
		return typed
	}
	if values, ok := value.([]any); ok {
		out := make([]map[string]any, 0, len(values))
		for _, item := range values {
			if typed, ok := item.(map[string]any); ok {
				out = append(out, typed)
			}
		}
		return out
	}
	return nil
}
