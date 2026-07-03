package supportsession

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestGatewayURLCandidatesPreferPrivateAddressForWildcardListen(t *testing.T) {
	firstPrivate := net.IPv4(10, 8, 0, 5)
	secondPrivate := net.IPv4(172, 16, 4, 9)
	firstPrivateURL := "http://" + net.JoinHostPort(firstPrivate.String(), "8787")
	candidates := GatewayURLCandidatesFromIPs("0.0.0.0:8787", "", []net.IP{
		net.ParseIP("203.0.113.10"),
		secondPrivate,
		firstPrivate,
	})

	if len(candidates) < 2 {
		t.Fatalf("expected LAN and loopback candidates, got %#v", candidates)
	}
	if candidates[0].URL != firstPrivateURL ||
		!candidates[0].Recommended ||
		candidates[0].Kind != "lan-private" {
		t.Fatalf("expected sorted private LAN recommendation, got %#v", candidates)
	}
	for _, candidate := range candidates {
		if strings.Contains(candidate.URL, "0.0.0.0") || strings.Contains(candidate.URL, "203.0.113.10") {
			t.Fatalf("candidate should not expose wildcard or non-private test IP: %#v", candidate)
		}
	}
}

func TestGatewayURLCandidatesRespectExplicitGateway(t *testing.T) {
	gatewayURL, candidates := ResolveGatewayURL("0.0.0.0:8787", "https://gateway.example.test/rdev")
	if gatewayURL != "https://gateway.example.test/rdev" {
		t.Fatalf("expected explicit gateway to win, got %q", gatewayURL)
	}
	if len(candidates) == 0 || !candidates[0].Recommended || candidates[0].Kind != "explicit" {
		t.Fatalf("expected explicit candidate to be recommended, got %#v", candidates)
	}
}

func TestGatewayURLCandidatesAppendConfiguredFallbacksBeforeLoopback(t *testing.T) {
	candidates := gatewayURLCandidatesFromIPsAndEnv("0.0.0.0:8787", "", []net.IP{
		net.ParseIP("192.168.50.10"),
	}, []GatewayEnvCandidate{
		{
			EnvVar: "RDEV_RELAY_GATEWAY_URL",
			URL:    "https://relay.example.test/rdev",
			Kind:   "relay",
			Scope:  "configured-relay",
			Reason: "configured relay gateway URL",
		},
		{
			EnvVar: "RDEV_MESH_GATEWAY_URL",
			URL:    "https://mesh.example.test/rdev",
			Kind:   "mesh",
			Scope:  "configured-mesh",
			Reason: "configured mesh gateway URL",
		},
	})

	if len(candidates) != 4 {
		t.Fatalf("expected LAN, relay, mesh, and loopback candidates, got %#v", candidates)
	}
	if candidates[0].Kind != "lan-private" || !candidates[0].Recommended {
		t.Fatalf("expected LAN candidate to stay recommended, got %#v", candidates)
	}
	if candidates[1].URL != "https://relay.example.test/rdev" ||
		candidates[1].Kind != "relay" ||
		candidates[1].Source != "env:RDEV_RELAY_GATEWAY_URL" ||
		candidates[1].Recommended {
		t.Fatalf("expected configured relay fallback after LAN, got %#v", candidates)
	}
	if candidates[2].Kind != "mesh" || candidates[3].Kind != "loopback" {
		t.Fatalf("expected configured fallbacks before loopback, got %#v", candidates)
	}
}

func TestGatewayURLCandidatesPreferConfiguredFallbackOverLoopbackWhenNoPrivateIP(t *testing.T) {
	candidates := gatewayURLCandidatesFromIPsAndEnv("0.0.0.0:8787", "", nil, []GatewayEnvCandidate{
		{
			EnvVar: "RDEV_HOSTED_GATEWAY_URL",
			URL:    "https://hosted.example.test/rdev",
			Kind:   "hosted",
			Scope:  "operator-provided-hosted-gateway",
			Reason: "configured hosted gateway URL",
		},
	})

	if len(candidates) != 2 {
		t.Fatalf("expected hosted and loopback candidates, got %#v", candidates)
	}
	if candidates[0].Kind != "hosted" || !candidates[0].Recommended {
		t.Fatalf("expected configured hosted gateway to be recommended before loopback, got %#v", candidates)
	}
	if candidates[0].Host != "hosted.example.test" || candidates[0].Source != "env:RDEV_HOSTED_GATEWAY_URL" {
		t.Fatalf("expected parsed configured host/source metadata, got %#v", candidates[0])
	}
	if candidates[1].Kind != "loopback" || candidates[1].Recommended {
		t.Fatalf("expected loopback to be same-machine fallback only, got %#v", candidates[1])
	}
}

