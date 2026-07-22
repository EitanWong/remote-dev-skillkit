package supportsession

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
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

func TestSupportSessionRdevBuildArgsStripDebugInfo(t *testing.T) {
	args := supportSessionRdevBuildArgs("/tmp/rdev")
	joined := strings.Join(args, " ")
	if !slices.Contains(args, "-trimpath") ||
		!slices.Contains(args, "-ldflags=-s -w") ||
		!strings.Contains(joined, "-o /tmp/rdev") ||
		args[len(args)-1] != "./cmd/rdev-bootstrap" {
		t.Fatalf("expected stripped reproducible bootstrap build args, got %#v", args)
	}
}

func TestSupportSessionAssetSpecsCoverBootstrapTargets(t *testing.T) {
	binDir := t.TempDir()
	specs := supportSessionAssetSpecs(binDir)
	got := make(map[string]supportSessionAssetSpec, len(specs))
	for _, spec := range specs {
		got[spec.GOOS+"/"+spec.GOARCH] = spec
	}
	for _, platform := range []string{
		"windows/amd64",
		"windows/arm64",
		"darwin/amd64",
		"darwin/arm64",
		"linux/amd64",
		"linux/arm64",
	} {
		if _, ok := got[platform]; !ok {
			t.Fatalf("support-session assets omitted bootstrap target %q: %#v", platform, specs)
		}
	}
	if len(got) != 6 {
		t.Fatalf("support-session assets contain unexpected bootstrap targets: %#v", specs)
	}
}

