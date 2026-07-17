package connectionentry

import (
	"encoding/json"
	"errors"
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

func TestBuildChecksRedactsPrivateInviteDetails(t *testing.T) {
	invite := testInvite(t, model.HostModeAttendedTemporary)
	plan := Plan{
		TargetOS:    "windows",
		SessionMode: string(model.HostModeAttendedTemporary),
		Ownership:   "third-party",
	}
	checks := buildChecks(invite, plan, "powershell.exe -File entry.ps1")
	sensitive := []string{
		invite.ConnectionEntry.EntryURL,
		invite.ManifestURL,
		invite.ManifestRootPublicKey,
		invite.Ticket.Code,
		invite.GatewayURL,
	}
	for _, check := range checks {
		for _, value := range sensitive {
			if value != "" && strings.Contains(check.Detail, value) {
				t.Fatalf("top-level check %q leaked private value %q in detail %q", check.Name, value, check.Detail)
			}
		}
	}
	for _, name := range []string{"entry_url", "manifest_url", "manifest_root_public_key", "ticket_code"} {
		var found bool
		for _, check := range checks {
			if check.Name != name {
				continue
			}
			found = true
			if !slices.Contains([]string{"missing", "invalid", "present", "valid"}, check.Detail) {
				t.Fatalf("check %q must expose only a status detail, got %q", name, check.Detail)
			}
		}
		if !found {
			t.Fatalf("expected check %q", name)
		}
	}
}

func TestPrivateCheckStatusDetails(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		valid bool
		want  string
	}{
		{name: "missing validation value", want: "missing"},
		{name: "invalid validation value", value: "not-a-url", want: "invalid"},
		{name: "valid validation value", value: "https://example.com", valid: true, want: "valid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validationStatusDetail(test.value, test.valid); got != test.want {
				t.Fatalf("validationStatusDetail(%q, %t) = %q, want %q", test.value, test.valid, got, test.want)
			}
		})
	}
	if got := presenceStatusDetail(" \t"); got != "missing" {
		t.Fatalf("presenceStatusDetail(blank) = %q, want missing", got)
	}
	if got := presenceStatusDetail("private-value"); got != "present" {
		t.Fatalf("presenceStatusDetail(value) = %q, want present", got)
	}
	if got := planArtifactStatus(false); got != "failed" {
		t.Fatalf("planArtifactStatus(false) = %q, want failed", got)
	}
}