func TestConfiguredGatewayURLCandidateReadsRuntimeFallback(t *testing.T) {
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", "https://hosted.example.test/rdev")
	gatewayURL, candidates := ConfiguredGatewayURLCandidate()

	if gatewayURL != "https://hosted.example.test/rdev" {
		t.Fatalf("expected configured hosted gateway, got %q with candidates %#v", gatewayURL, candidates)
	}
	if len(candidates) != 1 ||
		candidates[0].Kind != "hosted" ||
		candidates[0].Source != "env:RDEV_HOSTED_GATEWAY_URL" ||
		!candidates[0].Recommended {
		t.Fatalf("expected configured hosted candidate metadata, got %#v", candidates)
	}
}

func TestBuildPlanStandardizesVisibleSupportSession(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "support")
	plan := BuildPlan(context.Background(), Options{
		RepoRoot:    ".",
		WorkDir:     workDir,
		GatewayURL:  "http://192.0.2.10:8787",
		Target:      "windows",
		Reason:      "company computer support",
		AutoApprove: true,
		Locale:      "zh-CN",
	})

	if plan["schema_version"] != PlanSchemaVersion || plan["ok"] != true {
		t.Fatalf("unexpected plan identity: %#v", plan)
	}
	autoApprove := plan["auto_approve"].(map[string]any)
	if autoApprove["enabled"] != true || !strings.Contains(autoApprove["scope"].(string), "attended-temporary") {
		t.Fatalf("expected scoped attended-temporary auto approval, got %#v", autoApprove)
	}
	commands := plan["commands"].(map[string]any)
	startGateway := strings.Join(anyStrings(commands["start_gateway"].([]string)), "\x00")
	if !strings.Contains(startGateway, "--rdev-windows-amd64") ||
		!strings.Contains(startGateway, "--rdev-darwin-amd64") ||
		!strings.Contains(startGateway, "--rdev-linux-arm64") {
		t.Fatalf("expected all helper asset flags, got %s", startGateway)
	}
	createInvite := strings.Join(anyStrings(commands["create_invite_cli"].([]string)), "\x00")
	if !strings.Contains(createInvite, "--auto-approve") {
		t.Fatalf("expected auto-approve invite command, got %s", createInvite)
	}
	watch := strings.Join(anyStrings(commands["watch_connection_status"].([]string)), "\x00")
	if !strings.Contains(watch, "support-session") || !strings.Contains(watch, "status") || !strings.Contains(watch, "--wait") {
		t.Fatalf("expected status watch command, got %s", watch)
	}
	gatewayCandidates := plan["gateway_url_candidates"].([]GatewayURLCandidate)
	if len(gatewayCandidates) == 0 || gatewayCandidates[0].URL != "http://192.0.2.10:8787" || !gatewayCandidates[0].Recommended {
		t.Fatalf("expected explicit gateway candidate, got %#v", gatewayCandidates)
	}
	target := plan["target_user_instructions"].(map[string]any)
	if !strings.Contains(target["message"].(string), "目标电脑") ||
		!strings.Contains(target["windows"].(string), "bootstrap.ps1") ||
		strings.Contains(target["windows"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("unexpected target instructions: %#v", target)
	}
}

func TestBuildHandoffRoutesFreshAgentToStandardEntry(t *testing.T) {
	handoff := BuildHandoff(HandoffOptions{
		GatewayURL:  "https://gateway.example.test",
		Target:      "windows",
		Reason:      "repair workstation",
		TTLSeconds:  600,
		AutoApprove: true,
		Locale:      "zh-CN",
		RdevCommand: "rdev",
	})

	args := handoff["mcp_next_arguments"].(map[string]any)
	forbidden := strings.Join(anyStrings(handoff["forbidden"].([]string)), "\n")
	if handoff["schema_version"] != HandoffSchemaVersion ||
		handoff["selected_path"] != "create-with-reachable-gateway" ||
		handoff["mcp_next_tool"] != "rdev.support_session.create" ||
		args["gateway_url"] != "https://gateway.example.test" ||
		args["target"] != "windows" ||
		!strings.Contains(handoff["agent_next_step"].(string), "rdev.support_session.create") ||
		!strings.Contains(forbidden, "Agent-authored PowerShell or shell bootstrap/recovery scripts") {
		t.Fatalf("expected create handoff route, got %#v", handoff)
	}
}