func TestPrepareReportsCompressedAssetBudgetEvidence(t *testing.T) {
	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	content := bytes.Repeat([]byte("rdev-helper-payload\n"), 4096)
	for _, asset := range supportSessionAssetSpecs(binDir) {
		if err := os.MkdirAll(filepath.Dir(asset.Path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(asset.Path, content, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	prepare, err := Prepare(context.Background(), PrepareOptions{
		RepoRoot:   t.TempDir(),
		WorkDir:    workDir,
		GatewayURL: "https://gateway.example.test/rdev",
		Target:     "windows",
	})
	if err != nil {
		t.Fatal(err)
	}
	report := prepare["asset_report"].(map[string]any)
	if int64FromAny(t, report["download_budget_bytes"]) != supportSessionHelperGzipBudgetBytes ||
		int64FromAny(t, report["bootstrap_target_bytes"]) != supportSessionBootstrapTargetBytes ||
		report["all_gzip_within_budget"] != true ||
		report["bootstrap_connector_recommended"] != false ||
		!strings.Contains(report["first_connect_size_strategy"].(string), "1 MiB") ||
		!strings.Contains(report["first_connect_size_strategy"].(string), "rdev-bootstrap") ||
		report["publishes_native_first_connect_asset"] != true {
		t.Fatalf("expected aggregate compressed download budget evidence, got %#v", report)
	}
	assets := report["assets"].([]map[string]any)
	if len(assets) != 6 {
		t.Fatalf("expected six assets, got %#v", assets)
	}
	for _, asset := range assets {
		gzipURL, ok := asset["gzip_asset_url"].(string)
		if !ok || !strings.HasPrefix(gzipURL, "https://gateway.example.test/rdev/assets/") || !strings.HasSuffix(gzipURL, ".gz") {
			t.Fatalf("expected gzip asset URL, got %#v", asset)
		}
		sizeBytes := int64FromAny(t, asset["size_bytes"])
		gzipBytes := int64FromAny(t, asset["gzip_estimated_bytes"])
		if sizeBytes != int64(len(content)) ||
			gzipBytes <= 0 ||
			gzipBytes >= sizeBytes ||
			int64FromAny(t, asset["gzip_budget_bytes"]) != supportSessionHelperGzipBudgetBytes ||
			int64FromAny(t, asset["bootstrap_target_bytes"]) != supportSessionBootstrapTargetBytes ||
			asset["gzip_within_budget"] != true ||
			asset["bootstrap_target_met"] != true {
			t.Fatalf("expected compressed size evidence for asset, got %#v", asset)
		}
	}
}

func TestPrepareReportsDefaultFirstConnectAssetReality(t *testing.T) {
	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	content := deterministicBytes(2*1024*1024 + 17)
	for _, asset := range supportSessionAssetSpecs(binDir) {
		if err := os.MkdirAll(filepath.Dir(asset.Path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(asset.Path, content, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	prepare, err := Prepare(context.Background(), PrepareOptions{
		RepoRoot:   t.TempDir(),
		WorkDir:    workDir,
		GatewayURL: "https://gateway.example.test/rdev",
		Target:     "windows",
	})
	if err != nil {
		t.Fatal(err)
	}
	report := prepare["asset_report"].(map[string]any)
	if report["default_first_connect_surface"] != "signed-native-bootstrap" ||
		report["default_runner_download_kind"] != "signed-layered-core" ||
		report["first_task_requires_verified_core"] != true ||
		report["publishes_native_first_connect_asset"] != false {
		t.Fatalf("expected asset report to state default first-connect reality, got %#v", report)
	}
	if maxBytes := int64FromAny(t, report["default_bootstrap_gzip_estimated_max_bytes"]); maxBytes <= supportSessionBootstrapTargetBytes {
		t.Fatalf("expected oversized bootstrap gzip max to exceed target, got %d", maxBytes)
	}
	if report["default_bootstrap_meets_first_connect_target"] != false {
		t.Fatalf("expected oversized bootstrap not to pass the first-connect gate, got %#v", report)
	}
	nativeBootstrap := report["native_first_connect_asset"].(map[string]any)
	if nativeBootstrap["name"] != "rdev-bootstrap" ||
		nativeBootstrap["published"] != false ||
		nativeBootstrap["measured"] != true ||
		int64FromAny(t, nativeBootstrap["target_bytes"]) != supportSessionBootstrapTargetBytes ||
		nativeBootstrap["target_met"] != false ||
		!strings.Contains(nativeBootstrap["reason"].(string), "gate passes") {
		t.Fatalf("expected native bootstrap publication status, got %#v", nativeBootstrap)
	}
	assets := report["assets"].([]map[string]any)
	for _, asset := range assets {
		if asset["asset_role"] != "bootstrap" ||
			asset["used_by_default_first_connect"] != true ||
			asset["native_bootstrap_asset"] != true {
			t.Fatalf("expected bootstrap asset role evidence, got %#v", asset)
		}
	}
}

func deterministicBytes(n int) []byte {
	out := make([]byte, n)
	var x uint32 = 0x811c9dc5
	for i := range out {
		x ^= uint32(i) + 0x9e3779b9
		x *= 16777619
		out[i] = byte(x >> 16)
	}
	return out
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

func TestConfiguredGatewayURLCandidateReadsCloudflaredNamedTunnel(t *testing.T) {
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "https://rdev.example.test")
	gatewayURL, candidates := ConfiguredGatewayURLCandidate()

	if gatewayURL != "https://rdev.example.test" {
		t.Fatalf("expected named Cloudflare tunnel URL, got %q with candidates %#v", gatewayURL, candidates)
	}
	if len(candidates) != 1 ||
		candidates[0].Kind != "cloudflared-named" ||
		candidates[0].Scope != "configured-cloudflared-named-tunnel" ||
		candidates[0].Source != "env:RDEV_CLOUDFLARED_NAMED_TUNNEL_URL" ||
		!candidates[0].Recommended {
		t.Fatalf("expected configured named Cloudflare candidate metadata, got %#v", candidates)
	}
}

func TestGatewayCandidateSummaryExplainsEphemeralQuickTunnelAndStableOptions(t *testing.T) {
	summary := gatewayCandidateRunbookSummary([]GatewayURLCandidate{
		{
			URL:    "http://192.168.50.10:8787",
			Kind:   "lan-private",
			Scope:  "LAN/VPN/routed-private-network",
			Source: "local-interface",
		},
	})

	if summary["needs_public_tunnel"] != true ||
		summary["quick_tunnel_ephemeral"] != true {
		t.Fatalf("expected LAN-only summary to require public tunnel and mark quick tunnel ephemeral, got %#v", summary)
	}
	advice := summary["stable_gateway_advice"].(map[string]any)
	if !strings.Contains(advice["cloud_or_vps"].(string), "RDEV_HOSTED_GATEWAY_URL") ||
		!strings.Contains(advice["cloudflare_named_tunnel"].(string), "RDEV_CLOUDFLARED_NAMED_TUNNEL_URL") ||
		!strings.Contains(advice["do_not_persist_quick_tunnel"].(string), "trycloudflare.com") {
		t.Fatalf("expected stable gateway advice for hosted and named Cloudflare paths, got %#v", advice)
	}
}

func TestBuildPlanStandardizesVisibleSupportSession(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "support")
	plan := BuildPlan(context.Background(), Options{
		RepoRoot:     ".",
		WorkDir:      workDir,
		GatewayURL:   "http://192.0.2.10:8787",
		Target:       "windows",
		Reason:       "company computer support",
		AutoActivate: true,
		Locale:       "zh-CN",
	})

	if plan["schema_version"] != PlanSchemaVersion || plan["ok"] != true {
		t.Fatalf("unexpected plan identity: %#v", plan)
	}
	autoActivate := plan["auto_activate"].(map[string]any)
	if autoActivate["enabled"] != true || !strings.Contains(autoActivate["scope"].(string), "attended-temporary") {
		t.Fatalf("expected scoped attended-temporary auto activation, got %#v", autoActivate)
	}
	commands := plan["commands"].(map[string]any)
	startGateway := strings.Join(anyStrings(commands["start_gateway"].([]string)), "\x00")
	if !strings.Contains(startGateway, "--rdev-bootstrap-windows-amd64") ||
		!strings.Contains(startGateway, "--rdev-bootstrap-darwin-amd64") ||
		!strings.Contains(startGateway, "--rdev-bootstrap-linux-arm64") {
		t.Fatalf("expected all bootstrap asset flags, got %s", startGateway)
	}
	createInvite := strings.Join(anyStrings(commands["create_invite_cli"].([]string)), "\x00")
	if !strings.Contains(createInvite, "--auto-activate") {
		t.Fatalf("expected auto-activate invite command, got %s", createInvite)
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
		GatewayURL:   "https://gateway.example.test",
		Target:       "windows",
		Reason:       "repair workstation",
		TTLSeconds:   600,
		AutoActivate: true,
		Locale:       "zh-CN",
		RdevCommand:  "rdev",
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
		Addr:         "0.0.0.0:8787",
		Target:       "auto",
		AutoActivate: true,
		RdevCommand:  "rdev",
	})

	startCommand := strings.Join(anyStrings(handoff["foreground_start_command"].([]string)), "\x00")
	startNowCommand := strings.Join(anyStrings(handoff["cli_start_now_command"].([]string)), "\x00")
	prepareCommand := strings.Join(anyStrings(handoff["prepare_command"].([]string)), "\x00")
	if handoff["schema_version"] != HandoffSchemaVersion ||
		handoff["selected_path"] != "start-foreground-gateway" ||
		handoff["mcp_next_tool"] != "" ||
		!strings.Contains(startCommand, "support-session\x00start") ||
		strings.Contains(startCommand, "--gateway-url") ||
		strings.Contains(startNowCommand, "--gateway-url") ||
		strings.Contains(prepareCommand, "--gateway-url") ||
		!strings.Contains(handoff["agent_rule"].(string), "do not choose support-session plan") {
		t.Fatalf("expected foreground start handoff route, got %#v", handoff)
	}
}

func TestBuildHandoffPreservesExplicitCapabilities(t *testing.T) {
	handoff := BuildHandoff(HandoffOptions{
		Target: "windows", RdevCommand: "rdev",
		Capabilities: []string{"shell.user", "window.inspect", "screen.screenshot"},
	})
	for _, key := range []string{"cli_start_now_command", "foreground_start_command"} {
		joined := strings.Join(handoff[key].([]string), "\x00")
		if !strings.Contains(joined, "--capabilities\x00shell.user,window.inspect,screen.screenshot") {
			t.Fatalf("%s dropped capabilities: %v", key, handoff[key])
		}
	}
}

func TestBuildHandoffUsesConfiguredGatewayWithoutExplicitURL(t *testing.T) {
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", "https://hosted.example.test/rdev")
	handoff := BuildHandoff(HandoffOptions{
		Target:       "auto",
		AutoActivate: true,
		RdevCommand:  "rdev",
	})

	args := handoff["mcp_next_arguments"].(map[string]any)
	if handoff["schema_version"] != HandoffSchemaVersion ||
		handoff["selected_path"] != "create-with-reachable-gateway" ||
		handoff["mcp_next_tool"] != "rdev.support_session.create" ||
		handoff["gateway_url"] != "https://hosted.example.test/rdev" ||
		args["gateway_url"] != "https://hosted.example.test/rdev" ||
		!slices.Contains(handoff["cli_start_now_command"].([]string), "https://hosted.example.test/rdev") {
		t.Fatalf("expected configured gateway create route, got %#v", handoff)
	}
}

func TestBuildHandoffExplicitGatewayPreservesTunnelPolicyFlags(t *testing.T) {
	handoff := BuildHandoff(HandoffOptions{
		GatewayURL:                 "https://gateway.example.test",
		Target:                     "windows",
		RdevCommand:                "rdev",
		Region:                     "cn-mainland",
		ProviderPolicyPath:         "/secure/providers.json",
		AllowDegradedDirectHandoff: true,
	})
	for _, key := range []string{"cli_start_now_command", "foreground_start_command"} {
		command := handoff[key].([]string)
		joined := strings.Join(command, "\x00")
		if !strings.Contains(joined, "--gateway-url\x00https://gateway.example.test") ||
			!strings.Contains(joined, "--region\x00cn-mainland") ||
			!strings.Contains(joined, "--provider-policy\x00/secure/providers.json") ||
			!slices.Contains(command, "--allow-degraded-direct-handoff") {
			t.Fatalf("%s dropped explicit tunnel policy: %v", key, command)
		}
	}
}

func TestBuildConnectFromHandoffRoutesMissingGatewayToStart(t *testing.T) {
	handoff := BuildHandoff(HandoffOptions{
		Addr:         "0.0.0.0:8787",
		Target:       "auto",
		AutoActivate: true,
		RdevCommand:  "rdev",
	})
	connect := BuildConnectFromHandoff(handoff)
	startCommand := strings.Join(anyStrings(connect["foreground_start_command"].([]string)), "\x00")
	startNowCommand := strings.Join(anyStrings(connect["cli_start_now_command"].([]string)), "\x00")

	if connect["schema_version"] != ConnectSchemaVersion ||
		connect["selected_path"] != "start-foreground-gateway" ||
		connect["ready_to_send_to_human"] != false ||
		!strings.Contains(startNowCommand, "support-session\x00connect\x00--start") ||
		strings.Contains(startNowCommand, "--gateway-url") ||
		strings.Contains(startCommand, "--gateway-url") ||
		!strings.Contains(startCommand, "support-session\x00start") ||
		!strings.Contains(connect["agent_next_step"].(string), "cli_start_now_command") ||
		!strings.Contains(connect["agent_next_step"].(string), "ready_file.path") ||
		!strings.Contains(connect["agent_next_step"].(string), "status_file.path") {
		t.Fatalf("expected connect payload to route to foreground start, got %#v", connect)
	}
	contract := connect["fresh_agent_connect_contract"].(map[string]any)
	if contract["schema_version"] != FreshAgentConnectContractSchemaVersion ||
		contract["ready_to_send_human"] != false ||
		contract["first_tool"] != "rdev.sessions.connect" ||
		!strings.Contains(strings.Join(contract["recovery_if_rdev_missing"].([]string), "\n"), "go install ./cmd/rdev") ||
		!strings.Contains(strings.Join(contract["agent_must_not_generate"].([]string), "\n"), "PowerShell bootstrap code") {
		t.Fatalf("expected fresh-Agent connect contract, got %#v", contract)
	}
}

func TestBuildConnectFromCreatedFailsClosedWithoutAvailabilityReadiness(t *testing.T) {
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "http://192.0.2.10:8787",
		ManifestRootPublicKey: "manifest-root:abc",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "auto",
		Locale:                "en",
		RdevCommand:           "rdev",
		AutoActivate:          true,
	})
	connect := BuildConnectFromCreated(created)

	if connect["schema_version"] != ConnectSchemaVersion ||
		connect["selected_path"] != "created-with-reachable-gateway" ||
		connect["ready_to_send_to_human"] != false ||
		connect["user_handoff"] == nil ||
		connect["target_handoff_envelope"] == nil ||
		connect["connection_supervision"] == nil ||
		connect["gateway_candidate_preflight"] == nil ||
		connect["connectivity_helper_preflight"] == nil ||
		connect["connection_entry_runner_recommendation"] == nil ||
		connect["agent_connection_runbook"] == nil ||
		connect["fresh_agent_connect_contract"] == nil ||
		connect["target_command"] != created["target_command"] ||
		!strings.Contains(strings.ToLower(connect["agent_next_step"].(string)), "do not send") {
		t.Fatalf("expected fail-closed connect payload with compatibility fields, got %#v", connect)
	}
	contract := connect["fresh_agent_connect_contract"].(map[string]any)
	autoActivate := contract["auto_activate"].(map[string]any)
	if contract["schema_version"] != FreshAgentConnectContractSchemaVersion ||
		contract["ready_to_send_human"] != false ||
		contract["ticket_code"] != "ABCD-1234" ||
		autoActivate["enabled"] != true ||
		!strings.Contains(contract["human_surface"].(string), "target_handoff_envelope.full_text") ||
		!strings.Contains(strings.Join(contract["do_not_ask_human_for"].([]string), "\n"), "manifest root public key") ||
		!strings.Contains(strings.ToLower(contract["status_rule"].(string)), "do not send") {
		t.Fatalf("expected ready fresh-Agent connect contract, got %#v", contract)
	}
	envelope := connect["target_handoff_envelope"].(map[string]any)
	if envelope["schema_version"] != TargetHandoffEnvelopeSchemaVersion ||
		envelope["ready_to_forward"] != false ||
		!strings.Contains(envelope["full_text"].(string), "ABCD-1234") ||
		!strings.Contains(strings.ToLower(envelope["agent_rule"].(string)), "do not send") {
		t.Fatalf("expected ready target handoff envelope, got %#v", envelope)
	}
	connectHelperPreflight := connect["connectivity_helper_preflight"].(map[string]any)
	createdHelperPreflight := created["connectivity_helper_preflight"].(map[string]any)
	if connectHelperPreflight["schema_version"] != createdHelperPreflight["schema_version"] ||
		connectHelperPreflight["agent_rule"] != createdHelperPreflight["agent_rule"] {
		t.Fatalf("expected connect payload to mirror helper preflight, got %#v", connectHelperPreflight)
	}
	connectRunner := connect["connection_entry_runner_recommendation"].(map[string]any)
	createdRunner := created["connection_entry_runner_recommendation"].(map[string]any)
	if connectRunner["schema_version"] != createdRunner["schema_version"] ||
		connectRunner["invite_json"] != createdRunner["invite_json"] {
		t.Fatalf("expected connect payload to mirror runner recommendation, got %#v", connectRunner)
	}
	connectBootstrap, ok := connect["rdev_bootstrap_connector"].(map[string]any)
	if !ok {
		t.Fatalf("expected connect payload to mirror bootstrap connector contract, got %#v", connect["rdev_bootstrap_connector"])
	}
	createdBootstrap := created["rdev_bootstrap_connector"].(map[string]any)
	if connectBootstrap["schema_version"] != BootstrapConnectorSchemaVersion ||
		connectBootstrap["agent_rule"] != createdBootstrap["agent_rule"] {
		t.Fatalf("expected connect payload to mirror bootstrap connector contract, got %#v", connectBootstrap)
	}
	runbook := connect["agent_connection_runbook"].(map[string]any)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		!strings.Contains(runbook["agent_rule"].(string), "runbook before choosing lower-level") {
		t.Fatalf("expected connect runbook, got %#v", runbook)
	}
}

func TestBuildConnectMapsAvailabilityReadinessAliases(t *testing.T) {
	readiness := DirectAvailability(tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates: []tunnel.Candidate{
			{ProviderID: "stable", URL: "https://gateway.example"},
		},
	}, true)
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "https://gateway.example",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "auto",
		AvailabilityReadiness: readiness,
	})
	connect := BuildConnectFromCreated(created)

	for name, payload := range map[string]map[string]any{"created": created, "connect": connect} {
		got, ok := payload["availability_readiness"].(AvailabilityReadiness)
		if !ok || !reflect.DeepEqual(got, readiness) {
			t.Fatalf("%s readiness mismatch: %#v", name, payload["availability_readiness"])
		}
		if payload["ready_to_send_to_human"] != readiness.ReadyToSend {
			t.Fatalf("%s compatibility alias must derive from readiness: %#v", name, payload)
		}
		if payload["ready_to_send"] != readiness.ReadyToSend ||
			payload["ready_to_activate"] != readiness.ReadyToActivate ||
			payload["ready_to_execute"] != readiness.ReadyToExecute {
			t.Fatalf("%s readiness states must derive from readiness: %#v", name, payload)
		}
		contract := payload["fresh_agent_connect_contract"].(map[string]any)
		if contract["ready_to_send_human"] != readiness.ReadyToSend ||
			contract["ready_to_send"] != readiness.ReadyToSend ||
			contract["ready_to_activate"] != readiness.ReadyToActivate ||
			contract["ready_to_execute"] != readiness.ReadyToExecute {
			t.Fatalf("%s fresh-agent alias must derive from readiness: %#v", name, contract)
		}
	}

	started := BuildStarted(StartedOptions{
		Created:               created,
		AssetReport:           map[string]any{"all_ready": true},
		AvailabilityReadiness: readiness,
	})
	got, ok := started["availability_readiness"].(AvailabilityReadiness)
	if !ok || !reflect.DeepEqual(got, readiness) ||
		started["ready_to_send_to_human"] != readiness.ReadyToSend ||
		started["ready_to_send"] != readiness.ReadyToSend ||
		started["ready_to_activate"] != readiness.ReadyToActivate ||
		started["ready_to_execute"] != readiness.ReadyToExecute {
		t.Fatalf("started readiness mapping mismatch: %#v", started)
	}
}

func TestBuildPayloadsForbidHandoffWhenNotReadyToSend(t *testing.T) {
	readiness := DirectAvailability(tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates:    []tunnel.Candidate{{ProviderID: "provider", URL: "https://gateway.example"}},
	}, false)
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "https://gateway.example",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "auto",
		AvailabilityReadiness: readiness,
	})
	connect := BuildConnectFromCreated(created)
	started := BuildStarted(StartedOptions{
		Created:               created,
		AssetReport:           map[string]any{"all_ready": true},
		AvailabilityReadiness: readiness,
	})

	for name, payload := range map[string]map[string]any{"created": created, "connect": connect, "started": started} {
		envelope := payload["target_handoff_envelope"].(map[string]any)
		if envelope["ready_to_forward"] != false ||
			!strings.Contains(strings.ToLower(envelope["agent_rule"].(string)), "do not send") ||
			!strings.Contains(strings.ToLower(envelope["after_send"].(string)), "do not send") {
			t.Fatalf("%s envelope must forbid forwarding: %#v", name, envelope)
		}
		handoff := payload["user_handoff"].(map[string]any)
		if !strings.Contains(strings.ToLower(handoff["agent_next_step"].(string)), "do not send") {
			t.Fatalf("%s user handoff must forbid sending: %#v", name, handoff)
		}
		contract := payload["fresh_agent_connect_contract"].(map[string]any)
		if !strings.Contains(strings.ToLower(contract["human_surface"].(string)), "do not send") {
			t.Fatalf("%s human surface must forbid sending: %#v", name, contract)
		}
	}
	if !strings.Contains(strings.ToLower(strings.Join(created["agent_flow"].([]string), "\n")), "do not send") ||
		!strings.Contains(strings.ToLower(connect["agent_next_step"].(string)), "do not send") ||
		!strings.Contains(strings.ToLower(connect["human_surface_rule"].(string)), "do not send") ||
		!strings.Contains(strings.ToLower(strings.Join(started["agent_flow"].([]string), "\n")), "do not send") ||
		!strings.Contains(strings.ToLower(started["human_surface_rule"].(string)), "do not send") {
		t.Fatalf("payload instructions must consistently forbid sending: created=%#v connect=%#v started=%#v", created, connect, started)
	}
}