func TestConnectionEntryPlanSelectionEdges(t *testing.T) {
	if got := normalizeTargetOS("Solaris"); got != "solaris" {
		t.Fatalf("normalizeTargetOS(Solaris) = %q", got)
	}
	if got := normalizeOwnership("inventory"); got != "inventory" {
		t.Fatalf("normalizeOwnership(inventory) = %q", got)
	}
	if got := inferOwnership(testInvite(t, model.HostModeManaged)); got != "owned" {
		t.Fatalf("managed invite ownership = %q", got)
	}
	if got := inferOwnership(testInvite(t, model.HostModeAttendedTemporary)); got != "third-party" {
		t.Fatalf("temporary invite ownership = %q", got)
	}
	if got := commandForOS(nil, "windows"); got != "" {
		t.Fatalf("nil command map = %q", got)
	}
	if _, err := selectSessionMode(testInvite(t, model.HostModeAttendedTemporary), "third-party", "invalid-mode"); err == nil {
		t.Fatal("invalid requested session mode must be rejected")
	}
	if allAcceptanceChecksPassed(nil) {
		t.Fatal("empty acceptance checks must not pass")
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty blanks = %q", got)
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

func TestFromInviteMaterializationFailureLeavesNoPrivateOutput(t *testing.T) {
	tests := []struct {
		name              string
		failurePhase      string
		createEmptyOutDir bool
	}{
		{name: "before top-level files", failurePhase: "before_top_level_files"},
		{name: "after top-level files", failurePhase: "after_top_level_files", createEmptyOutDir: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWindowsLayeredFixture(t)
			parentDir := t.TempDir()
			outDir := filepath.Join(parentDir, "entry")
			if test.createEmptyOutDir {
				if err := os.Mkdir(outDir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			fixture.options.OutDir = outDir

			sentinel := errors.New("injected materialization failure")
			previousHook := connectionEntryMaterializationFailureHook
			connectionEntryMaterializationFailureHook = func(phase string) error {
				if phase == test.failurePhase {
					return sentinel
				}
				return nil
			}
			t.Cleanup(func() { connectionEntryMaterializationFailureHook = previousHook })

			if _, err := FromInvite(fixture.options); !errors.Is(err, sentinel) {
				t.Fatalf("expected injected failure, got %v", err)
			}
			assertConnectionEntryOutputAbsentOrEmpty(t, outDir)
			assertNoConnectionEntryStagingDirs(t, parentDir, filepath.Base(outDir))
		})
	}
}

func TestFromInviteRejectsPendingArchiveReplacementAndRestoresEmptyOutput(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	parentDir := t.TempDir()
	outDir := filepath.Join(parentDir, "entry")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	originalOutDir, err := os.Stat(outDir)
	if err != nil {
		t.Fatal(err)
	}
	fixture.options.OutDir = outDir

	var movedArchivePath string
	previousHook := connectionEntryMaterializationFailureHook
	connectionEntryMaterializationFailureHook = func(phase string) error {
		if phase != "before_windows_layered_archive_publish" {
			return nil
		}
		if !fileExists(filepath.Join(outDir, "connection-entry-plan.json")) {
			return errors.New("final materialization directory was not published before archive publication")
		}
		if fileExists(filepath.Join(outDir, windowsLayeredArchiveName)) {
			return errors.New("archive was published before the prepublication hook")
		}
		matches, err := filepath.Glob(filepath.Join(parentDir, "."+windowsLayeredArchiveName+".tmp-*"))
		if err != nil {
			return err
		}
		if len(matches) != 1 {
			return errors.New("expected one validated sibling archive temporary file")
		}
		movedArchivePath = matches[0] + ".moved"
		if err := os.Rename(matches[0], movedArchivePath); err != nil {
			return err
		}
		return os.WriteFile(matches[0], []byte("replacement archive must not publish\n"), 0o600)
	}
	t.Cleanup(func() { connectionEntryMaterializationFailureHook = previousHook })

	if _, err := FromInvite(fixture.options); err == nil {
		t.Fatal("expected pending archive identity replacement to fail materialization")
	}
	restoredOutDir, err := os.Stat(outDir)
	if err != nil {
		t.Fatalf("preexisting empty output directory was not restored: %v", err)
	}
	if !os.SameFile(originalOutDir, restoredOutDir) {
		t.Fatal("materialization rollback did not restore the preexisting output directory")
	}
	assertConnectionEntryOutputAbsentOrEmpty(t, outDir)
	assertNoConnectionEntryStagingDirs(t, parentDir, filepath.Base(outDir))
	if movedArchivePath == "" {
		t.Fatal("pending archive prepublication hook did not run")
	}
	if content, err := os.ReadFile(movedArchivePath); err == nil && len(content) != 0 {
		t.Fatalf("pending archive replacement failure left %d sensitive bytes on the original handle", len(content))
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("inspect moved pending archive: %v", err)
	}
}

func TestFromInviteMaterializationPublishesOnlyFinalPaths(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	parentDir := t.TempDir()
	outDir := filepath.Join(parentDir, "entry")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.options.OutDir = outDir

	plan, err := FromInvite(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	assertConnectionEntryPlanUsesFinalPaths(t, plan, outDir)
	assertNoConnectionEntryStagingDirs(t, parentDir, filepath.Base(outDir))

	planJSONPath := filepath.Join(outDir, "connection-entry-plan.json")
	content, err := os.ReadFile(planJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), ".entry.staging-") {
		t.Fatalf("persisted materialization plan leaked a staging path:\n%s", content)
	}
	var persisted Plan
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatal(err)
	}
	assertConnectionEntryPlanUsesFinalPaths(t, persisted, outDir)

	err = filepath.WalkDir(outDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".json") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), ".entry.staging-") {
			t.Errorf("persisted JSON %s leaked a staging path:\n%s", path, content)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFromInviteRejectsNonEmptyOutputWithoutCreatingStaging(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	parentDir := t.TempDir()
	outDir := filepath.Join(parentDir, "entry")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(outDir, "keep.txt")
	if err := os.WriteFile(markerPath, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.options.OutDir = outDir

	if _, err := FromInvite(fixture.options); err == nil || !strings.Contains(err.Error(), "out directory must be empty") {
		t.Fatalf("expected non-empty output rejection, got %v", err)
	}
	if !fileExists(markerPath) {
		t.Fatal("non-empty output rejection removed the existing file")
	}
	assertNoConnectionEntryStagingDirs(t, parentDir, filepath.Base(outDir))
}

func assertConnectionEntryOutputAbsentOrEmpty(t *testing.T, outDir string) {
	t.Helper()
	entries, err := os.ReadDir(outDir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed materialization left private output in %s: %#v", outDir, entries)
	}
}

func assertNoConnectionEntryStagingDirs(t *testing.T, parentDir, outBase string) {
	t.Helper()
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		t.Fatal(err)
	}
	prefixes := []string{"." + outBase + ".staging-", "." + outBase + ".empty-"}
	for _, entry := range entries {
		for _, prefix := range prefixes {
			if strings.HasPrefix(entry.Name(), prefix) {
				t.Fatalf("materialization left temporary directory %s", filepath.Join(parentDir, entry.Name()))
			}
		}
	}
}

func assertConnectionEntryPlanUsesFinalPaths(t *testing.T, plan Plan, outDir string) {
	t.Helper()
	finalDir, err := filepath.Abs(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.OutDir != finalDir {
		t.Fatalf("plan out_dir = %q, want %q", plan.OutDir, finalDir)
	}
	paths := map[string]string{
		"human_message": plan.HumanMessagePath,
	}
	if plan.RunnerPlan == nil {
		t.Fatal("expected runner plan")
	}
	paths["runner_manifest"] = plan.RunnerPlan.ManifestPath
	paths["runner_launcher"] = plan.RunnerPlan.LauncherPath
	paths["runner_plan"] = plan.RunnerPlan.PlanPath
	if plan.EntryPackagePlan == nil {
		t.Fatal("expected entry package plan")
	}
	paths["entry_plan"] = filepath.Join(finalDir, filepath.FromSlash(plan.EntryPackagePlan.PlanPath))
	paths["entry_launcher"] = filepath.Join(finalDir, filepath.FromSlash(plan.EntryPackagePlan.LauncherPath))
	if plan.EntryPackagePlan.ArchivePath != "" {
		paths["entry_archive"] = filepath.Join(finalDir, filepath.FromSlash(plan.EntryPackagePlan.ArchivePath))
	}
	for _, file := range plan.GeneratedFiles {
		paths["generated:"+file.Path] = file.Path
	}
	for name, path := range paths {
		if path == "" {
			t.Fatalf("%s path is empty", name)
		}
		relative, err := filepath.Rel(finalDir, path)
		if err != nil {
			t.Fatal(err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			t.Fatalf("%s path %q is outside final out dir %q", name, path, finalDir)
		}
		if strings.Contains(path, ".entry.staging-") {
			t.Fatalf("%s path leaked staging directory: %q", name, path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s final path is not published: %v", name, err)
		}
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
