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
	SchemaVersion           string         `json:"schema_version"`
	GeneratedAt             time.Time      `json:"generated_at"`
	OutDir                  string         `json:"out_dir"`
	GatewayURL              string         `json:"gateway_url"`
	ConnectNoGateway        map[string]any `json:"connect_no_gateway"`
	ConnectReachableGateway map[string]any `json:"connect_reachable_gateway"`
	HandoffNoGateway        map[string]any `json:"handoff_no_gateway"`
	HandoffReachableGateway map[string]any `json:"handoff_reachable_gateway"`
	CreatedSession          map[string]any `json:"created_session"`
	StartedSession          map[string]any `json:"started_session"`
	StableFallbackSession   map[string]any `json:"stable_fallback_session"`
	ConnectedStatus         map[string]any `json:"connected_status"`
	WaitingRecovery         map[string]any `json:"waiting_recovery"`
	BootstrapSelfRepair     map[string]any `json:"bootstrap_self_repair"`
	Checks                  []Check        `json:"checks"`
	RecommendedNextSteps    []string       `json:"recommended_next_steps"`
	RealEnvironmentRequired []string       `json:"real_environment_required"`
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
			Addr:        "0.0.0.0:8787",
			Target:      "auto",
			Reason:      "fresh Agent support-session acceptance",
			TTLSeconds:  7200,
			AutoApprove: true,
			Locale:      locale,
			RdevCommand: rdevCommand,
		})
	})
	handoffReachableGateway := supportsession.BuildHandoff(supportsession.HandoffOptions{
		Addr:        "0.0.0.0:8787",
		GatewayURL:  gatewayURL,
		Target:      "auto",
		Reason:      "fresh Agent support-session acceptance",
		TTLSeconds:  7200,
		AutoApprove: true,
		Locale:      locale,
		RdevCommand: rdevCommand,
	})
	connectNoGateway := supportsession.BuildConnectFromHandoff(handoffNoGateway)

	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicketWithMetadata(
		model.HostModeAttendedTemporary,
		7200,
		policyCapabilitiesToStringsForFreshAgent(policy.TemporaryDefaults()),
		"fresh Agent support-session acceptance",
		map[string]string{
			"connection_entry":  "standard-visible",
			"approval_contract": "target-consent-scoped-ticket",
			"auto_approve":      "attended-temporary",
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
		AutoApprove:           true,
	})
	connectReachableGateway := supportsession.BuildConnectFromCreated(created)
	started := supportsession.BuildStarted(supportsession.StartedOptions{
		Addr:       "0.0.0.0:8787",
		GatewayURL: gatewayURL,
		WorkDir:    filepath.Join(outDir, "support-session"),
		ReadyFile:  filepath.Join(outDir, "support-session", "support-session-ready.json"),
		StatusFile: filepath.Join(outDir, "support-session", "support-session-status.json"),
		Created:    created,
	})
	stableFallback := withFreshAgentGatewayEnv("RDEV_RELAY_GATEWAY_URL", "https://relay.example.test/rdev", func() map[string]any {
		stableURL, stableCandidates := supportsession.ConfiguredGatewayURLCandidate()
		stableTicket, err := gw.CreateTicketWithMetadata(
			model.HostModeAttendedTemporary,
			7200,
			policyCapabilitiesToStringsForFreshAgent(policy.TemporaryDefaults()),
			"fresh Agent stable fallback acceptance",
			map[string]string{
				"connection_entry":  "standard-visible",
				"approval_contract": "target-consent-scoped-ticket",
				"auto_approve":      "attended-temporary",
			},
		)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		handoff := supportsession.BuildHandoff(supportsession.HandoffOptions{
			Addr:        "0.0.0.0:8787",
			Target:      "auto",
			Reason:      "fresh Agent stable fallback acceptance",
			TTLSeconds:  7200,
			AutoApprove: true,
			Locale:      locale,
			RdevCommand: rdevCommand,
		})
		created := supportsession.BuildCreated(supportsession.CreatedOptions{
			GatewayURL:            stableURL,
			GatewayURLCandidates:  stableCandidates,
			ManifestRootPublicKey: manifestRootPublicKeyForFreshAgent(gw.ManifestRoot()),
			Ticket:                stableTicket,
			Target:                "windows",
			Locale:                locale,
			RdevCommand:           rdevCommand,
			AutoApprove:           true,
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
	bootstrapSelfRepair, bootstrapChecks, err := buildBootstrapSelfRepairContract(outDir, now)
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
		ConnectedStatus:         connectedStatus,
		WaitingRecovery:         waitingRecovery,
		BootstrapSelfRepair:     bootstrapSelfRepair,
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
			"real LAN departure or restrictive-network relay/mesh/VPN/SSH paths",
		},
	}
	report.Checks = append(report.Checks, bootstrapChecks...)
	if err := writeFreshAgentSupportSessionReport(filepath.Join(outDir, "report.json"), report); err != nil {
		return FreshAgentSupportSessionReport{}, err
	}
	return report, nil
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
	connectHelperPreflight := mapFromAny(input.ConnectReachableGateway["connectivity_helper_preflight"])
	handoff := mapFromAny(input.CreatedSession["user_handoff"])
	mcpFollowUp := mapSliceFromAny(input.CreatedSession["mcp_follow_up"])
	configuredWatcher := mapFromAny(input.CreatedSession["watch_connection_status_configured_gateway"])
	supervision := mapFromAny(input.CreatedSession["connection_supervision"])
	preflight := mapFromAny(input.CreatedSession["gateway_candidate_preflight"])
	helperPreflight := mapFromAny(input.CreatedSession["connectivity_helper_preflight"])
	runbook := mapFromAny(input.CreatedSession["agent_connection_runbook"])
	runbookStandardEntry := mapFromAny(runbook["standard_entry_tool"])
	runbookLowLevelRule := mapFromAny(runbook["low_level_entry_rule"])
	runbookFailurePrevention := mapFromAny(runbook["fresh_agent_failure_prevention"])
	readyFile := mapFromAny(input.StartedSession["ready_file"])
	statusFile := mapFromAny(input.StartedSession["status_file"])
	foregroundFeedback := mapFromAny(input.StartedSession["foreground_feedback"])
	session := mapFromAny(input.StartedSession["session"])
	startedHandoff := mapFromAny(input.StartedSession["user_handoff"])
	startedSupervision := mapFromAny(input.StartedSession["connection_supervision"])
	startedPreflight := mapFromAny(input.StartedSession["gateway_candidate_preflight"])
	startedHelperPreflight := mapFromAny(input.StartedSession["connectivity_helper_preflight"])
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
		{Name: "connect_with_gateway_returns_ready_handoff", Passed: input.ConnectReachableGateway["schema_version"] == supportsession.ConnectSchemaVersion && input.ConnectReachableGateway["selected_path"] == "created-with-reachable-gateway" && input.ConnectReachableGateway["ready_to_send_to_human"] == true && stringFromAny(connectUserHandoff["schema_version"]) == supportsession.UserHandoffSchemaVersion, Detail: stringFromAny(connectUserHandoff["copy_paste_kind"])},
		{Name: "connect_with_gateway_has_top_level_helper_preflight", Passed: stringFromAny(connectHelperPreflight["schema_version"]) == supportsession.ConnectivityHelperPreflightSchemaVersion && strings.Contains(strings.Join(stringSliceFromAny(connectHelperPreflight["forbidden"]), "\n"), "ExecutionPolicy Bypass"), Detail: fmt.Sprintf("%v", connectHelperPreflight["configured_helper_ids"])},
		{Name: "handoff_without_gateway_selects_foreground_start", Passed: input.HandoffNoGateway["selected_path"] == "start-foreground-gateway", Detail: stringFromAny(input.HandoffNoGateway["selected_path"])},
		{Name: "handoff_without_gateway_prefers_connect_start", Passed: containsAllStrings(noGatewayStartNowCommand, "support-session", "connect", "--start"), Detail: strings.Join(noGatewayStartNowCommand, " ")},
		{Name: "foreground_start_command_is_standard_tool", Passed: containsAllStrings(noGatewayCommand, "support-session", "start"), Detail: strings.Join(noGatewayCommand, " ")},
		{Name: "handoff_with_gateway_selects_create_tool", Passed: input.HandoffReachableGateway["selected_path"] == "create-with-reachable-gateway" && input.HandoffReachableGateway["mcp_next_tool"] == "rdev.support_session.create", Detail: stringFromAny(input.HandoffReachableGateway["selected_path"])},
		{Name: "create_arguments_include_gateway_and_waitable_target", Passed: stringFromAny(reachableArgs["gateway_url"]) != "" && stringFromAny(reachableArgs["target"]) == "auto", Detail: fmt.Sprintf("%v", reachableArgs)},
		{Name: "created_session_has_one_user_handoff", Passed: stringFromAny(handoff["schema_version"]) == supportsession.UserHandoffSchemaVersion && copyPaste != "", Detail: stringFromAny(handoff["copy_paste_kind"])},
		{Name: "created_session_copy_paste_is_not_rewritten_placeholder", Passed: copyPaste == targetCommand && !strings.Contains(copyPaste, "<ticket-code>") && !strings.Contains(copyPaste, "ExecutionPolicy Bypass"), Detail: copyPaste},
		{Name: "created_session_has_waiting_mcp_followup", Passed: len(mcpFollowUp) > 0 && stringFromAny(mcpFollowUp[0]["tool"]) == "rdev.support_session.status" && boolFromAny(mapFromAny(mcpFollowUp[0]["arguments"])["wait"]), Detail: fmt.Sprintf("%v", mcpFollowUp)},
		{Name: "configured_gateway_watcher_omits_gateway_url", Passed: !strings.Contains(strings.Join(stringSliceFromAny(configuredWatcher["command"]), " "), "--gateway-url"), Detail: strings.Join(stringSliceFromAny(configuredWatcher["command"]), " ")},
		{Name: "created_session_has_connection_supervision", Passed: stringFromAny(supervision["schema_version"]) == supportsession.ConnectionSupervisionSchemaVersion && stringFromAny(mapFromAny(supervision["mcp_watch_call"])["tool"]) == "rdev.support_session.status" && boolFromAny(mapFromAny(mapFromAny(supervision["mcp_watch_call"])["arguments"])["wait"]) && strings.Contains(stringFromAny(supervision["connected_report_rule"]), "connected_next_steps.user_report"), Detail: stringFromAny(supervision["upgrade_reason"])},
		{Name: "connection_supervision_covers_signed_candidate_runtime_failover", Passed: strings.Contains(strings.Join(stringSliceFromAny(supervision["automatic_downgrade_boundaries"]), "\n"), "signed join-manifest gateway candidates"), Detail: strings.Join(stringSliceFromAny(supervision["automatic_downgrade_boundaries"]), " | ")},
		{Name: "created_session_has_gateway_candidate_preflight", Passed: stringFromAny(preflight["schema_version"]) == supportsession.GatewayCandidatePreflightSchemaVersion && intFromAny(preflight["candidate_count"]) > 0 && strings.Contains(stringFromAny(preflight["agent_rule"]), "target command owns ordered URL fallback"), Detail: stringFromAny(preflight["preflight_mode"])},
		{Name: "created_session_has_connectivity_helper_preflight", Passed: stringFromAny(helperPreflight["schema_version"]) == supportsession.ConnectivityHelperPreflightSchemaVersion && stringFromAny(helperPreflight["agent_rule"]) != "" && strings.Contains(strings.Join(stringSliceFromAny(helperPreflight["forbidden"]), "\n"), "ExecutionPolicy Bypass"), Detail: fmt.Sprintf("%v", helperPreflight["configured_helper_ids"])},
		{Name: "created_session_has_agent_connection_runbook", Passed: stringFromAny(runbook["schema_version"]) == supportsession.AgentConnectionRunbookSchemaVersion && strings.Contains(strings.Join(stringSliceFromAny(runbook["sequence"]), "\n"), "user_handoff.message") && strings.Contains(strings.Join(stringSliceFromAny(runbook["forbidden"]), "\n"), "Agent-authored PowerShell"), Detail: stringFromAny(runbook["phase"])},
		{Name: "agent_runbook_starts_with_support_session_connect", Passed: stringFromAny(runbookStandardEntry["mcp_tool"]) == "rdev.support_session.connect" && strings.Contains(strings.Join(stringSliceFromAny(runbookStandardEntry["cli_command"]), " "), "support-session connect"), Detail: fmt.Sprintf("%v", runbookStandardEntry)},
		{Name: "agent_runbook_forbids_low_level_invite_first", Passed: strings.Contains(strings.Join(stringSliceFromAny(runbookLowLevelRule["do_not_start_with"]), "\n"), "rdev.invites.create") && strings.Contains(strings.Join(stringSliceFromAny(runbookLowLevelRule["do_not_start_with"]), "\n"), "rdev.connection_entry.plan"), Detail: fmt.Sprintf("%v", runbookLowLevelRule)},
		{Name: "agent_runbook_contains_real_failure_prevention", Passed: stringFromAny(runbookFailurePrevention["schema_version"]) == supportsession.FreshAgentFailurePreventionSchemaVersion && strings.Contains(strings.Join(stringSliceFromAny(runbookFailurePrevention["known_failure_pattern"]), "\n"), "rdev is required") && strings.Contains(strings.Join(stringSliceFromAny(runbookFailurePrevention["required_standard_path"]), "\n"), "cli_start_now_command") && strings.Contains(strings.Join(stringSliceFromAny(runbookFailurePrevention["forbidden_agent_generated_workarounds"]), "\n"), "ExecutionPolicy Bypass"), Detail: fmt.Sprintf("%v", runbookFailurePrevention)},
		{Name: "started_payload_has_top_level_handoff", Passed: input.StartedSession["ready_to_send_to_human"] == true && stringFromAny(startedHandoff["schema_version"]) == supportsession.UserHandoffSchemaVersion && stringFromAny(startedHandoff["copy_paste"]) == stringFromAny(input.StartedSession["target_command"]), Detail: stringFromAny(startedHandoff["copy_paste_kind"])},
		{Name: "started_payload_has_top_level_supervision", Passed: stringFromAny(startedSupervision["schema_version"]) == supportsession.ConnectionSupervisionSchemaVersion && stringFromAny(startedSupervision["ticket_code"]) == input.Ticket.Code, Detail: stringFromAny(startedSupervision["continuity_assessment"])},
		{Name: "started_payload_has_top_level_gateway_preflight", Passed: stringFromAny(startedPreflight["schema_version"]) == supportsession.GatewayCandidatePreflightSchemaVersion && intFromAny(startedPreflight["candidate_count"]) > 0, Detail: stringFromAny(startedPreflight["preflight_mode"])},
		{Name: "started_payload_has_top_level_helper_preflight", Passed: stringFromAny(startedHelperPreflight["schema_version"]) == supportsession.ConnectivityHelperPreflightSchemaVersion && strings.Contains(stringFromAny(startedHelperPreflight["agent_rule"]), "Connection Entry runner"), Detail: fmt.Sprintf("%v", startedHelperPreflight["configured_helper_ids"])},
		{Name: "started_payload_has_top_level_agent_runbook", Passed: stringFromAny(startedRunbook["schema_version"]) == supportsession.AgentConnectionRunbookSchemaVersion && strings.Contains(fmt.Sprintf("%v", startedRunbook["watch"]), "rdev.support_session.status"), Detail: stringFromAny(startedRunbook["phase"])},
		{Name: "started_payload_has_foreground_feedback", Passed: stringFromAny(foregroundFeedback["schema_version"]) == "rdev.support-session-foreground-feedback.v1" && stringFromAny(foregroundFeedback["event_prefix"]) == "rdev support session event: " && strings.Contains(stringFromAny(foregroundFeedback["connected_rule"]), "connection has been established"), Detail: stringFromAny(foregroundFeedback["event_prefix"])},
		{Name: "started_payload_exposes_ready_file", Passed: stringFromAny(readyFile["schema_version"]) == "rdev.support-session-ready-file.v1" && strings.Contains(stringFromAny(readyFile["path"]), "support-session-ready.json"), Detail: stringFromAny(readyFile["path"])},
		{Name: "started_payload_exposes_status_file", Passed: stringFromAny(statusFile["schema_version"]) == supportsession.StatusFileSchemaVersion && strings.Contains(stringFromAny(statusFile["path"]), "support-session-status.json") && strings.Contains(stringFromAny(statusFile["agent_rule"]), "connected_next_steps.user_report"), Detail: stringFromAny(statusFile["path"])},
		{Name: "started_payload_embeds_created_session", Passed: stringFromAny(session["schema_version"]) == supportsession.CreatedSchemaVersion && stringFromAny(session["ticket_code"]) == input.Ticket.Code, Detail: stringFromAny(session["ticket_code"])},
		{Name: "stable_fallback_handoff_uses_configured_gateway", Passed: stringFromAny(stableFallbackHandoff["selected_path"]) == "create-with-reachable-gateway" && stringFromAny(stableFallbackHandoffArgs["gateway_url"]) == "https://relay.example.test/rdev", Detail: fmt.Sprintf("%v", stableFallbackHandoffArgs)},
		{Name: "stable_fallback_created_uses_relay_candidate", Passed: strings.Contains(stringFromAny(stableFallbackCreated["target_command"]), "https://relay.example.test/rdev/join/") && strings.Contains(stringFromAny(stableFallbackCreated["target_command"]), "gateway_url_candidates=") && strings.Contains(stringFromAny(mapFromAny(stableFallbackCreated["user_handoff"])["copy_paste"]), "https://relay.example.test/rdev/join/"), Detail: stringFromAny(stableFallbackCreated["target_command"])},
		{Name: "stable_fallback_continuity_is_durable", Passed: boolFromAny(stableFallbackContinuity["stable_after_lan_change"]) && strings.Contains(strings.Join(stringSliceFromAny(stableFallbackContinuity["stable_fallback_kinds"]), "\n"), "relay"), Detail: fmt.Sprintf("%v", stableFallbackContinuity)},
		{Name: "stable_fallback_supervision_does_not_request_upgrade", Passed: stableFallbackSupervision["upgrade_recommended"] == false && strings.Contains(stringFromAny(stableFallbackSupervision["upgrade_reason"]), "stable hosted/relay/mesh/VPN/SSH fallback already configured"), Detail: stringFromAny(stableFallbackSupervision["upgrade_reason"])},
		{Name: "stable_fallback_runbook_reports_stable_candidate", Passed: boolFromAny(stableFallbackRunbookSummary["has_stable_configured_fallback"]) && strings.Contains(strings.Join(stringSliceFromAny(stableFallbackRunbookSummary["candidate_kinds"]), "\n"), "relay"), Detail: fmt.Sprintf("%v", stableFallbackRunbookSummary)},
		{Name: "auto_approval_connects_first_attended_host", Passed: input.Host.Status == model.HostStatusActive && input.ConnectedStatus["connected"] == true, Detail: string(input.Host.Status)},
		{Name: "connected_status_has_user_report", Passed: stringFromAny(connectedNext["schema_version"]) == supportsession.ConnectedNextStepsSchemaVersion && strings.TrimSpace(stringFromAny(connectedNext["user_report"])) != "", Detail: stringFromAny(connectedNext["user_report"])},
		{Name: "connected_status_points_to_capability_probe", Passed: strings.Contains(fmt.Sprintf("%v", connectedNext["mcp_next_calls"]), "rdev.hosts.capabilities"), Detail: fmt.Sprintf("%v", connectedNext["mcp_next_calls"])},
		{Name: "connected_status_has_agent_runbook", Passed: stringFromAny(statusRunbook["schema_version"]) == supportsession.AgentConnectionRunbookSchemaVersion && stringFromAny(statusRunbook["status"]) == "connected", Detail: stringFromAny(statusRunbook["phase"])},
		{Name: "waiting_recovery_has_agent_runbook", Passed: stringFromAny(recoveryRunbook["schema_version"]) == supportsession.AgentConnectionRunbookSchemaVersion && strings.Contains(strings.Join(stringSliceFromAny(recoveryRunbook["on_timeout_or_failure"]), "\n"), "gateway_candidate_preflight"), Detail: stringFromAny(recoveryRunbook["phase"])},
		{Name: "waiting_recovery_forbids_custom_scripts", Passed: strings.Contains(recoveryForbidden, "Agent-authored PowerShell") && strings.Contains(recoveryForbidden, "manual ticket/root/gateway/transport"), Detail: recoveryForbidden},
		{Name: "fresh_agent_surface_forbids_unsafe_shortcuts", Passed: strings.Contains(forbiddenText, "hidden install") && strings.Contains(forbiddenText, "ExecutionPolicy Bypass"), Detail: forbiddenText},
	}
	return checks
}

