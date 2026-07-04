package relayadapter

import (
	"os"
	"path/filepath"
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
	for _, path := range []string{"relay-adapter.json", "RELAY_ADAPTER.md", "runner.env.example"} {
		if _, err := os.Stat(filepath.Join(out, path)); err != nil {
			t.Fatalf("expected relay adapter file %s: %v", path, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(out, "relay-adapter.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), filepath.Dir(out)) ||
		strings.Contains(string(content), "192.168.") ||
		strings.Contains(string(content), "BEGIN PRIVATE KEY") {
		t.Fatalf("relay adapter package leaked private material: %s", string(content))
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
