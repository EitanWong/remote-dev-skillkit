package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
)

func TestPackageAndVerifyRelayAdapterEvidence(t *testing.T) {
	root := t.TempDir()
	relayDir := filepath.Join(root, "relay")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "chisel",
		GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForTest(t, root)
	pkg, err := PackageRelayAdapterEvidence(RelayAdapterPackageOptions{
		RelayAdapterPackagePath: relayDir,
		OutDir:                  filepath.Join(root, "package"),
		RunnerResultPath:        evidence.runnerResult,
		HelperTranscriptPath:    evidence.helperTranscript,
		GatewayStatusPath:       evidence.gatewayStatus,
		HostStatusPath:          evidence.hostStatus,
		ConnectionStatusPath:    evidence.connectionStatus,
		AuditPath:               evidence.audit,
		Now:                     time.Date(2026, 7, 4, 12, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected relay package ok: %#v", pkg.Checks)
	}
	if pkg.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github token redaction, got %#v", pkg.RedactionRuleCounts)
	}

	verification, err := VerifyRelayAdapterAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}
}

func TestVerifyRelayAdapterEvidenceRejectsMissingConnection(t *testing.T) {
	root := t.TempDir()
	relayDir := filepath.Join(root, "relay")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "frpc",
		GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForTest(t, root)
	if err := os.WriteFile(evidence.connectionStatus, []byte(`{"connected":false}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := PackageRelayAdapterEvidence(RelayAdapterPackageOptions{
		RelayAdapterPackagePath: relayDir,
		OutDir:                  filepath.Join(root, "package"),
		RunnerResultPath:        evidence.runnerResult,
		HelperTranscriptPath:    evidence.helperTranscript,
		GatewayStatusPath:       evidence.gatewayStatus,
		HostStatusPath:          evidence.hostStatus,
		ConnectionStatusPath:    evidence.connectionStatus,
		AuditPath:               evidence.audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatal("expected package to fail when connection status is not connected")
	}
	verification, err := VerifyRelayAdapterAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected verification to fail")
	}
	failures := failedRelayAcceptanceChecks(verification.Checks)
	if !strings.Contains(failures, "package_checks_passed") ||
		!strings.Contains(failures, "connection_status_connected") {
		t.Fatalf("expected connection failure checks, got %s", failures)
	}
}

type relayAdapterEvidenceForTest struct {
	runnerResult     string
	helperTranscript string
	gatewayStatus    string
	hostStatus       string
	connectionStatus string
	audit            string
}

func writeRelayAdapterEvidenceForTest(t *testing.T, root string) relayAdapterEvidenceForTest {
	t.Helper()
	dir := filepath.Join(root, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) string {
		t.Helper()
		path := filepath.Join(dir, name)
		var content []byte
		switch typed := value.(type) {
		case string:
			content = []byte(typed)
		default:
			var err error
			content, err = json.MarshalIndent(value, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			content = append(content, '\n')
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	return relayAdapterEvidenceForTest{
		runnerResult: write("runner-result.json", map[string]any{
			"schema_version":       "rdev.connection-entry.runner-result.v1",
			"selected_path":        "existing-frp-or-chisel-relay",
			"selected_gateway_url": "https://relay.example.invalid/rdev",
			"helper_started":       true,
		}),
		helperTranscript: write("helper-transcript.txt", "started chisel helper\nexported token ghp_abcdefghijklmnopqrstuvwx\n"),
		gatewayStatus:    write("gateway-status.json", map[string]any{"ok": true, "status": "healthy"}),
		hostStatus:       write("host-status.json", map[string]any{"ok": true, "host_status": "active"}),
		connectionStatus: write("connection-status.json", map[string]any{"ok": true, "connected": true}),
		audit:            write("audit.txt", "helper_start\nhost_registered\ncleanup\n"),
	}
}

func failedRelayAcceptanceChecks(checks []Check) string {
	var failed []string
	for _, check := range checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	return strings.Join(failed, ",")
}
