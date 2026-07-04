package relayadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildAndVerifyRelayAdapterPackage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "relay")
	pkg, err := Build(Options{
		OutDir:      out,
		Name:        "self-hosted-chisel-relay",
		AdapterKind: "chisel",
		GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package ok: %#v", pkg.Checks)
	}
	for _, path := range []string{"relay-adapter.json", "RELAY_ADAPTER.md", "runner.env.example", "acceptance-evidence-plan.json"} {
		if _, err := os.Stat(filepath.Join(out, path)); err != nil {
			t.Fatalf("expected relay adapter file %s: %v", path, err)
		}
	}
	assertFileContains(t, filepath.Join(out, "RELAY_ADAPTER.md"), "scaffold-evidence --relay-adapter-package")
	content, err := os.ReadFile(filepath.Join(out, "relay-adapter.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), filepath.Dir(out)) ||
		strings.Contains(string(content), "192.168.") ||
		strings.Contains(string(content), "BEGIN PRIVATE KEY") {
		t.Fatalf("relay adapter package leaked private material: %s", string(content))
	}
	planContent, err := os.ReadFile(filepath.Join(out, "acceptance-evidence-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan AcceptanceEvidencePlan
	if err := json.Unmarshal(planContent, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != AcceptanceEvidencePlanSchemaVersion ||
		plan.AdapterKind != "chisel" ||
		plan.ConnectionPathID != "existing-frp-or-chisel-relay" ||
		plan.PackagePath != "relay-adapter.json" ||
		plan.ExternalMutation ||
		!slices.Contains(plan.DryRunCommand, "--evidence-dir") ||
		!slices.Contains(plan.RunCommand, "--evidence-dir") ||
		!slices.Contains(plan.PackageCommand, "package-relay-adapter") ||
		!slices.Contains(plan.PackageCommand, "--evidence-dir") ||
		!slices.Contains(plan.VerifyCommand, "verify-relay-adapter-package") ||
		!slices.ContainsFunc(plan.AgentRules, func(rule string) bool {
			return strings.Contains(rule, "scaffold-evidence --relay-adapter-package")
		}) {
		t.Fatalf("unexpected acceptance evidence plan: %#v", plan)
	}
	planPaths := map[string]bool{}
	for _, file := range plan.EvidenceFiles {
		planPaths[file.Path] = true
	}
	for _, expected := range []string{"runner-result.json", "helper-transcript.txt", "gateway-status.json", "host-status.json", "connection-status.json", "audit.jsonl"} {
		if !planPaths[expected] {
			t.Fatalf("missing evidence plan path %q in %#v", expected, plan.EvidenceFiles)
		}
	}

	verification, err := Verify(out)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}
	if verification.SchemaVersion != VerificationSchemaVersion ||
		verification.AdapterKind != "chisel" {
		t.Fatalf("unexpected verification: %#v", verification)
	}
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

func TestBuildAndVerifyFRPCRelayAdapterPackage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "relay")
	pkg, err := Build(Options{
		OutDir:      out,
		AdapterKind: "frp",
		GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() || pkg.AdapterKind != "frpc" || pkg.Helper.Tool != "frpc" {
		t.Fatalf("unexpected frpc package: %#v", pkg)
	}
	verification, err := Verify(filepath.Join(out, "relay-adapter.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() || verification.AdapterKind != "frpc" {
		t.Fatalf("expected frpc verification ok: %#v", verification)
	}
}

func TestBuildAndVerifyAdditionalConnectivityAdapterPackages(t *testing.T) {
	cases := []struct {
		adapter          string
		kind             string
		tool             string
		gatewayEnv       string
		startArgvEnv     string
		installActionEnv string
		pathID           string
		installTool      string
		installURLVar    string
	}{
		{
			adapter:          "ssh",
			kind:             "ssh-tunnel",
			tool:             "ssh",
			gatewayEnv:       "RDEV_SSH_GATEWAY_URL",
			startArgvEnv:     "RDEV_SSH_TUNNEL_START_ARGV_JSON",
			installActionEnv: "RDEV_SSH_INSTALL_ACTION_JSON",
			pathID:           "existing-ssh-tunnel",
			installTool:      "ssh",
			installURLVar:    "",
		},
		{
			adapter:          "headscale",
			kind:             "headscale-tailscale",
			tool:             "tailscale",
			gatewayEnv:       "RDEV_MESH_GATEWAY_URL",
			startArgvEnv:     "RDEV_MESH_START_ARGV_JSON",
			installActionEnv: "RDEV_MESH_INSTALL_ACTION_JSON",
			pathID:           "existing-headscale-tailscale-mesh",
			installTool:      "tailscale",
			installURLVar:    "RDEV_MESH_DOWNLOAD_URL",
		},
		{
			adapter:          "wireguard",
			kind:             "wireguard",
			tool:             "wg",
			gatewayEnv:       "RDEV_VPN_GATEWAY_URL",
			startArgvEnv:     "RDEV_VPN_START_ARGV_JSON",
			installActionEnv: "RDEV_VPN_INSTALL_ACTION_JSON",
			pathID:           "existing-wireguard-vpn",
			installTool:      "wg",
			installURLVar:    "RDEV_VPN_DOWNLOAD_URL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.adapter, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "adapter")
			pkg, err := Build(Options{
				OutDir:      out,
				AdapterKind: tc.adapter,
				GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !pkg.OK() ||
				pkg.AdapterKind != tc.kind ||
				pkg.Helper.Tool != tc.tool ||
				pkg.RunnerEnv.GatewayURLVar != tc.gatewayEnv ||
				pkg.RunnerEnv.StartArgvVar != tc.startArgvEnv ||
				pkg.RunnerEnv.InstallActionVar != tc.installActionEnv ||
				pkg.RunnerEnv.ConnectionPathID != tc.pathID ||
				pkg.ConnectionPathID != tc.pathID {
				t.Fatalf("unexpected package: %#v", pkg)
			}
			if pkg.InstallAction.Tool != tc.installTool {
				t.Fatalf("unexpected install action tool: %#v", pkg.InstallAction)
			}
			if pkg.EvidencePlanPath != "acceptance-evidence-plan.json" ||
				!slices.ContainsFunc(pkg.Files, func(file PackageFile) bool {
					return file.Path == "acceptance-evidence-plan.json" && file.Kind == "acceptance-evidence-plan"
				}) {
				t.Fatalf("missing acceptance evidence plan: %#v", pkg.Files)
			}
			installArgv := strings.Join(pkg.InstallAction.Argv, " ")
			if tc.installURLVar == "" {
				if installArgv != "manual-review-required" {
					t.Fatalf("expected manual review for %s, got %#v", tc.adapter, pkg.InstallAction)
				}
			} else if !strings.Contains(installArgv, "rdev deps install") || !strings.Contains(installArgv, tc.installURLVar) {
				t.Fatalf("expected standard deps install action for %s, got %#v", tc.adapter, pkg.InstallAction)
			}
			verification, err := Verify(out)
			if err != nil {
				t.Fatal(err)
			}
			if !verification.OK() || verification.AdapterKind != tc.kind {
				t.Fatalf("expected verification ok: %#v", verification)
			}
		})
	}
}

func TestVerifyRelayAdapterPackageDetectsUnsafeHelperSurface(t *testing.T) {
	out := filepath.Join(t.TempDir(), "relay")
	_, err := Build(Options{
		OutDir:      out,
		AdapterKind: "chisel",
		GeneratedAt: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "runner.env.example"), []byte("RDEV_RELAY_GATEWAY_URL=https://relay.private.invalid/rdev\nRDEV_SECRET=private-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	verification, err := Verify(out)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected tampered relay adapter package to fail")
	}
	failures := failedNames(verification)
	if !strings.Contains(failures, "runner.env.example:file_sha256_matches") ||
		!strings.Contains(failures, "runner.env.example:file_has_no_private_surface") {
		t.Fatalf("expected checksum and private-surface failures, got %s", failures)
	}
}

func failedNames(verification Verification) string {
	var failed []string
	for _, check := range verification.Checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	for _, file := range verification.Files {
		for _, check := range file.Checks {
			if !check.Passed {
				failed = append(failed, file.Path+":"+check.Name)
			}
		}
	}
	return strings.Join(failed, ",")
}