func TestBuildStartedRequiresReadyAssetsToSend(t *testing.T) {
	readiness := DirectAvailability(tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates:    []tunnel.Candidate{{ProviderID: "provider", URL: "https://gateway.example"}},
	}, true)
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "https://gateway.example",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		AvailabilityReadiness: readiness,
	})

	blocked := BuildStarted(StartedOptions{Created: created, AvailabilityReadiness: readiness})
	blockedReadiness := blocked["availability_readiness"].(AvailabilityReadiness)
	blockedEnvelope := blocked["target_handoff_envelope"].(map[string]any)
	if blocked["ready_to_send"] != false || blocked["ready_to_send_to_human"] != false || blockedReadiness.ReadyToSend ||
		blockedEnvelope["ready_to_forward"] != false ||
		!strings.Contains(strings.ToLower(blocked["handoff_blocked_reason"].(string)), "bootstrap assets") {
		t.Fatalf("missing asset report must block sending: %#v", blocked)
	}

	ready := BuildStarted(StartedOptions{
		Created:               created,
		AssetReport:           map[string]any{"all_ready": true},
		AvailabilityReadiness: readiness,
	})
	if ready["ready_to_send"] != true || ready["ready_to_send_to_human"] != true ||
		ready["target_handoff_envelope"].(map[string]any)["ready_to_forward"] != true {
		t.Fatalf("ready assets and explicit override should permit sending: %#v", ready)
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
		AutoActivate:          true,
	})

	if created["schema_version"] != CreatedSchemaVersion ||
		created["target_command"] == "" ||
		strings.Contains(created["target_command"].(string), "<ticket-code>") ||
		strings.Contains(created["target_command"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("expected ready Windows command without unsafe placeholders: %#v", created)
	}
	if !strings.Contains(created["target_command"].(string), "powershell -NoProfile -Command") ||
		!strings.Contains(created["target_command"].(string), "bootstrap.ps1") ||
		!strings.Contains(created["target_command"].(string), "-UseBasicParsing") ||
		strings.Contains(created["target_command"].(string), "gateway_url_candidates=") ||
		strings.Contains(created["target_command"].(string), "-EncodedCommand") ||
		strings.Contains(created["target_command"].(string), "foreach ($u in $urls)") ||
		strings.Contains(created["target_command"].(string), "$urls has been generated by rdev") ||
		strings.Contains(created["target_command"].(string), "ProgressPrference") ||
		!strings.Contains(created["target_commands"].(map[string]string)["macos_linux"], "for u in") {
		t.Fatalf("expected readable Windows command plus structured gateway candidates: %#v", created["target_commands"])
	}
	if macLinux := created["target_commands"].(map[string]string)["macos_linux"]; !strings.Contains(macLinux, "198.51.100.10") ||
		strings.Contains(macLinux, "gateway_url_candidates=") ||
		!strings.Contains(macLinux, "--max-time 10") {
		t.Fatalf("expected macOS/Linux command to keep ordered gateway fallback without candidate query: %s", macLinux)
	}
	handoff := created["user_handoff"].(map[string]any)
	if handoff["schema_version"] != UserHandoffSchemaVersion ||
		handoff["copy_paste_kind"] != "windows" ||
		handoff["copy_paste"] != created["target_command"] ||
		!strings.Contains(handoff["message"].(string), "目标电脑") ||
		!strings.Contains(strings.ToLower(handoff["agent_next_step"].(string)), "do not send") ||
		!strings.Contains(strings.ToLower(handoff["agent_rule"].(string)), "do not send") {
		t.Fatalf("expected ready localized user handoff, got %#v", handoff)
	}
	envelope := created["target_handoff_envelope"].(map[string]any)
	if envelope["schema_version"] != TargetHandoffEnvelopeSchemaVersion ||
		envelope["ready_to_forward"] != false ||
		envelope["copy_paste"] != created["target_command"] ||
		!strings.Contains(envelope["full_text"].(string), envelope["message"].(string)) ||
		!strings.Contains(envelope["full_text"].(string), created["target_command"].(string)) ||
		!strings.Contains(envelope["full_text"].(string), "Device ID: RDEV-ABCD-1234") ||
		!strings.Contains(envelope["full_text"].(string), "Session Password: ABCD-1234") ||
		!strings.Contains(strings.Join(envelope["forbidden"].([]string), "\n"), "ExecutionPolicy Bypass") {
		t.Fatalf("expected target handoff envelope, got %#v", envelope)
	}
	remoteEntry := created["remote_control_entry"].(map[string]any)
	if remoteEntry["schema_version"] != RemoteControlEntrySchemaVersion ||
		remoteEntry["support_device_id"] != "RDEV-ABCD-1234" ||
		remoteEntry["session_passcode"] != "ABCD-1234" ||
		remoteEntry["explicit_disconnect_required"] != true ||
		!strings.Contains(remoteEntry["agent_rule"].(string), "remote-control app entry") {
		t.Fatalf("expected remote-control entry, got %#v", remoteEntry)
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
	if !strings.Contains(strings.Join(supervision["automatic_downgrade_boundaries"].([]string), "\n"), "signed join-manifest gateway candidates") {
		t.Fatalf("expected supervision to describe signed gateway candidate runtime failover, got %#v", supervision["automatic_downgrade_boundaries"])
	}
	contract := created["fresh_agent_connect_contract"].(map[string]any)
	if contract["schema_version"] != FreshAgentConnectContractSchemaVersion ||
		contract["ready_to_send_human"] != false ||
		contract["first_tool"] != "rdev.sessions.connect" ||
		!strings.Contains(strings.Join(contract["do_not_ask_human_for"].([]string), "\n"), "gateway URL") ||
		!strings.Contains(strings.Join(contract["agent_must_not_generate"].([]string), "\n"), "relay, mesh, VPN, SSH, or polling scripts") {
		t.Fatalf("expected created fresh-Agent connect contract, got %#v", contract)
	}
	bootstrap, ok := created["rdev_bootstrap_connector"].(map[string]any)
	if !ok {
		t.Fatalf("expected Agent-readable rdev bootstrap connector contract, got %#v", created["rdev_bootstrap_connector"])
	}
	if bootstrap["schema_version"] != "rdev.support-session-bootstrap-connector.v1" ||
		int64FromAny(t, bootstrap["first_connect_target_bytes"]) != supportSessionBootstrapTargetBytes ||
		bootstrap["default_first_connect_surface"] != "signed-native-bootstrap" ||
		bootstrap["publishes_native_first_connect_asset"] != true ||
		bootstrap["source"] != "rdev-bootstrap" ||
		bootstrap["grants_host_access"] != false ||
		bootstrap["can_run_session_tasks"] != false ||
		bootstrap["full_runner_phase"] != "download-signed-core-after-registration" ||
		bootstrap["must_not_skip_core_verification"] != true ||
		!slices.Contains(bootstrap["status_fields"].([]string), "target_preconnects") ||
		!slices.Contains(bootstrap["status_fields"].([]string), "target_preconnect_count") ||
		!strings.Contains(bootstrap["agent_rule"].(string), "connected=true") {
		t.Fatalf("expected bounded bootstrap connector contract, got %#v", bootstrap)
	}
	nativeBootstrap := bootstrap["native_connector"].(map[string]any)
	nativeCommand := nativeBootstrap["command"].([]string)
	if nativeBootstrap["schema_version"] != "rdev.bootstrap-native-connector.v1" ||
		nativeBootstrap["source"] != "rdev-bootstrap-native" ||
		nativeBootstrap["availability"] != "published-first-connect-asset" ||
		nativeBootstrap["published_by_support_session_assets"] != true ||
		nativeBootstrap["default_first_connect_surface"] != "signed-native-bootstrap" ||
		!slices.Contains(nativeCommand, "rdev-bootstrap") ||
		!slices.Contains(nativeCommand, "layered-run") ||
		!strings.Contains(strings.Join(nativeBootstrap["capabilities"].([]string), "\n"), "download verified core") ||
		nativeBootstrap["can_run_session_tasks_before_full_runner"] != false {
		t.Fatalf("expected native rdev-bootstrap layered contract, got %#v", nativeBootstrap)
	}
	optimizer := bootstrap["cdn_download_optimizer"].(map[string]any)
	if optimizer["schema_version"] != "rdev.cdn-optimizer-plan.v1" ||
		optimizer["provider"] != "cloudflare" ||
		optimizer["status"] != "dry-run-only" ||
		optimizer["enabled_by_default"] != false ||
		optimizer["requires_explicit_enable"] != true ||
		optimizer["asset_downloads_only"] != true {
		t.Fatalf("expected safe CDN optimizer dry-run plan, got %#v", optimizer)
	}
	forbidden := strings.Join(optimizer["forbidden_side_effects"].([]string), "\n")
	if !strings.Contains(forbidden, "DNS") ||
		!strings.Contains(forbidden, "hosts") ||
		!strings.Contains(forbidden, "proxy") ||
		!strings.Contains(forbidden, "firewall") {
		t.Fatalf("expected optimizer to forbid system network mutation, got %#v", optimizer["forbidden_side_effects"])
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
	runner := created["connection_entry_runner_recommendation"].(map[string]any)
	if runner["schema_version"] != ConnectionEntryRunnerRecommendationSchemaVersion ||
		runner["ok"] != true ||
		runner["standard_tool"] != "rdev.connection_entry.plan" ||
		runner["target_os"] != "windows" ||
		runner["recommended_now"] != true ||
		!strings.Contains(strings.Join(runner["agent_sequence"].([]string), "\n"), "dry-run the generated runner") ||
		!strings.Contains(strings.Join(runner["forbidden"].([]string), "\n"), "Agent-authored SSH") {
		t.Fatalf("expected runner recommendation contract, got %#v", runner)
	}
	var invite map[string]any
	if err := json.Unmarshal([]byte(runner["invite_json"].(string)), &invite); err != nil {
		t.Fatalf("expected valid inline invite JSON: %v", err)
	}
	if invite["schema_version"] != "rdev.agent-invite.v1" ||
		invite["gateway_url"] != "http://192.0.2.10:8787" ||
		invite["manifest_root_public_key"] != "manifest-root:abc" {
		t.Fatalf("expected runner recommendation to carry complete invite metadata, got %#v", invite)
	}
	mcpPlanCall := runner["mcp_plan_call"].(map[string]any)
	mcpPlanArgs := mcpPlanCall["arguments"].(map[string]any)
	if mcpPlanCall["tool"] != "rdev.connection_entry.plan" ||
		mcpPlanArgs["invite_json"] == "" ||
		mcpPlanArgs["target_os"] != "windows" ||
		mcpPlanArgs["session_mode"] != string(model.HostModeAttendedTemporary) {
		t.Fatalf("expected ready MCP runner materialization call, got %#v", mcpPlanCall)
	}
	dryRun := strings.Join(runner["cli_dry_run_argv_template"].([]string), "\x00")
	if !strings.Contains(dryRun, "connection-entry\x00run") || !strings.Contains(dryRun, "--dry-run") {
		t.Fatalf("expected dry-run runner command template, got %#v", runner["cli_dry_run_argv_template"])
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
	failurePrevention := runbook["fresh_agent_failure_prevention"].(map[string]any)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		runbook["phase"] != "created" ||
		watchRunbook["mcp_tool"] != "rdev.support_session.status" ||
		standardEntry["mcp_tool"] != "rdev.sessions.connect" ||
		!strings.Contains(strings.Join(runbook["sequence"].([]string), "\n"), "connected_next_steps.user_report") ||
		!strings.Contains(strings.Join(runbook["forbidden"].([]string), "\n"), "Agent-authored PowerShell") ||
		!strings.Contains(strings.Join(lowLevelEntry["do_not_start_with"].([]string), "\n"), "rdev.connection_entry.plan") {
		t.Fatalf("expected Agent connection runbook, got %#v", runbook)
	}
	if failurePrevention["schema_version"] != FreshAgentFailurePreventionSchemaVersion ||
		!strings.Contains(strings.Join(failurePrevention["known_failure_pattern"].([]string), "\n"), "bootstrap assets") ||
		!strings.Contains(strings.Join(failurePrevention["required_standard_path"].([]string), "\n"), "cli_start_now_command") ||
		!strings.Contains(strings.Join(failurePrevention["forbidden_agent_generated_workarounds"].([]string), "\n"), "ExecutionPolicy Bypass") {
		t.Fatalf("expected fresh-Agent failure-prevention contract, got %#v", failurePrevention)
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
		!strings.Contains(flow, "rdev_bootstrap_connector") ||
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
	supervision := connectionSupervision("ABCD-1234", "en", "rdev", "https://relay.example.test/rdev", map[string]any{
		"candidate_order": []map[string]any{{"join_url": "https://relay.example.test/join/ABCD-1234", "kind": "relay"}},
	}, policy)
	watchArgs := supervision["mcp_watch_call"].(map[string]any)["arguments"].(map[string]any)
	if supervision["schema_version"] != ConnectionSupervisionSchemaVersion ||
		supervision["stable_after_lan_change"] != true ||
		supervision["upgrade_recommended"] != false ||
		watchArgs["gateway_url"] != "https://relay.example.test/rdev" ||
		!strings.Contains(supervision["upgrade_reason"].(string), "already configured") {
		t.Fatalf("expected supervision to recognize stable fallback, got %#v", supervision)
	}
}

func TestBuildCreatedKeepsTargetInvitePortableWhenAgentRdevCommandIsAbsolute(t *testing.T) {
	agentRdev := "/Users/example/go/bin/rdev"
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "http://192.0.2.10:8787",
		ManifestRootPublicKey: "manifest-root:abc",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "windows",
		Locale:                "en",
		RdevCommand:           agentRdev,
		AutoActivate:          true,
	})

	supervision := created["connection_supervision"].(map[string]any)
	watchCommand := supervision["cli_watch_command"].([]string)
	if len(watchCommand) == 0 || watchCommand[0] != agentRdev {
		t.Fatalf("expected Agent-side watcher to use stable local rdev command, got %#v", watchCommand)
	}
	runner := created["connection_entry_runner_recommendation"].(map[string]any)
	inviteJSON := runner["invite_json"].(string)
	if strings.Contains(inviteJSON, "/Users/example") {
		t.Fatalf("target-side invite JSON must not leak Agent-local rdev path: %s", inviteJSON)
	}
	var invite map[string]any
	if err := json.Unmarshal([]byte(inviteJSON), &invite); err != nil {
		t.Fatalf("expected valid invite JSON: %v", err)
	}
	hostCommand, _ := invite["host_command"].(string)
	if !strings.Contains(hostCommand, "/bootstrap.sh") || !strings.Contains(hostCommand, "rdev-bootstrap") || strings.Contains(hostCommand, "/Users/example") {
		t.Fatalf("expected portable bootstrap-only target command, got %q", hostCommand)
	}
}

func TestBuildCreatedAutoTargetReturnsMultiPlatformHandoffWithCommandFallbacks(t *testing.T) {
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "http://192.0.2.10:8787",
		ManifestRootPublicKey: "manifest-root:abc",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "auto",
		Locale:                "en",
		AutoActivate:          true,
	})

	if created["recommended_surface"] != "multi_platform" ||
		created["target_command"] == "http://192.0.2.10:8787/join/ABCD-1234" {
		t.Fatalf("expected auto target to recommend multi-platform handoff, got %#v", created)
	}
	handoff := created["user_handoff"].(map[string]any)
	copyPaste, _ := handoff["copy_paste"].(string)
	if handoff["copy_paste_kind"] != "multi_platform" ||
		!strings.Contains(copyPaste, "Windows PowerShell") ||
		!strings.Contains(copyPaste, "macOS/Linux terminal") ||
		!strings.Contains(copyPaste, "Browser fallback") ||
		!strings.Contains(copyPaste, "bootstrap.ps1") ||
		!strings.Contains(copyPaste, "bootstrap.sh") ||
		!strings.Contains(strings.ToLower(handoff["auto_target_rule"].(string)), "do not send") ||
		!strings.Contains(handoff["windows_command"].(string), "bootstrap.ps1") ||
		!strings.Contains(handoff["macos_linux_command"].(string), "bootstrap.sh") {
		t.Fatalf("expected auto target handoff with executable command fallbacks, got %#v", handoff)
	}
	envelope := created["target_handoff_envelope"].(map[string]any)
	if !strings.Contains(envelope["full_text"].(string), "Windows PowerShell") ||
		!strings.Contains(envelope["full_text"].(string), "macOS/Linux terminal") ||
		envelope["copy_paste"] == created["join_url"] {
		t.Fatalf("expected envelope to avoid bare join URL, got %#v", envelope)
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
		Addr:                "127.0.0.1:8787",
		GatewayURL:          "http://127.0.0.1:8787",
		WorkDir:             "work/rdev-support-session",
		ReadyFile:           "work/rdev-support-session/support-session-ready.json",
		StatusFile:          "work/rdev-support-session/support-session-status.json",
		HandoffTextFile:     "work/rdev-support-session/target-handoff.txt",
		ConnectedReportFile: "work/rdev-support-session/connected-report.txt",
		Created:             created,
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
	if started["ready_to_send_to_human"] != false ||
		handoff["schema_version"] != UserHandoffSchemaVersion ||
		handoff["copy_paste"] != started["target_command"] ||
		started["target_command"] != session["target_command"] ||
		started["join_url"] != session["join_url"] ||
		started["connection_supervision"] == nil ||
		started["connectivity_helper_preflight"] == nil ||
		started["connection_entry_runner_recommendation"] == nil {
		t.Fatalf("expected top-level human handoff mirror, got %#v", started)
	}
	envelope := started["target_handoff_envelope"].(map[string]any)
	if envelope["schema_version"] != TargetHandoffEnvelopeSchemaVersion ||
		envelope["copy_paste"] != started["target_command"] ||
		!strings.Contains(strings.ToLower(envelope["after_send"].(string)), "do not send") {
		t.Fatalf("expected started top-level target handoff envelope, got %#v", envelope)
	}
	startedHelperPreflight := started["connectivity_helper_preflight"].(map[string]any)
	sessionHelperPreflight := session["connectivity_helper_preflight"].(map[string]any)
	if startedHelperPreflight["schema_version"] != sessionHelperPreflight["schema_version"] ||
		startedHelperPreflight["agent_rule"] != sessionHelperPreflight["agent_rule"] {
		t.Fatalf("expected started payload to mirror helper preflight, got %#v", startedHelperPreflight)
	}
	startedRunner := started["connection_entry_runner_recommendation"].(map[string]any)
	sessionRunner := session["connection_entry_runner_recommendation"].(map[string]any)
	if startedRunner["schema_version"] != sessionRunner["schema_version"] ||
		startedRunner["invite_json"] != sessionRunner["invite_json"] {
		t.Fatalf("expected started payload to mirror runner recommendation, got %#v", startedRunner)
	}
	startedBootstrap := started["rdev_bootstrap_connector"].(map[string]any)
	sessionBootstrap := session["rdev_bootstrap_connector"].(map[string]any)
	if startedBootstrap["schema_version"] != BootstrapConnectorSchemaVersion ||
		startedBootstrap["agent_rule"] != sessionBootstrap["agent_rule"] {
		t.Fatalf("expected started payload to mirror bootstrap connector contract, got %#v", startedBootstrap)
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
		!strings.Contains(strings.Join(runbook["sequence"].([]string), "\n"), "target_handoff_envelope.full_text") {
		t.Fatalf("expected top-level Agent runbook, got %#v", runbook)
	}
	feedback := started["foreground_feedback"].(map[string]any)
	if feedback["schema_version"] != "rdev.support-session-foreground-feedback.v1" ||
		feedback["stream"] != "stderr" ||
		feedback["log_event_schema_version"] != "rdev.support-session-foreground-log-event.v1" ||
		feedback["protected_status_schema_version"] != "rdev.support-session-foreground-event.v1" ||
		!strings.Contains(feedback["connected_rule"].(string), "connection has been established") {
		t.Fatalf("expected foreground feedback contract, got %#v", feedback)
	}
	readyFile := started["ready_file"].(map[string]any)
	if readyFile["schema_version"] != "rdev.support-session-ready-file.v1" ||
		readyFile["path"] != "work/rdev-support-session/support-session-ready.json" ||
		readyFile["contains"] != StartedSchemaVersion ||
		!strings.Contains(readyFile["agent_rule"].(string), "target_handoff_envelope.full_text") {
		t.Fatalf("expected ready file metadata, got %#v", readyFile)
	}
	statusFile := started["status_file"].(map[string]any)
	if statusFile["schema_version"] != StatusFileSchemaVersion ||
		statusFile["path"] != "work/rdev-support-session/support-session-status.json" ||
		statusFile["contains"] != "rdev.support-session-foreground-event.v1" ||
		statusFile["status_schema_version"] != StatusSchemaVersion ||
		!strings.Contains(statusFile["agent_rule"].(string), "connected_next_steps.user_report") {
		t.Fatalf("expected status file metadata, got %#v", statusFile)
	}
	handoffTextFile := started["handoff_text_file"].(map[string]any)
	if handoffTextFile["schema_version"] != HandoffTextFileSchemaVersion ||
		handoffTextFile["path"] != "work/rdev-support-session/target-handoff.txt" ||
		handoffTextFile["contains"] != "target_handoff_envelope.full_text" ||
		!strings.Contains(strings.ToLower(handoffTextFile["agent_rule"].(string)), "do not send") {
		t.Fatalf("expected handoff text file metadata, got %#v", handoffTextFile)
	}
	connectedReportFile := started["connected_report_file"].(map[string]any)
	if connectedReportFile["schema_version"] != ConnectedReportFileSchemaVersion ||
		connectedReportFile["path"] != "work/rdev-support-session/connected-report.txt" ||
		connectedReportFile["contains"] != "connected_next_steps.user_report" ||
		!strings.Contains(connectedReportFile["agent_rule"].(string), "plain text") {
		t.Fatalf("expected connected report file metadata, got %#v", connectedReportFile)
	}
	contract := started["fresh_agent_connect_contract"].(map[string]any)
	if contract["schema_version"] != FreshAgentConnectContractSchemaVersion ||
		contract["ready_to_send_human"] != false ||
		contract["ready_file_path"] != "work/rdev-support-session/support-session-ready.json" ||
		contract["status_file_path"] != "work/rdev-support-session/support-session-status.json" ||
		contract["handoff_text_file_path"] != "work/rdev-support-session/target-handoff.txt" ||
		contract["connected_report_file_path"] != "work/rdev-support-session/connected-report.txt" ||
		!strings.Contains(strings.Join(contract["agent_must_not_generate"].([]string), "\n"), "polling scripts") {
		t.Fatalf("expected started fresh-Agent connect contract, got %#v", contract)
	}
	forbidden := strings.Join(started["forbidden"].([]string), "\n")
	if !strings.Contains(forbidden, "background hidden gateway") ||
		!strings.Contains(forbidden, "ExecutionPolicy Bypass") {
		t.Fatalf("expected start guardrails, got %s", forbidden)
	}
}

func TestPrepareReportsBootstrapAssetsAndRecovery(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "cmd", "rdev-bootstrap"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/rdevtest\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "cmd", "rdev-bootstrap", "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
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
		t.Fatalf("expected built bootstrap assets, got %#v", assets)
	}
	for _, asset := range assets["assets"].([]map[string]any) {
		if asset["present"] != true || asset["sha256"] == "" {
			t.Fatalf("expected present hashed bootstrap asset, got %#v", asset)
		}
	}
	recovery := strings.Join(prepare["standard_recovery"].([]string), "\n")
	if !strings.Contains(recovery, "do not write custom PowerShell") {
		t.Fatalf("expected standard recovery guardrail, got %s", recovery)
	}
	encoded, err := json.Marshal(prepare)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, "bootstrap assets") {
		t.Fatalf("prepare report omitted bootstrap asset language: %s", text)
	}
	for _, forbidden := range []string{"helper assets", "platform rdev helper", "rdev is required", "rdev host serve"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("prepare report contains legacy connection language %q: %s", forbidden, text)
		}
	}
}

