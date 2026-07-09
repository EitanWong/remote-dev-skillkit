package connectionentry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestFromInviteReportsMissingWindowsReleaseInputs(t *testing.T) {
	invite := testInvite(t, model.HostModeAttendedTemporary)
	content := mustJSON(t, invite)

	plan, err := FromInvite(Options{
		InviteJSON: content,
		TargetOS:   "windows",
		Ownership:  "third-party",
		Now:        time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != MaterializationPlanSchemaVersion {
		t.Fatalf("unexpected schema: %#v", plan)
	}
	if plan.ConnectionEntryName != "Connection Entry" ||
		plan.EntryPackagePlanSchema != EntryPackagePlanSchemaVersion ||
		!slices.Contains(plan.HandoffContract, "Agents must use rdev.connection_entry.plan or rdev connection-entry plan before giving target-side instructions.") {
		t.Fatalf("expected universal connection entry contract, got %#v", plan)
	}
	if plan.SessionMode != string(model.HostModeAttendedTemporary) || plan.EntryURL == "" {
		t.Fatalf("expected attended temporary entry plan: %#v", plan)
	}
	if !strings.Contains(plan.EntryCommand, "powershell") || !strings.Contains(plan.EntryCommand, "bootstrap.ps1") {
		t.Fatalf("expected Windows entry command, got %q", plan.EntryCommand)
	}
	if len(plan.MissingInputs) == 0 || !slices.Contains(plan.MissingInputs, "release_bundle_url") {
		t.Fatalf("expected release input gaps, got %#v", plan.MissingInputs)
	}
	if plan.ModeDecision == "" || !strings.Contains(plan.ModeDecision, "attended-temporary") {
		t.Fatalf("expected attended temporary mode decision, got %#v", plan)
	}
	if !slices.Contains(plan.HumanSurface, "connection_entry.entry_url") {
		t.Fatalf("expected universal human surface, got %#v", plan.HumanSurface)
	}
	if !slices.Contains(plan.AgentMetadata, "manifest root public key") {
		t.Fatalf("expected agent-only metadata, got %#v", plan.AgentMetadata)
	}
	if plan.EntryPackagePlan != nil {
		t.Fatalf("missing release inputs should not generate entry package plan: %#v", plan.EntryPackagePlan)
	}
	if plan.RunnerPlan == nil ||
		plan.RunnerPlan.PackageMode != "self-contained-connection-entry-runner" ||
		plan.RunnerPlan.ManifestPath != "" ||
		!slices.Contains(plan.RunnerPlan.SelectionOrder, "existing-frp-or-chisel-relay") {
		t.Fatalf("no out dir should still return an unwritten runner plan: %#v", plan.RunnerPlan)
	}
}

func TestFromInviteGeneratesWindowsTemporaryMaterialization(t *testing.T) {
	bootstrap := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(bootstrap, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "entry")
	plan, err := FromInvite(Options{
		InviteJSON:                    mustJSON(t, testInvite(t, model.HostModeAttendedTemporary)),
		OutDir:                        out,
		TargetOS:                      "windows",
		Ownership:                     "third-party",
		WindowsBootstrapScriptPath:    bootstrap,
		WindowsHostDownloadURL:        "https://agent.example.com/rdev-host.exe",
		WindowsHostExpectedSHA256:     strings.Repeat("a", 64),
		ReleaseBundleURL:              "https://agent.example.com/release-bundle.json",
		ReleaseRootPublicKey:          "release-root:" + strings.Repeat("b", 43),
		WindowsVerifierDownloadURL:    "https://agent.example.com/rdev-verify.exe",
		WindowsVerifierExpectedSHA256: strings.Repeat("c", 64),
		Now:                           time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.MissingInputs) != 0 {
		t.Fatalf("expected no missing inputs, got %#v", plan.MissingInputs)
	}
	if plan.HumanMessagePath == "" || !fileExists(plan.HumanMessagePath) {
		t.Fatalf("expected human message file: %#v", plan)
	}
	if plan.EntryPackagePlan == nil || !plan.EntryPackagePlan.OK || !fileExists(plan.EntryPackagePlan.LauncherPath) {
		t.Fatalf("expected generated entry package plan: %#v", plan.EntryPackagePlan)
	}
	if plan.RunnerPlan == nil ||
		plan.RunnerManifestSchema != "rdev.connection-entry.runner.v1" ||
		plan.RunnerPlan.SchemaVersion != "rdev.connection-entry.runner.v1" ||
		!fileExists(plan.RunnerPlan.ManifestPath) ||
		!fileExists(plan.RunnerPlan.LauncherPath) ||
		!slices.Contains(plan.RunnerPlan.SelectionOrder, "existing-frp-or-chisel-relay") {
		t.Fatalf("expected self-contained runner package plan: %#v", plan.RunnerPlan)
	}
	if !slices.Contains(plan.EntryPackagePlan.AgentOnlyParameters, "manifest_root_public_key") {
		t.Fatalf("expected runner package to keep raw metadata agent-only: %#v", plan.EntryPackagePlan)
	}
	if plan.EntryPackagePlan.SchemaVersion != EntryPackagePlanSchemaVersion ||
		plan.EntryPackagePlan.TargetOS != "windows" ||
		plan.EntryPackagePlan.PlatformPlanKind != "windows-temporary-acceptance-plan" {
		t.Fatalf("expected generic entry package wrapper around Windows plan: %#v", plan.EntryPackagePlan)
	}
	if !slices.Contains(plan.EntryPackagePlan.AgentOnlyParameters, "manifest_root_public_key") ||
		!slices.Contains(plan.EntryPackagePlan.AgentOnlyParameters, "ticket_code") {
		t.Fatalf("expected raw connection parameters to stay agent-only: %#v", plan.EntryPackagePlan.AgentOnlyParameters)
	}
	launcher, err := os.ReadFile(plan.EntryPackagePlan.LauncherPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launcher), "-ManifestRootPublicKey") || strings.Contains(string(launcher), "ticket, root, gateway") {
		t.Fatalf("launcher should carry manifest root and avoid human flag assembly text:\n%s", string(launcher))
	}
	if !fileExists(filepath.Join(out, "connection-entry-plan.json")) {
		t.Fatalf("expected materialization plan JSON in out dir")
	}
}

func TestFromInviteReportsMissingManagedInputs(t *testing.T) {
	plan, err := FromInvite(Options{
		InviteJSON:  mustJSON(t, testInvite(t, model.HostModeManaged)),
		OutDir:      filepath.Join(t.TempDir(), "managed-entry"),
		TargetOS:    "linux",
		Ownership:   "owned",
		SessionMode: string(model.HostModeManaged),
		Now:         time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.EntryPackagePlan == nil ||
		plan.EntryPackagePlan.PlatformPlanKind != "connection-entry-runner" ||
		plan.RunnerPlan == nil {
		t.Fatalf("missing managed service inputs should still generate the foreground runner package plan: %#v", plan)
	}
	for _, expected := range []string{"managed_binary_path", "release_bundle_path", "release_root_public_key"} {
		if !slices.Contains(plan.MissingInputs, expected) {
			t.Fatalf("expected missing input %q, got %#v", expected, plan.MissingInputs)
		}
	}
}

func TestFromInviteGeneratesManagedServiceMaterialization(t *testing.T) {
	cases := []struct {
		name             string
		targetOS         string
		binary           string
		releaseBundle    string
		required         string
		kind             string
		generatedFile    string
		serviceName      string
		serviceLabel     string
		unitName         string
		expectedLauncher bool
	}{
		{
			name:             "macos",
			targetOS:         "darwin",
			binary:           "/opt/rdev/rdev",
			releaseBundle:    "/opt/rdev/release-bundle.json",
			required:         "rdev,rdev-host,rdev-verify",
			kind:             "managed-mac-service-plan",
			generatedFile:    "managed-macos/service-plan.json",
			serviceLabel:     "com.example.rdev.host",
			expectedLauncher: true,
		},
		{
			name:             "linux",
			targetOS:         "linux",
			binary:           "/opt/rdev/rdev",
			releaseBundle:    "/opt/rdev/release-bundle.json",
			required:         "rdev,rdev-host,rdev-verify",
			kind:             "linux-managed-service-plan",
			generatedFile:    "managed-linux/linux-managed-service-plan.json",
			unitName:         "rdev-host.service",
			expectedLauncher: true,
		},
		{
			name:          "windows",
			targetOS:      "windows",
			binary:        `C:\Program Files\rdev\rdev.exe`,
			releaseBundle: `C:\Program Files\rdev\release-bundle.json`,
			required:      "rdev.exe,rdev-host.exe,rdev-verify.exe",
			kind:          "windows-managed-service-plan",
			generatedFile: "managed-windows/windows-managed-service-plan.json",
			serviceName:   "RemoteDevSkillkitHost",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "entry")
			plan, err := FromInvite(Options{
				InviteJSON:                     mustJSON(t, testInvite(t, model.HostModeManaged)),
				OutDir:                         out,
				TargetOS:                       tc.targetOS,
				Ownership:                      "owned",
				SessionMode:                    string(model.HostModeManaged),
				ManagedBinaryPath:              tc.binary,
				ManagedServiceName:             tc.serviceName,
				ManagedServiceLabel:            tc.serviceLabel,
				ManagedUnitName:                tc.unitName,
				ReleaseBundlePath:              tc.releaseBundle,
				ReleaseRootPublicKey:           "release-root:" + strings.Repeat("b", 43),
				ReleaseBundleRequiredArtifacts: tc.required,
				Now:                            time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.MissingInputs) != 0 {
				t.Fatalf("expected no missing inputs, got %#v", plan.MissingInputs)
			}
			if plan.EntryPackagePlan == nil || !plan.EntryPackagePlan.OK {
				t.Fatalf("expected managed entry package plan: %#v", plan.EntryPackagePlan)
			}
			if plan.EntryPackagePlan.SchemaVersion != EntryPackagePlanSchemaVersion ||
				plan.EntryPackagePlan.PackageMode != "reviewed-managed-service-connection-entry" ||
				plan.EntryPackagePlan.PlatformPlanKind != tc.kind {
				t.Fatalf("unexpected managed package wrapper: %#v", plan.EntryPackagePlan)
			}
			if !fileExists(filepath.Join(out, tc.generatedFile)) {
				t.Fatalf("expected generated managed plan %s", filepath.Join(out, tc.generatedFile))
			}
			if tc.expectedLauncher && !fileExists(plan.EntryPackagePlan.LauncherPath) {
				t.Fatalf("expected generated launcher/unit path: %#v", plan.EntryPackagePlan)
			}
			if !slices.Contains(plan.EntryPackagePlan.AgentOnlyParameters, "managed_binary_path") ||
				!slices.Contains(plan.EntryPackagePlan.AgentOnlyParameters, "release_bundle_path") ||
				!slices.Contains(plan.HumanSurface, "reviewed managed-service entry package after owned-host activation") {
				t.Fatalf("expected managed metadata and surface split: %#v", plan)
			}
		})
	}
}

func TestFromInviteRejectsManagedEntryForThirdParty(t *testing.T) {
	_, err := FromInvite(Options{
		InviteJSON:  mustJSON(t, testInvite(t, model.HostModeAttendedTemporary)),
		TargetOS:    "windows",
		Ownership:   "third-party",
		SessionMode: string(model.HostModeManaged),
	})
	if err == nil || !strings.Contains(err.Error(), "ownership=owned") {
		t.Fatalf("expected managed third-party rejection, got %v", err)
	}
}

func testInvite(t *testing.T, mode model.HostMode) agentinvite.Invite {
	t.Helper()
	ticket := model.Ticket{
		ID:           "tkt_test",
		Code:         "ABCD-1234",
		Mode:         mode,
		Status:       model.TicketStatusActive,
		TTLSeconds:   600,
		Capabilities: []string{"shell.user"},
		Reason:       "repair target",
		CreatedAt:    time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
		ExpiresAt:    time.Date(2026, 7, 2, 12, 10, 0, 0, time.UTC),
	}
	invite, err := agentinvite.New(agentinvite.Options{
		GatewayURL:            "https://api.example.com/v1",
		JoinURL:               "https://api.example.com/join/ABCD-1234",
		ManifestURL:           "https://api.example.com/v1/tickets/ABCD-1234/manifest",
		ManifestRootPublicKey: "manifest-root:" + strings.Repeat("d", 43),
		Ticket:                ticket,
		Transport:             "auto",
		NetworkScope:          "auto",
		AuthorityProfile:      "max-control",
		CreatedAt:             ticket.CreatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return invite
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
