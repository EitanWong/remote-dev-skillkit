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
}