func TestPrepareDoesNotPromoteAutoLANToExplicitGatewayCommand(t *testing.T) {
	prepare, err := Prepare(context.Background(), PrepareOptions{
		Addr:   "0.0.0.0:8787",
		Target: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"command_to_connect_start", "command_to_start", "command_to_prepare_all"} {
		command := prepare[key].([]string)
		if slices.Contains(command, "--gateway-url") {
			t.Fatalf("%s should not pass auto LAN candidate as explicit gateway: %#v", key, command)
		}
	}
	candidates := prepare["gateway_url_candidates"].([]GatewayURLCandidate)
	if len(candidates) == 0 || candidates[0].Kind != "lan-private" {
		t.Fatalf("expected LAN candidates to remain available for diagnostics, got %#v", candidates)
	}
}

func TestPrepareReportsConnectivityHelperPreflight(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://127.0.0.1:8787")
	t.Setenv("RDEV_RELAY_START_ARGV_JSON", `["chisel","client","relay.example.invalid","R:8787:127.0.0.1:8787"]`)
	t.Setenv("RDEV_RELAY_INSTALL_ACTION_JSON", `{"schema_version":"rdev.connection-entry.dependency-install-action.v1","tool":"chisel","scope":"user","argv":["rdev","deps","install","chisel","--scope","user"],"expected_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`)
	prepare, err := Prepare(context.Background(), PrepareOptions{
		RepoRoot: ".",
		Target:   "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	preflight := prepare["connectivity_helper_preflight"].(map[string]any)
	if preflight["schema_version"] != ConnectivityHelperPreflightSchemaVersion ||
		preflight["ready_helper_count"] != 1 ||
		preflight["auto_execute"] != false ||
		!strings.Contains(preflight["agent_rule"].(string), "standard Connection Entry runner") {
		t.Fatalf("expected helper preflight contract, got %#v", preflight)
	}
	readiness := prepare["connection_readiness"].(map[string]any)
	if readiness["connectivity_helper_preflight"] == nil {
		t.Fatalf("expected readiness to mirror helper preflight, got %#v", readiness)
	}
	helpers := preflight["helpers"].([]map[string]any)
	var relay map[string]any
	for _, helper := range helpers {
		if helper["id"] == "existing-frp-or-chisel-relay" {
			relay = helper
			break
		}
	}
	if relay == nil ||
		relay["status"] != "ready-to-use-after-authorization-check" ||
		relay["gateway_configured"] != true ||
		relay["start_tool"] != "chisel" {
		t.Fatalf("expected configured relay helper report, got %#v", relay)
	}
	install := relay["install_action"].(map[string]any)
	if install["valid"] != true ||
		install["tool"] != "chisel" ||
		install["has_expected_sha256"] != true {
		t.Fatalf("expected valid relay install action report, got %#v", install)
	}
}

func TestPrepareReportsCloudflaredNamedTunnelPreflight(t *testing.T) {
	t.Setenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL", "https://rdev.example.test")
	t.Setenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN", "secret-token")
	prepare, err := Prepare(context.Background(), PrepareOptions{
		RepoRoot: ".",
		Target:   "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	preflight := prepare["connectivity_helper_preflight"].(map[string]any)
	helpers := preflight["helpers"].([]map[string]any)
	var named map[string]any
	for _, helper := range helpers {
		if helper["id"] == "cloudflared-named-tunnel" {
			named = helper
			break
		}
	}
	if named == nil ||
		named["status"] != "ready-to-use-after-authorization-check" ||
		named["gateway_configured"] != true ||
		named["token_configured"] != true ||
		named["gateway_url"] != "https://rdev.example.test" {
		t.Fatalf("expected named Cloudflare tunnel preflight without exposing token value, got %#v", named)
	}
	if strings.Contains(fmt.Sprintf("%v", named), "secret-token") {
		t.Fatalf("preflight must not expose token value: %#v", named)
	}
}

func TestPrepareRejectsUnsafeConnectivityHelperPreflight(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://127.0.0.1:8787")
	t.Setenv("RDEV_RELAY_START_ARGV_JSON", `["powershell","-ExecutionPolicy","Bypass","-Command","Install-Chisel"]`)
	prepare, err := Prepare(context.Background(), PrepareOptions{RepoRoot: "."})
	if err != nil {
		t.Fatal(err)
	}
	preflight := prepare["connectivity_helper_preflight"].(map[string]any)
	helpers := preflight["helpers"].([]map[string]any)
	var relay map[string]any
	for _, helper := range helpers {
		if helper["id"] == "existing-frp-or-chisel-relay" {
			relay = helper
			break
		}
	}
	if relay == nil ||
		relay["status"] != "invalid-start-argv" ||
		!strings.Contains(relay["error"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("expected unsafe helper argv to be rejected, got %#v", relay)
	}
}

func TestBuildStatusReportsConnectedFeedback(t *testing.T) {
	status := BuildStatus(StatusOptions{
		TicketCode: "ABCD-1234",
		Locale:     "zh-CN",
		GatewayURL: "https://gateway.example.test",
		Hosts: []model.Host{{
			ID:                  "host_1",
			TicketID:            "ticket_1",
			Status:              model.HostStatusActive,
			Name:                "win-dev",
			OS:                  "windows",
			Arch:                "amd64",
			IdentityFingerprint: "sha256:test-host-fingerprint",
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
		calls[0]["tool"] != "rdev.sessions.status" ||
		calls[0]["arguments"].(map[string]any)["session_id"] != "<session-id>" {
		t.Fatalf("expected connected next-step contract, got %#v", next)
	}
	if !strings.Contains(next["user_report"].(string), "保持连接在线") {
		t.Fatalf("expected connected report to keep the connector online, got %#v", next)
	}
	remoteEntry := status["remote_control_entry"].(map[string]any)
	if remoteEntry["schema_version"] != RemoteControlEntrySchemaVersion ||
		remoteEntry["support_device_id_source"] != "host_identity_fingerprint" ||
		remoteEntry["session_passcode"] != "ABCD-1234" ||
		remoteEntry["recommended_task_endpoint_id"] != "host_1" ||
		remoteEntry["explicit_disconnect_required"] != true {
		t.Fatalf("expected connected remote-control entry, got %#v", remoteEntry)
	}
	runbook := status["agent_connection_runbook"].(map[string]any)
	watch := runbook["watch"].(map[string]any)
	watchArgs := watch["mcp_arguments"].(map[string]any)
	watchCommand := watch["cli_command"].([]string)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		runbook["status"] != "connected" ||
		runbook["gateway_url"] != "https://gateway.example.test" ||
		watchArgs["gateway_url"] != "https://gateway.example.test" ||
		!slices.Contains(watchCommand, "--gateway-url") ||
		!slices.Contains(watchCommand, "https://gateway.example.test") ||
		!strings.Contains(strings.Join(runbook["on_connected"].([]string), "\n"), "capabilities") {
		t.Fatalf("expected connected status runbook, got %#v", runbook)
	}
}

func TestBuildStatusUsesBoundTargetEndpointAcrossNestedContracts(t *testing.T) {
	now := time.Now().UTC()
	ticket := model.Ticket{
		ID:        "tkt_bound",
		Code:      "BOUND-1234",
		SessionID: "ses_bound",
		Status:    model.TicketStatusActive,
		ExpiresAt: now.Add(10 * time.Minute),
	}
	session := controlplane.Session{
		ID:             ticket.SessionID,
		JoinCode:       ticket.Code,
		SourceTicketID: ticket.ID,
		Status:         controlplane.SessionStatusOnline,
		Endpoints: []controlplane.Endpoint{{
			ID:           "ep_bound_target",
			SessionID:    ticket.SessionID,
			Role:         controlplane.EndpointRoleTarget,
			Name:         "windows-target",
			Platform:     "windows/amd64",
			Capabilities: []string{"shell.user"},
			State:        controlplane.EndpointStateOnline,
			LastSeenAt:   now,
		}},
	}

	status := BuildStatus(StatusOptions{
		TicketCode: ticket.Code,
		Ticket:     &ticket,
		Session:    &session,
		Locale:     "en",
		GatewayURL: "https://gateway.example.test",
	})
	if status["connected"] != true || status["session_id"] != session.ID || status["recommended_target_endpoint_id"] != session.Endpoints[0].ID {
		t.Fatal("endpoint-driven status omitted the bound session or target endpoint")
	}
	next := status["connected_next_steps"].(map[string]any)
	calls, _ := next["mcp_next_calls"].([]map[string]any)
	capabilities, _ := next["capabilities"].([]string)
	if next["connected"] != true || next["session_id"] != session.ID || next["target_endpoint_id"] != session.Endpoints[0].ID || next["host_name"] != session.Endpoints[0].Name || !slices.Equal(capabilities, session.Endpoints[0].Capabilities) || len(calls) != 1 || calls[0]["arguments"].(map[string]any)["session_id"] != session.ID {
		t.Fatal("connected next steps did not use the real bound endpoint IDs")
	}
	remoteEntry := status["remote_control_entry"].(map[string]any)
	if remoteEntry["session_id"] != session.ID || remoteEntry["recommended_target_endpoint_id"] != session.Endpoints[0].ID {
		t.Fatal("remote control entry did not use the real bound endpoint IDs")
	}
}

func TestBuildStatusRejectsStaleOrTicketInvalidTargetEndpoint(t *testing.T) {
	now := time.Now().UTC()
	for _, tt := range []struct {
		name         string
		ticketStatus model.TicketStatus
		expiresAt    time.Time
		lastSeenAt   time.Time
	}{
		{name: "stale endpoint", ticketStatus: model.TicketStatusActive, expiresAt: now.Add(time.Minute), lastSeenAt: now.Add(-91 * time.Second)},
		{name: "expired ticket", ticketStatus: model.TicketStatusActive, expiresAt: now.Add(-time.Second), lastSeenAt: now},
		{name: "revoked ticket", ticketStatus: model.TicketStatusRevoked, expiresAt: now.Add(time.Minute), lastSeenAt: now},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ticket := model.Ticket{ID: "tkt_status", Code: "STATE-1234", SessionID: "ses_status", Status: tt.ticketStatus, ExpiresAt: tt.expiresAt}
			session := controlplane.Session{
				ID:             ticket.SessionID,
				JoinCode:       ticket.Code,
				SourceTicketID: ticket.ID,
				Status:         controlplane.SessionStatusOnline,
				Endpoints: []controlplane.Endpoint{{
					ID: "ep_status", SessionID: ticket.SessionID, Role: controlplane.EndpointRoleTarget,
					State: controlplane.EndpointStateOnline, LastSeenAt: tt.lastSeenAt,
				}},
			}
			status := BuildStatus(StatusOptions{TicketCode: ticket.Code, Ticket: &ticket, Session: &session})
			if status["connected"] == true || status["recommended_target_endpoint_id"] != "" {
				t.Fatal("stale or invalid ticket endpoint was reported connected")
			}
		})
	}
}

func TestBuildStatusReportsTargetDownloadingFromPreconnect(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	status := BuildStatus(StatusOptions{
		TicketCode: "WAIT-1234",
		Locale:     "zh-CN",
		Preconnects: []model.SupportSessionPreconnect{
			{
				ID:         "pre_old",
				TicketCode: "WAIT-1234",
				Phase:      "started",
				OS:         "windows",
				Arch:       "amd64",
				Source:     "bootstrap.ps1",
				CreatedAt:  now.Add(-2 * time.Minute),
				LastSeenAt: now.Add(-2 * time.Minute),
				SeenCount:  1,
			},
			{
				ID:         "pre_download",
				TicketCode: "WAIT-1234",
				Phase:      "downloading-core",
				OS:         "windows",
				Arch:       "amd64",
				Asset:      "rdev-host-windows-amd64.exe",
				Source:     "bootstrap.ps1",
				Message:    "downloading verified core runtime",
				CreatedAt:  now.Add(-1 * time.Minute),
				LastSeenAt: now,
				SeenCount:  3,
			},
		},
	})

	if status["status"] != "target-downloading" ||
		status["waiting"] != false ||
		status["connected"] != false ||
		!strings.Contains(status["feedback"].(string), "正在下载") ||
		!strings.Contains(status["feedback"].(string), "core runtime") ||
		strings.Contains(strings.ToLower(status["feedback"].(string)), "helper") ||
		!strings.Contains(status["next_action"].(string), "继续等待") {
		t.Fatalf("expected target downloading status, got %#v", status)
	}
	summary := status["target_preconnect_summary"].(map[string]any)
	latest := summary["latest"].(model.SupportSessionPreconnect)
	countByPhase := summary["count_by_phase"].(map[string]int)
	if summary["status"] != "target-downloading" ||
		summary["phase"] != "downloading-core" ||
		latest.ID != "pre_download" ||
		countByPhase["started"] != 1 ||
		countByPhase["downloading-core"] != 1 ||
		!strings.Contains(summary["agent_interpretation"].(string), "not disconnected") {
		t.Fatalf("expected target preconnect summary, got %#v", summary)
	}
}

func TestBuildStatusIncludesStandardConnectionRecovery(t *testing.T) {
	status := BuildStatus(StatusOptions{
		TicketCode: "WAIT-1234",
		Locale:     "en",
		GatewayURL: "https://gateway.example.test",
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
	watch := runbook["watch"].(map[string]any)
	watchArgs := watch["mcp_arguments"].(map[string]any)
	watchCommand := watch["cli_command"].([]string)
	if runbook["schema_version"] != AgentConnectionRunbookSchemaVersion ||
		runbook["phase"] != "recovery" ||
		runbook["gateway_url"] != "https://gateway.example.test" ||
		watchArgs["gateway_url"] != "https://gateway.example.test" ||
		!slices.Contains(watchCommand, "--gateway-url") ||
		!slices.Contains(watchCommand, "https://gateway.example.test") ||
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

func int64FromAny(t *testing.T, value any) int64 {
	t.Helper()
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		t.Fatalf("expected numeric value, got %T %#v", value, value)
		return 0
	}
}