func buildBootstrapSelfRepairContract(outDir string, now time.Time) (map[string]any, []Check, error) {
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
			"connection_entry":  "standard-visible",
			"approval_contract": "target-consent-scoped-ticket",
			"auto_approve":      "attended-temporary",
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
	report := map[string]any{
		"schema_version":       "rdev.acceptance.bootstrap-self-repair.v1",
		"join_url":             joinBase,
		"ticket_code":          ticket.Code,
		"windows_script_bytes": len(windowsBootstrap),
		"shell_script_bytes":   len(shellBootstrap),
		"asset_sha256":         assetResults,
		"agent_rule":           "fresh Agents should rely on support-session join bootstrap self-repair instead of asking target users to install rdev manually",
	}
	forbidden := joinPage + "\n" + windowsBootstrap + "\n" + shellBootstrap
	checks := []Check{
		{Name: "bootstrap_self_repair_join_page_available", Passed: strings.Contains(joinPage, "bootstrap.ps1") && strings.Contains(joinPage, "bootstrap.sh") && strings.Contains(joinPage, "rdev.connection-entry.package-catalog.v1"), Detail: joinBase},
		{Name: "bootstrap_self_repair_windows_downloads_verified_helper", Passed: strings.Contains(windowsBootstrap, "Downloading verified rdev helper") && strings.Contains(windowsBootstrap, "Invoke-WebRequest") && strings.Contains(windowsBootstrap, "Get-FileHash") && strings.Contains(windowsBootstrap, ".sha256"), Detail: "PowerShell downloads and verifies rdev-windows-amd64.exe when rdev is absent"},
		{Name: "bootstrap_self_repair_shell_downloads_verified_helper", Passed: strings.Contains(shellBootstrap, "Downloading verified rdev helper") && strings.Contains(shellBootstrap, "curl -fsSL") && strings.Contains(shellBootstrap, "shasum -a 256") && strings.Contains(shellBootstrap, ".sha256"), Detail: "shell downloads and verifies target OS/arch helper when rdev is absent"},
		{Name: "bootstrap_self_repair_pins_manifest_root", Passed: strings.Contains(windowsBootstrap, "--manifest-root-public-key") && strings.Contains(shellBootstrap, "--manifest-root-public-key"), Detail: "bootstrap scripts pin the join manifest trust root"},
		{Name: "bootstrap_self_repair_starts_visible_host", Passed: strings.Contains(windowsBootstrap, "host serve") && strings.Contains(shellBootstrap, "host serve") && strings.Contains(windowsBootstrap, "--transport auto") && strings.Contains(shellBootstrap, "--transport auto") && strings.Contains(windowsBootstrap, "--once=false") && strings.Contains(shellBootstrap, "--once=false"), Detail: "bootstrap scripts start attended host serve with transport auto"},
		{Name: "bootstrap_self_repair_assets_have_hashes", Passed: allAssetsOK, Detail: fmt.Sprintf("%v", assetResults)},
		{Name: "bootstrap_self_repair_no_manual_rdev_requirement", Passed: !strings.Contains(forbidden, "rdev is required") && !strings.Contains(forbidden, "Install the verified rdev release package") && !strings.Contains(forbidden, "ExecutionPolicy Bypass"), Detail: "join/bootstrap surface must not ask the target user to manually install rdev or bypass execution policy"},
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
	return nil
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