func TestBuildHandoffRoutesMissingGatewayToForegroundStart(t *testing.T) {
	handoff := BuildHandoff(HandoffOptions{
		Addr:        "0.0.0.0:8787",
		Target:      "auto",
		AutoApprove: true,
		RdevCommand: "rdev",
	})

	startCommand := strings.Join(anyStrings(handoff["foreground_start_command"].([]string)), "\x00")
	if handoff["schema_version"] != HandoffSchemaVersion ||
		handoff["selected_path"] != "start-foreground-gateway" ||
		handoff["mcp_next_tool"] != "" ||
		!strings.Contains(startCommand, "support-session\x00start") ||
		!strings.Contains(handoff["agent_rule"].(string), "do not choose support-session plan") {
		t.Fatalf("expected foreground start handoff route, got %#v", handoff)
	}
}

func TestBuildHandoffUsesConfiguredGatewayWithoutExplicitURL(t *testing.T) {
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", "https://hosted.example.test/rdev")
	handoff := BuildHandoff(HandoffOptions{
		Target:      "auto",
		AutoApprove: true,
		RdevCommand: "rdev",
	})

	args := handoff["mcp_next_arguments"].(map[string]any)
	if handoff["schema_version"] != HandoffSchemaVersion ||
		handoff["selected_path"] != "create-with-reachable-gateway" ||
		handoff["mcp_next_tool"] != "rdev.support_session.create" ||
		handoff["gateway_url"] != "https://hosted.example.test/rdev" ||
		args["gateway_url"] != "https://hosted.example.test/rdev" {
		t.Fatalf("expected configured gateway create route, got %#v", handoff)
	}
}

func TestBuildConnectFromHandoffRoutesMissingGatewayToStart(t *testing.T) {
	handoff := BuildHandoff(HandoffOptions{
		Addr:        "0.0.0.0:8787",
		Target:      "auto",
		AutoApprove: true,
		RdevCommand: "rdev",
	})
	connect := BuildConnectFromHandoff(handoff)
	startCommand := strings.Join(anyStrings(connect["foreground_start_command"].([]string)), "\x00")
	startNowCommand := strings.Join(anyStrings(connect["cli_start_now_command"].([]string)), "\x00")

	if connect["schema_version"] != ConnectSchemaVersion ||
		connect["selected_path"] != "start-foreground-gateway" ||
		connect["ready_to_send_to_human"] != false ||
		!strings.Contains(startNowCommand, "support-session\x00connect\x00--start") ||
		!strings.Contains(startCommand, "support-session\x00start") ||
		!strings.Contains(connect["agent_next_step"].(string), "cli_start_now_command") ||
		!strings.Contains(connect["agent_next_step"].(string), "ready_file.path") {
		t.Fatalf("expected connect payload to route to foreground start, got %#v", connect)
	}
}

func TestBuildConnectFromCreatedIsReadyForHumanHandoff(t *testing.T) {
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "http://192.0.2.10:8787",
		ManifestRootPublicKey: "manifest-root:abc",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "auto",
		Locale:                "en",
		RdevCommand:           "rdev",
		AutoApprove:           true,
	})
	connect := BuildConnectFromCreated(created)

	if connect["schema_version"] != ConnectSchemaVersion ||
		connect["selected_path"] != "created-with-reachable-gateway" ||
		connect["ready_to_send_to_human"] != true ||
		connect["user_handoff"] == nil ||
		connect["connection_supervision"] == nil ||
		connect["gateway_candidate_preflight"] == nil ||
		connect["agent_connection_runbook"] == nil ||
		connect["target_command"] != created["target_command"] ||
		!strings.Contains(connect["agent_next_step"].(string), "connected_next_steps.user_report") {
		t.Fatalf("expected ready connect payload, got %#v", connect)
	}
	runbook := connect["agent_connection_runbook"].(map[string]any)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		!strings.Contains(runbook["agent_rule"].(string), "runbook before choosing lower-level") {
		t.Fatalf("expected connect runbook, got %#v", runbook)
	}
}

