package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunFreshAgentSupportSessionWritesPassingReport(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://192.0.2.55:8787")
	out := filepath.Join(t.TempDir(), "fresh-agent")
	report, err := RunFreshAgentSupportSession(FreshAgentSupportSessionOptions{
		OutDir:      out,
		GatewayURL:  "http://127.0.0.1:8787",
		RdevCommand: "rdev",
		Locale:      "en",
		Now:         time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != FreshAgentSupportSessionReportSchemaVersion {
		t.Fatalf("unexpected schema %q", report.SchemaVersion)
	}
	if !allChecksPassed(report.Checks) {
		t.Fatalf("expected all checks to pass: %#v", report.Checks)
	}
	if _, err := os.Stat(filepath.Join(out, "report.json")); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
	if report.HandoffNoGateway["selected_path"] != "start-foreground-gateway" {
		t.Fatalf("expected foreground start path, got %#v", report.HandoffNoGateway)
	}
	if report.HandoffReachableGateway["mcp_next_tool"] != "rdev.support_session.create" {
		t.Fatalf("expected create next tool, got %#v", report.HandoffReachableGateway)
	}
	handoff := mapFromAny(report.CreatedSession["user_handoff"])
	copyPaste := stringFromAny(handoff["copy_paste"])
	if copyPaste == "" ||
		strings.Contains(copyPaste, "<ticket-code>") ||
		strings.Contains(copyPaste, "ExecutionPolicy Bypass") {
		t.Fatalf("unexpected copy-paste command: %s", copyPaste)
	}
	connectedNext := mapFromAny(report.ConnectedStatus["connected_next_steps"])
	if report.ConnectedStatus["connected"] != true ||
		!strings.Contains(stringFromAny(connectedNext["user_report"]), "Connection established") {
		t.Fatalf("expected connected user report, got %#v", report.ConnectedStatus)
	}
	if len(report.LiveRemoteE2EGates) == 0 {
		t.Fatalf("expected live remote E2E gates in report")
	}
	gates := map[string]map[string]any{}
	for _, gate := range report.LiveRemoteE2EGates {
		gates[stringFromAny(gate["name"])] = gate
	}
	windowsGate := gates["windows_support_session_smoke_remote_control"]
	if windowsGate["status"] != "requires_real_environment" ||
		windowsGate["target_os"] != "windows" ||
		!strings.Contains(strings.Join(stringSliceFromAny(windowsGate["proof_command"]), " "), "support-session smoke-test") ||
		windowsGate["remote_control"] != true {
		t.Fatalf("expected Windows live remote smoke gate, got %#v", windowsGate)
	}
	fileTransferGate := gates["windows_file_transfer_byte_compare"]
	fileTransferCommands := mapFromAny(fileTransferGate["proof_commands"])
	if !strings.Contains(strings.Join(stringSliceFromAny(fileTransferCommands["upload"]), " "), "files upload") ||
		!strings.Contains(strings.Join(stringSliceFromAny(fileTransferCommands["download"]), " "), "files download") ||
		!strings.Contains(strings.Join(stringSliceFromAny(fileTransferGate["required_evidence"]), " "), "byte_compare=match") {
		t.Fatalf("expected executable file transfer byte-compare gate, got %#v", fileTransferGate)
	}
	interruptGate := gates["windows_session_interrupt_flow"]
	interruptCommands := mapFromAny(interruptGate["proof_commands"])
	if interruptGate["mcp_tool"] != "rdev.sessions.interrupt" ||
		!strings.Contains(strings.Join(stringSliceFromAny(interruptCommands["mcp_server"]), " "), "mcp serve") ||
		!strings.Contains(strings.Join(stringSliceFromAny(interruptGate["required_evidence"]), " "), "rdev.sessions.events replays") {
		t.Fatalf("expected executable session-interrupt gate, got %#v", interruptGate)
	}
}

func TestRunFreshAgentSupportSessionGatesBootstrapFirstConnectEvidence(t *testing.T) {
	out := filepath.Join(t.TempDir(), "fresh-agent")
	report, err := RunFreshAgentSupportSession(FreshAgentSupportSessionOptions{
		OutDir:      out,
		GatewayURL:  "http://127.0.0.1:8787",
		RdevCommand: "rdev",
		Locale:      "en",
		Now:         time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"bootstrap_first_connect_scripts_under_budget",
		"bootstrap_first_connect_preconnect_does_not_grant_host_access",
		"bootstrap_first_connect_requires_verified_full_helper_upgrade",
	} {
		check, ok := checkByName(report.Checks, name)
		if !ok {
			t.Fatalf("expected fresh-agent acceptance check %q, got %#v", name, report.Checks)
		}
		if !check.Passed {
			t.Fatalf("expected check %q to pass, got %#v", name, check)
		}
	}

	bootstrap := mapFromAny(report.BootstrapSelfRepair["bootstrap_first_connect"])
	nativeBootstrap := mapFromAny(bootstrap["native_connector"])
	if bootstrap["schema_version"] != "rdev.acceptance.bootstrap-first-connect.v1" ||
		bootstrap["windows_within_budget"] != true ||
		bootstrap["shell_within_budget"] != true ||
		bootstrap["default_first_connect_surface"] != "script-preconnect" ||
		bootstrap["publishes_native_first_connect_asset"] != false ||
		bootstrap["preconnect_grants_host_access"] != false ||
		bootstrap["can_run_session_tasks_before_full_runner"] != false ||
		!strings.Contains(stringFromAny(bootstrap["staged_upgrade_rule"]), "SHA-256") ||
		nativeBootstrap["availability"] != "optional-if-rdev-bootstrap-is-already-installed-or-published" ||
		nativeBootstrap["published_by_support_session_assets"] != false ||
		nativeBootstrap["default_first_connect_surface"] != "script-preconnect" {
		t.Fatalf("expected bootstrap first-connect evidence, got %#v", bootstrap)
	}
}

func checkByName(checks []Check, name string) (Check, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return Check{}, false
}
