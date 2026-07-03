package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
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
	ConnectedStatus         map[string]any `json:"connected_status"`
	WaitingRecovery         map[string]any `json:"waiting_recovery"`
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
		Created:    created,
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
		ConnectedStatus:         connectedStatus,
		WaitingRecovery:         waitingRecovery,
		Checks: freshAgentSupportSessionChecks(freshAgentSupportSessionCheckInput{
			HandoffNoGateway:        handoffNoGateway,
			HandoffReachableGateway: handoffReachableGateway,
			ConnectNoGateway:        connectNoGateway,
			ConnectReachableGateway: connectReachableGateway,
			CreatedSession:          created,
			StartedSession:          started,
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
	ConnectedStatus         map[string]any
	WaitingRecovery         map[string]any
	Host                    model.Host
	Ticket                  model.Ticket
}

func freshAgentSupportSessionChecks(input freshAgentSupportSessionCheckInput) []Check {
	noGatewayCommand := stringSliceFromAny(input.HandoffNoGateway["foreground_start_command"])
	reachableArgs := mapFromAny(input.HandoffReachableGateway["mcp_next_arguments"])
	connectStartCommand := stringSliceFromAny(input.ConnectNoGateway["foreground_start_command"])
	connectUserHandoff := mapFromAny(input.ConnectReachableGateway["user_handoff"])
	handoff := mapFromAny(input.CreatedSession["user_handoff"])
	mcpFollowUp := mapSliceFromAny(input.CreatedSession["mcp_follow_up"])
	configuredWatcher := mapFromAny(input.CreatedSession["watch_connection_status_configured_gateway"])
	readyFile := mapFromAny(input.StartedSession["ready_file"])
	session := mapFromAny(input.StartedSession["session"])
	startedHandoff := mapFromAny(input.StartedSession["user_handoff"])
	connectedNext := mapFromAny(input.ConnectedStatus["connected_next_steps"])
	recoveryForbidden := strings.Join(stringSliceFromAny(input.WaitingRecovery["forbidden"]), "\n")
	copyPaste := stringFromAny(handoff["copy_paste"])
	targetCommand := stringFromAny(input.CreatedSession["target_command"])
	forbiddenText := strings.Join(stringSliceFromAny(input.CreatedSession["forbidden"]), "\n") + "\n" + targetCommand + "\n" + copyPaste
	checks := []Check{
		{Name: "connect_without_gateway_returns_foreground_start", Passed: input.ConnectNoGateway["schema_version"] == supportsession.ConnectSchemaVersion && input.ConnectNoGateway["selected_path"] == "start-foreground-gateway" && input.ConnectNoGateway["ready_to_send_to_human"] == false && containsAllStrings(connectStartCommand, "support-session", "start"), Detail: strings.Join(connectStartCommand, " ")},
		{Name: "connect_with_gateway_returns_ready_handoff", Passed: input.ConnectReachableGateway["schema_version"] == supportsession.ConnectSchemaVersion && input.ConnectReachableGateway["selected_path"] == "created-with-reachable-gateway" && input.ConnectReachableGateway["ready_to_send_to_human"] == true && stringFromAny(connectUserHandoff["schema_version"]) == supportsession.UserHandoffSchemaVersion, Detail: stringFromAny(connectUserHandoff["copy_paste_kind"])},
		{Name: "handoff_without_gateway_selects_foreground_start", Passed: input.HandoffNoGateway["selected_path"] == "start-foreground-gateway", Detail: stringFromAny(input.HandoffNoGateway["selected_path"])},
		{Name: "foreground_start_command_is_standard_tool", Passed: containsAllStrings(noGatewayCommand, "support-session", "start"), Detail: strings.Join(noGatewayCommand, " ")},
		{Name: "handoff_with_gateway_selects_create_tool", Passed: input.HandoffReachableGateway["selected_path"] == "create-with-reachable-gateway" && input.HandoffReachableGateway["mcp_next_tool"] == "rdev.support_session.create", Detail: stringFromAny(input.HandoffReachableGateway["selected_path"])},
		{Name: "create_arguments_include_gateway_and_waitable_target", Passed: stringFromAny(reachableArgs["gateway_url"]) != "" && stringFromAny(reachableArgs["target"]) == "auto", Detail: fmt.Sprintf("%v", reachableArgs)},
		{Name: "created_session_has_one_user_handoff", Passed: stringFromAny(handoff["schema_version"]) == supportsession.UserHandoffSchemaVersion && copyPaste != "", Detail: stringFromAny(handoff["copy_paste_kind"])},
		{Name: "created_session_copy_paste_is_not_rewritten_placeholder", Passed: copyPaste == targetCommand && !strings.Contains(copyPaste, "<ticket-code>") && !strings.Contains(copyPaste, "ExecutionPolicy Bypass"), Detail: copyPaste},
		{Name: "created_session_has_waiting_mcp_followup", Passed: len(mcpFollowUp) > 0 && stringFromAny(mcpFollowUp[0]["tool"]) == "rdev.support_session.status" && boolFromAny(mapFromAny(mcpFollowUp[0]["arguments"])["wait"]), Detail: fmt.Sprintf("%v", mcpFollowUp)},
		{Name: "configured_gateway_watcher_omits_gateway_url", Passed: !strings.Contains(strings.Join(stringSliceFromAny(configuredWatcher["command"]), " "), "--gateway-url"), Detail: strings.Join(stringSliceFromAny(configuredWatcher["command"]), " ")},
		{Name: "started_payload_has_top_level_handoff", Passed: input.StartedSession["ready_to_send_to_human"] == true && stringFromAny(startedHandoff["schema_version"]) == supportsession.UserHandoffSchemaVersion && stringFromAny(startedHandoff["copy_paste"]) == stringFromAny(input.StartedSession["target_command"]), Detail: stringFromAny(startedHandoff["copy_paste_kind"])},
		{Name: "started_payload_exposes_ready_file", Passed: stringFromAny(readyFile["schema_version"]) == "rdev.support-session-ready-file.v1" && strings.Contains(stringFromAny(readyFile["path"]), "support-session-ready.json"), Detail: stringFromAny(readyFile["path"])},
		{Name: "started_payload_embeds_created_session", Passed: stringFromAny(session["schema_version"]) == supportsession.CreatedSchemaVersion && stringFromAny(session["ticket_code"]) == input.Ticket.Code, Detail: stringFromAny(session["ticket_code"])},
		{Name: "auto_approval_connects_first_attended_host", Passed: input.Host.Status == model.HostStatusActive && input.ConnectedStatus["connected"] == true, Detail: string(input.Host.Status)},
		{Name: "connected_status_has_user_report", Passed: stringFromAny(connectedNext["schema_version"]) == supportsession.ConnectedNextStepsSchemaVersion && strings.TrimSpace(stringFromAny(connectedNext["user_report"])) != "", Detail: stringFromAny(connectedNext["user_report"])},
		{Name: "connected_status_points_to_capability_probe", Passed: strings.Contains(fmt.Sprintf("%v", connectedNext["mcp_next_calls"]), "rdev.hosts.capabilities"), Detail: fmt.Sprintf("%v", connectedNext["mcp_next_calls"])},
		{Name: "waiting_recovery_forbids_custom_scripts", Passed: strings.Contains(recoveryForbidden, "Agent-authored PowerShell") && strings.Contains(recoveryForbidden, "manual ticket/root/gateway/transport"), Detail: recoveryForbidden},
		{Name: "fresh_agent_surface_forbids_unsafe_shortcuts", Passed: strings.Contains(forbiddenText, "hidden install") && strings.Contains(forbiddenText, "ExecutionPolicy Bypass"), Detail: forbiddenText},
	}
	return checks
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