func TestBuildCreatedReturnsReadyCommandsWithoutPlaceholders(t *testing.T) {
	created := BuildCreated(CreatedOptions{
		GatewayURL: "http://192.0.2.10:8787",
		GatewayURLCandidates: []GatewayURLCandidate{
			{URL: "http://192.0.2.10:8787", Kind: "explicit", Recommended: true},
			{URL: "http://198.51.100.10:8787", Kind: "host"},
		},
		ManifestRootPublicKey: "manifest-root:abc",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "windows",
		Locale:                "zh-CN",
		RdevCommand:           "rdev",
		AutoApprove:           true,
	})

	if created["schema_version"] != CreatedSchemaVersion ||
		created["target_command"] == "" ||
		!strings.Contains(created["target_command"].(string), "ABCD-1234") ||
		strings.Contains(created["target_command"].(string), "<ticket-code>") ||
		strings.Contains(created["target_command"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("expected ready Windows command without unsafe placeholders: %#v", created)
	}
	if !strings.Contains(created["target_command"].(string), "foreach ($u in $urls)") ||
		!strings.Contains(created["target_command"].(string), "198.51.100.10") ||
		!strings.Contains(created["target_command"].(string), "gateway_url_candidates=") ||
		!strings.Contains(created["target_command"].(string), "-TimeoutSec 10") ||
		!strings.Contains(created["target_commands"].(map[string]string)["macos_linux"], "for u in") {
		t.Fatalf("expected target command to try ordered gateway candidates: %#v", created["target_commands"])
	}
	handoff := created["user_handoff"].(map[string]any)
	if handoff["schema_version"] != UserHandoffSchemaVersion ||
		handoff["copy_paste_kind"] != "windows" ||
		handoff["copy_paste"] != created["target_command"] ||
		!strings.Contains(handoff["message"].(string), "目标电脑") ||
		!strings.Contains(handoff["agent_next_step"].(string), "wait=true") ||
		!strings.Contains(handoff["agent_rule"].(string), "do not rewrite") {
		t.Fatalf("expected ready localized user handoff, got %#v", handoff)
	}
	macLinuxCommand := created["target_commands"].(map[string]string)["macos_linux"]
	if !strings.Contains(macLinuxCommand, "--connect-timeout 2") ||
		!strings.Contains(macLinuxCommand, "--max-time 10") ||
		!strings.Contains(macLinuxCommand, "--retry 1") {
		t.Fatalf("expected bounded curl fallback command, got %s", macLinuxCommand)
	}
	attemptPolicy := created["connection_attempt_policy"].(map[string]any)
	if attemptPolicy["schema_version"] != ConnectionAttemptPolicySchemaVersion ||
		attemptPolicy["windows_download_timeout_sec"] != 10 ||
		attemptPolicy["curl_connect_timeout_sec"] != 2 ||
		attemptPolicy["retries_per_candidate"] != 1 {
		t.Fatalf("expected structured target attempt policy, got %#v", attemptPolicy)
	}
	continuityPolicy := created["connection_continuity_policy"].(map[string]any)
	if continuityPolicy["schema_version"] != ContinuityPolicySchemaVersion ||
		continuityPolicy["stable_after_lan_change"] != false ||
		continuityPolicy["assessment"] != "lan-or-explicit-path" ||
		!strings.Contains(continuityPolicy["agent_rule"].(string), "LAN as an opportunistic first path") {
		t.Fatalf("expected structured continuity policy, got %#v", continuityPolicy)
	}
	supervision := created["connection_supervision"].(map[string]any)
	watchCall := supervision["mcp_watch_call"].(map[string]any)
	watchArgs := watchCall["arguments"].(map[string]any)
	if supervision["schema_version"] != ConnectionSupervisionSchemaVersion ||
		supervision["stable_after_lan_change"] != false ||
		supervision["upgrade_recommended"] != true ||
		watchCall["tool"] != "rdev.support_session.status" ||
		watchArgs["ticket_code"] != "ABCD-1234" ||
		watchArgs["wait"] != true ||
		!strings.Contains(supervision["connected_report_rule"].(string), "connected_next_steps.user_report") {
		t.Fatalf("expected Agent-side connection supervision contract, got %#v", supervision)
	}
	candidateOrder := attemptPolicy["candidate_order"].([]map[string]any)
	if len(candidateOrder) != 2 ||
		candidateOrder[0]["join_url"] == "" ||
		candidateOrder[1]["kind"] != "host" {
		t.Fatalf("expected ordered candidate policy, got %#v", candidateOrder)
	}
	candidates := created["gateway_url_candidates"].([]GatewayURLCandidate)
	if len(candidates) != 2 || !candidates[0].Recommended {
		t.Fatalf("expected gateway candidates in created payload, got %#v", candidates)
	}
	preflight := created["gateway_candidate_preflight"].(map[string]any)
	if preflight["schema_version"] != GatewayCandidatePreflightSchemaVersion ||
		preflight["candidate_count"] != 2 ||
		!strings.Contains(preflight["agent_rule"].(string), "target command owns ordered URL fallback") {
		t.Fatalf("expected gateway candidate preflight contract, got %#v", preflight)
	}
	preflightCandidates := preflight["candidates"].([]map[string]any)
	if preflightCandidates[0]["status"] != "operator-provided-unverified" ||
		preflightCandidates[0]["same_machine_only"] != false {
		t.Fatalf("expected operator-provided preflight candidate, got %#v", preflightCandidates)
	}
	runbook := created["agent_connection_runbook"].(map[string]any)
	watchRunbook := runbook["watch"].(map[string]any)
	standardEntry := runbook["standard_entry_tool"].(map[string]any)
	lowLevelEntry := runbook["low_level_entry_rule"].(map[string]any)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		runbook["phase"] != "created" ||
		watchRunbook["mcp_tool"] != "rdev.support_session.status" ||
		standardEntry["mcp_tool"] != "rdev.support_session.connect" ||
		!strings.Contains(strings.Join(runbook["sequence"].([]string), "\n"), "connected_next_steps.user_report") ||
		!strings.Contains(strings.Join(runbook["forbidden"].([]string), "\n"), "Agent-authored PowerShell") ||
		!strings.Contains(strings.Join(lowLevelEntry["do_not_start_with"].([]string), "\n"), "rdev.connection_entry.plan") {
		t.Fatalf("expected Agent connection runbook, got %#v", runbook)
	}
	followUp := created["mcp_follow_up"].([]map[string]any)
	if len(followUp) == 0 || followUp[0]["arguments"].(map[string]any)["wait"] != true {
		t.Fatalf("expected MCP follow-up to wait for the host, got %#v", followUp)
	}
	watch := strings.Join(created["watch_connection_status"].([]string), "\x00")
	if !strings.Contains(watch, "ABCD-1234") ||
		strings.Contains(watch, "<ticket-code>") ||
		!strings.Contains(watch, "--wait") {
		t.Fatalf("expected ready status watcher, got %s", watch)
	}
	configuredWatcher := created["watch_connection_status_configured_gateway"].(map[string]any)
	configuredCommand := strings.Join(configuredWatcher["command"].([]string), "\x00")
	if !strings.Contains(configuredCommand, "ABCD-1234") ||
		!strings.Contains(configuredCommand, "--wait") ||
		strings.Contains(configuredCommand, "--gateway-url") ||
		!strings.Contains(configuredWatcher["agent_rule"].(string), "RDEV_*_GATEWAY_URL") {
		t.Fatalf("expected configured-gateway status watcher, got %#v", configuredWatcher)
	}
	flow := strings.Join(created["agent_flow"].([]string), "\n")
	if !strings.Contains(flow, "proactively report") ||
		!strings.Contains(flow, "do not ask the human to assemble") {
		t.Fatalf("expected Agent-native flow, got %s", flow)
	}
}

func TestConnectionContinuityPolicyReportsConfiguredStableFallback(t *testing.T) {
	policy := connectionContinuityPolicy([]GatewayURLCandidate{
		{URL: "http://192.168.50.10:8787", Kind: "lan-private"},
		{URL: "https://relay.example.test/rdev", Kind: "relay", Source: "env:RDEV_RELAY_GATEWAY_URL"},
	})

	stableKinds := strings.Join(policy["stable_fallback_kinds"].([]string), ",")
	if policy["schema_version"] != ContinuityPolicySchemaVersion ||
		policy["has_lan_candidate"] != true ||
		policy["has_stable_configured_fallback"] != true ||
		policy["stable_after_lan_change"] != true ||
		policy["assessment"] != "stable-fallback-configured" ||
		!strings.Contains(stableKinds, "relay") {
		t.Fatalf("expected stable relay continuity policy, got %#v", policy)
	}
	supervision := connectionSupervision("ABCD-1234", "en", "rdev", map[string]any{
		"candidate_order": []map[string]any{{"join_url": "https://relay.example.test/join/ABCD-1234", "kind": "relay"}},
	}, policy)
	if supervision["schema_version"] != ConnectionSupervisionSchemaVersion ||
		supervision["stable_after_lan_change"] != true ||
		supervision["upgrade_recommended"] != false ||
		!strings.Contains(supervision["upgrade_reason"].(string), "already configured") {
		t.Fatalf("expected supervision to recognize stable fallback, got %#v", supervision)
	}
}

func TestBuildCreatedAutoTargetReturnsJoinURLHandoffWithCommandFallbacks(t *testing.T) {
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "http://192.0.2.10:8787",
		ManifestRootPublicKey: "manifest-root:abc",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "auto",
		Locale:                "en",
		AutoApprove:           true,
	})

	if created["recommended_surface"] != "join_url" ||
		created["target_command"] != "http://192.0.2.10:8787/join/ABCD-1234" {
		t.Fatalf("expected auto target to recommend join URL, got %#v", created)
	}
	handoff := created["user_handoff"].(map[string]any)
	if handoff["copy_paste_kind"] != "join_url" ||
		handoff["copy_paste"] != created["join_url"] ||
		!strings.Contains(handoff["auto_target_rule"].(string), "target platform is unknown") ||
		!strings.Contains(handoff["windows_command"].(string), "bootstrap.ps1") ||
		!strings.Contains(handoff["macos_linux_command"].(string), "bootstrap.sh") {
		t.Fatalf("expected auto target handoff with command fallbacks, got %#v", handoff)
	}
}

