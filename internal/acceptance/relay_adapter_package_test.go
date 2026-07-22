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
	cases := []struct {
		name        string
		adapterKind string
		selected    string
	}{
		{name: "chisel relay", adapterKind: "chisel", selected: "existing-frp-or-chisel-relay"},
		{name: "ssh tunnel", adapterKind: "ssh-tunnel", selected: "existing-ssh-tunnel"},
		{name: "headscale mesh", adapterKind: "headscale-tailscale", selected: "existing-headscale-tailscale-mesh"},
		{name: "wireguard vpn", adapterKind: "wireguard", selected: "existing-wireguard-vpn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			relayDir := filepath.Join(root, "relay")
			if _, err := relayadapter.Build(relayadapter.Options{
				OutDir:      relayDir,
				AdapterKind: tc.adapterKind,
				GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
			}); err != nil {
				t.Fatal(err)
			}
			evidence := writeRelayAdapterEvidenceForTest(t, root, tc.selected)
			pkg, err := PackageRelayAdapterEvidence(RelayAdapterPackageOptions{
				RelayAdapterPackagePath: relayDir,
				OutDir:                  filepath.Join(root, "package"),
				EvidenceDirPath:         evidence.dir,
				Now:                     time.Date(2026, 7, 4, 12, 5, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !pkg.OK() {
				t.Fatalf("expected relay package ok: %#v", pkg.Checks)
			}
			if pkg.SelectedPath != tc.selected {
				t.Fatalf("expected selected path %q, got %q", tc.selected, pkg.SelectedPath)
			}
			if len(pkg.AcceptedPaths) != 4 {
				t.Fatalf("expected accepted paths, got %#v", pkg.AcceptedPaths)
			}
			if pkg.RedactionRuleCounts["github_token"] != 1 {
				t.Fatalf("expected github token redaction, got %#v", pkg.RedactionRuleCounts)
			}
			if !relayPackageHasPath(pkg.Files, "evidence/audit.jsonl") {
				t.Fatalf("expected evidence-dir files in package: %#v", pkg.Files)
			}

			verification, err := VerifyRelayAdapterAcceptancePackage(pkg.OutDir)
			if err != nil {
				t.Fatal(err)
			}
			if !verification.OK() {
				t.Fatalf("expected verification ok: %#v", verification.Checks)
			}
			if verification.SelectedPath != tc.selected {
				t.Fatalf("expected verified selected path %q, got %q", tc.selected, verification.SelectedPath)
			}
		})
	}
}

func TestPackageRelayAdapterEvidenceRedactsRunnerPrivateSurface(t *testing.T) {
	root := t.TempDir()
	relayDir := filepath.Join(root, "relay")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "wireguard",
		GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForTest(t, root, "existing-wireguard-vpn")
	privatePath := filepath.Join(string(filepath.Separator), "Users", "example", "private", "runner-manifest.json")
	privateCache := filepath.Join(string(filepath.Separator), "Users", "example", "private", "cache")
	runnerResult, err := json.MarshalIndent(map[string]any{
		"schema_version":       "rdev.connection-entry.runner-result.v1",
		"manifest_path":        privatePath,
		"selected_path":        "existing-wireguard-vpn",
		"selected_gateway_url": "https://gateway.example.invalid/v1",
		"bootstrap_args":       []string{"rdev-bootstrap", "--cache-dir", privateCache, "--gateway", "https://gateway.example.invalid/v1"},
		"tool_results":         []map[string]any{{"name": "rdev-bootstrap", "found": true, "path": privatePath}},
		"helper_started":       true,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidence.runnerResult, append(runnerResult, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	pkg, err := PackageRelayAdapterEvidence(RelayAdapterPackageOptions{
		RelayAdapterPackagePath: relayDir,
		OutDir:                  filepath.Join(root, "package"),
		EvidenceDirPath:         evidence.dir,
		Now:                     time.Date(2026, 7, 4, 12, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package ok: %#v", pkg.Checks)
	}
	verification, err := VerifyRelayAdapterAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected sanitized runner evidence to verify: %#v", verification.Checks)
	}

	copied, err := os.ReadFile(filepath.Join(pkg.OutDir, "evidence", "runner-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(copied), privatePath) || strings.Contains(string(copied), "gateway.example.invalid") {
		t.Fatalf("runner evidence retained private surface: %s", copied)
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
	evidence := writeRelayAdapterEvidenceForTest(t, root, "existing-frp-or-chisel-relay")
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

func TestVerifyRelayAdapterEvidenceRejectsUnknownConnectivityPath(t *testing.T) {
	root := t.TempDir()
	relayDir := filepath.Join(root, "relay")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "chisel",
		GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForTest(t, root, "agent-authored-custom-tunnel")
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
		t.Fatal("expected package to fail when runner selects an unknown connectivity path")
	}
	verification, err := VerifyRelayAdapterAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected verification to fail")
	}
	if failures := failedRelayAcceptanceChecks(verification.Checks); !strings.Contains(failures, "runner_selected_standard_connectivity_path") {
		t.Fatalf("expected standard path failure, got %s", failures)
	}
}

func TestRelayAdapterEvidenceRejectsScaffoldPlaceholderEvidence(t *testing.T) {
	root := t.TempDir()
	relayDir := filepath.Join(root, "relay")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "wireguard",
		GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForTest(t, root, "existing-wireguard-vpn")
	if err := os.WriteFile(evidence.runnerResult, []byte("{\n  \"placeholder\": true,\n  \"replace_before_packaging\": true,\n  \"evidence_name\": \"runner-result\"\n}\n"), 0o600); err != nil {
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
		t.Fatal("expected package to fail with scaffold placeholder evidence")
	}
	failures := failedRelayAcceptanceChecks(pkg.Checks)
	if !strings.Contains(failures, "runner-result_copied") ||
		!strings.Contains(failures, "runner_result_present") {
		t.Fatalf("expected placeholder copy and runner result failures, got %s", failures)
	}
	verification, err := VerifyRelayAdapterAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected verification to fail with scaffold placeholder evidence")
	}
}

type relayAdapterEvidenceForTest struct {
	dir              string
	runnerResult     string
	helperTranscript string
	gatewayStatus    string
	hostStatus       string
	connectionStatus string
	audit            string
}

func writeRelayAdapterEvidenceForTest(t *testing.T, root, selectedPath string) relayAdapterEvidenceForTest {
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
		dir: dir,
		runnerResult: write("runner-result.json", map[string]any{
			"schema_version":       "rdev.connection-entry.runner-result.v1",
			"selected_path":        selectedPath,
			"selected_gateway_url": "https://relay.example.invalid/rdev",
			"helper_started":       true,
		}),
		helperTranscript: write("helper-transcript.txt", "started chisel helper\nexported token ghp_abcdefghijklmnopqrstuvwx\n"),
		gatewayStatus:    write("gateway-status.json", map[string]any{"ok": true, "status": "healthy"}),
		hostStatus:       write("host-status.json", map[string]any{"ok": true, "host_status": "active"}),
		connectionStatus: write("connection-status.json", map[string]any{"ok": true, "connected": true}),
		audit:            write("audit.jsonl", `{"event":"helper_start"}`+"\n"+`{"event":"host_registered"}`+"\n"+`{"event":"cleanup"}`+"\n"),
	}
}

func relayPackageHasPath(files []AcceptancePackageFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
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
