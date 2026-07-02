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
	flow := strings.Join(created["agent_flow"].([]string), "\n")
	if !strings.Contains(flow, "proactively report") ||
		!strings.Contains(flow, "do not ask the human to assemble") {
		t.Fatalf("expected Agent-native flow, got %s", flow)
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
	gateway := started["gateway"].(map[string]any)
	if gateway["lifecycle"] != "foreground-visible-process" ||
		!strings.Contains(gateway["stop"].(string), "interrupt") {
		t.Fatalf("expected visible foreground gateway lifecycle, got %#v", gateway)
	}
	if started["asset_report"].(map[string]any)["all_ready"] != true ||
		started["connection_readiness"].(map[string]any)["ready"] != true {
		t.Fatalf("expected asset/readiness reports in started payload, got %#v", started)
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