func TestBuildStartedWrapsForegroundGatewayAndSession(t *testing.T) {
	created := BuildCreated(CreatedOptions{
		GatewayURL: "http://127.0.0.1:8787",
		Ticket:     model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:     "linux",
		Locale:     "en",
	})
	started := BuildStarted(StartedOptions{
		Addr:       "127.0.0.1:8787",
		GatewayURL: "http://127.0.0.1:8787",
		WorkDir:    "work/rdev-support-session",
		ReadyFile:  "work/rdev-support-session/support-session-ready.json",
		Created:    created,
		AssetReport: map[string]any{
			"schema_version": "rdev.support-session-assets.v1",
			"all_ready":      true,
		},
		ConnectionReadiness: map[string]any{
			"ready":                        true,
			"target_bootstrap_self_repair": true,
		},
	})

	if started["schema_version"] != StartedSchemaVersion || started["ok"] != true {
		t.Fatalf("unexpected started payload: %#v", started)
	}
	session := started["session"].(map[string]any)
	if session["schema_version"] != CreatedSchemaVersion ||
		!strings.Contains(session["target_command"].(string), "ABCD-1234") {
		t.Fatalf("expected embedded created session, got %#v", session)
	}
	handoff := started["user_handoff"].(map[string]any)
	if started["ready_to_send_to_human"] != true ||
		handoff["schema_version"] != UserHandoffSchemaVersion ||
		handoff["copy_paste"] != started["target_command"] ||
		started["target_command"] != session["target_command"] ||
		started["join_url"] != session["join_url"] ||
		started["connection_supervision"] == nil {
		t.Fatalf("expected top-level human handoff mirror, got %#v", started)
	}
	watch := strings.Join(anyStrings(started["watch_connection_status"].([]string)), "\x00")
	if !strings.Contains(watch, "ABCD-1234") || !strings.Contains(watch, "--wait") {
		t.Fatalf("expected top-level watcher, got %#v", started["watch_connection_status"])
	}
	gateway := started["gateway"].(map[string]any)
	if gateway["lifecycle"] != "foreground-visible-process" ||
		!strings.Contains(gateway["stop"].(string), "interrupt") {
		t.Fatalf("expected visible foreground gateway lifecycle, got %#v", gateway)
	}
	if started["asset_report"].(map[string]any)["all_ready"] != true ||
		started["connection_readiness"].(map[string]any)["ready"] != true {
		t.Fatalf("expected asset/readiness reports in started payload, got %#v", started)
	}
	preflight := started["gateway_candidate_preflight"].(map[string]any)
	if preflight["schema_version"] != GatewayCandidatePreflightSchemaVersion ||
		!strings.Contains(preflight["agent_rule"].(string), "candidate table") {
		t.Fatalf("expected top-level gateway candidate preflight, got %#v", preflight)
	}
	runbook := started["agent_connection_runbook"].(map[string]any)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		!strings.Contains(strings.Join(runbook["sequence"].([]string), "\n"), "user_handoff.message") {
		t.Fatalf("expected top-level Agent runbook, got %#v", runbook)
	}
	feedback := started["foreground_feedback"].(map[string]any)
	if feedback["schema_version"] != "rdev.support-session-foreground-feedback.v1" ||
		feedback["stream"] != "stderr" ||
		!strings.Contains(feedback["connected_rule"].(string), "connection has been established") {
		t.Fatalf("expected foreground feedback contract, got %#v", feedback)
	}
	readyFile := started["ready_file"].(map[string]any)
	if readyFile["schema_version"] != "rdev.support-session-ready-file.v1" ||
		readyFile["path"] != "work/rdev-support-session/support-session-ready.json" ||
		readyFile["contains"] != StartedSchemaVersion ||
		!strings.Contains(readyFile["agent_rule"].(string), "user_handoff.copy_paste") {
		t.Fatalf("expected ready file metadata, got %#v", readyFile)
	}
	forbidden := strings.Join(started["forbidden"].([]string), "\n")
	if !strings.Contains(forbidden, "background hidden gateway") ||
		!strings.Contains(forbidden, "ExecutionPolicy Bypass") {
		t.Fatalf("expected start guardrails, got %s", forbidden)
	}
}

