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
