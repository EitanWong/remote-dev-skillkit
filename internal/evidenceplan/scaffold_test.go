package evidenceplan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostedprovider"
	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
)

func TestBuildHostedProviderRuntimeScaffold(t *testing.T) {
	root := t.TempDir()
	providerDir := filepath.Join(root, "hosted-provider")
	if _, err := hostedprovider.Build(hostedprovider.Options{
		OutDir:          providerDir,
		StorageProvider: "postgres",
		AuthProvider:    "oidc-jwks",
		GeneratedAt:     fixedTime(),
	}); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "scaffold")
	scaffold, err := Build(Options{
		PlanPath:    filepath.Join(providerDir, "runtime-evidence-plan.json"),
		OutDir:      out,
		GeneratedAt: fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !scaffold.OK || scaffold.ReadyForPackaging || scaffold.PlanKind != "hosted-provider-runtime" {
		t.Fatalf("unexpected scaffold status: %#v", scaffold)
	}
	if scaffold.CreatePlaceholders {
		t.Fatalf("default scaffold must not create placeholder evidence")
	}
	if len(scaffold.Commands.Preflight) == 0 ||
		!slicesContain(scaffold.Commands.Package, "package-hosted-provider-runtime") ||
		!slicesContain(scaffold.Commands.Verify, "verify-hosted-provider-runtime-package") {
		t.Fatalf("unexpected hosted commands: %#v", scaffold.Commands)
	}
	if _, err := os.Stat(filepath.Join(out, "gateway-startup.txt")); !os.IsNotExist(err) {
		t.Fatalf("default scaffold should not create evidence placeholders, err=%v", err)
	}
	assertFileContains(t, filepath.Join(out, "AGENT_CHECKLIST.md"), "Package command")
	assertFileContains(t, filepath.Join(out, "scaffold-report.json"), `"schema_version": "rdev.acceptance-evidence-scaffold.v1"`)
}

func TestBuildRelayAdapterScaffoldWithPlaceholders(t *testing.T) {
	root := t.TempDir()
	relayDir := filepath.Join(root, "relay-adapter")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "wireguard",
		GeneratedAt: fixedTime(),
	}); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "scaffold")
	scaffold, err := Build(Options{
		PlanPath:           filepath.Join(relayDir, "acceptance-evidence-plan.json"),
		OutDir:             out,
		CreatePlaceholders: true,
		GeneratedAt:        fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !scaffold.OK || scaffold.ReadyForPackaging || scaffold.PlanKind != "relay-adapter" {
		t.Fatalf("unexpected scaffold status: %#v", scaffold)
	}
	if len(scaffold.Commands.DryRun) == 0 ||
		len(scaffold.Commands.Run) == 0 ||
		!slicesContain(scaffold.Commands.Package, "package-relay-adapter") ||
		!slicesContain(scaffold.Commands.Verify, "verify-relay-adapter-package") {
		t.Fatalf("unexpected relay commands: %#v", scaffold.Commands)
	}
	assertFileContains(t, filepath.Join(out, "runner-result.json"), `"placeholder": true`)
	assertFileContains(t, filepath.Join(out, "helper-transcript.txt"), "PLACEHOLDER ONLY")

	content, err := os.ReadFile(filepath.Join(out, "scaffold-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Scaffold
	if err := json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}
	if !report.CreatePlaceholders || len(report.RecommendedActions) == 0 {
		t.Fatalf("expected placeholder warning in report: %#v", report)
	}
}

func TestStatusForScaffoldReportsMissingPlaceholderAndReadyEvidence(t *testing.T) {
	root := t.TempDir()
	relayDir := filepath.Join(root, "relay-adapter")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "chisel",
		GeneratedAt: fixedTime(),
	}); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "scaffold")
	if _, err := Build(Options{
		PlanPath:           filepath.Join(relayDir, "acceptance-evidence-plan.json"),
		OutDir:             out,
		CreatePlaceholders: true,
		GeneratedAt:        fixedTime(),
	}); err != nil {
		t.Fatal(err)
	}

	status, err := StatusForScaffold(StatusOptions{
		ScaffoldPath: out,
		GeneratedAt:  fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.ReadyForPackaging || status.PlaceholderCount == 0 || status.RequiredReady != 0 {
		t.Fatalf("placeholder scaffold must not be ready: %#v", status)
	}

	realFiles := map[string]string{
		"runner-result.json":     `{"schema_version":"rdev.connection-entry.runner-result.v1","selected_path":"existing-frp-or-chisel-relay"}`,
		"helper-transcript.txt":  "helper started and exited cleanly\n",
		"gateway-status.json":    `{"ok":true}`,
		"host-status.json":       `{"ok":true}`,
		"connection-status.json": `{"connected":true}`,
		"audit.jsonl":            `{"event":"relay_acceptance"}`,
	}
	for name, content := range realFiles {
		if err := os.WriteFile(filepath.Join(out, name), []byte(content+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	status, err = StatusForScaffold(StatusOptions{
		ScaffoldPath: filepath.Join(out, "scaffold-report.json"),
		GeneratedAt:  fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.ReadyForPackaging || !status.OK || status.RequiredReady != status.RequiredTotal ||
		status.PlaceholderCount != 0 || status.MissingCount != 0 || status.EmptyCount != 0 {
		t.Fatalf("real evidence should be ready: %#v", status)
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC)
}

func assertFileContains(t *testing.T, path, needle string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), needle) {
		t.Fatalf("expected %q in %s:\n%s", needle, path, string(content))
	}
}

func slicesContain(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