func TestPrepareReportsHelperAssetsAndRecovery(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "cmd", "rdev"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/rdevtest\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "cmd", "rdev", "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(t.TempDir(), "support")
	prepare, err := Prepare(context.Background(), PrepareOptions{
		RepoRoot:    repoRoot,
		WorkDir:     workDir,
		GatewayURL:  "http://127.0.0.1:8787",
		Target:      "windows",
		BuildAssets: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepare["schema_version"] != PrepareSchemaVersion || prepare["repo_root_valid"] != true {
		t.Fatalf("unexpected prepare identity: %#v", prepare)
	}
	readiness := prepare["connection_readiness"].(map[string]any)
	if readiness["target_bootstrap_self_repair"] != true || readiness["human_gets_one_command"] != true {
		t.Fatalf("expected one-command target bootstrap readiness, got %#v", readiness)
	}
	if readiness["gateway_url"] != "http://127.0.0.1:8787" {
		t.Fatalf("expected explicit gateway in readiness, got %#v", readiness)
	}
	connectivity := prepare["connectivity_strategy"].(map[string]any)
	order := connectivity["selection_order"].([]string)
	if connectivity["schema_version"] != "rdev.support-session-connectivity-strategy.v1" ||
		!strings.Contains(strings.Join(order, "\n"), "native-lan-gateway") ||
		!strings.Contains(strings.Join(order, "\n"), "existing-frp-or-chisel-relay") {
		t.Fatalf("expected adaptive connectivity strategy, got %#v", connectivity)
	}
	downgrade := strings.Join(connectivity["automatic_downgrade"].([]string), "\n")
	if !strings.Contains(downgrade, "falls back to HTTPS long-poll") ||
		!strings.Contains(downgrade, "short polling") {
		t.Fatalf("expected native transport downgrade rules, got %s", downgrade)
	}
	preflight := prepare["gateway_candidate_preflight"].(map[string]any)
	if preflight["schema_version"] != GatewayCandidatePreflightSchemaVersion ||
		preflight["preflight_mode"] != "local-classification-no-network-scan" ||
		preflight["candidate_count"] == 0 ||
		!strings.Contains(preflight["agent_rule"].(string), "target command owns ordered URL fallback") {
		t.Fatalf("expected gateway candidate preflight contract, got %#v", preflight)
	}
	readinessPreflight := readiness["gateway_candidate_preflight"].(map[string]any)
	if readinessPreflight["schema_version"] != GatewayCandidatePreflightSchemaVersion {
		t.Fatalf("expected readiness to mirror gateway candidate preflight, got %#v", readinessPreflight)
	}
	runbook := prepare["agent_connection_runbook"].(map[string]any)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		runbook["phase"] != "prepare" ||
		!strings.Contains(strings.Join(runbook["on_timeout_or_failure"].([]string), "\n"), "prepare --build-assets") {
		t.Fatalf("expected prepare Agent runbook, got %#v", runbook)
	}
	readinessRunbook := readiness["agent_connection_runbook"].(map[string]any)
	if readinessRunbook["schema_version"] != AgentConnectionRunbookSchemaVersion {
		t.Fatalf("expected readiness to mirror Agent runbook, got %#v", readinessRunbook)
	}
	assets := prepare["asset_report"].(map[string]any)
	if assets["all_ready"] != true || assets["build_assets"] != true {
		t.Fatalf("expected built helper assets, got %#v", assets)
	}
	for _, asset := range assets["assets"].([]map[string]any) {
		if asset["present"] != true || asset["sha256"] == "" {
			t.Fatalf("expected present hashed helper asset, got %#v", asset)
		}
	}
	recovery := strings.Join(prepare["standard_recovery"].([]string), "\n")
	if !strings.Contains(recovery, "do not write custom PowerShell") {
		t.Fatalf("expected standard recovery guardrail, got %s", recovery)
	}
}

func TestBuildStatusReportsConnectedFeedback(t *testing.T) {
	status := BuildStatus(StatusOptions{
		TicketCode: "ABCD-1234",
		Locale:     "zh-CN",
		Hosts: []model.Host{{
			ID:       "host_1",
			TicketID: "ticket_1",
			Status:   model.HostStatusActive,
			Name:     "win-dev",
			OS:       "windows",
			Arch:     "amd64",
		}},
	})

	if status["schema_version"] != StatusSchemaVersion ||
		status["connected"] != true ||
		status["status"] != "connected" ||
		!strings.Contains(status["feedback"].(string), "连接已经建立") {
		t.Fatalf("expected connected localized status, got %#v", status)
	}
	next := status["connected_next_steps"].(map[string]any)
	calls := next["mcp_next_calls"].([]map[string]any)
	if next["schema_version"] != ConnectedNextStepsSchemaVersion ||
		next["connected"] != true ||
		next["host_id"] != "host_1" ||
		!strings.Contains(next["user_report"].(string), "连接已经建立") ||
		len(calls) != 1 ||
		calls[0]["tool"] != "rdev.hosts.capabilities" ||
		calls[0]["arguments"].(map[string]any)["host_id"] != "host_1" {
		t.Fatalf("expected connected next-step contract, got %#v", next)
	}
	runbook := status["agent_connection_runbook"].(map[string]any)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		runbook["status"] != "connected" ||
		!strings.Contains(strings.Join(runbook["on_connected"].([]string), "\n"), "capabilities") {
		t.Fatalf("expected connected status runbook, got %#v", runbook)
	}
}

func TestBuildStatusIncludesStandardConnectionRecovery(t *testing.T) {
	status := BuildStatus(StatusOptions{
		TicketCode: "WAIT-1234",
		Locale:     "en",
	})

	recovery := status["connection_recovery"].(map[string]any)
	actions := strings.Join(anyStrings(recovery["agent_next_actions"].([]string)), "\n")
	forbidden := strings.Join(anyStrings(recovery["forbidden"].([]string)), "\n")
	if recovery["schema_version"] != ConnectionRecoverySchemaVersion ||
		recovery["status"] != "waiting" ||
		!strings.Contains(actions, "rdev.support_session.prepare") ||
		!strings.Contains(actions, "instead of writing ad hoc network scripts") ||
		!strings.Contains(forbidden, "Agent-authored PowerShell or shell relay scripts") {
		t.Fatalf("expected standard connection recovery contract, got %#v", recovery)
	}
	runbook := recovery["agent_connection_runbook"].(map[string]any)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		runbook["phase"] != "recovery" ||
		!strings.Contains(strings.Join(runbook["on_timeout_or_failure"].([]string), "\n"), "gateway_candidate_preflight") {
		t.Fatalf("expected recovery runbook, got %#v", runbook)
	}
	next := status["connected_next_steps"].(map[string]any)
	if next["schema_version"] != ConnectedNextStepsSchemaVersion ||
		next["connected"] != false ||
		next["host_id"] != "" ||
		next["mcp_next_calls"] != nil {
		t.Fatalf("waiting status should not invent connected next calls, got %#v", next)
	}
}

func anyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	out = append(out, values...)
	return out
}
